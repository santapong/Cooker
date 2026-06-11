package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// APITokenService owns the business rules for personal-access /
// service-account tokens (product-plan Tier 1): the role cap (you can't
// mint a token more privileged than yourself), ownership (you manage your
// own tokens; admins manage all), and the no-self-replication rule (a
// request authenticated *by* a token may not create or delete tokens).
// The handler stays thin and routes every decision through here.
type APITokenService struct {
	tokens store.APITokenStore
}

// NewAPITokenService wires the token service to its store.
func NewAPITokenService(tokens store.APITokenStore) *APITokenService {
	return &APITokenService{tokens: tokens}
}

// Sentinel errors the handler maps to HTTP status codes.
var (
	// ErrRoleTooHigh is returned by Create when the requested role
	// out-ranks the caller's own role. Maps to 403.
	ErrRoleTooHigh = errors.New("apitoken: requested role exceeds caller's role")
	// ErrInvalidRole is returned by Create for an unknown role string.
	// Maps to 400.
	ErrInvalidRole = errors.New("apitoken: unknown role")
	// ErrTokenActorForbidden is returned when a request authenticated by
	// an API token tries to create or delete a token (no self-
	// replication). Maps to 403.
	ErrTokenActorForbidden = errors.New("apitoken: token-authenticated requests cannot manage tokens")
	// ErrNotOwner is returned by Delete when a non-admin caller targets a
	// token they did not create. Maps to 404 (not 403) so a probing
	// caller can't enumerate other users' token ids.
	ErrNotOwner = errors.New("apitoken: not the owner")
	// ErrAdminOnly is returned by List when a non-admin requests all=true.
	// Maps to 403: this is an authorization refusal of a known operation,
	// not an attempt to probe a specific id, so 403 (not 404) is correct.
	ErrAdminOnly = errors.New("apitoken: admin role required")
	// ErrExpiryInPast is returned by Create when the supplied expiry is
	// already in the past. Maps to 400.
	ErrExpiryInPast = errors.New("apitoken: expiry is in the past")
	// ErrMFARequired is returned by Delete when an admin targets a token
	// they did NOT create and has not satisfied the step-up MFA gate.
	// Own-token deletion never triggers this. Maps to 403 with the
	// mfa_required body the frontend recognises.
	ErrMFARequired = errors.New("apitoken: mfa required to delete another user's token")
)

// CreateParams carries a token-creation request through the service. The
// caller identity (Sub/Email/Roles) comes from the authenticated request
// claims, never the request body.
type CreateParams struct {
	Name        string
	Role        string
	ExpiresAt   *time.Time
	CallerSub   string
	CallerEmail string
	CallerRoles []string
	// CallerIsToken is true when the request is itself authenticated by an
	// API token (Subject prefixed `token:`). Such requests are refused to
	// prevent a leaked token from minting fresh tokens.
	CallerIsToken bool
}

// DeleteParams carries a token-deletion request. CallerACR / CallerAMR and
// MFAValues drive the inline step-up gate that protects an admin deleting
// ANOTHER user's token; they are unused when the caller owns the token.
type DeleteParams struct {
	ID          string
	CallerSub   string
	CallerRoles []string
	CallerACR   string
	CallerAMR   []string
	// MFAValues is the operator-configured COOKER_OIDC_MFA_ACR_VALUES set.
	// Empty disables the step-up gate (matching RequireMFA passthrough).
	MFAValues []string
	// CallerIsToken refuses the delete outright (no self-replication).
	CallerIsToken bool
}

// roleRank assigns a privilege rank used for the "role <= own role" cap.
// admin dominates everything. operator and approver sit at the same mid
// rank but are NOT interchangeable — the approver role carries the
// promotion-approval right an operator lacks, so canMintRole treats the
// approver role specially rather than relying on rank alone. viewer is the
// floor. An unknown role ranks 0 and is rejected before this matters.
func roleRank(role string) int {
	switch auth.Role(role) {
	case auth.RoleAdmin:
		return 3
	case auth.RoleOperator, auth.RoleApprover:
		return 2
	case auth.RoleViewer:
		return 1
	default:
		return 0
	}
}

// validRole reports whether role is one of the four known RBAC roles.
func validRole(role string) bool {
	switch auth.Role(role) {
	case auth.RoleAdmin, auth.RoleOperator, auth.RoleApprover, auth.RoleViewer:
		return true
	default:
		return false
	}
}

// canMintRole encodes the role cap: a caller may mint a token whose role
// is one they could exercise themselves. An admin may mint any role. For
// the orthogonal approver role we require the caller to actually hold
// admin or approver — a plain operator (same numeric rank) must not be
// able to escalate into promotion-approval rights via a token. Otherwise
// the requested role must not out-rank the caller's highest role.
func canMintRole(callerRoles []string, requested string) bool {
	highest := 0
	holdsAdmin := false
	holdsApprover := false
	for _, r := range callerRoles {
		if rank := roleRank(r); rank > highest {
			highest = rank
		}
		switch auth.Role(r) {
		case auth.RoleAdmin:
			holdsAdmin = true
		case auth.RoleApprover:
			holdsApprover = true
		}
	}
	if holdsAdmin {
		return true
	}
	if auth.Role(requested) == auth.RoleApprover {
		return holdsApprover
	}
	return roleRank(requested) <= highest
}

// Create mints a token. It enforces (in order): no token may create a
// token; the role must be known; the expiry must not be in the past; and
// the role must pass the cap. On success it returns the persisted token
// AND the one-time plaintext — the caller is responsible for surfacing the
// plaintext to the operator exactly once and never persisting it.
func (s *APITokenService) Create(ctx context.Context, p CreateParams) (*model.APIToken, string, error) {
	if p.CallerIsToken {
		return nil, "", ErrTokenActorForbidden
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return nil, "", fmt.Errorf("apitoken: name is required")
	}
	if !validRole(p.Role) {
		return nil, "", ErrInvalidRole
	}
	if p.ExpiresAt != nil && !p.ExpiresAt.After(time.Now()) {
		return nil, "", ErrExpiryInPast
	}
	if !canMintRole(p.CallerRoles, p.Role) {
		return nil, "", ErrRoleTooHigh
	}

	plaintext, hash, displayPrefix, err := model.GenerateAPIToken()
	if err != nil {
		return nil, "", fmt.Errorf("apitoken: generate: %w", err)
	}

	tok := &model.APIToken{
		ID:             uuid.NewString(),
		Name:           name,
		DisplayPrefix:  displayPrefix,
		Hash:           hash,
		Role:           p.Role,
		CreatedBySub:   p.CallerSub,
		CreatedByEmail: p.CallerEmail,
		ExpiresAt:      p.ExpiresAt,
	}
	if err := s.tokens.Create(ctx, tok); err != nil {
		return nil, "", fmt.Errorf("apitoken: persist: %w", err)
	}
	return tok, plaintext, nil
}

// List returns tokens visible to the caller. A non-admin sees only the
// tokens they created. An admin may pass all=true to see every token;
// all=false still scopes an admin to their own tokens (so "my tokens"
// works uniformly). Hash is never serialised (json:"-" on the model), so
// these are safe to return directly.
func (s *APITokenService) List(ctx context.Context, callerSub string, callerRoles []string, all bool) ([]*model.APIToken, error) {
	if all {
		if !hasRole(callerRoles, auth.RoleAdmin) {
			return nil, ErrAdminOnly
		}
		return s.tokens.ListAll(ctx)
	}
	return s.tokens.ListByCreator(ctx, callerSub)
}

// Delete revokes a token. A token-authenticated request may never delete
// a token (no self-replication / no self-revocation cascade). A non-admin
// may delete only a token they created; targeting someone else's token
// returns ErrNotOwner, surfaced as 404 so ids can't be probed. An admin
// may delete any token — but deleting ANOTHER user's token is treated as a
// destructive admin action and requires the step-up MFA gate (when one is
// configured), mirroring the DELETE-pipeline / secret-reveal routes.
// Deleting one's own token never requires MFA.
func (s *APITokenService) Delete(ctx context.Context, p DeleteParams) error {
	if p.CallerIsToken {
		return ErrTokenActorForbidden
	}
	tok, err := s.tokens.Get(ctx, p.ID)
	if err != nil {
		return err // ErrNotFound flows through to a 404
	}
	isOwner := tok.CreatedBySub == p.CallerSub
	if !isOwner {
		if !hasRole(p.CallerRoles, auth.RoleAdmin) {
			return ErrNotOwner
		}
		// Admin deleting someone else's token: enforce step-up MFA when
		// the operator has configured it. Own-token deletes skip this.
		if !mfaSatisfied(p.CallerACR, p.CallerAMR, p.MFAValues) {
			return ErrMFARequired
		}
	}
	return s.tokens.Delete(ctx, p.ID)
}

// mfaSatisfied reports whether the caller's ACR/AMR claims satisfy the
// configured MFA value set. An empty config means MFA is disabled and the
// gate passes (matching auth.RequireMFA's passthrough). A token-
// authenticated caller never reaches here for a cross-owner delete (it is
// refused earlier), and tokens carry no ACR/AMR anyway.
func mfaSatisfied(acr string, amr, configured []string) bool {
	if len(configured) == 0 {
		return true
	}
	allowed := make(map[string]bool, len(configured))
	for _, v := range configured {
		if v != "" {
			allowed[v] = true
		}
	}
	if allowed[acr] {
		return true
	}
	for _, m := range amr {
		if allowed[m] {
			return true
		}
	}
	return false
}

// hasRole reports whether roles contains the given role.
func hasRole(roles []string, want auth.Role) bool {
	for _, r := range roles {
		if auth.Role(r) == want {
			return true
		}
	}
	return false
}
