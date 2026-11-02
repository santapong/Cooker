//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

func postgresRunSource() *model.PipelineRunSource {
	return &model.PipelineRunSource{Source: model.GitSourceIdentity{Provider: "github", ConnectionID: "approved", Host: "github.com", RepositoryID: "123", Repository: "owned/native"}, FullRef: "refs/heads/main", CommitSHA: strings.Repeat("a", 40), SourcePlanDigest: strings.Repeat("b", 64)}
}

func reopenRunSourceDB(t *testing.T, db *sql.DB) *sql.DB {
	t.Helper()
	var schema string
	if err := db.QueryRow(`SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(os.Getenv("COOKER_INTEGRATION_DATABASE_URL"))
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

func TestRunSourcePostgresPersistenceProgressAndLegacyQueries(t *testing.T) {
	db := claimTestDB(t)
	ctx := context.Background()
	if err := NewPipelineStore(db).Create(ctx, &model.Pipeline{ID: "p", Name: "current definition"}); err != nil {
		t.Fatal(err)
	}
	runs := NewRunStore(db)
	r := &model.PipelineRun{ID: "source-run", PipelineID: "p", Status: model.RunStatusPending, Source: postgresRunSource(), StageRuns: []model.StageRun{{StageID: "build", Logs: "private-log-canary"}}}
	if err := runs.Create(ctx, r); err != nil {
		t.Fatal(err)
	}
	if r.Source.CheckedOutAt != nil {
		t.Fatal("store generated an unverified checkout time")
	}
	checked := time.Now().UTC().Truncate(time.Microsecond)
	r.Source.CheckedOutAt = &checked
	r.Status = model.RunStatusRunning
	if err := runs.Update(ctx, r); err != nil {
		t.Fatal(err)
	}
	// Progress/status/heartbeat and legacy full update must preserve source.
	if err := runs.UpdateProgress(ctx, r.ID, []model.StageRun{{StageID: "build", Status: model.RunStatusSuccess, Logs: "private-log-canary"}}); err != nil {
		t.Fatal(err)
	}
	if err := runs.UpdateHeartbeat(ctx, r.ID, checked); err != nil {
		t.Fatal(err)
	}
	if err := runs.UpdateStatus(ctx, r.ID, model.RunStatusSuccess, &checked, ""); err != nil {
		t.Fatal(err)
	}
	legacy, err := runs.Get(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	legacy.Source = nil
	if err := runs.Update(ctx, legacy); err != nil {
		t.Fatal("nil legacy source update lost compatibility", err)
	}
	unchecked := legacy.Clone()
	unchecked.Source = r.Source.Clone()
	unchecked.Source.CheckedOutAt = nil
	if err := runs.Update(ctx, unchecked); err != nil {
		t.Fatal("unchecked compatible lineage erased checkout", err)
	}
	// This is the pre039 explicit write, not a claimed executable old release.
	if _, err := db.Exec(`UPDATE pipeline_runs SET status='success',error='' WHERE id=$1`, r.ID); err != nil {
		t.Fatal("older explicit update incompatible", err)
	}
	var id, pipeline, status string
	var stages, envs, variables, groups []byte
	var created time.Time
	var started, finished, heartbeat any
	var message, subject, email, token string
	var version int
	if err := db.QueryRow(`SELECT id,pipeline_id,status,stage_runs,env_statuses,variables,created_at,started_at,finished_at,error,heartbeat_at,started_by_user_sub,started_by_email,started_by_groups,started_by_token_hash,pipeline_version FROM pipeline_runs WHERE id=$1`, r.ID).Scan(&id, &pipeline, &status, &stages, &envs, &variables, &created, &started, &finished, &message, &heartbeat, &subject, &email, &groups, &token, &version); err != nil {
		t.Fatal("older explicit select incompatible", err)
	}
	reopened := NewRunStore(reopenRunSourceDB(t, db))
	for _, read := range []func() (*model.PipelineRun, error){
		func() (*model.PipelineRun, error) { return reopened.Get(ctx, r.ID) },
		func() (*model.PipelineRun, error) { return reopened.GetSummary(ctx, r.ID) },
		func() (*model.PipelineRun, error) {
			rows, err := reopened.List(ctx, "p", 10, 0)
			if err != nil || len(rows) != 1 {
				return nil, err
			}
			return rows[0], nil
		},
	} {
		got, err := read()
		if err != nil || got == nil || !reflect.DeepEqual(got.Source, r.Source) || got.StageRuns[0].Logs != "" {
			t.Fatal("reopened projection lost source or exposed progress logs", got, err)
		}
		got.Source.CommitSHA = strings.Repeat("c", 40)
		*got.Source.CheckedOutAt = checked.Add(time.Hour)
	}
	stable, _ := runs.Get(ctx, r.ID)
	if stable.Source.CommitSHA != r.Source.CommitSHA || !stable.Source.CheckedOutAt.Equal(checked) {
		t.Fatal("read pointer mutation changed stored source")
	}
	for _, mutation := range []string{"commit", "digest", "checked-time"} {
		next := stable.Clone()
		next.Status = model.RunStatusFailed
		switch mutation {
		case "commit":
			next.Source.CommitSHA = strings.Repeat("d", 40)
		case "digest":
			next.Source.SourcePlanDigest = strings.Repeat("d", 64)
		case "checked-time":
			time := checked.Add(time.Second)
			next.Source.CheckedOutAt = &time
		}
		if err := runs.Update(ctx, next); !errors.Is(err, store.ErrConflict) {
			t.Fatal("changed source replaced immutable receipt", mutation, err)
		}
	}
	stable, _ = runs.Get(ctx, r.ID)
	if stable.Status != model.RunStatusSuccess {
		t.Fatal("rejected receipt partially updated lifecycle")
	}
	if _, err := db.Exec(`INSERT INTO pipeline_runs(id,pipeline_id,status,stage_runs,env_statuses,variables) VALUES('pre039','p','pending','[]','[]','{}')`); err != nil {
		t.Fatal("older explicit insert incompatible", err)
	}
	old, err := NewRunStore(db).Get(ctx, "pre039")
	if err != nil || old.Source != nil {
		t.Fatal("legacy source reconstructed from current definitions", old, err)
	}
	old.Source = postgresRunSource()
	if err := runs.Update(ctx, old); err != nil {
		t.Fatal("accepted recovery could not attach source", err)
	}
}

func TestRunSourcePostgresConcurrentObservationIsFirstWriteOnly(t *testing.T) {
	db := claimTestDB(t)
	ctx := context.Background()
	if err := NewPipelineStore(db).Create(ctx, &model.Pipeline{ID: "p", Name: "native"}); err != nil {
		t.Fatal(err)
	}
	run := &model.PipelineRun{ID: "first-checkout", PipelineID: "p", Status: model.RunStatusPending, Source: postgresRunSource()}
	if err := NewRunStore(db).Create(ctx, run); err != nil {
		t.Fatal(err)
	}
	checked := time.Now().UTC().Truncate(time.Microsecond)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for n := 0; n < 2; n++ {
		next := run.Clone()
		timestamp := checked.Add(time.Duration(n) * time.Second)
		next.Source.CheckedOutAt = &timestamp
		wg.Add(1)
		go func() { defer wg.Done(); errs <- NewRunStore(db).Update(ctx, next) }()
	}
	wg.Wait()
	close(errs)
	accepted, conflicts := 0, 0
	for err := range errs {
		if err == nil {
			accepted++
		} else if errors.Is(err, store.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if accepted != 1 || conflicts != 1 {
		t.Fatal("competing checkout observations replaced each other", accepted, conflicts)
	}
	stored, err := NewRunStore(reopenRunSourceDB(t, db)).Get(ctx, run.ID)
	if err != nil || stored.Source.CheckedOutAt == nil {
		t.Fatal("confirmed observation missing", stored, err)
	}
	// An equal instant in another zone preserves the same observation.
	equal := stored.Clone()
	zoneTime := equal.Source.CheckedOutAt.In(time.FixedZone("fixture-zone", 7*60*60))
	equal.Source.CheckedOutAt = &zoneTime
	if err := NewRunStore(db).Update(ctx, equal); err != nil {
		t.Fatal("equivalent checkout instant rejected", err)
	}
}

func TestRunSourcePostgresTypedProjectionSecrecyAndDowngradeGuard(t *testing.T) {
	db := claimTestDB(t)
	ctx := context.Background()
	if err := NewPipelineStore(db).Create(ctx, &model.Pipeline{ID: "p", Name: "native"}); err != nil {
		t.Fatal(err)
	}
	// Typed decode/encode drops provider/private fields rather than persisting a
	// raw response. The canary is synthetic; no owner credential is read.
	b, _ := json.Marshal(postgresRunSource())
	var wire map[string]any
	json.Unmarshal(b, &wire)
	wire["accessToken"] = "credential-canary"
	wire["root"] = "/tmp/cooker-source-path-canary"
	wire["environmentValues"] = map[string]string{"PRIVATE": "environment-canary"}
	b, _ = json.Marshal(wire)
	var source model.PipelineRunSource
	if err := json.Unmarshal(b, &source); err != nil {
		t.Fatal(err)
	}
	r := &model.PipelineRun{ID: "safe", PipelineID: "p", Source: &source, Status: model.RunStatusPending}
	if err := NewRunStore(db).Create(ctx, r); err != nil {
		t.Fatal(err)
	}
	var persisted string
	if err := db.QueryRow(`SELECT source::text FROM pipeline_runs WHERE id='safe'`).Scan(&persisted); err != nil || strings.Contains(persisted, "canary") || strings.Contains(persisted, "accessToken") || strings.Contains(persisted, "environmentValues") || strings.Contains(persisted, "root") {
		t.Fatal("private wire data persisted", err)
	}
	got, err := NewRunStore(db).Get(ctx, "safe")
	if err != nil {
		t.Fatal(err)
	}
	b, _ = json.Marshal(got)
	if strings.Contains(string(b), "canary") {
		t.Fatal("private fields escaped run response")
	}
	if err := rollbackMigration(ctx, db, "039_pipeline_run_source"); err == nil {
		t.Fatal("downgrade discarded populated source receipt")
	}
	if _, err := NewRunStore(db).Get(ctx, "safe"); err != nil {
		t.Fatal("refused downgrade changed live schema", err)
	}
	// Removing owned fixture rows proves an empty-schema downgrade is reversible.
	if _, err := db.Exec(`DELETE FROM pipeline_runs`); err != nil {
		t.Fatal(err)
	}
	if err := rollbackMigration(ctx, db, "039_pipeline_run_source"); err != nil {
		t.Fatal("empty receipt schema could not downgrade", err)
	}
	if err := applyMigrations(ctx, db); err != nil {
		t.Fatal("receipt schema could not reapply", err)
	}
	if err := NewRunStore(db).Create(ctx, &model.PipelineRun{ID: "legacy", PipelineID: "p", Status: model.RunStatusPending}); err != nil {
		t.Fatal(err)
	}
	var null bool
	if err := db.QueryRow(`SELECT source IS NULL FROM pipeline_runs WHERE id='legacy'`).Scan(&null); err != nil || !null {
		t.Fatal("legacy source was not SQL NULL", null, err)
	}
}
