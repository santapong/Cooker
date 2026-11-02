//go:build integration

package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
	storepg "github.com/santapong/cooker/internal/store/postgres"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

type preflightPoolTrace struct {
	*jobqueue.Postgres
	suspended chan error
}

func (s *preflightPoolTrace) Suspend(ctx context.Context, owner, id, reason string) error {
	err := s.Postgres.Suspend(ctx, owner, id, reason)
	s.suspended <- err
	return err
}
func TestAppPreflightActualPoolAtomicFailureAndIdentityFence(t *testing.T) {
	if os.Getenv("COOKER_APP_PREFLIGHT_ACCEPTANCE") != "owned-local-v1" {
		t.Skip("explicit owned PostgreSQL App pool fixture required")
	}
	for _, mode := range []string{"storage-fault", "foreign-history", "foreign-run-actor", "invalid-typed-marker"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			raw, err := url.Parse(os.Getenv("COOKER_INTEGRATION_DATABASE_URL"))
			if err != nil || raw.Hostname() != "127.0.0.1" || raw.User == nil || raw.User.Username() != "fresh_worker_fixture" || raw.Path != "/cooker_fresh_worker_test" {
				t.Fatal("unowned database refused")
			}
			admin, err := sql.Open("postgres", raw.String())
			if err != nil {
				t.Fatal(err)
			}
			defer admin.Close()
			schema := fmt.Sprintf("preflight_pool_%d", time.Now().UnixNano())
			if _, err := admin.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
				t.Fatal(err)
			}
			defer admin.ExecContext(context.Background(), `DROP SCHEMA `+schema+` CASCADE`)
			query := raw.Query()
			query.Set("search_path", schema)
			raw.RawQuery = query.Encode()
			st, err := storepg.NewStore(ctx, raw.String())
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			db, err := sql.Open("postgres", raw.String())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			jobs := jobqueue.NewPostgres(db)
			jobs.OnTransition = storepg.SyncJobRun
			app := &model.App{ID: "app", Name: "fixture", GitHubRepo: "fixture/preflight", Branch: "main"}
			if err := st.Apps.Create(ctx, app); err != nil {
				t.Fatal(err)
			}
			stamp := time.Now().UTC()
			actor := &model.PipelineRun{StartedByUserSub: "accepted"}
			p := AppDeployPlan{Version: 1, RunID: "run", App: app, Commit: strings.Repeat("a", 40), CreatedAt: stamp, Actor: actor}
			payload, _ := json.Marshal(p)
			if err := st.Pipelines.Create(ctx, &model.Pipeline{ID: "app-app-run", Name: "accepted"}); err != nil {
				t.Fatal(err)
			}
			r := actor.Clone()
			r.ID, r.PipelineID, r.CreatedAt, r.Status = "run", "app-app-run", stamp, model.RunStatusPending
			if err := st.Runs.Create(ctx, r); err != nil {
				t.Fatal(err)
			}
			if mode == "foreign-run-actor" {
				if _, err := db.ExecContext(ctx, `UPDATE pipeline_runs SET started_by_email='other'`); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "foreign-history" || mode == "invalid-typed-marker" {
				id, appID, kind := "foreign", "other", "deploy"
				if mode == "invalid-typed-marker" {
					id, appID, kind = store.AppPreflightHistoryID("run"), "app", "rejected"
				} else {
					if err := st.Apps.Create(ctx, &model.App{ID: "other", Name: "other", GitHubRepo: "fixture/other", Branch: "main"}); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := db.ExecContext(ctx, `INSERT INTO app_deploys(id,app_id,run_id,pipeline_id,status,kind)VALUES($1,$2,'run','app-app-run','failed',$3)`, id, appID, kind); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "storage-fault" {
				if _, err := db.ExecContext(ctx, `CREATE FUNCTION fault_insert() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic-storage-canary'; END $$; CREATE TRIGGER fault_insert BEFORE INSERT ON app_deploys FOR EACH ROW WHEN(NEW.kind='rejected') EXECUTE FUNCTION fault_insert()`); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := st.Runs.Get(ctx, "run")
			historyBefore, _ := st.AppDeploys.ListByApp(ctx, "app", 100)
			if _, err := jobs.Enqueue(ctx, JobKindAppDeploy, payload, jobqueue.EnqueueOptions{ID: "app-run:run", MaxAttempts: 1}); err != nil {
				t.Fatal(err)
			}
			trace := &preflightPoolTrace{Postgres: jobs, suspended: make(chan error, 1)}
			q := &AppDeployQueue{Jobs: jobs, Store: st, Deployer: NewAppDeployer(NewExecutor(), ""), RequireRegistryPins: true}
			dispatch := jobqueue.NewRegistry()
			dispatch.Register(JobKindAppDeploy, func(c context.Context, j *jobqueue.Job) error {
				err := q.Handle(c, j)
				if !errors.Is(err, jobqueue.ErrOutcomeUnknown) || strings.Contains(err.Error(), "canary") {
					return errors.New("fixture rejected attempt incorrectly classified")
				}
				return err
			})
			pool, err := jobqueue.NewPool(jobqueue.PoolOptions{Store: trace, Registry: dispatch, Size: 1, PollEvery: 10 * time.Millisecond, LeaseTimeout: time.Minute, HeartbeatEvery: 20 * time.Second, WorkerIDPrefix: "owned-preflight", ClaimContext: func(c context.Context, j *jobqueue.Job) context.Context {
				return store.WithRunClaim(c, j.ID, j.LockedBy, nil)
			}})
			if err != nil {
				t.Fatal(err)
			}
			runCtx, stop := context.WithCancel(ctx)
			done := make(chan error, 1)
			go func() { done <- pool.Run(runCtx) }()
			select {
			case suspension := <-trace.suspended:
				if (mode == "storage-fault") != (suspension == nil) {
					stop()
					<-done
					t.Fatal("lifecycle identity/fault fence failed")
				}
			case <-ctx.Done():
				stop()
				<-done
				t.Fatal("real pool did not suspend")
			}
			stop()
			<-done
			after, _ := st.Runs.Get(ctx, "run")
			j, _ := jobs.Get(ctx, "app-run:run")
			rows, _ := st.AppDeploys.ListByApp(ctx, "app", 100)
			if j.Attempts != 1 || j.StartedAt == nil {
				t.Fatal("fixture never made real claim")
			}
			if mode == "storage-fault" {
				if j.Status != jobqueue.StatusNeedsAttention || after.Status != model.RunStatusNeedsAttention || len(rows) != 1 || rows[0].Kind == model.AppDeployKindRejected || rows[0].ImageRef != "" || rows[0].Digest != "" {
					t.Fatal("failed outcome transaction did not retain truthful uncertainty")
				}
			}
			if mode != "storage-fault" {
				if j.Status != jobqueue.StatusRunning || !reflect.DeepEqual(before, after) || !reflect.DeepEqual(rows, historyBefore) {
					t.Fatal("contradictory domain overwritten by pool suspend")
				}
			}
			var stages int
			if db.QueryRowContext(ctx, `SELECT count(*) FROM stage_log_entries`).Scan(&stages) != nil || stages != 0 {
				t.Fatal("preflight emitted stage effects")
			}
		})
	}
}
