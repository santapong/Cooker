package builder

import (
	"context"
	"fmt"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/moby/buildkit/session/auth"
	"github.com/santapong/cooker/internal/build/registryauth"
	"google.golang.org/grpc"
)

// BuildKit's credential RPC is scoped to this build session. The daemon handles
// registry token exchange; credentials never become build args or layer contents.
type registrySession struct {
	auth.UnimplementedAuthServer
	resolve registryauth.Resolver
}

func (s *registrySession) Register(server *grpc.Server) { auth.RegisterAuthServer(server, s) }
func (s *registrySession) Credentials(ctx context.Context, req *auth.CredentialsRequest) (*auth.CredentialsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("registry credential unavailable")
	}
	a, err := registryauth.Resolve(ctx, s.resolve, req.Host)
	if err != nil {
		return nil, fmt.Errorf("registry credential unavailable")
	}
	if a == nil {
		a = authn.Anonymous
	}
	cfg, err := a.Authorization()
	if err != nil {
		return nil, fmt.Errorf("registry credential unavailable")
	}
	if cfg.IdentityToken != "" {
		return &auth.CredentialsResponse{Secret: cfg.IdentityToken}, nil
	}
	return &auth.CredentialsResponse{Username: cfg.Username, Secret: cfg.Password}, nil
}
