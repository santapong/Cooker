package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/source/connection"
	"github.com/santapong/cooker/internal/store/memory"
)

func TestTriggerPreviewAndSaveCannotMintClientSuppliedAuthority(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	codec, _ := crypto.NewCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	connections := &connection.Service{Store: connection.NewMemory(), Codec: codec}
	conn := &connection.Connection{Name: "fixture", Provider: "github", Repositories: []string{"acme/shop"}, Enabled: true}
	if err := connections.Create(ctx, conn, strings.Repeat("fixture-key-", 4)); err != nil {
		t.Fatal(err)
	}
	st := memory.New()
	app := &model.App{ID: "app", Name: "shop", GitHubRepo: "acme/shop", BuildPlan: &model.BuildPlan{Kind: model.BuildPlanCompose, Path: "compose.yaml"}, DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost}}
	if err := st.Apps.Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	source := &webhookTagSource{identity: model.GitSourceIdentity{Provider: "github", Host: "github.com", ConnectionID: conn.ID, RepositoryID: "19", Repository: "acme/shop"}, revision: service.SourceRevision{FullRef: "refs/heads/main", Commit: strings.Repeat("a", 40), Object: strings.Repeat("a", 40)}}
	allowed := true
	a := &service.Automation{Store: st, Connections: connections, Source: source, Triggers: &service.TriggerService{Store: memory.NewTriggerStore()}, Inspect: func(context.Context, *model.App, map[string]string) (*service.AutomationEnvelope, error) {
		return &service.AutomationEnvelope{Hash: strings.Repeat("b", 64)}, nil
	}, Authorize: func(_ context.Context, r *model.TriggerRule, _ *model.App) error {
		if r.ActorSubject != "cooker:trigger:"+r.ID || len(r.ActorGroups) != 0 {
			t.Fatal("forged authority reached authorization")
		}
		if !allowed {
			return context.Canceled
		}
		return nil
	}}
	h := &Handler{Automation: a}
	router := gin.New()
	router.POST("/preview", h.PreviewTriggerRule)
	router.POST("/save", h.SaveTriggerRule)
	rule := model.TriggerRule{ID: uuid.NewString(), AppID: app.ID, Enabled: true, Source: source.identity, RefKind: "branch", Match: "exact", Include: []string{"main"}, Approval: "automatic", ActorSubject: "forged-admin", ActorGroups: []string{"admin"}, PlanHash: "client-plan", ApprovedEnvelopeHash: "client-approval"}
	send := func(path string, value any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(value)
		req := httptest.NewRequest("POST", path, strings.NewReader(string(raw)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	req := triggerRequest{Rule: rule, Ref: "refs/heads/main"}
	w := send("/preview", req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var preview service.TriggerPreview
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Rule.ActorSubject != "cooker:trigger:"+rule.ID || len(preview.Rule.ActorGroups) != 0 || preview.Rule.ApprovedEnvelopeHash == rule.ApprovedEnvelopeHash || preview.Rule.PlanHash == rule.PlanHash {
		t.Fatal("client authority survived preview")
	}
	if w := send("/save", req); w.Code != 409 {
		t.Fatal("unreviewed envelope accepted", w.Code)
	}
	req.ExpectedEnvelopeHash, req.ExpectedPlanHash = preview.Rule.ApprovedEnvelopeHash, preview.Rule.PlanHash
	allowed = false
	if w := send("/save", req); w.Code != 422 {
		t.Fatal("revoked machine authority accepted", w.Code)
	}
	allowed = true
	w = send("/save", req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	saved, err := a.Triggers.Store.GetRule(ctx, rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ActorSubject != "cooker:trigger:"+rule.ID || len(saved.ActorGroups) != 0 {
		t.Fatal("forged authority persisted")
	}
}
