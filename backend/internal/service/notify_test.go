package service

import (
	"context"
	"sync"
	"testing"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/notifier"
)

// captureNotifier records the events it is asked to send. Registered
// under a chosen kind so a matching Target routes to it.
type captureNotifier struct {
	kind string
	mu   sync.Mutex
	got  []notifier.EventType
}

func (c *captureNotifier) Kind() string { return c.kind }
func (c *captureNotifier) Send(_ context.Context, _ notifier.Target, e notifier.Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, e.Type)
	return nil
}
func (c *captureNotifier) events() []notifier.EventType {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]notifier.EventType(nil), c.got...)
}

// dispatcherWithCapture wires a real dispatcher over a memory target
// store with one enabled wildcard target routed to cap.
func dispatcherWithCapture(t *testing.T, cap *captureNotifier) *notifier.Dispatcher {
	t.Helper()
	store := notifier.NewMemoryTargetStore()
	if err := store.Create(context.Background(), notifier.Target{
		ID: "t1", Kind: cap.kind, Enabled: true, // empty EventTypes = wildcard
	}); err != nil {
		t.Fatal(err)
	}
	reg := notifier.NewRegistry()
	reg.Register(cap)
	return notifier.NewDispatcher(store, reg)
}

func TestNotifyRunOutcome_EmitsFailureAndSuccess(t *testing.T) {
	cap := &captureNotifier{kind: "webhook"}
	disp := dispatcherWithCapture(t, cap)
	p := &model.Pipeline{ID: "p1", Name: "checkout"}

	NotifyRunOutcome(disp, p, &model.PipelineRun{ID: "r1", Status: model.RunStatusFailed, Error: "boom"}, nil)
	NotifyRunOutcome(disp, p, &model.PipelineRun{ID: "r2", Status: model.RunStatusSuccess}, nil)
	// execErr path (ctx-deadline style) maps to run.failed too.
	NotifyRunOutcome(disp, p, &model.PipelineRun{ID: "r3", Status: model.RunStatusRunning}, context.DeadlineExceeded)

	got := cap.events()
	want := []notifier.EventType{notifier.EventRunFailed, notifier.EventRunSucceeded, notifier.EventRunFailed}
	if len(got) != len(want) {
		t.Fatalf("emitted %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestNotifyRunOutcome_NilDispatcherIsNoop(t *testing.T) {
	// Must not panic.
	NotifyRunOutcome(nil, &model.Pipeline{}, &model.PipelineRun{Status: model.RunStatusFailed}, nil)
}

func TestNotifyDeployOutcome_MapsBuildAndDeployFailures(t *testing.T) {
	cap := &captureNotifier{kind: "webhook"}
	disp := dispatcherWithCapture(t, cap)
	app := &model.App{ID: "a1", Name: "shop", DeployTarget: model.DeployTarget{Namespace: "prod"}}

	NotifyDeployOutcome(disp, app, "r1", "", true, false)                // success
	NotifyDeployOutcome(disp, app, "r2", "kaniko exploded", false, true) // build failed
	NotifyDeployOutcome(disp, app, "r3", "apply rejected", false, false) // deploy failed

	got := cap.events()
	want := []notifier.EventType{notifier.EventDeploySucceeded, notifier.EventBuildFailed, notifier.EventDeployFailed}
	if len(got) != len(want) {
		t.Fatalf("emitted %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestNotifyCanary_EmitsGivenType(t *testing.T) {
	cap := &captureNotifier{kind: "webhook"}
	disp := dispatcherWithCapture(t, cap)
	app := &model.App{ID: "a1", Name: "shop"}
	c := &model.AppCanary{ID: "c1", RunID: "r1", Status: model.CanaryPromoted}

	NotifyCanary(disp, app, c, notifier.EventCanaryPromoted, "")
	if got := cap.events(); len(got) != 1 || got[0] != notifier.EventCanaryPromoted {
		t.Fatalf("emitted %v, want [canary.promoted]", got)
	}
}
