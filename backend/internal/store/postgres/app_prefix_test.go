package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

func TestAppPrefixPostgresMigrationAndPersistence(t *testing.T) {
	dsn := os.Getenv("COOKER_INTEGRATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires a disposable PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	schema := fmt.Sprintf("cooker_prefix_test_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	st, err := NewStore(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	app := &model.App{ID: "reviewed", Name: "reviewed", GitHubRepo: "acme/shop", Branch: "develop", CreatedAt: time.Now(), UpdatedAt: time.Now(), BuildPlan: &model.BuildPlan{Kind: model.BuildPlanCompose, Commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", InstallationID: 7, Files: []string{"compose.yaml", "deploy/compose.prod.yaml"}, Profiles: []string{"production"}, Variables: map[string]string{"PORT": "8080"}}, DeployTarget: model.DeployTarget{Kind: model.DeployTargetECS, Prefix: "shop", ExternalServices: []model.ExternalServiceBinding{{Service: "db", Provider: "gcp-cloud-sql", Resource: "project:region:db", Consumers: []string{"api"}, Environment: map[string]string{"DATABASE_URL": "CLOUD_DB"}}}}}
	if err := st.Apps.Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	got, err := st.Apps.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.BuildPlan.Commit != app.BuildPlan.Commit || got.BuildPlan.InstallationID != 7 || len(got.BuildPlan.Files) != 2 || got.DeployTarget.ExternalServices[0].Environment["DATABASE_URL"] != "CLOUD_DB" {
		t.Fatal("review configuration did not survive JSONB storage")
	}
	var wg sync.WaitGroup
	var wins atomic.Int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ns := ""
			if i%2 == 0 {
				ns = "default"
			}
			a := &model.App{ID: fmt.Sprint(i), Name: fmt.Sprint(i), GitHubRepo: "acme/shop", Branch: "main", CreatedAt: time.Now(), UpdatedAt: time.Now(), DeployTarget: model.DeployTarget{Kind: model.DeployTargetKubernetes, Prefix: "shop", Namespace: ns}}
			err := st.Apps.Create(ctx, a)
			if err == nil {
				wins.Add(1)
			} else if !errors.Is(err, store.ErrConflict) {
				t.Errorf("prefix violation was not mapped to conflict: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("%d concurrent prefix claims succeeded", wins.Load())
	}
	// No migration mutates older unscoped rows; they still load normally.
	if err := st.Apps.Create(ctx, &model.App{ID: "legacy", Name: "legacy", GitHubRepo: "acme/legacy", Branch: "main", CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	second, err := NewStore(ctx, u.String())
	if err != nil {
		t.Fatal("migration replay:", err)
	}
	defer second.Close()
	if _, err := second.Apps.Get(ctx, "legacy"); err != nil {
		t.Fatal(err)
	}
}
