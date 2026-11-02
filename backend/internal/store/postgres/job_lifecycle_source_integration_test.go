//go:build integration

package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// The harness creates this role/database on a dedicated internal Docker network.
// This suite must not write through an owner's generic integration connection.
func sourceLifecycleFixtureURL(dsn string) (*url.URL, error) {
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme != "postgres" || u.Hostname() != "cooker-source-lifecycle-pg" || u.Port() != "5432" || u.Path != "/cooker_source_lifecycle_test" || u.User == nil || u.User.Username() != "source_lifecycle_fixture" || u.Query().Get("sslmode") != "disable" || len(u.Query()) != 1 || len(u.Query()["sslmode"]) != 1 || u.Fragment != "" {
		return nil, errors.New("refusing non-owned source-lifecycle fixture")
	}
	if _, password := u.User.Password(); password {
		return nil, errors.New("fixture must not read a credential")
	}
	return u, nil
}

func sourceLifecycleTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("COOKER_SOURCE_LIFECYCLE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("owned disposable source-lifecycle PostgreSQL required")
	}
	u, err := sourceLifecycleFixtureURL(dsn)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	schema := "source_lifecycle_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		_, _ = admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(); _, _ = admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); admin.Close() })
	if err = applyMigrations(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

func reopenSourceLifecycleDB(t *testing.T, db *sql.DB) *sql.DB {
	t.Helper()
	var schema string
	if err := db.QueryRow(`SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	u, err := sourceLifecycleFixtureURL(os.Getenv("COOKER_SOURCE_LIFECYCLE_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	reopened, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reopened.Close() })
	if err := reopened.PingContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	return reopened
}

func sourceLifecyclePlan(t *testing.T, db *sql.DB) *recoveryJobPayload {
	t.Helper()
	p := &model.Pipeline{ID: "p", Name: "accepted source build", Source: &model.PipelineSourceConfig{ConnectionID: "approved", Repository: "fixture/native", FullRef: "refs/heads/main"}, Stages: []model.Stage{{ID: "build", Type: model.StageTypeBuild, Config: model.StageConfig{Context: ".", Dockerfile: "Dockerfile"}}}}
	p.SetDefaults()
	p.CreatedAt, p.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond), time.Now().UTC().Truncate(time.Microsecond)
	if err := NewPipelineStore(db).Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	// PostgreSQL assigns the initial version; Create intentionally does not
	// replace the caller's struct. Use the persisted accepted definition.
	p, err := NewPipelineStore(db).Get(context.Background(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	plan := &model.PipelineSourcePlan{Version: 1, PipelineID: p.ID, PipelineVersion: p.Version, ConnectionVersion: 2, Source: model.GitSourceIdentity{Provider: "github", ConnectionID: "approved", Host: "github.com", RepositoryID: "123", Repository: "fixture/native"}, FullRef: "refs/heads/main", CommitSHA: strings.Repeat("a", 40), EnvironmentVersions: map[string]int{}, ReviewPolicy: "manual", InputFiles: []model.SourceInputHash{{Path: ".dockerignore"}, {Path: "Dockerfile", Present: true, SHA256: strings.Repeat("b", 64)}}}
	sourceLifecycleBindPlan(p, plan)
	receipt, err := model.RunSourceFromAcceptedPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	run := &model.PipelineRun{ID: "r", PipelineID: p.ID, PipelineVersion: p.Version, Status: model.RunStatusPending, CreatedAt: time.Now().UTC().Truncate(time.Microsecond), Source: receipt, StartedByUserSub: "accepted-actor", StartedByEmail: "fixture@example.invalid", StartedByGroups: []string{"deployers"}, StartedByTokenHash: "non-secret-fixture-hash", Variables: map[string]string{"CONFIG": "plain"}, StageRuns: []model.StageRun{{StageID: "build", Status: model.RunStatusPending}}}
	payload := &recoveryJobPayload{RunID: run.ID, PipelineID: p.ID, Pipeline: p, Run: run, Source: plan}
	if _, err := recoveredPipelineSource(payload); err != nil {
		t.Fatal("baseline fixture must be valid before poison/recovery checks", err)
	}
	return payload
}

func sourceLifecycleBindPlan(p *model.Pipeline, plan *model.PipelineSourcePlan) {
	plan.PlanHash = model.PipelineSourceConfigDigest(p, plan)
	b, _ := json.Marshal(plan.InputFiles)
	hash := sha256.Sum256(b)
	plan.EnvelopeHash = hex.EncodeToString(hash[:])
}

func sourceLifecycleQueue(t *testing.T, db *sql.DB, payload any) (*jobqueue.Postgres, *jobqueue.Job) {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	jobs := jobqueue.NewPostgres(db)
	jobs.OnTransition = SyncJobRun
	j, err := jobs.Enqueue(context.Background(), "pipeline-run", b, jobqueue.EnqueueOptions{ID: "run:r", ConcurrencyKey: "pipeline:p", MaxAttempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	return jobs, j
}

func sourceLifecycleProof(t *testing.T, payload *recoveryJobPayload) {
	t.Helper()
	sourceLifecycleBindPlan(payload.Pipeline, payload.Source)
	receipt, err := model.RunSourceFromAcceptedPlan(payload.Source)
	if err != nil {
		t.Fatal(err)
	}
	payload.Run.Source = receipt
	payload.Trigger = &model.PipelineTriggerProof{IntentID: "intent", RuleID: "rule", RuleVersion: 3, TargetKind: "pipeline", TargetID: payload.Pipeline.ID, SourcePlanDigest: receipt.SourcePlanDigest, EnvelopeHash: payload.Source.EnvelopeHash}
}

func TestSourceLifecycleFixtureRefusesOwnerConnections(t *testing.T) {
	const owned = "postgres://source_lifecycle_fixture@cooker-source-lifecycle-pg:5432/cooker_source_lifecycle_test?sslmode=disable"
	if _, err := sourceLifecycleFixtureURL(owned); err != nil {
		t.Fatal(err)
	}
	for _, dsn := range []string{strings.Replace(owned, "cooker-source-lifecycle-pg", "localhost", 1), strings.Replace(owned, "source_lifecycle_fixture@", "owner@", 1), strings.Replace(owned, "/cooker_source_lifecycle_test", "/cooker", 1), owned + "&search_path=public", strings.Replace(owned, "source_lifecycle_fixture@", "source_lifecycle_fixture:secret-canary@", 1), owned + "#fragment", "malformed"} {
		if _, err := sourceLifecycleFixtureURL(dsn); err == nil || strings.Contains(err.Error(), "secret-canary") {
			t.Fatal("unsafe fixture DSN accepted or echoed")
		}
	}
}

func TestSourceLifecycleRecoversAcceptedReceiptAcrossRestartAndRepeat(t *testing.T) {
	for _, mode := range []string{"pending-cancel", "expired-claim", "review-selected-tag"} {
		t.Run(mode, func(t *testing.T) {
			db := sourceLifecycleTestDB(t)
			ctx := context.Background()
			payload := sourceLifecyclePlan(t, db)
			if mode == "review-selected-tag" {
				payload.Source.FullRef = "refs/tags/v1.0"
				payload.Source.ObjectSHA = strings.Repeat("c", 40)
				payload.Source.ReviewPolicy = "review"
				sourceLifecycleProof(t, payload)
			}
			want := payload.Run.Source.Clone()
			// Older accepted plans may omit the presentation receipt. Recovery
			// must derive it from the admitted plan without manufacturing checkout.
			payload.Run.Source = nil
			jobs, job := sourceLifecycleQueue(t, db, payload)
			acceptedDigest, _ := jobqueue.PayloadDigest(job.Payload)
			// Change the current definition/ref. Recovery uses existence only;
			// its lineage remains the captured accepted commit/tag and version.
			current, err := NewPipelineStore(db).Get(ctx, "p")
			if err != nil {
				t.Fatal(err)
			}
			current.Source.FullRef = "refs/heads/new-current-ref"
			if err = NewPipelineStore(db).Update(ctx, current); err != nil {
				t.Fatal(err)
			}
			wantStatus := model.RunStatusCancelled
			if mode != "pending-cancel" {
				if _, err = jobs.Dequeue(ctx, "lost-worker", []string{"pipeline-run"}); err != nil {
					t.Fatal(err)
				}
				if _, err = db.Exec(`UPDATE jobs SET locked_at=NOW()-INTERVAL '2 minutes' WHERE id=$1`, job.ID); err != nil {
					t.Fatal(err)
				}
				wantStatus = model.RunStatusNeedsAttention
			}
			// A fresh pool models recovery after process restart, not merely an
			// in-memory pointer retained by the admitting process.
			reopened := reopenSourceLifecycleDB(t, db)
			jobs = jobqueue.NewPostgres(reopened)
			jobs.OnTransition = SyncJobRun
			if mode == "pending-cancel" {
				for repeat := 0; repeat < 2; repeat++ {
					if status, err := jobs.Cancel(ctx, job.ID); err != nil || status != jobqueue.StatusCancelled {
						t.Fatal("cancel recovery failed", status, err)
					}
				}
			} else {
				if rows, err := jobs.RecoverExpired(ctx, time.Now().Add(-time.Minute)); err != nil || len(rows) != 1 {
					t.Fatal("expired claim recovery failed", err)
				}
				if rows, err := jobs.RecoverExpired(ctx, time.Now()); err != nil || len(rows) != 0 {
					t.Fatal("repeat recovery changed accepted work", err)
				}
			}
			got, err := NewRunStore(reopened).Get(ctx, "r")
			if err != nil || got.Status != wantStatus || !reflect.DeepEqual(got.Source, want) || got.Source.CheckedOutAt != nil || got.PipelineVersion != payload.Run.PipelineVersion || got.StartedByUserSub != payload.Run.StartedByUserSub || !got.CreatedAt.Equal(payload.Run.CreatedAt) {
				t.Fatal("recovery lost accepted source/actor or claimed checkout", got, err)
			}
			if mode != "pending-cancel" {
				if err := jobs.Resolve(ctx, job.ID, jobqueue.StatusFailed, "fixture operator verified the accepted external outcome"); err != nil {
					t.Fatal(err)
				}
				got, err = NewRunStore(reopenSourceLifecycleDB(t, db)).Get(ctx, "r")
				if err != nil || got.Status != model.RunStatusFailed || !reflect.DeepEqual(got.Source, want) || got.Source.CheckedOutAt != nil {
					t.Fatal("resolution replaced receipt or manufactured checkout", got, err)
				}
			}
			after, err := jobs.Get(ctx, job.ID)
			if err != nil {
				t.Fatal(err)
			}
			afterDigest, _ := jobqueue.PayloadDigest(after.Payload)
			if acceptedDigest != afterDigest {
				t.Fatal("lifecycle repinned accepted payload")
			}
		})
	}
}

func TestSourceLifecyclePreservesObservedCheckoutAndConcurrentRecovery(t *testing.T) {
	for _, observed := range []bool{false, true} {
		t.Run(fmt.Sprintf("observed-%t", observed), func(t *testing.T) {
			db := sourceLifecycleTestDB(t)
			ctx := context.Background()
			payload := sourceLifecyclePlan(t, db)
			stored := payload.Run.Clone()
			checked := time.Now().UTC().Truncate(time.Microsecond)
			if observed {
				stored.Source.CheckedOutAt = &checked
			} else {
				stored.Source = nil // A view created before the receipt write.
			}
			if err := NewRunStore(db).Create(ctx, stored); err != nil {
				t.Fatal(err)
			}
			jobs, job := sourceLifecycleQueue(t, db, payload)
			errors := make(chan error, 8)
			var wg sync.WaitGroup
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func() { defer wg.Done(); _, err := jobs.Cancel(ctx, job.ID); errors <- err }()
			}
			wg.Wait()
			close(errors)
			for err := range errors {
				if err != nil {
					t.Fatal(err)
				}
			}
			got, err := NewRunStore(reopenSourceLifecycleDB(t, db)).Get(ctx, "r")
			want := payload.Run.Source.Clone()
			if observed {
				want.CheckedOutAt = &checked
			}
			if err != nil || got.Status != model.RunStatusCancelled || !reflect.DeepEqual(got.Source, want) {
				t.Fatal("concurrent recovery replaced observation or source", got, err)
			}
			var count int
			if err := db.QueryRow(`SELECT count(*) FROM pipeline_runs WHERE id='r'`).Scan(&count); err != nil || count != 1 {
				t.Fatal("recovery duplicated run view", count, err)
			}
		})
	}
}

func TestSourceLifecycleRejectsPoisonedAcceptedMetadataAtomically(t *testing.T) {
	mutations := []struct {
		name string
		edit func(*recoveryJobPayload)
	}{
		{"missing-pipeline", func(p *recoveryJobPayload) { p.Pipeline = nil }},
		{"missing-run", func(p *recoveryJobPayload) { p.Run = nil }},
		{"missing-source-plan", func(p *recoveryJobPayload) { p.Source = nil }},
		{"unbound-pipeline", func(p *recoveryJobPayload) { p.Pipeline.Source = nil }},
		{"pipeline-identity", func(p *recoveryJobPayload) { p.Pipeline.ID = "another" }},
		{"pipeline-version", func(p *recoveryJobPayload) { p.Pipeline.Version++ }},
		{"run-identity", func(p *recoveryJobPayload) { p.Run.ID = "another" }},
		{"run-pipeline", func(p *recoveryJobPayload) { p.Run.PipelineID = "another" }},
		{"run-version", func(p *recoveryJobPayload) { p.Run.PipelineVersion++ }},
		{"run-started", func(p *recoveryJobPayload) { now := time.Now(); p.Run.StartedAt = &now }},
		{"stage-started", func(p *recoveryJobPayload) { now := time.Now(); p.Run.StageRuns[0].StartedAt = &now }},
		{"stage-success", func(p *recoveryJobPayload) { p.Run.StageRuns[0].Status = model.RunStatusSuccess }},
		{"admission-checkout-claim", func(p *recoveryJobPayload) { now := time.Now(); p.Run.Source.CheckedOutAt = &now }},
		{"run-source-commit", func(p *recoveryJobPayload) { p.Run.Source.CommitSHA = strings.Repeat("d", 40) }},
		{"run-source-digest", func(p *recoveryJobPayload) { p.Run.Source.SourcePlanDigest = strings.Repeat("d", 64) }},
		{"source-plan-version", func(p *recoveryJobPayload) { p.Source.Version = 0 }},
		{"source-pipeline-identity", func(p *recoveryJobPayload) { p.Source.PipelineID = "another" }},
		{"source-pipeline-version", func(p *recoveryJobPayload) { p.Source.PipelineVersion++ }},
		{"source-connection-version", func(p *recoveryJobPayload) { p.Source.ConnectionVersion = 0 }},
		{"source-connection", func(p *recoveryJobPayload) { p.Source.Source.ConnectionID = "another" }},
		{"source-repository", func(p *recoveryJobPayload) { p.Source.Source.Repository = "another/repo" }},
		{"source-ref", func(p *recoveryJobPayload) { p.Source.FullRef = "untyped-ref-canary" }},
		{"source-commit", func(p *recoveryJobPayload) { p.Source.CommitSHA = "provider-token-canary" }},
		{"source-plan-hash", func(p *recoveryJobPayload) { p.Source.PlanHash = strings.Repeat("c", 64) }},
		{"source-input-envelope", func(p *recoveryJobPayload) { p.Source.InputFiles[1].SHA256 = strings.Repeat("c", 64) }},
		{"source-input-traversal", func(p *recoveryJobPayload) {
			p.Source.InputFiles[1].Path = "../Dockerfile"
			sourceLifecycleBindPlan(p.Pipeline, p.Source)
		}},
		{"source-input-metadata", func(p *recoveryJobPayload) {
			p.Source.InputFiles[1].Path = ".git/config"
			sourceLifecycleBindPlan(p.Pipeline, p.Source)
		}},
		{"source-environments", func(p *recoveryJobPayload) { p.Source.EnvironmentVersions["extra"] = 1 }},
		{"private-registry-environment", func(p *recoveryJobPayload) {
			p.Pipeline.Stages[0].EnvironmentID = "registry_internal"
			p.Source.EnvironmentVersions["registry_internal"] = 1
			sourceLifecycleBindPlan(p.Pipeline, p.Source)
		}},
		{"stripped-review-proof", func(p *recoveryJobPayload) {
			p.Source.ReviewPolicy = "review"
			sourceLifecycleBindPlan(p.Pipeline, p.Source)
		}},
		{"proof-target", func(p *recoveryJobPayload) {
			p.Source.ReviewPolicy = "automatic"
			sourceLifecycleProof(t, p)
			p.Trigger.TargetID = "another"
		}},
		{"proof-digest", func(p *recoveryJobPayload) {
			p.Source.ReviewPolicy = "automatic"
			sourceLifecycleProof(t, p)
			p.Trigger.SourcePlanDigest = strings.Repeat("d", 64)
		}},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			db := sourceLifecycleTestDB(t)
			ctx := context.Background()
			payload := sourceLifecyclePlan(t, db)
			mutation.edit(payload)
			jobs, job := sourceLifecycleQueue(t, db, payload)
			_, err := jobs.Cancel(ctx, job.ID)
			if err == nil || strings.Contains(err.Error(), "canary") {
				t.Fatal("poison was accepted or exposed", err)
			}
			unchanged, err := jobs.Get(ctx, job.ID)
			if err != nil || unchanged.Status != jobqueue.StatusPending {
				t.Fatal("failed domain validation partially committed job", unchanged, err)
			}
			var rows int
			if err := db.QueryRow(`SELECT count(*) FROM pipeline_runs`).Scan(&rows); err != nil || rows != 0 {
				t.Fatal("poisoned recovery created a run view", rows, err)
			}
		})
	}
}

func TestSourceLifecycleRejectsStoredLineageOrActorMismatchBeforeTransition(t *testing.T) {
	for _, mutation := range []string{"commit", "digest", "actor", "groups", "email", "token-hash", "version", "pipeline", "malformed-source", "missing-accepted-plan"} {
		t.Run(mutation, func(t *testing.T) {
			db := sourceLifecycleTestDB(t)
			ctx := context.Background()
			payload := sourceLifecyclePlan(t, db)
			stored := payload.Run.Clone()
			checked := time.Now().UTC().Truncate(time.Microsecond)
			stored.Source.CheckedOutAt = &checked
			switch mutation {
			case "commit":
				stored.Source.CommitSHA = strings.Repeat("c", 40)
			case "digest":
				stored.Source.SourcePlanDigest = strings.Repeat("c", 64)
			case "actor":
				stored.StartedByUserSub = "changed-actor"
			case "groups":
				stored.StartedByGroups = []string{"changed-group"}
			case "email":
				stored.StartedByEmail = "changed@example.invalid"
			case "token-hash":
				stored.StartedByTokenHash = "changed-hash"
			case "version":
				stored.PipelineVersion++
			case "pipeline":
				if err := NewPipelineStore(db).Create(ctx, &model.Pipeline{ID: "other", Name: "other"}); err != nil {
					t.Fatal(err)
				}
				stored.PipelineID = "other"
			case "missing-accepted-plan":
				payload.Pipeline, payload.Source, payload.Run = nil, nil, nil
			}
			if err := NewRunStore(db).Create(ctx, stored); err != nil {
				t.Fatal(err)
			}
			if mutation == "malformed-source" {
				if _, err := db.Exec(`UPDATE pipeline_runs SET source='{"source":{"provider":"private-canary"}}' WHERE id='r'`); err != nil {
					t.Fatal(err)
				}
			}
			var before []byte
			if err := db.QueryRow(`SELECT source FROM pipeline_runs WHERE id='r'`).Scan(&before); err != nil {
				t.Fatal(err)
			}
			jobs, job := sourceLifecycleQueue(t, db, payload)
			if _, err := jobs.Cancel(ctx, job.ID); err == nil || strings.Contains(err.Error(), "canary") {
				t.Fatal("stored mismatch accepted or exposed", err)
			}
			var after []byte
			var status string
			if err := db.QueryRow(`SELECT status,source FROM pipeline_runs WHERE id='r'`).Scan(&status, &after); err != nil || status != "pending" || string(after) != string(before) {
				t.Fatal("rejected lineage partially updated source/lifecycle", err)
			}
			unchanged, err := jobs.Get(ctx, job.ID)
			if err != nil || unchanged.Status != jobqueue.StatusPending {
				t.Fatal("rejected lineage partially committed job", unchanged, err)
			}
		})
	}
}

func TestSourceLifecycleLegacyAndDeletedPipelinesStayUnknownAndAbsent(t *testing.T) {
	for _, mode := range []string{"legacy-id-only", "legacy-captured-run", "deleted-source-pipeline"} {
		t.Run(mode, func(t *testing.T) {
			db := sourceLifecycleTestDB(t)
			ctx := context.Background()
			payload := sourceLifecyclePlan(t, db)
			var accepted any = payload
			if mode != "deleted-source-pipeline" {
				payload.Run.Source = nil
				if mode == "legacy-id-only" {
					if err := NewRunStore(db).Create(ctx, payload.Run); err != nil {
						t.Fatal(err)
					}
					accepted = map[string]string{"runId": "r"}
				} else {
					payload.Pipeline, payload.Source = nil, nil
				}
			}
			jobs, job := sourceLifecycleQueue(t, db, accepted)
			if mode == "deleted-source-pipeline" {
				if _, err := db.Exec(`DELETE FROM pipelines WHERE id='p'`); err != nil {
					t.Fatal(err)
				}
			}
			if status, err := jobs.Cancel(ctx, job.ID); err != nil || status != jobqueue.StatusCancelled {
				t.Fatal("legacy/deleted cancellation failed", status, err)
			}
			got, err := NewRunStore(db).Get(ctx, "r")
			if mode == "deleted-source-pipeline" {
				if !errors.Is(err, store.ErrNotFound) {
					t.Fatal("deleted source view resurrected", err)
				}
				if _, err := NewPipelineStore(db).Get(ctx, "p"); !errors.Is(err, store.ErrNotFound) {
					t.Fatal("deleted definition resurrected", err)
				}
			} else if err != nil || got.Status != model.RunStatusCancelled || got.Source != nil {
				t.Fatal("legacy run inferred source from current pipeline", got, err)
			}
		})
	}
}

func TestSourceLifecycleRollsBackRecoveryAndResolutionOnStoredMismatch(t *testing.T) {
	for _, mode := range []string{"expired-claim", "operator-resolution"} {
		t.Run(mode, func(t *testing.T) {
			db := sourceLifecycleTestDB(t)
			ctx := context.Background()
			payload := sourceLifecyclePlan(t, db)
			stored := payload.Run.Clone()
			checked := time.Now().UTC().Truncate(time.Microsecond)
			stored.Source.CheckedOutAt, stored.StartedAt, stored.Status = &checked, &checked, model.RunStatusRunning
			if err := NewRunStore(db).Create(ctx, stored); err != nil {
				t.Fatal(err)
			}
			jobs, job := sourceLifecycleQueue(t, db, payload)
			if _, err := jobs.Dequeue(ctx, "lost-worker", []string{"pipeline-run"}); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`UPDATE jobs SET locked_at=NOW()-INTERVAL '2 minutes' WHERE id=$1`, job.ID); err != nil {
				t.Fatal(err)
			}
			wantJob, wantRun := jobqueue.StatusRunning, model.RunStatusRunning
			if mode == "operator-resolution" {
				if recovered, err := jobs.RecoverExpired(ctx, time.Now().Add(-time.Minute)); err != nil || len(recovered) != 1 {
					t.Fatal("baseline recovery failed", err)
				}
				wantJob, wantRun = jobqueue.StatusNeedsAttention, model.RunStatusNeedsAttention
			}
			// Deliberate fixture-only corruption tests the independent lifecycle
			// read guard; the normal RunStore rejects replacement of lineage.
			if _, err := db.Exec(`UPDATE pipeline_runs SET source=jsonb_set(source,'{commitSha}',to_jsonb($1::text)) WHERE id='r'`, strings.Repeat("d", 40)); err != nil {
				t.Fatal(err)
			}
			var before []byte
			if err := db.QueryRow(`SELECT source FROM pipeline_runs WHERE id='r'`).Scan(&before); err != nil {
				t.Fatal(err)
			}
			reopened := reopenSourceLifecycleDB(t, db)
			jobs = jobqueue.NewPostgres(reopened)
			jobs.OnTransition = SyncJobRun
			var transitionErr error
			if mode == "expired-claim" {
				_, transitionErr = jobs.RecoverExpired(ctx, time.Now().Add(-time.Minute))
			} else {
				transitionErr = jobs.Resolve(ctx, job.ID, jobqueue.StatusSucceeded, "fixture operator checked the exact external outcome")
			}
			if transitionErr == nil {
				t.Fatal("corrupted receipt accepted by recovery/resolution")
			}
			unchanged, err := jobs.Get(ctx, job.ID)
			if err != nil || unchanged.Status != wantJob || mode == "expired-claim" && unchanged.LockedBy != "lost-worker" {
				t.Fatal("rejected lifecycle partially committed queue/revocation", unchanged, err)
			}
			var after []byte
			var status string
			if err := db.QueryRow(`SELECT status,source FROM pipeline_runs WHERE id='r'`).Scan(&status, &after); err != nil || model.RunStatus(status) != wantRun || string(before) != string(after) {
				t.Fatal("rejected lifecycle partially committed run/source", err)
			}
		})
	}
}

func TestSourceLifecycleProjectsOnlyAllowedReceiptFields(t *testing.T) {
	db := sourceLifecycleTestDB(t)
	ctx := context.Background()
	payload := sourceLifecyclePlan(t, db)
	b, _ := json.Marshal(payload)
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	canaries := []string{"provider-token-canary", "private-environment-canary", "/tmp/private-checkout-canary", "raw-source-file-canary"}
	for _, value := range []map[string]any{raw["source"].(map[string]any), raw["run"].(map[string]any)["source"].(map[string]any)} {
		value["accessToken"], value["environmentValue"], value["checkoutPath"], value["contents"] = canaries[0], canaries[1], canaries[2], canaries[3]
	}
	jobs, job := sourceLifecycleQueue(t, db, raw)
	if _, err := jobs.Cancel(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	var persisted []byte
	if err := db.QueryRow(`SELECT source FROM pipeline_runs WHERE id='r'`).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	got, err := NewRunStore(reopenSourceLifecycleDB(t, db)).GetSummary(ctx, "r")
	if err != nil || got.Source == nil || got.Source.CheckedOutAt != nil {
		t.Fatal("projection missing or claims checkout", err)
	}
	public, _ := json.Marshal(got.Source)
	for _, canary := range canaries {
		if strings.Contains(string(persisted), canary) || strings.Contains(string(public), canary) || strings.Contains(got.Error, canary) {
			t.Fatal("typed receipt projection persisted or exposed an unknown private field")
		}
	}
	if !reflect.DeepEqual(got.Source, payload.Run.Source) {
		t.Fatal("unknown source fields changed safe lineage")
	}
}
