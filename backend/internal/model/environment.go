package model

import "time"

// Environment represents a deployment target (Dev, Staging, Production).
type Environment struct {
	ID        string            `json:"id" db:"id"`
	Name      string            `json:"name" db:"name"`
	Order     int               `json:"order" db:"order"` // Promotion order: 1=Dev, 2=Staging, 3=Prod
	Target    EnvironmentTarget `json:"target"`
	Promotion PromotionPolicy   `json:"promotion"`
	Variables map[string]string `json:"variables"`
	CreatedAt time.Time         `json:"createdAt" db:"created_at"`
}

// EnvironmentTarget defines where an environment deploys to.
type EnvironmentTarget struct {
	Type        string `json:"type"`        // "cluster" or "namespace"
	ClusterID   string `json:"clusterId"`   // References a configured K8s cluster
	Namespace   string `json:"namespace"`   // Target namespace
	KubeContext string `json:"kubeContext"` // kubeconfig context name
}

// PromotionPolicy controls how deployments promote between environments.
type PromotionPolicy struct {
	Strategy          string   `json:"strategy"`                    // "auto" or "manual"
	RequiredApprovers int      `json:"requiredApprovers,omitempty"` // For manual approval
	AutoPromoteOn     []string `json:"autoPromoteOn,omitempty"`     // Conditions: ["tests_pass", "health_check"]
}
