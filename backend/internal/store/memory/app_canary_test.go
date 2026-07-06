package memory

import (
	"context"
	"errors"
	"testing"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

func newProgressingCanary(id, appID string) *model.AppCanary {
	return &model.AppCanary{
		ID:                  id,
		AppID:               appID,
		RunID:               "run-" + id,
		StableImage:         "reg/app:stable",
		CanaryImage:         "reg/app:new",
		Weight:              10,
		Status:              model.CanaryProgressing,
		AutoPromote:         true,
		HealthWindowSeconds: 60,
		Healthy:             true,
	}
}

func TestAppCanaryStore_CreateGetActive(t *testing.T) {
	st := New()
	ctx := context.Background()
	c := newProgressingCanary("c1", "a1")
	if err := st.AppCanaries.Create(ctx, c); err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.StartedAt.IsZero() {
		t.Error("Create should stamp StartedAt")
	}

	got, err := st.AppCanaries.GetActive(ctx, "a1")
	if err != nil {
		t.Fatalf("get active: %v", err)
	}
	if got.ID != "c1" || got.Weight != 10 || got.Status != model.CanaryProgressing {
		t.Errorf("unexpected active canary: %+v", got)
	}
}

func TestAppCanaryStore_OneActivePerApp(t *testing.T) {
	st := New()
	ctx := context.Background()
	if err := st.AppCanaries.Create(ctx, newProgressingCanary("c1", "a1")); err != nil {
		t.Fatal(err)
	}
	// Second progressing canary for the same app must conflict.
	err := st.AppCanaries.Create(ctx, newProgressingCanary("c2", "a1"))
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected ErrConflict for a second active canary, got %v", err)
	}
	// A different app is fine.
	if err := st.AppCanaries.Create(ctx, newProgressingCanary("c3", "a2")); err != nil {
		t.Fatalf("different app should not conflict: %v", err)
	}
}

func TestAppCanaryStore_UpdateFreesActiveSlot(t *testing.T) {
	st := New()
	ctx := context.Background()
	c := newProgressingCanary("c1", "a1")
	if err := st.AppCanaries.Create(ctx, c); err != nil {
		t.Fatal(err)
	}
	// Resolve the first canary.
	c.Status = model.CanaryPromoted
	c.Weight = 100
	if err := st.AppCanaries.Update(ctx, c); err != nil {
		t.Fatalf("update: %v", err)
	}
	// GetActive now finds nothing.
	if _, err := st.AppCanaries.GetActive(ctx, "a1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected no active canary after promote, got %v", err)
	}
	// And a fresh canary can start.
	if err := st.AppCanaries.Create(ctx, newProgressingCanary("c2", "a1")); err != nil {
		t.Fatalf("new canary after resolve should succeed: %v", err)
	}
}

func TestAppCanaryStore_UpdateNotFound(t *testing.T) {
	st := New()
	err := st.AppCanaries.Update(context.Background(), &model.AppCanary{ID: "ghost", Status: model.CanaryAborted})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestAppCanaryStore_ListProgressing(t *testing.T) {
	st := New()
	ctx := context.Background()
	_ = st.AppCanaries.Create(ctx, newProgressingCanary("c1", "a1"))
	_ = st.AppCanaries.Create(ctx, newProgressingCanary("c2", "a2"))
	resolved := newProgressingCanary("c3", "a3")
	resolved.Status = model.CanaryAborted
	_ = st.AppCanaries.Create(ctx, resolved)

	got, err := st.AppCanaries.ListProgressing(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 progressing canaries, got %d", len(got))
	}
	for _, c := range got {
		if c.Status != model.CanaryProgressing {
			t.Errorf("ListProgressing returned a %s canary", c.Status)
		}
	}
}

func TestAppStore_CanaryConfigRoundTrip(t *testing.T) {
	st := New()
	ctx := context.Background()
	a := &model.App{
		ID: "a1", Name: "shop", GitHubRepo: "o/shop", Branch: "main",
		Canary: model.CanaryConfig{Strategy: model.DeployStrategyCanary, Weight: 25, AutoPromote: true, HealthWindowSeconds: 120},
	}
	if err := st.Apps.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	got, err := st.Apps.Get(ctx, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Canary.IsCanary() || got.Canary.Weight != 25 || !got.Canary.AutoPromote || got.Canary.HealthWindowSeconds != 120 {
		t.Errorf("canary config not round-tripped: %+v", got.Canary)
	}

	// An app with no canary config reads back as an explicit rolling default.
	plain := &model.App{ID: "a2", Name: "blog", GitHubRepo: "o/blog", Branch: "main"}
	if err := st.Apps.Create(ctx, plain); err != nil {
		t.Fatal(err)
	}
	gotPlain, err := st.Apps.Get(ctx, "a2")
	if err != nil {
		t.Fatal(err)
	}
	if gotPlain.Canary.Strategy != model.DeployStrategyRolling {
		t.Errorf("empty canary config should normalize to rolling, got %q", gotPlain.Canary.Strategy)
	}
}
