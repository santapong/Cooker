package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDo_SucceedsImmediately(t *testing.T) {
	calls := 0
	err := Do(context.Background(), Policy{MaxAttempts: 3, Initial: time.Millisecond, Max: time.Millisecond}, func(context.Context) error {
		calls++
		return nil
	})
	if err != nil || calls != 1 {
		t.Fatalf("got err=%v calls=%d, want nil/1", err, calls)
	}
}

func TestDo_RetriesUntilSuccess(t *testing.T) {
	calls := 0
	err := Do(context.Background(), Policy{MaxAttempts: 5, Initial: time.Millisecond, Max: time.Millisecond}, func(context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("got err=%v calls=%d, want nil/3", err, calls)
	}
}

func TestDo_StopsOnNonTransient(t *testing.T) {
	calls := 0
	wantErr := errors.New("permanent")
	err := Do(context.Background(), Policy{
		MaxAttempts: 5,
		Initial:     time.Millisecond,
		Max:         time.Millisecond,
		IsTransient: func(error) bool { return false },
	}, func(context.Context) error {
		calls++
		return wantErr
	})
	if !errors.Is(err, wantErr) || calls != 1 {
		t.Fatalf("got err=%v calls=%d, want permanent/1", err, calls)
	}
}

func TestDo_RespectsContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	err := Do(ctx, Default, func(context.Context) error {
		calls++
		return errors.New("never")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got err=%v, want context.Canceled", err)
	}
	if calls > 1 {
		t.Fatalf("got calls=%d, want at most 1", calls)
	}
}

func TestDo_ExhaustsMaxAttempts(t *testing.T) {
	calls := 0
	wantErr := errors.New("transient")
	err := Do(context.Background(), Policy{MaxAttempts: 4, Initial: time.Millisecond, Max: time.Millisecond}, func(context.Context) error {
		calls++
		return wantErr
	})
	if !errors.Is(err, wantErr) || calls != 4 {
		t.Fatalf("got err=%v calls=%d, want transient/4", err, calls)
	}
}

func TestIsContextErr(t *testing.T) {
	if !IsContextErr(context.Canceled) || !IsContextErr(context.DeadlineExceeded) {
		t.Fatal("IsContextErr should match context errors")
	}
	if IsContextErr(errors.New("other")) {
		t.Fatal("IsContextErr should not match unrelated errors")
	}
}
