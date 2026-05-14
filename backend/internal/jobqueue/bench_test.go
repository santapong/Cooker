package jobqueue

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// BenchmarkMemoryEnqueue measures Enqueue throughput on the in-memory
// store. The Postgres equivalent is bound by network + INSERT cost
// and is best measured against a real database (deferred).
func BenchmarkMemoryEnqueue(b *testing.B) {
	ctx := context.Background()
	q := NewMemory()
	payload := json.RawMessage(`{"runId":"r1"}`)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := q.Enqueue(ctx, "pipeline-run", payload, EnqueueOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkMemoryDequeueWarm measures Dequeue throughput against a
// pre-warmed queue of N pending jobs (one Dequeue per iteration
// consumes one job). The intent is to surface any quadratic blow-up
// in the scan-and-skip logic; performance should stay roughly
// O(N) per call (acceptable for tests; production uses Postgres).
func BenchmarkMemoryDequeueWarm(b *testing.B) {
	ctx := context.Background()
	q := NewMemory()
	for i := 0; i < b.N; i++ {
		if _, err := q.Enqueue(ctx, "k", nil, EnqueueOptions{}); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		j, err := q.Dequeue(ctx, "w", nil)
		if err != nil || j == nil {
			b.Fatalf("Dequeue: %v / %v", j, err)
		}
	}
}

// BenchmarkExponentialBackoff measures the cost of NextDelay. The
// crypto/rand jitter is the dominant cost; should still be sub-
// microsecond.
func BenchmarkExponentialBackoff(b *testing.B) {
	p := ExponentialBackoff{Base: 5 * time.Second, Max: 5 * time.Minute}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = p.NextDelay(3)
	}
}

// BenchmarkPgTextArray measures the array-literal formatter so we
// have a baseline before considering pq.Array switchover.
func BenchmarkPgTextArray(b *testing.B) {
	in := []string{"pipeline-run", "notification-send", "scheduled-trigger"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = pgTextArray(in)
	}
}
