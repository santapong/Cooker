package connection

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/crypto"
)

func TestConnectionEncryptsAndRedactsSecretAndPreventsLostUpdate(t *testing.T) {
	ctx := context.Background()
	codec, err := crypto.NewCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{Store: NewMemory(), Codec: codec}
	c := &Connection{Name: "Example GitHub", Provider: "github", Repositories: []string{"acme/shop"}, Enabled: true}
	secret := strings.Repeat("fixture-secret", 4)
	if err := s.Create(ctx, c, secret); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(c.Credential), secret) {
		t.Fatal("plaintext credential persisted")
	}
	wire, _ := json.Marshal(c)
	if strings.Contains(string(wire), secret) || strings.Contains(string(wire), "credential") {
		t.Fatal("credential escaped into JSON")
	}
	opened, err := s.WebhookSecret(c)
	if err != nil || string(opened) != secret {
		t.Fatal("credential round trip failed")
	}
	stale, _ := s.Store.Get(ctx, c.ID)
	c.Enabled = false
	if err := s.Update(ctx, c, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Update(ctx, stale, nil); !errors.Is(err, ErrConflict) {
		t.Fatal("stale writer overwrote connection", err)
	}
	if c.Allows("acme/shop") {
		t.Fatal("disabled connection allowed source")
	}
}

func TestConnectionOriginAndProviderBoundaries(t *testing.T) {
	for _, origin := range []string{"https://github.com.evil.example", "http://github.com", "https://token@github.com", "https://github.com/path", "https://127.0.0.1"} {
		c := &Connection{Name: "source", Provider: "github", Origin: origin, Repositories: []string{"acme/shop"}}
		if Validate(c) == nil {
			t.Fatalf("accepted unsupported origin %s", origin)
		}
	}
	c := &Connection{Name: "source", Provider: "bitbucket", Repositories: []string{"acme/shop"}}
	if Validate(c) == nil {
		t.Fatal("unsupported provider advertised checkout")
	}
}

func TestTokenRotationPreservesWebhookAndLegacyUpdatePreservesToken(t *testing.T) {
	ctx := context.Background()
	codec, _ := crypto.NewCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	s := &Service{Store: NewMemory(), Codec: codec}
	c := &Connection{Name: "GitLab fixture", Provider: "gitlab", Origin: "https://gitlab.example.com", AuthMode: "token", WebhookAuthMode: "signing", Repositories: []string{"team/subteam/repo"}, Enabled: true}
	hook := "whsec_" + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("h", 32)))
	token := "fixture-token"
	if err := s.CreateWithCredentials(ctx, c, Credentials{WebhookSecret: hook, Token: token}); err != nil {
		t.Fatal(err)
	}
	if !c.HasToken || c.Username != "oauth2" {
		t.Fatal("token capabilities not set")
	}
	wire, _ := json.Marshal(c)
	if strings.Contains(string(wire), token) || strings.Contains(string(c.Credential), token) {
		t.Fatal("plaintext token escaped")
	}
	rotated := "whsec_" + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("r", 32)))
	if err := s.Update(ctx, c, &rotated); err != nil {
		t.Fatal(err)
	}
	value, err := s.OpenCredentials(c)
	if err != nil || value.Token != token || value.WebhookSecret != rotated {
		t.Fatal("legacy webhook rotation lost token", err)
	}
	next := "next-fixture-token"
	s.TokenProbe = func(_ context.Context, candidate *Connection) error {
		probe, err := s.OpenCredentials(candidate)
		if err != nil || probe.Token != next || probe.WebhookSecret != rotated {
			t.Fatal("replacement probe did not receive its candidate credentials")
		}
		return nil
	}
	if err := s.UpdateWithCredentials(ctx, c, CredentialsPatch{Token: &next}); err != nil {
		t.Fatal(err)
	}
	value, _ = s.OpenCredentials(c)
	if value.Token != next || value.WebhookSecret != rotated {
		t.Fatal("token rotation lost webhook")
	}
	c.Origin = "https://evil.example"
	c.APIOrigin = c.Origin
	if err := s.Update(ctx, c, nil); err == nil {
		t.Fatal("credential moved to another host")
	}
}
func TestConnectionExplicitPoliciesAndCredentialMode(t *testing.T) {
	c := &Connection{Name: "Gitea fixture", Provider: "gitea", Origin: "https://127.0.0.1:9443", Repositories: []string{"team/repo"}}
	if Validate(c) == nil {
		t.Fatal("private origin accepted without explicit policy")
	}
	c.AllowPrivateNetwork = true
	if err := Validate(c); err != nil {
		t.Fatal(err)
	}
	c.Origin = "https://169.254.169.254"
	c.APIOrigin = c.Origin
	if Validate(c) == nil {
		t.Fatal("metadata host accepted with private policy")
	}
	c = &Connection{Name: "GitLab fixture", Provider: "gitlab", Origin: "https://gitlab.com", Repositories: []string{"a/b/c"}}
	if Validate(c) == nil {
		t.Fatal("GitLab webhook mode silently defaulted")
	}
	c.WebhookAuthMode = "legacy_token"
	if err := Validate(c); err != nil {
		t.Fatal(err)
	}
	c.AuthMode = "token"
	if err := Validate(c); err != nil {
		t.Fatal(err)
	}
	if validateCredentials(c, Credentials{WebhookSecret: strings.Repeat("x", 32)}) == nil {
		t.Fatal("empty token permitted")
	}
	c.AuthMode = "anonymous"
	c.Username = ""
	if validateCredentials(c, Credentials{WebhookSecret: strings.Repeat("x", 32), Token: "fixture"}) == nil {
		t.Fatal("anonymous accepted token")
	}
}

func TestGitLabSigningTokenFormatIsValidatedBeforePersistence(t *testing.T) {
	c := &Connection{Provider: "gitlab", WebhookAuthMode: "signing", AuthMode: "anonymous"}
	for _, secret := range []string{strings.Repeat("x", 40), "whsec_" + base64.StdEncoding.EncodeToString(make([]byte, 16)), "whsec_" + base64.StdEncoding.EncodeToString(make([]byte, 64))} {
		if validateCredentials(c, Credentials{WebhookSecret: secret}) == nil {
			t.Fatal("invalid signing token accepted")
		}
	}
	if err := validateCredentials(c, Credentials{WebhookSecret: "whsec_" + base64.StdEncoding.EncodeToString(make([]byte, 32))}); err != nil {
		t.Fatal(err)
	}
}
