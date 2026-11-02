//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
)

func TestAppRecoveryHistoryPreservesEvidenceWithoutInventingRuntimeSuccess(t *testing.T) {
	for _, mode := range []string{"missing history", "existing artifact evidence", "deleted app"} {
		t.Run(mode, func(t *testing.T) {
			db := claimTestDB(t)
			ctx := context.Background()
			app := &model.App{ID: "app", Name: "fixture", GitHubRepo: "fixture/recovery", Branch: "main"}
			if err := NewAppStore(db).Create(ctx, app); err != nil {
				t.Fatal(err)
			}
			payload, _ := json.Marshal(map[string]any{"runId": "run", "app": app, "commit": "0123456789012345678901234567890123456789", "actor": &model.PipelineRun{StartedByUserSub: "accepted-operator"}, "createdAt": time.Now().UTC()})
			jobs := jobqueue.NewPostgres(db)
			jobs.OnTransition = SyncJobRun
			job, err := jobs.Enqueue(ctx, "app-deploy", payload, jobqueue.EnqueueOptions{ID: "app-run:run", ConcurrencyKey: "app:app", MaxAttempts: 1})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := jobs.Dequeue(ctx, "lost-worker", nil); err != nil {
				t.Fatal(err)
			}
			if mode == "existing artifact evidence" {
				if err := NewAppDeployStore(db).Create(ctx, &model.AppDeploy{ID: "already-recorded", AppID: "app", RunID: "run", PipelineID: "observed-pipeline", Status: model.RunStatusRunning, ImageRef: "registry.invalid/app@sha256:recorded", Digest: "sha256:recorded", Kind: model.AppDeployKindDeploy}); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "deleted app" {
				if _, err := db.Exec(`DELETE FROM apps WHERE id='app'`); err != nil {
					t.Fatal(err)
				}
			}
			before, err := jobs.Get(ctx, job.ID)
			if err != nil {
				t.Fatal(err)
			}
			beforeDigest, _ := jobqueue.PayloadDigest(before.Payload)
			if recovered, err := jobs.RecoverExpired(ctx, time.Now().Add(time.Second)); err != nil || len(recovered) != 1 {
				t.Fatal("recovery failed", err)
			}
			rows, err := NewAppDeployStore(db).ListByApp(ctx, "app", 10)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "deleted app" {
				if len(rows) != 0 {
					t.Fatal("deleted App history was recreated")
				}
				return
			}
			if len(rows) != 1 || rows[0].Status != model.RunStatusNeedsAttention || rows[0].RunID != "run" {
				t.Fatal("missing uncertain attempt history", rows)
			}
			if mode == "missing history" && (rows[0].ImageRef != "" || rows[0].Digest != "" || rows[0].PipelineID != "app-app-run") {
				t.Fatal("recovery invented runtime evidence", rows[0])
			}
			if mode == "existing artifact evidence" && (rows[0].ID != "already-recorded" || rows[0].ImageRef != "registry.invalid/app@sha256:recorded" || rows[0].Digest != "sha256:recorded" || rows[0].PipelineID != "observed-pipeline") {
				t.Fatal("recovery replaced existing evidence", rows[0])
			}
			run, err := NewRunStore(db).Get(ctx, "run")
			if err != nil || run.StartedByUserSub != "accepted-operator" || run.Status != model.RunStatusNeedsAttention || run.FinishedAt != nil {
				t.Fatal("accepted actor/run identity or uncertain status changed", err)
			}
			after, err := jobs.Get(ctx, job.ID)
			if err != nil {
				t.Fatal(err)
			}
			afterDigest, _ := jobqueue.PayloadDigest(after.Payload)
			if beforeDigest != afterDigest {
				t.Fatal("recovery changed accepted source/config payload")
			}
			if again, err := jobs.RecoverExpired(ctx, time.Now().Add(time.Second)); err != nil || len(again) != 0 {
				t.Fatal("recovery was not idempotent", err)
			}
			if err := jobs.Resolve(ctx, job.ID, jobqueue.StatusSucceeded, "fixture administrator checked the exact external outcome"); err != nil {
				t.Fatal(err)
			}
			rows, err = NewAppDeployStore(db).ListByApp(ctx, "app", 10)
			if err != nil || len(rows) != 1 || rows[0].Status != model.RunStatusSuccess {
				t.Fatal("resolved history diverged", err)
			}
		})
	}
}
