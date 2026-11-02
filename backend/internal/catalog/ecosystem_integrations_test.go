package catalog

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/provider/vercel"
)

func ecosystemCatalogItem(id, access string, projectIDs ...string) vercel.IntegrationInstallation {
	return vercel.IntegrationInstallation{ID: id, IntegrationID: "int_neon", Name: "Neon", Slug: "neon", InstallationType: "marketplace", ProviderStatus: "ready", ProjectAccess: access, ProjectIDs: projectIDs}
}

func ecosystemCatalogInventory(at time.Time, items ...vercel.IntegrationInstallation) *vercel.IntegrationInventory {
	attempted := at.Add(time.Second)
	return &vercel.IntegrationInventory{Items: append([]vercel.IntegrationInstallation{}, items...), ObservedAt: &at, AttemptedAt: &attempted, State: "available", Complete: true}
}

func ecosystemCatalogOverlap(v *vercel.View, id string, coreAt time.Time, inventory *vercel.IntegrationInventory) *vercel.Connection {
	c := *v.Connections[0]
	c.ID, c.LastSuccessAt, c.Integrations = id, &coreAt, inventory
	c.ProjectIDs = append([]string{}, c.ProjectIDs...)
	v.Connections = append(v.Connections, &c)
	p := v.Projects[0]
	p.ConnectionID, p.Name = id, id
	v.Projects = append(v.Projects, p)
	return &c
}

func ecosystemCatalogApp(t *testing.T, s *Service) AppDetail {
	t.Helper()
	apps := s.Apps(context.Background(), Query{}, false)
	if apps.Total != 1 || len(apps.Items) != 1 {
		t.Fatalf("expected one imported app: %#v", apps)
	}
	detail, err := s.App(context.Background(), apps.Items[0].ID, false)
	if err != nil {
		t.Fatal(err)
	}
	return detail
}

func TestEcosystemCatalogExactProjectAccessDoesNotCreateExecutableResources(t *testing.T) {
	s, v := fixture()
	ctx := context.Background()
	before := s.Services(ctx, Query{Environment: "production"}, true)
	v.Connections[0].Integrations = ecosystemCatalogInventory(fixtureTime,
		ecosystemCatalogItem("icfg_all", "all_projects", "prj_unselected"),
		ecosystemCatalogItem("icfg_allowed", "selected_projects", "prj_one", "prj_foreign"),
		ecosystemCatalogItem("icfg_denied", "selected_projects", "prj_foreign"),
		ecosystemCatalogItem("icfg_none", "selected_projects"),
	)
	app := ecosystemCatalogApp(t, s)
	pipeline, err := s.Pipeline(ctx, app.App.PipelineID, true)
	if err != nil || app.Integrations == nil || pipeline.Integrations == nil || len(app.Integrations.Items) != 2 || !reflect.DeepEqual(app.Integrations, pipeline.Integrations) {
		t.Fatal("app/pipeline integration projection disagrees", app, pipeline, err)
	}
	for _, item := range app.Integrations.Items {
		if !reflect.DeepEqual(item.ProjectIDs, []string{"prj_one"}) || (item.ProjectAccess != "all_projects" && item.ProjectAccess != "selected_projects") {
			t.Fatal("catalog exposed access outside this exact selected provider project", item)
		}
	}
	if app.App.Capabilities.Edit || app.App.Capabilities.Deploy || pipeline.Pipeline.Capabilities.Edit || pipeline.Pipeline.Capabilities.Deploy || app.App.Health != "unknown" {
		t.Fatal("installation access became execution permission or resource health")
	}
	after := s.Services(ctx, Query{Environment: "production"}, true)
	if !reflect.DeepEqual(before.Items, after.Items) || after.Total != 2 || s.Apps(ctx, Query{}, true).Total != 1 || s.Pipelines(ctx, Query{}, true).Total != 1 {
		t.Fatal("installation access invented services, apps, or pipelines", after)
	}
	nativeApps, err := s.Store.Apps.List(ctx, 0, 0)
	if err != nil || len(nativeApps) != 0 {
		t.Fatal("catalog read wrote native apps", nativeApps, err)
	}
	nativePipelines, err := s.Store.Pipelines.List(ctx, 0, 0)
	if err != nil || len(nativePipelines) != 0 {
		t.Fatal("catalog read wrote executable pipelines", nativePipelines, err)
	}
	v.Connections[0].Enabled = false
	if paused := ecosystemCatalogApp(t, s); paused.Integrations == nil || len(paused.Integrations.Items) != 2 {
		t.Fatal("pause erased retained access observation", paused)
	}
	v.Connections[0].ProjectIDs = []string{}
	if deselected := s.Apps(ctx, Query{}, false); deselected.Total != 0 {
		t.Fatal("deselected connection retained integration contribution", deselected)
	}
}

func TestEcosystemCatalogIntegrationClockMergesIndependentlyOfCoreAndDetails(t *testing.T) {
	s, v := fixture()
	fresh := fixtureTime.Add(10 * time.Minute)
	v.Connections[0].Integrations = ecosystemCatalogInventory(fresh, ecosystemCatalogItem("icfg_fresh", "all_projects"))
	other := ecosystemCatalogOverlap(v, "conn_new_core", fixtureTime.Add(time.Hour), ecosystemCatalogInventory(fixtureTime.Add(-time.Hour), ecosystemCatalogItem("icfg_old", "all_projects")))
	olderDetails := *v.Projects[1].Details
	olderDetails.ObservedAt = fixtureTime.Add(-time.Hour)
	v.Projects[1].Details = &olderDetails
	app := ecosystemCatalogApp(t, s)
	if app.App.Name != other.ID || app.Integrations == nil || len(app.Integrations.Items) != 1 || app.Integrations.Items[0].ID != "icfg_fresh" || !app.Integrations.ObservedAt.Equal(fresh) || app.App.MetadataFreshness == nil || !app.App.MetadataFreshness.ObservedAt.Equal(fixtureTime) {
		t.Fatal("core or details clock displaced independent integration observation", app)
	}
	other.ProjectIDs = nil
	if app := ecosystemCatalogApp(t, s); app.App.Name != "Beta" || app.Integrations.Items[0].ID != "icfg_fresh" {
		t.Fatal("deselected overlap still contributed", app)
	}
}

func TestEcosystemCatalogNewerSuccessfulEmptyClearsOldInstallations(t *testing.T) {
	s, v := fixture()
	v.Connections[0].Integrations = ecosystemCatalogInventory(fixtureTime, ecosystemCatalogItem("icfg_old", "all_projects"))
	emptyAt := fixtureTime.Add(time.Minute)
	empty := ecosystemCatalogInventory(emptyAt)
	// The fresher inventory belongs to an older core snapshot.
	ecosystemCatalogOverlap(v, "conn_empty", fixtureTime.Add(-time.Hour), empty)
	app := ecosystemCatalogApp(t, s)
	if app.Integrations == nil || !app.Integrations.Complete || app.Integrations.State != "available" || app.Integrations.Items == nil || len(app.Integrations.Items) != 0 || !app.Integrations.ObservedAt.Equal(emptyAt) || !app.Integrations.AttemptedAt.Equal(*empty.AttemptedAt) {
		t.Fatal("newer complete empty observation retained older installations", app)
	}
}

func TestEcosystemCatalogKeepsObservationAndWarningsFromOneConnection(t *testing.T) {
	s, v := fixture()
	good := ecosystemCatalogInventory(fixtureTime, ecosystemCatalogItem("icfg_good", "all_projects"))
	v.Connections[0].Integrations = good
	failedAt := fixtureTime.Add(time.Hour)
	failed := &vercel.IntegrationInventory{Items: []vercel.IntegrationInstallation{}, AttemptedAt: &failedAt, State: "unavailable", ErrorCode: "forbidden", Error: "This connection cannot read installation metadata"}
	other := ecosystemCatalogOverlap(v, "conn_failed", failedAt, failed)
	app := ecosystemCatalogApp(t, s)
	if app.Integrations.State != "available" || app.Integrations.Error != "" || app.Integrations.ErrorCode != "" || !app.Integrations.AttemptedAt.Equal(*good.AttemptedAt) || app.Integrations.Items[0].ID != "icfg_good" {
		t.Fatal("another token's first failure was grafted onto successful observation", app)
	}
	staleAt := fixtureTime.Add(2 * time.Minute)
	stale := ecosystemCatalogInventory(staleAt, ecosystemCatalogItem("icfg_retained", "all_projects"))
	stale.State, stale.ErrorCode, stale.Error, stale.AttemptedAt = "stale", "rate_limited", "Retained observation awaiting retry", &failedAt
	other.Integrations = stale
	app = ecosystemCatalogApp(t, s)
	if app.Integrations.Items[0].ID != "icfg_retained" || app.Integrations.State != stale.State || app.Integrations.ErrorCode != stale.ErrorCode || app.Integrations.Error != stale.Error || !app.Integrations.ObservedAt.Equal(staleAt) || !app.Integrations.AttemptedAt.Equal(failedAt) || !app.Integrations.Complete {
		t.Fatal("winning retained observation lost its own data/clock/warning pairing", app)
	}
}

func TestEcosystemCatalogUnobservedAttemptsAndEqualClockTies(t *testing.T) {
	t.Run("newest failed attempt hides unobserved rows", func(t *testing.T) {
		s, v := fixture()
		firstAt, secondAt := fixtureTime, fixtureTime.Add(time.Minute)
		v.Connections[0].Integrations = &vercel.IntegrationInventory{Items: []vercel.IntegrationInstallation{ecosystemCatalogItem("icfg_unproven", "all_projects")}, AttemptedAt: &firstAt, State: "unavailable", ErrorCode: "forbidden", Error: "Older attempt"}
		failed := &vercel.IntegrationInventory{Items: []vercel.IntegrationInstallation{ecosystemCatalogItem("icfg_unproven_other", "all_projects")}, AttemptedAt: &secondAt, ObservedAt: &secondAt, Complete: false, State: "available", ErrorCode: "rate_limited", Error: "Newer attempt"}
		ecosystemCatalogOverlap(v, "conn_new_attempt", fixtureTime.Add(-time.Hour), failed)
		app := ecosystemCatalogApp(t, s)
		if app.Integrations == nil || app.Integrations.State != "unavailable" || app.Integrations.Complete || app.Integrations.ObservedAt != nil || len(app.Integrations.Items) != 0 || !app.Integrations.AttemptedAt.Equal(secondAt) || app.Integrations.Error != "Newer attempt" {
			t.Fatal("first-failure fallback fabricated successful inventory or chose core clock", app)
		}
	})
	t.Run("healthy wins equal success then stable connection identity", func(t *testing.T) {
		s, v := fixture()
		stale := ecosystemCatalogInventory(fixtureTime, ecosystemCatalogItem("icfg_stale", "all_projects"))
		stale.State, stale.ErrorCode, stale.Error = "stale", "forbidden", "Retained observation"
		v.Connections[0].ID = "conn_a_stale"
		v.Projects[0].ConnectionID = v.Connections[0].ID
		v.Connections[0].Integrations = stale
		ecosystemCatalogOverlap(v, "conn_z_healthy", fixtureTime.Add(-time.Hour), ecosystemCatalogInventory(fixtureTime, ecosystemCatalogItem("icfg_healthy", "all_projects")))
		if app := ecosystemCatalogApp(t, s); app.Integrations.Items[0].ID != "icfg_healthy" {
			t.Fatal("lexical identity displaced healthy observation at equal success clock", app)
		}
		v.Connections[0].Integrations = ecosystemCatalogInventory(fixtureTime, ecosystemCatalogItem("icfg_lexical", "all_projects"))
		if app := ecosystemCatalogApp(t, s); app.Integrations.Items[0].ID != "icfg_lexical" {
			t.Fatal("equal healthy observations are not deterministically selected", app)
		}
	})
}

func TestEcosystemCatalogSeparatesAccountsAndRejectsInvalidAccountScope(t *testing.T) {
	s, v := fixture()
	v.Connections[0].TeamID = "team_fixture"
	v.Connections[0].Integrations = ecosystemCatalogInventory(fixtureTime, ecosystemCatalogItem("icfg_same", "all_projects"))
	otherInventory := ecosystemCatalogInventory(fixtureTime.Add(time.Hour), ecosystemCatalogItem("icfg_same", "all_projects"))
	otherInventory.Items[0].Name = "Same vendor, different account"
	other := ecosystemCatalogOverlap(v, "conn_other_account", fixtureTime.Add(time.Hour), otherInventory)
	other.Identity = vercel.Identity{ID: "team_other", Scope: "team"}
	other.TeamID = "team_other"
	apps := s.Apps(context.Background(), Query{}, false)
	if apps.Total != 2 {
		t.Fatal("same project and installation IDs collapsed across accounts", apps)
	}
	for _, app := range apps.Items {
		detail, err := s.App(context.Background(), app.ID, false)
		if err != nil || detail.Integrations == nil || len(detail.Integrations.Items) != 1 {
			t.Fatal("separate account observation missing", detail, err)
		}
		wantName := "Neon"
		if app.Name == other.ID {
			wantName = "Same vendor, different account"
		}
		if detail.Integrations.Items[0].Name != wantName {
			t.Fatal("inventory crossed account identity", detail)
		}
	}
	for _, test := range []struct{ name, scope, identity, team string }{
		{"different team", "team", "team_fixture", "team_other"},
		{"personal team mismatch", "personal", "user_fixture", "team_other"},
		{"unknown scope", "future", "team_fixture", ""},
		{"missing identity", "team", "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			local, view := fixture()
			view.Connections[0].Integrations = ecosystemCatalogInventory(fixtureTime, ecosystemCatalogItem("icfg_one", "all_projects"))
			view.Connections[0].Identity = vercel.Identity{ID: test.identity, Scope: test.scope}
			view.Connections[0].TeamID = test.team
			if detail := ecosystemCatalogApp(t, local); detail.Integrations != nil {
				t.Fatal("invalid account supplied installation projection", detail)
			}
		})
	}
}

func TestEcosystemCatalogClonesAndOmitsLegacyAndNativeObservations(t *testing.T) {
	s, v := fixture()
	ctx := context.Background()
	original := ecosystemCatalogInventory(fixtureTime, ecosystemCatalogItem("icfg_one", "selected_projects", "prj_one", "prj_unselected"))
	v.Connections[0].Integrations = original
	app := ecosystemCatalogApp(t, s)
	app.Integrations.Items[0].Name = "mutated"
	app.Integrations.Items[0].ProjectIDs[0] = "prj_mutated"
	*app.Integrations.ObservedAt, *app.Integrations.AttemptedAt = time.Time{}, time.Time{}
	pipeline, err := s.Pipeline(ctx, app.App.PipelineID, false)
	if err != nil || pipeline.Integrations == nil || pipeline.Integrations.Items[0].Name != "Neon" || pipeline.Integrations.Items[0].ProjectIDs[0] != "prj_one" || !pipeline.Integrations.ObservedAt.Equal(fixtureTime) || pipeline.Integrations.AttemptedAt.IsZero() || original.Items[0].ProjectIDs[0] != "prj_one" || original.Items[0].ProjectIDs[1] != "prj_unselected" {
		t.Fatal("detail mutation escaped clone boundary", pipeline, err)
	}
	v.Connections[0].Integrations = nil
	legacy := ecosystemCatalogApp(t, s)
	legacyPipeline, err := s.Pipeline(ctx, legacy.App.PipelineID, false)
	if err != nil || legacy.Integrations != nil || legacyPipeline.Integrations != nil {
		t.Fatal("legacy details fabricated installation observation", legacy, legacyPipeline, err)
	}
	native := &model.App{ID: "native_ecosystem", Name: "Native", DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost}, UpdatedAt: fixtureTime}
	if err := s.Store.Apps.Create(ctx, native); err != nil {
		t.Fatal(err)
	}
	nativeDetail, err := s.App(ctx, native.ID, false)
	if err != nil || nativeDetail.Integrations != nil {
		t.Fatal("native detail gained provider integration observation", nativeDetail, err)
	}
	for _, detail := range []any{legacy, legacyPipeline, nativeDetail} {
		encoded, err := json.Marshal(detail)
		if err != nil || strings.Contains(string(encoded), `"integrations"`) {
			t.Fatal("optional absent integrations changed legacy/native JSON", string(encoded), err)
		}
	}
}
