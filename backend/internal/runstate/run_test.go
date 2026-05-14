package runstate

import (
	"errors"
	"testing"

	"github.com/santapong/cooker/internal/model"
)

func TestRunHappyPath(t *testing.T) {
	fsm := NewRunFSM(StatePending)
	fsm = fsm.MustApply(EventEnqueue)
	if fsm.Current() != StateQueued {
		t.Fatalf("after Enqueue: %s", fsm.Current())
	}
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
	terms := []State{StateSucceeded, StateFailed, StateCancelled, StateTimedOut}
	for _, s := range terms {
		fsm := NewRunFSM(s)
		for _, e := range []Event{EventEnqueue, EventStart, EventSucceed, EventFail, EventCancel, EventTimeout} {
			if _, err := fsm.Apply(e); err == nil {
				t.Errorf("%s --%s--> succeeded; want rejection", s, e)
			}
		}
	}
}

func TestRunCancelFromPendingAndQueued(t *testing.T) {
	// Pending can be cancelled (run never started).
	fsm := NewRunFSM(StatePending).MustApply(EventCancel)
	if fsm.Current() != StateCancelled {
		t.Fatalf("Pending --Cancel--> %s want Cancelled", fsm.Current())
	}
	// Queued can also be cancelled (job in queue before worker pickup).
	fsm = NewRunFSM(StateQueued).MustApply(EventCancel)
	if fsm.Current() != StateCancelled {
		t.Fatalf("Queued --Cancel--> %s want Cancelled", fsm.Current())
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
		StateQueued:    false,
		StateRunning:   false,
		StateSucceeded: true,
		StateFailed:    true,
		StateCancelled: true,
		StateTimedOut:  true,
	}
	for s, want := range cases {
		if got := IsTerminal(s); got != want {
			t.Errorf("IsTerminal(%s)=%v want %v", s, got, want)
		}
	}
}
