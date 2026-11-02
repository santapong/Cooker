package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/source/connection"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
)

type journalFaultStore struct {
	*memory.TriggerStore
	failBegin, failRecord, failFinish bool
	cancelAfterRecord                 context.CancelFunc
	finishContextIndependent          bool
}

func (s *journalFaultStore) BeginWebhookAttempt(ctx context.Context, a *model.WebhookAttempt) error {
	if s.failBegin {
		return errors.New("synthetic-secret-canary")
	}
	return s.TriggerStore.BeginWebhookAttempt(ctx, a)
}
func (s *journalFaultStore) RecordDelivery(ctx context.Context, d *model.TriggerDelivery, i []*model.TriggerIntent) (*model.TriggerDelivery, bool, error) {
	if s.failRecord {
		return nil, false, errors.New("synthetic-secret-canary")
	}
	saved, dup, err := s.TriggerStore.RecordDelivery(ctx, d, i)
	if s.cancelAfterRecord != nil {
		s.cancelAfterRecord()
	}
	return saved, dup, err
}
func (s *journalFaultStore) FinishWebhookAttempt(ctx context.Context, id, status, code, delivery string) error {
	_, deadline := ctx.Deadline()
	s.finishContextIndependent = ctx.Err() == nil && deadline
	if s.failFinish {
		return errors.New("synthetic-secret-canary")
	}
	return s.TriggerStore.FinishWebhookAttempt(ctx, id, status, code, delivery)
}

func journalHTTPFixture(t *testing.T) (*journalFaultStore, func(context.Context, string, string, string, bool) *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	codec, _ := crypto.NewCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	connections := &connection.Service{Store: connection.NewMemory(), Codec: codec}
	conn := &connection.Connection{Name: "fixture", Provider: "github", Enabled: true, Repositories: []string{"team/shop"}}
	secret := strings.Repeat("fixture-webhook-secret-", 3)
	if err := connections.Create(ctx, conn, secret); err != nil {
		t.Fatal(err)
	}
	s := &journalFaultStore{TriggerStore: memory.NewTriggerStore()}
	triggers := &service.TriggerService{Store: s}
	if err := triggers.SaveRule(ctx, &model.TriggerRule{ID: "rule", AppID: "app", EnvironmentID: "environment", Enabled: true, Source: model.GitSourceIdentity{Provider: "github", ConnectionID: conn.ID, Host: "github.com", RepositoryID: "19", Repository: "team/shop"}, RefKind: "branch", Match: "exact", Include: []string{"main"}, Approval: "review"}); err != nil {
		t.Fatal(err)
	}
	h := &Handler{SourceConnections: connections, Automation: &service.Automation{Triggers: triggers}}
	r := gin.New()
	r.POST("/hook/:id", h.SourceWebhook)
	send := func(ctx context.Context, id, event, body string, signed bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/hook/"+conn.ID, strings.NewReader(body)).WithContext(ctx)
		req.Header.Set("X-GitHub-Delivery", id)
		req.Header.Set("X-GitHub-Event", event)
		req.Header.Set("Authorization", "synthetic-secret-canary")
		if signed {
			mac := hmac.New(sha256.New, []byte(secret))
			mac.Write([]byte(body))
			req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if strings.Contains(w.Body.String(), "synthetic-secret-canary") {
			t.Fatal("private request/error escaped response")
		}
		return w
	}
	return s, send
}

func TestWebhookJournalHTTPAuthenticationRetryRejectionAndNoDuplicateIntent(t *testing.T) {
	s, send := journalHTTPFixture(t)
	ctx := context.Background()
	body := `{"ref":"refs/heads/main","after":"` + strings.Repeat("a", 40) + `","repository":{"id":19,"full_name":"team/shop"},"environment":{"PASSWORD":"synthetic-secret-canary"}}`
	if w := send(ctx, "one", "push", body, false); w.Code != 401 {
		t.Fatal(w.Code)
	}
	p, _ := s.ListWebhookAttempts(ctx, store.WebhookAttemptQuery{})
	if p.Total != 0 {
		t.Fatal("unauthenticated request created audit identity")
	}
	for n := 0; n < 2; n++ {
		if w := send(ctx, "one", "push", body, true); w.Code != 202 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := send(ctx, "two", "push", body, true); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	changed := strings.Replace(body, strings.Repeat("a", 40), strings.Repeat("b", 40), 1)
	if w := send(ctx, "one", "push", changed, true); w.Code != 409 {
		t.Fatal(w.Code)
	}
	if w := send(ctx, "ping", "ping", `{"private":"synthetic-secret-canary"}`, true); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := send(ctx, "malformed", "push", `{"private":"synthetic-secret-canary"`, true); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w := send(ctx, "not-allowed", "push", strings.Replace(body, "team/shop", "private/repo", 1), true); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := send(ctx, "invalid", "push", strings.Replace(body, "refs/heads/main", "not-a-full-ref", 1), true); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w := send(ctx, strings.Repeat("x", 300), "push", body, true); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w := send(ctx, "tag", "push", strings.Replace(body, "refs/heads/main", "refs/tags/v1", 1), true); w.Code != 503 {
		t.Fatal(w.Code)
	}
	p, err := s.ListWebhookAttempts(ctx, store.WebhookAttemptQuery{})
	if err != nil || p.Total != 10 {
		t.Fatal(p, err)
	}
	seen := map[string]int{}
	for _, a := range p.Items {
		seen[a.ReasonCode]++
		if a.Status == "received" || a.FinishedAt == nil {
			t.Fatal("completed request left unfinished audit", a)
		}
	}
	for code, n := range map[string]int{"admitted": 2, "already_recorded": 1, "delivery_identity_conflict": 1, "unsupported_event": 1, "invalid_json": 1, "repository_not_allowed": 1, "invalid_event": 1, "invalid_delivery_identity": 1, "tag_source_unavailable": 1} {
		if seen[code] != n {
			t.Fatal(code, seen)
		}
	}
	b, _ := json.Marshal(p)
	if strings.Contains(string(b), "synthetic-secret-canary") || strings.Contains(string(b), "private/repo") {
		t.Fatal("private request metadata stored in audit")
	}
	intents, _ := s.ListIntents(ctx, "app", 100)
	if len(intents) != 1 {
		t.Fatal("authenticated retry admitted another intent", len(intents))
	}
	history, _ := s.ListTriggerHistory(ctx, store.TriggerHistoryQuery{Status: "duplicate"})
	if history.Total != 1 || history.Items[0].IntentID != intents[0].ID {
		t.Fatal("logical duplicate lost original intent", history)
	}
}

func TestWebhookJournalHTTPUnconfirmedAuditAndCancelledRequest(t *testing.T) {
	for _, mode := range []string{"begin", "admission", "finish", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			s, send := journalHTTPFixture(t)
			ctx := context.Background()
			if mode == "begin" {
				s.failBegin = true
			}
			if mode == "admission" {
				s.failRecord = true
			}
			if mode == "finish" {
				s.failFinish = true
			}
			if mode == "cancel" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				s.cancelAfterRecord = cancel
			}
			body := `{"ref":"refs/heads/main","after":"` + strings.Repeat("a", 40) + `","repository":{"id":19,"full_name":"team/shop"}}`
			w := send(ctx, "original", "push", body, true)
			want := 503
			if mode == "cancel" {
				want = 202
			}
			if w.Code != want {
				t.Fatal(w.Code, w.Body.String())
			}
			page, _ := s.ListWebhookAttempts(context.Background(), store.WebhookAttemptQuery{})
			switch mode {
			case "begin":
				if page.Total != 0 {
					t.Fatal(page)
				}
			case "admission":
				if page.Total != 1 || page.Items[0].Status != "uncertain" {
					t.Fatal(page)
				}
			case "finish":
				if page.Total != 1 || page.Items[0].Status != "received" || page.Items[0].FinishedAt != nil {
					t.Fatal(page)
				}
				s.failFinish = false
				if w := send(context.Background(), "original", "push", body, true); w.Code != 202 {
					t.Fatal(w.Code)
				}
				page, _ = s.ListWebhookAttempts(context.Background(), store.WebhookAttemptQuery{})
				if page.Total != 2 {
					t.Fatal(page)
				}
				intents, _ := s.ListIntents(context.Background(), "app", 100)
				if len(intents) != 1 {
					t.Fatal("unconfirmed response replayed intent")
				}
			case "cancel":
				if !s.finishContextIndependent || page.Total != 1 || page.Items[0].Status != "accepted" {
					t.Fatal("request cancellation blocked bounded audit completion", page)
				}
			}
		})
	}
}
