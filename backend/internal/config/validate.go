package config

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

func (c *Config) Validate() error {
	// AI triage is env-independent: enabling it without a key is a
	// misconfiguration in dev and prod alike — the route would 502 on
	// every click. Fail at boot instead.
	if c.Triage.Enabled && c.Triage.APIKey == "" {
		return fmt.Errorf("config: COOKER_AI_TRIAGE_ENABLED=true requires ANTHROPIC_API_KEY")
	}
	// Self-hosted licensing (M2) is permissive: no license => Free, never
	// an error. The single hard rule, env-independent, is that a license
	// token without a public key cannot be verified — boot would silently
	// degrade to Free and the operator would never know their paid license
	// didn't take. Fail fast so the misconfiguration is obvious.
	if c.License.Key != "" && len(c.License.PublicKeys) == 0 {
		return fmt.Errorf("config: COOKER_LICENSE_KEY is set but no public key is configured (COOKER_LICENSE_PUBLIC_KEY / COOKER_LICENSE_PUBLIC_KEYS); a license cannot be verified without a public key")
	}
	// Metrics port hygiene (W1-03 / W1-04), env-independent: a dedicated
	// metrics listener must not collide with the app port (one of the two
	// would fail to bind, non-deterministically), and a configured port must
	// be a valid TCP port. 0 means "no dedicated listener" and is always
	// allowed. These are misconfigurations in dev and prod alike — fail fast.
	if c.Observability.MetricsPort > 0 {
		if c.Observability.MetricsPort == c.Port {
			return fmt.Errorf("config: COOKER_METRICS_PORT must differ from COOKER_PORT (%d)", c.Port)
		}
		if c.Observability.MetricsPort > 65535 {
			return fmt.Errorf("config: COOKER_METRICS_PORT=%d is out of range (1..65535)", c.Observability.MetricsPort)
		}
	} else if c.Observability.MetricsPort < 0 {
		return fmt.Errorf("config: COOKER_METRICS_PORT=%d is out of range (1..65535)", c.Observability.MetricsPort)
	}
	if strings.Contains(c.Audit.Destination, "db") && c.Env.IsProduction() && c.DatabaseURL == "" {
		return fmt.Errorf("config: COOKER_AUDIT_DESTINATION=db requires DATABASE_URL in production")
	}
	// Proxy hygiene, env-independent: a bad scheme would mint broken
	// DeployedURLs for every app; fail at boot instead.
	if c.Proxy.Scheme != "" && c.Proxy.Scheme != "http" && c.Proxy.Scheme != "https" {
		return fmt.Errorf("config: COOKER_PROXY_SCHEME must be \"http\" or \"https\", got %q", c.Proxy.Scheme)
	}
	if !c.Env.IsProduction() {
		return nil
	}
	var problems []string
	if c.DatabaseURL == "" {
		problems = append(problems, "DATABASE_URL is required in production")
	} else if strings.Contains(c.DatabaseURL, "cooker:cooker@localhost") {
		problems = append(problems, "DATABASE_URL still uses the dev default (cooker:cooker@localhost); set a real value")
	} else if u, err := url.Parse(c.DatabaseURL); err == nil && u.Host != "" {
		host := u.Hostname()
		if host != "" && host != "localhost" && host != "127.0.0.1" && host != "::1" {
			switch u.Query().Get("sslmode") {
			case "require", "verify-ca", "verify-full":
			default:
				problems = append(problems, "DATABASE_URL requires sslmode=require (or verify-ca/verify-full) in production when host is not localhost")
			}
		}
	}
	requireSecretKey := c.SecretsBackend != "keepsave"
	if requireSecretKey {
		if c.SecretKey == "" {
			problems = append(problems, "COOKER_SECRET_KEY is required in production")
		} else {
			decoded, err := base64.StdEncoding.DecodeString(c.SecretKey)
			switch {
			case err != nil:
				problems = append(problems, "COOKER_SECRET_KEY must be a base64-encoded 32-byte key")
			case len(decoded) < 32:
				problems = append(problems, fmt.Sprintf("COOKER_SECRET_KEY decodes to %d bytes; need at least 32 (AES-256)", len(decoded)))
			}
		}
	}
	switch c.SecretsBackend {
	case "", "database":
	case "keepsave":
		if c.KeepSave.URL == "" {
			problems = append(problems, "COOKER_SECRETS_KEEPSAVE_URL is required when SecretsBackend=keepsave")
		} else if !strings.HasPrefix(c.KeepSave.URL, "https://") {
			problems = append(problems, "COOKER_SECRETS_KEEPSAVE_URL must use https:// in production")
		}
		if c.KeepSave.ProjectID == "" {
			problems = append(problems, "COOKER_SECRETS_KEEPSAVE_PROJECT_ID is required when SecretsBackend=keepsave")
		}
		if c.KeepSave.APIKey == "" {
			problems = append(problems, "COOKER_SECRETS_KEEPSAVE_API_KEY is required when SecretsBackend=keepsave")
		}
	case "vault":
		if c.Vault.Addr == "" {
			problems = append(problems, "COOKER_SECRETS_VAULT_ADDR is required when SecretsBackend=vault")
		}
	case "aws":
	case "gcp":
		if c.GCPSecrets.ProjectID == "" {
			problems = append(problems, "COOKER_SECRETS_GCP_PROJECT_ID is required when SecretsBackend=gcp")
		}
	default:
		problems = append(problems, fmt.Sprintf("unknown COOKER_SECRETS_BACKEND %q (want database|keepsave|vault|aws|gcp)", c.SecretsBackend))
	}
	if len(c.AllowedOrigins) == 0 {
		problems = append(problems, "COOKER_ALLOWED_ORIGINS is required in production (no permissive default)")
	} else if len(c.AllowedOrigins) == 1 && c.AllowedOrigins[0] == "*" {
		problems = append(problems, "COOKER_ALLOWED_ORIGINS=* is rejected in production; specify exact origins")
	}
	if !c.OIDC.Enabled && !c.LocalAuth.Enabled {
		// Fail closed: with both auth paths disabled the OIDC middleware
		// falls back to devHandler(), which injects a dev admin user with
		// RoleAdmin on every request — i.e. an unauthenticated admin API.
		// This must never boot in production (CWE-1188).
		problems = append(problems, "no authentication is enabled in production: set COOKER_OIDC_ENABLED=true or COOKER_LOCAL_AUTH_ENABLED=true (both disabled injects a dev admin user on every request)")
	}
	if c.LocalAuth.Enabled {
		decoded, err := DecodeLocalAuthSigningKey(c.LocalAuth.JWTSigningKey)
		switch {
		case err != nil:
			problems = append(problems, "COOKER_LOCAL_AUTH_JWT_SIGNING_KEY: "+err.Error())
		case len(decoded) < 32:
			problems = append(problems, fmt.Sprintf("COOKER_LOCAL_AUTH_JWT_SIGNING_KEY decodes to %d bytes; need at least 32", len(decoded)))
		}
	}
	if c.Audit.Enabled {
		switch c.Audit.Destination {
		case "stdout":
		case "file":
			if c.Audit.FilePath == "" {
				problems = append(problems, "COOKER_AUDIT_FILE_PATH is required when COOKER_AUDIT_DESTINATION=file")
			}
		default:
			problems = append(problems, fmt.Sprintf("unknown COOKER_AUDIT_DESTINATION %q (want \"stdout\" or \"file\")", c.Audit.Destination))
		}
	}
	if c.BuilderBackend == "docker" {
		problems = append(problems, "COOKER_BUILDER=docker is unsafe in production (host docker.sock RCE-to-host); use kaniko, buildah, or buildkit")
	}
	if c.PusherBackend == "docker" {
		problems = append(problems, "COOKER_PUSHER=docker is forbidden in production (docker.sock RCE-to-host risk); use crane")
	}
	if c.Governance.URL != "" && c.Governance.CallerToken == "" {
		problems = append(problems, "COOKER_GOVERNANCE_CALLER_TOKEN is required in production when COOKER_GOVERNANCE_URL is set (Grovernance v1.1+ rejects unauthenticated callers)")
	}
	if c.ReplicaCount > 1 && !c.StickySessions {
		var perProcess []string
		if c.RateLimit.Enabled && c.RateLimit.Backend != "redis" {
			perProcess = append(perProcess, "COOKER_RATE_LIMIT_BACKEND")
		}
		if c.WSTicket.Backend != "redis" {
			perProcess = append(perProcess, "COOKER_WS_TICKET_BACKEND")
		}
		if c.WSHub.Backend != "redis" {
			perProcess = append(perProcess, "COOKER_WS_HUB_BACKEND")
		}
		if len(perProcess) > 0 {
			problems = append(problems, fmt.Sprintf(
				"COOKER_REPLICA_COUNT=%d requires COOKER_STICKY_SESSIONS=true or redis backend for: %s",
				c.ReplicaCount, strings.Join(perProcess, ", ")))
		}
	}
	// Scheduler safety: depends on jobqueue.
	if c.Scheduler.Enabled && !c.JobQueue.Enabled {
		problems = append(problems, "COOKER_SCHEDULER_ENABLED=true requires COOKER_JOBQUEUE_ENABLED=true")
	}
	// Cloud inventory (OR-2): an enabled provider must have its required
	// locator (AWS region / GCP project). Credentials are NOT forced here
	// — both providers fall back to the platform credential chain (IRSA /
	// Workload Identity / ADC), exactly like the awsm/gcpsm backends, so a
	// pod using workload identity needs no key env var. A region/project,
	// however, is not discoverable and must be set.
	if c.CloudInventory.AWS.Enabled && c.CloudInventory.AWS.Region == "" {
		problems = append(problems, "COOKER_CLOUD_AWS_REGION is required when COOKER_CLOUD_AWS_ENABLED=true")
	}
	if c.CloudInventory.GCP.Enabled && c.CloudInventory.GCP.ProjectID == "" {
		problems = append(problems, "COOKER_CLOUD_GCP_PROJECT_ID is required when COOKER_CLOUD_GCP_ENABLED=true")
	}
	if len(problems) > 0 {
		return errors.New("config: " + strings.Join(problems, "; "))
	}
	return nil
}

// SSHHostLister is the minimal store surface ValidateSSHHosts needs.
// Implemented by store.Store via st.Hosts.List, but kept narrow here
// to avoid importing the store package (and creating an import cycle).
type SSHHostLister interface {
	ListSSHHostsLaxStrictHostKey(ctx context.Context) ([]SSHHostSummary, error)
}

// SSHHostSummary is the minimal Host info config validation needs
// to identify a non-compliant row in an error message.
type SSHHostSummary struct {
	ID   string
	Name string
}

// ValidateSSHHosts enforces the production-mode invariant that no
// registered SSH host has SSHStrictHostKey=false. Called by
// server.New AFTER the store is built but BEFORE serving traffic.
// In non-production environments it is a no-op.
//
// This check exists at the post-store layer (not in Validate()
// itself) because Validate() runs before the database connection is
// open. The two-stage boot sequence — first Validate() the env, then
// open the store, then ValidateSSHHosts() — keeps Validate() pure
// while still failing the boot on lax-TOFU production hosts.
func (c *Config) ValidateSSHHosts(ctx context.Context, lister SSHHostLister) error {
	if !c.Env.IsProduction() {
		return nil
	}
	if lister == nil {
		return nil
	}
	lax, err := lister.ListSSHHostsLaxStrictHostKey(ctx)
	if err != nil {
		return fmt.Errorf("config: list ssh hosts: %w", err)
	}
	if len(lax) == 0 {
		return nil
	}
	names := make([]string, 0, len(lax))
	for _, h := range lax {
		names = append(names, fmt.Sprintf("%s (%s)", h.Name, h.ID))
	}
	return errors.New("config: SSH hosts with sshStrictHostKey=false are forbidden in production: " +
		strings.Join(names, ", "))
}
