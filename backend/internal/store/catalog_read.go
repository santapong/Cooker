package store

import (
	"context"
	"time"

	"github.com/santapong/cooker/internal/model"
)

// CatalogPipelineSummary deliberately omits pipeline definitions and variables.
// LatestRun contains only identity, status and lifecycle timestamps: no stages,
// environment data, variables, actor credentials or raw execution errors.
type CatalogPipelineSummary struct {
	ID         string
	Name       string
	StageCount int
	UpdatedAt  time.Time
	LatestRun  *model.PipelineRun
}

// Optional readers preserve existing Store interfaces and fixture compatibility.
type CatalogPipelineReader interface {
	ListCatalog(context.Context) ([]CatalogPipelineSummary, error)
}
type CatalogAppDeployReader interface {
	LatestByApps(context.Context, []string) (map[string]*model.AppDeploy, error)
}
