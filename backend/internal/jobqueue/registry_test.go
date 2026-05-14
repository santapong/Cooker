package jobqueue

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRegistryRegisterAndDispatch(t *testing.T) {
	r := NewRegistry()
	var got string
	r.Register("alpha", func(ctx context.Context, j *Job) error {
		got = j.ID
		return nil
	})
	if err := r.Dispatch(context.Background(), &Job{ID: "j1", Kind: "alpha"}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if got != "j1" {
		t.Fatalf("handler didn't run; got=%q", got)
	}
}

func TestRegistryDispatchUnknownKindErrors(t *testing.T) {
	r := NewRegistry()
	err := r.Dispatch(context.Background(), &Job{Kind: "missing"})
	if err == nil || !strings.Contains(err.Error(), "no handler registered") {
		t.Fatalf("want unknown-kind error, got %v", err)
	}
}

func TestRegistryDuplicateRegisterPanics(t *testing.T) {
	r := NewRegistry()
	r.Register("k", func(context.Context, *Job) error { return nil })
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("want panic on duplicate register, got nil")
		}
	}()
	r.Register("k", func(context.Context, *Job) error { return nil })
}

func TestRegistryEmptyKindPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("want panic on empty kind")
		}
	}()
	NewRegistry().Register("", func(context.Context, *Job) error { return nil })
}

func TestRegistryNilHandlerPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("want panic on nil handler")
		}
	}()
	NewRegistry().Register("k", nil)
}

func TestRegistryKindsReturnsRegistered(t *testing.T) {
	r := NewRegistry()
	r.Register("alpha", func(context.Context, *Job) error { return nil })
	r.Register("beta", func(context.Context, *Job) error { return nil })
	kinds := r.Kinds()
	if len(kinds) != 2 {
		t.Fatalf("len(Kinds)=%d want 2", len(kinds))
	}
	seen := map[string]bool{kinds[0]: true, kinds[1]: true}
	if !seen["alpha"] || !seen["beta"] {
		t.Fatalf("Kinds=%v want [alpha beta] in some order", kinds)
	}
}

func TestRegistryDispatchPropagatesError(t *testing.T) {
	r := NewRegistry()
	want := errors.New("boom")
	r.Register("k", func(context.Context, *Job) error { return want })
	err := r.Dispatch(context.Background(), &Job{Kind: "k"})
	if !errors.Is(err, want) {
		t.Fatalf("want %v, got %v", want, err)
	}
}
