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
	return store.New(
		&pipelines{m: map[string]*model.Pipeline{}},
		&runs{m: map[string]*model.PipelineRun{}},
		&environments{m: map[string]*model.Environment{}},
		&promotions{m: map[string]*model.RunPromotion{}, approvals: map[string][]model.PromotionApproval{}},
		&stageApprovals{m: map[string]*model.StageApproval{}, votes: map[string][]model.StageApprovalVote{}},
		&apps{m: map[string]*model.App{}},
		&appDeploys{m: map[string]*model.AppDeploy{}},
		&appCanaries{m: map[string]*model.AppCanary{}},
		&auditEvents{},
		&hosts{m: map[string]*model.Host{}},
		&registryConfigs{m: map[string]*model.RegistryConfig{}},
		&clusterConfigs{m: map[string]*model.ClusterConfig{}},
		&users{byID: map[string]*model.User{}, byEmail: map[string]string{}},
		&apiTokens{m: map[string]*model.APIToken{}},
		&licenses{},
		nil,
		nil,
	)
}
