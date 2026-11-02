// Package catalog provides read-only workspace projections. Imported resources
// are never written into executable App, Pipeline, Run, or job stores.
package catalog

import (
	"time"

	"github.com/santapong/cooker/internal/provider/vercel"
)

type SourceStatus struct {
	Provider  string `json:"provider"`
	Available bool   `json:"available"`
	Error     string `json:"error,omitempty"`
}
type Freshness struct {
	State      string     `json:"state"`
	ObservedAt *time.Time `json:"observedAt,omitempty"`
	Error      string     `json:"error,omitempty"`
}
type Capabilities struct {
	Edit    bool `json:"edit"`
	Deploy  bool `json:"deploy"`
	Inspect bool `json:"inspect"`
	Logs    bool `json:"logs"`
}
type Deployment struct {
	ID                string     `json:"id"`
	Provider          string     `json:"provider"`
	Status            string     `json:"status"`
	Environment       string     `json:"environment"`
	CreatedAt         time.Time  `json:"createdAt"`
	GitBranch         string     `json:"gitBranch,omitempty"`
	GitCommitSHA      string     `json:"gitCommitSHA,omitempty"`
	URL               string     `json:"url,omitempty"`
	ProductionURLs    []string   `json:"productionUrls"`
	InspectorURL      string     `json:"inspectorUrl,omitempty"`
	BuildingAt        *time.Time `json:"buildingAt,omitempty"`
	ReadyAt           *time.Time `json:"readyAt,omitempty"`
	Href              string     `json:"href,omitempty"`
	SourceLabel       string     `json:"sourceLabel,omitempty"`
	ArtifactSHA256    string     `json:"artifactSha256,omitempty"`
	MetadataFreshness *Freshness `json:"metadataFreshness,omitempty"`
	AliasFreshness    *Freshness `json:"aliasFreshness,omitempty"`
}
type App struct {
	ID                   string       `json:"id"`
	Name                 string       `json:"name"`
	Provider             string       `json:"provider"`
	Management           string       `json:"management"`
	Href                 string       `json:"href"`
	PipelineID           string       `json:"pipelineId,omitempty"`
	Framework            string       `json:"framework,omitempty"`
	Repository           string       `json:"repository,omitempty"`
	Branch               string       `json:"branch,omitempty"`
	Environment          string       `json:"environment,omitempty"`
	Health               string       `json:"health"`
	LatestDeployment     *Deployment  `json:"latestDeployment,omitempty"`
	ProductionDeployment *Deployment  `json:"productionDeployment,omitempty"`
	Freshness            Freshness    `json:"freshness"`
	MetadataFreshness    *Freshness   `json:"metadataFreshness,omitempty"`
	Capabilities         Capabilities `json:"capabilities"`
	SourceLabel          string       `json:"sourceLabel,omitempty"`
	ArtifactSHA256       string       `json:"artifactSha256,omitempty"`
	NativeAppID          string       `json:"nativeAppId,omitempty"`
	SourceUnavailable    bool         `json:"sourceUnavailable,omitempty"`
}
type ServiceRecord struct {
	ID              string     `json:"id"`
	AppID           string     `json:"appId"`
	AppName         string     `json:"appName"`
	AppHref         string     `json:"appHref"`
	Name            string     `json:"name"`
	Provider        string     `json:"provider"`
	Management      string     `json:"management"`
	Environment     string     `json:"environment"`
	Status          string     `json:"status"`
	Health          string     `json:"health"`
	Framework       string     `json:"framework,omitempty"`
	Runtime         string     `json:"runtime,omitempty"`
	Definition      string     `json:"definition"`
	DeploymentID    string     `json:"deploymentId,omitempty"`
	DeploymentHref  string     `json:"deploymentHref,omitempty"`
	InspectorURL    string     `json:"inspectorUrl,omitempty"`
	ObservedAt      *time.Time `json:"observedAt,omitempty"`
	Error           string     `json:"error,omitempty"`
	Href            string     `json:"href,omitempty"`
	NativeServiceID string     `json:"nativeServiceId,omitempty"`
}
type Pipeline struct {
	ID                string       `json:"id"`
	Name              string       `json:"name"`
	Provider          string       `json:"provider"`
	Management        string       `json:"management"`
	Kind              string       `json:"kind"`
	Href              string       `json:"href"`
	AppID             string       `json:"appId,omitempty"`
	AppName           string       `json:"appName,omitempty"`
	Stages            int          `json:"stages,omitempty"`
	LatestDeployment  *Deployment  `json:"latestDeployment,omitempty"`
	Freshness         Freshness    `json:"freshness"`
	Capabilities      Capabilities `json:"capabilities"`
	SourceLabel       string       `json:"sourceLabel,omitempty"`
	Repository        string       `json:"repository,omitempty"`
	Branch            string       `json:"branch,omitempty"`
	ArtifactSHA256    string       `json:"artifactSha256,omitempty"`
	NativePipelineID  string       `json:"nativePipelineId,omitempty"`
	SourceUnavailable bool         `json:"sourceUnavailable,omitempty"`
}
type List[T any] struct {
	Items   []T            `json:"items"`
	Total   int            `json:"total"`
	Limit   int            `json:"limit"`
	Offset  int            `json:"offset"`
	Sources []SourceStatus `json:"sources"`
}
type AppDetail struct {
	App          App                          `json:"app"`
	Services     []ServiceRecord              `json:"services"`
	Deployments  []Deployment                 `json:"deployments"`
	Pipeline     *Pipeline                    `json:"pipeline,omitempty"`
	Sources      []SourceStatus               `json:"sources"`
	Integrations *vercel.IntegrationInventory `json:"integrations,omitempty"`
}
type PipelineDetail struct {
	Pipeline     Pipeline                     `json:"pipeline"`
	App          *App                         `json:"app,omitempty"`
	Services     []ServiceRecord              `json:"services"`
	Deployments  []Deployment                 `json:"deployments"`
	Sources      []SourceStatus               `json:"sources"`
	Integrations *vercel.IntegrationInventory `json:"integrations,omitempty"`
}
type Query struct {
	Search, Provider, Management, Environment, Status, AppID string
	Limit, Offset                                            int
}
