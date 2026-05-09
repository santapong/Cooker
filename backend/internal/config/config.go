package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
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
	// ReplicaCount is the number of Cooker replicas the chart spins
	// up (set as COOKER_REPLICA_COUNT). Used by Validate to refuse
	// boot when shared state is per-process and sticky sessions are
	// off. Default 1.
	ReplicaCount int
	// StickySessions signals that the operator has configured
	// session-affinity at ingress (NGINX, ALB, Traefik). Lets
	// Validate accept memory-backed rate limiter / WS ticket store
	// at >1 replicas. Default false.
	StickySessions bool
	RateLimit      RateLimitConfig
	WSTicket       WSTicketConfig
	WSHub          WSHubConfig
	OIDC           OIDCConfig
	LocalAuth      LocalAuthConfig
	Docker         DockerConfig
	Kubernetes     KubernetesConfig
	KeepSave       KeepSaveConfig
	Vault          VaultConfig
	AWSSecrets     AWSSecretsConfig
	GCPSecrets     GCPSecretsConfig
	DeployTargets  DeployTargetsConfig
	Audit          AuditConfig
	Observability  ObservabilityConfig
	// AppHealthInterval is how often the AppHealthChecker probes each
	// App for readiness. Default 30s. Operators with large fleets may
	// raise this to reduce backend pressure; setting 0 disables the
	// checker entirely.
	AppHealthInterval time.Duration
}

// WSHubConfig configures the WebSocket broadcast fan-out backend.
// "memory" (default) keeps the existing in-process channel hub;
// "redis" uses pub/sub on the shared Redis client so any replica can
// deliver broadcasts to its connected clients.
type WSHubConfig struct {
	Backend string // "memory" | "redis"
}

// ObservabilityConfig configures Prometheus /metrics and OpenTelemetry
// tracing. Both are opt-in (off by default) and add no runtime cost
// when disabled.
type ObservabilityConfig struct {
	MetricsEnabled bool
	TracingEnabled bool
	OTLPEndpoint   string // host:port for OTLP/gRPC trace exporter
	OTLPInsecure   bool
	ServiceName    string
	ServiceVersion string
}

// RateLimitConfig tunes per-user rate limiting on expensive endpoints.
type RateLimitConfig struct {
	Enabled   bool
	PerMinute int
	Burst     int
	// Backend selects the storage layer. "memory" (default) is
	// per-process and per-replica; "redis" backs onto the URL in
	// RedisURL via go-redis/redis_rate (multi-replica safe).
	Backend string
}

// WSTicketConfig configures the WebSocket single-use ticket store.
// "memory" (default) is per-process; "redis" shares state across
// cooker replicas via Redis GETDEL.
type WSTicketConfig struct {
	Backend string // "memory" | "redis"
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

// LocalAuthConfig configures the email + password authentication path
// that runs alongside OIDC. When Enabled is true the server registers
// /api/v1/auth/local/{signup,signin,me} endpoints and the auth
// middleware accepts JWTs signed with JWTSigningKey. The first user
// to sign up is granted admin (bootstrap pattern); subsequent signups
// default to viewer and must be promoted by an admin via the API.
type LocalAuthConfig struct {
	Enabled       bool
	JWTSigningKey string        // base64 or raw; >=32 bytes after decode
	TokenTTL      time.Duration // default 12h
	// AllowSignup gates the /signup endpoint. Operators who only want
	// to invite specific users via admin-created accounts should set
	// this to false; the UI then hides the sign-up form.
	AllowSignup bool
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

	// Buildah builder knobs (see builder.BuildahConfig). Active when
	// COOKER_BUILDER=buildah.
	BuildahImage          string
	BuildahServiceAccount string
	BuildahContextPVC     string
	BuildahStorageDriver  string // "overlay" | "vfs"; default "vfs"
}

// KeepSaveConfig configures the KeepSave secrets backend. Required
// when SecretsBackend == "keepsave".
type KeepSaveConfig struct {
	URL       string // base URL of the KeepSave server, e.g. http://keepsave:8080
	ProjectID string // single project that owns all of Cooker's secrets
	APIKey    string // X-API-Key value; per-environment scoping is fine
}

// VaultConfig configures the HashiCorp Vault KV v2 backend.
type VaultConfig struct {
	Addr   string // VAULT_ADDR equivalent
	Token  string // VAULT_TOKEN equivalent (can be empty when Vault Agent injects it)
	Mount  string // KV v2 mount path; default "secret"
	Prefix string // path prefix appended under <mount>; default ""
}

// AWSSecretsConfig configures the AWS Secrets Manager backend.
type AWSSecretsConfig struct {
	Region string
	Prefix string // default "cooker"
}

// GCPSecretsConfig configures the GCP Secret Manager backend.
type GCPSecretsConfig struct {
	ProjectID string
	Prefix    string // default "cooker"
}

// DeployTargetsConfig bundles the credentials operators provide for
// each cloud deploy target. Empty fields skip registration of that
// target — callers don't have to wire every backend they don't use.
type DeployTargetsConfig struct {
	CloudRunProject string
	CloudRunRegion  string

	ECSRegion         string
	ECSCluster        string
	ECSExecutionRole  string
	ECSTaskRole       string
	ECSSubnets        []string
	ECSSecurityGroups []string

	FlyToken  string
	FlyRegion string

	RenderToken   string
	RenderOwnerID string
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
		ReplicaCount:    getEnvInt("COOKER_REPLICA_COUNT", 1),
		StickySessions:  getEnvBool("COOKER_STICKY_SESSIONS", false),
		RateLimit: RateLimitConfig{
			Enabled:   getEnvBool("COOKER_RATE_LIMIT_ENABLED", true),
			PerMinute: getEnvInt("COOKER_RATE_LIMIT_PER_MINUTE", 10),
			Burst:     getEnvInt("COOKER_RATE_LIMIT_BURST", 3),
			Backend:   getEnv("COOKER_RATE_LIMIT_BACKEND", "memory"),
		},
		WSTicket: WSTicketConfig{
			Backend: getEnv("COOKER_WS_TICKET_BACKEND", "memory"),
		},
		WSHub: WSHubConfig{
			Backend: getEnv("COOKER_WS_HUB_BACKEND", "memory"),
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
		LocalAuth: LocalAuthConfig{
			Enabled:       getEnvBool("COOKER_LOCAL_AUTH_ENABLED", false),
			JWTSigningKey: getEnv("COOKER_LOCAL_AUTH_JWT_SIGNING_KEY", ""),
			TokenTTL:      getEnvDuration("COOKER_LOCAL_AUTH_TOKEN_TTL", 12*time.Hour),
			AllowSignup:   getEnvBool("COOKER_LOCAL_AUTH_ALLOW_SIGNUP", true),
		},
		Docker: DockerConfig{
			Host:      getEnv("DOCKER_HOST", "unix:///var/run/docker.sock"),
			TLSVerify: getEnvBool("DOCKER_TLS_VERIFY", false),
			CertPath:  getEnv("DOCKER_CERT_PATH", ""),
		},
		Kubernetes: KubernetesConfig{
			InCluster:             getEnvBool("COOKER_K8S_IN_CLUSTER", false),
			Kubeconfig:            getEnv("KUBECONFIG", ""),
			Namespace:             getEnv("COOKER_K8S_NAMESPACE", "cooker"),
			KanikoImage:           getEnv("COOKER_KANIKO_IMAGE", "gcr.io/kaniko-project/executor:latest"),
			KanikoServiceAccount:  getEnv("COOKER_KANIKO_SERVICE_ACCOUNT", ""),
			KanikoContextPVC:      getEnv("COOKER_KANIKO_CONTEXT_PVC", ""),
			BuildahImage:          getEnv("COOKER_BUILDAH_IMAGE", "quay.io/buildah/stable:latest"),
			BuildahServiceAccount: getEnv("COOKER_BUILDAH_SERVICE_ACCOUNT", ""),
			BuildahContextPVC:     getEnv("COOKER_BUILDAH_CONTEXT_PVC", ""),
			BuildahStorageDriver:  getEnv("COOKER_BUILDAH_STORAGE_DRIVER", "vfs"),
		},
		KeepSave: KeepSaveConfig{
			URL:       getEnv("COOKER_SECRETS_KEEPSAVE_URL", ""),
			ProjectID: getEnv("COOKER_SECRETS_KEEPSAVE_PROJECT_ID", ""),
			APIKey:    getEnv("COOKER_SECRETS_KEEPSAVE_API_KEY", ""),
		},
		Vault: VaultConfig{
			Addr:   getEnv("COOKER_SECRETS_VAULT_ADDR", ""),
			Token:  getEnv("COOKER_SECRETS_VAULT_TOKEN", ""),
			Mount:  getEnv("COOKER_SECRETS_VAULT_MOUNT", "secret"),
			Prefix: getEnv("COOKER_SECRETS_VAULT_PREFIX", "cooker"),
		},
		AWSSecrets: AWSSecretsConfig{
			Region: getEnv("COOKER_SECRETS_AWS_REGION", ""),
			Prefix: getEnv("COOKER_SECRETS_AWS_PREFIX", "cooker"),
		},
		GCPSecrets: GCPSecretsConfig{
			ProjectID: getEnv("COOKER_SECRETS_GCP_PROJECT_ID", ""),
			Prefix:    getEnv("COOKER_SECRETS_GCP_PREFIX", "cooker"),
		},
		DeployTargets: DeployTargetsConfig{
			CloudRunProject:   getEnv("COOKER_DEPLOY_CLOUDRUN_PROJECT", ""),
			CloudRunRegion:    getEnv("COOKER_DEPLOY_CLOUDRUN_REGION", ""),
			ECSRegion:         getEnv("COOKER_DEPLOY_ECS_REGION", ""),
			ECSCluster:        getEnv("COOKER_DEPLOY_ECS_CLUSTER", ""),
			ECSExecutionRole:  getEnv("COOKER_DEPLOY_ECS_EXECUTION_ROLE", ""),
			ECSTaskRole:       getEnv("COOKER_DEPLOY_ECS_TASK_ROLE", ""),
			ECSSubnets:        getEnvCSV("COOKER_DEPLOY_ECS_SUBNETS", nil),
			ECSSecurityGroups: getEnvCSV("COOKER_DEPLOY_ECS_SECURITY_GROUPS", nil),
			FlyToken:          getEnv("COOKER_DEPLOY_FLY_TOKEN", ""),
			FlyRegion:         getEnv("COOKER_DEPLOY_FLY_REGION", ""),
			RenderToken:       getEnv("COOKER_DEPLOY_RENDER_TOKEN", ""),
			RenderOwnerID:     getEnv("COOKER_DEPLOY_RENDER_OWNER_ID", ""),
		},
		Audit: AuditConfig{
			Enabled:     getEnvBool("COOKER_AUDIT_ENABLED", env.IsProduction()),
			Destination: getEnv("COOKER_AUDIT_DESTINATION", "stdout"),
			FilePath:    getEnv("COOKER_AUDIT_FILE_PATH", ""),
		},
		Observability: ObservabilityConfig{
			MetricsEnabled: getEnvBool("COOKER_METRICS_ENABLED", false),
			TracingEnabled: getEnvBool("COOKER_TRACING_ENABLED", false),
			OTLPEndpoint:   getEnv("COOKER_OTLP_ENDPOINT", ""),
			OTLPInsecure:   getEnvBool("COOKER_OTLP_INSECURE", false),
			ServiceName:    getEnv("COOKER_SERVICE_NAME", "cooker"),
			ServiceVersion: getEnv("COOKER_SERVICE_VERSION", "dev"),
		},
		AppHealthInterval: getEnvDuration("COOKER_APP_HEALTH_INTERVAL", 30*time.Second),
	}
}

// Validate enforces production-mode invariants. Errors are intended
// to be fatal at startup.
func (c *Config) Validate() error {
	if !c.Env.IsProduction() {
		return nil
	}
	var problems []string
	// DATABASE_URL must not be empty or the dev default in production.
	// The dev default points at localhost with throwaway credentials;
	// allowing it through silently is a deployment-mistake amplifier.
	if c.DatabaseURL == "" {
		problems = append(problems, "DATABASE_URL is required in production")
	} else if strings.Contains(c.DatabaseURL, "cooker:cooker@localhost") {
		problems = append(problems, "DATABASE_URL still uses the dev default (cooker:cooker@localhost); set a real value")
	}
	// COOKER_SECRET_KEY is required whenever the active secrets backend
	// will encrypt anything via crypto.Codec — that's database (always)
	// and any other backend that fronts environment-secret reveal /
	// app webhook decryption flows. Treat any non-keepsave backend as
	// requiring the key in production.
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
		// Already covered by the requireSecretKey block above.
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
		// Region can be auto-discovered from instance metadata; no
		// hard requirement here.
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
		slog.Warn("OIDC and local auth both disabled in production; backend will inject dev admin user on every request")
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
			// fine
		case "file":
			if c.Audit.FilePath == "" {
				problems = append(problems, "COOKER_AUDIT_FILE_PATH is required when COOKER_AUDIT_DESTINATION=file")
			}
		default:
			problems = append(problems, fmt.Sprintf("unknown COOKER_AUDIT_DESTINATION %q (want \"stdout\" or \"file\")", c.Audit.Destination))
		}
	}
	// Builder safety: docker-socket builder is convenient on dev hosts
	// but gives the Cooker container root-equivalent access to the host
	// docker daemon. RCE in Cooker -> host takeover. Refuse in prod.
	if c.BuilderBackend == "docker" {
		problems = append(problems, "COOKER_BUILDER=docker is unsafe in production (host docker.sock RCE-to-host); use kaniko, buildah, or buildkit")
	}
	// Multi-replica safety: per-process state must be either shared via
	// Redis or pinned via sticky sessions. Otherwise a request lands on
	// a different replica than the one that issued the WS ticket / saw
	// the rate-limit token, and behaviour becomes unpredictable.
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

// DecodeLocalAuthSigningKey returns the raw bytes for the configured
// JWT signing key. The configured value can be base64 (preferred,
// matches COOKER_SECRET_KEY) or a raw secret. We try base64 first
// and fall back to the raw bytes when decode fails — operators who
// `openssl rand -hex 32` get the same protection without the b64 step.
func DecodeLocalAuthSigningKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("required when COOKER_LOCAL_AUTH_ENABLED=true")
	}
	if decoded, err := base64.StdEncoding.DecodeString(s); err == nil && len(decoded) >= 32 {
		return decoded, nil
	}
	return []byte(s), nil
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
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
