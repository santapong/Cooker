package model

import (
	"testing"
	"time"
)

// TestPipelineRun_Clone verifies Clone produces a deep copy: mutating the
// original's nested slices/maps/pointers after cloning must not affect the
// clone. This is the snapshot primitive the run-state concurrency contract
// relies on (docs/proposals/run-state-concurrency-2026.md).
func TestPipelineRun_Clone(t *testing.T) {
	t0 := time.Unix(1000, 0)
	orig := &PipelineRun{
		ID:              "run-1",
		PipelineID:      "pipe-1",
		Status:          RunStatusRunning,
		StartedAt:       &t0,
		Variables:       map[string]string{"k": "v"},
		StartedByGroups: []string{"admins"},
		StageRuns: []StageRun{
			{
				StageID:   "s1",
				Status:    RunStatusRunning,
				StartedAt: &t0,
				Logs:      "line1\n",
				Artifacts: []Artifact{{Type: "oci-image", Ref: "r", Digest: "d"}},
				Outputs:   map[string]string{"tag": "v1"},
			},
		},
		EnvironmentStatuses: []EnvironmentStatus{
			{EnvironmentID: "env-1", Status: EnvStatusDeploying, PromotedAt: &t0},
		},
	}

	clone := orig.Clone()

	// Independence of the StageRuns backing array + nested ref types.
	orig.StageRuns[0].Status = RunStatusSuccess
	orig.StageRuns[0].Logs = "line1\nline2\n"
	orig.StageRuns[0].Outputs["tag"] = "v2"
	orig.StageRuns[0].Artifacts[0].Ref = "mutated"
	orig.Variables["k"] = "mutated"
	orig.StartedByGroups[0] = "mutated"
	*orig.StartedAt = time.Unix(2000, 0)

	if got := clone.StageRuns[0].Status; got != RunStatusRunning {
		t.Errorf("clone StageRun status aliased: got %s, want running", got)
	}
	if got := clone.StageRuns[0].Logs; got != "line1\n" {
		t.Errorf("clone StageRun logs aliased: got %q", got)
	}
	if got := clone.StageRuns[0].Outputs["tag"]; got != "v1" {
		t.Errorf("clone StageRun outputs map aliased: got %q, want v1", got)
	}
	if got := clone.StageRuns[0].Artifacts[0].Ref; got != "r" {
		t.Errorf("clone StageRun artifacts slice aliased: got %q, want r", got)
	}
	if got := clone.Variables["k"]; got != "v" {
		t.Errorf("clone Variables map aliased: got %q, want v", got)
	}
	if got := clone.StartedByGroups[0]; got != "admins" {
		t.Errorf("clone StartedByGroups slice aliased: got %q, want admins", got)
	}
	if got := clone.StartedAt; got == nil || !got.Equal(time.Unix(1000, 0)) {
		t.Errorf("clone StartedAt pointer aliased: got %v, want 1000", got)
	}
	if got := clone.EnvironmentStatuses[0].PromotedAt; got == nil || !got.Equal(time.Unix(1000, 0)) {
		t.Errorf("clone EnvironmentStatuses PromotedAt aliased: got %v", got)
	}
}

// TestPipelineRun_Clone_Nil guards the nil receiver.
func TestPipelineRun_Clone_Nil(t *testing.T) {
	var r *PipelineRun
	if r.Clone() != nil {
		t.Error("Clone of nil run should be nil")
	}
}
