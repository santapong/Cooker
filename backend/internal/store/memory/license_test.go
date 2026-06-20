package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

func mkLicense(plan string, expires *time.Time) *model.License {
	return &model.License{
		ID:               "active",
		RawToken:         "ck_lic_" + plan,
		Plan:             plan,
		Seats:            3,
		Features:         []string{"multi-replica", "oidc"},
		Customer:         "Acme Corp",
		IssuedAt:         time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		ExpiresAt:        expires,
		InstalledBySub:   "alice",
		InstalledByEmail: "alice@x.com",
	}
}

func TestLicenseStore_SetGetActiveDelete(t *testing.T) {
	st := New().Licenses
	ctx := context.Background()

	// Nothing installed yet.
	if _, err := st.GetActive(ctx); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetActive with no license should be ErrNotFound; got %v", err)
	}

	exp := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	lic := mkLicense("crew", &exp)
	if err := st.Set(ctx, lic); err != nil {
		t.Fatalf("set: %v", err)
	}
	if lic.InstalledAt.IsZero() {
		t.Fatalf("Set should stamp InstalledAt when zero")
	}

	got, err := st.GetActive(ctx)
	if err != nil {
		t.Fatalf("GetActive: %v", err)
	}
	if got.Plan != "crew" || got.Seats != 3 || got.Customer != "Acme Corp" {
		t.Fatalf("unexpected license: %+v", got)
	}
	if len(got.Features) != 2 || got.Features[0] != "multi-replica" {
		t.Fatalf("features round-trip failed: %+v", got.Features)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(exp) {
		t.Fatalf("expiresAt round-trip failed: %v", got.ExpiresAt)
	}

	// Set replaces (single active license).
	if err := st.Set(ctx, mkLicense("constellation", nil)); err != nil {
		t.Fatalf("replace set: %v", err)
	}
	got, err = st.GetActive(ctx)
	if err != nil {
		t.Fatalf("GetActive after replace: %v", err)
	}
	if got.Plan != "constellation" || got.ExpiresAt != nil {
		t.Fatalf("replace did not take: %+v", got)
	}

	// Delete clears it.
	if err := st.Delete(ctx); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := st.GetActive(ctx); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetActive after delete should be ErrNotFound; got %v", err)
	}
	// Delete is idempotent when nothing is installed.
	if err := st.Delete(ctx); err != nil {
		t.Fatalf("idempotent delete should be a no-op; got %v", err)
	}
}

// TestLicenseStore_ReturnsCopies confirms mutating a returned license
// does not corrupt the stored record (the store hands out copies).
func TestLicenseStore_ReturnsCopies(t *testing.T) {
	st := New().Licenses
	ctx := context.Background()
	exp := time.Now().Add(time.Hour)
	if err := st.Set(ctx, mkLicense("crew", &exp)); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetActive(ctx)
	got.Plan = "constellation"
	got.Features[0] = "tampered"
	*got.ExpiresAt = time.Unix(0, 0)
	fresh, _ := st.GetActive(ctx)
	if fresh.Plan == "constellation" {
		t.Errorf("store returned a mutable reference (plan)")
	}
	if fresh.Features[0] == "tampered" {
		t.Errorf("store returned a mutable reference (features)")
	}
	if fresh.ExpiresAt.Equal(time.Unix(0, 0)) {
		t.Errorf("store returned a mutable reference (expiresAt)")
	}
}

func TestLicense_IsExpiredIsActive(t *testing.T) {
	now := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	cases := []struct {
		name        string
		lic         *model.License
		wantExpired bool
		wantActive  bool
	}{
		{"perpetual", &model.License{}, false, true},
		{"future expiry", &model.License{ExpiresAt: &future}, false, true},
		{"past expiry", &model.License{ExpiresAt: &past}, true, false},
		{"expires exactly now", &model.License{ExpiresAt: &now}, true, false},
		{"nil receiver", nil, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.lic != nil {
				if got := tc.lic.IsExpired(now); got != tc.wantExpired {
					t.Errorf("IsExpired = %v, want %v", got, tc.wantExpired)
				}
			}
			if got := tc.lic.IsActive(now); got != tc.wantActive {
				t.Errorf("IsActive = %v, want %v", got, tc.wantActive)
			}
		})
	}
}
