//go:build integration

package scheduler_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/scheduler"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
)

func occurrenceFixture(t *testing.T) (*sql.DB, *scheduler.PostgresStore, *jobqueue.Postgres, store.PipelineStore, service.ScheduledOccurrence) {
	t.Helper()
	db := dialLiveScheduler(t)
	st := scheduler.NewPostgresStore(db)
	jq := jobqueue.NewPostgres(db)
	jq.OnEnqueue = scheduler.SyncOccurrenceAdmission
	pipelines := memory.New().Pipelines
	if err := pipelines.Create(context.Background(), &model.Pipeline{ID: "p", Name: "original", Variables: map[string]string{"REVISION": "original"}, Stages: []model.Stage{}, Edges: []model.Edge{}}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 2, 13, 30, 15, 0, time.UTC)
	if err := st.Create(context.Background(), scheduler.Schedule{ID: "s", PipelineID: "p", CronExpr: "* * * * *", Timezone: "UTC", NextRunAt: now.Add(-time.Minute), Enabled: true}); err != nil {
		t.Fatal(err)
	}
	s, err := st.Get(context.Background(), "s")
	if err != nil {
		t.Fatal(err)
	}
	o := service.ScheduledOccurrence{ScheduleID: s.ID, ScheduleVersion: s.Version, ScheduledFor: s.NextRunAt, PipelineID: s.PipelineID, CronExpr: s.CronExpr, Timezone: s.Timezone, AdmittedAt: now, NextRunAt: time.Date(2026, 10, 2, 13, 31, 0, 0, time.UTC)}
	return db, st, jq, pipelines, o
}

func assertOccurrenceState(t *testing.T, db *sql.DB, jobs, journal int) {
	t.Helper()
	var gotJobs, gotJournal int
	if err := db.QueryRow(`SELECT COUNT(*) FROM jobs`).Scan(&gotJobs); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM schedule_occurrences`).Scan(&gotJournal); err != nil {
		t.Fatal(err)
	}
	if gotJobs != jobs || gotJournal != journal {
		t.Fatalf("jobs=%d journal=%d, want %d/%d", gotJobs, gotJournal, jobs, journal)
	}
}

func TestLive_ScheduleOccurrenceRollsBackAfterJobAndJournal(t *testing.T) {
	db, st, jq, pipelines, occurrence := occurrenceFixture(t)
	ctx := context.Background()
	jq.OnEnqueue = func(ctx context.Context, tx *sql.Tx, j *jobqueue.Job) error {
		if err := scheduler.SyncOccurrenceAdmission(ctx, tx, j); err != nil {
			return err
		}
		return errors.New("injected failure after insert/journal/advance before commit")
	}
	enq := service.NewJobQueueEnqueuer(jq, pipelines)
	if err := enq.EnqueueScheduledRun(ctx, occurrence); err == nil {
		t.Fatal("wanted admission failure")
	}
	assertOccurrenceState(t, db, 0, 0)
	s, _ := st.Get(ctx, "s")
	if s.Version != 1 || !s.NextRunAt.Equal(occurrence.ScheduledFor) || s.LastRunID != "" {
		t.Fatalf("failed admission changed schedule: %+v", s)
	}
	jq.OnEnqueue = scheduler.SyncOccurrenceAdmission
	if err := enq.EnqueueScheduledRun(ctx, occurrence); err != nil {
		t.Fatal(err)
	}
	assertOccurrenceState(t, db, 1, 1)
}

func TestLive_ScheduleOccurrenceDatabaseErrorLeavesNoJob(t *testing.T) {
	db, st, jq, pipelines, occurrence := occurrenceFixture(t)
	if _, err := db.Exec(`
		CREATE FUNCTION reject_schedule_fire() RETURNS TRIGGER LANGUAGE plpgsql AS $$
		 BEGIN RAISE EXCEPTION 'injected schedule persistence failure'; END $$;
		CREATE TRIGGER fail_fire BEFORE UPDATE ON schedules
		 FOR EACH ROW EXECUTE FUNCTION reject_schedule_fire();`); err != nil {
		t.Fatal(err)
	}
	if err := service.NewJobQueueEnqueuer(jq, pipelines).EnqueueScheduledRun(context.Background(), occurrence); err == nil {
		t.Fatal("wanted actual PostgreSQL update error")
	}
	assertOccurrenceState(t, db, 0, 0)
	s, _ := st.Get(context.Background(), "s")
	if s.Version != 1 || s.LastRunID != "" || !s.NextRunAt.Equal(occurrence.ScheduledFor) {
		t.Fatal("database error advanced schedule")
	}
}

func TestLive_ScheduleOccurrenceCancelledAdmissionRollsBack(t *testing.T) {
	db, st, jq, pipelines, occurrence := occurrenceFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	jq.OnEnqueue = func(ctx context.Context, tx *sql.Tx, job *jobqueue.Job) error {
		if err := scheduler.SyncOccurrenceAdmission(ctx, tx, job); err != nil {
			return err
		}
		cancel()
		return ctx.Err()
	}
	if err := service.NewJobQueueEnqueuer(jq, pipelines).EnqueueScheduledRun(ctx, occurrence); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled admission=%v", err)
	}
	assertOccurrenceState(t, db, 0, 0)
	s, _ := st.Get(context.Background(), "s")
	if s.Version != 1 || !s.NextRunAt.Equal(occurrence.ScheduledFor) {
		t.Fatal("cancelled admission advanced schedule")
	}
}

func TestLive_ScheduleOccurrenceTamperedPinsRejectAtomically(t *testing.T) {
	for _, change := range []string{"runId", "target", "nextSlot", "pendingRunVersion", "queueKey"} {
		t.Run(change, func(t *testing.T) {
			db, st, jq, pipelines, occurrence := occurrenceFixture(t)
			jq.OnEnqueue = func(ctx context.Context, tx *sql.Tx, job *jobqueue.Job) error {
				var payload map[string]any
				if err := json.Unmarshal(job.Payload, &payload); err != nil {
					return err
				}
				switch change {
				case "runId":
					payload["runId"] = "other"
				case "target":
					payload["scheduledOccurrence"].(map[string]any)["pipelineId"] = "other"
				case "nextSlot":
					payload["scheduledOccurrence"].(map[string]any)["nextRunAt"] = occurrence.NextRunAt.Add(time.Minute).Format(time.RFC3339Nano)
				case "pendingRunVersion":
					payload["run"].(map[string]any)["pipelineVersion"] = float64(99)
				case "queueKey":
					job.ConcurrencyKey = "unrelated"
				}
				data, err := json.Marshal(payload)
				if err != nil {
					return err
				}
				job.Payload = data
				return scheduler.SyncOccurrenceAdmission(ctx, tx, job)
			}
			if err := service.NewJobQueueEnqueuer(jq, pipelines).EnqueueScheduledRun(context.Background(), occurrence); !errors.Is(err, scheduler.ErrConflict) {
				t.Fatalf("tampered admission=%v", err)
			}
			assertOccurrenceState(t, db, 0, 0)
			s, _ := st.Get(context.Background(), "s")
			if s.Version != 1 || !s.NextRunAt.Equal(occurrence.ScheduledFor) {
				t.Fatal("tampered admission advanced schedule")
			}
		})
	}
}

func TestLive_ScheduleOccurrenceConcurrentAdmission(t *testing.T) {
	db, st, jq, pipelines, occurrence := occurrenceFixture(t)
	enq := service.NewJobQueueEnqueuer(jq, pipelines)
	var accepted atomic.Int32
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if enq.EnqueueScheduledRun(context.Background(), occurrence) == nil {
				accepted.Add(1)
			}
		}()
	}
	workers.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("successful admissions=%d", accepted.Load())
	}
	assertOccurrenceState(t, db, 1, 1)
	s, _ := st.Get(context.Background(), "s")
	if s.Version != 2 || s.LastRunID != occurrence.RunID() || !s.NextRunAt.Equal(occurrence.NextRunAt) {
		t.Fatalf("admitted state: %+v", s)
	}
}

func TestLive_ScheduleOccurrenceRejectsChangedAndDisabledSchedule(t *testing.T) {
	for _, edit := range []string{"pipeline", "cron", "timezone", "disable", "same-content-version"} {
		t.Run(edit, func(t *testing.T) {
			db, st, jq, pipelines, occurrence := occurrenceFixture(t)
			s, _ := st.Get(context.Background(), "s")
			switch edit {
			case "pipeline":
				s.PipelineID = "other"
			case "cron":
				s.CronExpr = "0 * * * *"
			case "timezone":
				s.Timezone = "Asia/Bangkok"
			case "disable":
				s.Enabled = false
			}
			if err := st.Update(context.Background(), s); err != nil {
				t.Fatal(err)
			}
			if err := service.NewJobQueueEnqueuer(jq, pipelines).EnqueueScheduledRun(context.Background(), occurrence); !errors.Is(err, scheduler.ErrConflict) {
				t.Fatalf("stale admission=%v", err)
			}
			assertOccurrenceState(t, db, 0, 0)
			current, _ := st.Get(context.Background(), "s")
			if current.Version != 2 || current.LastRunID != "" || !current.NextRunAt.Equal(s.NextRunAt) {
				t.Fatalf("edited schedule overwritten: %+v", current)
			}
		})
	}
}

func TestLive_ScheduleOccurrenceConcurrentEditCannotOverwriteFire(t *testing.T) {
	db, st, jq, pipelines, occurrence := occurrenceFixture(t)
	locked, release := make(chan struct{}), make(chan struct{})
	jq.OnEnqueue = func(ctx context.Context, tx *sql.Tx, job *jobqueue.Job) error {
		if err := scheduler.SyncOccurrenceAdmission(ctx, tx, job); err != nil {
			return err
		}
		close(locked)
		<-release
		return nil
	}
	s, _ := st.Get(context.Background(), "s")
	s.Enabled = false
	admitted := make(chan error, 1)
	go func() {
		admitted <- service.NewJobQueueEnqueuer(jq, pipelines).EnqueueScheduledRun(context.Background(), occurrence)
	}()
	<-locked
	edited := make(chan error, 1)
	go func() { edited <- st.Update(context.Background(), s) }()
	close(release)
	if err := <-admitted; err != nil {
		t.Fatal(err)
	}
	if err := <-edited; !errors.Is(err, scheduler.ErrConflict) {
		t.Fatalf("stale edit=%v", err)
	}
	assertOccurrenceState(t, db, 1, 1)
	current, _ := st.Get(context.Background(), "s")
	if current.Version != 2 || !current.Enabled || !current.NextRunAt.Equal(occurrence.NextRunAt) {
		t.Fatalf("edit overwrote occurrence: %+v", current)
	}
}

type lostAdmissionReply struct {
	*jobqueue.Postgres
	lose atomic.Bool
}

func (s *lostAdmissionReply) Enqueue(ctx context.Context, kind string, payload json.RawMessage, opts jobqueue.EnqueueOptions) (*jobqueue.Job, error) {
	job, err := s.Postgres.Enqueue(ctx, kind, payload, opts)
	if err == nil && !s.lose.Swap(true) {
		return nil, errors.New("injected lost acknowledgement after durable commit")
	}
	return job, err
}

func TestLive_ScheduleOccurrenceRestartAfterLostReply(t *testing.T) {
	db, st, jq, pipelines, occurrence := occurrenceFixture(t)
	enq := service.NewJobQueueEnqueuer(&lostAdmissionReply{Postgres: jq}, pipelines)
	if err := enq.EnqueueScheduledRun(context.Background(), occurrence); err == nil {
		t.Fatal("expected simulated lost reply")
	}
	assertOccurrenceState(t, db, 1, 1)
	// A restarted scheduler has fresh store/queue handles and sees advancement.
	freshStore := scheduler.NewPostgresStore(db)
	freshQueue := jobqueue.NewPostgres(db)
	freshQueue.OnEnqueue = scheduler.SyncOccurrenceAdmission
	runner, err := scheduler.NewRunner(scheduler.Options{DB: db, Store: freshStore, Enqueuer: service.NewJobQueueEnqueuer(freshQueue, pipelines), NowFn: func() time.Time { return occurrence.AdmittedAt }, TickEvery: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	_ = runner.Run(ctx)
	assertOccurrenceState(t, db, 1, 1)
	current, _ := st.Get(context.Background(), "s")
	if current.LastRunID != occurrence.RunID() || current.Version != 2 {
		t.Fatal("restart lost admission")
	}
	// A stale replica cannot use the retained job to advance an edited row.
	current.Enabled = false
	if err := freshStore.Update(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	if err := service.NewJobQueueEnqueuer(freshQueue, pipelines).EnqueueScheduledRun(context.Background(), occurrence); err == nil {
		t.Fatal("old occurrence unexpectedly accepted")
	}
	assertOccurrenceState(t, db, 1, 1)
	disabled, _ := st.Get(context.Background(), "s")
	if disabled.Enabled || disabled.Version != 3 {
		t.Fatal("old occurrence overwrote disabled schedule")
	}
}

func TestLive_ScheduleOccurrenceCancelledJobAndRetentionCannotReplay(t *testing.T) {
	db, st, jq, pipelines, occurrence := occurrenceFixture(t)
	enq := service.NewJobQueueEnqueuer(jq, pipelines)
	if err := enq.EnqueueScheduledRun(context.Background(), occurrence); err != nil {
		t.Fatal(err)
	}
	status, err := jq.Cancel(context.Background(), "run:"+occurrence.RunID())
	if err != nil || status != jobqueue.StatusCancelled {
		t.Fatalf("cancel status=%s error=%v", status, err)
	}
	if _, err := jq.DeleteOlderThan(context.Background(), time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	assertOccurrenceState(t, db, 0, 1)
	current, _ := st.Get(context.Background(), "s")
	current.NextRunAt = occurrence.ScheduledFor
	if err := st.Update(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	occurrence.ScheduleVersion = 3
	if err := enq.EnqueueScheduledRun(context.Background(), occurrence); err == nil {
		t.Fatal("retained journal did not reject old slot")
	}
	assertOccurrenceState(t, db, 0, 1)
	after, _ := st.Get(context.Background(), "s")
	if after.Version != 3 || !after.NextRunAt.Equal(occurrence.ScheduledFor) {
		t.Fatal("journal conflict partially advanced schedule")
	}
}

func TestLive_ScheduleOccurrencePinsPipelineAndPreservesManualAdmission(t *testing.T) {
	db, _, jq, pipelines, occurrence := occurrenceFixture(t)
	enq := service.NewJobQueueEnqueuer(jq, pipelines)
	if err := enq.EnqueueScheduledRun(context.Background(), occurrence); err != nil {
		t.Fatal(err)
	}
	p, _ := pipelines.Get(context.Background(), "p")
	p.Name = "edited"
	p.Variables = map[string]string{"REVISION": "edited"}
	if err := pipelines.Update(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	j, err := jq.Get(context.Background(), "run:"+occurrence.RunID())
	if err != nil {
		t.Fatal(err)
	}
	var plan struct {
		Pipeline *model.Pipeline              `json:"pipeline"`
		Run      *model.PipelineRun           `json:"run"`
		Slot     *service.ScheduledOccurrence `json:"scheduledOccurrence"`
	}
	if err := json.Unmarshal(j.Payload, &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Pipeline == nil || plan.Pipeline.Name != "original" || plan.Pipeline.Variables["REVISION"] != "original" || plan.Run == nil || plan.Run.PipelineVersion != plan.Pipeline.Version || plan.Slot == nil || plan.Slot.ScheduleVersion != 1 {
		t.Fatalf("queued snapshot changed: %+v", plan)
	}
	if err := enq.EnqueueRun(context.Background(), "p", "manual"); err != nil {
		t.Fatal(err)
	}
	assertOccurrenceState(t, db, 2, 1)
}

func TestLive_ScheduleOccurrenceMigrationPreservesLegacyState(t *testing.T) {
	db := dialLiveScheduler(t)
	if _, err := db.Exec(`DROP TABLE schedule_occurrences; ALTER TABLE schedules DROP COLUMN version;
	 INSERT INTO schedules(id,pipeline_id,cron_expr,timezone,next_run_at,last_run_id)
	 VALUES('legacy','p','* * * * *','UTC','2026-10-02T13:00:00Z','retained')`); err != nil {
		t.Fatal(err)
	}
	up, err := os.ReadFile("../store/postgres/migrations/034_schedule_occurrences.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(up)); err != nil {
		t.Fatal(err)
	}
	s, err := scheduler.NewPostgresStore(db).Get(context.Background(), "legacy")
	if err != nil || s.Version != 1 || s.LastRunID != "retained" {
		t.Fatalf("migration=%+v %v", s, err)
	}
	// Old readers retain their previous column projection after the addition.
	var id, pipeline, run string
	if err := db.QueryRow(`SELECT id,pipeline_id,last_run_id FROM schedules WHERE id='legacy'`).Scan(&id, &pipeline, &run); err != nil || id != "legacy" || pipeline != "p" || run != "retained" {
		t.Fatalf("legacy projection %q/%q/%q %v", id, pipeline, run, err)
	}
}
