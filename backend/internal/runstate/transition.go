package runstate

import (
	"github.com/santapong/cooker/internal/model"
)

// TransitionRun applies an event to the supplied PipelineRun's
// status field and returns the new model.RunStatus. Returns
// ErrInvalidTransition on a disallowed edge — the caller should
// log and skip rather than overwrite blindly, since the disallowed
// transition usually means a concurrent worker already finished the
// run.
//
// Usage from the executor:
//
//	next, err := runstate.TransitionRun(run.Status, runstate.EventStart)
//	if err != nil { return err }
//	run.Status = next
func TransitionRun(current model.RunStatus, event Event) (model.RunStatus, error) {
	fsm := NewRunFSM(StateFromRunStatus(current))
	next, err := fsm.Apply(event)
	if err != nil {
		return current, err
	}
	return RunStatusFromState(next.Current()), nil
}

// TransitionStage is the stage-scoped sibling of TransitionRun.
// Stages and runs share the State / Event alphabet so this exists
// mostly for call-site readability — reading TransitionStage(...)
// in the executor tells you you're updating a stage, not the run.
func TransitionStage(current model.RunStatus, event Event) (model.RunStatus, error) {
	fsm := NewStageFSM(StateFromRunStatus(current))
	next, err := fsm.Apply(event)
	if err != nil {
		return current, err
	}
	return RunStatusFromState(next.Current()), nil
}

// CanTransitionRun is the read-only variant: reports whether the
// transition would succeed. Useful in handlers that want to return
// 409 Conflict early when a request asks for an impossible state
// change (e.g. cancel an already-terminal run).
func CanTransitionRun(current model.RunStatus, event Event) bool {
	return NewRunFSM(StateFromRunStatus(current)).Can(event)
}
