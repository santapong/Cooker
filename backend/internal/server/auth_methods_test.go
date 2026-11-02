package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/config"
)

func TestAuthMethodsSocialCapabilities(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, enabled := range []bool{false, true} {
		cfg := &config.Config{OIDC: config.OIDCConfig{Enabled: enabled, Broker: "keycloak", IssuerURL: "https://id.fixture.invalid/realms/cooker", ClientID: "cooker", ClientSecret: "synthetic-oauth-secret-canary", SocialProviders: []string{"github", "google"}}, LocalAuth: config.LocalAuthConfig{Enabled: true, AllowSignup: true, JWTSigningKey: "synthetic-signing-key-canary"}}
		s := &Server{config: cfg}
		r := gin.New()
		r.GET("/api/v1/auth/methods", s.authMethods)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/auth/methods", nil))
		if w.Code != 200 {
			t.Fatalf("anonymous discovery failed: %d", w.Code)
		}
		for _, secret := range []string{cfg.OIDC.ClientSecret, cfg.LocalAuth.JWTSigningKey, cfg.OIDC.IssuerURL} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("discovery exposed private or unnecessary configuration")
			}
		}
		var response struct {
			OIDC struct {
				Enabled   bool     `json:"enabled"`
				Broker    string   `json:"broker"`
				Providers []string `json:"socialProviders"`
			}
			Local struct {
				Enabled     bool
				AllowSignup bool
			}
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		want := 0
		if enabled {
			want = 2
		}
		if len(response.OIDC.Providers) != want || !response.Local.Enabled || !response.Local.AllowSignup {
			t.Fatalf("incorrect capabilities: %s", w.Body.String())
		}
	}
}
