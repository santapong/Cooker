package service

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/deployer"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
)

// fakeWeighted records DeployWeighted calls and can be told to fail.
type fakeWeighted struct {
	calls   []deployer.WeightedRequest
	failNow bool
}

func (f *fakeWeighted) Deploy(_ context.Context, _ deployer.Request) (deployer.Result, error) {
	return deployer.Result{}, nil
}

func (f *fakeWeighted) DeployWeighted(_ context.Context, req deployer.WeightedRequest) (deployer.WeightedResult, error) {
	f.calls = append(f.calls, req)
	if f.failNow {
		return deployer.WeightedResult{}, errors.New("apply boom")
	}
	return deployer.WeightedResult{CanaryReplicas: 1, StableReplicas: 3}, nil
}

func (f *fakeWeighted) lastWeight() int {
	if len(f.calls) == 0 {
		return -1
	}
	return f.calls[len(f.calls)-1].Weight
}

// fakeBuilder stands in for AppDeployer.BuildAndPushImage so tests don't
// clone/build a real repo.
type fakeBuilder struct {
	image string
	err   error
}

func (f *fakeBuilder) BuildAndPushImage(_ context.Context, _ *model.App, _ string, _ io.Writer) (string, *model.Pipeline, *model.PipelineRun, error) {
	if f.err != nil {
		return "", nil, nil, f.err
	}
	return f.image, &model.Pipeline{ID: "p"}, &model.PipelineRun{ID: "r", Status: model.RunStatusSuccess}, nil
}

func canaryApp() *model.App {
	return &model.App{
		ID:           "app1",
		Name:         "shop",
		GitHubRepo:   "o/shop",
		Branch:       "main",
		DeployTarget: model.DeployTarget{Kind: model.DeployTargetKubernetes, Namespace: "prod"},
		Canary:       model.CanaryConfig{Strategy: model.DeployStrategyCanary, Weight: 20, AutoPromote: true, HealthWindowSeconds: 60},
	}
}

func newCanaryFixture(t *testing.T, app *model.App, weighted deployer.WeightedDeployer, opts ...CanaryOption) (*CanaryService, *store.Store) {
	t.Helper()
	st := memory.New()
	if err := st.Apps.Create(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	b := &fakeBuilder{image: "reg/shop:new"}
	svc := NewCanaryService(st.Apps, st.AppCanaries, st.AppDeploys, b, weighted, opts...)
	return svc, st
}

func TestCanary_StartEstablishesSplitAndPersists(t *testing.T) {
	app := canaryApp()
	fw := &fakeWeighted{}
	svc, st := newCanaryFixture(t, app, fw)

	c, err := svc.Start(context.Background(), app, "run1", io.Discard)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if c.Status != model.CanaryProgressing || c.Weight != 20 || c.CanaryImage != "reg/shop:new" {
		t.Errorf("unexpected canary: %+v", c)
	}
	if fw.lastWeight() != 20 {
		t.Errorf("weighted deploy called with weight %d, want 20", fw.lastWeight())
	}
	if c.PromoteAfter == nil {
		t.Error("auto-promote canary should set PromoteAfter")
	}
	// Persisted and discoverable as the active canary.
	got, err := st.AppCanaries.GetActive(context.Background(), "app1")
	if err != nil {
		t.Fatalf("active: %v", err)
	}
	if got.ID != c.ID {
		t.Errorf("active canary mismatch: %s vs %s", got.ID, c.ID)
	}
}

func TestCanary_StartUnsupportedTarget(t *testing.T) {
	app := canaryApp()
	app.DeployTarget.Kind = model.DeployTargetDockerHost
	// Even with a weighted deployer wired, a non-K8s target is unsupported.
	svc, _ := newCanaryFixture(t, app, &fakeWeighted{})
	_, err := svc.Start(context.Background(), app, "run1", io.Discard)
	if !errors.Is(err, ErrCanaryUnsupported) {
		t.Fatalf("expected ErrCanaryUnsupported, got %v", err)
	}
	if !errors.Is(err, deployer.ErrCanaryUnsupported) {
		t.Errorf("error should also match the deployer sentinel via Is")
	}
}

func TestCanary_StartNilWeightedDeployerUnsupported(t *testing.T) {
	app := canaryApp()
	st := memory.New()
	_ = st.Apps.Create(context.Background(), app)
	svc := NewCanaryService(st.Apps, st.AppCanaries, st.AppDeploys, &fakeBuilder{image: "reg/shop:new"}, nil)
	_, err := svc.Start(context.Background(), app, "run1", io.Discard)
	if !errors.Is(err, ErrCanaryUnsupported) {
		t.Fatalf("nil weighted deployer must yield ErrCanaryUnsupported, got %v", err)
	}
}

func TestCanary_StartRejectsSecondInFlight(t *testing.T) {
	app := canaryApp()
	fw := &fakeWeighted{}
	svc, _ := newCanaryFixture(t, app, fw)
	if _, err := svc.Start(context.Background(), app, "run1", io.Discard); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Start(context.Background(), app, "run2", io.Discard)
	if !errors.Is(err, ErrCanaryInFlight) {
		t.Fatalf("expected ErrCanaryInFlight, got %v", err)
	}
}

func TestCanary_PromoteShiftsTo100(t *testing.T) {
	app := canaryApp()
	fw := &fakeWeighted{}
	svc, st := newCanaryFixture(t, app, fw)
	if _, err := svc.Start(context.Background(), app, "run1", io.Discard); err != nil {
		t.Fatal(err)
	}
	c, err := svc.Promote(context.Background(), "app1")
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if c.Status != model.CanaryPromoted || c.Weight != 100 || c.ResolvedAt == nil {
		t.Errorf("unexpected promoted canary: %+v", c)
	}
	if fw.lastWeight() != 100 {
		t.Errorf("promote should re-balance to weight 100, got %d", fw.lastWeight())
	}
	// No longer active.
	if _, err := st.AppCanaries.GetActive(context.Background(), "app1"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("promoted canary should not be active, got %v", err)
	}
}

func TestCanary_AbortRollsBackToZero(t *testing.T) {
	app := canaryApp()
	fw := &fakeWeighted{}
	svc, _ := newCanaryFixture(t, app, fw)
	if _, err := svc.Start(context.Background(), app, "run1", io.Discard); err != nil {
		t.Fatal(err)
	}
	c, err := svc.Abort(context.Background(), "app1", "looks bad")
	if err != nil {
		t.Fatalf("abort: %v", err)
	}
	if c.Status != model.CanaryAborted || c.Weight != 0 || c.Message != "looks bad" {
		t.Errorf("unexpected aborted canary: %+v", c)
	}
	if fw.lastWeight() != 0 {
		t.Errorf("abort should re-balance to weight 0, got %d", fw.lastWeight())
	}
}

func TestCanary_PromoteNoActive(t *testing.T) {
	app := canaryApp()
	svc, _ := newCanaryFixture(t, app, &fakeWeighted{})
	_, err := svc.Promote(context.Background(), "app1")
	if !errors.Is(err, ErrNoActiveCanary) {
		t.Fatalf("expected ErrNoActiveCanary, got %v", err)
	}
}

func TestCanary_SweepAutoPromotesHealthy(t *testing.T) {
	app := canaryApp()
	fw := &fakeWeighted{}
	now := time.Now()
	clock := func() time.Time { return now }
	healthy := ProberFunc(func(_ context.Context, _ *model.App) (model.AppHealth, string, string) {
		return model.AppHealthHealthy, "ok", ""
	})
	svc, st := newCanaryFixture(t, app, fw, WithCanaryClock(clock), WithCanaryProber(healthy))
	if _, err := svc.Start(context.Background(), app, "run1", io.Discard); err != nil {
		t.Fatal(err)
	}
	// Before the window elapses: sweep is a no-op.
	svc.SweepAutoPromote(context.Background())
	if c, _ := st.AppCanaries.GetActive(context.Background(), "app1"); c == nil {
		t.Fatal("canary should still be progressing before the window")
	}
	// Advance past the window and sweep again → auto-promote.
	now = now.Add(2 * time.Minute)
	svc.SweepAutoPromote(context.Background())
	if _, err := st.AppCanaries.GetActive(context.Background(), "app1"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("healthy canary past window should auto-promote, still active: %v", err)
	}
	if fw.lastWeight() != 100 {
		t.Errorf("auto-promote should land at weight 100, got %d", fw.lastWeight())
	}
}

func TestCanary_SweepAutoRollsBackUnhealthy(t *testing.T) {
	app := canaryApp()
	fw := &fakeWeighted{}
	now := time.Now()
	clock := func() time.Time { return now }
	failing := ProberFunc(func(_ context.Context, _ *model.App) (model.AppHealth, string, string) {
		return model.AppHealthFailed, "crashlooping", ""
	})
	svc, st := newCanaryFixture(t, app, fw, WithCanaryClock(clock), WithCanaryProber(failing))
	if _, err := svc.Start(context.Background(), app, "run1", io.Discard); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	svc.SweepAutoPromote(context.Background())
	// Aborted: no longer active, and the last weighted call was a rollback.
	if _, err := st.AppCanaries.GetActive(context.Background(), "app1"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unhealthy canary should auto-rollback, still active: %v", err)
	}
	if fw.lastWeight() != 0 {
		t.Errorf("auto-rollback should land at weight 0, got %d", fw.lastWeight())
	}
}

func TestCanary_SweepLeavesManualCanary(t *testing.T) {
	app := canaryApp()
	app.Canary.AutoPromote = false // manual canary
	fw := &fakeWeighted{}
	now := time.Now()
	clock := func() time.Time { return now }
	svc, st := newCanaryFixture(t, app, fw, WithCanaryClock(clock))
	if _, err := svc.Start(context.Background(), app, "run1", io.Discard); err != nil {
		t.Fatal(err)
	}
	now = now.Add(1 * time.Hour)
	svc.SweepAutoPromote(context.Background())
	// A manual canary is never auto-resolved — it waits for an operator.
	if _, err := st.AppCanaries.GetActive(context.Background(), "app1"); err != nil {
		t.Errorf("manual canary must stay active through sweeps, got %v", err)
	}
}

func TestCanary_StartFailedSplitRecordsFailedState(t *testing.T) {
	app := canaryApp()
	fw := &fakeWeighted{failNow: true}
	svc, st := newCanaryFixture(t, app, fw)
	_, err := svc.Start(context.Background(), app, "run1", io.Discard)
	if err == nil {
		t.Fatal("expected error when the split apply fails")
	}
	// A failed-state row is recorded for the UI; it is terminal (not active).
	if _, err := st.AppCanaries.GetActive(context.Background(), "app1"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("failed canary must be terminal, got active: %v", err)
	}
}

// PM26-07-01 pins: stableImageFor must resolve a real serving image —
// the original implementation queried GetActive (guaranteed empty at
// that point in Start) and returned "", rendering the stable Deployment
// with a blank image and leaving Abort with no rollback target.

func TestCanary_StartWithNoHistorySplitsCanaryImageAgainstItself(t *testing.T) {
	app := canaryApp()
	fw := &fakeWeighted{}
	svc, _ := newCanaryFixture(t, app, fw)

	c, err := svc.Start(context.Background(), app, "run1", io.Discard)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if len(fw.calls) == 0 {
		t.Fatal("weighted deploy never called")
	}
	if got := fw.calls[0].StableImage; got == "" {
		t.Fatal("stable image must never be empty — an empty image is rejected by the API server")
	} else if got != "reg/shop:new" {
		t.Errorf("no history: stable should fall back to the canary image, got %q", got)
	}
	if c.StableImage != "reg/shop:new" {
		t.Errorf("persisted StableImage = %q, want canary-image fallback", c.StableImage)
	}
}

func TestCanary_StartUsesLatestPromotedImageAsStable(t *testing.T) {
	app := canaryApp()
	fw := &fakeWeighted{}
	svc, st := newCanaryFixture(t, app, fw)

	// A previous rollout was promoted: its canary image is what serves.
	old := time.Now().Add(-time.Hour)
	if err := st.AppCanaries.Create(context.Background(), &model.AppCanary{
		ID: "prev", AppID: app.ID, CanaryImage: "reg/shop:v1", StableImage: "reg/shop:v0",
		Status: model.CanaryPromoted, ResolvedAt: &old,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Start(context.Background(), app, "run2", io.Discard); err != nil {
		t.Fatalf("start: %v", err)
	}
	if got := fw.calls[0].StableImage; got != "reg/shop:v1" {
		t.Errorf("stable image = %q, want the promoted image reg/shop:v1", got)
	}
}

func TestCanary_StartPrefersNewerDeployOverOlderPromote(t *testing.T) {
	app := canaryApp()
	fw := &fakeWeighted{}
	svc, st := newCanaryFixture(t, app, fw)

	promoted := time.Now().Add(-2 * time.Hour)
	if err := st.AppCanaries.Create(context.Background(), &model.AppCanary{
		ID: "prev", AppID: app.ID, CanaryImage: "reg/shop:v1",
		Status: model.CanaryPromoted, ResolvedAt: &promoted,
	}); err != nil {
		t.Fatal(err)
	}
	// A plain deploy shipped after that promote — it supersedes it.
	if err := st.AppDeploys.Create(context.Background(), &model.AppDeploy{
		ID: "d1", AppID: app.ID, ImageRef: "reg/shop:v2",
		Status: model.RunStatusSuccess, CreatedAt: time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Start(context.Background(), app, "run3", io.Discard); err != nil {
		t.Fatalf("start: %v", err)
	}
	if got := fw.calls[0].StableImage; got != "reg/shop:v2" {
		t.Errorf("stable image = %q, want the newer deploy's reg/shop:v2", got)
	}
}
