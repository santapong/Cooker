package database

import (
	"context"
	"errors"
	"testing"

	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/secrets"
	"github.com/santapong/cooker/internal/store/memory"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	codec, err := crypto.NewCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	st := memory.New()
	if err := st.Environments.Create(context.Background(), &model.Environment{ID: "e1", Name: "prod"}); err != nil {
		t.Fatal(err)
	}
	return New(st.Environments, codec)
}

func TestRoundTrip(t *testing.T) {
	m := newTestManager(t)
	ctx := context.Background()

	if err := m.Put(ctx, "e1", "DB_PASSWORD", []byte("hunter2")); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := m.Get(ctx, "e1", "DB_PASSWORD")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != "hunter2" {
		t.Errorf("round-trip: got %q want hunter2", got)
	}

	keys, err := m.List(ctx, "e1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 1 || keys[0] != "DB_PASSWORD" {
		t.Errorf("list: %v", keys)
	}

	if err := m.Delete(ctx, "e1", "DB_PASSWORD"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := m.Get(ctx, "e1", "DB_PASSWORD"); !errors.Is(err, secrets.ErrNotFound) {
		t.Errorf("after delete: expected ErrNotFound, got %v", err)
	}
}

func TestUnknownEnv_ErrNotFound(t *testing.T) {
	m := newTestManager(t)
	if _, err := m.Get(context.Background(), "missing", "K"); !errors.Is(err, secrets.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestUnknownKey_ErrNotFound(t *testing.T) {
	m := newTestManager(t)
	if _, err := m.Get(context.Background(), "e1", "missing"); !errors.Is(err, secrets.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
