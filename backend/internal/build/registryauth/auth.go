// Package registryauth shares registry credentials without writing them into
// pipeline JSON, Dockerfiles, build arguments, or the process environment.
package registryauth

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
)

// Resolver resolves only the registry host requested by an adapter. A missing
// connection is anonymous; a configured but unreadable secret is an error.
type Resolver func(context.Context, string) (authn.Authenticator, error)

type keychain struct {
	ctx      context.Context
	resolver Resolver
}

func (k keychain) Resolve(r authn.Resource) (authn.Authenticator, error) {
	return Resolve(k.ctx, k.resolver, r.RegistryStr())
}
func Keychain(ctx context.Context, resolver Resolver) authn.Keychain { return keychain{ctx, resolver} }

var ErrUnavailable = errors.New("registry: scoped credential unavailable")

// Endpoint accepts only bounded nonsecret endpoints. Paths identify configured
// endpoints, not repository ACLs. Docker Hub aliases share one auth host.
func Endpoint(raw string) (endpoint, host string, err error) {
	if raw == "" || len(raw) > 512 {
		return "", "", ErrUnavailable
	}
	for _, r := range raw {
		if r < 33 || r > 126 || strings.ContainsRune("\"\\<>`", r) {
			return "", "", ErrUnavailable
		}
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, e := url.Parse(raw)
	if e != nil {
		return "", "", ErrUnavailable
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", "", ErrUnavailable
	}
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	host = u.Host
	switch host {
	case "docker.io", "registry-1.docker.io", "index.docker.io":
		host = "index.docker.io"
	}
	return u.String(), host, nil
}

// Resolve makes nil mean anonymous, never the process Docker configuration.
func Resolve(ctx context.Context, resolver Resolver, host string) (authn.Authenticator, error) {
	if resolver == nil {
		return authn.Anonymous, nil
	}
	a, err := resolver(ctx, host)
	if err != nil || a == nil {
		return nil, ErrUnavailable
	}
	return a, nil
}
