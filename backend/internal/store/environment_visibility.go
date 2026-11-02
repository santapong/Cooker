package store

import (
	"context"

	"github.com/santapong/cooker/internal/model"
)

// PublicEnvironmentLister filters credential-only scopes before pagination.
// Internal registry and secret-manager paths continue using EnvironmentStore.
type PublicEnvironmentLister interface {
	ListPublic(context.Context, int, int) ([]*model.Environment, error)
}
