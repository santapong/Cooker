// Package tsnet exposes a Dialer that routes connections through a
// Tailscale tailnet Cooker joins agentlessly via tailscale.com/tsnet.
//
// Building without the "tsnet" build tag yields a stub that reports
// unavailability so Cooker can compile and test in environments
// that don't have the Tailscale module vendored. Production builds
// enable -tags tsnet to link in the real implementation.
//
// Contract (both the stub and the real impl satisfy it):
//
//	d, err := tsnet.New(ctx, cfg)
//	if err != nil { ... }
//	defer d.Close()
//	client := &http.Client{Transport: d.Transport()}
//	// ...or use d.DialContext as a net.Dialer.
package tsnet

import (
	"context"
	"errors"
	"net"
	"net/http"
)

// ErrDisabled is returned when the tsnet transport was requested
// but the build does not include the real implementation (missing
// -tags tsnet) or COOKER_TAILSCALE_AUTHKEY is unset.
var ErrDisabled = errors.New("tsnet: disabled in this build")

// Config drives tsnet server construction. Hostname is how Cooker
// appears on the tailnet; StateDir is where tsnet persists the
// node key across restarts; AuthKey is the ephemeral reusable
// auth key from Tailscale admin.
type Config struct {
	Hostname string
	StateDir string
	AuthKey  string
	// Ephemeral, when true, makes the tsnet node auto-expire when
	// Cooker shuts down. Recommended for short-lived CI runners.
	Ephemeral bool
}

// Dialer exposes the two forms callers need: a net-dialer style
// DialContext, and an http.RoundTripper for API clients that want
// a preconfigured client.
type Dialer interface {
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
	Transport() http.RoundTripper
	Close() error
}
