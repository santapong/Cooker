package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/provider/vercel"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
)

type fixtureVercel struct {
	view *vercel.View
	err  error
}

func (f fixtureVercel) View(context.Context) (*vercel.View, error) { return f.view, f.err }

type fixturePublications []*vercel.Publication

func (f fixturePublications) List(context.Context) ([]*vercel.Publication, error) { return f, nil }

var fixtureTime = time.Date(2026, 10, 2, 8, 0, 0, 0, time.UTC)

func fixture() (*Service, *vercel.View) {
	at := fixtureTime
	d := vercel.Deployment{ID: "dpl_prod", ProjectID: "prj_one", Name: "Beta", State: "READY", Target: "production", URL: "beta-unique.vercel.app", CreatedAt: at.Add(-time.Hour), Details: &vercel.DeploymentDetails{AliasAssigned: true, Aliases: []string{"beta.vercel.app"}, InspectorURL: "https://vercel.com/team/beta/deployment", ServicesKnown: true, Services: []vercel.ServiceDefinition{{Name: "web", Runtime: "nodejs", Framework: "nextjs"}, {Name: "api", Runtime: "python", Framework: "fastapi"}}, ObservedAt: at}}
	p := vercel.Project{ID: "prj_one", Name: "Beta", Framework: "nextjs", Details: &vercel.ProjectDetails{Repository: "org/repo", ProductionBranch: "main", ProductionDeploymentID: d.ID, ProductionDeployment: &d, ServicesKnown: true, Services: []vercel.ServiceDefinition{{Name: "configured-web"}}, ObservedAt: at}}
	c := &vercel.Connection{ID: "conn_one", Identity: vercel.Identity{ID: "team_fixture", Scope: "team"}, ProjectIDs: []string{p.ID}, Enabled: true, LastSuccessAt: &at}
	preview := vercel.Deployment{ID: "dpl_preview", ProjectID: p.ID, State: "ERROR", Target: "", CreatedAt: at, URL: "preview.vercel.app", GitBranch: "feature", Details: &vercel.DeploymentDetails{ServicesKnown: true, Services: []vercel.ServiceDefinition{{Name: "web"}}, ObservedAt: at}}
	v := &vercel.View{Connections: []*vercel.Connection{c}, Projects: []vercel.ImportedProject{{Project: p, ConnectionID: c.ID}}, Deployments: []vercel.ImportedDeployment{{Deployment: d, ConnectionID: c.ID}, {Deployment: preview, ConnectionID: c.ID}}}
	return &Service{Store: memory.New(), Vercel: fixtureVercel{view: v}, Now: func() time.Time { return fixtureTime }}, v
}
func TestCatalogStableIdentityDeduplicationAndNoExecutionWrites(t *testing.T) {
	s, v := fixture()
	ctx := context.Background()
	before := s.Apps(ctx, Query{}, true)
	if before.Total != 1 || before.Items[0].Capabilities.Edit || before.Items[0].Capabilities.Deploy {
		t.Fatal("external catalog gained execution capability", before)
	}
	id := before.Items[0].ID
	later := fixtureTime.Add(time.Minute)
	copyConn := *v.Connections[0]
	copyConn.ID = "conn_new"
	copyConn.LastSuccessAt = &later
	v.Connections = append(v.Connections, &copyConn)
	renamed := v.Projects[0]
	renamed.Name = "Renamed"
	renamed.ConnectionID = copyConn.ID
	v.Projects = append(v.Projects, renamed)
	after := s.Apps(ctx, Query{}, true)
	if after.Total != 1 || after.Items[0].ID != id || after.Items[0].Name != "Renamed" {
		t.Fatal("provider rename/overlap changed identity", after)
	}
	native, _ := s.Store.Apps.List(ctx, 0, 0)
	pipelines, _ := s.Store.Pipelines.List(ctx, 0, 0)
	if len(native) != 0 || len(pipelines) != 0 {
		t.Fatal("read imported executable resources")
	}
	v.Connections[1].ProjectIDs = nil
	if n := s.Apps(ctx, Query{}, false); n.Total != 1 || n.Items[0].Name != "Beta" {
		t.Fatal("deselected newer connection retained ownership", n)
	}
	v.Connections = nil
	if n := s.Apps(ctx, Query{}, false); n.Total != 0 {
		t.Fatal("disconnected inventory retained", n)
	}
}
func TestCurrentProductionIsProviderBindingNotNewestReady(t *testing.T) {
	s, v := fixture()
	ctx := context.Background()
	a := s.Apps(ctx, Query{}, false).Items[0]
	if a.ProductionDeployment == nil || a.ProductionDeployment.ID != "dpl_prod" || a.LatestDeployment.ID != "dpl_preview" {
		t.Fatal("failed preview replaced current production", a)
	}
	if len(a.ProductionDeployment.ProductionURLs) != 1 || a.ProductionDeployment.URL == a.ProductionDeployment.ProductionURLs[0] {
		t.Fatal("alias/deployment URL collapsed")
	}
	// Promoted preview retains its original target in some provider responses.
	v.Projects[0].Details.ProductionDeployment.Target = ""
	if p := s.Apps(ctx, Query{}, false).Items[0].ProductionDeployment; p.Environment != "production" {
		t.Fatal("promotion binding ignored", p)
	}
	v.Projects[0].Details = nil
	if a := s.Apps(ctx, Query{}, false).Items[0]; a.ProductionDeployment != nil {
		t.Fatal("production inferred from history")
	}
}
func TestServiceDefinitionsAndOwnObservationRetention(t *testing.T) {
	s, v := fixture()
	ctx := context.Background()
	rows := s.Services(ctx, Query{Environment: "production_first"}, false)
	if rows.Total != 2 {
		t.Fatal("production-first service membership", rows)
	}
	for _, r := range rows.Items {
		if r.Definition != "deployed" || r.Health != "unknown" || r.DeploymentID != "dpl_prod" || r.Status != "READY" {
			t.Fatal("service/health conflated", r)
		}
	}
	v.Projects[0].Details.ProductionDeployment.Details.Error = "Metadata refresh failed; last-known details retained"
	rows = s.Services(ctx, Query{Environment: "production"}, false)
	if rows.Total != 2 || rows.Items[0].Error == "" || !rows.Items[0].ObservedAt.Equal(fixtureTime) {
		t.Fatal("last-known membership or observation lost", rows)
	}
	v.Projects[0].Details.ProductionDeployment.Details.ServicesKnown = false
	rows = s.Services(ctx, Query{Environment: "production"}, false)
	if rows.Total != 1 || rows.Items[0].Definition != "configured" || rows.Items[0].Status != "configured" {
		t.Fatal("configured component promoted to deployed", rows)
	}
	v.Projects[0].Details.ServicesKnown = false
	rows = s.Services(ctx, Query{Environment: "production"}, false)
	if rows.Items[0].Definition != "unavailable" || rows.Items[0].Status != "unknown" {
		t.Fatal("unknown metadata invented component")
	}
	v.Projects[0].Details.ServicesKnown = true
	v.Projects[0].Details.Services = nil
	rows = s.Services(ctx, Query{Environment: "production"}, false)
	if rows.Items[0].Definition != "application" || rows.Items[0].Name != "Application" {
		t.Fatal("ordinary project fallback", rows)
	}
}
func TestNativeRecordedServicesPermissionsPaginationAndSourceFailures(t *testing.T) {
	s, _ := fixture()
	ctx := context.Background()
	app := &model.App{ID: "native", Name: "Alpha", GitHubRepo: "org/native", Branch: "main", DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost}, HealthStatus: model.AppHealthHealthy, UpdatedAt: fixtureTime}
	p := &model.Pipeline{ID: "dag", Name: "Native DAG", Stages: []model.Stage{{ID: "web", Name: "Web", Type: model.StageTypeDeploy, Config: model.StageConfig{ComposeServiceName: "web", DeployRuntime: "docker", RuntimeName: "native-web"}}}, UpdatedAt: fixtureTime}
	p.SetDefaults()
	if err := s.Store.Apps.Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.Pipelines.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.AppDeploys.Create(ctx, &model.AppDeploy{ID: "deploy", AppID: app.ID, PipelineID: p.ID, RunID: "run", Status: model.RunStatusSuccess, CreatedAt: fixtureTime}); err != nil {
		t.Fatal(err)
	}
	list := s.Apps(ctx, Query{Limit: 1, Offset: 1}, false)
	if list.Total != 2 || len(list.Items) != 1 || list.Items[0].Management != "vercel" {
		t.Fatal("combined pagination", list)
	}
	filtered := s.Apps(ctx, Query{Search: "org/native", Limit: 1}, true)
	if filtered.Total != 1 || !filtered.Items[0].Capabilities.Edit || filtered.Items[0].Href != "/apps/native" {
		t.Fatal("filter before paging/native actions", filtered)
	}
	d, err := s.App(ctx, app.ID, false)
	if err != nil || len(d.Services) != 1 || d.Services[0].Definition != "recorded" || d.App.Capabilities.Edit {
		t.Fatal("native recorded detail", d, err)
	}
	rows := s.Services(ctx, Query{Environment: "production_first"}, false)
	if rows.Total != 3 {
		t.Fatal("unclassified native hidden from production-first", rows)
	}
	s.Vercel = fixtureVercel{err: errors.New("provider-secret-canary")}
	list = s.Apps(ctx, Query{}, false)
	if list.Total != 1 || list.Sources[1].Available || strings.Contains(list.Sources[1].Error, "canary") {
		t.Fatal("failed source suppressed native or leaked body", list)
	}
	if _, err = s.App(ctx, "vapp_missing", false); !errors.Is(err, ErrUnavailable) {
		t.Fatal("failed provider looked like deletion", err)
	}
}
func TestPublicationAttributionAndURLsAreFenced(t *testing.T) {
	s, v := fixture()
	ctx := context.Background()
	s.Publications = fixturePublications{{Status: "succeeded", AccountID: "team_fixture", AccountScope: "team", ProjectID: "prj_one", DeploymentID: "dpl_prod", ArtifactSHA256: strings.Repeat("a", 64)}}
	v.Projects[0].Details.ProductionDeployment.Details.Aliases = append(v.Projects[0].Details.ProductionDeployment.Details.Aliases, "javascript:alert(1)", "https://user:secret@bad.example", "https://127.0.0.1", "https://safe.example?token=secret-canary")
	a := s.Apps(ctx, Query{}, false).Items[0]
	d, err := s.App(ctx, a.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if d.App.SourceLabel != "Uploaded Cooker landing bundle" || d.App.ProductionDeployment.ArtifactSHA256 == "" || d.App.LatestDeployment.ArtifactSHA256 != "" {
		t.Fatal("publication attributed to unrelated preview", d.App)
	}
	for _, release := range d.Deployments {
		if release.ID == "dpl_preview" && release.SourceLabel != "" {
			t.Fatal("history source falsely attributed")
		}
	}
	bytes, _ := json.Marshal(d)
	for _, unsafe := range []string{"javascript:", "user:secret", "127.0.0.1", "secret-canary"} {
		if strings.Contains(string(bytes), unsafe) {
			t.Fatal("unsafe provider field leaked", unsafe)
		}
	}
	if len(d.App.ProductionDeployment.ProductionURLs) != 1 {
		t.Fatal("safe URLs missing", d.App.ProductionDeployment.ProductionURLs)
	}
}
func TestPausedAndFailedSnapshotsStayVisible(t *testing.T) {
	s, v := fixture()
	ctx := context.Background()
	v.Connections[0].Enabled = false
	if a := s.Apps(ctx, Query{}, false); a.Total != 1 || a.Items[0].Freshness.State != "paused" {
		t.Fatal("pause deleted snapshot", a)
	}
	v.Connections[0].Enabled = true
	v.Connections[0].SyncError = "Credential expired"
	if a := s.Apps(ctx, Query{}, false); a.Total != 1 || a.Items[0].Freshness.State != "failed" || a.Items[0].Freshness.ObservedAt == nil {
		t.Fatal("failure deleted snapshot", a)
	}
}

func TestUnverifiedPublicationCannotBecomeSourceEvidence(t *testing.T) {
	s, v := fixture()
	v.Projects[0].Details.ServicesKnown = false
	v.Projects[0].Details.ProductionDeployment.Details.ServicesKnown = false
	s.Publications = fixturePublications{{Status: "uncertain", AccountID: "team_fixture", AccountScope: "team", ProjectID: "prj_one", DeploymentID: "dpl_prod", ArtifactSHA256: strings.Repeat("b", 64)}}
	a := s.Apps(context.Background(), Query{}, false).Items[0]
	if a.ArtifactSHA256 != "" || a.ProductionDeployment.ArtifactSHA256 != "" {
		t.Fatal("unverified receipt attributed to deployed source")
	}
	rows := s.Services(context.Background(), Query{Environment: "production"}, false)
	if rows.Total != 1 || rows.Items[0].Definition != "unavailable" {
		t.Fatal("unverified receipt invented website", rows)
	}
}

func TestIndependentMetadataClocksAndProductionStatusFilter(t *testing.T) {
	s, v := fixture()
	old := fixtureTime.Add(-time.Hour)
	project := *v.Projects[0].Details
	project.ObservedAt = old
	project.Error = "Metadata refresh failed; last-known details retained"
	productionCopy := *project.ProductionDeployment
	productionDetails := *productionCopy.Details
	productionDetails.ObservedAt, productionDetails.Error = old, project.Error
	productionCopy.Details, project.ProductionDeployment = &productionDetails, &productionCopy
	newerConnection := *v.Connections[0]
	newerConnection.ID = "new_token"
	now := fixtureTime.Add(time.Minute)
	newerConnection.LastSuccessAt = &now
	v.Connections = append(v.Connections, &newerConnection)
	newProject := v.Projects[0]
	newProject.ConnectionID, newProject.Details, newProject.Name = newerConnection.ID, &project, "Renamed"
	v.Projects = append(v.Projects, newProject)
	for _, d := range append([]vercel.ImportedDeployment{}, v.Deployments...) {
		d.ConnectionID = newerConnection.ID
		copyDetails := *d.Details
		copyDetails.ObservedAt, copyDetails.Error = old, "Metadata refresh failed; last-known details retained"
		d.Details = &copyDetails
		v.Deployments = append(v.Deployments, d)
	}
	apps := s.Apps(context.Background(), Query{Status: "READY"}, false)
	if apps.Total != 1 {
		t.Fatal("failed latest preview replaced production filter", apps)
	}
	a := apps.Items[0]
	if a.Name != "Renamed" || a.MetadataFreshness == nil || !a.MetadataFreshness.ObservedAt.Equal(fixtureTime) || a.MetadataFreshness.Error != "" || a.ProductionDeployment.MetadataFreshness.Error != "" {
		t.Fatal("core freshness discarded newer matching metadata", a)
	}
	// Once the older connection is deselected its observations cannot enrich
	// another selected connection's incomplete snapshot.
	v.Connections[0].ProjectIDs = nil
	a = s.Apps(context.Background(), Query{}, false).Items[0]
	if a.MetadataFreshness.Error == "" || a.Freshness.State != "stale" || a.ProductionDeployment.MetadataFreshness.Error == "" || !a.ProductionDeployment.MetadataFreshness.ObservedAt.Equal(old) {
		t.Fatal("stale own observations hidden", a)
	}
}

func TestRecordedNativeServicesDoNotFollowEditedAppDestination(t *testing.T) {
	s, _ := fixture()
	ctx := context.Background()
	if err := s.Store.Environments.Create(ctx, &model.Environment{ID: "deployed-env", Name: "QA"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.Environments.Create(ctx, &model.Environment{ID: "new-env", Name: "production"}); err != nil {
		t.Fatal(err)
	}
	a := &model.App{ID: "native-edited", Name: "Native", EnvironmentID: "new-env", DeployTarget: model.DeployTarget{Kind: model.DeployTargetKubernetes}}
	p := &model.Pipeline{ID: "recorded", Name: "Recorded", Stages: []model.Stage{{ID: "web", Name: "Web", Type: model.StageTypeDeploy, EnvironmentID: "deployed-env", Config: model.StageConfig{DeployRuntime: "docker", ComposeServiceName: "web"}}}}
	if err := s.Store.Apps.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.Pipelines.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.AppDeploys.Create(ctx, &model.AppDeploy{ID: "deployed", AppID: a.ID, PipelineID: p.ID, Status: model.RunStatusSuccess, CreatedAt: fixtureTime}); err != nil {
		t.Fatal(err)
	}
	rows := s.Services(ctx, Query{Management: "cooker", AppID: a.ID}, false)
	if rows.Total != 1 || rows.Items[0].Environment != "QA" || rows.Items[0].Provider != "docker-host" || rows.Items[0].DeploymentID != "deployed" {
		t.Fatal("edited destination relabelled recorded service", rows)
	}
}

type refusingNativeApps struct{ store.AppStore }

func (refusingNativeApps) List(context.Context, int, int) ([]*model.App, error) {
	panic("provider-only read touched native inventory")
}
func TestProviderOnlyReadDoesNotScanNativeInventory(t *testing.T) {
	s, _ := fixture()
	s.Store.Apps = refusingNativeApps{s.Store.Apps}
	ctx := context.Background()
	if s.Apps(ctx, Query{Provider: "vercel"}, false).Total != 1 || s.Services(ctx, Query{Provider: "vercel", Environment: "production"}, false).Total != 2 || s.Pipelines(ctx, Query{Management: "vercel"}, false).Total != 1 {
		t.Fatal("provider-only inventory failed")
	}
}
