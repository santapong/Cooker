package database

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/secrets"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
)

type conditionalFaultStore struct {
	store.EnvironmentStore
	fault   error
	ready   chan struct{}
	release chan struct{}
}

func (s *conditionalFaultStore) Get(ctx context.Context, id string) (*model.Environment, error) {
	e, err := s.EnvironmentStore.Get(ctx, id)
	if s.ready != nil {
		s.ready <- struct{}{}
		<-s.release
	}
	return e, err
}
func (s *conditionalFaultStore) Update(ctx context.Context, e *model.Environment) error {
	if s.fault != nil {
		return s.fault
	}
	return s.EnvironmentStore.Update(ctx, e)
}

func conditionalFixture(t *testing.T) (*Manager, store.EnvironmentStore) {
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
	if err := st.Environments.Create(context.Background(), &model.Environment{ID: "conditional-env", Name: "Production", Version: 7, PlainVars: map[string]string{"DATABASE_NAME": "app"}}); err != nil {
		t.Fatal(err)
	}
	return New(st.Environments, codec), st.Environments
}

func TestConditionalSecretsGenerationAndConfigurationFence(t *testing.T) {
	m, st := conditionalFixture(t)
	ctx := context.Background()
	next, err := m.PutAtGeneration(ctx, "conditional-env", "PASSWORD", []byte("owned-conditional-secret-canary"), 7)
	if err != nil || next != 8 {
		t.Fatalf("initial generation=%d err=%v", next, err)
	}
	for _, op := range []string{"put", "delete"} {
		var err error
		if op == "put" {
			_, err = m.PutAtGeneration(ctx, "conditional-env", "PASSWORD", []byte("stale-replacement"), 7)
		} else {
			_, err = m.DeleteAtGeneration(ctx, "conditional-env", "PASSWORD", 7)
		}
		if !errors.Is(err, secrets.ErrGenerationConflict) {
			t.Fatalf("stale %s did not conflict", op)
		}
	}
	got, err := m.Get(ctx, "conditional-env", "PASSWORD")
	if err != nil || string(got) != "owned-conditional-secret-canary" {
		t.Fatal("stale edit changed secret")
	}
	env, err := st.Get(ctx, "conditional-env")
	if err != nil {
		t.Fatal(err)
	}
	cp := *env
	cp.Name = "Renamed"
	if err := st.Update(ctx, &cp); err != nil {
		t.Fatal(err)
	}
	if _, err := m.PutAtGeneration(ctx, env.ID, "PASSWORD", []byte("configuration-stale"), 8); !errors.Is(err, secrets.ErrGenerationConflict) {
		t.Fatal("configuration change did not fence secret")
	}
	next, err = m.DeleteAtGeneration(ctx, env.ID, "PASSWORD", 9)
	if err != nil || next != 10 {
		t.Fatal("fresh deletion did not advance generation")
	}
	if _, err := m.Get(ctx, env.ID, "PASSWORD"); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatal("deleted key still available")
	}
}

func TestConditionalSecretsFailingCASDoesNotMutateSharedSnapshot(t *testing.T) {
	m, base := conditionalFixture(t)
	ctx := context.Background()
	if _, err := m.PutAtGeneration(ctx, "conditional-env", "PASSWORD", []byte("original-canary"), 7); err != nil {
		t.Fatal(err)
	}
	fault := &conditionalFaultStore{EnvironmentStore: base, fault: store.ErrConflict}
	m.envs = fault
	if _, err := m.PutAtGeneration(ctx, "conditional-env", "PASSWORD", []byte("rejected-canary"), 8); !errors.Is(err, secrets.ErrGenerationConflict) {
		t.Fatal("write conflict missing")
	}
	if _, err := m.DeleteAtGeneration(ctx, "conditional-env", "PASSWORD", 8); !errors.Is(err, secrets.ErrGenerationConflict) {
		t.Fatal("delete conflict missing")
	}
	e, err := base.Get(ctx, "conditional-env")
	if err != nil || e.Version != 8 {
		t.Fatal("failed edit changed generation")
	}
	got, err := m.Get(ctx, e.ID, "PASSWORD")
	if err != nil || string(got) != "original-canary" {
		t.Fatal("failed CAS corrupted shared store")
	}
	fault.fault = errors.New("owned-backend-secret-canary")
	if err := m.Put(ctx, e.ID, "PASSWORD", []byte("legacy-rejected")); err == nil {
		t.Fatal("legacy failure missing")
	}
	got, err = m.Get(ctx, e.ID, "PASSWORD")
	if err != nil || string(got) != "original-canary" {
		t.Fatal("failed legacy Put mutated shared store")
	}
	if err := m.Delete(ctx, e.ID, "PASSWORD"); err == nil {
		t.Fatal("legacy delete failure missing")
	}
	got, err = m.Get(ctx, e.ID, "PASSWORD")
	if err != nil || string(got) != "original-canary" {
		t.Fatal("failed legacy Delete mutated shared store")
	}
}

func TestConditionalSecretsConcurrentSameGenerationOnlyOneWins(t *testing.T) {
	m, base := conditionalFixture(t)
	barrier := &conditionalFaultStore{EnvironmentStore: base, ready: make(chan struct{}, 2), release: make(chan struct{})}
	m.envs = barrier
	errorsOut := make(chan error, 2)
	var wg sync.WaitGroup
	for _, key := range []string{"ONE", "TWO"} {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			_, err := m.PutAtGeneration(context.Background(), "conditional-env", key, []byte("concurrent-canary"), 7)
			errorsOut <- err
		}(key)
	}
	<-barrier.ready
	<-barrier.ready
	close(barrier.release)
	wg.Wait()
	close(errorsOut)
	successes, conflicts := 0, 0
	for err := range errorsOut {
		if err == nil {
			successes++
		} else if errors.Is(err, secrets.ErrGenerationConflict) {
			conflicts++
		} else {
			t.Fatal("unexpected write failure")
		}
	}
	e, err := base.Get(context.Background(), "conditional-env")
	if err != nil || successes != 1 || conflicts != 1 || e.Version != 8 || len(e.Secrets) != 1 {
		t.Fatal("concurrent CAS did not preserve one exact accepted edit")
	}
}

func TestConditionalSecretsCollisionCancellationAndMissingEnvironment(t *testing.T) {
	m, st := conditionalFixture(t)
	if _, err := m.PutAtGeneration(context.Background(), "conditional-env", "DATABASE_NAME", []byte("must-stay-private"), 7); !errors.Is(err, secrets.ErrKeyCollision) {
		t.Fatal("plain/secret collision accepted")
	}
	if err := m.Put(context.Background(), "conditional-env", "DATABASE_NAME", []byte("legacy-must-stay-private")); !errors.Is(err, secrets.ErrKeyCollision) {
		t.Fatal("legacy collision accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.PutAtGeneration(ctx, "conditional-env", "PASSWORD", []byte("cancelled-secret"), 7); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled edit accepted")
	}
	if _, err := m.DeleteAtGeneration(ctx, "conditional-env", "PASSWORD", 7); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled delete accepted")
	}
	e, err := st.Get(context.Background(), "conditional-env")
	if err != nil || e.Version != 7 || len(e.Secrets) != 0 {
		t.Fatal("failed mutation changed inventory")
	}
	if _, err := m.PutAtGeneration(context.Background(), "missing", "PASSWORD", []byte("missing"), 1); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatal("missing environment not classified")
	}
	if _, err := m.DeleteAtGeneration(context.Background(), "missing", "PASSWORD", 1); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatal("missing delete environment not classified")
	}
}
