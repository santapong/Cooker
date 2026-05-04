package model

import "time"

// Environment represents a deployment target (Dev, Staging, Production).
type Environment struct {
	ID        string            `json:"id" db:"id"`
	Name      string            `json:"name" db:"name"`
	Order     int               `json:"order" db:"order"` // Promotion order: 1=Dev, 2=Staging, 3=Prod
	Target    EnvironmentTarget `json:"target"`
	Promotion PromotionPolicy   `json:"promotion"`

	// PlainVars are non-sensitive key/value pairs stored as-is. Safe to
	// include in list/get responses for any authenticated user.
	PlainVars map[string]string `json:"plainVars"`

	// Secrets holds sensitive values encrypted at rest. Each value is
	// an AES-GCM-sealed blob (nonce ++ ciphertext ++ tag). This field
	// is never serialised to clients directly — Redact() strips it
	// and exposes SecretKeys in its place.
	Secrets map[string][]byte `json:"-"`

	// SecretKeys is the sorted list of secret names available on this
	// environment. Populated by Redact() for client responses; ignored
	// on inbound payloads (see CreateEnvironment/UpdateEnvironment).
	SecretKeys []string `json:"secretKeys,omitempty"`

	// Variables is a deprecated alias for PlainVars. Kept populated on
	// marshal for backwards-compatible clients until Phase 3 ships.
	Variables map[string]string `json:"variables,omitempty"`

	CreatedAt time.Time `json:"createdAt" db:"created_at"`
	// Version powers optimistic concurrency on Update; see store.ErrConflict.
	Version int `json:"version" db:"version"`
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

// Redact returns a copy of e safe for clients that should not see
// secret values. The raw Secrets map is dropped; SecretKeys carries
// only the key names (sorted) so UIs can render an editor listing.
func (e *Environment) Redact() *Environment {
	cp := *e
	cp.Secrets = nil
	if len(e.Secrets) > 0 {
		keys := make([]string, 0, len(e.Secrets))
		for k := range e.Secrets {
			keys = append(keys, k)
		}
		// Sort for stable list responses.
		for i := 1; i < len(keys); i++ {
			for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
				keys[j-1], keys[j] = keys[j], keys[j-1]
			}
		}
		cp.SecretKeys = keys
	}
	return &cp
}
