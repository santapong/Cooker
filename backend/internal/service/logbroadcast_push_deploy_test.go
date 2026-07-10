package service

import (
	"context"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/deploy/deployer"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/build/pusher"
)

// streamingPusher writes payload to req.LogWriter (when set) then
// returns a fixed Result. Lets us drive executePush's tee behaviour
// without standing up a real registry.
type streamingPusher struct {
	payload []byte
	res     pusher.Result
}

func (p *streamingPusher) Push(_ context.Context, req pusher.Request) (pusher.Result, error) {
	if req.LogWriter != nil && len(p.payload) > 0 {
		_, _ = req.LogWriter.Write(p.payload)
	}
	return p.res, nil
}

// streamingDeployer mirrors streamingPusher for deploy stages.
type streamingDeployer struct {
	payload []byte
	res     deployer.Result
}

func (d *streamingDeployer) Deploy(_ context.Context, req deployer.Request) (deployer.Result, error) {
	if req.LogWriter != nil && len(d.payload) > 0 {
		_, _ = req.LogWriter.Write(d.payload)
	}
	return d.res, nil
}

// TestExecutor_PushStage_BroadcastsLogLines locks the parallel of
// TestExecutor_BuildStage_BroadcastsLogLines for the push half of T2.
// A pipeline with a single push stage should: (a) tee adapter output
// into StageRun.Logs and (b) broadcast each newline-terminated line on
// the canonical stage-logs channel.
func TestExecutor_PushStage_BroadcastsLogLines(t *testing.T) {
	rec := &recordedBroadcasts{}
	sp := &streamingPusher{
		payload: []byte("Pushing app:v1 -> reg/app:v1\nPushed image to reg/app:v1\n"),
		res:     pusher.Result{Digest: "sha256:deadbeef"},
	}

	p := &model.Pipeline{
		ID: "pipe-push-1",
		Stages: []model.Stage{
			{ID: "p", Name: "Push", Type: model.StageTypePush, Config: model.StageConfig{
				Repository: "reg/app:v1",
				Image:      "app:v1",
			}},
		},
	}
	run := &model.PipelineRun{
		ID:         "run-push-1",
		PipelineID: "pipe-push-1",
		Status:     model.RunStatusPending,
		StageRuns:  []model.StageRun{{StageID: "p", Status: model.RunStatusPending}},
	}

	exec := NewExecutor(WithPusher(sp), WithLogBroadcaster(rec.record))
	if _, err := exec.Execute(context.Background(), p, run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := rec.snapshot()
	if len(got) != 2 {
		t.Fatalf("broadcast count: got %d want 2 (got=%v)", len(got), got)
	}
	for _, g := range got {
		if g.channel != "stage-logs:run-push-1:p" {
			t.Errorf("channel: got %q want %q", g.channel, "stage-logs:run-push-1:p")
		}
	}
	if !strings.Contains(run.StageRuns[0].Logs, "Pushed image to reg/app:v1") {
		t.Errorf("StageRun.Logs missing canonical line: %q", run.StageRuns[0].Logs)
	}
}

// TestExecutor_DeployStage_BroadcastsLogLines is the deploy half of T2.
func TestExecutor_DeployStage_BroadcastsLogLines(t *testing.T) {
	rec := &recordedBroadcasts{}
	sd := &streamingDeployer{
		payload: []byte("Applied Deployment/web\nApplied Service/web\n"),
		res:     deployer.Result{AppliedResources: []string{"Deployment/default/web", "Service/default/web"}},
	}

	p := &model.Pipeline{
		ID: "pipe-deploy-1",
		Stages: []model.Stage{
			{ID: "d", Name: "Deploy", Type: model.StageTypeDeploy, Config: model.StageConfig{
				ManifestPath: "/tmp/manifest.yaml",
				Namespace:    "default",
			}},
		},
	}
	run := &model.PipelineRun{
		ID:         "run-deploy-1",
		PipelineID: "pipe-deploy-1",
		Status:     model.RunStatusPending,
		StageRuns:  []model.StageRun{{StageID: "d", Status: model.RunStatusPending}},
	}

	exec := NewExecutor(WithDeployer(sd), WithLogBroadcaster(rec.record))
	if _, err := exec.Execute(context.Background(), p, run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := rec.snapshot()
	if len(got) != 2 {
		t.Fatalf("broadcast count: got %d want 2 (got=%v)", len(got), got)
	}
	for _, g := range got {
		if g.channel != "stage-logs:run-deploy-1:d" {
			t.Errorf("channel: got %q want %q", g.channel, "stage-logs:run-deploy-1:d")
		}
	}
	if !strings.Contains(run.StageRuns[0].Logs, "Applied Deployment/web") {
		t.Errorf("StageRun.Logs missing canonical line: %q", run.StageRuns[0].Logs)
	}
}

// TestExecutor_PushStage_NoBroadcasterIsNoOp confirms StageRun.Logs is
// still populated when no broadcaster is wired — historical behaviour
// for the build stage must hold for push too.
func TestExecutor_PushStage_NoBroadcasterIsNoOp(t *testing.T) {
	sp := &streamingPusher{
		payload: []byte("Pushed image to reg/app:v1\n"),
		res:     pusher.Result{Digest: "sha256:00"},
	}
	p := &model.Pipeline{
		ID: "pipe-push-2",
		Stages: []model.Stage{
			{ID: "p", Name: "Push", Type: model.StageTypePush, Config: model.StageConfig{
				Repository: "reg/app:v1",
				Image:      "app:v1",
			}},
		},
	}
	run := &model.PipelineRun{
		ID:         "run-push-2",
		PipelineID: "pipe-push-2",
		Status:     model.RunStatusPending,
		StageRuns:  []model.StageRun{{StageID: "p", Status: model.RunStatusPending}},
	}

	exec := NewExecutor(WithPusher(sp))
	if _, err := exec.Execute(context.Background(), p, run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := run.StageRuns[0].Logs; !strings.Contains(got, "Pushed image to reg/app:v1") {
		t.Errorf("StageRun.Logs: got %q", got)
	}
}

// TestExecutor_DeployStage_NoBroadcasterIsNoOp mirrors the previous.
func TestExecutor_DeployStage_NoBroadcasterIsNoOp(t *testing.T) {
	sd := &streamingDeployer{
		payload: []byte("Applied Deployment/web\n"),
	}
	p := &model.Pipeline{
		ID: "pipe-deploy-2",
		Stages: []model.Stage{
			{ID: "d", Name: "Deploy", Type: model.StageTypeDeploy, Config: model.StageConfig{
				ManifestPath: "/tmp/manifest.yaml",
				Namespace:    "default",
			}},
		},
	}
	run := &model.PipelineRun{
		ID:         "run-deploy-2",
		PipelineID: "pipe-deploy-2",
		Status:     model.RunStatusPending,
		StageRuns:  []model.StageRun{{StageID: "d", Status: model.RunStatusPending}},
	}

	exec := NewExecutor(WithDeployer(sd))
	if _, err := exec.Execute(context.Background(), p, run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := run.StageRuns[0].Logs; !strings.Contains(got, "Applied Deployment/web") {
		t.Errorf("StageRun.Logs: got %q", got)
	}
}
