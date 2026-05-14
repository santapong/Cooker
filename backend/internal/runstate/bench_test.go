package runstate

import (
	"testing"

	"github.com/santapong/cooker/internal/model"
)

// BenchmarkRunFSMApply measures the cost of a single valid
// transition. Goal: < 100 ns/op and 0 allocs/op (FSM is a value
// type sharing an immutable map).
func BenchmarkRunFSMApply(b *testing.B) {
	fsm := NewRunFSM(StatePending)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = fsm.Apply(EventStart)
	}
}

// BenchmarkRunFSMApplyInvalid measures the cost of a rejected
// transition. The error-wrap allocates a *fmt.wrapError; ~2 allocs
// expected. Should still complete in tens of ns.
func BenchmarkRunFSMApplyInvalid(b *testing.B) {
	fsm := NewRunFSM(StateSucceeded) // terminal
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = fsm.Apply(EventStart)
	}
}

// BenchmarkTransitionRun measures the adapter overhead (one FSM
// construction + one Apply). The construction is zero-alloc since
// the table is shared by reference.
func BenchmarkTransitionRun(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = TransitionRun(model.RunStatusRunning, EventSucceed)
	}
}
