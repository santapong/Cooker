package service

import (
	"context"
	"errors"
	"testing"

	"github.com/santapong/cooker/internal/model"
)

// edgePipeline: build → test; test --failure--> notify; test --success--> deploy.
// Failing the test stage must run notify, skip deploy, and the run is failed.
func edgePipeline() (*model.Pipeline, *model.PipelineRun) {
	p := &model.Pipeline{
		ID: "pipe-edges",
		Stages: []model.Stage{
			{ID: "build", Name: "Build", Type: model.StageTypeBuild, Config: model.StageConfig{
				Context: "/ctx", Tags: []string{"t"},
			}},
			{ID: "push-notify", Name: "Notify", Type: model.StageTypePush, Config: model.StageConfig{
				Image: "i", Repository: "r",
			}},
			{ID: "push-deploy", Name: "Ship", Type: model.StageTypePush, Config: model.StageConfig{
				Image: "i", Repository: "r",
			}},
		},
		Edges: []model.Edge{
			{ID: "e1", Source: "build", Target: "push-notify", Condition: "failure"},
			{ID: "e2", Source: "build", Target: "push-deploy", Condition: "success"},
		},
	}
	run := &model.PipelineRun{
		ID: "run-edges", PipelineID: p.ID, Status: model.RunStatusPending,
		StageRuns: []model.StageRun{
			{StageID: "build", Status: model.RunStatusPending},
			{StageID: "push-notify", Status: model.RunStatusPending},
			{StageID: "push-deploy", Status: model.RunStatusPending},
		},
	}
	return p, run
}

func stageStatus(run *model.PipelineRun, id string) model.RunStatus {
	for i := range run.StageRuns {
		if run.StageRuns[i].StageID == id {
			return run.StageRuns[i].Status
		}
	}
	return ""
}

func TestExecutor_EdgeConditions_FailurePathRuns(t *testing.T) {
	p, run := edgePipeline()
	mb := &mockBuilder{err: errors.New("compile error")}
	mp := &mockPusher{}
	exec := NewExecutor(WithBuilder(mb), WithPusher(mp))

	result, err := exec.Execute(context.Background(), p, run)
	if err == nil {
		t.Fatal("expected the build failure to surface")
	}
	if result.Status != model.RunStatusFailed {
		t.Errorf("run status = %s, want failed", result.Status)
	}
	if got := stageStatus(run, "push-notify"); got != model.RunStatusSuccess {
		t.Errorf("failure-edge stage = %s, want success (it must run)", got)
	}
	if got := stageStatus(run, "push-deploy"); got != model.RunStatusSkipped {
		t.Errorf("success-edge stage = %s, want skipped", got)
	}
}

func TestExecutor_EdgeConditions_SuccessPathSkipsFailureEdge(t *testing.T) {
	p, run := edgePipeline()
	mb := &mockBuilder{}
	mp := &mockPusher{}
	exec := NewExecutor(WithBuilder(mb), WithPusher(mp))

	result, err := exec.Execute(context.Background(), p, run)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != model.RunStatusSuccess {
		t.Errorf("run status = %s, want success", result.Status)
	}
	if got := stageStatus(run, "push-notify"); got != model.RunStatusSkipped {
		t.Errorf("failure-edge stage = %s, want skipped on the green path", got)
	}
	if got := stageStatus(run, "push-deploy"); got != model.RunStatusSuccess {
		t.Errorf("success-edge stage = %s, want success", got)
	}
}

// Skip propagation: build fails → deploy skipped → post-deploy
// (success-edge off deploy) also skipped; an always-edge off deploy runs.
func TestExecutor_EdgeConditions_SkipPropagates(t *testing.T) {
	p := &model.Pipeline{
		ID: "pipe-skipchain",
		Stages: []model.Stage{
			{ID: "build", Type: model.StageTypeBuild, Config: model.StageConfig{Context: "/c", Tags: []string{"t"}}},
			{ID: "deploy", Type: model.StageTypePush, Config: model.StageConfig{Image: "i", Repository: "r"}},
			{ID: "post", Type: model.StageTypePush, Config: model.StageConfig{Image: "i", Repository: "r"}},
			{ID: "cleanup", Type: model.StageTypePush, Config: model.StageConfig{Image: "i", Repository: "r"}},
		},
		Edges: []model.Edge{
			{ID: "e1", Source: "build", Target: "deploy", Condition: "success"},
			{ID: "e2", Source: "deploy", Target: "post", Condition: "success"},
			{ID: "e3", Source: "deploy", Target: "cleanup", Condition: "always"},
		},
	}
	run := &model.PipelineRun{
		ID: "run-skipchain", PipelineID: p.ID, Status: model.RunStatusPending,
		StageRuns: []model.StageRun{
			{StageID: "build", Status: model.RunStatusPending},
			{StageID: "deploy", Status: model.RunStatusPending},
			{StageID: "post", Status: model.RunStatusPending},
			{StageID: "cleanup", Status: model.RunStatusPending},
		},
	}
	exec := NewExecutor(WithBuilder(&mockBuilder{err: errors.New("boom")}), WithPusher(&mockPusher{}))
	if _, err := exec.Execute(context.Background(), p, run); err == nil {
		t.Fatal("expected failure")
	}
	if got := stageStatus(run, "deploy"); got != model.RunStatusSkipped {
		t.Errorf("deploy = %s, want skipped", got)
	}
	if got := stageStatus(run, "post"); got != model.RunStatusSkipped {
		t.Errorf("post = %s, want skipped (skip propagates through success-edges)", got)
	}
	if got := stageStatus(run, "cleanup"); got != model.RunStatusSuccess {
		t.Errorf("cleanup = %s, want success (always-edge runs after skipped upstream)", got)
	}
}

// Rollback knob: with edge conditions off, the legacy behaviour holds —
// first failure aborts the run at its level barrier and downstream
// stages stay pending (not skipped).
func TestExecutor_EdgeConditions_FlagOffLegacyAbort(t *testing.T) {
	t.Setenv("COOKER_EDGE_CONDITIONS_ENABLED", "false")
	p, run := edgePipeline()
	exec := NewExecutor(WithBuilder(&mockBuilder{err: errors.New("boom")}), WithPusher(&mockPusher{}))
	if _, err := exec.Execute(context.Background(), p, run); err == nil {
		t.Fatal("expected failure")
	}
	if got := stageStatus(run, "push-notify"); got != model.RunStatusPending {
		t.Errorf("flag off: failure-edge stage = %s, want pending (legacy abort)", got)
	}
	if got := stageStatus(run, "push-deploy"); got != model.RunStatusPending {
		t.Errorf("flag off: success-edge stage = %s, want pending (legacy abort)", got)
	}
}
