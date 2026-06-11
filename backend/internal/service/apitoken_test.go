package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
)

func newTokenService(t *testing.T) (*APITokenService, store.APITokenStore) {
	t.Helper()
	st := memory.New().APITokens
	return NewAPITokenService(st), st
}

func adminRoles() []string    { return []string{string(auth.RoleAdmin)} }
func operatorRoles() []string { return []string{string(auth.RoleOperator)} }
func viewerRoles() []string   { return []string{string(auth.RoleViewer)} }

func TestAPIToken_Create_ReturnsPlaintextOnce(t *testing.T) {
	svc, st := newTokenService(t)
	tok, plaintext, err := svc.Create(context.Background(), CreateParams{
		Name:        "ci",
		Role:        string(auth.RoleOperator),
		CallerSub:   "u1",
		CallerEmail: "u1@x.com",
		CallerRoles: adminRoles(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.HasPrefix(plaintext, model.APITokenPrefix) {
		t.Errorf("plaintext should carry %q prefix; got %q", model.APITokenPrefix, plaintext)
	}
	// The stored row holds the hash of the plaintext, never the plaintext.
	if tok.Hash != model.HashAPIToken(plaintext) {
		t.Errorf("stored hash does not match plaintext hash")
	}
	if tok.Hash == plaintext {
		t.Errorf("hash must not equal plaintext")
	}
	// DisplayPrefix is a short, non-secret identifier.
	if len(tok.DisplayPrefix) > len(plaintext) || !strings.HasPrefix(plaintext, tok.DisplayPrefix) {
		t.Errorf("display prefix %q is not a prefix of the plaintext", tok.DisplayPrefix)
	}
	// It is retrievable by hash (the auth path), but the plaintext is gone.
	got, err := st.GetByHash(context.Background(), tok.Hash)
	if err != nil || got.ID != tok.ID {
		t.Errorf("token not retrievable by hash: %v", err)
	}
}

func TestAPIToken_Create_RoleCap(t *testing.T) {
	svc, _ := newTokenService(t)
	cases := []struct {
		name        string
		callerRoles []string
		requested   string
		wantErr     error
	}{
		{"viewer cannot mint admin", viewerRoles(), string(auth.RoleAdmin), ErrRoleTooHigh},
		{"viewer cannot mint operator", viewerRoles(), string(auth.RoleOperator), ErrRoleTooHigh},
		{"viewer can mint viewer", viewerRoles(), string(auth.RoleViewer), nil},
		{"operator cannot mint admin", operatorRoles(), string(auth.RoleAdmin), ErrRoleTooHigh},
		{"operator can mint operator", operatorRoles(), string(auth.RoleOperator), nil},
		{"operator can mint viewer", operatorRoles(), string(auth.RoleViewer), nil},
		{"operator cannot mint approver", operatorRoles(), string(auth.RoleApprover), ErrRoleTooHigh},
		{"admin can mint admin", adminRoles(), string(auth.RoleAdmin), nil},
		{"admin can mint approver", adminRoles(), string(auth.RoleApprover), nil},
		{"approver can mint approver", []string{string(auth.RoleApprover)}, string(auth.RoleApprover), nil},
		{"approver can mint viewer", []string{string(auth.RoleApprover)}, string(auth.RoleViewer), nil},
		{"unknown role rejected", adminRoles(), "wizard", ErrInvalidRole},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := svc.Create(context.Background(), CreateParams{
				Name:        "t",
				Role:        tc.requested,
				CallerSub:   "u1",
				CallerRoles: tc.callerRoles,
			})
			if tc.wantErr == nil && err != nil {
				t.Fatalf("expected success; got %v", err)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected %v; got %v", tc.wantErr, err)
			}
		})
	}
}

func TestAPIToken_Create_NoTokenSelfReplication(t *testing.T) {
	svc, _ := newTokenService(t)
	_, _, err := svc.Create(context.Background(), CreateParams{
		Name:          "spawn",
		Role:          string(auth.RoleViewer),
		CallerSub:     "token:abc",
		CallerRoles:   adminRoles(), // even an admin-role token can't mint
		CallerIsToken: true,
	})
	if !errors.Is(err, ErrTokenActorForbidden) {
		t.Fatalf("token-authenticated create must be forbidden; got %v", err)
	}
}

func TestAPIToken_Create_ExpiryInPastRejected(t *testing.T) {
	svc, _ := newTokenService(t)
	past := time.Now().Add(-time.Minute)
	_, _, err := svc.Create(context.Background(), CreateParams{
		Name:        "t",
		Role:        string(auth.RoleViewer),
		ExpiresAt:   &past,
		CallerSub:   "u1",
		CallerRoles: adminRoles(),
	})
	if !errors.Is(err, ErrExpiryInPast) {
		t.Fatalf("expected ErrExpiryInPast; got %v", err)
	}
}

func TestAPIToken_List_OwnershipAndAdminAll(t *testing.T) {
	svc, _ := newTokenService(t)
	ctx := context.Background()
	mustCreate := func(sub string, roles []string) {
		if _, _, err := svc.Create(ctx, CreateParams{Name: "t", Role: string(auth.RoleViewer), CallerSub: sub, CallerRoles: roles}); err != nil {
			t.Fatalf("seed create: %v", err)
		}
	}
	mustCreate("alice", operatorRoles())
	mustCreate("alice", operatorRoles())
	mustCreate("bob", operatorRoles())

	// Alice sees only her two.
	mine, err := svc.List(ctx, "alice", operatorRoles(), false)
	if err != nil || len(mine) != 2 {
		t.Fatalf("alice's own list = %d (%v), want 2", len(mine), err)
	}

	// A non-admin asking all=true is refused.
	if _, err := svc.List(ctx, "alice", operatorRoles(), true); !errors.Is(err, ErrAdminOnly) {
		t.Fatalf("non-admin all=true should be ErrAdminOnly; got %v", err)
	}

	// Admin all=true sees everything.
	all, err := svc.List(ctx, "carol", adminRoles(), true)
	if err != nil || len(all) != 3 {
		t.Fatalf("admin all list = %d (%v), want 3", len(all), err)
	}
}

func TestAPIToken_Delete_Ownership(t *testing.T) {
	svc, _ := newTokenService(t)
	ctx := context.Background()
	tok, _, err := svc.Create(ctx, CreateParams{Name: "t", Role: string(auth.RoleViewer), CallerSub: "alice", CallerRoles: operatorRoles()})
	if err != nil {
		t.Fatal(err)
	}

	// Bob (non-admin) cannot delete Alice's token → ErrNotOwner (→ 404).
	if err := svc.Delete(ctx, DeleteParams{ID: tok.ID, CallerSub: "bob", CallerRoles: operatorRoles()}); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("non-owner delete should be ErrNotOwner; got %v", err)
	}
	// Alice can delete her own.
	if err := svc.Delete(ctx, DeleteParams{ID: tok.ID, CallerSub: "alice", CallerRoles: operatorRoles()}); err != nil {
		t.Fatalf("owner delete failed: %v", err)
	}
	// Now gone.
	if err := svc.Delete(ctx, DeleteParams{ID: tok.ID, CallerSub: "alice", CallerRoles: operatorRoles()}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second delete should be ErrNotFound; got %v", err)
	}
}

func TestAPIToken_Delete_NoTokenSelfReplication(t *testing.T) {
	svc, _ := newTokenService(t)
	ctx := context.Background()
	tok, _, _ := svc.Create(ctx, CreateParams{Name: "t", Role: string(auth.RoleViewer), CallerSub: "alice", CallerRoles: operatorRoles()})

	if err := svc.Delete(ctx, DeleteParams{ID: tok.ID, CallerSub: "token:x", CallerRoles: adminRoles(), CallerIsToken: true}); !errors.Is(err, ErrTokenActorForbidden) {
		t.Fatalf("token-authenticated delete must be forbidden; got %v", err)
	}
}

// TestAPIToken_Delete_AdminCrossOwnerMFA: an admin deleting ANOTHER user's
// token is MFA-gated when MFA is configured; satisfying the ACR passes;
// own-token delete never requires MFA.
func TestAPIToken_Delete_AdminCrossOwnerMFA(t *testing.T) {
	svc, _ := newTokenService(t)
	ctx := context.Background()
	mfa := []string{"mfa"}

	// Alice's token; admin Carol tries to delete it.
	tok, _, _ := svc.Create(ctx, CreateParams{Name: "t", Role: string(auth.RoleViewer), CallerSub: "alice", CallerRoles: operatorRoles()})

	// Admin without MFA claim → ErrMFARequired.
	if err := svc.Delete(ctx, DeleteParams{ID: tok.ID, CallerSub: "carol", CallerRoles: adminRoles(), MFAValues: mfa}); !errors.Is(err, ErrMFARequired) {
		t.Fatalf("admin cross-owner delete without MFA should be ErrMFARequired; got %v", err)
	}
	// Admin WITH the MFA acr → allowed.
	if err := svc.Delete(ctx, DeleteParams{ID: tok.ID, CallerSub: "carol", CallerRoles: adminRoles(), CallerACR: "mfa", MFAValues: mfa}); err != nil {
		t.Fatalf("admin cross-owner delete with MFA should succeed; got %v", err)
	}

	// A second token owned by Dave; Dave (no MFA configured-claim) deletes
	// his OWN → must succeed regardless of MFA config.
	tok2, _, _ := svc.Create(ctx, CreateParams{Name: "t2", Role: string(auth.RoleViewer), CallerSub: "dave", CallerRoles: operatorRoles()})
	if err := svc.Delete(ctx, DeleteParams{ID: tok2.ID, CallerSub: "dave", CallerRoles: operatorRoles(), MFAValues: mfa}); err != nil {
		t.Fatalf("own-token delete must not require MFA; got %v", err)
	}
}
