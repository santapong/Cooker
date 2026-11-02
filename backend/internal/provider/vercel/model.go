// Package vercel imports provider-owned deployment records into Cooker and
// provides bounded publication of the embedded landing to new static projects.
package vercel

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound    = errors.New("vercel connection not found")
	ErrConflict    = errors.New("vercel connection changed; reload before retrying")
	ErrBusy        = errors.New("vercel sync already running or waiting for retry")
	ErrDisabled    = errors.New("select projects and enable the connection before syncing")
	ErrUnavailable = errors.New("vercel integration requires a database and COOKER_SECRET_KEY")
	ErrInvalid     = errors.New("invalid vercel connection")
)

const SyncInterval = 120 * time.Second
const HistoryLimit = 100
const MaxSelectedProjects = 20
const SyncTimeout = 90 * time.Second
const leaseDuration = 2 * time.Minute

type Connection struct {
	ID            string                `json:"id"`
	Name          string                `json:"name"`
	TeamID        string                `json:"teamId"`
	Identity      Identity              `json:"identity"`
	Enabled       bool                  `json:"enabled"`
	HasToken      bool                  `json:"hasToken"`
	Version       int                   `json:"version"`
	ProjectIDs    []string              `json:"projectIds"`
	CreatedAt     time.Time             `json:"createdAt"`
	UpdatedAt     time.Time             `json:"updatedAt"`
	LastAttemptAt *time.Time            `json:"lastAttemptAt,omitempty"`
	LastSuccessAt *time.Time            `json:"lastSuccessAt,omitempty"`
	NextSyncAt    *time.Time            `json:"nextSyncAt,omitempty"`
	RetryAt       *time.Time            `json:"retryAt,omitempty"`
	SyncError     string                `json:"syncError,omitempty"`
	Syncing       bool                  `json:"syncing"`
	Coverage      *ProjectCoverage      `json:"coverage,omitempty"`
	Integrations  *IntegrationInventory `json:"integrations,omitempty"`
	Credential    []byte                `json:"-"`
	Lease         string                `json:"-"`
	LeaseUntil    *time.Time            `json:"-"`
	Failures      int                   `json:"-"`
}

type Snapshot struct {
	Projects     []Project             `json:"projects"`
	Deployments  []Deployment          `json:"deployments"`
	Coverage     *ProjectCoverage      `json:"coverage,omitempty"`
	Integrations *IntegrationInventory `json:"integrations,omitempty"`
}

// IntegrationInstallation records a provider installation's allowed project
// access. It is not evidence of resource use, connectivity or resource health.
type IntegrationInstallation struct {
	ID               string   `json:"id"`
	IntegrationID    string   `json:"integrationId"`
	Slug             string   `json:"slug,omitempty"`
	Name             string   `json:"name"`
	InstallationType string   `json:"installationType"`
	ProviderStatus   string   `json:"providerStatus"`
	ProjectAccess    string   `json:"projectAccess"`
	ProjectIDs       []string `json:"projectIds"`
	// ownerID is transient validation metadata, never persisted or projected.
	ownerID string
}

// IntegrationInventory has an independent success clock. Complete describes
// the retained successful observation, which may be stale after a later failure.
type IntegrationInventory struct {
	Items       []IntegrationInstallation `json:"items"`
	ObservedAt  *time.Time                `json:"observedAt,omitempty"`
	AttemptedAt *time.Time                `json:"attemptedAt,omitempty"`
	State       string                    `json:"state"`
	ErrorCode   string                    `json:"errorCode,omitempty"`
	Error       string                    `json:"error,omitempty"`
	Complete    bool                      `json:"complete"`
}

// ProjectCoverage describes one successful, complete discovery in the saved
// account/team scope. It does not claim access to other accounts or teams.
type ProjectCoverage struct {
	AccessibleProjects int       `json:"accessibleProjects"`
	SelectedProjects   int       `json:"selectedProjects"`
	ObservedAt         time.Time `json:"observedAt"`
}
type Record struct {
	Connection
	Snapshot Snapshot
}
type ImportedProject struct {
	Project
	ConnectionID string `json:"connectionId"`
}
type ImportedDeployment struct {
	Deployment
	ConnectionID string `json:"connectionId"`
}
type View struct {
	Connections         []*Connection        `json:"connections"`
	Projects            []ImportedProject    `json:"projects"`
	Deployments         []ImportedDeployment `json:"deployments"`
	SyncIntervalSeconds int                  `json:"syncIntervalSeconds"`
	HistoryLimit        int                  `json:"historyLimit"`
}

type API interface {
	Verify(context.Context, string, string) (Identity, error)
	Projects(context.Context, string, string) ([]Project, error)
	Deployments(context.Context, string, string, string) ([]Deployment, error)
}

// IntegrationsAPI is optional so core inventory adapters remain compatible.
type IntegrationsAPI interface {
	Integrations(context.Context, string, string) ([]IntegrationInstallation, error)
}

// Store fences both configuration changes and sync attempts. Finish cannot
// publish a stale account/project selection, or overwrite a newer sync lease.
type Store interface {
	List(context.Context) ([]*Record, error)
	Get(context.Context, string) (*Record, error)
	Create(context.Context, *Record) error
	Update(context.Context, *Record, int) error
	Delete(context.Context, string, int) error
	Claim(context.Context, string, int, time.Time, time.Time, bool) (string, error)
	Finish(context.Context, string, int, string, *Snapshot, string, time.Time, *time.Time) error
}
