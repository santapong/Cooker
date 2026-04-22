//go:build !tsnet

package tsnet

import "context"

// New returns ErrDisabled in stub builds. Invoked via the New
// symbol so callers don't need to probe build tags themselves.
func New(_ context.Context, _ Config) (Dialer, error) {
	return nil, ErrDisabled
}
