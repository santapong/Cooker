package runstate

import (
	"errors"
	"testing"
)

func TestFSMAllowAndApply(t *testing.T) {
	b := NewBuilder("t").
		Allow(Transition{From: "a", Event: "go", To: "b"}).
		Allow(Transition{From: "b", Event: "go", To: "c"})
	fsm := b.Build("a")

	if fsm.Current() != "a" {
		t.Fatalf("initial=%s want a", fsm.Current())
	}
	next, err := fsm.Apply("go")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if next.Current() != "b" {
		t.Fatalf("after first Apply: current=%s want b", next.Current())
	}
	next, err = next.Apply("go")
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if next.Current() != "c" {
		t.Fatalf("after second Apply: current=%s want c", next.Current())
	}
}

func TestFSMReceiverIsValueType(t *testing.T) {
	b := NewBuilder("t").Allow(Transition{From: "a", Event: "go", To: "b"})
	fsm := b.Build("a")
	// Apply returns a NEW FSM; calling it without assigning back must
	// not mutate the original.
	_, _ = fsm.Apply("go")
	if fsm.Current() != "a" {
		t.Fatalf("original mutated: current=%s want a", fsm.Current())
	}
}

func TestFSMInvalidTransitionWraps(t *testing.T) {
	b := NewBuilder("t").Allow(Transition{From: "a", Event: "go", To: "b"})
	fsm := b.Build("b") // there is no outgoing edge from b
	_, err := fsm.Apply("go")
	if err == nil {
		t.Fatal("want invalid-transition error, got nil")
	}
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("want ErrInvalidTransition wrapped, got %v", err)
	}
}

func TestFSMCan(t *testing.T) {
	b := NewBuilder("t").
		Allow(Transition{From: "a", Event: "go", To: "b"}).
		Allow(Transition{From: "a", Event: "stop", To: "halt"})
	fsm := b.Build("a")
	if !fsm.Can("go") {
		t.Fatal("Can(go)=false from a")
	}
	if !fsm.Can("stop") {
		t.Fatal("Can(stop)=false from a")
	}
	if fsm.Can("reset") {
		t.Fatal("Can(reset)=true from a but no edge declared")
	}
}

func TestFSMDuplicateAllowPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("want panic on duplicate Allow")
		}
	}()
	NewBuilder("t").
		Allow(Transition{From: "a", Event: "go", To: "b"}).
		Allow(Transition{From: "a", Event: "go", To: "c"})
}

func TestFSMMustApplyPanicsOnInvalid(t *testing.T) {
	fsm := NewBuilder("t").
		Allow(Transition{From: "a", Event: "go", To: "b"}).
		Build("a")
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("want panic on MustApply with no edge")
		}
	}()
	fsm.MustApply("missing")
}
