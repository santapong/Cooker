package service

import (
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
)

func ts(sec int) *time.Time {
	t := time.Date(2026, 6, 1, 12, 0, sec, 0, time.UTC)
	return &t
}

func TestBuildRunDiff(t *testing.T) {
	against := &model.PipelineRun{
		ID: "green", Status: model.RunStatusSuccess, PipelineVersion: 3,
		Variables: map[string]string{"REGION": "eu", "DROPPED": "1"},
		StageRuns: []model.StageRun{
			{StageID: "build", Status: model.RunStatusSuccess, StartedAt: ts(0), FinishedAt: ts(10),
				Outputs: map[string]string{"digest": "sha256:aaa", "tag": "v1"}},
			{StageID: "deploy", Status: model.RunStatusSuccess, StartedAt: ts(10), FinishedAt: ts(12)},
		},
	}
	base := &model.PipelineRun{
		ID: "red", Status: model.RunStatusFailed, PipelineVersion: 5,
		Variables: map[string]string{"REGION": "us", "NEW": "x"},
		StageRuns: []model.StageRun{
			{StageID: "build", Status: model.RunStatusSuccess, StartedAt: ts(0), FinishedAt: ts(25),
				Outputs: map[string]string{"digest": "sha256:bbb", "tag": "v1"}},
			{StageID: "deploy", Status: model.RunStatusFailed, StartedAt: ts(25), FinishedAt: ts(26)},
		},
	}
	p := &model.Pipeline{Stages: []model.Stage{{ID: "build", Name: "Build"}, {ID: "deploy", Name: "Ship"}}}

	d := BuildRunDiff(base, against, p)
	if d.AgainstRunID != "green" || d.AgainstStatus != "success" {
		t.Errorf("baseline meta wrong: %+v", d)
	}
	if d.DefinitionChanged == nil || !*d.DefinitionChanged {
		t.Error("definition changed (v3→v5) not reported")
	}
	if d.PipelineVersion == nil || d.PipelineVersion.From != 3 || d.PipelineVersion.To != 5 {
		t.Errorf("version delta = %+v", d.PipelineVersion)
	}
	if d.Variables == nil ||
		d.Variables.Added["NEW"] != "x" ||
		d.Variables.Removed["DROPPED"] != "1" ||
		d.Variables.Changed["REGION"].From != "eu" || d.Variables.Changed["REGION"].To != "us" {
		t.Errorf("variables diff = %+v", d.Variables)
	}
	if len(d.Stages) != 2 {
		t.Fatalf("stage diffs = %d, want 2", len(d.Stages))
	}
	build := d.Stages[0]
	if build.Name != "Build" || build.DurationMS.From != 10_000 || build.DurationMS.To != 25_000 || build.DurationMS.DeltaMS != 15_000 {
		t.Errorf("build duration diff = %+v", build.DurationMS)
	}
	if !build.Digest.Changed || build.Digest.From != "sha256:aaa" || build.Digest.To != "sha256:bbb" {
		t.Errorf("build digest diff = %+v", build.Digest)
	}
	if len(build.OutputsChangedKeys) != 1 || build.OutputsChangedKeys[0] != "digest" {
		t.Errorf("outputs changed keys = %v", build.OutputsChangedKeys)
	}
	deploy := d.Stages[1]
	if deploy.Status.From != model.RunStatusSuccess || deploy.Status.To != model.RunStatusFailed {
		t.Errorf("deploy status diff = %+v", deploy.Status)
	}
}

func TestBuildRunDiff_MissingTimestampsAndVersions(t *testing.T) {
	against := &model.PipelineRun{ID: "old", Status: model.RunStatusSuccess,
		StageRuns: []model.StageRun{{StageID: "s", Status: model.RunStatusSuccess}}}
	base := &model.PipelineRun{ID: "new", Status: model.RunStatusSuccess,
		StageRuns: []model.StageRun{{StageID: "s", Status: model.RunStatusSkipped}}}

	d := BuildRunDiff(base, against, nil)
	if d.DefinitionChanged != nil || d.PipelineVersion != nil {
		t.Error("pre-017 runs (version 0) must omit the definition delta")
	}
	if d.Stages[0].DurationMS.From != -1 || d.Stages[0].DurationMS.To != -1 {
		t.Errorf("missing timestamps must yield -1 durations: %+v", d.Stages[0].DurationMS)
	}
	if d.Variables != nil {
		t.Error("identical (empty) variables must omit the diff block")
	}
}
