package memory

import (
	"context"
	"errors"
	"fmt"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
	"sync"
	"sync/atomic"
	"testing"
)

func TestAppPrefixIsAtomicAndReviewConfigurationSurvivesReload(t *testing.T) {
	s := New().Apps
	ctx := context.Background()
	var wg sync.WaitGroup
	var wins atomic.Int32
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ns := ""
			if i%2 == 0 {
				ns = "default"
			}
			err := s.Create(ctx, &model.App{ID: fmt.Sprint(i), DeployTarget: model.DeployTarget{Kind: model.DeployTargetKubernetes, Prefix: "shop", Namespace: ns}})
			if err == nil {
				wins.Add(1)
			} else if !errors.Is(err, store.ErrConflict) {
				t.Errorf("unexpected error: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("%d apps claimed the same namespace/prefix", wins.Load())
	}
	a := &model.App{ID: "reviewed", BuildPlan: &model.BuildPlan{Kind: model.BuildPlanCompose, Commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", InstallationID: 7, Files: []string{"compose.yaml", "deploy/compose.prod.yaml"}, Profiles: []string{"prod"}, Variables: map[string]string{"PORT": "8080"}}, DeployTarget: model.DeployTarget{Kind: model.DeployTargetECS, Prefix: "shop", ExternalServices: []model.ExternalServiceBinding{{Service: "db", Provider: "gcp-cloud-sql", Consumers: []string{"api"}, Environment: map[string]string{"DATABASE_URL": "CLOUD_DB"}}}}}
	if err := s.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	a.BuildPlan.Files[0] = "changed"
	a.DeployTarget.ExternalServices[0].Environment["DATABASE_URL"] = "changed"
	got, err := s.Get(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.BuildPlan.Files[0] != "compose.yaml" || got.DeployTarget.ExternalServices[0].Environment["DATABASE_URL"] != "CLOUD_DB" || got.BuildPlan.InstallationID != 7 {
		t.Fatal("stored review changed through caller alias")
	}
	got.BuildPlan.Variables["PORT"] = "9090"
	again, _ := s.Get(ctx, a.ID)
	if again.BuildPlan.Variables["PORT"] != "8080" {
		t.Fatal("read result aliases stored configuration")
	}
}
