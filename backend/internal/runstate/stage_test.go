package runstate

import (
	"testing"
)

func TestStageHappyPath(t *testing.T) {
	fsm := NewStageFSM(StatePending).
		MustApply(EventEnqueue).
		MustApply(EventStart).
		MustApply(EventSucceed)
	if fsm.Current() != StateSucceeded {
		t.Fatalf("final=%s want %s", fsm.Current(), StateSucceeded)
	}
}

func TestStageTimeoutFromRunning(t *testing.T) {
	fsm := NewStageFSM(StateRunning).MustApply(EventTimeout)
	if fsm.Current() != StateTimedOut {
		t.Fatalf("Running --Timeout--> %s want %s", fsm.Current(), StateTimedOut)
	}
}

func TestStageFailFromRunning(t *testing.T) {
	fsm := NewStageFSM(StateRunning).MustApply(EventFail)
	if fsm.Current() != StateFailed {
		t.Fatalf("Running --Fail--> %s want %s", fsm.Current(), StateFailed)
	}
}

func TestStageTerminalIsSticky(t *testing.T) {
	// Same property as runs: once a stage is terminal, no event
	// should re-open it.
	for _, s := range []State{StateSucceeded, StateFailed, StateCancelled, StateTimedOut} {
		fsm := NewStageFSM(s)
		if _, err := fsm.Apply(EventStart); err == nil {
			t.Errorf("%s --Start--> succeeded; want rejection", s)
		}
	}
}
