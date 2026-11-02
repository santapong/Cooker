package catalog

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
)

// Literal kind permits the original-source negative without a production enum
// overlay. All records are owned in-memory fixtures, not deployed applications.
func catalogRejectedFixture(t *testing.T, rejectedCount int) (*Service, *model.App, string) {
	t.Helper()
	ctx := context.Background()
	s := &Service{Store: memory.New(), Now: func() time.Time { return fixtureTime }}
	app := &model.App{ID: "native-rejections", Name: "Native", DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost}}
	if err := s.Store.Apps.Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	p := &model.Pipeline{ID: "live-graph", Name: "Recorded deployment", Stages: []model.Stage{{ID: "deploy-web", Name: "Web", Type: model.StageTypeDeploy, Config: model.StageConfig{ComposeServiceName: "web", DeployRuntime: "docker", RuntimeName: "live-web-1"}}}}
	if err := s.Store.Pipelines.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.Runs.Create(ctx, &model.PipelineRun{ID: "live-run", PipelineID: p.ID, Status: model.RunStatusSuccess, StageRuns: []model.StageRun{{StageID: "deploy-web", Status: model.RunStatusSuccess}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.AppDeploys.Create(ctx, &model.AppDeploy{ID: "live-deploy", AppID: app.ID, RunID: "live-run", PipelineID: p.ID, Kind: model.AppDeployKindDeploy, Status: model.RunStatusSuccess, CreatedAt: fixtureTime.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	latestID := ""
	for i := range rejectedCount {
		latestID = fmt.Sprintf("reject-%03d", i)
		addCatalogRejectedAttempt(t, s, app, latestID, fixtureTime.Add(time.Duration(i)*time.Second))
	}
	return s, app, latestID
}

func addCatalogRejectedAttempt(t *testing.T, s *Service, app *model.App, id string, at time.Time) {
	t.Helper()
	ctx := context.Background()
	p := &model.Pipeline{ID: "rejected-graph-" + id, Name: "Rejected deployment", Stages: []model.Stage{}}
	if err := s.Store.Pipelines.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.Runs.Create(ctx, &model.PipelineRun{ID: "rejected-run-" + id, PipelineID: p.ID, Status: model.RunStatusFailed, CreatedAt: at, Error: "accepted registry authority is unavailable or changed; review and start a new deployment"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.AppDeploys.Create(ctx, &model.AppDeploy{ID: id, AppID: app.ID, RunID: "rejected-run-" + id, PipelineID: p.ID, Kind: model.AppDeployKind("rejected"), Status: model.RunStatusFailed, CreatedAt: at}); err != nil {
		t.Fatal(err)
	}
}

type rejectedHistoryCompatibilityStore struct{ store.AppDeployStore }

func TestCatalogRejectedHistorySeparatesLatestAttemptAndRecordedServices(t *testing.T) {
	for _, mode := range []string{"concrete beyond history page", "compatibility wrapper"} {
		t.Run(mode, func(t *testing.T) {
			count := 101
			if mode == "compatibility wrapper" {
				count = 1
			}
			s, app, lastID := catalogRejectedFixture(t, count)
			if mode == "compatibility wrapper" {
				s.Store.AppDeploys = rejectedHistoryCompatibilityStore{s.Store.AppDeploys}
			}
			ctx := context.Background()
			apps := s.Apps(ctx, Query{}, false)
			if apps.Total != 1 || apps.Items[0].LatestDeployment == nil || apps.Items[0].LatestDeployment.ID != lastID || apps.Items[0].LatestDeployment.Status != string(model.RunStatusFailed) || apps.Items[0].PipelineID != "rejected-graph-"+lastID {
				t.Fatal("latest failed attempt/link hidden in App summary")
			}
			detail, err := s.App(ctx, app.ID, false)
			if err != nil || len(detail.Deployments) == 0 || detail.Deployments[0].ID != lastID || detail.Deployments[0].Status != string(model.RunStatusFailed) || detail.App.LatestDeployment.ID != lastID {
				t.Fatal("rejected attempt hidden from detail history", err)
			}
			if len(detail.Services) != 1 || detail.Services[0].DeploymentID != "live-deploy" || detail.Services[0].NativeServiceID != "web" || detail.Services[0].Status != string(model.RunStatusSuccess) || detail.Services[0].Health != "unknown" {
				t.Fatal("detail services followed rejection or inferred live health")
			}
			services := s.Services(ctx, Query{}, false)
			if services.Total != 1 || services.Items[0].DeploymentID != "live-deploy" || services.Items[0].ObservedAt == nil || !services.Items[0].ObservedAt.Equal(fixtureTime.Add(-time.Hour)) {
				t.Fatal("global service membership/clock followed rejected attempt")
			}
		})
	}
}

func TestCatalogRejectedHistoryRetainsFailedAndUncertainRuntimeContext(t *testing.T) {
	for _, state := range []model.RunStatus{model.RunStatusFailed, model.RunStatusNeedsAttention} {
		t.Run(string(state), func(t *testing.T) {
			s, app, _ := catalogRejectedFixture(t, 0)
			ctx := context.Background()
			p := &model.Pipeline{ID: "new-runtime-graph", Stages: []model.Stage{{ID: "new-service", Name: "New", Type: model.StageTypeDeploy, Config: model.StageConfig{ComposeServiceName: "new-web", RuntimeName: "new-web-1", DeployRuntime: "docker"}}}}
			if err := s.Store.Pipelines.Create(ctx, p); err != nil {
				t.Fatal(err)
			}
			if err := s.Store.Runs.Create(ctx, &model.PipelineRun{ID: "new-runtime-run", PipelineID: p.ID, Status: state, StageRuns: []model.StageRun{{StageID: "new-service", Status: state}}}); err != nil {
				t.Fatal(err)
			}
			if err := s.Store.AppDeploys.Create(ctx, &model.AppDeploy{ID: "new-runtime", AppID: app.ID, RunID: "new-runtime-run", PipelineID: p.ID, Status: state, Kind: model.AppDeployKindDeploy, CreatedAt: fixtureTime}); err != nil {
				t.Fatal(err)
			}
			addCatalogRejectedAttempt(t, s, app, "latest-reject", fixtureTime.Add(time.Second))
			services := s.Services(ctx, Query{}, false)
			if services.Total != 1 || services.Items[0].DeploymentID != "new-runtime" || services.Items[0].NativeServiceID != "new-web" || services.Items[0].Status != string(state) || services.Items[0].Health != "unknown" {
				t.Fatal("ordinary failed/uncertain runtime skipped to older success")
			}
		})
	}
}

func TestCatalogRejectedHistoryOnlyRejectedAppsHaveNoServices(t *testing.T) {
	s := &Service{Store: memory.New(), Now: func() time.Time { return fixtureTime }}
	ctx := context.Background()
	app := &model.App{ID: "only-rejected", Name: "Only rejected", DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost}}
	if err := s.Store.Apps.Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	addCatalogRejectedAttempt(t, s, app, "only", fixtureTime)
	detail, err := s.App(ctx, app.ID, false)
	if err != nil || len(detail.Deployments) != 1 || detail.Deployments[0].ID != "only" || detail.App.LatestDeployment.Status != string(model.RunStatusFailed) || detail.App.PipelineID != "rejected-graph-only" || len(detail.Services) != 0 || detail.App.Health != "unknown" {
		t.Fatal("only-rejected App lost failure history or gained runtime truth", err)
	}
	if services := s.Services(ctx, Query{}, false); services.Total != 0 {
		t.Fatal("only-rejected App manufactured service inventory")
	}
}
