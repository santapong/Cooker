package service

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/santapong/cooker/internal/entitlements"
	"github.com/santapong/cooker/internal/license"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// LicenseService owns the business rules for self-hosted licensing (M2 —
// docs/launch/01-billing-monetization.md §4): verifying a pasted license
// token against the configured vendor public key, rejecting tokens that
// are invalid or already expired at install time, persisting the single
// active license, and resolving the current entitlements. The handler
// stays thin and routes every decision through here. No HTTP types cross
// this boundary.
//
// The defining product rule lives here: an EXPIRED license is rejected at
// INSTALL (you cannot newly install something already dead), but a license
// that expires LATER simply degrades to Free at read time via the
// entitlements engine — it never errors and never bricks the instance.
type LicenseService struct {
	store store.LicenseStore
	// pubKeys are the vendor Ed25519 public keys used to verify tokens. A
	// token verifies if ANY key in the set validates its signature, which
	// supports key rotation (trust the outgoing and incoming keys during a
	// rollover window). An empty/nil slice (operator set no public key)
	// disables installs: every Install returns ErrVerificationDisabled and
	// Current falls back to the stored license / Free.
	pubKeys []ed25519.PublicKey
}

// NewLicenseService wires the service to its store and the configured
// vendor public keys. pubKeys may be nil/empty when no public key is
// configured; the service then refuses installs but still serves the
// current license and Free entitlements. When multiple keys are supplied,
// a token signed by any of them verifies (key rotation).
func NewLicenseService(st store.LicenseStore, pubKeys []ed25519.PublicKey) *LicenseService {
	return &LicenseService{store: st, pubKeys: pubKeys}
}

// Sentinel errors the handler maps to HTTP status codes.
var (
	// ErrLicenseInvalid is returned by Install when the token fails
	// signature verification or is malformed. Maps to 400. Wraps the
	// underlying license.Err* so callers can inspect if needed.
	ErrLicenseInvalid = errors.New("license: token is invalid")
	// ErrLicenseExpired is returned by Install when the token verifies but
	// is already expired as of now (you cannot install a dead license).
	// Maps to 400. A license that expires in the future installs fine and
	// later degrades to Free at read time.
	ErrLicenseExpired = errors.New("license: token is already expired")
	// ErrVerificationDisabled is returned by Install when no vendor public
	// key was configured, so no token can be verified. Maps to 400.
	ErrVerificationDisabled = errors.New("license: verification is disabled (no public key configured)")
	// ErrLicenseEmpty is returned by Install for a blank token. Maps to 400.
	ErrLicenseEmpty = errors.New("license: token is required")
)

// Install verifies rawToken against the configured public key, rejects an
// invalid or already-expired token, then persists a model.License as the
// single active license, recording who installed it. On success it returns
// the persisted record. The plaintext RawToken is stored (never echoed to
// clients — model.License.RawToken is json:"-") so the active license can
// be re-verified or rotated later.
func (s *LicenseService) Install(ctx context.Context, rawToken, installedBySub, installedByEmail string) (*model.License, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return nil, ErrLicenseEmpty
	}
	if len(s.pubKeys) == 0 {
		return nil, ErrVerificationDisabled
	}
	claims, err := license.VerifyAny(rawToken, s.pubKeys)
	if err != nil {
		// Malformed / bad-signature / wrong-key all surface as "invalid"
		// to the operator — we wrap the specific cause for logs/debugging.
		return nil, fmt.Errorf("%w: %v", ErrLicenseInvalid, err)
	}
	now := time.Now()
	if claims.IsExpired(now) {
		return nil, ErrLicenseExpired
	}

	lic := &model.License{
		ID:               uuid.NewString(),
		RawToken:         rawToken,
		Plan:             claims.Plan,
		Seats:            claims.Seats,
		Features:         claims.Features,
		Customer:         claims.Customer,
		IssuedAt:         claims.IssuedAtTime(),
		ExpiresAt:        claims.ExpiresAtTime(),
		InstalledAt:      now,
		InstalledBySub:   installedBySub,
		InstalledByEmail: installedByEmail,
	}
	if err := s.store.Set(ctx, lic); err != nil {
		return nil, fmt.Errorf("license: persist: %w", err)
	}
	return lic, nil
}

// Current returns the installed license (or nil when none is installed)
// and the entitlements it resolves to as of now. A missing license is NOT
// an error — it degrades to Free. Any other store error is wrapped and
// returned (with Free entitlements as a safe fallback so a transient store
// failure still yields a usable, conservative result).
func (s *LicenseService) Current(ctx context.Context) (*model.License, entitlements.Entitlements, error) {
	now := time.Now()
	lic, err := s.store.GetActive(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return nil, entitlements.Free(), nil
	}
	if err != nil {
		return nil, entitlements.Free(), fmt.Errorf("license: load active: %w", err)
	}
	return lic, entitlements.FromLicense(lic, now), nil
}

// Entitlements is a convenience that returns just the resolved
// entitlements for the current install, degrading to Free on any error.
// It is the read path the entitlement gate (server.entitlementGate)
// consults; it never errors so the gate can stay simple.
func (s *LicenseService) Entitlements(ctx context.Context) entitlements.Entitlements {
	_, ent, err := s.Current(ctx)
	if err != nil {
		return entitlements.Free()
	}
	return ent
}

// Remove clears the installed license, reverting the instance to Free.
// Idempotent: removing when none is installed is not an error.
func (s *LicenseService) Remove(ctx context.Context) error {
	if err := s.store.Delete(ctx); err != nil {
		return fmt.Errorf("license: remove: %w", err)
	}
	return nil
}
