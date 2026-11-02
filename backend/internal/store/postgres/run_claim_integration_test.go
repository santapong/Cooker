//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

func claimTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("COOKER_INTEGRATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("COOKER_INTEGRATION_DATABASE_URL is required")
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("cooker_claim_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(); _, _ = admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); admin.Close() })
	if err = applyMigrations(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestRunClaimFencesRevokedWorkerAndDeployHistory(t *testing.T) {
	db := claimTestDB(t)
	ctx := context.Background()
	runs := NewRunStore(db)
	if err := NewPipelineStore(db).Create(ctx, &model.Pipeline{ID: "p", Name: "test"}); err != nil {
		t.Fatal(err)
	}
	run := &model.PipelineRun{ID: "r", PipelineID: "p", Status: model.RunStatusRunning}
	if err := runs.Create(ctx, run); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO jobs(id,kind,payload,status,locked_by,locked_at) VALUES('j','pipeline-run','{"runId":"r"}','running','worker',NOW())`); err != nil {
		t.Fatal(err)
	}
	owned := store.WithRunClaim(ctx, "j", "worker", nil)
	if err := runs.UpdateHeartbeat(owned, "r", time.Now()); err != nil {
		t.Fatal(err)
	}
	// Hold the cancellation job lock while a stale worker begins a write.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`UPDATE jobs SET status='needs_attention',locked_by='' WHERE id='j'`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- runs.UpdateStatus(owned, "r", model.RunStatusSuccess, nil, "") }()
	select {
	case err := <-done:
		t.Fatalf("writer bypassed uncommitted revocation: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	if _, err = tx.Exec(`UPDATE pipeline_runs SET status='needs_attention' WHERE id='r'`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-done; !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale write=%v", err)
	}
	for name, write := range map[string]func() error{
		"update":    func() error { return runs.Update(owned, run) },
		"progress":  func() error { return runs.UpdateProgress(owned, "r", []model.StageRun{{StageID: "stale"}}) },
		"heartbeat": func() error { return runs.UpdateHeartbeat(owned, "r", time.Now()) },
		"history":   func() error { return NewAppDeployStore(db).Create(owned, &model.AppDeploy{ID: "d", RunID: "r"}) },
	} {
		if err := write(); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("%s accepted revoked claim: %v", name, err)
		}
	}
	got, err := runs.Get(ctx, "r")
	if err != nil || got.Status != model.RunStatusNeedsAttention {
		t.Fatalf("run overwritten: %+v %v", got, err)
	}
	// Conversely, a writer which obtained the claim first may finish, but the
	// following atomic job/domain cancellation must be the final visible state.
	if _, err = db.Exec(`UPDATE jobs SET status='running',locked_by='worker' WHERE id='j'`); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- withRunClaim(owned, db, "r", func(writer claimWriter) error {
			close(entered)
			<-release
			_, err := writer.ExecContext(ctx, `UPDATE pipeline_runs SET status='success' WHERE id='r'`)
			return err
		})
	}()
	<-entered
	jobs := jobqueue.NewPostgres(db)
	jobs.OnTransition = SyncJobRun
	cancelDone := make(chan error, 1)
	go func() { _, err := jobs.Cancel(ctx, "j"); cancelDone <- err }()
	select {
	case err := <-cancelDone:
		t.Fatalf("cancellation bypassed owning writer lock: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	if err := <-writeDone; err != nil {
		t.Fatal(err)
	}
	if err := <-cancelDone; err != nil {
		t.Fatal(err)
	}
	got, err = runs.Get(ctx, "r")
	if err != nil || got.Status != model.RunStatusNeedsAttention {
		t.Fatalf("atomic lifecycle lost final state: %+v %v", got, err)
	}
	apps := NewAppStore(db)
	if err := apps.Create(ctx, &model.App{ID: "a", Name: "test", GitHubRepo: "test/repo", DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost}}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE jobs SET status='running',locked_by='worker',payload=jsonb_set(payload,'{app}','{"id":"a"}') WHERE id='j'`); err != nil {
		t.Fatal(err)
	}
	if err := apps.UpdateDeployedURL(owned, "a", "https://valid.example"); err != nil {
		t.Fatal(err)
	}
	if err := apps.UpdateDeployedURL(owned, "another-app", "https://wrong.example"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("wrong app identity accepted: %v", err)
	}
	if _, err = db.Exec(`UPDATE jobs SET status='needs_attention',locked_by='' WHERE id='j'`); err != nil {
		t.Fatal(err)
	}
	if err := apps.UpdateDeployedURL(owned, "a", "https://stale.example"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale app URL accepted: %v", err)
	}
	app, err := apps.Get(ctx, "a")
	if err != nil || app.DeployedURL != "https://valid.example" {
		t.Fatalf("deployed URL overwritten: %+v %v", app, err)
	}
}
