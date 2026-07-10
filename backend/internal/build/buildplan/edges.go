package buildplan

import "github.com/santapong/cooker/internal/model"

// Edge-condition evaluation (dag-adaptation Primitive #2 / DR-4).
//
// Semantics, pinned by tests in edges_test.go:
//
//   - ""/"success": run iff the upstream stage succeeded.
//   - "failure":    run iff the upstream stage failed.
//   - "always":     run once the upstream stage is terminal — success,
//     failed, or skipped.
//   - A skipped upstream propagates skip through success- AND
//     failure-edges; only "always" runs after a skipped upstream.
//     (Rationale: a stage that never ran neither succeeded nor failed,
//     so neither conditional edge is satisfied.)
//   - Joins AND across all incoming edges: a stage runs iff every
//     incoming edge allows it. This preserves the legacy all-success
//     semantics for unlabelled fan-in.

// EdgeAllows reports whether one incoming edge with the given
// condition lets the downstream stage run, given the upstream stage's
// terminal status.
func EdgeAllows(upstream model.RunStatus, condition string) bool {
	switch condition {
	case "", "success":
		return upstream == model.RunStatusSuccess
	case "failure":
		return upstream == model.RunStatusFailed
	case "always":
		return upstream == model.RunStatusSuccess ||
			upstream == model.RunStatusFailed ||
			upstream == model.RunStatusSkipped
	default:
		// Unknown conditions are rejected at validation; if one leaks
		// through (hand-edited row), fail closed.
		return false
	}
}

// StageShouldRun evaluates every incoming edge of stageID (AND join)
// against the upstream statuses in statusOf. Stages with no incoming
// edges always run. statusOf must return the upstream stage's status;
// the dagrunner's level barrier guarantees upstreams are terminal by
// the time a downstream is evaluated.
func StageShouldRun(stageID string, incoming []model.Edge, statusOf func(stageID string) model.RunStatus) bool {
	for _, e := range incoming {
		if e.Target != stageID {
			continue
		}
		if !EdgeAllows(statusOf(e.Source), e.Condition) {
			return false
		}
	}
	return true
}
