package service

import (
	"context"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/santapong/cooker/internal/build/registryauth"
	"github.com/santapong/cooker/internal/secrets"
	"github.com/santapong/cooker/internal/store"
)

// RegistryAuthResolver is retained for source compatibility only. A hostname
// cannot establish App/Pipeline authorization; callers must provide accepted
// evidence to RegistryAuthorityService. No global discovery or credential reads.
func RegistryAuthResolver(registries store.RegistryConfigStore, secs secrets.Manager) registryauth.Resolver {
	return func(ctx context.Context, host string) (authn.Authenticator, error) {
		return nil, ErrRegistryAuthorityRequired
	}
}
func registryHost(raw string) string {
	_, host, _ := registryauth.Endpoint(raw)
	return host
}
