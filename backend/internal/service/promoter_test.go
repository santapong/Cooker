package service

import (
	"testing"

	"github.com/cooker-ci/cooker/internal/model"
)

func makeEnvironments() []*model.Environment {
	return []*model.Environment{
		{ID: "dev", Name: "Development", Order: 1, Promotion: model.PromotionPolicy{Strategy: "auto"}},
		{ID: "staging", Name: "Staging", Order: 2, Promotion: model.PromotionPolicy{Strategy: "manual", RequiredApprovers: 1}},
		{ID: "prod", Name: "Production", Order: 3, Promotion: model.PromotionPolicy{Strategy: "manual", RequiredApprovers: 2}},
	}
}

func TestNewPromoter_SortsEnvironments(t *testing.T) {
	// Pass environments in reverse order
	envs := []*model.Environment{
		{ID: "prod", Name: "Production", Order: 3},
		{ID: "dev", Name: "Development", Order: 1},
		{ID: "staging", Name: "Staging", Order: 2},
	}

	p := NewPromoter(envs)
	next := p.NextEnvironment("dev")
	if next == nil {
		t.Fatal("expected next environment after dev")
	}
	if next.ID != "staging" {
		t.Errorf("expected staging, got %s", next.ID)
	}
}

func TestNextEnvironment(t *testing.T) {
	p := NewPromoter(makeEnvironments())

	next := p.NextEnvironment("dev")
	if next == nil || next.ID != "staging" {
		t.Errorf("expected staging after dev, got %v", next)
	}

	next = p.NextEnvironment("staging")
	if next == nil || next.ID != "prod" {
		t.Errorf("expected prod after staging, got %v", next)
	}

	next = p.NextEnvironment("prod")
	if next != nil {
		t.Errorf("expected nil after prod, got %v", next)
	}

	next = p.NextEnvironment("nonexistent")
	if next != nil {
		t.Errorf("expected nil for nonexistent env, got %v", next)
	}
}

func TestShouldAutoPromote(t *testing.T) {
	p := NewPromoter(makeEnvironments())

	// Dev -> Staging (staging is manual)
	staging := p.NextEnvironment("dev")
	if p.ShouldAutoPromote(staging) {
		t.Error("staging should not auto-promote")
	}

	// Create auto env for testing
	autoEnv := &model.Environment{
		ID:        "auto-env",
		Promotion: model.PromotionPolicy{Strategy: "auto"},
	}
	if !p.ShouldAutoPromote(autoEnv) {
		t.Error("auto-env should auto-promote")
	}
}

func TestPromote_AutoStrategy(t *testing.T) {
	envs := []*model.Environment{
		{ID: "dev", Name: "Development", Order: 1, Promotion: model.PromotionPolicy{Strategy: "auto"}},
		{ID: "staging", Name: "Staging", Order: 2, Promotion: model.PromotionPolicy{Strategy: "auto"}},
	}
	p := NewPromoter(envs)

	run := &model.PipelineRun{ID: "run-1"}
	status, err := p.Promote(run, "dev", "user@test.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status.EnvironmentID != "staging" {
		t.Errorf("expected staging, got %s", status.EnvironmentID)
	}
	if status.Status != model.EnvStatusDeploying {
		t.Errorf("expected deploying, got %s", status.Status)
	}
	if status.PromotedAt == nil {
		t.Error("expected PromotedAt to be set")
	}
}

func TestPromote_ManualStrategy_NoApprover(t *testing.T) {
	p := NewPromoter(makeEnvironments())

	run := &model.PipelineRun{ID: "run-1"}
	status, err := p.Promote(run, "dev", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status.Status != model.EnvStatusAwaitingApproval {
		t.Errorf("expected awaiting_approval, got %s", status.Status)
	}
	if len(run.EnvironmentStatuses) != 1 {
		t.Errorf("expected 1 env status, got %d", len(run.EnvironmentStatuses))
	}
}

func TestPromote_ManualStrategy_WithApprover(t *testing.T) {
	p := NewPromoter(makeEnvironments())

	run := &model.PipelineRun{ID: "run-1"}
	status, err := p.Promote(run, "dev", "admin@test.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status.Status != model.EnvStatusDeploying {
		t.Errorf("expected deploying, got %s", status.Status)
	}
	if status.ApprovedBy != "admin@test.com" {
		t.Errorf("expected approvedBy admin@test.com, got %s", status.ApprovedBy)
	}
}

func TestPromote_NoNextEnvironment(t *testing.T) {
	p := NewPromoter(makeEnvironments())

	run := &model.PipelineRun{ID: "run-1"}
	_, err := p.Promote(run, "prod", "admin@test.com")
	if err == nil {
		t.Fatal("expected error when promoting from last environment")
	}
}

func TestApprovePromotion_Success(t *testing.T) {
	p := NewPromoter(makeEnvironments())

	run := &model.PipelineRun{
		ID: "run-1",
		EnvironmentStatuses: []model.EnvironmentStatus{
			{EnvironmentID: "staging", Status: model.EnvStatusAwaitingApproval},
		},
	}

	err := p.ApprovePromotion(run, "staging", "approver@test.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if run.EnvironmentStatuses[0].Status != model.EnvStatusDeploying {
		t.Errorf("expected deploying, got %s", run.EnvironmentStatuses[0].Status)
	}
	if run.EnvironmentStatuses[0].ApprovedBy != "approver@test.com" {
		t.Errorf("expected approvedBy approver@test.com, got %s", run.EnvironmentStatuses[0].ApprovedBy)
	}
	if run.EnvironmentStatuses[0].PromotedAt == nil {
		t.Error("expected PromotedAt to be set")
	}
}

func TestApprovePromotion_NotFound(t *testing.T) {
	p := NewPromoter(makeEnvironments())

	run := &model.PipelineRun{
		ID:                  "run-1",
		EnvironmentStatuses: []model.EnvironmentStatus{},
	}

	err := p.ApprovePromotion(run, "staging", "approver@test.com")
	if err == nil {
		t.Fatal("expected error when no pending promotion found")
	}
}

func TestApprovePromotion_WrongStatus(t *testing.T) {
	p := NewPromoter(makeEnvironments())

	run := &model.PipelineRun{
		ID: "run-1",
		EnvironmentStatuses: []model.EnvironmentStatus{
			{EnvironmentID: "staging", Status: model.EnvStatusDeploying},
		},
	}

	err := p.ApprovePromotion(run, "staging", "approver@test.com")
	if err == nil {
		t.Fatal("expected error when status is not awaiting_approval")
	}
}
