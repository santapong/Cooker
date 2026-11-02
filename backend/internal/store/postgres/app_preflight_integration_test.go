//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type preflightPGFixture struct {
	db      *sql.DB
	jobs    *jobqueue.Postgres
	ctx     context.Context
	f       store.AppPreflightFailure
	payload []byte
}

func newPreflightPGFixture(t *testing.T, stamp time.Time, environment ...bool) *preflightPGFixture {
	t.Helper()
	if os.Getenv("COOKER_APP_PREFLIGHT_ACCEPTANCE") != "owned-local-v1" {
		t.Skip("explicit owned App preflight PostgreSQL fixture required")
	}
	u, err := url.Parse(os.Getenv("COOKER_INTEGRATION_DATABASE_URL"))
	if err != nil || u.Scheme != "postgres" || u.Hostname() != "127.0.0.1" || u.User == nil || u.User.Username() != "fresh_worker_fixture" || u.Path != "/cooker_fresh_worker_test" {
		t.Fatal("unowned database refused")
	}
	db := claimTestDB(t)
	ctx := context.Background()
	actor := &model.PipelineRun{StartedByUserSub: "accepted-operator", StartedByEmail: "fixture@example.invalid", StartedByGroups: []string{"operator"}, StartedByTokenHash: "accepted-audit-hash"}
	app := &model.App{ID: "app", Name: "fixture", GitHubRepo: "fixture/preflight", Branch: "main"}
	environmentVersion := 0
	if len(environment) == 1 && environment[0] {
		env := &model.Environment{ID: "env", Name: "accepted"}
		if err := NewEnvironmentStore(db).Create(ctx, env); err != nil {
			t.Fatal(err)
		}
		app.EnvironmentID, environmentVersion = env.ID, env.Version
	}
	if err := NewAppStore(db).Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"version": 1, "app": app, "runId": "run", "commit": strings.Repeat("a", 40), "createdAt": stamp, "actor": actor, "environmentVersion": environmentVersion, "registryAuthority": map[string]any{"version": 1}, "safeUnconsumedPin": "unchanged-reference"})
	f, err := store.DecodeAppPreflightFailure(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewPipelineStore(db).Create(ctx, &model.Pipeline{ID: f.PipelineID, Name: "accepted view"}); err != nil {
		t.Fatal(err)
	}
	run := actor.Clone()
	run.ID, run.PipelineID, run.Status, run.CreatedAt = f.RunID, f.PipelineID, model.RunStatusPending, stamp
	run.Variables = map[string]string{"ordinaryConfig": "preserve"}
	if err := NewRunStore(db).Create(ctx, run); err != nil {
		t.Fatal(err)
	}
	jobs := jobqueue.NewPostgres(db)
	jobs.OnTransition = SyncJobRun
	if _, err := jobs.Enqueue(ctx, "app-deploy", payload, jobqueue.EnqueueOptions{ID: "app-run:run", ConcurrencyKey: "app:app", MaxAttempts: 1}); err != nil {
		t.Fatal(err)
	}
	job, err := jobs.Dequeue(ctx, "worker", nil)
	if err != nil || job.ID != "app-run:run" {
		t.Fatal("fixture claim failed")
	}
	return &preflightPGFixture{db: db, jobs: jobs, ctx: store.WithRunClaim(ctx, job.ID, job.LockedBy, nil), f: f, payload: payload}
}
func (f *preflightPGFixture) assertPendingNoHistory(t *testing.T) {
	t.Helper()
	r, err := NewRunStore(f.db).Get(context.Background(), f.f.RunID)
	if err != nil || r.Status != model.RunStatusPending || r.Error != "" || r.FinishedAt != nil {
		t.Fatal("atomic rollback changed run")
	}
	var n int
	if f.db.QueryRow(`SELECT count(*) FROM app_deploys`).Scan(&n) != nil || n != 0 {
		t.Fatal("atomic rollback left history")
	}
}
func (f *preflightPGFixture) assertRejected(t *testing.T) (*model.PipelineRun, *model.AppDeploy) {
	t.Helper()
	r, err := NewRunStore(f.db).Get(context.Background(), f.f.RunID)
	d, e := NewAppDeployStore(f.db).ListByApp(context.Background(), f.f.AppID, 100)
	if err != nil || e != nil || len(d) != 1 || !store.AppPreflightMarkerValid(f.f, r, d[0]) {
		t.Fatal("exact rejected outcome unavailable")
	}
	return r, d[0]
}
func TestAppPreflightPostgresPrecision(t *testing.T) {
	for _, nano := range []int{123456100, 123456499, 123456500, 123456789, 999999789} {
		t.Run(time.Duration(nano).String(), func(t *testing.T) {
			f := newPreflightPGFixture(t, time.Date(2026, 9, 1, 0, 0, 0, nano, time.UTC))
			if err := NewAppDeployStore(f.db).RecordAppPreflightFailure(f.ctx, f.f); err != nil {
				t.Fatal("valid nanosecond accepted timestamp rejected")
			}
			f.assertRejected(t)
		})
	}
}
func TestAppPreflightPostgresAtomicConcurrentAndReopen(t *testing.T) {
	f := newPreflightPGFixture(t, time.Now().UTC().Truncate(time.Microsecond))
	var schema string
	if f.db.QueryRow(`SELECT current_schema()`).Scan(&schema) != nil {
		t.Fatal("fixture schema missing")
	}
	u, _ := url.Parse(os.Getenv("COOKER_INTEGRATION_DATABASE_URL"))
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	other, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	before, _ := NewRunStore(f.db).Get(context.Background(), f.f.RunID)
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		pool := f.db
		if i%2 == 0 {
			pool = other
		}
		go func() { defer wg.Done(); errs <- NewAppDeployStore(pool).RecordAppPreflightFailure(f.ctx, f.f) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal("concurrent rejected outcome failed")
		}
	}
	r, d := f.assertRejected(t)
	stamp := *r.FinishedAt
	if !reflect.DeepEqual(r.Variables, before.Variables) || r.StartedByUserSub != before.StartedByUserSub || r.Source != nil {
		t.Fatal("accepted run metadata altered")
	}
	for i := 0; i < 6; i++ {
		reopen, e := sql.Open("postgres", u.String())
		if e != nil {
			t.Fatal(e)
		}
		e = NewAppDeployStore(reopen).RecordAppPreflightFailure(f.ctx, f.f)
		reopen.Close()
		if e != nil {
			t.Fatal("reopened retry failed")
		}
	}
	r2, d2 := f.assertRejected(t)
	if !reflect.DeepEqual(d, d2) || !r2.FinishedAt.Equal(stamp) {
		t.Fatal("retry changed stable history/time")
	}
	j, _ := f.jobs.Get(context.Background(), "app-run:run")
	if digest, _ := jobqueue.PayloadDigest(j.Payload); digest != f.f.PayloadDigest {
		t.Fatal("accepted payload changed")
	}
}
func TestAppPreflightPostgresAtomicRollbackFaults(t *testing.T) {
	for _, mode := range []string{"insert-error", "insert-skip", "update-error", "update-skip", "deferred-error", "rewrite-row"} {
		t.Run(mode, func(t *testing.T) {
			f := newPreflightPGFixture(t, time.Now().UTC().Truncate(time.Microsecond))
			table, event, body, deferred := "app_deploys", "INSERT", "RAISE EXCEPTION 'synthetic-storage-canary';", false
			switch mode {
			case "insert-skip":
				body = "RETURN NULL;"
			case "update-error":
				table, event = "pipeline_runs", "UPDATE"
			case "update-skip":
				table, event, body = "pipeline_runs", "UPDATE", "RETURN NULL;"
			case "deferred-error":
				table, event, deferred = "pipeline_runs", "UPDATE", true
			case "rewrite-row":
				body = "NEW.kind := 'deploy'; RETURN NEW;"
			}
			if _, err := f.db.Exec(`CREATE FUNCTION fixture_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN ` + body + ` END $$`); err != nil {
				t.Fatal(err)
			}
			ddl := `CREATE TRIGGER fixture_fault BEFORE ` + event + ` ON ` + table + ` FOR EACH ROW EXECUTE FUNCTION fixture_fault()`
			if deferred {
				ddl = `CREATE CONSTRAINT TRIGGER fixture_fault AFTER UPDATE ON pipeline_runs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fixture_fault()`
			}
			if _, err := f.db.Exec(ddl); err != nil {
				t.Fatal(err)
			}
			if err := NewAppDeployStore(f.db).RecordAppPreflightFailure(f.ctx, f.f); err == nil {
				t.Fatal("faulted transaction reported success")
			}
			f.assertPendingNoHistory(t)
		})
	}
}
func TestAppPreflightPostgresInvalidIdentityAndEffects(t *testing.T) {
	for _, mode := range []string{"no-claim", "wrong-owner", "wrong-kind", "wrong-commit", "wrong-app", "wrong-time", "wrong-actor", "wrong-pipeline", "success", "attention", "cancelled", "started", "stage-running", "stage-finished", "stage-output", "stage-artifact", "stage-log", "durable-log", "deleted-app", "history-other-id", "history-multiple", "legacy"} {
		t.Run(mode, func(t *testing.T) {
			f := newPreflightPGFixture(t, time.Now().UTC().Truncate(time.Microsecond))
			ctx := context.Background()
			r, _ := NewRunStore(f.db).Get(ctx, f.f.RunID)
			switch mode {
			case "no-claim":
				f.ctx = ctx
			case "wrong-owner":
				f.ctx = store.WithRunClaim(ctx, "app-run:run", "other", nil)
			case "wrong-kind":
				_, _ = f.db.Exec(`UPDATE jobs SET kind='pipeline-run'`)
			case "wrong-commit":
				f.f.CommitSHA = strings.Repeat("b", 40)
			case "wrong-app":
				f.f.AppID = "wrong"
				f.f.PipelineID = "app-wrong-run"
			case "wrong-time":
				f.f.AcceptedAt = f.f.AcceptedAt.Add(time.Second)
			case "wrong-actor":
				f.f.Actor.StartedByUserSub = "other"
			case "wrong-pipeline":
				if err := NewPipelineStore(f.db).Create(ctx, &model.Pipeline{ID: "other", Name: "other"}); err != nil {
					t.Fatal(err)
				}
				if _, err := f.db.Exec(`UPDATE pipeline_runs SET pipeline_id='other'`); err != nil {
					t.Fatal(err)
				}
				r.PipelineID = "other"
			case "success":
				r.Status = model.RunStatusSuccess
			case "attention":
				r.Status = model.RunStatusNeedsAttention
			case "cancelled":
				r.Status = model.RunStatusCancelled
			case "started":
				stamp := time.Now()
				r.StartedAt = &stamp
			case "stage-running":
				r.StageRuns = []model.StageRun{{Status: model.RunStatusRunning}}
			case "stage-finished":
				stamp := time.Now()
				r.StageRuns = []model.StageRun{{Status: model.RunStatusPending, FinishedAt: &stamp}}
			case "stage-output":
				r.StageRuns = []model.StageRun{{Status: model.RunStatusPending, Outputs: map[string]string{"x": "effect"}}}
			case "stage-artifact":
				r.StageRuns = []model.StageRun{{Status: model.RunStatusPending, Artifacts: []model.Artifact{{Type: "image"}}}}
			case "stage-log":
				r.StageRuns = []model.StageRun{{Status: model.RunStatusPending, Logs: "effect"}}
			case "durable-log":
				if _, err := f.db.Exec(`INSERT INTO stage_log_streams(run_id,stage_id)VALUES('run','stage'); INSERT INTO stage_log_entries(run_id,stage_id,seq,timestamp,line)VALUES('run','stage',1,NOW(),'effect')`); err != nil {
					t.Fatal(err)
				}
			case "deleted-app":
				if _, err := f.db.Exec(`DELETE FROM apps`); err != nil {
					t.Fatal(err)
				}
			case "history-other-id", "history-multiple":
				if _, err := f.db.Exec(`INSERT INTO app_deploys(id,app_id,run_id,pipeline_id,status,kind)VALUES('other','app','run',$1,'failed','deploy')`, f.f.PipelineID); err != nil {
					t.Fatal(err)
				}
				if mode == "history-multiple" {
					if _, err := f.db.Exec(`INSERT INTO app_deploys(id,app_id,run_id,pipeline_id,status,kind)VALUES($1,'app','run',$2,'failed','rejected')`, store.AppPreflightHistoryID("run"), f.f.PipelineID); err != nil {
						t.Fatal(err)
					}
				}
			case "legacy":
				stamp := time.Now().UTC().Truncate(time.Microsecond)
				r.Status, r.Error, r.FinishedAt = model.RunStatusFailed, store.AppPreflightRegistrySummary, &stamp
			}

			if err := NewRunStore(f.db).Update(ctx, r); err != nil {
				t.Fatal(err)
			}
			before, _ := NewRunStore(f.db).Get(ctx, f.f.RunID)
			var count int
			_ = f.db.QueryRow(`SELECT count(*) FROM app_deploys`).Scan(&count)
			err := NewAppDeployStore(f.db).RecordAppPreflightFailure(f.ctx, f.f)
			if mode == "legacy" {
				if err != nil {
					t.Fatal("known legacy repair failed")
				}
				got, _ := f.assertRejected(t)
				if !got.FinishedAt.Equal(*before.FinishedAt) {
					t.Fatal("legacy time overwritten")
				}
				return
			}
			if err == nil {
				t.Fatal("invalid identity/effect state admitted")
			}
			after, _ := NewRunStore(f.db).Get(ctx, "run")
			var n int
			_ = f.db.QueryRow(`SELECT count(*) FROM app_deploys`).Scan(&n)
			if !reflect.DeepEqual(before, after) || n != count {
				t.Fatal("rejected operation changed state")
			}
		})
	}
}
func TestAppPreflightPostgresRecoveryPreservesExactMarker(t *testing.T) {
	for _, mode := range []string{"complete-marker", "absent-marker", "wrong-kind", "artifact", "multiple", "durable-log", "invalid-envelope", "wrong-version", "null-version", "missing-version"} {
		t.Run(mode, func(t *testing.T) {
			f := newPreflightPGFixture(t, time.Now().UTC().Truncate(time.Microsecond))
			if err := NewAppDeployStore(f.db).RecordAppPreflightFailure(f.ctx, f.f); err != nil {
				t.Fatal(err)
			}
			before, d := f.assertRejected(t)
			switch mode {
			case "wrong-version":
				_, _ = f.db.Exec(`UPDATE jobs SET payload=jsonb_set(payload,'{version}','2'::jsonb)`)
			case "null-version":
				_, _ = f.db.Exec(`UPDATE jobs SET payload=jsonb_set(payload,'{version}','null'::jsonb)`)
			case "missing-version":
				_, _ = f.db.Exec(`UPDATE jobs SET payload=payload-'version'`)
			case "invalid-envelope":
				_, _ = f.db.Exec(`UPDATE jobs SET payload=jsonb_set(payload,'{commit}','"invalid"'::jsonb)`)
			case "absent-marker":
				_, _ = f.db.Exec(`DELETE FROM app_deploys`)
			case "wrong-kind":
				_, _ = f.db.Exec(`UPDATE app_deploys SET kind='deploy'`)
			case "artifact":
				_, _ = f.db.Exec(`UPDATE app_deploys SET digest='sha256:unknown'`)
			case "multiple":
				_, _ = f.db.Exec(`INSERT INTO app_deploys(id,app_id,run_id,pipeline_id,status,kind)VALUES('other','app','run',$1,'failed','deploy')`, f.f.PipelineID)
			case "durable-log":
				_, _ = f.db.Exec(`INSERT INTO stage_log_streams(run_id,stage_id)VALUES('run','stage'); INSERT INTO stage_log_entries(run_id,stage_id,seq,timestamp,line)VALUES('run','stage',1,NOW(),'effect')`)
			}
			recovered, recoveryErr := f.jobs.RecoverExpired(context.Background(), time.Now().Add(time.Second))
			invalidTyped := mode == "artifact" || mode == "multiple" || mode == "durable-log" || mode == "invalid-envelope" || mode == "wrong-version" || mode == "null-version" || mode == "missing-version"
			if invalidTyped {
				if recoveryErr == nil {
					t.Fatal("invalid typed rejection silently hidden")
				}
				job, _ := f.jobs.Get(context.Background(), "app-run:run")
				after, _ := NewRunStore(f.db).Get(context.Background(), f.f.RunID)
				if job.Status != jobqueue.StatusRunning || !reflect.DeepEqual(before, after) {
					t.Fatal("invalid marker transition did not roll back")
				}
				return
			}
			if recoveryErr != nil || len(recovered) != 1 {
				t.Fatal("actual expiry failed")
			}
			job, _ := f.jobs.Get(context.Background(), "app-run:run")
			r, _ := NewRunStore(f.db).Get(context.Background(), f.f.RunID)
			if job.Status != jobqueue.StatusNeedsAttention || job.Attempts != 1 {
				t.Fatal("queue uncertainty hidden")
			}
			if mode == "complete-marker" {
				got, h := f.assertRejected(t)
				if !reflect.DeepEqual(before, got) || !reflect.DeepEqual(d, h) {
					t.Fatal("determinate business outcome overwritten")
				}
				if err := NewAppDeployStore(f.db).RecordAppPreflightFailure(f.ctx, f.f); !errors.Is(err, store.ErrConflict) {
					t.Fatal("stale claim rewrote outcome")
				}
				return
			}
			if r.Status != model.RunStatusNeedsAttention || r.FinishedAt != nil {
				t.Fatal("partial marker fabricated determinate outcome")
			}
		})
	}
}
func TestAppPreflightPostgresCanceledJobLock(t *testing.T) {
	f := newPreflightPGFixture(t, time.Now().UTC().Truncate(time.Microsecond))
	tx, err := f.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SELECT id FROM jobs WHERE id='app-run:run' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(f.ctx, 30*time.Millisecond)
	defer cancel()
	if err := NewAppDeployStore(f.db).RecordAppPreflightFailure(ctx, f.f); err == nil {
		t.Fatal("canceled wait admitted")
	}
	tx.Rollback()
	f.assertPendingNoHistory(t)
}
