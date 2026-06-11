package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

func TestRegistryConfigStore_CreateListDelete(t *testing.T) {
	st := New()
	ctx := context.Background()
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	// Insert out of name order; List must come back name-ascending.
	for _, r := range []*model.RegistryConfig{
		{ID: "r2", Name: "zulu", URL: "zulu.io", PasswordRef: "registry:r2", CreatedAt: now, UpdatedAt: now},
		{ID: "r1", Name: "alpha", URL: "alpha.io", CreatedAt: now, UpdatedAt: now},
	} {
		if err := st.Registries.Create(ctx, r); err != nil {
			t.Fatalf("create %s: %v", r.ID, err)
		}
	}

	got, err := st.Registries.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 registries, got %d", len(got))
	}
	if got[0].Name != "alpha" || got[1].Name != "zulu" {
		t.Errorf("not name-ascending: %s, %s", got[0].Name, got[1].Name)
	}

	// Get round-trips the password ref (the secret reference, not the secret).
	one, err := st.Registries.Get(ctx, "r2")
	if err != nil {
		t.Fatal(err)
	}
	if one.PasswordRef != "registry:r2" {
		t.Errorf("password ref not persisted: %q", one.PasswordRef)
	}

	if err := st.Registries.Delete(ctx, "r1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, _ = st.Registries.List(ctx)
	if len(got) != 1 || got[0].ID != "r2" {
		t.Fatalf("delete did not remove r1: %+v", got)
	}
}

func TestRegistryConfigStore_GetAndDeleteNotFound(t *testing.T) {
	st := New()
	ctx := context.Background()
	if _, err := st.Registries.Get(ctx, "ghost"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get ghost: expected ErrNotFound, got %v", err)
	}
	if err := st.Registries.Delete(ctx, "ghost"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Delete ghost: expected ErrNotFound, got %v", err)
	}
}

func TestClusterConfigStore_CreateListDelete(t *testing.T) {
	st := New()
	ctx := context.Background()
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	for _, c := range []*model.ClusterConfig{
		{ID: "c2", Name: "prod", Context: "prod-ctx", KubeconfigRef: "cluster:c2", CreatedAt: now, UpdatedAt: now},
		{ID: "c1", Name: "dev", CreatedAt: now, UpdatedAt: now},
	} {
		if err := st.Clusters.Create(ctx, c); err != nil {
			t.Fatalf("create %s: %v", c.ID, err)
		}
	}

	got, err := st.Clusters.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 clusters, got %d", len(got))
	}
	if got[0].Name != "dev" || got[1].Name != "prod" {
		t.Errorf("not name-ascending: %s, %s", got[0].Name, got[1].Name)
	}

	one, err := st.Clusters.Get(ctx, "c2")
	if err != nil {
		t.Fatal(err)
	}
	if one.KubeconfigRef != "cluster:c2" {
		t.Errorf("kubeconfig ref not persisted: %q", one.KubeconfigRef)
	}

	if err := st.Clusters.Delete(ctx, "c2"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, _ = st.Clusters.List(ctx)
	if len(got) != 1 || got[0].ID != "c1" {
		t.Fatalf("delete did not remove c2: %+v", got)
	}
}

func TestClusterConfigStore_GetAndDeleteNotFound(t *testing.T) {
	st := New()
	ctx := context.Background()
	if _, err := st.Clusters.Get(ctx, "ghost"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get ghost: expected ErrNotFound, got %v", err)
	}
	if err := st.Clusters.Delete(ctx, "ghost"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Delete ghost: expected ErrNotFound, got %v", err)
	}
}
