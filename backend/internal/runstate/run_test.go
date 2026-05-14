package runstate

import (
	"errors"
	"testing"

	"github.com/santapong/cooker/internal/model"
)

func TestRunHappyPath(t *testing.T) {
	fsm := NewRunFSM(StatePending)
	fsm = fsm.MustApply(EventStart)
	if fsm.Current() != StateRunning {
		t.Fatalf("after Start: %s", fsm.Current())
	}
	fsm = fsm.MustApply(EventSucceed)
	if fsm.Current() != StateSucceeded {
		t.Fatalf("after Succeed: %s", fsm.Current())
	}
}

func TestRunTerminalIsSticky(t *testing.T) {
	terms := []State{StateSucceeded, StateFailed, StateCancelled}
	for _, s := range terms {
		fsm := NewRunFSM(s)
		for _, e := range []Event{EventStart, EventSucceed, EventFail, EventCancel} {
			if _, err := fsm.Apply(e); err == nil {
				t.Errorf("%s --%s--> succeeded; want rejection", s, e)
			}
		}
	}
}

func TestRunCancelFromPending(t *testing.T) {
	fsm := NewRunFSM(StatePending).MustApply(EventCancel)
	if fsm.Current() != StateCancelled {
		t.Fatalf("Pending --Cancel--> %s want Cancelled", fsm.Current())
	}
}

func TestRunCancelFromRunning(t *testing.T) {
	fsm := NewRunFSM(StatePending).MustApply(EventStart).MustApply(EventCancel)
	if fsm.Current() != StateCancelled {
		t.Fatalf("Running --Cancel--> %s want Cancelled", fsm.Current())
	}
}

func TestRunFailFromRunning(t *testing.T) {
	fsm := NewRunFSM(StateRunning).MustApply(EventFail)
	if fsm.Current() != StateFailed {
		t.Fatalf("Running --Fail--> %s want Failed", fsm.Current())
	}
}

func TestRunInvalidEventRejected(t *testing.T) {
	// Pending --Succeed--> ... is not a legal edge; the run has to
	// go through Running first.
	_, err := NewRunFSM(StatePending).Apply(EventSucceed)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("want ErrInvalidTransition, got %v", err)
	}
}

func TestTransitionRunAdapter(t *testing.T) {
	next, err := TransitionRun(model.RunStatusRunning, EventSucceed)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if next != model.RunStatusSuccess {
		t.Fatalf("Running + Succeed -> %s want %s", next, model.RunStatusSuccess)
	}
	// The adapter must propagate the invalid-transition error untouched.
	_, err = TransitionRun(model.RunStatusSuccess, EventFail)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("want ErrInvalidTransition from terminal, got %v", err)
	}
}

func TestTransitionRunReturnsCurrentOnError(t *testing.T) {
	// The adapter must return the input current state untouched on
	// error so callers that ignore the error (against advice) at
	// least don't corrupt the column.
	next, err := TransitionRun(model.RunStatusSuccess, EventFail)
	if err == nil {
		t.Fatal("want error")
	}
	if next != model.RunStatusSuccess {
		t.Fatalf("current mutated on error: %s", next)
	}
}

func TestCanTransitionRun(t *testing.T) {
	if !CanTransitionRun(model.RunStatusRunning, EventCancel) {
		t.Fatal("Running should accept Cancel")
	}
	if CanTransitionRun(model.RunStatusSuccess, EventCancel) {
		t.Fatal("Terminal Success should reject Cancel")
	}
}

func TestIsTerminal(t *testing.T) {
	cases := map[State]bool{
		StatePending:   false,
		StateRunning:   false,
		StateSucceeded: true,
		StateFailed:    true,
		StateCancelled: true,
	}
	for s, want := range cases {
		if got := IsTerminal(s); got != want {
			t.Errorf("IsTerminal(%s)=%v want %v", s, got, want)
		}
	}
}

// Pin every State constant value to its corresponding
// model.RunStatus so a future renaming of one without the other
// breaks the test instead of silently corrupting run rows.
func TestStateValuesMatchModelRunStatus(t *testing.T) {
	cases := map[State]model.RunStatus{
		StatePending:   model.RunStatusPending,
		StateRunning:   model.RunStatusRunning,
		StateSucceeded: model.RunStatusSuccess,
		StateFailed:    model.RunStatusFailed,
		StateCancelled: model.RunStatusCancelled,
	}
	for s, rs := range cases {
		if string(s) != string(rs) {
			t.Errorf("State(%q) != model.RunStatus(%q)", s, rs)
		}
	}
}
