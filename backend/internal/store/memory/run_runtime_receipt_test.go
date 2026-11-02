package memory

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
)

func TestRunRuntimeReceiptProgressSummaryAndOwnership(t *testing.T) {
	st, ctx := New(), context.Background()
	receipt := &model.RuntimeReceipt{Version: 1, Runtime: "docker", RunID: "run", StageID: "deploy", ContainerID: strings.Repeat("a", 64), Name: "web", ImageReference: "web:v1", ImageID: "sha256:" + strings.Repeat("b", 64), CapturedAt: time.Now().UTC()}
	run := &model.PipelineRun{ID: "run", PipelineID: "p", Status: model.RunStatusRunning, StageRuns: []model.StageRun{{StageID: "deploy", Status: model.RunStatusSuccess, Logs: "strip-me", RuntimeReceipt: receipt}}}
	if err := st.Runs.Create(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := st.Runs.UpdateProgress(ctx, run.ID, run.StageRuns); err != nil {
		t.Fatal(err)
	}
	receipt.Name = "caller-mutated"
	for _, read := range []func() (*model.PipelineRun, error){
		func() (*model.PipelineRun, error) { return st.Runs.Get(ctx, run.ID) },
		func() (*model.PipelineRun, error) { return st.Runs.GetSummary(ctx, run.ID) },
	} {
		got, err := read()
		if err != nil || got.StageRuns[0].RuntimeReceipt == nil || got.StageRuns[0].RuntimeReceipt.Name != "web" || got.StageRuns[0].Logs != "" || got.Status != model.RunStatusRunning {
			t.Fatal("progress lost receipt or caller ownership", err)
		}
		got.StageRuns[0].RuntimeReceipt.Name = "reader-mutated"
	}
	got, _ := st.Runs.Get(ctx, run.ID)
	if got.StageRuns[0].RuntimeReceipt.Name != "web" {
		t.Fatal("receipt aliased through reads")
	}
	got.StageRuns[0].RuntimeReceipt = nil
	got.StageRuns[0].RuntimeBindingWarning = model.RuntimeBindingUnavailable
	if err := st.Runs.UpdateProgress(ctx, run.ID, got.StageRuns); err != nil {
		t.Fatal(err)
	}
	after, _ := st.Runs.GetSummary(ctx, run.ID)
	if after.StageRuns[0].RuntimeReceipt != nil || after.StageRuns[0].RuntimeBindingWarning != model.RuntimeBindingUnavailable {
		t.Fatal("retry warning/receipt clearing lost")
	}
}
