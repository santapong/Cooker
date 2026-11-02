// Package connection stores administrator-approved Git provider connections.
// Credentials are encrypted separately and never returned by JSON encoding.
package connection

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/source/gittransport"
	"github.com/santapong/cooker/internal/validate"
)

var ErrNotFound = errors.New("source connection not found")
var ErrConflict = errors.New("source connection changed")
var ErrCredentialCheck = errors.New("replacement token could not read every allowed repository; saved credentials were not changed")
var ErrCredentialCheckUnavailable = errors.New("source credential checks are unavailable; saved credentials were not changed")

type Connection struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Provider       string `json:"provider"`
	Origin         string `json:"origin"`
	APIOrigin      string `json:"apiOrigin"`
	InstallationID int64  `json:"installationId,omitempty"`
	AuthMode       string `json:"authMode"`
	Username       string `json:"username,omitempty"`
	// WebhookAuthMode is explicit for GitLab: signing or legacy_token.
	WebhookAuthMode string `json:"webhookAuthMode,omitempty"`
	// These policies may only be set through administrator-authorized operations.
	AllowPrivateNetwork bool   `json:"allowPrivateNetwork,omitempty"`
	CAPEM               string `json:"caPEM,omitempty"`
	HasToken            bool   `json:"hasToken"`
	// Repositories is an explicit administrator allowlist. Empty denies all.
	Repositories     []string  `json:"repositories"`
	Enabled          bool      `json:"enabled"`
	Version          int       `json:"version"`
	HasWebhookSecret bool      `json:"hasWebhookSecret"`
	Credential       []byte    `json:"-"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type Credentials struct {
	WebhookSecret string `json:"webhookSecret"`
	Token         string `json:"token,omitempty"`
}

// CredentialsPatch preserves omitted values; an explicit empty Token clears it.
type CredentialsPatch struct {
	WebhookSecret *string
	Token         *string
}

func (Credentials) String() string { return "[source credentials]" }

type Store interface {
	List(context.Context) ([]*Connection, error)
	Get(context.Context, string) (*Connection, error)
	Create(context.Context, *Connection) error
	Update(context.Context, *Connection) error
}

type Service struct {
	Store Store
	Codec *crypto.Codec
	// TokenProbe inspects the sealed candidate without changing the saved
	// connection. A replacement is refused when this capability is absent.
	TokenProbe func(context.Context, *Connection) error
}

func (c *Connection) Allows(repository string) bool {
	if !c.Enabled {
		return false
	}
	for _, r := range c.Repositories {
		if strings.EqualFold(r, repository) {
			return true
		}
	}
	return false
}

func Validate(c *Connection) error {
	if c == nil {
		return fmt.Errorf("source connection is required")
	}
	if err := validate.Name("connection name", c.Name); err != nil {
		return err
	}
	switch c.Provider {
	case "github":
		if c.Origin == "" {
			c.Origin = "https://github.com"
		}
		if c.APIOrigin == "" {
			c.APIOrigin = "https://api.github.com"
		}
		if c.Origin != "https://github.com" || c.APIOrigin != "https://api.github.com" || c.AllowPrivateNetwork || c.CAPEM != "" {
			return fmt.Errorf("this GitHub adapter supports the public github.com origins and trust roots only")
		}
		if c.WebhookAuthMode == "" {
			c.WebhookAuthMode = "signing"
		}
		if c.WebhookAuthMode != "signing" {
			return fmt.Errorf("GitHub requires signed webhooks")
		}
	case "gitlab", "gitea":
		if c.Origin == "" {
			return fmt.Errorf("self-hosted source requires an administrator-approved origin")
		}
		if c.APIOrigin == "" {
			c.APIOrigin = c.Origin
		}
		if c.APIOrigin != c.Origin {
			return fmt.Errorf("provider API and Git origins must match")
		}
		if c.InstallationID != 0 {
			return fmt.Errorf("GitHub installation is not valid for this provider")
		}
		if c.Provider == "gitlab" {
			if c.WebhookAuthMode != "signing" && c.WebhookAuthMode != "legacy_token" {
				return fmt.Errorf("GitLab webhook authentication must explicitly select signing or legacy_token")
			}
		} else {
			if c.WebhookAuthMode == "" {
				c.WebhookAuthMode = "signing"
			}
			if c.WebhookAuthMode != "signing" {
				return fmt.Errorf("gitea requires signed webhooks")
			}
		}
	default:
		return fmt.Errorf("source adapter is not available for provider %q", c.Provider)
	}
	if c.InstallationID < 0 {
		return fmt.Errorf("invalid GitHub installation")
	}
	if c.AuthMode == "" {
		c.AuthMode = "anonymous"
		if c.InstallationID > 0 {
			c.AuthMode = "github_app"
		}
	}
	switch c.AuthMode {
	case "anonymous":
		if c.Username != "" || c.InstallationID != 0 {
			return fmt.Errorf("anonymous source cannot carry an identity")
		}
	case "token":
		if c.InstallationID != 0 {
			return fmt.Errorf("token source cannot carry a GitHub installation")
		}
		if c.Username == "" {
			if c.Provider == "gitlab" {
				c.Username = "oauth2"
			}
			if c.Provider == "github" {
				c.Username = "x-access-token"
			}
		}
		if c.Username == "" {
			return fmt.Errorf("gitea token requires its account username")
		}
		if err := gittransport.ValidateCredentials(gittransport.Credentials{Username: c.Username, Token: "validation"}); err != nil {
			return err
		}
	case "github_app":
		if c.Provider != "github" || c.InstallationID <= 0 || c.Username != "" {
			return fmt.Errorf("GitHub App source requires an installation and no token username")
		}
	default:
		return fmt.Errorf("unsupported source authentication mode")
	}
	if err := c.TransportConfig().Validate(); err != nil {
		return err
	}
	if len(c.Repositories) == 0 || len(c.Repositories) > 100 {
		return fmt.Errorf("allowlist must contain 1–100 repositories")
	}
	seen := map[string]bool{}
	for _, repo := range c.Repositories {
		if err := gittransport.ValidateRepositoryPath(repo, c.Provider == "gitlab"); err != nil {
			return err
		}
		key := strings.ToLower(repo)
		if seen[key] {
			return fmt.Errorf("duplicate repository in allowlist")
		}
		seen[key] = true
	}
	return nil
}
func (c *Connection) TransportConfig() gittransport.Config {
	return gittransport.Config{Origin: c.Origin, APIOrigin: c.APIOrigin, AllowPrivateNetwork: c.AllowPrivateNetwork, CAPEM: c.CAPEM}
}
func validateCredentials(c *Connection, value Credentials) error {
	if len(value.WebhookSecret) < 32 || len(value.WebhookSecret) > 512 {
		return fmt.Errorf("webhook secret must contain 32–512 characters")
	}
	if c.Provider == "gitlab" && c.WebhookAuthMode == "signing" {
		key, err := base64.StdEncoding.Strict().DecodeString(strings.TrimPrefix(value.WebhookSecret, "whsec_"))
		if !strings.HasPrefix(value.WebhookSecret, "whsec_") || strings.ContainsAny(value.WebhookSecret, "\r\n") || err != nil || len(key) != 32 {
			return fmt.Errorf("GitLab signing token must be whsec_ followed by base64 encoding of a 32-byte key")
		}
	}
	if c.AuthMode == "token" {
		if value.Token == "" {
			return fmt.Errorf("token authentication requires a source token")
		}
		return gittransport.ValidateCredentials(gittransport.Credentials{Username: c.Username, Token: value.Token})
	}
	if value.Token != "" {
		return fmt.Errorf("source token requires token authentication mode")
	}
	return nil
}
func (s *Service) seal(value Credentials) ([]byte, error) {
	if s.Codec == nil || !s.Codec.Active() {
		return nil, fmt.Errorf("credential encryption is not configured")
	}
	data, _ := json.Marshal(value)
	defer clear(data)
	cipher, err := s.Codec.Seal(data)
	if err != nil {
		return nil, fmt.Errorf("cannot encrypt source credential")
	}
	return cipher, nil
}
func (s *Service) Create(ctx context.Context, c *Connection, secret string) error {
	return s.CreateWithCredentials(ctx, c, Credentials{WebhookSecret: secret})
}
func (s *Service) CreateWithCredentials(ctx context.Context, c *Connection, value Credentials) error {
	if err := Validate(c); err != nil {
		return err
	}
	if err := validateCredentials(c, value); err != nil {
		return err
	}
	sealed, err := s.seal(value)
	if err != nil {
		return err
	}
	c.ID, c.Version, c.Credential, c.HasWebhookSecret, c.HasToken = uuid.NewString(), 1, sealed, true, value.Token != ""
	c.CreatedAt = time.Now().UTC()
	c.UpdatedAt = c.CreatedAt
	return s.Store.Create(ctx, c)
}

// Update preserves every credential except the explicitly rotated webhook secret.
func (s *Service) Update(ctx context.Context, c *Connection, secret *string) error {
	return s.UpdateWithCredentials(ctx, c, CredentialsPatch{WebhookSecret: secret})
}
func (s *Service) UpdateWithCredentials(ctx context.Context, c *Connection, patch CredentialsPatch) error {
	if err := Validate(c); err != nil {
		return err
	}
	previous, err := s.Store.Get(ctx, c.ID)
	if err != nil {
		return err
	}
	if previous.Version != c.Version {
		return ErrConflict
	}
	// A new connection is required for another host/provider, so an existing
	// encrypted token can never silently acquire a different destination.
	if c.Provider != previous.Provider || c.Origin != previous.Origin || c.APIOrigin != previous.APIOrigin {
		return fmt.Errorf("source origin and provider are immutable; create a new connection")
	}
	value, err := s.OpenCredentials(previous)
	if err != nil {
		return err
	}
	if c.Username != previous.Username && value.Token != "" && patch.Token == nil {
		return fmt.Errorf("changing the token identity requires explicit token rotation")
	}
	if patch.WebhookSecret != nil {
		value.WebhookSecret = *patch.WebhookSecret
	}
	if patch.Token != nil {
		value.Token = *patch.Token
	}
	if err := validateCredentials(c, value); err != nil {
		return err
	}
	c.Credential, err = s.seal(value)
	if err != nil {
		return err
	}
	if patch.Token != nil && value.Token != "" {
		if s.TokenProbe == nil {
			return ErrCredentialCheckUnavailable
		}
		probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		probeErr := s.TokenProbe(probeCtx, copyConnection(c))
		probeFailed := probeErr != nil || probeCtx.Err() != nil
		cancel()
		if probeFailed || ctx.Err() != nil {
			return ErrCredentialCheck
		}
	}
	c.HasWebhookSecret, c.HasToken, c.CreatedAt = true, value.Token != "", previous.CreatedAt
	c.UpdatedAt = time.Now().UTC()
	return s.Store.Update(ctx, c)
}
func (s *Service) OpenCredentials(c *Connection) (Credentials, error) {
	var value Credentials
	if c == nil || s.Codec == nil || !s.Codec.Active() {
		return value, fmt.Errorf("credential encryption is unavailable")
	}
	data, err := s.Codec.Open(c.Credential)
	if err != nil {
		return value, fmt.Errorf("source credential is unavailable")
	}
	defer clear(data)
	if err := json.Unmarshal(data, &value); err != nil || value.WebhookSecret == "" {
		return Credentials{}, fmt.Errorf("source credential is invalid")
	}
	return value, nil
}
func (s *Service) WebhookSecret(c *Connection) ([]byte, error) {
	value, err := s.OpenCredentials(c)
	if err != nil {
		return nil, err
	}
	return []byte(value.WebhookSecret), nil
}
