package buildplan

import (
	"testing"

	"github.com/santapong/cooker/internal/model"
)

func TestEdgeAllows(t *testing.T) {
	cases := []struct {
		upstream  model.RunStatus
		condition string
		want      bool
	}{
		// success-edges (empty = default)
		{model.RunStatusSuccess, "", true},
		{model.RunStatusSuccess, "success", true},
		{model.RunStatusFailed, "", false},
		{model.RunStatusFailed, "success", false},
		{model.RunStatusSkipped, "success", false}, // skip propagates
		// failure-edges
		{model.RunStatusFailed, "failure", true},
		{model.RunStatusSuccess, "failure", false},
		{model.RunStatusSkipped, "failure", false}, // skip propagates
		// always-edges: any terminal upstream
		{model.RunStatusSuccess, "always", true},
		{model.RunStatusFailed, "always", true},
		{model.RunStatusSkipped, "always", true},
		// unknown condition fails closed
		{model.RunStatusSuccess, "on-tuesdays", false},
	}
	for _, tc := range cases {
		if got := EdgeAllows(tc.upstream, tc.condition); got != tc.want {
			t.Errorf("EdgeAllows(%s, %q) = %v, want %v", tc.upstream, tc.condition, got, tc.want)
		}
	}
}

func TestStageShouldRun_ANDJoin(t *testing.T) {
	edges := []model.Edge{
		{Source: "a", Target: "z", Condition: "success"},
		{Source: "b", Target: "z", Condition: "success"},
	}
	statuses := map[string]model.RunStatus{
		"a": model.RunStatusSuccess,
		"b": model.RunStatusSuccess,
	}
	statusOf := func(id string) model.RunStatus { return statuses[id] }

	if !StageShouldRun("z", edges, statusOf) {
		t.Error("both upstreams succeeded; z should run")
	}
	statuses["b"] = model.RunStatusFailed
	if StageShouldRun("z", edges, statusOf) {
		t.Error("AND join: one failed upstream must block z")
	}
}

func TestStageShouldRun_NoIncomingEdgesRuns(t *testing.T) {
	if !StageShouldRun("root", nil, func(string) model.RunStatus { return model.RunStatusFailed }) {
		t.Error("a stage with no incoming edges must always run")
	}
}

func TestStageShouldRun_IgnoresOtherTargets(t *testing.T) {
	edges := []model.Edge{
		{Source: "a", Target: "other", Condition: "success"},
	}
	// Edge targets a different stage; z has effectively no incoming.
	if !StageShouldRun("z", edges, func(string) model.RunStatus { return model.RunStatusFailed }) {
		t.Error("edges for other targets must not affect z")
	}
}

func TestStageShouldRun_MixedConditions(t *testing.T) {
	// notify runs iff test failed AND cleanup is terminal.
	edges := []model.Edge{
		{Source: "test", Target: "notify", Condition: "failure"},
		{Source: "cleanup", Target: "notify", Condition: "always"},
	}
	statuses := map[string]model.RunStatus{
		"test":    model.RunStatusFailed,
		"cleanup": model.RunStatusSkipped,
	}
	if !StageShouldRun("notify", edges, func(id string) model.RunStatus { return statuses[id] }) {
		t.Error("failure-edge satisfied + always-edge over skipped should run")
	}
	statuses["test"] = model.RunStatusSuccess
	if StageShouldRun("notify", edges, func(id string) model.RunStatus { return statuses[id] }) {
		t.Error("failure-edge over a success upstream must block")
	}
}
