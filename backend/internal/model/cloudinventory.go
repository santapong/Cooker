package model

// Cloud-inventory domain types. These are pure data shapes (no
// behaviour) returned by the read-only cloud inventory providers and
// aggregated by the CloudInventoryService. They are deliberately
// provider-agnostic: an AWS EC2 instance and a GCP Compute instance
// both flatten into a CloudResource so the frontend renders one table.

// CloudResourceType enumerates the read-only resource categories the
// inventory providers report. The set is intentionally small — compute,
// managed Kubernetes, and container registries — matching what the panel
// surfaces today.
type CloudResourceType string

const (
	// CloudResourceCompute is a virtual machine / instance
	// (AWS EC2, GCP Compute Engine).
	CloudResourceCompute CloudResourceType = "compute"
	// CloudResourceCluster is a managed Kubernetes cluster
	// (AWS EKS, GCP GKE).
	CloudResourceCluster CloudResourceType = "cluster"
	// CloudResourceRegistry is a container image registry / repository
	// (AWS ECR, GCP Artifact Registry).
	CloudResourceRegistry CloudResourceType = "registry"
)

// CloudProvider names the cloud a resource or cost belongs to.
type CloudProvider string

const (
	CloudProviderAWS CloudProvider = "aws"
	CloudProviderGCP CloudProvider = "gcp"
)

// CloudResource is one discovered cloud resource, flattened to a common
// shape across providers. ID is the provider-native identifier (instance
// ID, cluster name, repository URI); Name is a human label when the
// provider exposes one (else equal to ID). Region is the location the
// resource lives in. Status is a provider-native lifecycle string
// (e.g. "running", "ACTIVE") passed through verbatim for display.
// Tags carries provider labels/tags when cheaply available; it may be
// nil. No secret or credential material is ever placed on this struct.
type CloudResource struct {
	Provider CloudProvider     `json:"provider"`
	Type     CloudResourceType `json:"type"`
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Region   string            `json:"region"`
	Status   string            `json:"status"`
	Tags     map[string]string `json:"tags,omitempty"`
}

// CostServiceLine is month-to-date spend for a single cloud service
// (e.g. "Amazon Elastic Compute Cloud", "Compute Engine"). Amount is in
// the provider's billing currency, rendered as a decimal string to avoid
// float rounding surprises in the UI; Currency is the ISO code.
type CostServiceLine struct {
	Service  string `json:"service"`
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

// CostSummary is a provider's current-month-to-date spend, broken down
// by service. Total is the sum of the lines (provider-reported, not
// recomputed, so it matches the billing console). Currency is the ISO
// code the amounts are denominated in. Start/End bound the period the
// figures cover (RFC3339 dates), so the UI can label "MTD through
// <End>".
type CostSummary struct {
	Provider CloudProvider     `json:"provider"`
	Currency string            `json:"currency"`
	Total    string            `json:"total"`
	Start    string            `json:"start"`
	End      string            `json:"end"`
	Services []CostServiceLine `json:"services"`
}

// ProviderInventory is the result of querying one provider. Error is the
// provider-scoped failure message when that provider's calls failed; it
// is non-empty ONLY for the failing provider, so one cloud being down
// yields partial results rather than a whole-request error. Resources
// and Cost are zero-valued when Error is set.
type ProviderInventory struct {
	Provider  CloudProvider   `json:"provider"`
	Resources []CloudResource `json:"resources"`
	Cost      *CostSummary    `json:"cost,omitempty"`
	Error     string          `json:"error,omitempty"`
}

// CloudInventory is the aggregated response across all enabled
// providers. Enabled is false when no provider is configured — the
// handlers return 200 with Enabled=false and an empty Providers slice
// rather than an error, so the UI can render a friendly "configure a
// cloud" empty state. Providers lists the enabled providers' names
// (even ones that errored) so the UI can show a per-provider banner.
// FetchedAt is the RFC3339 timestamp the snapshot was produced (cache
// fill time, not request time), so the UI can show "as of".
type CloudInventory struct {
	Enabled   bool                `json:"enabled"`
	Providers []CloudProvider     `json:"providers"`
	Results   []ProviderInventory `json:"results"`
	FetchedAt string              `json:"fetchedAt"`
}
