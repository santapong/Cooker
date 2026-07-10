// Package memory provides an in-memory implementation of the store
// interfaces. Intended for unit tests and local development when a
// PostgreSQL instance is not available.
package memory

import (
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// New returns an in-memory aggregate store. Safe for concurrent use.
func New() *store.Store {
	return store.New(store.Components{
		Pipelines:      &pipelines{m: map[string]*model.Pipeline{}},
		Runs:           &runs{m: map[string]*model.PipelineRun{}},
		Environments:   &environments{m: map[string]*model.Environment{}},
		Promotions:     &promotions{m: map[string]*model.RunPromotion{}, approvals: map[string][]model.PromotionApproval{}},
		StageApprovals: &stageApprovals{m: map[string]*model.StageApproval{}, votes: map[string][]model.StageApprovalVote{}},
		Apps:           &apps{m: map[string]*model.App{}},
		AppDeploys:     &appDeploys{m: map[string]*model.AppDeploy{}},
		AppCanaries:    &appCanaries{m: map[string]*model.AppCanary{}},
		AuditEvents:    &auditEvents{},
		Hosts:          &hosts{m: map[string]*model.Host{}},
		Registries:     &registryConfigs{m: map[string]*model.RegistryConfig{}},
		Clusters:       &clusterConfigs{m: map[string]*model.ClusterConfig{}},
		Users:          &users{byID: map[string]*model.User{}, byEmail: map[string]string{}},
		APITokens:      &apiTokens{m: map[string]*model.APIToken{}},
		Licenses:       &licenses{},
	})
}
