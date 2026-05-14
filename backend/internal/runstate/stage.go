package runstate

// stageBuilder declares the legal edges for a single stage run. The
// alphabet is identical to a pipeline run — stages and runs share
// the lifecycle shape — but a distinct builder lets us evolve them
// independently (e.g. stages may grow a "skipped" terminal state
// while runs do not).
var stageBuilder = NewBuilder("stage").
	Allow(Transition{From: StatePending, Event: EventStart, To: StateRunning}).
	Allow(Transition{From: StatePending, Event: EventCancel, To: StateCancelled}).
	Allow(Transition{From: StateRunning, Event: EventSucceed, To: StateSucceeded}).
	Allow(Transition{From: StateRunning, Event: EventFail, To: StateFailed}).
	Allow(Transition{From: StateRunning, Event: EventCancel, To: StateCancelled})

// NewStageFSM returns a fresh FSM for a single stage. Same shape as
// NewRunFSM — a separate factory keeps the call sites in the
// executor self-documenting.
func NewStageFSM(initial State) FSM { return stageBuilder.Build(initial) }
