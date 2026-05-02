// Package secrets defines the abstraction Cooker uses to read and
// write environment-scoped secrets. Adapters live in subpackages:
// database/ (the default, AES-GCM + JSONB) and keepsave/ (delegates
// to a KeepSave server over HTTP). Selection is driven by
// COOKER_SECRETS_BACKEND.
package secrets

import (
	"context"
	"errors"
)

// ErrNotFound is returned by Get when the requested secret does not
// exist on the given environment. Adapters MUST translate backend-
// specific 404-equivalents to this sentinel; callers use errors.Is.
var ErrNotFound = errors.New("secrets: not found")

// Manager is the interface every secrets backend implements. Methods
// are scoped by the Cooker environment ID; adapters that address by
// name internally (e.g. KeepSave) resolve ID -> name themselves.
// Authorization stays in the handler layer, not the adapter.
type Manager interface {
	Get(ctx context.Context, envID, key string) ([]byte, error)
	Put(ctx context.Context, envID, key string, value []byte) error
	Delete(ctx context.Context, envID, key string) error
	List(ctx context.Context, envID string) ([]string, error)
}

// ErrPromotionUnsupported is returned by adapters that do not
// implement Promoter. The handler maps this to HTTP 501.
var ErrPromotionUnsupported = errors.New("secrets: promotion not supported by this backend")

// Promoter is an optional interface adapters may implement to
// support copying secrets from one environment to another in a
// single backend round-trip. Today only the KeepSave adapter
// implements it; the database adapter falls back to ErrPromotionUnsupported.
//
// keys may be empty to mean "all keys in fromEnvID".
type Promoter interface {
	Promote(ctx context.Context, fromEnvID, toEnvID string, keys []string) error
}
