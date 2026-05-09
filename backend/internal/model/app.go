package model

import "time"

// AppHealth is the post-deploy health verdict observed by the
// AppHealthChecker. "unknown" means no probe has run yet (or the
// target kind has no probe wired); "healthy" means the deployed
// workload is fully serving; "degraded" is partial readiness;
// "failed" means the probe asserts the workload is not serving.
type AppHealth string

const (
	AppHealthUnknown  AppHealth = "unknown"
	AppHealthHealthy  AppHealth = "healthy"
	AppHealthDegraded AppHealth = "degraded"
	AppHealthFailed   AppHealth = "failed"
)

// BuildPlanKind identifies how an App's source should be turned
// into a container image.
type BuildPlanKind string

const (
	BuildPlanDockerfile BuildPlanKind = "dockerfile"
	BuildPlanCompose    BuildPlanKind = "compose"
	BuildPlanBuildpack  BuildPlanKind = "buildpack"
)

// DeployTargetKind selects the deploy backend for an App.
type DeployTargetKind string

const (
	DeployTargetDockerHost DeployTargetKind = "docker-host"
	DeployTargetKubernetes DeployTargetKind = "kubernetes"
	DeployTargetCloudRun   DeployTargetKind = "cloud-run"
	DeployTargetECS        DeployTargetKind = "ecs"
	DeployTargetFly        DeployTargetKind = "fly"
	DeployTargetRender     DeployTargetKind = "render"
)

// BuildPlan is a resolved build strategy for an App's repository.
// Detected automatically via internal/buildplan; operators may
// override it on the App.
type BuildPlan struct {
	Kind       BuildPlanKind     `json:"kind"`
	Path       string            `json:"path,omitempty"`       // e.g., "Dockerfile", "docker-compose.yml"
	Args       map[string]string `json:"args,omitempty"`       // Build args / buildpack env
	Buildpacks []string          `json:"buildpacks,omitempty"` // Paketo buildpack IDs
}

// DeployTarget pins an App to a specific runtime.
type DeployTarget struct {
	Kind      DeployTargetKind `json:"kind"`
	HostID    string           `json:"hostId,omitempty"`    // Managed host (Phase 4)
	Namespace string           `json:"namespace,omitempty"` // Kubernetes only
	Region    string           `json:"region,omitempty"`    // Cloud targets
	Service   string           `json:"service,omitempty"`   // Cloud Run service name
}

// App is a user-facing unit of deployment: one GitHub repo, one
// build plan, one deploy target. The "Deploy" button on an App
// synthesizes a Clone→Build→Push→Deploy run at request time.
type App struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`

	// GitHubRepo is "owner/name". Cooker clones via HTTPS unless a
	// deploy key on the App overrides this.
	GitHubRepo string `json:"githubRepo"`
	Branch     string `json:"branch"` // Default: "main".

	// BuildPlan. Nil means "detect at deploy time".
	BuildPlan *BuildPlan `json:"buildPlan,omitempty"`

	DeployTarget DeployTarget `json:"deployTarget"`

	// RegistryRef optionally pins the destination registry
	// (e.g. "ghcr.io/org"). Empty falls back to the bundled registry.
	RegistryRef string `json:"registryRef,omitempty"`

	// EnvironmentID links this App to a Cooker Environment for
	// PlainVars + Secrets injection.
	EnvironmentID string `json:"environmentId,omitempty"`

	// WebhookSecret is the HMAC secret configured on the GitHub
	// webhook. Stored encrypted (via the same Codec as secrets).
	// Never serialised back to clients; rotated via a dedicated
	// endpoint.
	WebhookSecret []byte `json:"-"`
	HasWebhook    bool   `json:"hasWebhook"` // convenience flag for clients

	// AutoDeploy, when true, triggers a deploy on every push event
	// that matches Branch. When false, only manual deploys run.
	AutoDeploy bool `json:"autoDeploy"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	// Version powers optimistic concurrency on Update; see store.ErrConflict.
	Version int `json:"version"`

	// HealthStatus is the latest verdict from AppHealthChecker (Week 1
	// observability batch). "unknown" until the first probe runs, or when
	// the target kind has no probe wired. Updated via the dedicated
	// AppStore.UpdateHealth method, never via Update — health is a side
	// channel that should not bump Version or block on a stale row.
	HealthStatus    AppHealth  `json:"healthStatus" db:"health_status"`
	HealthCheckedAt *time.Time `json:"healthCheckedAt,omitempty" db:"health_checked_at"`
	HealthMessage   string     `json:"healthMessage,omitempty" db:"health_message"`
}

// Redact returns a copy of a safe for client responses.
func (a *App) Redact() *App {
	cp := *a
	cp.WebhookSecret = nil
	cp.HasWebhook = len(a.WebhookSecret) > 0
	return &cp
}
