package store

import (
	"context"
	"github.com/santapong/cooker/internal/model"
)

// RegistryGrantStore is optional on legacy stores. Only admin Settings handlers
// may expose this mutation; an App/Pipeline selection never changes grants.
type RegistryGrantStore interface {
	UpdateGrants(ctx context.Context, id string, expectedVersion int64, appIDs, pipelineIDs []string) (*model.RegistryConfig, error)
}
