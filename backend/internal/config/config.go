package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
)

// Env represents the deployment environment. Affects defaults and
// strictness of startup validation. Values: "dev", "uat", "production".
type Env string

const (
	EnvDev        Env = "dev"
	EnvUAT        Env = "uat"
	EnvProduction Env = "production"
)

// IsProduction reports whether the environment is production.
func (e Env) IsProduction() bool { return e == EnvProduction }

// Config holds all application configuration.
type Config struct {
	Env             Env
	Port            int
	DatabaseURL     string
	RedisURL        string
	AllowedOrigins  []string
	SecretKey       string
	Registry        string
	BuilderBackend  string // "noop" | "docker" | "buildkit" | "kaniko"
	PusherBackend   string // "noop" | "docker" | "crane"
	DeployerBackend string // "noop" | "kubectl" | "clientgo"
	// SecretsBackend selects how environment secrets are stored.
	// "database" (default) keeps the historical AES-GCM + JSONB path;
	// "keepsave" delegates to a KeepSave server.
	SecretsBackend string // "database" | "keepsave"
	RateLimit      RateLimitConfig
	OIDC           OIDCConfig
	Docker         DockerConfig
	Kubernetes     KubernetesConfig
	KeepSave       KeepSaveConfig
	Audit          AuditConfig
}

// RateLimitConfig tunes per-user rate limiting on expensive endpoints.
type RateLimitConfig struct {
	Enabled   bool
	PerMinute int
	Burst     int
}

// OIDCConfig holds SSO/OIDC authentication configuration.
type OIDCConfig struct {
	Enabled      bool
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
	// GroupRoleMap maps OIDC group names to Cooker role strings
	// ("admin"|"operator"|"approver"|"viewer"). Empty falls back to the
	// auth.DefaultGroupRoleMap. Loaded from COOKER_OIDC_GROUP_MAP as a
	// CSV of "group:role" pairs, e.g.
	//   "platform-admins:admin,platform-eng:operator,security-team:approver"
	GroupRoleMap map[string]string
	// MFAACRValues lists ACR values that satisfy the step-up MFA gate
	// applied to destructive admin routes. A token's `acr` claim must
	// be one of these (or its `amr` must contain a matching method) to
	// pass auth.RequireMFA. Empty disables the gate. Loaded from
	// COOKER_OIDC_MFA_ACR_VALUES (CSV).
	MFAACRValues []string
}

// DockerConfig holds Docker Engine connection settings.
type DockerConfig struct {
	Host      string
	TLSVerify bool
	CertPath  string
}

// KubernetesConfig holds Kubernetes connection settings.
type KubernetesConfig struct {
	InCluster  bool
	Kubeconfig string
	// Namespace is the namespace Cooker creates Kaniko build Jobs in.
	// Cooker's ServiceAccount needs Job + Pod RBAC here. Default: "cooker".
	Namespace string
	// KanikoImage pins the Kaniko executor image. Default: latest.
	KanikoImage string
	// KanikoServiceAccount runs the Kaniko Job's pod. Empty uses the
	// namespace's default ServiceAccount.
	KanikoServiceAccount string
	// KanikoContextPVC is the PersistentVolumeClaim mounted at the
	// build-context path on both Cooker and the Kaniko Job. Operators
	// stage source there before invoking the builder. Empty is
	// development-only (emptyDir fallback won't see Cooker's source).
	KanikoContextPVC string
}

// KeepSaveConfig configures the KeepSave secrets backend. Required
// when SecretsBackend == "keepsave".
type KeepSaveConfig struct {
	URL       string // base URL of the KeepSave server, e.g. http://keepsave:8080
	ProjectID string // single project that owns all of Cooker's secrets
	APIKey    string // X-API-Key value; per-environment scoping is fine
}

// AuditConfig configures the audit-log middleware. When Enabled,
// every authenticated POST/PUT/PATCH/DELETE under /api/v1 produces
// one structured event. Defaults: on in production, off elsewhere.
type AuditConfig struct {
	Enabled     bool
	Destination string // "stdout" | "file"
	FilePath    string // required when Destination == "file"
}

// Load reads configuration from environment variables with sensible defaults.
func Load() *Config {
	env := Env(getEnv("COOKER_ENV", string(EnvDev)))
	originDefault := []string{"http://localhost:5173", "http://localhost:3000"}
	if env.IsProduction() {
		originDefault = nil
	}
	return &Config{
		Env:             env,
		Port:            getEnvInt("COOKER_PORT", 8080),
		DatabaseURL:     getEnv("DATABASE_URL", "postgres://cooker:cooker@localhost:5432/cooker?sslmode=disable"),
		RedisURL:        getEnv("REDIS_URL", "redis://localhost:6379"),
		AllowedOrigins:  getEnvCSV("COOKER_ALLOWED_ORIGINS", originDefault),
		SecretKey:       getEnv("COOKER_SECRET_KEY", ""),
		Registry:        getEnv("COOKER_REGISTRY", "localhost:5000/cooker"),
		BuilderBackend:  getEnv("COOKER_BUILDER", "noop"),
		PusherBackend:   getEnv("COOKER_PUSHER", "noop"),
		DeployerBackend: getEnv("COOKER_DEPLOYER", "noop"),
		SecretsBackend:  getEnv("COOKER_SECRETS_BACKEND", "database"),
		RateLimit: RateLimitConfig{
			Enabled:   getEnvBool("COOKER_RATE_LIMIT_ENABLED", true),
			PerMinute: getEnvInt("COOKER_RATE_LIMIT_PER_MINUTE", 10),
			Burst:     getEnvInt("COOKER_RATE_LIMIT_BURST", 3),
		},
		OIDC: OIDCConfig{
			Enabled:      getEnvBool("COOKER_OIDC_ENABLED", false),
			IssuerURL:    getEnv("COOKER_OIDC_ISSUER_URL", ""),
			ClientID:     getEnv("COOKER_OIDC_CLIENT_ID", ""),
			ClientSecret: getEnv("COOKER_OIDC_CLIENT_SECRET", ""),
			RedirectURL:  getEnv("COOKER_OIDC_REDIRECT_URL", ""),
			Scopes:       []string{"openid", "profile", "email", "groups"},
			GroupRoleMap: parseGroupRoleMap(getEnv("COOKER_OIDC_GROUP_MAP", "")),
			MFAACRValues: getEnvCSV("COOKER_OIDC_MFA_ACR_VALUES", nil),
		},
		Docker: DockerConfig{
			Host:      getEnv("DOCKER_HOST", "unix:///var/run/docker.sock"),
			TLSVerify: getEnvBool("DOCKER_TLS_VERIFY", false),
			CertPath:  getEnv("DOCKER_CERT_PATH", ""),
		},
		Kubernetes: KubernetesConfig{
			InCluster:            getEnvBool("COOKER_K8S_IN_CLUSTER", false),
			Kubeconfig:           getEnv("KUBECONFIG", ""),
			Namespace:            getEnv("COOKER_K8S_NAMESPACE", "cooker"),
			KanikoImage:          getEnv("COOKER_KANIKO_IMAGE", "gcr.io/kaniko-project/executor:latest"),
			KanikoServiceAccount: getEnv("COOKER_KANIKO_SERVICE_ACCOUNT", ""),
			KanikoContextPVC:     getEnv("COOKER_KANIKO_CONTEXT_PVC", ""),
		},
		KeepSave: KeepSaveConfig{
			URL:       getEnv("COOKER_SECRETS_KEEPSAVE_URL", ""),
			ProjectID: getEnv("COOKER_SECRETS_KEEPSAVE_PROJECT_ID", ""),
			APIKey:    getEnv("COOKER_SECRETS_KEEPSAVE_API_KEY", ""),
		},
		Audit: AuditConfig{
			Enabled:     getEnvBool("COOKER_AUDIT_ENABLED", env.IsProduction()),
			Destination: getEnv("COOKER_AUDIT_DESTINATION", "stdout"),
			FilePath:    getEnv("COOKER_AUDIT_FILE_PATH", ""),
		},
	}
}

// Validate enforces production-mode invariants. Errors are intended
// to be fatal at startup.
func (c *Config) Validate() error {
	if !c.Env.IsProduction() {
		return nil
	}
	var problems []string
	switch c.SecretsBackend {
	case "", "database":
		if c.SecretKey == "" {
			problems = append(problems, "COOKER_SECRET_KEY is required in production with secrets backend=database")
		} else {
			decoded, err := base64.StdEncoding.DecodeString(c.SecretKey)
			switch {
			case err != nil:
				problems = append(problems, "COOKER_SECRET_KEY must be a base64-encoded 32-byte key")
			case len(decoded) < 32:
				problems = append(problems, fmt.Sprintf("COOKER_SECRET_KEY decodes to %d bytes; need at least 32 (AES-256)", len(decoded)))
			}
		}
	case "keepsave":
		if c.KeepSave.URL == "" {
			problems = append(problems, "COOKER_SECRETS_KEEPSAVE_URL is required when SecretsBackend=keepsave")
		}
		if c.KeepSave.ProjectID == "" {
			problems = append(problems, "COOKER_SECRETS_KEEPSAVE_PROJECT_ID is required when SecretsBackend=keepsave")
		}
		if c.KeepSave.APIKey == "" {
			problems = append(problems, "COOKER_SECRETS_KEEPSAVE_API_KEY is required when SecretsBackend=keepsave")
		}
	default:
		problems = append(problems, fmt.Sprintf("unknown COOKER_SECRETS_BACKEND %q (want \"database\" or \"keepsave\")", c.SecretsBackend))
	}
	if len(c.AllowedOrigins) == 0 {
		problems = append(problems, "COOKER_ALLOWED_ORIGINS is required in production (no permissive default)")
	}
	if !c.OIDC.Enabled {
		log.Printf("warning: COOKER_OIDC_ENABLED=false in production; the backend will inject a dev admin user on every request")
	}
	if c.Audit.Enabled {
		switch c.Audit.Destination {
		case "stdout":
			// fine
		case "file":
			if c.Audit.FilePath == "" {
				problems = append(problems, "COOKER_AUDIT_FILE_PATH is required when COOKER_AUDIT_DESTINATION=file")
			}
		default:
			problems = append(problems, fmt.Sprintf("unknown COOKER_AUDIT_DESTINATION %q (want \"stdout\" or \"file\")", c.Audit.Destination))
		}
	}
	if len(problems) > 0 {
		return errors.New("config: " + strings.Join(problems, "; "))
	}
	return nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

// parseGroupRoleMap parses a CSV of "group:role" pairs into a map.
// Empty or malformed input yields nil so callers fall back to defaults.
// Whitespace around tokens is tolerated; pairs missing either side are
// skipped silently — operator typos surface as users defaulting to
// viewer rather than as a startup crash.
func parseGroupRoleMap(raw string) map[string]string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	out := make(map[string]string)
	for _, pair := range strings.Split(raw, ",") {
		parts := strings.SplitN(pair, ":", 2)
		if len(parts) != 2 {
			continue
		}
		group := strings.TrimSpace(parts[0])
		role := strings.TrimSpace(parts[1])
		if group == "" || role == "" {
			continue
		}
		out[group] = role
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func getEnvCSV(key string, fallback []string) []string {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}
