package model

// ComposeService represents a service defined in a docker-compose file.
type ComposeService struct {
	Name        string            `json:"name"`
	Image       string            `json:"image"`
	Build       *ComposeBuild     `json:"build,omitempty"`
	Ports       []string          `json:"ports"`
	Environment map[string]string `json:"environment"`
	DependsOn   []string          `json:"dependsOn"`
	Networks    []string          `json:"networks"`
	Volumes     []string          `json:"volumes"`
	Command     string            `json:"command"`
	Status      string            `json:"status"`
	// Labels holds the raw compose `labels` for the service. Used to
	// derive Group (via the com.cooker.group key) and available for
	// future label-driven behaviour.
	Labels map[string]string `json:"labels,omitempty"`
	// Group is the deployment group-box this service belongs to,
	// derived at parse time: labels["com.cooker.group"] → else the
	// single network name → else "default".
	Group string `json:"group,omitempty"`
	// Resources holds the parsed CPU/memory limits for the service,
	// sourced from `deploy.resources.limits` or the top-level
	// `mem_limit`/`cpus` keys. Nil when the compose file sets none.
	Resources *ResourceLimits `json:"resources,omitempty"`
}

// ResourceLimits captures per-service CPU/memory limits in a
// runtime-neutral form. The raw compose strings are preserved so each
// runtime can render its own flag/field, and the normalized numeric
// forms let callers compare or default without re-parsing.
//
//   - Memory: the compose-native string, e.g. "512m", "1g".
//   - MemoryBytes: Memory normalized to bytes (0 when unset/unparsable).
//   - CPUs: the compose-native string, e.g. "0.5", "1.5".
//   - NanoCPUs: CPUs × 1e9 (0 when unset/unparsable). 1 CPU = 1e9.
type ResourceLimits struct {
	Memory      string `json:"memory,omitempty"`
	MemoryBytes int64  `json:"memoryBytes,omitempty"`
	CPUs        string `json:"cpus,omitempty"`
	NanoCPUs    int64  `json:"nanoCpus,omitempty"`
}

// ComposeGroupLabel is the compose label key Cooker reads to assign a
// service to a deployment group-box.
const ComposeGroupLabel = "com.cooker.group"

// ComposeBuild represents the build config of a compose service.
type ComposeBuild struct {
	Context    string `json:"context"`
	Dockerfile string `json:"dockerfile"`
}

// ComposeConnection represents a relationship between two compose services.
type ComposeConnection struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"` // "depends_on", "env_reference", "network"
	Label  string `json:"label"`
}

// ComposeGraph is the full parsed graph of a docker-compose file.
type ComposeGraph struct {
	Services    []ComposeService    `json:"services"`
	Connections []ComposeConnection `json:"connections"`
	Networks    []string            `json:"networks"`
	Volumes     []string            `json:"volumes"`
}
