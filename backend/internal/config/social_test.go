package config

import "testing"

func TestSocialSignInConfig(t *testing.T) {
	valid := OIDCConfig{Enabled: true, Broker: "keycloak", IssuerURL: "https://id.fixture.invalid/realms/cooker", ClientID: "cooker", SocialProviders: []string{"github", "google"}}
	tests := []struct {
		name      string
		change    func(*OIDCConfig)
		wantError bool
	}{
		{"configured", func(*OIDCConfig) {}, false},
		{"disabled", func(c *OIDCConfig) { c.Enabled = false }, true},
		{"no broker", func(c *OIDCConfig) { c.Broker = "" }, true},
		{"unknown broker", func(c *OIDCConfig) { c.Broker = "other" }, true},
		{"unknown provider", func(c *OIDCConfig) { c.SocialProviders = []string{"https://evil.invalid"} }, true},
		{"duplicate", func(c *OIDCConfig) { c.SocialProviders = []string{"github", "github"} }, true},
		{"missing issuer", func(c *OIDCConfig) { c.IssuerURL = " " }, true},
		{"missing client", func(c *OIDCConfig) { c.ClientID = "" }, true},
		{"ordinary OIDC unchanged", func(c *OIDCConfig) { c.Broker = ""; c.SocialProviders = nil }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := valid
			tt.change(&c)
			cfg := Config{Env: EnvDev, OIDC: c}
			if err := cfg.Validate(); (err != nil) != tt.wantError {
				t.Fatalf("Validate()=%v; want error=%v", err, tt.wantError)
			}
		})
	}
	if err := (&Config{Env: EnvDev}).Validate(); err != nil {
		t.Fatalf("default local configuration changed: %v", err)
	}
}

func TestLoadSocialSignIn(t *testing.T) {
	t.Setenv("COOKER_OIDC_BROKER", "keycloak")
	t.Setenv("COOKER_OIDC_SOCIAL_PROVIDERS", "github, google")
	c := Load().OIDC
	if c.Broker != "keycloak" || len(c.SocialProviders) != 2 || c.SocialProviders[1] != "google" {
		t.Fatalf("public broker settings were not loaded")
	}
}
