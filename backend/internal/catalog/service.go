package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/provider/vercel"
	"github.com/santapong/cooker/internal/store"
)

var ErrNotFound = errors.New("catalog resource not found")
var ErrUnavailable = errors.New("catalog source is temporarily unavailable")

type VercelReader interface {
	View(context.Context) (*vercel.View, error)
}
type PublicationReader interface {
	List(context.Context) ([]*vercel.Publication, error)
}
type scopedPublicationReader interface {
	VerifiedForDeployments(context.Context, []string) ([]*vercel.Publication, error)
}
type Service struct {
	Store        *store.Store
	Vercel       VercelReader
	Publications PublicationReader
	Now          func() time.Time
}
type snapshot struct {
	apps         []App
	services     []ServiceRecord
	pipelines    []Pipeline
	history      map[string][]Deployment
	integrations map[string]*vercel.IntegrationInventory
	sources      []SourceStatus
}

func stableID(prefix string, parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return prefix + hex.EncodeToString(h[:])
}
func observed(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	v := t.UTC()
	return &v
}
func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func (s *Service) read(ctx context.Context, write bool, q Query, id, mode string) *snapshot {
	out := &snapshot{apps: []App{}, services: []ServiceRecord{}, pipelines: []Pipeline{}, history: map[string][]Deployment{}, integrations: map[string]*vercel.IntegrationInventory{}, sources: []SourceStatus{}}
	if q.Management != "vercel" && q.Provider != "vercel" && !strings.HasPrefix(id, "vapp_") && !strings.HasPrefix(id, "vflow_") {
		s.native(ctx, out, write, q, id, mode)
	}
	if q.Management != "cooker" && (q.Provider == "" || q.Provider == "all" || q.Provider == "vercel") && (id == "" || strings.HasPrefix(id, "vapp_") || strings.HasPrefix(id, "vflow_")) {
		s.imported(ctx, out)
	}
	sort.Slice(out.apps, func(i, j int) bool {
		a, b := strings.ToLower(out.apps[i].Name), strings.ToLower(out.apps[j].Name)
		if a == b {
			return out.apps[i].ID < out.apps[j].ID
		}
		return a < b
	})
	sort.Slice(out.services, func(i, j int) bool {
		a, b := strings.ToLower(out.services[i].AppName+"/"+out.services[i].Name), strings.ToLower(out.services[j].AppName+"/"+out.services[j].Name)
		if a == b {
			return out.services[i].ID < out.services[j].ID
		}
		return a < b
	})
	sort.Slice(out.pipelines, func(i, j int) bool {
		a, b := strings.ToLower(out.pipelines[i].Name), strings.ToLower(out.pipelines[j].Name)
		if a == b {
			return out.pipelines[i].ID < out.pipelines[j].ID
		}
		return a < b
	})
	return out
}
func nativeHref(appID string, d *model.AppDeploy) string {
	if d.PipelineID != "" && d.RunID != "" {
		return "/apps/" + url.PathEscape(appID) + "/deployments/" + url.PathEscape(d.PipelineID) + "/" + url.PathEscape(d.RunID)
	}
	return "/apps/" + url.PathEscape(appID)
}
func (s *Service) native(ctx context.Context, out *snapshot, write bool, q Query, id, mode string) {
	status := SourceStatus{Provider: "cooker", Available: true}
	if s.Store == nil || s.Store.Apps == nil || s.Store.Pipelines == nil {
		out.sources = append(out.sources, SourceStatus{Provider: "cooker", Available: false, Error: "Cooker inventory is unavailable"})
		return
	}
	var apps []*model.App
	var appErr error
	if id != "" && mode == "app" {
		if a, err := s.Store.Apps.Get(ctx, id); err == nil {
			apps = []*model.App{a}
		} else if !errors.Is(err, store.ErrNotFound) {
			appErr = err
		}
	} else {
		apps, appErr = s.Store.Apps.List(ctx, 0, 0)
	}
	if appErr != nil {
		status.Available = false
		status.Error = "Cooker application inventory could not be read"
	}
	if q.AppID != "" || mode == "apps" {
		filtered := []*model.App{}
		for _, a := range apps {
			if q.AppID != "" && a.ID != q.AppID {
				continue
			}
			if mode == "apps" && (!contains(a.Name+" "+a.GitHubRepo, q.Search) || !match(string(a.DeployTarget.Kind), q.Provider)) {
				continue
			}
			filtered = append(filtered, a)
		}
		apps = filtered
	}
	// Minimal bulk projections avoid reading every generated DAG, run/log blob,
	// and full history just to count or list apps.
	appIDs := []string{}
	for _, a := range apps {
		appIDs = append(appIDs, a.ID)
	}
	latest := map[string]*model.AppDeploy{}
	if s.Store.AppDeploys != nil {
		if reader, ok := s.Store.AppDeploys.(store.CatalogAppDeployReader); ok {
			var err error
			latest, err = reader.LatestByApps(ctx, appIDs)
			if err != nil {
				status.Available = false
				status.Error = "Some Cooker deployment summaries could not be read"
			}
		} else {
			for _, a := range apps {
				if rows, err := s.Store.AppDeploys.ListByApp(ctx, a.ID, 1); err == nil && len(rows) > 0 {
					latest[a.ID] = rows[0]
				} else if err != nil {
					status.Available = false
					status.Error = "Some Cooker deployment summaries could not be read"
				}
			}
		}
	}
	runtimeAttempts := map[string]*model.AppDeploy{}
	if (mode == "services" || mode == "app" || mode == "pipeline") && s.Store.AppDeploys != nil {
		var err error
		runtimeAttempts, err = store.LatestRuntimeAttempts(ctx, s.Store.AppDeploys, appIDs)
		if err != nil {
			status.Available = false
			status.Error = "Some Cooker runtime deployment summaries could not be read"
		}
	}
	envNames := map[string]string{}
	environmentName := func(key string) string {
		if key == "" {
			return "unclassified"
		}
		if name, ok := envNames[key]; ok {
			return name
		}
		if s.Store.Environments != nil {
			if e, err := s.Store.Environments.Get(ctx, key); err == nil {
				envNames[key] = e.Name
				return e.Name
			}
		}
		return "unclassified"
	}
	pipelineCache := map[string]*model.Pipeline{}
	recordedPipeline := func(key string) *model.Pipeline {
		if key == "" {
			return nil
		}
		if p, ok := pipelineCache[key]; ok {
			return p
		}
		p, err := s.Store.Pipelines.Get(ctx, key)
		if err != nil {
			pipelineCache[key] = nil
			return nil
		}
		pipelineCache[key] = p
		return p
	}
	nativeOwners := map[string]App{}
	for _, a := range apps {
		ca := App{ID: a.ID, NativeAppID: a.ID, Name: a.Name, Provider: string(a.DeployTarget.Kind), Management: "cooker", Href: "/apps/" + url.PathEscape(a.ID), Repository: a.GitHubRepo, Branch: a.Branch, Health: string(a.HealthStatus), Environment: environmentName(a.EnvironmentID), Freshness: Freshness{State: "recorded", ObservedAt: observed(a.UpdatedAt)}, Capabilities: Capabilities{Edit: write, Deploy: write, Inspect: true}, SourceLabel: "Cooker source configuration"}
		if ca.Health == "" {
			ca.Health = "unknown"
		}
		history := []*model.AppDeploy{}
		if d := latest[a.ID]; d != nil {
			history = append(history, d)
		}
		if id == a.ID && mode == "app" && s.Store.AppDeploys != nil {
			if rows, err := s.Store.AppDeploys.ListByApp(ctx, a.ID, 100); err == nil {
				history = rows
			} else {
				ca.SourceUnavailable = true
			}
		}
		out.history[a.ID] = []Deployment{}
		for _, d := range history {
			out.history[a.ID] = append(out.history[a.ID], Deployment{ID: d.ID, Provider: "cooker", Status: string(d.Status), Environment: "unclassified", CreatedAt: d.CreatedAt, ProductionURLs: []string{}, Href: nativeHref(a.ID, d)})
		}
		if len(history) > 0 {
			ca.PipelineID = history[0].PipelineID
			ca.LatestDeployment = &out.history[a.ID][0]
			if d := runtimeAttempts[a.ID]; d != nil {
				p := recordedPipeline(d.PipelineID)
				if p == nil {
					ca.SourceUnavailable = true
				} else {
					states := map[string]string{}
					if s.Store.Runs != nil && d.RunID != "" {
						if r, err := s.Store.Runs.GetSummary(ctx, d.RunID); err == nil {
							for _, sr := range r.StageRuns {
								states[sr.StageID] = string(sr.Status)
							}
						}
					}
					for _, st := range p.Stages {
						if st.Type != model.StageTypeDeploy {
							continue
						}
						key := st.Config.ComposeServiceName
						if key == "" {
							key = st.ID
						}
						name := st.Config.ComposeServiceName
						if name == "" {
							name = st.Name
						}
						state := states[st.ID]
						if state == "" {
							state = "unknown"
						}
						env := environmentName(st.EnvironmentID)
						provider := recordedProvider(st.Config.DeployRuntime)
						out.services = append(out.services, ServiceRecord{ID: stableID("svc_", a.ID, env, key), AppID: a.ID, AppName: a.Name, AppHref: ca.Href, Href: ca.Href, Name: name, NativeServiceID: key, Provider: provider, Management: "cooker", Environment: env, Status: state, Health: "unknown", Runtime: st.Config.DeployRuntime, Definition: "recorded", DeploymentID: d.ID, DeploymentHref: nativeHref(a.ID, d), ObservedAt: observed(d.CreatedAt)})
					}
				}
			}
			nativeOwners[history[0].PipelineID] = ca
		}
		if ca.SourceUnavailable {
			ca.Freshness.Error = "Recorded deployment details are unavailable"
		}
		out.apps = append(out.apps, ca)
	}
	// Only pipeline requests require the complete list of native definitions.
	// Implementations supply a minimal SQL/memory summary with latest run joined.
	if mode == "pipelines" || mode == "pipeline" {
		summaries := []store.CatalogPipelineSummary{}
		if reader, ok := s.Store.Pipelines.(store.CatalogPipelineReader); ok {
			var err error
			summaries, err = reader.ListCatalog(ctx)
			if err != nil {
				status.Available = false
				status.Error = "Cooker pipeline summaries could not be read"
			}
		} else {
			if ps, err := s.Store.Pipelines.List(ctx, 0, 0); err == nil {
				for _, p := range ps {
					summary := store.CatalogPipelineSummary{ID: p.ID, Name: p.Name, StageCount: len(p.Stages), UpdatedAt: p.UpdatedAt}
					if s.Store.Runs != nil {
						if rs, err := s.Store.Runs.List(ctx, p.ID, 1, 0); err == nil && len(rs) > 0 {
							summary.LatestRun = rs[0]
						}
					}
					summaries = append(summaries, summary)
				}
			} else {
				status.Available = false
				status.Error = "Cooker pipeline inventory could not be read"
			}
		}
		for _, p := range summaries {
			if id != "" && p.ID != id {
				continue
			}
			cp := Pipeline{ID: p.ID, NativePipelineID: p.ID, Name: p.Name, Provider: "cooker", Management: "cooker", Kind: "cooker-dag", Href: "/pipelines/" + url.PathEscape(p.ID) + "/edit", Stages: p.StageCount, Freshness: Freshness{State: "recorded", ObservedAt: observed(p.UpdatedAt)}, Capabilities: Capabilities{Edit: write, Deploy: write, Inspect: true}}
			if a, ok := nativeOwners[p.ID]; ok {
				cp.AppID = a.ID
				cp.AppName = a.Name
				cp.LatestDeployment = a.LatestDeployment
			}
			if r := p.LatestRun; r != nil {
				cp.LatestDeployment = &Deployment{ID: r.ID, Provider: "cooker", Status: string(r.Status), Environment: "unclassified", CreatedAt: r.CreatedAt, ProductionURLs: []string{}, Href: "/pipelines/" + url.PathEscape(p.ID) + "/runs/" + url.PathEscape(r.ID)}
			}
			out.pipelines = append(out.pipelines, cp)
		}
	} else if id != "" && mode == "app" {
		for _, a := range out.apps {
			if p := recordedPipeline(a.PipelineID); p != nil {
				out.pipelines = append(out.pipelines, Pipeline{ID: p.ID, NativePipelineID: p.ID, Name: p.Name, Provider: "cooker", Management: "cooker", Kind: "cooker-dag", Href: "/pipelines/" + url.PathEscape(p.ID) + "/edit", AppID: a.ID, AppName: a.Name, Stages: len(p.Stages), Freshness: a.Freshness, Capabilities: Capabilities{Edit: write, Deploy: write, Inspect: true}})
			}
		}
	}
	out.sources = append(out.sources, status)
}
func recordedProvider(runtime string) string {
	switch runtime {
	case "docker", "compose":
		return "docker-host"
	case "kubernetes":
		return "kubernetes"
	case "ecs":
		return "ecs"
	case "cloud-run":
		return "cloud-run"
	default:
		return "cooker"
	}
}

func providerURL(raw string, dashboard bool) string { return vercel.SafeDisplayURL(raw, dashboard) }

func deployment(d vercel.Deployment, productionID string) Deployment {
	env := d.Target
	if d.ID == productionID && productionID != "" {
		env = "production"
	} else if env == "" {
		env = "preview"
	}
	cd := Deployment{ID: d.ID, Provider: "vercel", Status: d.State, Environment: env, CreatedAt: d.CreatedAt, GitBranch: d.GitBranch, GitCommitSHA: d.GitCommitSHA, URL: providerURL(d.URL, false), ProductionURLs: []string{}}
	if cd.Status == "" {
		cd.Status = "unknown"
	}
	if d.Details != nil {
		f := Freshness{State: "up_to_date", ObservedAt: observed(d.Details.ObservedAt), Error: d.Details.Error}
		if f.ObservedAt == nil {
			f.State = "unavailable"
		} else if f.Error != "" {
			f.State = "stale"
		}
		cd.MetadataFreshness = &f
		cd.InspectorURL = providerURL(d.Details.InspectorURL, true)
		cd.BuildingAt = d.Details.BuildingAt
		cd.ReadyAt = d.Details.ReadyAt
		aliases := d.Details.Aliases
		assigned := d.Details.AliasAssigned
		if d.ID == productionID && d.Details.AliasDetails != nil {
			observation := d.Details.AliasDetails
			f := Freshness{State: "up_to_date", ObservedAt: observed(observation.ObservedAt), Error: observation.Error}
			if observation.DeploymentID != d.ID {
				f = Freshness{State: "unavailable", Error: "Production alias observation does not match this deployment"}
			} else if f.ObservedAt == nil {
				f.State = "unavailable"
			} else if f.Error != "" {
				f.State = "stale"
			}
			cd.AliasFreshness = &f
			// An explicit empty observation clears legacy aliases. Failed reads use
			// only the retained observation for this exact deployment, on its clock.
			aliases = observation.Aliases
			assigned = observation.DeploymentID == d.ID && f.ObservedAt != nil
		}
		if d.ID == productionID && assigned {
			seen := map[string]bool{}
			for _, alias := range aliases {
				if link := providerURL(alias, false); link != "" && !seen[link] {
					seen[link] = true
					cd.ProductionURLs = append(cd.ProductionURLs, link)
				}
			}
			sort.Strings(cd.ProductionURLs)
		}
	}
	return cd
}
func connectionFreshness(c *vercel.Connection, now time.Time) Freshness {
	f := Freshness{State: "up_to_date", ObservedAt: c.LastSuccessAt, Error: c.SyncError}
	switch {
	case !c.Enabled:
		f.State = "paused"
	case c.SyncError != "":
		f.State = "failed"
	case c.LastSuccessAt == nil:
		f.State = "never_synced"
	case c.Syncing:
		f.State = "syncing"
	case now.Sub(*c.LastSuccessAt) > 3*vercel.SyncInterval:
		f.State = "stale"
	}
	return f
}
func (s *Service) imported(ctx context.Context, out *snapshot) {
	status := SourceStatus{Provider: "vercel", Available: true}
	if s.Vercel == nil {
		status.Available = false
		status.Error = "Vercel integration is not configured"
		out.sources = append(out.sources, status)
		return
	}
	v, err := s.Vercel.View(ctx)
	if err != nil || v == nil {
		status.Available = false
		status.Error = "Vercel inventory is unavailable; native Cooker resources remain separate"
		out.sources = append(out.sources, status)
		return
	}
	connections := map[string]*vercel.Connection{}
	selectedProject := func(c *vercel.Connection, projectID string) bool {
		if c == nil {
			return false
		}
		for _, selected := range c.ProjectIDs {
			if selected == projectID {
				return true
			}
		}
		return false
	}
	for _, c := range v.Connections {
		if c == nil {
			continue
		}
		connections[c.ID] = c
		if c.SyncError != "" {
			status.Available = false
			status.Error = "Some Vercel connections could not refresh; last-known data is retained"
		}
	}
	type choice struct {
		project    vercel.ImportedProject
		connection *vercel.Connection
	}
	chosen := map[string]choice{}
	projectDetails := map[string]*vercel.ProjectDetails{}
	integrationDetails := map[string]integrationChoice{}
	deploymentDetails := map[string]*vercel.DeploymentDetails{}
	aliasDetails := map[string]*vercel.DeploymentAliasDetails{}
	collectDeployment := func(c *vercel.Connection, d vercel.Deployment) {
		if !selectedProject(c, d.ProjectID) || d.Details == nil {
			return
		}
		key := stableID("d_", c.Identity.Scope, c.Identity.ID, d.ProjectID, d.ID)
		old := deploymentDetails[key]
		if old == nil || d.Details.ObservedAt.After(old.ObservedAt) || (d.Details.ObservedAt.Equal(old.ObservedAt) && old.Error != "" && d.Details.Error == "") {
			deploymentDetails[key] = d.Details
		}
		if detail := d.Details.AliasDetails; detail != nil && detail.DeploymentID == d.ID {
			old := aliasDetails[key]
			if old == nil || detail.ObservedAt.After(old.ObservedAt) || (detail.ObservedAt.Equal(old.ObservedAt) && old.Error != "" && detail.Error == "") {
				aliasDetails[key] = detail
			}
		}
	}
	for _, d := range v.Deployments {
		collectDeployment(connections[d.ConnectionID], d.Deployment)
	}
	for _, p := range v.Projects {
		if p.Details != nil && p.Details.ProductionDeployment != nil {
			d := p.Details.ProductionDeployment
			if d.ProjectID == p.ID && d.ID == p.Details.ProductionDeploymentID {
				collectDeployment(connections[p.ConnectionID], *d)
			}
		}
	}
	mergeDeploymentDetails := func(c *vercel.Connection, d *vercel.Deployment) {
		key := stableID("d_", c.Identity.Scope, c.Identity.ID, d.ProjectID, d.ID)
		detail := deploymentDetails[key]
		if detail == nil {
			detail = d.Details
		}
		if detail != nil {
			cp := *detail
			if alias := aliasDetails[key]; alias != nil {
				ac := *alias
				ac.Aliases = append([]string{}, alias.Aliases...)
				cp.AliasDetails = &ac
			}
			d.Details = &cp
		}
	}
	for _, p := range v.Projects {
		c := connections[p.ConnectionID]
		if c == nil {
			continue
		}
		selected := false
		for _, id := range c.ProjectIDs {
			if id == p.ID {
				selected = true
				break
			}
		}
		if !selected {
			continue
		}
		key := stableID("vapp_", c.Identity.Scope, c.Identity.ID, p.ID)
		if inventory := projectIntegrationInventory(c, p.ID); inventory != nil {
			candidate := integrationChoice{connectionID: c.ID, inventory: inventory}
			if preferIntegrationInventory(candidate, integrationDetails[key]) {
				integrationDetails[key] = candidate
			}
		}
		if detail := p.Details; detail != nil {
			old := projectDetails[key]
			if old == nil || detail.ObservedAt.After(old.ObservedAt) || (detail.ObservedAt.Equal(old.ObservedAt) && old.Error != "" && detail.Error == "") {
				projectDetails[key] = detail
			}
		}
		old, exists := chosen[key]
		newer := !exists || (c.LastSuccessAt != nil && (old.connection.LastSuccessAt == nil || c.LastSuccessAt.After(*old.connection.LastSuccessAt)))
		equal := exists && ((c.LastSuccessAt == nil && old.connection.LastSuccessAt == nil) || (c.LastSuccessAt != nil && old.connection.LastSuccessAt != nil && c.LastSuccessAt.Equal(*old.connection.LastSuccessAt)))
		if newer || (equal && c.ID < old.connection.ID) {
			chosen[key] = choice{p, c}
		}
	}
	publications := []*vercel.Publication{}
	if s.Publications != nil {
		ids := []string{}
		for _, d := range v.Deployments {
			ids = append(ids, d.ID)
		}
		for _, p := range projectDetails {
			if p.ProductionDeploymentID != "" {
				ids = append(ids, p.ProductionDeploymentID)
			}
		}
		var ops []*vercel.Publication
		var err error
		if reader, ok := s.Publications.(scopedPublicationReader); ok {
			ops, err = reader.VerifiedForDeployments(ctx, ids)
		} else {
			ops, err = s.Publications.List(ctx)
		}
		if err == nil {
			publications = ops
		} else {
			status.Available = false
			status.Error = "Vercel publication source details are unavailable"
		}
	}
	for id, entry := range chosen {
		p, c := entry.project, entry.connection
		out.integrations[id] = integrationDetails[id].inventory
		// Core history and optional observations have independent clocks. Never
		// discard a fresher identity-matching production binding on another token.
		if detail := projectDetails[id]; detail != nil {
			cp := *detail
			p.Details = &cp
		}
		ca := App{ID: id, Name: p.Name, Provider: "vercel", Management: "vercel", Href: "/apps/synced/" + id, PipelineID: strings.Replace(id, "vapp_", "vflow_", 1), Framework: p.Framework, Health: "unknown", Freshness: connectionFreshness(c, s.now()), Capabilities: Capabilities{Inspect: true}, SourceLabel: "Source not reported"}
		productionID := ""
		if p.Details != nil {
			metadata := Freshness{State: "up_to_date", ObservedAt: observed(p.Details.ObservedAt), Error: p.Details.Error}
			if metadata.ObservedAt == nil {
				metadata.State = "unavailable"
			} else if metadata.Error != "" {
				metadata.State = "stale"
			}
			ca.MetadataFreshness = &metadata
			productionID = p.Details.ProductionDeploymentID
			ca.Repository = p.Details.Repository
			ca.Branch = p.Details.ProductionBranch
			if ca.Repository != "" {
				ca.SourceLabel = "Connected repository"
			}
			if p.Details.Error != "" {
				ca.SourceUnavailable = true
				ca.Freshness.Error = p.Details.Error
				if ca.Freshness.State == "up_to_date" {
					ca.Freshness.State = "stale"
				}
				status.Available = false
				status.Error = "Some Vercel project metadata is last-known; refresh details are unavailable"
			}
		}
		history := []vercel.Deployment{}
		for _, d := range v.Deployments {
			if d.ConnectionID == c.ID && d.ProjectID == p.ID {
				history = append(history, d.Deployment)
			}
		}
		sort.Slice(history, func(i, j int) bool {
			if history[i].CreatedAt.Equal(history[j].CreatedAt) {
				return history[i].ID < history[j].ID
			}
			return history[i].CreatedAt.After(history[j].CreatedAt)
		})
		var prod, preview *vercel.Deployment
		out.history[id] = []Deployment{}
		for i := range history {
			d := &history[i]
			mergeDeploymentDetails(c, d)
			out.history[id] = append(out.history[id], deployment(*d, productionID))
			if d.ID == productionID {
				prod = d
			}
			if preview == nil && d.ID != productionID && (d.Target == "" || d.Target == "preview") {
				preview = d
			}
		}
		if p.Details != nil && p.Details.ProductionDeployment != nil && p.Details.ProductionDeployment.ID == productionID && p.Details.ProductionDeployment.ProjectID == p.ID {
			nested := p.Details.ProductionDeployment
			if prod == nil {
				cp := *nested
				prod = &cp
			} else if nested.Details != nil && (prod.Details == nil || nested.Details.ObservedAt.After(prod.Details.ObservedAt)) {
				prod.Details = nested.Details
			}
			if detail := deploymentDetails[stableID("d_", c.Identity.Scope, c.Identity.ID, p.ID, productionID)]; detail != nil && (prod.Details == nil || detail.ObservedAt.After(prod.Details.ObservedAt)) {
				prod.Details = detail
			}
		}
		if prod != nil {
			mergeDeploymentDetails(c, prod)
			for i := range out.history[id] {
				if out.history[id][i].ID == prod.ID {
					out.history[id][i] = deployment(*prod, productionID)
				}
			}
		}
		if len(out.history[id]) > 0 {
			cp := out.history[id][0]
			ca.LatestDeployment = &cp
		}
		if prod != nil {
			cp := deployment(*prod, productionID)
			ca.ProductionDeployment = &cp
		}
		for _, d := range []*vercel.Deployment{prod, preview} {
			if d != nil && d.Details != nil && d.Details.Error != "" {
				ca.SourceUnavailable = true
				ca.Freshness.Error = d.Details.Error
				if ca.Freshness.State == "up_to_date" {
					ca.Freshness.State = "stale"
				}
				status.Available = false
				status.Error = "Some Vercel deployment metadata is last-known; refresh details are unavailable"
			}
		}
		if prod != nil && prod.Details != nil && prod.Details.AliasDetails != nil && prod.Details.AliasDetails.Error != "" {
			ca.SourceUnavailable = true
			ca.Freshness.Error = prod.Details.AliasDetails.Error
			if ca.Freshness.State == "up_to_date" {
				ca.Freshness.State = "stale"
			}
			status.Available = false
			status.Error = "Some Vercel production aliases are last-known; refresh details are unavailable"
		}
		for _, op := range publications {
			if op.Status == "succeeded" && op.AccountID == c.Identity.ID && op.AccountScope == c.Identity.Scope && op.ProjectID == p.ID && op.DeploymentID != "" && op.ArtifactSHA256 != "" {
				for i := range out.history[id] {
					if out.history[id][i].ID == op.DeploymentID {
						out.history[id][i].SourceLabel = "Uploaded Cooker landing bundle"
						out.history[id][i].ArtifactSHA256 = op.ArtifactSHA256
					}
				}
				if ca.LatestDeployment != nil && ca.LatestDeployment.ID == op.DeploymentID {
					ca.LatestDeployment.SourceLabel = "Uploaded Cooker landing bundle"
					ca.LatestDeployment.ArtifactSHA256 = op.ArtifactSHA256
				}
				if ca.ProductionDeployment != nil && ca.ProductionDeployment.ID == op.DeploymentID {
					ca.ProductionDeployment.SourceLabel = "Uploaded Cooker landing bundle"
					ca.ProductionDeployment.ArtifactSHA256 = op.ArtifactSHA256
				}
			}
		}
		preferred := ca.ProductionDeployment
		if preferred == nil {
			preferred = ca.LatestDeployment
		}
		if preferred != nil && preferred.ArtifactSHA256 != "" {
			ca.SourceLabel = preferred.SourceLabel
			ca.ArtifactSHA256 = preferred.ArtifactSHA256
		}
		for _, pair := range []struct {
			env string
			d   *vercel.Deployment
		}{{"production", prod}, {"preview", preview}} {
			out.services = append(out.services, projectServices(ca, p, pair.env, pair.d)...)
		}
		cp := Pipeline{ID: ca.PipelineID, Name: ca.Name, Provider: "vercel", Management: "vercel", Kind: "provider-flow", Href: "/pipelines/synced/" + ca.PipelineID, AppID: ca.ID, AppName: ca.Name, LatestDeployment: ca.LatestDeployment, Freshness: ca.Freshness, Capabilities: ca.Capabilities, SourceLabel: ca.SourceLabel, Repository: ca.Repository, Branch: ca.Branch, ArtifactSHA256: ca.ArtifactSHA256, SourceUnavailable: ca.SourceUnavailable}
		out.apps = append(out.apps, ca)
		out.pipelines = append(out.pipelines, cp)
	}
	out.sources = append(out.sources, status)
}
func projectServices(a App, p vercel.ImportedProject, env string, d *vercel.Deployment) []ServiceRecord {
	if env == "preview" && d == nil {
		return nil
	}
	definitions := []vercel.ServiceDefinition{}
	kind := "unavailable"
	known := false
	var at *time.Time
	message := "Service details unavailable"
	if d != nil && d.Details != nil && d.Details.ServicesKnown {
		definitions = d.Details.Services
		known = true
		kind = "deployed"
		at = observed(d.Details.ObservedAt)
		message = d.Details.Error
	}
	if !known && p.Details != nil && p.Details.ServicesKnown {
		definitions = p.Details.Services
		known = true
		kind = "configured"
		at = observed(p.Details.ObservedAt)
		message = p.Details.Error
		if len(definitions) == 0 && d != nil && p.Details.Error == "" {
			kind = "application"
		}
	}
	if known && len(definitions) == 0 {
		definitions = []vercel.ServiceDefinition{{Name: "Application", Framework: p.Framework}}
		if kind == "deployed" {
			kind = "application"
		}
	}
	if !known {
		definitions = []vercel.ServiceDefinition{{Name: "Service details unavailable"}}
		if d != nil && d.Details != nil && d.Details.ApplicationObserved {
			definitions = []vercel.ServiceDefinition{{Name: "Application"}}
			kind = "aggregate"
			message = "This is the application deployment. Vercel did not report its detailed component inventory."
			at = observed(d.Details.ObservedAt)
		}
	}
	// The bounded publication receipt proves this exact deployment's static
	// component even when older provider APIs omit their service metadata.
	if !known && d != nil && a.ProductionDeployment != nil && a.ProductionDeployment.ID == d.ID && a.ProductionDeployment.ArtifactSHA256 != "" {
		definitions = []vercel.ServiceDefinition{{Name: "Website", Type: "static", Runtime: "static"}}
		kind = "application"
		known = true
		message = ""
		at = observed(d.CreatedAt)
	}
	out := []ServiceRecord{}
	for _, def := range definitions {
		name := def.Name
		if name == "" {
			name = "Application"
		}
		r := ServiceRecord{ID: stableID("svc_", a.ID, env, name), AppID: a.ID, AppName: a.Name, AppHref: a.Href, Href: a.Href + "?tab=services", Name: name, Provider: "vercel", Management: "vercel", Environment: env, Status: "unknown", Health: "unknown", Framework: def.Framework, Runtime: def.Runtime, Definition: kind, ObservedAt: at, Error: message}
		if kind == "configured" {
			r.Status = "configured"
		}
		if d != nil {
			r.DeploymentID = d.ID
			r.DeploymentHref = "/pipelines/synced/" + a.PipelineID + "?deployment=" + url.QueryEscape(d.ID)
			if d.Details != nil {
				r.InspectorURL = providerURL(d.Details.InspectorURL, true)
				if d.Details.Error != "" {
					if kind == "aggregate" {
						r.Error = message + " " + d.Details.Error
					} else {
						r.Error = d.Details.Error
					}
				}
			}
			if kind == "deployed" || kind == "application" || kind == "aggregate" {
				r.Status = d.State
				if r.Status == "" {
					r.Status = "unknown"
				}
			}
		}
		out = append(out, r)
	}
	return out
}

func contains(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(strings.TrimSpace(needle)))
}
func match(value, filter string) bool {
	return filter == "" || filter == "all" || strings.EqualFold(value, filter)
}
func serviceEnvironment(r ServiceRecord, filter string) bool {
	if filter == "production_first" {
		return r.Management == "cooker" || r.Environment == "production"
	}
	return match(r.Environment, filter)
}
func page[T any](items []T, sources []SourceStatus, q Query) List[T] {
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	total := len(items)
	start := offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	return List[T]{Items: append([]T{}, items[start:end]...), Total: total, Limit: limit, Offset: offset, Sources: sources}
}
func (s *Service) Apps(ctx context.Context, q Query, write bool) List[App] {
	v := s.read(ctx, write, q, "", "apps")
	items := []App{}
	for _, a := range v.apps {
		state := "unknown"
		if a.Management == "vercel" && a.ProductionDeployment != nil {
			state = a.ProductionDeployment.Status
		} else if a.Management != "vercel" && a.LatestDeployment != nil {
			state = a.LatestDeployment.Status
		}
		if contains(a.Name+" "+a.Repository, q.Search) && match(a.Provider, q.Provider) && match(a.Management, q.Management) && match(a.Environment, q.Environment) && match(state, q.Status) {
			items = append(items, a)
		}
	}
	return page(items, v.sources, q)
}
func (s *Service) Services(ctx context.Context, q Query, write bool) List[ServiceRecord] {
	v := s.read(ctx, write, q, "", "services")
	items := []ServiceRecord{}
	for _, r := range v.services {
		if contains(r.AppName+" "+r.Name+" "+r.Framework+" "+r.Runtime, q.Search) && match(r.Provider, q.Provider) && match(r.Management, q.Management) && serviceEnvironment(r, q.Environment) && match(r.Status, q.Status) && (q.AppID == "" || r.AppID == q.AppID) {
			items = append(items, r)
		}
	}
	return page(items, v.sources, q)
}
func (s *Service) Pipelines(ctx context.Context, q Query, write bool) List[Pipeline] {
	v := s.read(ctx, write, q, "", "pipelines")
	items := []Pipeline{}
	for _, p := range v.pipelines {
		state := "unknown"
		if p.LatestDeployment != nil {
			state = p.LatestDeployment.Status
		}
		if contains(p.Name+" "+p.AppName+" "+p.Repository, q.Search) && match(p.Provider, q.Provider) && match(p.Management, q.Management) && match(state, q.Status) && (q.AppID == "" || p.AppID == q.AppID) {
			items = append(items, p)
		}
	}
	return page(items, v.sources, q)
}
func details(v *snapshot, a App) AppDetail {
	d := AppDetail{App: a, Services: []ServiceRecord{}, Deployments: append([]Deployment{}, v.history[a.ID]...), Sources: v.sources, Integrations: cloneIntegrationInventory(v.integrations[a.ID])}
	for _, r := range v.services {
		if r.AppID == a.ID {
			d.Services = append(d.Services, r)
		}
	}
	for _, p := range v.pipelines {
		if p.ID == a.PipelineID {
			cp := p
			d.Pipeline = &cp
			break
		}
	}
	return d
}
func (s *Service) App(ctx context.Context, id string, write bool) (AppDetail, error) {
	v := s.read(ctx, write, Query{}, id, "app")
	for _, a := range v.apps {
		if a.ID == id {
			return details(v, a), nil
		}
	}
	return AppDetail{}, missing(v, id)
}
func (s *Service) Pipeline(ctx context.Context, id string, write bool) (PipelineDetail, error) {
	v := s.read(ctx, write, Query{}, id, "pipeline")
	for _, p := range v.pipelines {
		if p.ID == id {
			d := PipelineDetail{Pipeline: p, Services: []ServiceRecord{}, Deployments: []Deployment{}, Sources: v.sources}
			for _, a := range v.apps {
				if a.ID == p.AppID {
					ad := details(v, a)
					d.App = &ad.App
					d.Services = ad.Services
					d.Deployments = ad.Deployments
					d.Integrations = ad.Integrations
					break
				}
			}
			return d, nil
		}
	}
	return PipelineDetail{}, missing(v, id)
}
func missing(v *snapshot, id string) error {
	provider := "cooker"
	if strings.HasPrefix(id, "vapp_") || strings.HasPrefix(id, "vflow_") {
		provider = "vercel"
	}
	for _, src := range v.sources {
		if src.Provider == provider && !src.Available {
			return ErrUnavailable
		}
	}
	return ErrNotFound
}
