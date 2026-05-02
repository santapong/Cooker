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
}

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
