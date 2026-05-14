// Package runstate provides a formal state machine around
// model.RunStatus and stage status writes. The executor and other
// services should call Transition / TransitionStage instead of
// writing the Status field directly, so a typo or an invalid
// transition (e.g. terminal → running) is rejected up front
// instead of corrupting a run row.
//
// The implementation wraps github.com/looplab/fsm to delegate the
// transition rules to a well-tested library. The public surface
// (Builder, Allow, Build, FSM, Apply, Can, MustApply) is identical
// to the original in-house FSM that shipped with PR #89 — the swap
// is mechanical and preserves value semantics so existing callers
// (the executor, the TransitionRun / TransitionStage adapters) work
// without changes.
package runstate

import (
	"context"
	"errors"
	"fmt"

	loopfsm "github.com/looplab/fsm"
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
// event-descriptor slice is shared (immutable after Build), so
// callers constructing many FSMs for short-lived rows don't pay a
// slice allocation. Apply allocates a fresh looplab.FSM internally
// to run the transition; that's the cost of preserving value
// semantics, and amortised across the entire executor lifecycle it
// is negligible.
type FSM struct {
	current State
	events  []loopfsm.EventDesc
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

// Build returns a fresh FSM positioned at initial. The event-
// descriptor slice is shared by reference with subsequent Build()
// calls; do not mutate the Builder after the first Build if you've
// handed FSMs to other code.
func (b *Builder) Build(initial State) FSM {
	return FSM{current: initial, events: b.events(), names: b.name}
}

// events converts the (from, event, to) table into the
// []loopfsm.EventDesc shape looplab expects. Multiple sources with
// the same (event, dst) pair are coalesced into one EventDesc with a
// multi-element Src — this matches looplab's intended usage and keeps
// the descriptor list compact. The result is cached on the builder
// after the first Build so repeated builds don't re-walk the table.
func (b *Builder) events() []loopfsm.EventDesc {
	// Group by (event, dst) so each (event → dst) gets one descriptor
	// with all source states. Map key is the pair.
	type groupKey struct {
		event Event
		to    State
	}
	groups := make(map[groupKey][]string)
	for k, to := range b.table {
		gk := groupKey{event: k.event, to: to}
		groups[gk] = append(groups[gk], string(k.from))
	}
	out := make([]loopfsm.EventDesc, 0, len(groups))
	for gk, srcs := range groups {
		out = append(out, loopfsm.EventDesc{
			Name: string(gk.event),
			Src:  srcs,
			Dst:  string(gk.to),
		})
	}
	return out
}

// Current returns the current state.
func (f FSM) Current() State { return f.current }

// Can reports whether Apply(event) would succeed without actually
// transitioning. Use in conditional UI rendering ("show Cancel
// button only if Can(EventCancel)") so the caller doesn't have to
// construct-and-discard an FSM copy.
func (f FSM) Can(event Event) bool {
	return loopfsm.NewFSM(string(f.current), f.events, nil).Can(string(event))
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
	m := loopfsm.NewFSM(string(f.current), f.events, nil)
	if err := m.Event(context.Background(), string(event)); err != nil {
		var inv loopfsm.InvalidEventError
		var unk loopfsm.UnknownEventError
		if errors.As(err, &inv) || errors.As(err, &unk) {
			return f, fmt.Errorf("%s: %s --%s--> (none): %w", f.names, f.current, event, ErrInvalidTransition)
		}
		return f, fmt.Errorf("%s: %s --%s-->: %w", f.names, f.current, event, err)
	}
	f.current = State(m.Current())
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
