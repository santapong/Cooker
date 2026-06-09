package config

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
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
	Env               Env
	Port              int
	DatabaseURL       string
	RedisURL          string
	AllowedOrigins    []string
	SecretKey         string
	Registry          string
	BuilderBackend    string
	PusherBackend     string
	DeployerBackend   string
	SecretsBackend    string
	ReplicaCount      int
	StickySessions    bool
	RateLimit         RateLimitConfig
	WSTicket          WSTicketConfig
	WSHub             WSHubConfig
	OIDC              OIDCConfig
	LocalAuth         LocalAuthConfig
	Docker            DockerConfig
	Kubernetes        KubernetesConfig
	KeepSave          KeepSaveConfig
	Vault             VaultConfig
	AWSSecrets        AWSSecretsConfig
	GCPSecrets        GCPSecretsConfig
	DeployTargets     DeployTargetsConfig
	Audit             AuditConfig
	Observability     ObservabilityConfig
	AppHealthInterval time.Duration
	JobQueue          JobQueueConfig
	// Scheduler configures the Phase-2 cron-triggered runs loop. Default
	// Enabled=false; requires JobQueue.Enabled=true at boot. See
	// internal/scheduler/scheduler.go.
	Scheduler SchedulerConfig
	// Governance configures the Grovernance Platform admission hook
	// (Phase-4). URL empty -> disabled (no-op middleware). See
	// internal/governance/client.go.
	Governance GovernanceConfig
	// Triage configures the opt-in AI failure-triage endpoint
	// (roadmap M4). Enabled=false keeps the route returning 503 and
	// no key is ever required. The API key stays server-side.
	Triage TriageConfig
}

// TriageConfig wires the Anthropic Messages API client behind
// POST /pipelines/:id/runs/:runId/stages/:stageId/triage.
type TriageConfig struct {
	Enabled bool   // COOKER_AI_TRIAGE_ENABLED (default false)
	Model   string // COOKER_AI_TRIAGE_MODEL (default claude-fable-5)
	APIKey  string // ANTHROPIC_API_KEY (required when Enabled)
}

// GovernanceConfig configures the call-out to the Grovernance Platform's
// /authorize endpoint that gates every deploy. An empty URL disables the
// integration; FailOpenEnvs is the comma-separated list of envs that should
// proceed when Grovernance is unreachable (production is fail-closed by
// default). BootstrapServices is the allow-list of service names that bypass
// the gate — required so Grovernance itself can be deployed through Cooker.
//
// CallerToken authenticates Cooker to Grovernance. Required by Grovernance
// v1.1+ in production posture (GOVERNANCE_REQUIRE_CALLER_AUTH=true on the
// gate). Loaded from COOKER_GOVERNANCE_CALLER_TOKEN; production validation
// requires it whenever URL is set.
//
// DelegateToken is the second service-account token, used by the pipeline-
// deploy executor when it calls AuthorizeOnBehalf with a pre-resolved actor.
// Must hold the governance.authorize_on_behalf scope on the gate. Optional —
// when empty the executor hook is a no-op (the HTTP middleware still gates
// /apps/:id/deploy). Loaded from COOKER_GOVERNANCE_DELEGATE_TOKEN.
//
// BreakGlassEnabled toggles the narrow escape hatch in the HTTP middleware:
// when the gate is unreachable AND env is fail-closed AND the request
// carries X-Break-Glass-Justification, log a structured event and let the
// request through. Off by default. Loaded from
// COOKER_GOVERNANCE_BREAK_GLASS_ENABLED. Audit lives in the slog stream — no
// dedicated table in v1.1; see Milestone D for the audit-backup runbook.
type GovernanceConfig struct {
	URL               string
	FailOpenEnvs      []string
	BootstrapServices []string
	CallerToken       string
	DelegateToken     string
	BreakGlassEnabled bool
}

type WSHubConfig struct {
	Backend string
}

type ObservabilityConfig struct {
	MetricsEnabled bool
	TracingEnabled bool
	OTLPEndpoint   string
	OTLPInsecure   bool
	ServiceName    string
	ServiceVersion string
}

type RateLimitConfig struct {
	Enabled   bool
	PerMinute int
	Burst     int
	Backend   string
}

type WSTicketConfig struct {
	Backend string
}

type OIDCConfig struct {
	Enabled      bool
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
	GroupRoleMap map[string]string
	MFAACRValues []string
}

type LocalAuthConfig struct {
	Enabled       bool
	JWTSigningKey string
	TokenTTL      time.Duration
	AllowSignup   bool
}

type DockerConfig struct {
	Host      string
	TLSVerify bool
	CertPath  string
}

type KubernetesConfig struct {
	InCluster             bool
	Kubeconfig            string
	Namespace             string
	KanikoImage           string
	KanikoServiceAccount  string
	KanikoContextPVC      string
	BuildahImage          string
	BuildahServiceAccount string
	BuildahContextPVC     string
	BuildahStorageDriver  string
}

type KeepSaveConfig struct {
	URL       string
	ProjectID string
	APIKey    string
}

type VaultConfig struct {
	Addr   string
	Token  string
	Mount  string
	Prefix string
}

type AWSSecretsConfig struct {
	Region string
	Prefix string
}

type GCPSecretsConfig struct {
	ProjectID string
	Prefix    string
}

type DeployTargetsConfig struct {
	CloudRunProject   string
	CloudRunRegion    string
	ECSRegion         string
	ECSCluster        string
	ECSExecutionRole  string
	ECSTaskRole       string
	ECSSubnets        []string
	ECSSecurityGroups []string
	FlyToken          string
	FlyRegion         string
	RenderToken       string
	RenderOwnerID     string
}

type AuditConfig struct {
	Enabled     bool
	Destination string
	FilePath    string
}

// JobQueueConfig configures the Phase-1 durable async job queue.
// Default Enabled=false; when false, no queue is booted.
type JobQueueConfig struct {
	Enabled        bool
	Workers        int
	WorkerIDPrefix string
}

// SchedulerConfig configures the Phase-2 cron scheduler. Requires
// JobQueue.Enabled=true — the scheduler enqueues runs through the
// jobqueue rather than running them inline.
type SchedulerConfig struct {
	Enabled   bool          // COOKER_SCHEDULER_ENABLED (default false)
	TickEvery time.Duration // COOKER_SCHEDULER_TICK (default 30s)
}

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
		WSTicket: WSTicketConfig{Backend: getEnv("COOKER_WS_TICKET_BACKEND", "memory")},
		WSHub:    WSHubConfig{Backend: getEnv("COOKER_WS_HUB_BACKEND", "memory")},
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
		JobQueue: JobQueueConfig{
			Enabled:        getEnvBool("COOKER_JOBQUEUE_ENABLED", false),
			Workers:        getEnvInt("COOKER_JOBQUEUE_WORKERS", 4),
			WorkerIDPrefix: getEnv("COOKER_JOBQUEUE_WORKER_PREFIX", "cooker"),
		},
		Scheduler: SchedulerConfig{
			Enabled:   getEnvBool("COOKER_SCHEDULER_ENABLED", false),
			TickEvery: getEnvDuration("COOKER_SCHEDULER_TICK", 30*time.Second),
		},
		Governance: GovernanceConfig{
			URL:               getEnv("COOKER_GOVERNANCE_URL", ""),
			FailOpenEnvs:      getEnvCSV("COOKER_GOVERNANCE_FAIL_OPEN_ENVS", []string{"dev", "staging"}),
			BootstrapServices: getEnvCSV("COOKER_GOVERNANCE_BOOTSTRAP_SERVICES", []string{"governance"}),
			CallerToken:       getEnv("COOKER_GOVERNANCE_CALLER_TOKEN", ""),
			DelegateToken:     getEnv("COOKER_GOVERNANCE_DELEGATE_TOKEN", ""),
			BreakGlassEnabled: getEnvBool("COOKER_GOVERNANCE_BREAK_GLASS_ENABLED", false),
		},
		Triage: TriageConfig{
			Enabled: getEnvBool("COOKER_AI_TRIAGE_ENABLED", false),
			Model:   getEnv("COOKER_AI_TRIAGE_MODEL", ""),
			APIKey:  getEnv("ANTHROPIC_API_KEY", ""),
		},
	}
}

func (c *Config) Validate() error {
	// AI triage is env-independent: enabling it without a key is a
	// misconfiguration in dev and prod alike — the route would 502 on
	// every click. Fail at boot instead.
	if c.Triage.Enabled && c.Triage.APIKey == "" {
		return fmt.Errorf("config: COOKER_AI_TRIAGE_ENABLED=true requires ANTHROPIC_API_KEY")
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
