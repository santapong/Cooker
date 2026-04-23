//go:build tsnet

// This file compiles when the binary is built with `-tags tsnet`.
// It wraps tailscale.com/tsnet.Server into our Dialer interface.
//
// The heavy dependency (tailscale.com) is intentionally not added
// to go.mod in the default build to keep offline/CI builds fast.
// When operators need remote Docker-host execution they rebuild
// with `go build -tags tsnet ./...`, which pulls in the module via
// `go mod tidy`. See deploy/helm/cooker/values.yaml for the
// corresponding runtime env var (COOKER_TAILSCALE_AUTHKEY).

package tsnet

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"

	"tailscale.com/tsnet"
)

type realDialer struct {
	srv *tsnet.Server
}

// New joins the tailnet using cfg and returns a Dialer that routes
// every connection through tsnet.
func New(ctx context.Context, cfg Config) (Dialer, error) {
	if cfg.AuthKey == "" {
		return nil, errors.New("tsnet: AuthKey is required")
	}
	if cfg.Hostname == "" {
		cfg.Hostname = "cooker"
	}
	if cfg.StateDir == "" {
		tmp, err := os.MkdirTemp("", "cooker-tsnet-*")
		if err != nil {
			return nil, err
		}
		cfg.StateDir = tmp
	}
	srv := &tsnet.Server{
		Hostname:  cfg.Hostname,
		Dir:       cfg.StateDir,
		AuthKey:   cfg.AuthKey,
		Ephemeral: cfg.Ephemeral,
	}
	if _, err := srv.Up(ctx); err != nil {
		return nil, err
	}
	return &realDialer{srv: srv}, nil
}

func (d *realDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return d.srv.Dial(ctx, network, addr)
}

func (d *realDialer) Transport() http.RoundTripper {
	return &http.Transport{DialContext: d.DialContext}
}

func (d *realDialer) Close() error { return d.srv.Close() }
