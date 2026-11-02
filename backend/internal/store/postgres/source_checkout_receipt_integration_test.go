//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

func sourceReceiptPostgresFixture(t *testing.T, db *sql.DB) (*RunStore, *jobqueue.Postgres, *jobqueue.Job, context.Context, *model.PipelineRunSource) {
	t.Helper()
	ctx := context.Background()
	id := uuid.NewString()
	pipelineID := "owned-p-" + id
	runID := "owned-r-" + id
	ts := time.Date(2026, 10, 3, 2, 3, 4, 123456789, time.UTC)
	if err := NewPipelineStore(db).Create(ctx, &model.Pipeline{ID: pipelineID, Name: "Owned source-only recorder"}); err != nil {
		t.Fatal(err)
	}
	runs := NewRunStore(db)
	r := &model.PipelineRun{ID: runID, PipelineID: pipelineID, PipelineVersion: 1, Status: model.RunStatusRunning, CreatedAt: ts, StartedAt: &ts, Source: postgresRunSource(), StartedByUserSub: "owned-admin", StartedByEmail: "fixture@invalid.local", StartedByGroups: []string{"admin"}, StartedByTokenHash: strings.Repeat("c", 64), Variables: map[string]string{"CONFIG_NAME": "owned"}, StageRuns: []model.StageRun{{StageID: "build", Status: model.RunStatusRunning, StartedAt: &ts, Logs: "already recorded evidence", Outputs: map[string]string{"proof": "preserve"}}}}
	if err := runs.Create(ctx, r); err != nil {
		t.Fatal(err)
	}
	jobs := jobqueue.NewPostgres(db)
	payload, _ := json.Marshal(map[string]string{"runId": runID})
	if _, err := jobs.Enqueue(ctx, "pipeline-run", payload, jobqueue.EnqueueOptions{ID: "run:" + runID, ConcurrencyKey: "pipeline:" + pipelineID}); err != nil {
		t.Fatal(err)
	}
	j, err := jobs.Dequeue(ctx, "owned-recorder-worker-"+id, []string{"pipeline-run"})
	if err != nil || j == nil || j.ID != "run:"+runID || j.Attempts != 1 {
		t.Fatal("actual SQL claim unavailable")
	}
	claim := store.WithRunClaim(ctx, j.ID, j.LockedBy, nil)
	observed := r.Source.Clone()
	checked := ts.Add(time.Minute)
	observed.CheckedOutAt = &checked
	return runs, jobs, j, claim, observed
}
func TestSourceCheckoutPostgresReceiptPreservesEvidenceAndReopen(t *testing.T) {
	db, dsn := registryAdmissionTestDB(t)
	runs, _, j, claim, observed := sourceReceiptPostgresFixture(t, db)
	runID := strings.TrimPrefix(j.ID, "run:")
	before, _ := runs.Get(claim, runID)
	if err := runs.RecordSourceCheckout(claim, runID, observed); err != nil {
		t.Fatal(err)
	}
	after, _ := runs.Get(claim, runID)
	want := before.Clone()
	want.Source = observed.Clone()
	if !reflect.DeepEqual(after, want) {
		t.Fatal("source-only SQL write changed unrelated evidence")
	}
	if err := runs.RecordSourceCheckout(claim, runID, observed); err != nil {
		t.Fatal("exact observed receipt not idempotent", err)
	}
	reopened, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	actual, err := NewRunStore(reopened).Get(context.Background(), runID)
	if err != nil || !reflect.DeepEqual(actual, want) {
		t.Fatal("reopen lost exact JSON nanosecond receipt")
	}
	original := *observed.CheckedOutAt
	*observed.CheckedOutAt = original.Add(time.Hour)
	observed.Source.Repository = "foreign/changed"
	*after.Source.CheckedOutAt = original.Add(time.Second)
	again, _ := runs.Get(claim, runID)
	if !reflect.DeepEqual(again, want) {
		t.Fatal("input/returned alias changed durable receipt")
	}
}
func TestSourceCheckoutPostgresReceiptAuthorityAndStateFences(t *testing.T) {
	db, _ := registryAdmissionTestDB(t)
	cases := []struct {
		name   string
		change func(*RunStore, *jobqueue.Postgres, *jobqueue.Job, context.Context, *model.PipelineRunSource) (context.Context, string, *model.PipelineRunSource)
	}{
		{"no-claim", func(r *RunStore, q *jobqueue.Postgres, j *jobqueue.Job, c context.Context, s *model.PipelineRunSource) (context.Context, string, *model.PipelineRunSource) {
			return context.Background(), strings.TrimPrefix(j.ID, "run:"), s
		}},
		{"wrong-owner", func(r *RunStore, q *jobqueue.Postgres, j *jobqueue.Job, c context.Context, s *model.PipelineRunSource) (context.Context, string, *model.PipelineRunSource) {
			return store.WithRunClaim(context.Background(), j.ID, "foreign", nil), strings.TrimPrefix(j.ID, "run:"), s
		}},
		{"wrong-job", func(r *RunStore, q *jobqueue.Postgres, j *jobqueue.Job, c context.Context, s *model.PipelineRunSource) (context.Context, string, *model.PipelineRunSource) {
			return store.WithRunClaim(context.Background(), "run:foreign", j.LockedBy, nil), strings.TrimPrefix(j.ID, "run:"), s
		}},
		{"wrong-run", func(r *RunStore, q *jobqueue.Postgres, j *jobqueue.Job, c context.Context, s *model.PipelineRunSource) (context.Context, string, *model.PipelineRunSource) {
			return c, "foreign", s
		}},
		{"stale-claim", func(r *RunStore, q *jobqueue.Postgres, j *jobqueue.Job, c context.Context, s *model.PipelineRunSource) (context.Context, string, *model.PipelineRunSource) {
			if err := q.Suspend(context.Background(), j.LockedBy, j.ID, "fixture revoked claim"); err != nil {
				t.Fatal(err)
			}
			return c, strings.TrimPrefix(j.ID, "run:"), s
		}},
		{"missing-time", func(r *RunStore, q *jobqueue.Postgres, j *jobqueue.Job, c context.Context, s *model.PipelineRunSource) (context.Context, string, *model.PipelineRunSource) {
			s.CheckedOutAt = nil
			return c, strings.TrimPrefix(j.ID, "run:"), s
		}},
		{"wrong-lineage", func(r *RunStore, q *jobqueue.Postgres, j *jobqueue.Job, c context.Context, s *model.PipelineRunSource) (context.Context, string, *model.PipelineRunSource) {
			s.SourcePlanDigest = strings.Repeat("d", 64)
			return c, strings.TrimPrefix(j.ID, "run:"), s
		}},
		{"missing-durable-source", func(r *RunStore, q *jobqueue.Postgres, j *jobqueue.Job, c context.Context, s *model.PipelineRunSource) (context.Context, string, *model.PipelineRunSource) {
			if _, err := db.Exec(`UPDATE pipeline_runs SET source=NULL WHERE id=$1`, strings.TrimPrefix(j.ID, "run:")); err != nil {
				t.Fatal(err)
			}
			return c, strings.TrimPrefix(j.ID, "run:"), s
		}},
		{"attention-run", func(r *RunStore, q *jobqueue.Postgres, j *jobqueue.Job, c context.Context, s *model.PipelineRunSource) (context.Context, string, *model.PipelineRunSource) {
			if err := r.UpdateStatus(c, strings.TrimPrefix(j.ID, "run:"), model.RunStatusNeedsAttention, nil, "uncertain"); err != nil {
				t.Fatal(err)
			}
			return c, strings.TrimPrefix(j.ID, "run:"), s
		}},
		{"terminal-run", func(r *RunStore, q *jobqueue.Postgres, j *jobqueue.Job, c context.Context, s *model.PipelineRunSource) (context.Context, string, *model.PipelineRunSource) {
			now := time.Now().UTC()
			if err := r.UpdateStatus(c, strings.TrimPrefix(j.ID, "run:"), model.RunStatusSuccess, &now, ""); err != nil {
				t.Fatal(err)
			}
			return c, strings.TrimPrefix(j.ID, "run:"), s
		}},
		{"changed-observation-time", func(r *RunStore, q *jobqueue.Postgres, j *jobqueue.Job, c context.Context, s *model.PipelineRunSource) (context.Context, string, *model.PipelineRunSource) {
			if err := r.RecordSourceCheckout(c, strings.TrimPrefix(j.ID, "run:"), s); err != nil {
				t.Fatal(err)
			}
			*s.CheckedOutAt = s.CheckedOutAt.Add(time.Nanosecond)
			return c, strings.TrimPrefix(j.ID, "run:"), s
		}},
		{"cancelled-context", func(r *RunStore, q *jobqueue.Postgres, j *jobqueue.Job, c context.Context, s *model.PipelineRunSource) (context.Context, string, *model.PipelineRunSource) {
			cancelled, cancel := context.WithCancel(c)
			cancel()
			return cancelled, strings.TrimPrefix(j.ID, "run:"), s
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runs, jobs, j, claim, source := sourceReceiptPostgresFixture(t, db)
			c, id, source := tc.change(runs, jobs, j, claim, source)
			runID := strings.TrimPrefix(j.ID, "run:")
			before, _ := runs.Get(context.Background(), runID)
			if err := runs.RecordSourceCheckout(c, id, source); err == nil {
				t.Fatal("contradictory source/claim accepted")
			}
			after, _ := runs.Get(context.Background(), runID)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("rejected receipt modified durable evidence")
			}
		})
	}
}
func TestSourceCheckoutPostgresReceiptConcurrentProgressAndHeartbeat(t *testing.T) {
	db, _ := registryAdmissionTestDB(t)
	runs, _, j, claim, observed := sourceReceiptPostgresFixture(t, db)
	runID := strings.TrimPrefix(j.ID, "run:")
	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for k := 0; k < 3; k++ {
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			for i := 0; i < 30; i++ {
				var err error
				switch k {
				case 0:
					err = runs.RecordSourceCheckout(claim, runID, observed)
				case 1:
					err = runs.UpdateProgress(claim, runID, []model.StageRun{{StageID: "build", Status: model.RunStatusRunning, Outputs: map[string]string{"iteration": strconv.Itoa(i)}}})
				case 2:
					err = runs.UpdateHeartbeat(claim, runID, observed.CheckedOutAt.Add(time.Duration(i)*time.Microsecond))
				}
				if err != nil {
					errs <- err
					return
				}
			}
		}(k)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	after, _ := runs.Get(claim, runID)
	if after.Source.CheckedOutAt == nil || !after.Source.CheckedOutAt.Equal(*observed.CheckedOutAt) || after.StageRuns[0].Outputs["iteration"] != "29" || after.HeartbeatAt == nil || !after.HeartbeatAt.Equal(observed.CheckedOutAt.Add(29*time.Microsecond).Round(time.Microsecond)) {
		t.Fatal("source recorder erased concurrent progress/heartbeat")
	}
}
func TestSourceCheckoutPostgresReceiptFaultRollbackAndRevokedLock(t *testing.T) {
	db, _ := registryAdmissionTestDB(t)
	faults := []struct {
		name, body string
		deferred   bool
	}{
		{"skipped-update", `RETURN NULL;`, false},
		{"rewritten-source", `NEW.source=jsonb_set(NEW.source,'{checkedOutAt}','"2026-10-04T00:00:00Z"');RETURN NEW;`, false},
		{"statement-error", `RAISE EXCEPTION 'owned fixture source update fault';`, false},
		{"deferred-commit-error", `RAISE EXCEPTION 'owned fixture source commit fault';`, true},
	}
	for _, f := range faults {
		t.Run(f.name, func(t *testing.T) {
			runs, _, j, claim, source := sourceReceiptPostgresFixture(t, db)
			runID := strings.TrimPrefix(j.ID, "run:")
			before, _ := runs.Get(claim, runID)
			if _, err := db.Exec(`CREATE FUNCTION owned_source_receipt_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN ` + f.body + ` END $$`); err != nil {
				t.Fatal(err)
			}
			ddl := `CREATE TRIGGER owned_source_receipt_fault BEFORE UPDATE OF source ON pipeline_runs FOR EACH ROW EXECUTE FUNCTION owned_source_receipt_fault()`
			if f.deferred {
				ddl = `CREATE CONSTRAINT TRIGGER owned_source_receipt_fault AFTER UPDATE ON pipeline_runs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION owned_source_receipt_fault()`
			}
			if _, err := db.Exec(ddl); err != nil {
				t.Fatal(err)
			}
			err := runs.RecordSourceCheckout(claim, runID, source)
			if _, dropErr := db.Exec(`DROP TRIGGER owned_source_receipt_fault ON pipeline_runs;DROP FUNCTION owned_source_receipt_fault()`); dropErr != nil {
				t.Fatal(dropErr)
			}
			if err == nil {
				t.Fatal("source persistence fault acknowledged false success")
			}
			after, _ := runs.Get(claim, runID)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("failed source acknowledgement changed durable receipt/evidence")
			}
		})
	}
	t.Run("cancelled-job-lock", func(t *testing.T) {
		runs, _, j, claim, s := sourceReceiptPostgresFixture(t, db)
		runID := strings.TrimPrefix(j.ID, "run:")
		before, _ := runs.Get(claim, runID)
		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err := tx.Exec(`SELECT 1 FROM jobs WHERE id=$1 FOR UPDATE`, j.ID); err != nil {
			t.Fatal(err)
		}
		wait, cancel := context.WithTimeout(claim, 100*time.Millisecond)
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- runs.RecordSourceCheckout(wait, runID, s) }()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("cancelled locked writer acknowledged source")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("cancelled source lock did not return")
		}
		if tx.Rollback() != nil {
			t.Fatal("owned lock release failed")
		}
		after, _ := runs.Get(context.Background(), runID)
		if !reflect.DeepEqual(before, after) {
			t.Fatal("canceled source writer modified evidence")
		}
	})
	t.Run("revocation-before-source-write", func(t *testing.T) {
		runs, jobs, j, claim, s := sourceReceiptPostgresFixture(t, db)
		runID := strings.TrimPrefix(j.ID, "run:")
		before, _ := runs.Get(claim, runID)
		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err := tx.Exec(`UPDATE jobs SET status='needs_attention',locked_by='' WHERE id=$1`, j.ID); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- runs.RecordSourceCheckout(claim, runID, s) }()
		select {
		case err := <-done:
			t.Fatalf("source writer bypassed in-flight revocation: %v", err)
		case <-time.After(50 * time.Millisecond):
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if err := <-done; !errors.Is(err, store.ErrConflict) {
			t.Fatal("revoked source writer regained authority")
		}
		after, _ := runs.Get(context.Background(), runID)
		if !reflect.DeepEqual(before, after) {
			t.Fatal("revoked source writer changed evidence")
		}
		actual, _ := jobs.Get(context.Background(), j.ID)
		if actual.Status != jobqueue.StatusNeedsAttention {
			t.Fatal("revocation fixture identity failed")
		}
	})
}
