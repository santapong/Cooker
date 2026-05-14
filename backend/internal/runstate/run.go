package runstate

import "github.com/santapong/cooker/internal/model"

// Canonical states for a pipeline run. Strings match model.RunStatus
// values so a State can be cast to model.RunStatus (and vice versa)
// in the transition adapter.
//
// The state alphabet here is the subset of model.RunStatus that the
// run lifecycle actually moves through. "queued" is intentionally
// *not* a run state — it belongs to the jobqueue (the job that wraps
// the run can be pending/running, but the run row stays at pending
// until a worker picks it up and calls Start). "timed_out" is
// represented as a flavor of "failed"; the orphan sweep already
// uses 'failed' for stale rows (store/postgres/run.go SweepOrphans).
const (
	StatePending   State = "pending"
	StateRunning   State = "running"
	StateSucceeded State = "success" // matches model.RunStatusSuccess
	StateFailed    State = "failed"
	StateCancelled State = "cancelled"
)

// Events that can drive run / stage transitions.
const (
	EventStart   Event = "start"
	EventSucceed Event = "succeed"
	EventFail    Event = "fail"
	EventCancel  Event = "cancel"
)

// runBuilder declares the legal edges for a pipeline run. Reused
// across all run FSMs so the table allocation happens once.
var runBuilder = NewBuilder("run").
	Allow(Transition{From: StatePending, Event: EventStart, To: StateRunning}).
	Allow(Transition{From: StatePending, Event: EventCancel, To: StateCancelled}).
	Allow(Transition{From: StateRunning, Event: EventSucceed, To: StateSucceeded}).
	Allow(Transition{From: StateRunning, Event: EventFail, To: StateFailed}).
	Allow(Transition{From: StateRunning, Event: EventCancel, To: StateCancelled})

// NewRunFSM returns a fresh FSM positioned at the supplied initial
// state. Pass model.RunStatus values from the store (cast through
// StateFromRunStatus) so a run loaded from Postgres resumes its FSM
// at the right state.
func NewRunFSM(initial State) FSM { return runBuilder.Build(initial) }

// IsTerminal reports whether the state is a final state. Terminal
// states have no outgoing transitions; subsequent Apply returns
// ErrInvalidTransition.
func IsTerminal(s State) bool {
	switch s {
	case StateSucceeded, StateFailed, StateCancelled:
		return true
	}
	return false
}

// StateFromRunStatus converts a model.RunStatus to a State. The two
// types are intentionally string-compatible so this is a cheap cast,
// but the function exists as the documented boundary so a future
// renumbering doesn't have to grep for raw casts.
func StateFromRunStatus(s model.RunStatus) State { return State(s) }

// RunStatusFromState is the inverse; same comment.
func RunStatusFromState(s State) model.RunStatus { return model.RunStatus(s) }
