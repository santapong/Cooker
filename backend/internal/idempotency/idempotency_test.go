package idempotency

import (
	"context"
	"testing"
	"time"
)

func TestMemoryGetSet(t *testing.T) {
	m := NewMemory(0)
	ctx := context.Background()
	if _, ok := m.Get(ctx, "missing"); ok {
		t.Fatal("missing key should miss")
	}
	want := Entry{Status: 202, Body: []byte(`{"id":"r1"}`)}
	if err := m.Set(ctx, "k", want, time.Minute); err != nil {
		t.Fatal(err)
	}
	got, ok := m.Get(ctx, "k")
	if !ok || got.Status != 202 || string(got.Body) != string(want.Body) {
		t.Fatalf("got %+v ok=%v, want %+v", got, ok, want)
	}
}

func TestMemoryExpiry(t *testing.T) {
	m := NewMemory(0)
	ctx := context.Background()
	_ = m.Set(ctx, "k", Entry{Status: 200}, time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	if _, ok := m.Get(ctx, "k"); ok {
		t.Fatal("expired entry should miss")
	}
}
