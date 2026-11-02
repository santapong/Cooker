package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/source/connection"
	"github.com/santapong/cooker/internal/store/memory"
)

const automationErrorCanary = "synthetic-private-provider-password-92409"

type automationPublicErrorStore struct {
	*memory.TriggerStore
	failTransition  bool
	transitionCalls int
}

func (s *automationPublicErrorStore) TransitionIntent(ctx context.Context, id string, version int, status model.TriggerIntentStatus, reason string, receipt *model.TriggerReceipt) error {
	s.transitionCalls++
	if s.failTransition {
		return errors.New("database error: " + automationErrorCanary)
	}
	return s.TriggerStore.TransitionIntent(ctx, id, version, status, reason, receipt)
}

type automationPublicErrorFixture struct {
	automation *service.Automation
	triggers   *automationPublicErrorStore
	rule       *model.TriggerRule
	intent     *model.TriggerIntent
	failure    string
	inspected  int
}

func newAutomationPublicErrorFixture(t *testing.T) *automationPublicErrorFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	codec, err := crypto.NewCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	connections := &connection.Service{Store: connection.NewMemory(), Codec: codec}
	conn := &connection.Connection{Name: "fixture", Provider: "github", Repositories: []string{"team/shop"}, Enabled: true}
	if err := connections.Create(ctx, conn, strings.Repeat("fixture-key-", 4)); err != nil {
		t.Fatal(err)
	}
	st := memory.New()
	env := &model.Environment{ID: "fixture-environment", Name: "Fixture", Version: 1}
	if err := st.Environments.Create(ctx, env); err != nil {
		t.Fatal(err)
	}
	app := &model.App{ID: "fixture-app", Name: "Fixture", GitHubRepo: "team/shop", EnvironmentID: env.ID, BuildPlan: &model.BuildPlan{Kind: model.BuildPlanCompose, Path: "compose.yaml"}, DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost}}
	if err := st.Apps.Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	source := &webhookTagSource{identity: model.GitSourceIdentity{Provider: "github", Host: "github.com", ConnectionID: conn.ID, RepositoryID: "19", Repository: "team/shop"}, revision: service.SourceRevision{FullRef: "refs/heads/main", Commit: strings.Repeat("a", 40), Object: strings.Repeat("a", 40)}}
	f := &automationPublicErrorFixture{triggers: &automationPublicErrorStore{TriggerStore: memory.NewTriggerStore()}}
	f.automation = &service.Automation{Store: st, Connections: connections, Source: source, Triggers: &service.TriggerService{Store: f.triggers}, ResolveEnvironment: func(context.Context, *model.App) (map[string]string, error) {
		if f.failure == "environment" {
			return nil, errors.New("secret manager error: " + automationErrorCanary)
		}
		return map[string]string{"PRIVATE_PASSWORD": automationErrorCanary}, nil
	}, Inspect: func(_ context.Context, _ *model.App, values map[string]string) (*service.AutomationEnvelope, error) {
		f.inspected++
		if values["PRIVATE_PASSWORD"] != automationErrorCanary {
			t.Fatal("fixture did not exercise resolved private environment")
		}
		if f.failure == "inspection" {
			return nil, errors.New("inspection diagnostic includes " + values["PRIVATE_PASSWORD"])
		}
		return &service.AutomationEnvelope{Hash: strings.Repeat("b", 64)}, nil
	}, Authorize: func(context.Context, *model.TriggerRule, *model.App) error { return nil }}
	rule := &model.TriggerRule{ID: uuid.NewString(), AppID: app.ID, Enabled: true, Source: source.identity, RefKind: "branch", Match: "exact", Include: []string{"main"}, Approval: "review"}
	preview, err := f.automation.Preview(ctx, rule, "refs/heads/main")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.automation.Triggers.SaveRule(ctx, preview.Rule); err != nil {
		t.Fatal(err)
	}
	f.rule = preview.Rule
	event := model.VerifiedGitEvent{Source: source.identity, DeliveryID: "fixture-delivery", Event: "push", FullRef: "refs/heads/main", CommitSHA: source.revision.Commit, PayloadHash: strings.Repeat("c", 64)}
	delivery, _, err := f.automation.Triggers.AcceptVerified(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	f.intent, err = f.triggers.GetIntent(ctx, delivery.Decisions[0].IntentID)
	if err != nil || f.intent.Status != model.TriggerAwaitingReview {
		t.Fatal("fixture does not require approval", f.intent, err)
	}
	f.inspected = 0
	return f
}

func assertAutomationSafeError(t *testing.T, w *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	if w.Code != wantStatus {
		t.Fatal(w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), automationErrorCanary) || strings.Contains(w.Body.String(), "PRIVATE_PASSWORD") {
		t.Fatal("private nested error escaped the public response")
	}
	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response["code"] != wantCode || response["error"] == "" || len(response) != 2 {
		t.Fatal("response lacks a bounded safe public error", response, err)
	}
}

func TestAutomationPreviewAndSavePrivateFailuresHaveSafePublicErrors(t *testing.T) {
	for _, failure := range []string{"inspection", "environment"} {
		for _, action := range []string{"preview", "save"} {
			t.Run(action+"/"+failure, func(t *testing.T) {
				f := newAutomationPublicErrorFixture(t)
				f.failure = failure
				h := &Handler{Automation: f.automation}
				r := gin.New()
				r.POST("/preview", h.PreviewTriggerRule)
				r.POST("/save", h.SaveTriggerRule)
				body, _ := json.Marshal(triggerRequest{Rule: *f.rule, Ref: "refs/heads/main"})
				req := httptest.NewRequest("POST", "/"+action, strings.NewReader(string(body)))
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				assertAutomationSafeError(t, w, 422, "trigger_review_unavailable")
				stored, err := f.triggers.GetRule(context.Background(), f.rule.ID)
				if err != nil || stored.Version != f.rule.Version {
					t.Fatal("failed inspection changed saved rule", stored, err)
				}
				if (failure == "inspection") != (f.inspected == 1) {
					t.Fatal("fixture failed before the intended private-error boundary", f.inspected)
				}
			})
		}
	}
}

func TestAutomationApprovalPrivateFailuresHaveSafePublicErrors(t *testing.T) {
	for _, failure := range []string{"inspection", "transition"} {
		t.Run(failure, func(t *testing.T) {
			f := newAutomationPublicErrorFixture(t)
			f.failure = failure
			f.triggers.failTransition = failure == "transition"
			h := &Handler{Automation: f.automation}
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set("user", &auth.Claims{Subject: "fixture-admin", Roles: []string{"admin"}})
				c.Next()
			})
			r.POST("/intents/:id/approve", h.ApproveTriggerIntent)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("POST", "/intents/"+f.intent.ID+"/approve", nil))
			assertAutomationSafeError(t, w, 409, "trigger_approval_unavailable")
			stored, err := f.triggers.GetIntent(context.Background(), f.intent.ID)
			if err != nil || stored.Status != model.TriggerAwaitingReview || stored.Version != f.intent.Version {
				t.Fatal("failed approval incorrectly advanced the intent", stored, err)
			}
			if f.inspected != 1 || (failure == "transition") != (f.triggers.transitionCalls == 1) {
				t.Fatal("fixture did not reach the intended private-error boundary", f.inspected, f.triggers.transitionCalls)
			}
		})
	}
}
