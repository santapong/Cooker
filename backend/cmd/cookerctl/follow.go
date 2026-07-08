package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/santapong/cooker/internal/model"
)

// pollInterval is how often `run --follow` re-reads the run status over
// REST. ~2s keeps the transition feed responsive without hammering the
// server; the run endpoint is a cheap single-row read.
const pollInterval = 2 * time.Second

// followRun polls the run-detail endpoint every pollInterval, printing
// each stage's status transition and fetching that stage's logs the
// moment it reaches a terminal state. It returns 0 when the run succeeds
// and 1 when it fails or is cancelled, so the exit code is scriptable.
//
// This is deliberately REST polling, not WebSocket: V1 keeps the
// transport simple (no ticket exchange, no upgrade) at the cost of a few
// seconds of latency.
func followRun(ctx context.Context, c *client, cfg globalConfig, pipelineID, runID string, stdout, stderr io.Writer) int {
	// Stage id → display name, for readable transition lines. A missing
	// pipeline (deleted mid-run) degrades gracefully to bare ids.
	names := map[string]string{}
	if p, err := c.GetPipeline(ctx, pipelineID); err == nil {
		for _, s := range p.Stages {
			names[s.ID] = s.Name
		}
	}
	stageName := func(id string) string {
		if n := names[id]; n != "" {
			return n
		}
		return id
	}

	seen := map[string]model.RunStatus{} // last status printed per stage
	logged := map[string]bool{}          // stages whose logs were fetched

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		run, err := c.GetRun(ctx, pipelineID, runID)
		if err != nil {
			return reportError(stderr, err)
		}

		for _, sr := range run.StageRuns {
			if prev, ok := seen[sr.StageID]; !ok || prev != sr.Status {
				seen[sr.StageID] = sr.Status
				fmt.Fprintf(stdout, "  [%s] %s → %s\n",
					time.Now().Format("15:04:05"), stageName(sr.StageID), sr.Status)
			}
			if isTerminalStage(sr.Status) && !logged[sr.StageID] {
				logged[sr.StageID] = true
				printStageLogs(ctx, c, pipelineID, runID, sr.StageID, stageName(sr.StageID), stdout)
			}
		}

		if isTerminalRun(run.Status) {
			fmt.Fprintf(stdout, "Run %s finished: %s\n", run.ID, run.Status)
			if cfg.json {
				if code := printJSON(stdout, stderr, run); code != 0 {
					return code
				}
			}
			if run.Status == model.RunStatusSuccess {
				return 0
			}
			return 1
		}

		select {
		case <-ctx.Done():
			fmt.Fprintln(stderr, "error:", ctx.Err())
			return 1
		case <-ticker.C:
		}
	}
}

// printStageLogs fetches and prints one stage's final logs, indented under
// a header. A fetch failure is noted but never aborts the follow loop —
// logs are best-effort relative to the run outcome.
func printStageLogs(ctx context.Context, c *client, pipelineID, runID, stageID, name string, stdout io.Writer) {
	logs, err := c.StageLogs(ctx, pipelineID, runID, stageID)
	if err != nil {
		fmt.Fprintf(stdout, "    (logs unavailable for %s: %s)\n", name, err)
		return
	}
	logs = strings.TrimRight(logs, "\n")
	if logs == "" {
		return
	}
	fmt.Fprintf(stdout, "    --- logs: %s ---\n", name)
	for _, line := range strings.Split(logs, "\n") {
		fmt.Fprintf(stdout, "    %s\n", line)
	}
}

// isTerminalRun reports whether a run-level status will not change again.
func isTerminalRun(s model.RunStatus) bool {
	switch s {
	case model.RunStatusSuccess, model.RunStatusFailed, model.RunStatusCancelled:
		return true
	default:
		return false
	}
}

// isTerminalStage reports whether a stage-level status is final. Unlike a
// run, a stage may also be Skipped.
func isTerminalStage(s model.RunStatus) bool {
	switch s {
	case model.RunStatusSuccess, model.RunStatusFailed, model.RunStatusCancelled, model.RunStatusSkipped:
		return true
	default:
		return false
	}
}
