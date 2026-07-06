package service

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/deployer"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
)

// fakeWeighted records DeployWeighted calls and can be told to fail.
// Guarded by a mutex so the concurrency tests (PM26-07-02) can assert
// the call count without racing the recorder.
type fakeWeighted struct {
	mu      sync.Mutex
	calls   []deployer.WeightedRequest
	failNow bool
	// failFrom makes DeployWeighted start failing from the Nth call
	// (1-based). 0 disables. Lets a test succeed on Start's split but
	// fail on the subsequent promote/abort.
	failFrom int
}

func (f *fakeWeighted) Deploy(_ context.Context, _ deployer.Request) (deployer.Result, error) {
	return deployer.Result{}, nil
}

func (f *fakeWeighted) DeployWeighted(_ context.Context, req deployer.WeightedRequest) (deployer.WeightedResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	n := len(f.calls)
	fail := f.failNow || (f.failFrom > 0 && n >= f.failFrom)
	f.mu.Unlock()
	if fail {
		return deployer.WeightedResult{}, errors.New("apply boom")
	}
	return deployer.WeightedResult{CanaryReplicas: 1, StableReplicas: 3}, nil
}

func (f *fakeWeighted) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeWeighted) lastWeight() int {
	f.mu.Lock()
	defer f.mu.Unlock()
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

// PM26-07-02: concurrent terminal transitions must resolve exactly once.
// Two goroutines race Promote vs Abort on the same progressing canary;
// the store CAS (ClaimTerminal) must let exactly one win, so exactly
// one extra DeployWeighted lands beyond the one Start issued.
func TestCanary_ConcurrentPromoteAbortResolvesOnce(t *testing.T) {
	app := canaryApp()
	app.Canary.AutoPromote = false // manual: no sweeper interference
	fw := &fakeWeighted{}
	svc, st := newCanaryFixture(t, app, fw)
	started, err := svc.Start(context.Background(), app, "run1", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if fw.count() != 1 {
		t.Fatalf("Start should issue exactly 1 weighted deploy, got %d", fw.count())
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = svc.Promote(context.Background(), "app1") }()
	go func() { defer wg.Done(); _, _ = svc.Abort(context.Background(), "app1", "operator") }()
	wg.Wait()

	// Exactly one of promote/abort performed a cluster change.
	if got := fw.count(); got != 2 {
		t.Errorf("expected exactly 1 terminal DeployWeighted (total 2), got total %d", got)
	}
	// The single row is terminal.
	c, err := st.AppCanaries.Get(context.Background(), started.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Status.IsTerminal() {
		t.Errorf("canary must be terminal after the race, got %s", c.Status)
	}
	if _, err := st.AppCanaries.GetActive(context.Background(), "app1"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("no canary should remain active, got %v", err)
	}
}

// PM26-07-06: Start reserves the slot with a pending row before building.
// A build failure flips that row to failed (not a second row) and frees
// the one-per-app slot so the next Start succeeds.
func TestCanary_StartBuildFailureFreesSlot(t *testing.T) {
	app := canaryApp()
	st := memory.New()
	if err := st.Apps.Create(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	failing := &fakeBuilder{err: errors.New("clone exploded")}
	svc := NewCanaryService(st.Apps, st.AppCanaries, st.AppDeploys, failing, &fakeWeighted{})

	if _, err := svc.Start(context.Background(), app, "run1", io.Discard); err == nil {
		t.Fatal("expected Start to fail when the build fails")
	}
	// No active canary, and the slot is free for a retry.
	if _, err := st.AppCanaries.GetActive(context.Background(), "app1"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("failed Start must leave no active canary, got %v", err)
	}
	// A subsequent Start with a working builder must NOT hit ErrCanaryInFlight.
	svcOK := NewCanaryService(st.Apps, st.AppCanaries, st.AppDeploys,
		&fakeBuilder{image: "reg/shop:new"}, &fakeWeighted{})
	if _, err := svcOK.Start(context.Background(), app, "run2", io.Discard); err != nil {
		t.Fatalf("retry after a failed Start must succeed, got %v", err)
	}
}

// PM26-07-06: the pending row is created before the split, so a second
// concurrent Start (pending vs pending) is rejected by the unique slot.
func TestCanary_StartPendingRowSerializes(t *testing.T) {
	app := canaryApp()
	fw := &fakeWeighted{}
	svc, st := newCanaryFixture(t, app, fw)
	if _, err := svc.Start(context.Background(), app, "run1", io.Discard); err != nil {
		t.Fatal(err)
	}
	// The row progressed past pending to progressing after a successful split.
	c, err := st.AppCanaries.GetActive(context.Background(), "app1")
	if err != nil {
		t.Fatalf("expected an active (progressing) canary, got %v", err)
	}
	if c.Status != model.CanaryProgressing {
		t.Errorf("after a successful Start the row should be progressing, got %s", c.Status)
	}
}

// Regression (self-review #4/#5/#7): a DeployWeighted failure AFTER the
// CAS claim must revert the row to progressing so the canary stays
// active and retryable, not stranded terminal-in-DB / cluster-unchanged.
func TestCanary_PromoteDeployFailureRevertsToProgressing(t *testing.T) {
	app := canaryApp()
	app.Canary.AutoPromote = false
	fw := &fakeWeighted{failFrom: 2} // Start's split (call 1) ok; promote (call 2) fails
	svc, st := newCanaryFixture(t, app, fw)
	started, err := svc.Start(context.Background(), app, "run1", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Promote(context.Background(), "app1"); err == nil {
		t.Fatal("expected promote to fail when the split apply fails")
	}
	// The canary must still be active (progressing), not stranded terminal.
	got, err := st.AppCanaries.GetActive(context.Background(), "app1")
	if err != nil {
		t.Fatalf("canary must remain active after a failed promote, got %v", err)
	}
	if got.ID != started.ID || got.Status != model.CanaryProgressing {
		t.Errorf("expected the same progressing canary, got %+v", got)
	}
}

// Regression (self-review #1): the promoted image must round-trip through
// the store Update (Start now sets images via Update, not Create). The
// memory store swaps the whole struct, so this asserts the image survives
// a fresh read — the behaviour the Postgres Update SET clause must match.
func TestCanary_ImagesPersistedThroughUpdate(t *testing.T) {
	app := canaryApp()
	fw := &fakeWeighted{}
	svc, st := newCanaryFixture(t, app, fw)
	if _, err := svc.Start(context.Background(), app, "run1", io.Discard); err != nil {
		t.Fatal(err)
	}
	got, err := st.AppCanaries.GetActive(context.Background(), "app1")
	if err != nil {
		t.Fatal(err)
	}
	if got.CanaryImage != "reg/shop:new" || got.StableImage == "" {
		t.Errorf("images must survive the pending->progressing Update, got stable=%q canary=%q",
			got.StableImage, got.CanaryImage)
	}
}

// Regression (self-review #2): the auto-promote window anchors to
// split-live, not to Start-entry, so build time doesn't silently consume
// it. A builder that advances the clock simulates a slow build.
func TestCanary_PromoteAfterAnchoredPostBuild(t *testing.T) {
	app := canaryApp() // AutoPromote=true, window 60s
	base := time.Now()
	cur := base
	clock := func() time.Time { return cur }
	// Builder that "takes" 10 minutes of wall time.
	slow := builderFunc(func() { cur = cur.Add(10 * time.Minute) })
	st := memory.New()
	if err := st.Apps.Create(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	svc := NewCanaryService(st.Apps, st.AppCanaries, st.AppDeploys, slow, &fakeWeighted{}, WithCanaryClock(clock))
	c, err := svc.Start(context.Background(), app, "run1", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.PromoteAfter == nil {
		t.Fatal("auto-promote canary should set PromoteAfter")
	}
	// PromoteAfter must be ~window after the POST-build time (base+10m),
	// not after base — otherwise it's already in the past.
	wantMin := base.Add(10 * time.Minute)
	if c.PromoteAfter.Before(wantMin) {
		t.Errorf("PromoteAfter %v must anchor to split-live (>= %v), not to Start-entry",
			c.PromoteAfter, wantMin)
	}
}

// Regression (self-review #3/#6/#9): an orphaned pending row is reaped by
// the sweep, freeing the one-per-app slot.
func TestCanary_SweepReapsStalePending(t *testing.T) {
	app := canaryApp()
	st := memory.New()
	if err := st.Apps.Create(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	// A pending row left behind by a crashed Start, an hour+ old.
	old := time.Now().Add(-2 * time.Hour)
	if err := st.AppCanaries.Create(context.Background(), &model.AppCanary{
		ID: "stuck", AppID: app.ID, Status: model.CanaryPending, StartedAt: old,
	}); err != nil {
		t.Fatal(err)
	}
	svc := NewCanaryService(st.Apps, st.AppCanaries, st.AppDeploys,
		&fakeBuilder{image: "reg/shop:new"}, &fakeWeighted{})
	svc.SweepAutoPromote(context.Background())

	// Slot is free: a fresh Start must not hit ErrCanaryInFlight.
	if _, err := svc.Start(context.Background(), app, "run2", io.Discard); err != nil {
		t.Fatalf("after reaping the stale pending row, Start should succeed, got %v", err)
	}
}

// builderFunc adapts a side-effect func to canaryImageBuilder, returning
// a fixed image after running the effect (used to simulate build time).
type builderFunc func()

func (b builderFunc) BuildAndPushImage(_ context.Context, _ *model.App, _ string, _ io.Writer) (string, *model.Pipeline, *model.PipelineRun, error) {
	b()
	return "reg/shop:new", &model.Pipeline{ID: "p"}, &model.PipelineRun{ID: "r", Status: model.RunStatusSuccess}, nil
}

// fakeCanaryProber is a weighted deployer that ALSO reports canary
// readiness, so the service takes the PM26-07-04 canary-workload path.
type fakeCanaryProber struct {
	fakeWeighted
	ready bool
}

func (f *fakeCanaryProber) CanaryReady(_ context.Context, _, _ string) (bool, string, error) {
	if f.ready {
		return true, "2/2 ready", nil
	}
	return false, "0/2 ready", nil
}

// PM26-07-04: the sweep must probe the CANARY workload, not the app. An
// unhealthy canary behind a (would-be healthy) app prober must roll back,
// not auto-promote.
func TestCanary_SweepProbesCanaryWorkloadNotApp(t *testing.T) {
	app := canaryApp()
	fw := &fakeCanaryProber{ready: false} // canary pods crash-looping
	now := time.Now()
	clock := func() time.Time { return now }
	// App prober would say HEALTHY (it sees the stable pods) — must be ignored.
	appHealthy := ProberFunc(func(_ context.Context, _ *model.App) (model.AppHealth, string, string) {
		return model.AppHealthHealthy, "stable ok", ""
	})
	svc, st := newCanaryFixture(t, app, fw, WithCanaryClock(clock), WithCanaryProber(appHealthy))
	started, err := svc.Start(context.Background(), app, "run1", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute) // past the window
	svc.SweepAutoPromote(context.Background())

	c, err := st.AppCanaries.Get(context.Background(), started.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != model.CanaryAborted {
		t.Errorf("unhealthy canary workload must auto-rollback despite a healthy app probe, got %s", c.Status)
	}
}

// PM26-07-07: the sweep reaps a MANUAL (AutoPromote=false) canary whose
// app was deleted, rather than leaving it progressing forever.
func TestCanary_SweepReapsManualCanaryWhenAppGone(t *testing.T) {
	app := canaryApp()
	app.Canary.AutoPromote = false
	fw := &fakeWeighted{}
	svc, st := newCanaryFixture(t, app, fw)
	started, err := svc.Start(context.Background(), app, "run1", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	// Delete the app out from under the canary (memory store: no cascade).
	if err := st.Apps.Delete(context.Background(), "app1"); err != nil {
		t.Fatal(err)
	}
	svc.SweepAutoPromote(context.Background())

	c, err := st.AppCanaries.Get(context.Background(), started.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != model.CanaryAborted {
		t.Errorf("manual canary with a deleted app must be reaped (aborted), got %s", c.Status)
	}
}
