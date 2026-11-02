package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/model"
)

type registryProbeFixture struct {
	deniedRegistrySecretFixture
	listed []string
}

func (f *registryProbeFixture) List(_ context.Context, id string) ([]string, error) {
	f.listed = append(f.listed, id)
	return []string{"password"}, nil
}

func TestRegistryEnvironmentSecretsProbeRejectsPrivateAndUnknownScopes(t *testing.T) {
	h := newTestHandler(t)
	f := &registryProbeFixture{}
	h.Secrets = f
	ctx := context.Background()
	for _, e := range []*model.Environment{
		{ID: "_registries", Name: "_registries (internal)", Order: 0},
		{ID: "registry_owned", Order: 1},
		{ID: "friendly-alias", Name: "\tREGISTRY_owned\u00a0", Order: 2},
		{ID: "public", Name: "Production", Order: 3},
	} {
		if err := h.Store.Environments.Create(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"_registries", "registry_owned", "friendly-alias", "missing"} {
		w := httptest.NewRecorder()
		secretsTestRouter(h).ServeHTTP(w, newRequest(http.MethodPost, "/settings/secrets/test", map[string]string{"environmentId": id}))
		if w.Code != http.StatusNotFound || len(f.listed) != 0 || f.calls != 0 {
			t.Fatalf("private or unknown probe reached provider: HTTP%d", w.Code)
		}
	}
	for _, body := range []any{nil, map[string]string{"environmentId": "public"}} {
		w := httptest.NewRecorder()
		secretsTestRouter(h).ServeHTTP(w, newRequest(http.MethodPost, "/settings/secrets/test", body))
		if w.Code != http.StatusOK {
			t.Fatalf("public probe denied: HTTP%d", w.Code)
		}
	}
	if len(f.listed) != 2 || f.listed[0] != "public" || f.listed[1] != "public" || f.calls != 0 {
		t.Fatal("default probe did not skip all private scopes")
	}
}

func TestRegistryEnvironmentSecretsProbePrivateOnlyDoesNotReadProvider(t *testing.T) {
	h := newTestHandler(t)
	f := &registryProbeFixture{}
	h.Secrets = f
	if err := h.Store.Environments.Create(context.Background(), &model.Environment{ID: "alias", Name: "registry_owned"}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	secretsTestRouter(h).ServeHTTP(w, newRequest(http.MethodPost, "/settings/secrets/test", nil))
	if w.Code != http.StatusConflict || len(f.listed) != 0 || f.calls != 0 {
		t.Fatal("private-only default probe reached provider")
	}
}

func TestRegistryEnvironmentRunPromotionCannotCreateOrApprovePrivateTargets(t *testing.T) {
	ctx := context.Background()
	private := []*model.Environment{
		{ID: "_registries", Name: "_registries (internal)"},
		{ID: "registry_owned"},
		{ID: "friendly-alias", Name: "\tREGISTRY_owned\u00a0"},
	}
	h := seedPromotionFixture(t, append(private, &model.Environment{ID: "public", Name: "Production", Promotion: model.PromotionPolicy{Strategy: "manual", RequiredApprovers: 1}})...)
	for _, env := range private {
		if err := h.Store.Promotions.CreatePromotion(ctx, &model.RunPromotion{ID: "old-" + env.ID, RunID: "r1", PipelineID: "p1", EnvironmentID: env.ID, Strategy: "manual", RequiredApprovers: 1, Status: model.PromotionAwaitingApproval}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.Promotions.Promote(ctx, "r1", "p1", "public", "owned-operator"); err != nil {
		t.Fatal(err)
	}
	operator := &auth.Claims{Subject: "owned-operator", Roles: []string{string(auth.RoleOperator)}}
	approver := &auth.Claims{Subject: "owned-approver", Roles: []string{string(auth.RoleApprover)}}
	r := gin.New()
	r.POST("/pipelines/:id/runs/:runId/promote", withUser(h.PromoteRun, operator))
	r.POST("/pipelines/:id/runs/:runId/approve", withUser(h.ApprovePromotion, approver))
	r.GET("/pipelines/:id/runs/:runId/env-status", withUser(h.GetEnvStatus, operator))
	for _, env := range private {
		for _, action := range []string{"promote", "approve"} {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, newRequest(http.MethodPost, "/pipelines/p1/runs/r1/"+action, map[string]string{"environmentId": env.ID}))
			if w.Code != http.StatusNotFound {
				t.Fatalf("private promotion visible: HTTP%d", w.Code)
			}
		}
		stored, err := h.Store.Promotions.GetPromotion(ctx, "r1", env.ID)
		if err != nil || stored.Status != model.PromotionAwaitingApproval || stored.ApprovalCount() != 0 {
			t.Fatal("private historical promotion mutated")
		}
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodGet, "/pipelines/p1/runs/r1/env-status", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("public status listing failed: HTTP%d", w.Code)
	}
	statuses := decodeStatuses(t, w)
	if len(statuses) != 1 || statuses[0].EnvironmentID != "public" {
		t.Fatal("private promotion leaked through statuses")
	}
}
