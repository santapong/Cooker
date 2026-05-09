package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cooker-ci/cooker/internal/model"
	"github.com/cooker-ci/cooker/internal/store"
)

// TestUpdateHealth_WritesAndReadsBack pins the AppStore.UpdateHealth
// contract: it sets the three health fields on the App without
// bumping Version (probe writes must not race with concurrent
// user-driven Update calls via optimistic-concurrency).
func TestUpdateHealth_WritesAndReadsBack(t *testing.T) {
	st := New()
	ctx := context.Background()
	at := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)

	a := &model.App{
		ID:         "app-1",
		Name:       "demo",
		GitHubRepo: "acme/demo",
		Branch:     "main",
		Version:    7,
	}
	if err := st.Apps.Create(ctx, a); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := st.Apps.UpdateHealth(ctx, "app-1", model.AppHealthDegraded, "1/2 replicas ready", at); err != nil {
		t.Fatalf("UpdateHealth: %v", err)
	}

	got, err := st.Apps.Get(ctx, "app-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.HealthStatus != model.AppHealthDegraded {
		t.Errorf("HealthStatus: got %q want %q", got.HealthStatus, model.AppHealthDegraded)
	}
	if got.HealthMessage != "1/2 replicas ready" {
		t.Errorf("HealthMessage: got %q", got.HealthMessage)
	}
	if got.HealthCheckedAt == nil || !got.HealthCheckedAt.Equal(at) {
		t.Errorf("HealthCheckedAt: got %v want %v", got.HealthCheckedAt, at)
	}
	// Version must NOT have moved — health writes are observational.
	if got.Version != 7 {
		t.Errorf("Version moved: got %d want 7", got.Version)
	}
}

func TestUpdateHealth_UnknownAppReturnsNotFound(t *testing.T) {
	st := New()
	err := st.Apps.UpdateHealth(context.Background(), "missing", model.AppHealthHealthy, "", time.Now())
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdateHealth_DefaultsToUnknownOnNewApp(t *testing.T) {
	st := New()
	ctx := context.Background()
	a := &model.App{ID: "app-2", Name: "demo2", GitHubRepo: "acme/demo2", Branch: "main"}
	if err := st.Apps.Create(ctx, a); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := st.Apps.Get(ctx, "app-2")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	// Memory store doesn't apply a default — but the model's zero value
	// for AppHealth is the empty string; the postgres schema's COALESCE
	// gives it 'unknown'. Pin the memory behaviour here so any future
	// drift between impls is caught.
	if got.HealthStatus != "" {
		t.Errorf("expected zero-value HealthStatus on new memory App, got %q", got.HealthStatus)
	}
	if got.HealthCheckedAt != nil {
		t.Errorf("expected nil HealthCheckedAt on new App, got %v", got.HealthCheckedAt)
	}
}
