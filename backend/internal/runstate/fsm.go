// Package runstate provides a formal state machine around
// model.RunStatus and stage status writes. The executor and other
// services should call Transition / TransitionStage instead of
// writing the Status field directly, so a typo or an invalid
// transition (e.g. terminal → running) is rejected up front
// instead of corrupting a run row.
//
// The implementation is a small transition-table FSM rather than a
// general-purpose library: the alphabet is small (7 states, 7
// events) and a hand-rolled version is easier to read than the
// library equivalent for this scope. If the alphabet grows past ~20
// states or we need fan-out hooks (on-enter, on-leave callbacks),
// swapping to github.com/looplab/fsm becomes worthwhile.
package runstate

import (
	"errors"
	"fmt"
)

// State is a single state in the FSM (e.g. "pending", "running").
// Use the typed constants in run.go / stage.go rather than raw
// strings so the compiler catches typos.
type State string

// Event triggers a transition from one State to another. The same
// event name may map to different destination states depending on
// the current state (e.g. "complete" goes from running→succeeded
// but is not legal from pending).
type Event string

// Transition declares a single legal edge in the FSM graph.
type Transition struct {
	From  State
	Event Event
	To    State
}

// ErrInvalidTransition is returned by Apply when the current state
// has no legal transition for the supplied event. Wrap with %w if
// re-raising so callers can errors.Is against the sentinel.
var ErrInvalidTransition = errors.New("runstate: invalid transition")

// FSM is a value type — cheap to construct, cheap to copy. The
// transition map is shared (immutable after build), so callers
// constructing many FSMs for short-lived rows don't pay a map
// allocation.
type FSM struct {
	current State
	table   map[transitionKey]State
	names   string // a short label used in error messages ("run" / "stage")
}

type transitionKey struct {
	from  State
	event Event
}

// Builder collects transitions before producing an immutable FSM.
// Construct via NewBuilder, call Allow(...) for each edge, then
// Build(initialState). The same Builder can produce many FSMs by
// repeated Build calls.
type Builder struct {
	table map[transitionKey]State
	name  string
}

// NewBuilder starts a fresh Builder. name is used in error messages
// so callers can tell a stage transition error from a run one.
func NewBuilder(name string) *Builder {
	return &Builder{table: make(map[transitionKey]State), name: name}
}

// Allow declares one legal transition. Duplicate (from, event) is a
// programmer bug — panics so the misconfiguration surfaces at boot,
// not at the first stray Apply.
func (b *Builder) Allow(t Transition) *Builder {
	k := transitionKey{from: t.From, event: t.Event}
	if _, exists := b.table[k]; exists {
		panic(fmt.Sprintf("runstate: duplicate transition %s: %s --%s--> ...", b.name, t.From, t.Event))
	}
	b.table[k] = t.To
	return b
}

// Build returns a fresh FSM positioned at initial. The transition
// map is shared by reference with subsequent Build() calls; do not
// mutate the Builder after the first Build if you've handed FSMs to
// other code.
func (b *Builder) Build(initial State) FSM {
	return FSM{current: initial, table: b.table, names: b.name}
}

// Current returns the current state.
func (f FSM) Current() State { return f.current }

// Can reports whether Apply(event) would succeed without actually
// transitioning. Use in conditional UI rendering ("show Cancel
// button only if Can(EventCancel)") so the caller doesn't have to
// construct-and-discard an FSM copy.
func (f FSM) Can(event Event) bool {
	_, ok := f.table[transitionKey{from: f.current, event: event}]
	return ok
}

// Apply transitions the FSM by event, returning the new state.
// Returns a wrapped ErrInvalidTransition when the edge is not
// declared. The receiver value's current field is updated by the
// caller assigning the returned FSM back (this is intentional: FSM
// is a value type, so the receiver doesn't mutate in place).
//
//	fsm, err := fsm.Apply(EventStart)
//	if err != nil { ... }
func (f FSM) Apply(event Event) (FSM, error) {
	to, ok := f.table[transitionKey{from: f.current, event: event}]
	if !ok {
		return f, fmt.Errorf("%s: %s --%s--> (none): %w", f.names, f.current, event, ErrInvalidTransition)
	}
	f.current = to
	return f, nil
}

// MustApply panics on invalid transition. For tests and for code
// paths where the transition has already been validated by Can.
func (f FSM) MustApply(event Event) FSM {
	next, err := f.Apply(event)
	if err != nil {
		panic(err)
	}
	return next
}
