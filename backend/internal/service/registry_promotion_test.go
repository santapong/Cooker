package service

import (
	"context"
	"errors"
	"testing"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
)

type registryPromotionEnvFixture struct {
	envs  map[string]*model.Environment
	reads int
	err   error
}

func (f *registryPromotionEnvFixture) Get(_ context.Context, id string) (*model.Environment, error) {
	f.reads++
	if f.err != nil {
		return nil, f.err
	}
	if env := f.envs[id]; env != nil {
		return env, nil
	}
	return nil, store.ErrNotFound
}

func TestRegistryEnvironmentPromotionServiceRejectsPrivateBeforeMutations(t *testing.T) {
	ctx := context.Background()
	st := memory.New()
	f := &registryPromotionEnvFixture{envs: map[string]*model.Environment{
		"alias":  {ID: "alias", Name: "\tREGISTRY_owned\u00a0"},
		"public": {ID: "public", Name: "Production", Promotion: model.PromotionPolicy{Strategy: "manual", RequiredApprovers: 1}},
	}}
	svc := NewPromotionService(st.Promotions, f)
	for _, id := range []string{"registry_owned", "_registries", "alias"} {
		if err := st.Promotions.CreatePromotion(ctx, &model.RunPromotion{ID: "old-" + id, RunID: "run", PipelineID: "pipeline", EnvironmentID: id, Strategy: "manual", RequiredApprovers: 1, Status: model.PromotionAwaitingApproval}); err != nil {
			t.Fatal(err)
		}
		before := f.reads
		if _, err := svc.Promote(ctx, "run", "pipeline", id, "operator"); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("private idempotent promotion allowed")
		}
		if _, err := svc.Promote(ctx, "new-run", "pipeline", id, "operator"); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("private new promotion allowed")
		}
		if _, err := svc.Approve(ctx, "run", id, "approver", "", ""); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("private approval allowed")
		}
		if model.IsRegistryCredentialEnvironmentID(id) && f.reads != before {
			t.Fatal("reserved identity reached environment store")
		}
		p, err := st.Promotions.GetPromotion(ctx, "run", id)
		if err != nil || p.ApprovalCount() != 0 || p.Status != model.PromotionAwaitingApproval {
			t.Fatal("private promotion changed")
		}
		if _, err := st.Promotions.GetPromotion(ctx, "new-run", id); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("private target persisted")
		}
	}
	if _, err := svc.Promote(ctx, "run", "pipeline", "public", "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Approve(ctx, "run", "public", "approver", "", ""); err != nil {
		t.Fatal(err)
	}
	statuses, err := svc.Statuses(ctx, "run")
	if err != nil || len(statuses) != 1 || statuses[0].EnvironmentID != "public" || statuses[0].Status != model.EnvStatus(model.PromotionApproved) {
		t.Fatal("public status or private filtering incorrect")
	}
	f.err = errors.New("owned-store-unavailable")
	if _, err := svc.Statuses(ctx, "run"); err == nil {
		t.Fatal("unavailable environment source appeared complete")
	}
}

func TestRegistryEnvironmentLegacyPromotionChainSkipsPrivateScopes(t *testing.T) {
	alias := &model.Environment{ID: "alias", Name: "\tREGISTRY_owned\u00a0", Order: 1, Promotion: model.PromotionPolicy{Strategy: "auto"}}
	p := NewPromoter([]*model.Environment{
		{ID: "dev", Name: "Development", Order: 0},
		alias,
		{ID: "registry_owned", Order: 2},
		nil,
		{ID: "public", Name: "Production", Order: 3, Promotion: model.PromotionPolicy{Strategy: "auto"}},
	})
	if len(p.environments) != 2 || p.NextEnvironment("dev").ID != "public" || p.NextEnvironment("alias") != nil || p.ShouldAutoPromote(alias) || p.ShouldAutoPromote(nil) {
		t.Fatal("private scope included in promotion chain")
	}
	for _, id := range []string{"alias", "registry_owned", "_registries"} {
		run := &model.PipelineRun{EnvironmentStatuses: []model.EnvironmentStatus{{EnvironmentID: id, Status: model.EnvStatusAwaitingApproval}}}
		if _, err := p.Promote(run, id, "operator"); err == nil {
			t.Fatal("private source promoted")
		}
		if err := p.ApprovePromotion(run, id, "approver"); err == nil || run.EnvironmentStatuses[0].Status != model.EnvStatusAwaitingApproval {
			t.Fatal("private legacy approval allowed")
		}
	}
	run := &model.PipelineRun{}
	if status, err := p.Promote(run, "dev", "operator"); err != nil || status.EnvironmentID != "public" {
		t.Fatal("public promotion chain changed")
	}
	// A later name change must not re-enable a filtered private identity.
	alias.Name = "renamed"
	if p.NextEnvironment("alias") != nil {
		t.Fatal("filtered private identity was re-enabled")
	}
}
