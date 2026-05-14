package runstate

import (
	"testing"
)

func TestStageHappyPath(t *testing.T) {
	fsm := NewStageFSM(StatePending).
		MustApply(EventStart).
		MustApply(EventSucceed)
	if fsm.Current() != StateSucceeded {
		t.Fatalf("final=%s want %s", fsm.Current(), StateSucceeded)
	}
}

func TestStageFailFromRunning(t *testing.T) {
	fsm := NewStageFSM(StateRunning).MustApply(EventFail)
	if fsm.Current() != StateFailed {
		t.Fatalf("Running --Fail--> %s want %s", fsm.Current(), StateFailed)
	}
}

func TestStageCancelFromRunning(t *testing.T) {
	fsm := NewStageFSM(StateRunning).MustApply(EventCancel)
	if fsm.Current() != StateCancelled {
		t.Fatalf("Running --Cancel--> %s want %s", fsm.Current(), StateCancelled)
	}
}

func TestStageTerminalIsSticky(t *testing.T) {
	for _, s := range []State{StateSucceeded, StateFailed, StateCancelled} {
		fsm := NewStageFSM(s)
		if _, err := fsm.Apply(EventStart); err == nil {
			t.Errorf("%s --Start--> succeeded; want rejection", s)
		}
	}
}
