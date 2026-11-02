package service

import (
	"context"
	"errors"
	"testing"

	"github.com/santapong/cooker/internal/secrets"
	"github.com/santapong/cooker/internal/store/memory"
)

type registryFixtureSecrets struct{ key string }

func (s *registryFixtureSecrets) Get(_ context.Context, env, key string) ([]byte, error) {
	s.key = env + "/" + key
	return []byte("fixture-password"), nil
}
func (*registryFixtureSecrets) Put(context.Context, string, string, []byte) error { return nil }
func (*registryFixtureSecrets) Delete(context.Context, string, string) error      { return nil }
func (*registryFixtureSecrets) List(context.Context, string) ([]string, error)    { return nil, nil }

var _ secrets.Manager = (*registryFixtureSecrets)(nil)

func TestRegistryHostOnlyResolverCannotDiscoverCredentials(t *testing.T) {
	st := memory.New()
	ctx := WithLogSecrets(context.Background(), nil)
	secs := &registryFixtureSecrets{}
	resolve := RegistryAuthResolver(st.Registries, secs)
	for _, host := range []string{"ghcr.io", "unrelated.example"} {
		if _, err := resolve(ctx, host); !errors.Is(err, ErrRegistryAuthorityRequired) {
			t.Fatal("host-only resolver accepted missing authority")
		}
	}
	if secs.key != "" {
		t.Fatal("legacy resolver read credentials")
	}
}
