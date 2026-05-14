package notifier

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeNotifier records every Send for assertions in tests.
type fakeNotifier struct {
	kind string
	mu   sync.Mutex
	sent []Event
	err  error
}

func (f *fakeNotifier) Kind() string { return f.kind }
func (f *fakeNotifier) Send(_ context.Context, _ Target, event Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, event)
	return f.err
}
func (f *fakeNotifier) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func TestDispatcherFansOutToMatchingTargets(t *testing.T) {
	store := NewMemoryTargetStore()
	slackFake := &fakeNotifier{kind: "slack"}
	discordFake := &fakeNotifier{kind: "discord"}
	reg := NewRegistry()
	reg.Register(slackFake)
	reg.Register(discordFake)
	d := NewDispatcher(store, reg)

	_ = store.Create(context.Background(), Target{ID: "s1", Kind: "slack", Enabled: true})
	_ = store.Create(context.Background(), Target{ID: "d1", Kind: "discord", Enabled: true})

	err := d.Dispatch(context.Background(), Event{Type: EventRunFailed, PipelineName: "x"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if slackFake.count() != 1 || discordFake.count() != 1 {
		t.Fatalf("counts: slack=%d discord=%d", slackFake.count(), discordFake.count())
	}
}

func TestDispatcherRespectsEventTypeFilter(t *testing.T) {
	store := NewMemoryTargetStore()
	fake := &fakeNotifier{kind: "webhook"}
	reg := NewRegistry()
	reg.Register(fake)
	d := NewDispatcher(store, reg)

	_ = store.Create(context.Background(), Target{
		ID:         "w1",
		Kind:       "webhook",
		Enabled:    true,
		EventTypes: []EventType{EventRunFailed}, // only failures
	})

	// A success event must not reach the target.
	if err := d.Dispatch(context.Background(), Event{Type: EventRunSucceeded}); err != nil {
		t.Fatal(err)
	}
	if fake.count() != 0 {
		t.Fatalf("sent on filtered event: count=%d", fake.count())
	}
	// A failure event must reach the target.
	if err := d.Dispatch(context.Background(), Event{Type: EventRunFailed}); err != nil {
		t.Fatal(err)
	}
	if fake.count() != 1 {
		t.Fatalf("filter let in wrong count: %d", fake.count())
	}
}

func TestDispatcherSkipsDisabled(t *testing.T) {
	store := NewMemoryTargetStore()
	fake := &fakeNotifier{kind: "slack"}
	reg := NewRegistry()
	reg.Register(fake)
	d := NewDispatcher(store, reg)

	_ = store.Create(context.Background(), Target{ID: "s1", Kind: "slack", Enabled: false})

	if err := d.Dispatch(context.Background(), Event{Type: EventRunFailed}); err != nil {
		t.Fatal(err)
	}
	if fake.count() != 0 {
		t.Fatalf("disabled target received event: count=%d", fake.count())
	}
}

func TestDispatcherCollectsErrorsPerTarget(t *testing.T) {
	store := NewMemoryTargetStore()
	boomA := &fakeNotifier{kind: "slack", err: errors.New("a boom")}
	boomB := &fakeNotifier{kind: "discord", err: errors.New("b boom")}
	reg := NewRegistry()
	reg.Register(boomA)
	reg.Register(boomB)
	d := NewDispatcher(store, reg)

	_ = store.Create(context.Background(), Target{ID: "s1", Kind: "slack", Enabled: true})
	_ = store.Create(context.Background(), Target{ID: "d1", Kind: "discord", Enabled: true})

	err := d.Dispatch(context.Background(), Event{Type: EventRunFailed})
	if err == nil {
		t.Fatal("want joined error, got nil")
	}
	// errors.Join wraps both — verify both messages surface.
	msg := err.Error()
	if !contains(msg, "a boom") || !contains(msg, "b boom") {
		t.Fatalf("joined error missing per-target context: %s", msg)
	}
}

func TestDispatcherWarnOnUnregisteredKind(t *testing.T) {
	store := NewMemoryTargetStore()
	reg := NewRegistry() // empty
	d := NewDispatcher(store, reg)

	_ = store.Create(context.Background(), Target{ID: "x1", Kind: "unregistered", Enabled: true})

	// Should not error — the dispatcher logs + meters this case
	// rather than failing the whole Dispatch.
	if err := d.Dispatch(context.Background(), Event{Type: EventRunFailed}); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestRegistryDuplicatePanics(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&fakeNotifier{kind: "slack"})
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("want panic on duplicate kind")
		}
	}()
	reg.Register(&fakeNotifier{kind: "slack"})
}

func TestRegistryNilNotifierPanics(t *testing.T) {
	reg := NewRegistry()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("want panic on nil notifier")
		}
	}()
	reg.Register(nil)
}

func TestTargetMatches(t *testing.T) {
	cases := []struct {
		t    Target
		et   EventType
		want bool
	}{
		{Target{Enabled: true}, EventRunFailed, true},
		{Target{Enabled: false}, EventRunFailed, false},
		{Target{Enabled: true, EventTypes: []EventType{EventRunFailed}}, EventRunFailed, true},
		{Target{Enabled: true, EventTypes: []EventType{EventRunFailed}}, EventRunSucceeded, false},
	}
	for i, c := range cases {
		if got := c.t.Matches(c.et); got != c.want {
			t.Errorf("case %d: got %v want %v", i, got, c.want)
		}
	}
}

func TestTargetDecodeConfig(t *testing.T) {
	t0 := Target{Config: json.RawMessage(`{"webhookUrl":"https://example.com"}`)}
	var cfg SlackConfig
	if err := t0.DecodeConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.WebhookURL != "https://example.com" {
		t.Fatalf("got %q", cfg.WebhookURL)
	}
}

func TestMemoryTargetStoreCRUD(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryTargetStore()
	if err := s.Create(ctx, Target{ID: "t1", Kind: "slack", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, Target{ID: "t1", Kind: "slack"}); err == nil {
		t.Fatal("want duplicate-id error")
	}
	got, err := s.Get(ctx, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "slack" || !got.Enabled {
		t.Fatalf("got %+v", got)
	}
	got.Enabled = false
	if err := s.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	enabled, _ := s.ListEnabled(ctx)
	if len(enabled) != 0 {
		t.Fatalf("ListEnabled after disable: %d", len(enabled))
	}
	if err := s.Delete(ctx, "t1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, "t1"); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("want ErrTargetNotFound, got %v", err)
	}
}

// Smoke test that the timeout on Dispatcher.SendTimeout actually
// cancels a slow notifier.
func TestDispatcherHonoursSendTimeout(t *testing.T) {
	store := NewMemoryTargetStore()
	slow := &slowNotifier{kind: "webhook", sleep: 500 * time.Millisecond}
	reg := NewRegistry()
	reg.Register(slow)
	d := NewDispatcher(store, reg)
	d.SendTimeout = 30 * time.Millisecond

	_ = store.Create(context.Background(), Target{ID: "w1", Kind: "webhook", Enabled: true})

	start := time.Now()
	err := d.Dispatch(context.Background(), Event{Type: EventRunFailed})
	dur := time.Since(start)
	if err == nil {
		t.Fatal("want timeout error")
	}
	if dur > 200*time.Millisecond {
		t.Fatalf("SendTimeout not enforced: dur=%s", dur)
	}
}

type slowNotifier struct {
	kind  string
	sleep time.Duration
	done  int32
}

func (s *slowNotifier) Kind() string { return s.kind }
func (s *slowNotifier) Send(ctx context.Context, _ Target, _ Event) error {
	select {
	case <-time.After(s.sleep):
		atomic.AddInt32(&s.done, 1)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
