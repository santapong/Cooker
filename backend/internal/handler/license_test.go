package handler

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/license"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/store/memory"
)

// newLicenseTestHandler returns a Handler whose License service is wired
// over an in-memory store and the supplied verifier public keys. Passing
// nil keys models the "no public key configured" posture (installs are
// refused with ErrVerificationDisabled). The store is shared with the
// returned handler so a successful Install is observable via GetLicense.
func newLicenseTestHandler(t *testing.T, pubKeys []ed25519.PublicKey) *Handler {
	t.Helper()
	st := memory.New()
	h := &Handler{}
	h.License = service.NewLicenseService(st.Licenses, pubKeys)
	return h
}

// signLicenseToken mints a signed license token with the given private key
// and claims, mirroring cmd/cooker-license's sign path (the one wire format).
func signLicenseToken(t *testing.T, priv ed25519.PrivateKey, claims *license.Claims) string {
	t.Helper()
	payload, err := license.MarshalClaims(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	return license.Encode(payload, ed25519.Sign(priv, payload))
}

// licenseRouter mounts the three license routes mirroring production:
// GET is readable by any authenticated user; POST/DELETE are admin-only
// (auth.RequireRole(auth.RoleAdmin)) — see server/router.go.
func licenseRouter(h *Handler, claims *auth.Claims) *gin.Engine {
	r := gin.New()
	admin := auth.RequireRole(auth.RoleAdmin)
	r.GET("/license", withUser(h.GetLicense, claims))
	r.POST("/license", withUser(admin, claims), h.InstallLicense)
	r.DELETE("/license", withUser(admin, claims), h.DeleteLicense)
	return r
}

func TestLicense_Handler_GetStatusNoneWhenUninstalled(t *testing.T) {
	h := newLicenseTestHandler(t, nil)
	viewer := &auth.Claims{Subject: "v", Email: "v@x.com", Roles: []string{string(auth.RoleViewer)}}
	r := licenseRouter(h, viewer)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodGet, "/license", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("get: %d %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != licenseStatusNone {
		t.Errorf("status = %v, want %q", resp["status"], licenseStatusNone)
	}
	if _, ok := resp["plan"]; !ok {
		t.Errorf("Free entitlements should still report a plan: %s", w.Body.String())
	}
	// With no license installed there is no installer identity to leak.
	if strings.Contains(w.Body.String(), "installedBy") {
		t.Errorf("uninstalled GET must not carry installedBy*: %s", w.Body.String())
	}
}

func TestLicense_Handler_GetActiveShapeAndNoTokenLeak(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	h := newLicenseTestHandler(t, []ed25519.PublicKey{pub})
	admin := &auth.Claims{Subject: "admin-sub", Email: "admin@secret.example", Roles: []string{string(auth.RoleAdmin)}}
	r := licenseRouter(h, admin)

	exp := time.Now().Add(8760 * time.Hour).Unix()
	rawToken := signLicenseToken(t, priv, &license.Claims{
		Plan:      "crew",
		Seats:     5,
		Customer:  "Acme Inc",
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: exp,
	})

	// Install (admin POST) → 201.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/license", map[string]any{"token": rawToken}))
	if w.Code != http.StatusCreated {
		t.Fatalf("install: %d %s", w.Code, w.Body.String())
	}

	// GET → active shape with plan/seats/customer/expiresAt/installedAt.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodGet, "/license", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("get: %d %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	var resp map[string]any
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != licenseStatusActive {
		t.Errorf("status = %v, want %q", resp["status"], licenseStatusActive)
	}
	if resp["plan"] != "crew" {
		t.Errorf("plan = %v, want crew", resp["plan"])
	}
	if resp["customer"] != "Acme Inc" {
		t.Errorf("customer = %v, want Acme Inc", resp["customer"])
	}
	for _, k := range []string{"seats", "features", "entitlements", "expiresAt", "installedAt"} {
		if _, ok := resp[k]; !ok {
			t.Errorf("active GET missing %q: %s", k, body)
		}
	}

	// CRITICAL (W1-06): the raw signed token must NEVER appear in any
	// response body. Assert against the literal token bytes AND the
	// raw_token / rawToken field names.
	if strings.Contains(body, rawToken) {
		t.Errorf("GET response leaks the raw license token: %s", body)
	}
	if strings.Contains(body, "raw_token") || strings.Contains(body, "rawToken") {
		t.Errorf("GET response carries a raw token field: %s", body)
	}

	// W1-08: installer PII (email / sub) must not be exposed on the
	// any-authed-user GET.
	if strings.Contains(body, "installedBy") {
		t.Errorf("GET response leaks installer identity (W1-08): %s", body)
	}
	if strings.Contains(body, "admin@secret.example") {
		t.Errorf("GET response leaks installer email (W1-08): %s", body)
	}
}

func TestLicense_Handler_InstallResponseNeverLeaksRawToken(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	h := newLicenseTestHandler(t, []ed25519.PublicKey{pub})
	admin := &auth.Claims{Subject: "a", Email: "a@x.com", Roles: []string{string(auth.RoleAdmin)}}
	r := licenseRouter(h, admin)

	// Perpetual license (no expiry).
	rawToken := signLicenseToken(t, priv, &license.Claims{
		Plan:     "constellation",
		Customer: "Globex",
		IssuedAt: time.Now().Unix(),
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/license", map[string]any{"token": rawToken}))
	if w.Code != http.StatusCreated {
		t.Fatalf("install: %d %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, rawToken) {
		t.Errorf("install 201 response leaks the raw token: %s", body)
	}
	if strings.Contains(body, "raw_token") || strings.Contains(body, "rawToken") {
		t.Errorf("install 201 response carries a raw token field: %s", body)
	}
}

func TestLicense_Handler_InstallErrorMapping(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("empty token -> 400", func(t *testing.T) {
		h := newLicenseTestHandler(t, []ed25519.PublicKey{pub})
		admin := &auth.Claims{Subject: "a", Email: "a@x.com", Roles: []string{string(auth.RoleAdmin)}}
		r := licenseRouter(h, admin)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, newRequest(http.MethodPost, "/license", map[string]any{"token": "   "}))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("empty token: %d %s", w.Code, w.Body.String())
		}
	})

	t.Run("invalid token -> 400", func(t *testing.T) {
		h := newLicenseTestHandler(t, []ed25519.PublicKey{pub})
		admin := &auth.Claims{Subject: "a", Email: "a@x.com", Roles: []string{string(auth.RoleAdmin)}}
		r := licenseRouter(h, admin)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, newRequest(http.MethodPost, "/license", map[string]any{"token": "not-a-valid-token"}))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid token: %d %s", w.Code, w.Body.String())
		}
	})

	t.Run("already-expired token -> 400", func(t *testing.T) {
		h := newLicenseTestHandler(t, []ed25519.PublicKey{pub})
		admin := &auth.Claims{Subject: "a", Email: "a@x.com", Roles: []string{string(auth.RoleAdmin)}}
		r := licenseRouter(h, admin)
		expired := signLicenseToken(t, priv, &license.Claims{
			Plan:      "crew",
			Customer:  "Acme",
			IssuedAt:  time.Now().Add(-48 * time.Hour).Unix(),
			ExpiresAt: time.Now().Add(-time.Hour).Unix(),
		})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, newRequest(http.MethodPost, "/license", map[string]any{"token": expired}))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expired token: %d %s", w.Code, w.Body.String())
		}
	})

	t.Run("verification disabled (no pubkey) -> 400", func(t *testing.T) {
		h := newLicenseTestHandler(t, nil)
		admin := &auth.Claims{Subject: "a", Email: "a@x.com", Roles: []string{string(auth.RoleAdmin)}}
		r := licenseRouter(h, admin)
		anyToken := signLicenseToken(t, priv, &license.Claims{Plan: "crew", Customer: "Acme", IssuedAt: time.Now().Unix()})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, newRequest(http.MethodPost, "/license", map[string]any{"token": anyToken}))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("verification disabled: %d %s", w.Code, w.Body.String())
		}
	})
}

func TestLicense_Handler_AdminOnlyMutations(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	viewer := &auth.Claims{Subject: "v", Email: "v@x.com", Roles: []string{string(auth.RoleViewer)}}
	token := signLicenseToken(t, priv, &license.Claims{Plan: "crew", Customer: "Acme", IssuedAt: time.Now().Unix()})

	// POST as viewer → 403 (admin gate), never reaching the service.
	h := newLicenseTestHandler(t, []ed25519.PublicKey{pub})
	w := httptest.NewRecorder()
	licenseRouter(h, viewer).ServeHTTP(w, newRequest(http.MethodPost, "/license", map[string]any{"token": token}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("viewer install should be 403; got %d %s", w.Code, w.Body.String())
	}

	// DELETE as viewer → 403.
	w = httptest.NewRecorder()
	licenseRouter(h, viewer).ServeHTTP(w, newRequest(http.MethodDelete, "/license", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("viewer delete should be 403; got %d %s", w.Code, w.Body.String())
	}
}

func TestLicense_Handler_DeleteIsIdempotent(t *testing.T) {
	h := newLicenseTestHandler(t, nil)
	admin := &auth.Claims{Subject: "a", Email: "a@x.com", Roles: []string{string(auth.RoleAdmin)}}
	r := licenseRouter(h, admin)

	// Delete when none installed → 200, status none.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodDelete, "/license", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != licenseStatusNone {
		t.Errorf("status = %v, want %q", resp["status"], licenseStatusNone)
	}
}
