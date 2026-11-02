package vercel

import (
	"context"
	"strings"
	"time"
)

// These projections deliberately exclude provider env, build config, metadata,
// routes, credentials and logs. Unknown service inventory is not an empty one.
type ServiceDefinition struct {
	Name      string `json:"name"`
	Type      string `json:"type,omitempty"`
	Framework string `json:"framework,omitempty"`
	Runtime   string `json:"runtime,omitempty"`
}
type ProjectDetails struct {
	Repository             string              `json:"repository,omitempty"`
	ProductionBranch       string              `json:"productionBranch,omitempty"`
	ProductionDeploymentID string              `json:"productionDeploymentId,omitempty"`
	ProductionDeployment   *Deployment         `json:"productionDeployment,omitempty"`
	Services               []ServiceDefinition `json:"services"`
	ServicesKnown          bool                `json:"servicesKnown"`
	ObservedAt             time.Time           `json:"observedAt"`
	Error                  string              `json:"error,omitempty"`
	ownerID                string
}
type DeploymentDetails struct {
	AliasDetails  *DeploymentAliasDetails `json:"aliasDetails,omitempty"`
	Aliases       []string                `json:"aliases"`
	AliasAssigned bool                    `json:"aliasAssigned"`
	InspectorURL  string                  `json:"inspectorUrl,omitempty"`
	Services      []ServiceDefinition     `json:"services"`
	ServicesKnown bool                    `json:"servicesKnown"`
	// ApplicationObserved proves an identity-matched owner deployment response,
	// not complete component inventory or an ordinary single-service build mode.
	ApplicationObserved bool       `json:"applicationObserved,omitempty"`
	BuildingAt          *time.Time `json:"buildingAt,omitempty"`
	ReadyAt             *time.Time `json:"readyAt,omitempty"`
	ObservedAt          time.Time  `json:"observedAt"`
	Error               string     `json:"error,omitempty"`
	ownerID             string
}

// DetailsAPI is optional, so existing inventory adapters and publication mocks
// remain compatible. Deployment identity is returned for explicit validation.
type DetailsAPI interface {
	ProjectDetails(context.Context, string, string, string) (*ProjectDetails, error)
	DeploymentDetails(context.Context, string, string, string) (*Deployment, error)
}
type serviceWire struct {
	ServiceName string `json:"serviceName"`
	Name        string `json:"name"`
	ServiceType string `json:"serviceType"`
	Type        string `json:"type"`
	Framework   string `json:"framework"`
	Runtime     string `json:"runtime"`
	Schema      string `json:"schema"`
}

func serviceDefinitions(values *[]serviceWire, project bool) ([]ServiceDefinition, bool, error) {
	out := []ServiceDefinition{}
	if values == nil {
		return out, false, nil
	}
	if len(*values) > 200 {
		return nil, false, invalidResponse()
	}
	seen := map[string]bool{}
	for _, v := range *values {
		name, kind := v.Name, v.Type
		if project {
			name, kind = v.ServiceName, v.ServiceType
		} else if v.Schema != "" && v.Schema != "experimentalServices" && v.Schema != "experimentalServicesV2" {
			return nil, false, invalidResponse()
		}
		name = displayText(name, 256)
		if name == "" || seen[name] {
			return nil, false, invalidResponse()
		}
		seen[name] = true
		switch kind {
		case "web", "worker", "job", "cron":
		default:
			kind = ""
		}
		out = append(out, ServiceDefinition{Name: name, Type: kind, Framework: displayText(v.Framework, 128), Runtime: displayText(v.Runtime, 128)})
	}
	return out, true, nil
}

// ProjectDetails follows GET /v9/projects/{idOrName}; the production deployment
// ID comes only from targets.production.id, never newest READY or matching name.
// Schema source: https://openapi.vercel.sh/ (checked 2026-10-02).
func (c *Client) ProjectDetails(ctx context.Context, token, team, id string) (*ProjectDetails, error) {
	if err := validateScope(token, team); err != nil {
		return nil, err
	}
	if !validProviderID(id) {
		return nil, invalidResponse()
	}
	var raw struct {
		ID        string `json:"id"`
		AccountID string `json:"accountId"`
		Targets   struct {
			Production *struct {
				ID string `json:"id"`
			} `json:"production"`
		} `json:"targets"`
		Link     *struct{ Type, Org, Repo, Owner, Slug, ProjectNamespace, ProjectName, ProductionBranch string } `json:"link"`
		Services *[]serviceWire                                                                                  `json:"services"`
	}
	if err := c.get(ctx, token, "/v9/projects/"+id, scopeQuery(team), &raw); err != nil {
		return nil, err
	}
	if raw.ID != id || (team != "" && raw.AccountID != "" && raw.AccountID != team) {
		return nil, invalidResponse()
	}
	services, known, err := serviceDefinitions(raw.Services, true)
	if err != nil {
		return nil, err
	}
	out := &ProjectDetails{Services: services, ServicesKnown: known, ObservedAt: time.Now().UTC(), ownerID: raw.AccountID}
	if raw.Targets.Production != nil {
		if !validProviderID(raw.Targets.Production.ID) {
			return nil, invalidResponse()
		}
		out.ProductionDeploymentID = raw.Targets.Production.ID
	}
	if raw.Link != nil {
		link := raw.Link
		out.ProductionBranch = displayText(link.ProductionBranch, 512)
		owner, repo := link.Org, link.Repo
		switch link.Type {
		case "gitlab":
			owner, repo = link.ProjectNamespace, link.ProjectName
		case "bitbucket":
			owner, repo = link.Owner, link.Slug
		case "cursor-origin":
			owner = link.Owner
		}
		if safeRepositoryPart(owner) && safeRepositoryPart(repo) {
			out.Repository = owner + "/" + repo
		}
	}
	return out, nil
}
func safeRepositoryPart(s string) bool {
	if s == "" || len(s) > 256 || strings.HasPrefix(s, "/") || strings.HasSuffix(s, "/") {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./", r)) {
			return false
		}
	}
	for _, p := range strings.Split(s, "/") {
		if p == "" || p == "." || p == ".." {
			return false
		}
	}
	return true
}
func detailTime(ms int64) *time.Time {
	if ms <= 0 || ms > 253402300799999 {
		return nil
	}
	t := time.UnixMilli(ms).UTC()
	return &t
}

// DeploymentDetails handles the owner/public/reduced v13 union with a typed
// allowlist. Reduced responses cannot invent missing project/service identity.
func (c *Client) DeploymentDetails(ctx context.Context, token, team, id string) (*Deployment, error) {
	if err := validateScope(token, team); err != nil {
		return nil, err
	}
	if !validProviderID(id) {
		return nil, invalidResponse()
	}
	var raw struct {
		ID, ProjectID, OwnerID, Name, URL, ReadyState, Target, InspectorURL string
		Project                                                             *struct{ ID string } `json:"project"`
		ProjectSettings                                                     *struct {
			Framework *string `json:"framework"`
		} `json:"projectSettings"`
		CreatedAt, BuildingAt, Ready, ReadyAt int64
		Alias                                 []string       `json:"alias"`
		AliasAssigned                         bool           `json:"aliasAssigned"`
		Services                              *[]serviceWire `json:"services"`
		GitSource                             *gitSourceWire `json:"gitSource"`
	}
	if err := c.get(ctx, token, "/v13/deployments/"+id, scopeQuery(team), &raw); err != nil {
		return nil, err
	}
	if raw.ID != id || (team != "" && raw.OwnerID != "" && raw.OwnerID != team) {
		return nil, invalidResponse()
	}
	applicationObserved := validProviderID(raw.ProjectID) && validProviderID(raw.OwnerID) && raw.ProjectSettings != nil
	if raw.ProjectSettings != nil && raw.ProjectSettings.Framework != nil && *raw.ProjectSettings.Framework == "services" {
		// This positive service-mode signal cannot become an aggregate fallback
		// when its component metadata is unavailable. Other frameworks still do
		// not establish complete inventory or ordinary build mode.
		applicationObserved = false
	}
	if raw.Project != nil {
		if raw.ProjectID != "" && raw.ProjectID != raw.Project.ID {
			return nil, invalidResponse()
		}
		raw.ProjectID = raw.Project.ID
	}
	if !validProviderID(raw.ProjectID) {
		return nil, invalidResponse()
	}
	services, known, err := serviceDefinitions(raw.Services, false)
	if err != nil {
		return nil, err
	}
	details := &DeploymentDetails{Aliases: []string{}, AliasAssigned: raw.AliasAssigned, InspectorURL: deploymentURL(raw.InspectorURL), Services: services, ServicesKnown: known, ApplicationObserved: applicationObserved, BuildingAt: detailTime(raw.BuildingAt), ObservedAt: time.Now().UTC(), ownerID: raw.OwnerID}
	if len(raw.Alias) > 200 {
		return nil, invalidResponse()
	}
	seen := map[string]bool{}
	for _, alias := range raw.Alias {
		u := deploymentURL(alias)
		if u != "" && !seen[u] {
			details.Aliases = append(details.Aliases, u)
			seen[u] = true
		}
	}
	ready := raw.Ready
	if ready == 0 {
		ready = raw.ReadyAt
	}
	details.ReadyAt = detailTime(ready)
	out := &Deployment{ID: raw.ID, ProjectID: raw.ProjectID, Name: displayText(raw.Name, 256), URL: deploymentURL(raw.URL), State: deploymentState(raw.ReadyState), Target: displayText(raw.Target, 128), Details: details}
	out.GitBranch, out.GitCommitSHA = sourceGitIdentity(raw.GitSource)
	if out.Target == "" {
		out.Target = "preview"
	}
	if created := detailTime(raw.CreatedAt); created != nil {
		out.CreatedAt = *created
	}
	return out, nil
}

// gitSource's documented owner/public unions share only these safe fields.
// Repository URLs, credentials, commit author/email/message and raw metadata
// are intentionally not decoded. The short anonymous union has no project
// identity and remains rejected by DeploymentDetails above.
type gitSourceWire struct {
	Type string `json:"type"`
	Ref  string `json:"ref"`
	SHA  string `json:"sha"`
}

func sourceGitIdentity(source *gitSourceWire) (string, string) {
	if source == nil {
		return "", ""
	}
	switch source.Type {
	case "github", "github-custom-host", "github-limited", "gitlab", "bitbucket", "vercel", "cursor-origin", "custom":
		// Reuse the inventory's bounded display-ref and full hexadecimal SHA rules.
		return gitMetadata(map[string]string{"gitCommitRef": source.Ref, "gitCommitSha": source.SHA})
	default:
		return "", ""
	}
}

func cloneProjectDetails(p *ProjectDetails) *ProjectDetails {
	if p == nil {
		return nil
	}
	out := *p
	out.Services = append([]ServiceDefinition{}, p.Services...)
	if p.ProductionDeployment != nil {
		d := cloneDeployment(*p.ProductionDeployment)
		out.ProductionDeployment = &d
	}
	return &out
}
func cloneDeployment(d Deployment) Deployment {
	if d.Details != nil {
		v := *d.Details
		v.Aliases = append([]string{}, v.Aliases...)
		v.AliasDetails = cloneAliasDetails(v.AliasDetails)
		v.Services = append([]ServiceDefinition{}, v.Services...)
		if v.BuildingAt != nil {
			t := *v.BuildingAt
			v.BuildingAt = &t
		}
		if v.ReadyAt != nil {
			t := *v.ReadyAt
			v.ReadyAt = &t
		}
		d.Details = &v
	}
	return d
}
func cloneSnapshot(s Snapshot) Snapshot {
	out := Snapshot{Projects: append([]Project{}, s.Projects...), Deployments: append([]Deployment{}, s.Deployments...), Coverage: cloneCoverage(s.Coverage), Integrations: cloneIntegrationInventory(s.Integrations)}
	for i := range out.Projects {
		out.Projects[i].Details = cloneProjectDetails(out.Projects[i].Details)
	}
	for i := range out.Deployments {
		out.Deployments[i] = cloneDeployment(out.Deployments[i])
	}
	return out
}

func cloneCoverage(c *ProjectCoverage) *ProjectCoverage {
	if c == nil || c.ObservedAt.IsZero() || c.AccessibleProjects < 0 || c.SelectedProjects < 0 || c.SelectedProjects > c.AccessibleProjects {
		return nil
	}
	v := *c
	return &v
}
