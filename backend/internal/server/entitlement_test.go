package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/entitlements"
)

// fakeResolver returns a fixed entitlements value for the gate tests.
type fakeResolver struct{ ent entitlements.Entitlements }

func (f fakeResolver) Entitlements(context.Context) entitlements.Entitlements { return f.ent }

func gateRouter(r entitlementResolver, feature string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	eng := gin.New()
	eng.GET("/gated", entitlementGate(r, feature), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return eng
}

func TestEntitlementGate_AllowsWhenFeaturePresent(t *testing.T) {
	res := fakeResolver{ent: entitlements.Entitlements{
		Plan:     entitlements.PlanCrew,
		Features: map[string]bool{entitlements.FeatureOIDCMFA: true},
	}}
	w := httptest.NewRecorder()
	gateRouter(res, entitlements.FeatureOIDCMFA).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/gated", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 happy path, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestEntitlementGate_Rejects402WhenFeatureMissing(t *testing.T) {
	res := fakeResolver{ent: entitlements.Free()} // Free has no paid features
	w := httptest.NewRecorder()
	gateRouter(res, entitlements.FeatureOIDCMFA).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/gated", nil))
	if w.Code != http.StatusPaymentRequired {
		t.Fatalf("expected 402, got %d", w.Code)
	}
	var body struct {
		Error   string `json:"error"`
		Feature string `json:"feature"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error != "feature_not_in_plan" {
		t.Errorf("error = %q, want feature_not_in_plan", body.Error)
	}
	if body.Feature != entitlements.FeatureOIDCMFA {
		t.Errorf("feature = %q, want %q", body.Feature, entitlements.FeatureOIDCMFA)
	}
}

func TestEntitlementGate_NilResolverFailsOpen(t *testing.T) {
	w := httptest.NewRecorder()
	gateRouter(nil, entitlements.FeatureOIDCMFA).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/gated", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("nil resolver should fail open (200), got %d", w.Code)
	}
}
