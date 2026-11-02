//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
)

// Uses the existing isolated SQL fixture; it starts no Docker workload. A fresh
// pool verifies actual JSONB persistence rather than only a marshal roundtrip.
func TestRunRuntimeReceiptPostgresPersistenceProgressAndRestart(t *testing.T) {
	db := claimTestDB(t)
	ctx := context.Background()
	if err := NewPipelineStore(db).Create(ctx, &model.Pipeline{ID: "runtime-receipt-p", Name: "receipt"}); err != nil {
		t.Fatal(err)
	}
	receipt := &model.RuntimeReceipt{Version: 1, Runtime: "docker", RunID: "receipt-run", StageID: "deploy", ContainerID: strings.Repeat("a", 64), Name: "web", ImageReference: "web:v1", ImageID: "sha256:" + strings.Repeat("b", 64), CapturedAt: time.Now().UTC().Truncate(time.Microsecond)}
	run := &model.PipelineRun{ID: receipt.RunID, PipelineID: "runtime-receipt-p", Status: model.RunStatusRunning, StageRuns: []model.StageRun{{StageID: "deploy", Status: model.RunStatusSuccess, Logs: "strip-me", RuntimeReceipt: receipt}}}
	runs := NewRunStore(db)
	if err := runs.Create(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := runs.UpdateProgress(ctx, run.ID, run.StageRuns); err != nil {
		t.Fatal(err)
	}
	finished := time.Now().UTC()
	if err := runs.UpdateStatus(ctx, run.ID, model.RunStatusSuccess, &finished, ""); err != nil {
		t.Fatal(err)
	}
	reopened := NewRunStore(reopenRunSourceDB(t, db))
	for _, read := range []func() (*model.PipelineRun, error){
		func() (*model.PipelineRun, error) { return reopened.Get(ctx, run.ID) },
		func() (*model.PipelineRun, error) { return reopened.GetSummary(ctx, run.ID) },
		func() (*model.PipelineRun, error) {
			rows, err := reopened.List(ctx, run.PipelineID, 10, 0)
			if err != nil || len(rows) != 1 {
				return nil, err
			}
			return rows[0], nil
		},
	} {
		got, err := read()
		if err != nil || got == nil || !reflect.DeepEqual(got.StageRuns[0].RuntimeReceipt, receipt) || got.StageRuns[0].Logs != "" || got.Status != model.RunStatusSuccess {
			t.Fatal("receipt lost on persisted projection/restart", err)
		}
	}
	var persisted []byte
	if err := db.QueryRow(`SELECT stage_runs FROM pipeline_runs WHERE id=$1`, run.ID).Scan(&persisted); err != nil || !strings.Contains(string(persisted), "runtimeReceipt") {
		t.Fatal("receipt absent from JSONB", err)
	}
	var stages []model.StageRun
	if err := json.Unmarshal(persisted, &stages); err != nil {
		t.Fatal(err)
	}
	stages[0].RuntimeReceipt = nil
	stages[0].RuntimeBindingWarning = model.RuntimeBindingUnavailable
	if err := reopened.UpdateProgress(ctx, run.ID, stages); err != nil {
		t.Fatal(err)
	}
	got, err := NewRunStore(reopenRunSourceDB(t, db)).Get(ctx, run.ID)
	if err != nil || got.StageRuns[0].RuntimeReceipt != nil || got.StageRuns[0].RuntimeBindingWarning != model.RuntimeBindingUnavailable {
		t.Fatal("retry clearing/fixed warning not persisted", err)
	}
	legacy := &model.PipelineRun{ID: "legacy-receipt-run", PipelineID: run.PipelineID, Status: model.RunStatusSuccess, StageRuns: []model.StageRun{{StageID: "deploy", Status: model.RunStatusSuccess}}}
	if err := runs.Create(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	old, err := reopened.Get(ctx, legacy.ID)
	if err != nil || old.StageRuns[0].RuntimeReceipt != nil || old.StageRuns[0].RuntimeBindingWarning != "" {
		t.Fatal("legacy row manufactured observation metadata", err)
	}
}
