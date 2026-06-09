package service

import (
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
)

func at(sec int) *time.Time {
	t := time.Date(2026, 6, 1, 12, 0, sec, 0, time.UTC)
	return &t
}

func TestComputeAnalytics(t *testing.T) {
	p := &model.Pipeline{
		ID:     "pipe",
		Stages: []model.Stage{{ID: "build", Name: "Build", Type: model.StageTypeBuild}},
	}
	mkRun := func(id string, status model.RunStatus, buildSecs int) *model.PipelineRun {
		return &model.PipelineRun{
			ID: id, Status: status, StartedAt: at(0), FinishedAt: at(buildSecs),
			StageRuns: []model.StageRun{{
				StageID: "build", Status: status, StartedAt: at(0), FinishedAt: at(buildSecs),
			}},
		}
	}
	runs := []*model.PipelineRun{
		mkRun("r1", model.RunStatusSuccess, 10),
		mkRun("r2", model.RunStatusSuccess, 20),
		mkRun("r3", model.RunStatusFailed, 30),
		mkRun("r4", model.RunStatusSuccess, 40),
	}

	a := ComputeAnalytics(p, runs)
	if a.RunCount != 4 {
		t.Errorf("RunCount = %d", a.RunCount)
	}
	if a.SuccessRate != 0.75 {
		t.Errorf("SuccessRate = %v, want 0.75", a.SuccessRate)
	}
	if len(a.Stages) != 1 {
		t.Fatalf("stages = %d", len(a.Stages))
	}
	b := a.Stages[0]
	if b.Samples != 4 || b.Name != "Build" {
		t.Errorf("stage stats = %+v", b)
	}
	// sorted: 10s 20s 30s 40s → p50 nearest-rank = 20s, p95 = 40s, avg = 25s
	if b.P50MS != 20_000 || b.P95MS != 40_000 || b.AvgMS != 25_000 {
		t.Errorf("p50=%d p95=%d avg=%d", b.P50MS, b.P95MS, b.AvgMS)
	}
	if b.SuccessRate != 0.75 {
		t.Errorf("stage success = %v", b.SuccessRate)
	}
	if len(a.Runs) != 4 || a.Runs[0].DurationMS != 10_000 {
		t.Errorf("run points = %+v", a.Runs)
	}
}

func TestComputeAnalytics_RunningStagesExcluded(t *testing.T) {
	runs := []*model.PipelineRun{{
		ID: "r1", Status: model.RunStatusRunning, StartedAt: at(0),
		StageRuns: []model.StageRun{{StageID: "s", Status: model.RunStatusRunning, StartedAt: at(0)}},
	}}
	a := ComputeAnalytics(nil, runs)
	if a.SuccessRate != 0 {
		t.Errorf("running runs must not count toward success rate")
	}
	if a.Stages[0].Samples != 0 {
		t.Errorf("running stage must contribute no duration sample")
	}
	if a.Runs[0].DurationMS != -1 {
		t.Errorf("unfinished run duration must be -1, got %d", a.Runs[0].DurationMS)
	}
}

func TestComputeAnalytics_EmptyHistory(t *testing.T) {
	a := ComputeAnalytics(&model.Pipeline{ID: "p"}, nil)
	if a.RunCount != 0 || a.SuccessRate != 0 || len(a.Runs) != 0 {
		t.Errorf("empty history analytics = %+v", a)
	}
}
