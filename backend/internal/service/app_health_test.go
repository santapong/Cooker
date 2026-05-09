package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cooker-ci/cooker/internal/model"
	"github.com/cooker-ci/cooker/internal/store"
)

// fakeAppLister is a minimal in-memory AppLister for the health-check
// tests. Records every UpdateHealth so assertions can pin the exact
// (status, message, ts) tuple the checker wrote.
type fakeAppLister struct {
	mu         sync.Mutex
	listResult []*model.App
	listErr    error
	writes     []healthWrite
	writeErr   error
}

type healthWrite struct {
	id      string
	status  model.AppHealth
	message string
	at      time.Time
}

func (f *fakeAppLister) List(_ context.Context) ([]*model.App, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]*model.App, len(f.listResult))
	copy(out, f.listResult)
	return out, nil
}

func (f *fakeAppLister) UpdateHealth(_ context.Context, id string, status model.AppHealth, msg string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.writeErr != nil {
		return f.writeErr
	}
	f.writes = append(f.writes, healthWrite{id: id, status: status, message: msg, at: at})
	return nil
}

func (f *fakeAppLister) snapshot() []healthWrite {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]healthWrite, len(f.writes))
	copy(out, f.writes)
	return out
}

func TestAppHealthChecker_TickWritesPerTargetVerdict(t *testing.T) {
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	apps := &fakeAppLister{
		listResult: []*model.App{
			{ID: "k", Name: "k-app", DeployTarget: model.DeployTarget{Kind: model.DeployTargetKubernetes}},
			{ID: "c", Name: "c-app", DeployTarget: model.DeployTarget{Kind: model.DeployTargetCloudRun}},
			{ID: "u", Name: "u-app", DeployTarget: model.DeployTarget{Kind: "weird-future-kind"}},
		},
	}

	checker := NewAppHealthChecker(apps,
		WithClock(func() time.Time { return now }),
		WithProber(model.DeployTargetKubernetes, ProberFunc(func(_ context.Context, a *model.App) (model.AppHealth, string) {
			return model.AppHealthHealthy, "all replicas ready"
		})),
		WithProber(model.DeployTargetCloudRun, ProberFunc(func(_ context.Context, a *model.App) (model.AppHealth, string) {
			return model.AppHealthDegraded, "latest revision not ready"
		})),
	)

	checker.tick(context.Background())

	got := apps.snapshot()
	if len(got) != 3 {
		t.Fatalf("expected 3 writes, got %d (%v)", len(got), got)
	}

	want := map[string]healthWrite{
		"k": {id: "k", status: model.AppHealthHealthy, message: "all replicas ready", at: now},
		"c": {id: "c", status: model.AppHealthDegraded, message: "latest revision not ready", at: now},
		"u": {id: "u", status: model.AppHealthUnknown, message: "no health probe registered for target kind weird-future-kind", at: now},
	}
	for _, w := range got {
		if got, ok := want[w.id]; ok {
			if w != got {
				t.Errorf("write for %q: got %+v want %+v", w.id, w, got)
			}
		} else {
			t.Errorf("unexpected write: %+v", w)
		}
	}
}

func TestAppHealthChecker_ListErrorIsNonFatal(t *testing.T) {
	apps := &fakeAppLister{listErr: errors.New("database down")}
	checker := NewAppHealthChecker(apps)
	checker.tick(context.Background())
	if got := apps.snapshot(); len(got) != 0 {
		t.Errorf("expected zero writes on list error, got %d", len(got))
	}
}

func TestAppHealthChecker_DeletedAppRaceTolerated(t *testing.T) {
	apps := &fakeAppLister{
		listResult: []*model.App{
			{ID: "deleted", Name: "x", DeployTarget: model.DeployTarget{Kind: model.DeployTargetKubernetes}},
		},
		writeErr: store.ErrNotFound,
	}
	checker := NewAppHealthChecker(apps,
		WithProber(model.DeployTargetKubernetes, ProberFunc(func(_ context.Context, _ *model.App) (model.AppHealth, string) {
			return model.AppHealthHealthy, ""
		})),
	)
	// Should not panic / log.Fatal — ErrNotFound is benign.
	checker.tick(context.Background())
}

func TestAppHealthChecker_RunRespectsContextCancellation(t *testing.T) {
	apps := &fakeAppLister{}
	checker := NewAppHealthChecker(apps,
		WithAppHealthInterval(50*time.Millisecond),
	)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		_ = checker.Run(ctx)
		close(done)
	}()

	// Cancel after the immediate first-tick path runs.
	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}

func TestWithProber_NilUnregisters(t *testing.T) {
	apps := &fakeAppLister{}
	checker := NewAppHealthChecker(apps,
		WithProber(model.DeployTargetKubernetes, ProberFunc(func(_ context.Context, _ *model.App) (model.AppHealth, string) {
			return model.AppHealthHealthy, "registered"
		})),
		WithProber(model.DeployTargetKubernetes, nil), // unregister
	)
	got, msg := checker.proberFor(model.DeployTargetKubernetes).Probe(context.Background(), &model.App{
		DeployTarget: model.DeployTarget{Kind: model.DeployTargetKubernetes},
	})
	if got != model.AppHealthUnknown {
		t.Errorf("after unregister: got %q want unknown", got)
	}
	if msg == "" {
		t.Error("expected non-empty message after unregister")
	}
}
