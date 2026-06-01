package model

import "time"

// Pipeline represents a CI/CD pipeline definition with its stages and edges.
type Pipeline struct {
	ID          string            `json:"id" db:"id"`
	Name        string            `json:"name" db:"name"`
	Description string            `json:"description" db:"description"`
	Stages      []Stage           `json:"stages"`
	Edges       []Edge            `json:"edges"`
	Variables   map[string]string `json:"variables"`
	CreatedAt   time.Time         `json:"createdAt" db:"created_at"`
	UpdatedAt   time.Time         `json:"updatedAt" db:"updated_at"`
	// Version is the optimistic-concurrency token. Incremented on
	// every successful Update; clients echo it back on PUT/PATCH so
	// concurrent edits are detected and rejected with 409.
	Version int `json:"version" db:"version"`
}

// StageType identifies the kind of pipeline stage.
type StageType string

const (
	StageTypeBuild        StageType = "build"
	StageTypeTest         StageType = "test"
	StageTypeDeploy       StageType = "deploy"
	StageTypePush         StageType = "push"
	StageTypeApproval     StageType = "approval"
	StageTypeCustom       StageType = "custom"
	StageTypeGitOpsCommit StageType = "gitops-commit"
)

// Stage is a single step in a pipeline, rendered as a node in the graph UI.
type Stage struct {
	ID            string      `json:"id"`
	Name          string      `json:"name"`
	Type          StageType   `json:"type"`
	Config        StageConfig `json:"config"`
	EnvironmentID string      `json:"environmentId,omitempty"`
	Position      Position    `json:"position"`
	// Group names the deployment group-box this stage belongs to,
	// rendered as a container node in the deployment DAG view. Empty
	// means "ungrouped" (backward compatible with hand-built
	// pipelines). Populated by compose-driven synthesis.
	Group string `json:"group,omitempty"`
}

// StageConfig holds type-specific configuration for a pipeline stage.
type StageConfig struct {
	// Build
	Dockerfile string            `json:"dockerfile,omitempty"`
	Context    string            `json:"context,omitempty"`
	BuildArgs  map[string]string `json:"buildArgs,omitempty"`
	Tags       []string          `json:"tags,omitempty"`
	Platforms  []string          `json:"platforms,omitempty"` // Multi-arch OCI Image Index

	// Test
	Image   string   `json:"image,omitempty"`
	Command []string `json:"command,omitempty"`

	// Push
	Registry   string `json:"registry,omitempty"`
	Repository string `json:"repository,omitempty"`

	// Deploy (K8s)
	Namespace    string                 `json:"namespace,omitempty"`
	ManifestPath string                 `json:"manifestPath,omitempty"`
	HelmChart    string                 `json:"helmChart,omitempty"`
	HelmValues   map[string]interface{} `json:"helmValues,omitempty"`

	// Deploy runtime selection for compose-synthesized per-service
	// deploy stages: "kubernetes" (manifest apply), "docker" (per-
	// service docker run), or "compose" (docker compose up). Empty
	// preserves the legacy manifest/helm dispatch.
	DeployRuntime string `json:"deployRuntime,omitempty"`

	// Compose provenance — set on stages synthesized from a compose
	// service so the executor/runtime layer can correlate a stage back
	// to its originating service (and re-derive a manifest/run command).
	ComposeServiceName  string `json:"composeServiceName,omitempty"`
	ComposeBuildContext string `json:"composeBuildContext,omitempty"`
	ComposeDockerfile   string `json:"composeDockerfile,omitempty"`
	// ComposePorts carries the compose service's `ports:` so a
	// docker-run deploy can publish them (-p). Format is compose-native
	// ("host:container" or bare "port"). K8s deploys use the synthesized
	// manifest's port instead, so this is docker-runtime-specific.
	ComposePorts []string `json:"composePorts,omitempty"`

	// Resources carries per-service CPU/memory limits to apply at
	// deploy time (K8s resources.limits, or docker --memory/--cpus).
	Resources *ResourceLimits `json:"resources,omitempty"`

	// Custom
	Script  string `json:"script,omitempty"`
	Timeout string `json:"timeout,omitempty"`
	Retries int    `json:"retries,omitempty"`

	// Env overrides the stage's environment variables. Values here win
	// over anything inherited from the Pipeline.Variables or the
	// resolved Environment.PlainVars.
	Env map[string]string `json:"env,omitempty"`
	// SecretRefs names Environment.Secrets entries the executor must
	// decrypt and inject into this stage's runtime. The stage never
	// sees ciphertext; resolution happens just before execution.
	SecretRefs []string `json:"secretRefs,omitempty"`

	// GitOps (StageTypeGitOpsCommit)
	GitOpsRepo    string `json:"gitopsRepo,omitempty"`    // e.g., git@github.com:org/gitops.git
	GitOpsBranch  string `json:"gitopsBranch,omitempty"`  // defaults to "main"
	GitOpsPath    string `json:"gitopsPath,omitempty"`    // path within the repo to update
	GitOpsMessage string `json:"gitopsMessage,omitempty"` // commit message template
	// GitOpsContent is the manifest (or values.yaml) body Cooker
	// should commit at GitOpsPath. Templating (${IMAGE}, etc.)
	// happens at run time.
	GitOpsContent string `json:"gitopsContent,omitempty"`
}

// Edge connects two stages in the pipeline graph.
type Edge struct {
	ID        string `json:"id"`
	Source    string `json:"source"`
	Target    string `json:"target"`
	Condition string `json:"condition,omitempty"` // "success", "failure", "always"
}

// Position stores x,y coordinates for the graph UI.
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}
