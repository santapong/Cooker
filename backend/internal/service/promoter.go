package service

import (
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/cooker-ci/cooker/internal/model"
)

// Promoter handles promotion logic between environments (Dev → Staging → Production).
type Promoter struct {
	environments []*model.Environment
}

// NewPromoter creates a promoter with the given environments.
func NewPromoter(envs []*model.Environment) *Promoter {
	sorted := make([]*model.Environment, len(envs))
	copy(sorted, envs)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Order < sorted[j].Order
	})
	return &Promoter{environments: sorted}
}

// NextEnvironment returns the next environment in the promotion chain, or nil if at the end.
func (p *Promoter) NextEnvironment(currentEnvID string) *model.Environment {
	for i, env := range p.environments {
		if env.ID == currentEnvID && i+1 < len(p.environments) {
			return p.environments[i+1]
		}
	}
	return nil
}

// ShouldAutoPromote checks if the next environment allows automatic promotion.
func (p *Promoter) ShouldAutoPromote(nextEnv *model.Environment) bool {
	return nextEnv.Promotion.Strategy == "auto"
}

// Promote attempts to promote a pipeline run to the next environment.
func (p *Promoter) Promote(run *model.PipelineRun, currentEnvID string, approvedBy string) (*model.EnvironmentStatus, error) {
	nextEnv := p.NextEnvironment(currentEnvID)
	if nextEnv == nil {
		return nil, fmt.Errorf("no next environment to promote to")
	}

	if nextEnv.Promotion.Strategy == "manual" && approvedBy == "" {
		// Set to awaiting approval
		status := &model.EnvironmentStatus{
			EnvironmentID: nextEnv.ID,
			Status:        model.EnvStatusAwaitingApproval,
		}
		run.EnvironmentStatuses = append(run.EnvironmentStatuses, *status)
		log.Printf("[promoter] run %s awaiting approval for env %s", run.ID, nextEnv.Name)
		return status, nil
	}

	// Promote
	now := time.Now()
	status := &model.EnvironmentStatus{
		EnvironmentID: nextEnv.ID,
		Status:        model.EnvStatusDeploying,
		PromotedAt:    &now,
		ApprovedBy:    approvedBy,
	}
	run.EnvironmentStatuses = append(run.EnvironmentStatuses, *status)

	log.Printf("[promoter] run %s promoted to env %s (approved by: %s)", run.ID, nextEnv.Name, approvedBy)
	return status, nil
}

// ApprovePromotion approves a pending promotion and triggers deployment.
func (p *Promoter) ApprovePromotion(run *model.PipelineRun, envID string, approvedBy string) error {
	for i := range run.EnvironmentStatuses {
		es := &run.EnvironmentStatuses[i]
		if es.EnvironmentID == envID && es.Status == model.EnvStatusAwaitingApproval {
			now := time.Now()
			es.Status = model.EnvStatusDeploying
			es.PromotedAt = &now
			es.ApprovedBy = approvedBy
			log.Printf("[promoter] promotion approved for env %s by %s", envID, approvedBy)
			return nil
		}
	}
	return fmt.Errorf("no pending promotion found for environment %s", envID)
}
