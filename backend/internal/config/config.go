package config

import "time"

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
	Env            Env
	Port           int
	DatabaseURL    string
	RedisURL       string
	AllowedOrigins []string
	SecretKey      string
	Registry       string
	// BuildCacheRepo, when non-empty, stamps a registry layer-cache
	// ref (CacheSpec{Mode:"registry"}) onto the build stages of
	// app-deploy synthesized pipelines. Hand-built pipelines configure
	// cache per stage instead.
	BuildCacheRepo string
	BuilderBackend string
	// StageRunner selects the container runtime for Test/Custom stages:
	// "kube" (one-shot Job), "docker" (`docker run`), or "noop" (default —
	// no container; dev/test only). Loaded from COOKER_STAGE_RUNNER.
	StageRunner       string
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
	CloudInventory    CloudInventoryConfig
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
	// Feedback configures the in-app feedback button behind
	// POST /feedback (pure GitHub-issue relay, nothing persisted).
	// Token empty (the default) keeps the feature off: the route
	// returns 503 and the frontend hides the button. The token stays
	// server-side.
	Feedback FeedbackConfig
	// License configures self-hosted offline licensing (M2 — see
	// docs/launch/01-billing-monetization.md §4). Both fields empty =>
	// the install runs on the Free (Explorer) tier with no license, which
	// is the default and never an error.
	License LicenseConfig
}

// LicenseConfig holds the self-hosted licensing inputs. An operator may
// set a signed license token at boot (Key) which the server installs once
// at startup; verification needs the vendor's Ed25519 public key
// (PublicKey, base64). The model is permissive: no license means Free.
// The only hard rule is that a Key without a PublicKey cannot be verified
// and is therefore a misconfiguration (caught in Validate).
type LicenseConfig struct {
	// Key is an optional signed license token an operator sets at boot via
	// COOKER_LICENSE_KEY. When set, the server attempts to install it once
	// at startup, degrading to Free (with a log line) on any failure —
	// never panicking. Empty = no boot-time install (the admin API can
	// still install one later).
	Key string
	// PublicKey is the base64-encoded Ed25519 public key
	// (COOKER_LICENSE_PUBLIC_KEY) used to verify license tokens. Empty
	// disables verification entirely (every install attempt fails, Free
	// is used). Kept for back-compat; it is appended to PublicKeys.
	PublicKey string
	// PublicKeys is the set of base64-encoded Ed25519 public keys
	// (COOKER_LICENSE_PUBLIC_KEYS, comma-separated) any of which may
	// verify a license token. This supports key rotation: an operator can
	// trust both the outgoing and incoming vendor keys during a rollover
	// window. The singular PublicKey is appended for back-compat, so a
	// deployment that only sets COOKER_LICENSE_PUBLIC_KEY keeps working.
	// Verification succeeds if ANY configured key validates the token.
	PublicKeys []string
}

// TriageConfig wires the Anthropic Messages API client behind
// POST /pipelines/:id/runs/:runId/stages/:stageId/triage.
type TriageConfig struct {
	Enabled bool   // COOKER_AI_TRIAGE_ENABLED (default false)
	Model   string // COOKER_AI_TRIAGE_MODEL (default claude-fable-5)
	APIKey  string // ANTHROPIC_API_KEY (required when Enabled)
}

// FeedbackConfig wires the GitHub issue relay behind POST /feedback.
// An empty Token disables the feature (no Validate rule — off is a
// valid configuration, not a misconfiguration).
type FeedbackConfig struct {
	Repo  string // COOKER_FEEDBACK_GITHUB_REPO (default santapong/Cooker)
	Token string // COOKER_FEEDBACK_GITHUB_TOKEN (empty = feature off)
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
	// MetricsPort, when > 0, serves /metrics on a SEPARATE HTTP listener
	// (COOKER_METRICS_PORT) instead of registering it on the public app
	// router. This keeps /metrics off the public app ingress (audit
	// finding M0-1). 0 (default) preserves the single-port behaviour:
	// /metrics is mounted on the main router.
	MetricsPort int
	// MetricsHost is the bind interface for the dedicated metrics listener
	// (COOKER_METRICS_HOST). It is only consulted when MetricsPort > 0. The
	// default "" binds all interfaces (back-compat, and required so an
	// in-cluster Prometheus ServiceMonitor can scrape the pod IP). Operators
	// who rely on the dedicated metrics port MUST restrict access to it via a
	// NetworkPolicy (or bind it to a private interface here) — it is not
	// gated by the app's auth middleware. The chart side is handled
	// separately.
	MetricsHost string
}

type RateLimitConfig struct {
	Enabled   bool
	PerMinute int
	Burst     int
	Backend   string
	// Webhook is a separate per-source-IP limiter for the unauthenticated
	// git-provider webhook receivers (/webhooks/*). Those routes are NOT
	// behind the /api/v1 per-user limiter, and each request costs a DB
	// lookup + a secret decryption before the HMAC/token check — so an
	// unauthenticated caller could amplify that work. This bounds it.
	Webhook WebhookRateLimitConfig
}

// WebhookRateLimitConfig tunes the dedicated per-IP limiter on the
// unauthenticated webhook receivers. It is independent of the per-user
// limiter (different threat: unauthenticated amplification, not per-user
// fairness), so it has its own enable flag and budget. Always in-memory /
// per-process — multi-replica deployments should also enforce edge
// rate-limiting, exactly like the per-user limiter.
type WebhookRateLimitConfig struct {
	Enabled   bool
	PerMinute int
	Burst     int
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

// CloudInventoryConfig configures the read-only cloud inventory & cost
// panel (OR-2). Each provider is enabled independently; when neither is
// enabled the feature is dormant (the GET endpoints return 200 with
// enabled=false). CacheTTL bounds how often the enabled providers are
// queried — the cost APIs (AWS Cost Explorer) are billed per request, so
// the default is deliberately coarse.
type CloudInventoryConfig struct {
	CacheTTL time.Duration
	AWS      CloudAWSConfig
	GCP      CloudGCPConfig
}

// CloudAWSConfig holds the AWS provider inputs. Region is required when
// Enabled. AccessKeyID/SecretAccessKey are optional explicit static
// credentials; empty means the standard AWS chain (IRSA / instance
// profile / env / shared config), matching the awsm secrets backend.
type CloudAWSConfig struct {
	Enabled         bool
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

// CloudGCPConfig holds the GCP provider inputs. ProjectID is required
// when Enabled. CredentialsJSON is an optional service-account key (raw
// JSON); empty means Application Default Credentials, matching the gcpsm
// secrets backend.
type CloudGCPConfig struct {
	Enabled         bool
	ProjectID       string
	CredentialsJSON string
}

type AuditConfig struct {
	Enabled bool
	// Destination accepts a comma list: stdout | file | db, e.g.
	// "db,stdout" writes through to both (roadmap M5).
	Destination string
	FilePath    string
	// DBRetention bounds the queryable audit trail when the db sink
	// is active. 0 disables the sweep. COOKER_AUDIT_DB_RETENTION,
	// default 90 days.
	DBRetention time.Duration
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
