package service

import (
	"sort"
	"time"

	"github.com/santapong/cooker/internal/model"
)

// Stage-duration analytics (roadmap M4): pure computation over run
// history. The handler fetches the runs; this never touches a store.

type PipelineAnalytics struct {
	PipelineID  string  `json:"pipelineId"`
	RunCount    int     `json:"runCount"`
	SuccessRate float64 `json:"successRate"`
	// Stages is keyed to the pipeline's current stage list order when
	// available, falling back to first-seen order from history.
	Stages []StageStats `json:"stages"`
	// Runs is the per-run series, newest first.
	Runs []RunPoint `json:"runs"`
}

type StageStats struct {
	StageID     string  `json:"stageId"`
	Name        string  `json:"name,omitempty"`
	Type        string  `json:"type,omitempty"`
	Samples     int     `json:"samples"`
	P50MS       int64   `json:"p50Ms"`
	P95MS       int64   `json:"p95Ms"`
	AvgMS       int64   `json:"avgMs"`
	SuccessRate float64 `json:"successRate"`
}

type RunPoint struct {
	RunID      string          `json:"runId"`
	CreatedAt  time.Time       `json:"createdAt"`
	Status     model.RunStatus `json:"status"`
	DurationMS int64           `json:"durationMs"`
	// StageDurations carries per-stage ms for row expansion in the UI.
	StageDurations map[string]int64 `json:"stageDurations,omitempty"`
}

// ComputeAnalytics derives duration/success statistics from runs.
// Only terminal runs count toward rates; only stage runs with both
// timestamps contribute duration samples (skipped/never-started
// stages are excluded). p may be nil (names/types then omitted).
func ComputeAnalytics(p *model.Pipeline, runs []*model.PipelineRun) *PipelineAnalytics {
	out := &PipelineAnalytics{Stages: []StageStats{}, Runs: []RunPoint{}}
	if p != nil {
		out.PipelineID = p.ID
	}

	names := map[string]string{}
	types := map[string]string{}
	var stageOrder []string
	if p != nil {
		for i := range p.Stages {
			s := &p.Stages[i]
			names[s.ID] = s.Name
			types[s.ID] = string(s.Type)
			stageOrder = append(stageOrder, s.ID)
		}
	}
	seen := map[string]bool{}
	for _, id := range stageOrder {
		seen[id] = true
	}

	durations := map[string][]int64{}
	successes := map[string]int{}
	terminalStages := map[string]int{}
	terminalRuns, successRuns := 0, 0

	for _, r := range runs {
		point := RunPoint{RunID: r.ID, CreatedAt: r.CreatedAt, Status: r.Status, DurationMS: -1, StageDurations: map[string]int64{}}
		if r.StartedAt != nil && r.FinishedAt != nil {
			point.DurationMS = int64(r.FinishedAt.Sub(*r.StartedAt) / time.Millisecond)
		}
		if isTerminalRunStatus(r.Status) {
			terminalRuns++
			if r.Status == model.RunStatusSuccess {
				successRuns++
			}
		}
		for i := range r.StageRuns {
			sr := &r.StageRuns[i]
			if !seen[sr.StageID] {
				seen[sr.StageID] = true
				stageOrder = append(stageOrder, sr.StageID)
			}
			if isTerminalRunStatus(sr.Status) && sr.Status != "skipped" {
				terminalStages[sr.StageID]++
				if sr.Status == model.RunStatusSuccess {
					successes[sr.StageID]++
				}
			}
			if sr.StartedAt != nil && sr.FinishedAt != nil {
				ms := int64(sr.FinishedAt.Sub(*sr.StartedAt) / time.Millisecond)
				durations[sr.StageID] = append(durations[sr.StageID], ms)
				point.StageDurations[sr.StageID] = ms
			}
		}
		out.Runs = append(out.Runs, point)
	}

	out.RunCount = len(runs)
	if terminalRuns > 0 {
		out.SuccessRate = float64(successRuns) / float64(terminalRuns)
	}

	for _, id := range stageOrder {
		samples := durations[id]
		st := StageStats{
			StageID: id,
			Name:    names[id],
			Type:    types[id],
			Samples: len(samples),
		}
		if n := terminalStages[id]; n > 0 {
			st.SuccessRate = float64(successes[id]) / float64(n)
		}
		if len(samples) > 0 {
			sorted := append([]int64(nil), samples...)
			sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
			st.P50MS = percentile(sorted, 0.50)
			st.P95MS = percentile(sorted, 0.95)
			var sum int64
			for _, v := range sorted {
				sum += v
			}
			st.AvgMS = sum / int64(len(sorted))
		}
		out.Stages = append(out.Stages, st)
	}
	return out
}

// percentile picks the nearest-rank value from an ascending slice.
func percentile(sorted []int64, q float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(q*float64(len(sorted))+0.5) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func isTerminalRunStatus(s model.RunStatus) bool {
	switch s {
	case model.RunStatusSuccess, model.RunStatusFailed, model.RunStatusCancelled,
		// "skipped" lands with the edge-conditions milestone (M2); the
		// literal keeps this branch independent of that constant.
		model.RunStatus("skipped"):
		return true
	}
	return false
}
