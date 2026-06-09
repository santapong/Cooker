package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/builder"
	"github.com/santapong/cooker/internal/model"
)

func boolPtr(b bool) *bool { return &b }

func TestPolicyFromStage(t *testing.T) {
	build := func(cfg model.StageConfig) *model.Stage {
		return &model.Stage{ID: "b", Type: model.StageTypeBuild, Config: cfg}
	}

	t.Run("legacy retries int", func(t *testing.T) {
		p := policyFromStage(build(model.StageConfig{Retries: 2}))
		if p.MaxAttempts != 3 {
			t.Errorf("MaxAttempts = %d, want 3 (1 + 2 retries)", p.MaxAttempts)
		}
		if p.Initial != time.Second || p.Max != 15*time.Second {
			t.Errorf("defaults changed: initial=%s max=%s", p.Initial, p.Max)
		}
	})

	t.Run("structured policy wins", func(t *testing.T) {
		p := policyFromStage(build(model.StageConfig{
			Retries: 9, // ignored when Retry is set with MaxAttempts
			Retry:   &model.RetryPolicy{MaxAttempts: 4, InitialMS: 500, MaxMS: 4000},
		}))
		if p.MaxAttempts != 4 {
			t.Errorf("MaxAttempts = %d, want 4", p.MaxAttempts)
		}
		if p.Initial != 500*time.Millisecond || p.Max != 4*time.Second {
			t.Errorf("initial=%s max=%s", p.Initial, p.Max)
		}
	})

	t.Run("exponential false pins delay", func(t *testing.T) {
		p := policyFromStage(build(model.StageConfig{
			Retry: &model.RetryPolicy{MaxAttempts: 3, InitialMS: 700, MaxMS: 60_000, Exponential: boolPtr(false)},
		}))
		if p.Max != p.Initial {
			t.Errorf("constant backoff must pin Max to Initial; got initial=%s max=%s", p.Initial, p.Max)
		}
	})

	t.Run("clamps", func(t *testing.T) {
		p := policyFromStage(build(model.StageConfig{
			Retry: &model.RetryPolicy{MaxAttempts: 99, InitialMS: 1, MaxMS: 99_000_000},
		}))
		if p.MaxAttempts != 10 {
			t.Errorf("MaxAttempts = %d, want clamp to 10", p.MaxAttempts)
		}
		if p.Initial != 100*time.Millisecond {
			t.Errorf("Initial = %s, want clamp to 100ms", p.Initial)
		}
		if p.Max != 5*time.Minute {
			t.Errorf("Max = %s, want clamp to 5m", p.Max)
		}
	})

	t.Run("approval never retries", func(t *testing.T) {
		s := &model.Stage{ID: "a", Type: model.StageTypeApproval, Config: model.StageConfig{
			Retry: &model.RetryPolicy{MaxAttempts: 5},
		}}
		if p := policyFromStage(s); p.MaxAttempts != 1 {
			t.Errorf("approval MaxAttempts = %d, want 1", p.MaxAttempts)
		}
	})
}

// flakyBuilder fails N times then succeeds — exercises the retry loop
// end-to-end through Execute.
type flakyBuilder struct {
	mockBuilder
	failures int
}

func (f *flakyBuilder) Build(ctx context.Context, req builder.Request) (builder.Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	n := len(f.calls)
	f.mu.Unlock()
	if n <= f.failures {
		return builder.Result{}, errors.New("transient registry blip")
	}
	return builder.Result{Tags: req.Tags}, nil
}

func TestExecutor_RetryPolicy_FailTwiceThenSucceed(t *testing.T) {
	fb := &flakyBuilder{failures: 2}
	p := &model.Pipeline{
		ID: "pipe-retry",
		Stages: []model.Stage{
			{ID: "b", Type: model.StageTypeBuild, Config: model.StageConfig{
				Context: "/c", Tags: []string{"t"},
				Retry: &model.RetryPolicy{MaxAttempts: 3, InitialMS: 100, MaxMS: 100},
			}},
		},
	}
	run := &model.PipelineRun{
		ID: "run-retry", PipelineID: p.ID, Status: model.RunStatusPending,
		StageRuns: []model.StageRun{{StageID: "b", Status: model.RunStatusPending}},
	}
	exec := NewExecutor(WithBuilder(fb))
	result, err := exec.Execute(context.Background(), p, run)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != model.RunStatusSuccess {
		t.Errorf("status = %s, want success after retries", result.Status)
	}
	if fb.callCount() != 3 {
		t.Errorf("build attempts = %d, want 3", fb.callCount())
	}
}
