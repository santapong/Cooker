package service

import (
	"time"

	"github.com/santapong/cooker/internal/model"
)

// Run diff (roadmap M3): compare a run against a baseline (typically
// the last green run) — per-stage status/duration/digest deltas plus
// a variables diff and a definition-version delta. Pure computation;
// the handler resolves which two runs to compare.

type RunDiff struct {
	AgainstRunID  string `json:"againstRunId,omitempty"`
	AgainstStatus string `json:"againstStatus,omitempty"`
	// Reason is set instead of the fields below when no baseline run
	// could be resolved (e.g. no successful prior run).
	Reason string `json:"reason,omitempty"`
	// DefinitionChanged reports whether Pipeline.Version moved between
	// the two runs. Omitted (nil) when either run predates the
	// pipeline_version stamp (migration 017).
	DefinitionChanged *bool          `json:"definitionChanged,omitempty"`
	PipelineVersion   *VersionDelta  `json:"pipelineVersionDelta,omitempty"`
	Variables         *VariablesDiff `json:"variables,omitempty"`
	Stages            []StageDiff    `json:"stages"`
}

type VersionDelta struct {
	From int `json:"from"`
	To   int `json:"to"`
}

type VariablesDiff struct {
	Added   map[string]string      `json:"added,omitempty"`
	Removed map[string]string      `json:"removed,omitempty"`
	Changed map[string]ValueChange `json:"changed,omitempty"`
}

type ValueChange struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type StageDiff struct {
	StageID string `json:"stageId"`
	Name    string `json:"name,omitempty"`
	Status  struct {
		From model.RunStatus `json:"from"`
		To   model.RunStatus `json:"to"`
	} `json:"status"`
	// Durations in ms; -1 means "not measurable" (missing timestamps,
	// e.g. skipped or never-started stages).
	DurationMS struct {
		From    int64 `json:"from"`
		To      int64 `json:"to"`
		DeltaMS int64 `json:"deltaMs"`
	} `json:"durationMs"`
	Digest struct {
		From    string `json:"from,omitempty"`
		To      string `json:"to,omitempty"`
		Changed bool   `json:"changed"`
	} `json:"digest"`
	OutputsChangedKeys []string `json:"outputsChangedKeys,omitempty"`
}

// BuildRunDiff computes the diff of base (the run being inspected)
// against a baseline run of the same pipeline. p may be nil (stage
// names then fall back to IDs).
func BuildRunDiff(base, against *model.PipelineRun, p *model.Pipeline) *RunDiff {
	d := &RunDiff{
		AgainstRunID:  against.ID,
		AgainstStatus: string(against.Status),
		Stages:        []StageDiff{},
	}

	if base.PipelineVersion > 0 && against.PipelineVersion > 0 {
		changed := base.PipelineVersion != against.PipelineVersion
		d.DefinitionChanged = &changed
		d.PipelineVersion = &VersionDelta{From: against.PipelineVersion, To: base.PipelineVersion}
	}

	d.Variables = diffVariables(against.Variables, base.Variables)

	names := map[string]string{}
	if p != nil {
		for i := range p.Stages {
			names[p.Stages[i].ID] = p.Stages[i].Name
		}
	}
	baselineStages := map[string]*model.StageRun{}
	for i := range against.StageRuns {
		baselineStages[against.StageRuns[i].StageID] = &against.StageRuns[i]
	}

	for i := range base.StageRuns {
		cur := &base.StageRuns[i]
		sd := StageDiff{StageID: cur.StageID, Name: names[cur.StageID]}
		sd.Status.To = cur.Status
		sd.DurationMS.To = stageDurationMS(cur)
		sd.Digest.To = stageDigest(cur)

		if prev, ok := baselineStages[cur.StageID]; ok {
			sd.Status.From = prev.Status
			sd.DurationMS.From = stageDurationMS(prev)
			sd.Digest.From = stageDigest(prev)
			sd.OutputsChangedKeys = changedOutputKeys(prev.Outputs, cur.Outputs)
		} else {
			sd.DurationMS.From = -1
		}
		if sd.DurationMS.From >= 0 && sd.DurationMS.To >= 0 {
			sd.DurationMS.DeltaMS = sd.DurationMS.To - sd.DurationMS.From
		}
		sd.Digest.Changed = sd.Digest.From != "" && sd.Digest.To != "" && sd.Digest.From != sd.Digest.To
		d.Stages = append(d.Stages, sd)
	}
	return d
}

func stageDurationMS(sr *model.StageRun) int64 {
	if sr.StartedAt == nil || sr.FinishedAt == nil {
		return -1
	}
	return int64(sr.FinishedAt.Sub(*sr.StartedAt) / time.Millisecond)
}

func stageDigest(sr *model.StageRun) string {
	if v, ok := sr.Outputs["digest"]; ok && v != "" {
		return v
	}
	for _, a := range sr.Artifacts {
		if a.Type == "oci-image" && a.Digest != "" {
			return a.Digest
		}
	}
	return ""
}

func diffVariables(from, to map[string]string) *VariablesDiff {
	out := &VariablesDiff{
		Added:   map[string]string{},
		Removed: map[string]string{},
		Changed: map[string]ValueChange{},
	}
	for k, v := range to {
		if old, ok := from[k]; !ok {
			out.Added[k] = v
		} else if old != v {
			out.Changed[k] = ValueChange{From: old, To: v}
		}
	}
	for k, v := range from {
		if _, ok := to[k]; !ok {
			out.Removed[k] = v
		}
	}
	if len(out.Added) == 0 && len(out.Removed) == 0 && len(out.Changed) == 0 {
		return nil
	}
	return out
}

func changedOutputKeys(from, to map[string]string) []string {
	var keys []string
	for k, v := range to {
		if old, ok := from[k]; !ok || old != v {
			keys = append(keys, k)
		}
	}
	for k := range from {
		if _, ok := to[k]; !ok {
			keys = append(keys, k)
		}
	}
	return keys
}
