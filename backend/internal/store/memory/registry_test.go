package memory

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

func TestRegistryVersionsGrantsAndCopies(t *testing.T) {
	s := New().Registries
	ctx := context.Background()
	r := &model.RegistryConfig{ID: "r", Name: "fixture", AllowedAppIDs: []string{"app"}}
	if err := s.Create(ctx, r); err != nil {
		t.Fatal(err)
	}
	first := r.Version
	r.AllowedAppIDs[0] = "mutated"
	got, _ := s.Get(ctx, r.ID)
	if got.AllowedAppIDs[0] != "app" {
		t.Fatal("input aliases store")
	}
	got.AllowedAppIDs[0] = "mutated"
	listed, _ := s.List(ctx)
	listed[0].AllowedAppIDs[0] = "mutated"
	redacted, _ := s.Get(ctx, r.ID)
	redacted.Redact().AllowedAppIDs[0] = "mutated"
	got, _ = s.Get(ctx, r.ID)
	if got.AllowedAppIDs[0] != "app" {
		t.Fatal("read aliases store")
	}
	if err := s.Create(ctx, r); !errors.Is(err, store.ErrConflict) {
		t.Fatal("duplicate ID overwrote grants")
	}
	updater := s.(store.RegistryGrantStore)
	newer, err := updater.UpdateGrants(ctx, r.ID, first, nil, []string{"pipe"})
	if err != nil || newer.Version <= first {
		t.Fatal("grant update did not fence generation", err)
	}
	if _, err = updater.UpdateGrants(ctx, r.ID, first, []string{"old"}, nil); !errors.Is(err, store.ErrConflict) {
		t.Fatal("stale grant edit accepted")
	}
	if err = s.Delete(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.Create(ctx, r); err != nil {
		t.Fatal(err)
	}
	if r.Version <= newer.Version {
		t.Fatal("delete/recreate reused generation")
	}
}
func TestRegistryConcurrentGrantEditsCAS(t *testing.T) {
	s := New().Registries
	ctx := context.Background()
	r := &model.RegistryConfig{ID: "r", Name: "fixture"}
	if err := s.Create(ctx, r); err != nil {
		t.Fatal(err)
	}
	version := r.Version
	var success atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.(store.RegistryGrantStore).UpdateGrants(ctx, r.ID, version, []string{"app"}, nil)
			if err == nil {
				success.Add(1)
			} else if !errors.Is(err, store.ErrConflict) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if success.Load() != 1 {
		t.Fatal("stale concurrent edits were not fenced")
	}
}
