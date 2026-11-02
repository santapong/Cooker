package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/source/connection"
	"github.com/santapong/cooker/internal/store/memory"
)

func TestSourceWebhookAuthenticatesBeforeDurableDedupeAndFansOut(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	codec, _ := crypto.NewCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	connections := &connection.Service{Store: connection.NewMemory(), Codec: codec}
	conn := &connection.Connection{Name: "fixture", Provider: "github", Enabled: true, Repositories: []string{"acme/shop"}}
	secret := strings.Repeat("fixture-secret-", 3)
	if err := connections.Create(ctx, conn, secret); err != nil {
		t.Fatal(err)
	}
	triggers := &service.TriggerService{Store: memory.NewTriggerStore()}
	for _, id := range []string{"one", "two"} {
		if err := triggers.SaveRule(ctx, &model.TriggerRule{ID: id, AppID: id, Enabled: true, Source: model.GitSourceIdentity{Provider: "github", Host: "github.com", ConnectionID: conn.ID, RepositoryID: "19", Repository: "acme/shop"}, RefKind: "branch", Match: "exact", Include: []string{"main"}, Approval: "review"}); err != nil {
			t.Fatal(err)
		}
	}
	h := &Handler{SourceConnections: connections, Automation: &service.Automation{Triggers: triggers}}
	router := gin.New()
	router.POST("/webhooks/connections/:id", h.SourceWebhook)
	body := fmt.Sprintf(`{"ref":"refs/heads/main","after":%q,"repository":{"id":19,"full_name":"acme/shop"}}`, strings.Repeat("a", 40))
	send := func(body, signature string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/webhooks/connections/"+conn.ID, strings.NewReader(body))
		req.Header.Set("X-GitHub-Delivery", "delivery-one")
		req.Header.Set("X-GitHub-Event", "push")
		req.Header.Set("X-Hub-Signature-256", signature)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	sign := func(b string) string {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(b))
		return "sha256=" + hex.EncodeToString(mac.Sum(nil))
	}
	if w := send(body, "sha256=bad"); w.Code != 401 {
		t.Fatal(w.Code, w.Body.String())
	}
	w := send(body, sign(body))
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		Delivery  model.TriggerDelivery `json:"delivery"`
		Duplicate bool                  `json:"duplicate"`
	}
	json.Unmarshal(w.Body.Bytes(), &response)
	if response.Duplicate || len(response.Delivery.Decisions) != 2 {
		t.Fatalf("bad delivery: %+v", response)
	}
	w = send(body, sign(body))
	json.Unmarshal(w.Body.Bytes(), &response)
	if w.Code != 202 || !response.Duplicate {
		t.Fatal("authenticated retry did not dedupe")
	}
	changed := strings.ReplaceAll(body, strings.Repeat("a", 40), strings.Repeat("b", 40))
	if w := send(changed, sign(changed)); w.Code != 409 {
		t.Fatal("same identity accepted different content", w.Code)
	}
	if strings.Contains(w.Body.String(), secret) {
		t.Fatal("credential appeared in response")
	}
}

func TestGitLabSigningNeverDowngradesAndRejectsExpiredSignatures(t *testing.T) {
	key := []byte(strings.Repeat("k", 32))
	secret := []byte("whsec_" + base64.StdEncoding.EncodeToString(key))
	now := time.Unix(1800000000, 0)
	h := http.Header{}
	h.Set("webhook-id", "fixture")
	h.Set("webhook-timestamp", fmt.Sprint(now.Unix()))
	h.Set("X-Gitlab-Event", "Tag Push Hook")
	h.Set("X-Gitlab-Token", string(secret))
	body := []byte(`{}`)
	c := &connection.Connection{Provider: "gitlab", WebhookAuthMode: "signing"}
	if _, _, err := authenticateSourceWebhook(c, secret, body, h, now); err == nil {
		t.Fatal("signature mode downgraded to token")
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("fixture." + fmt.Sprint(now.Unix()) + "."))
	mac.Write(body)
	h.Set("webhook-signature", "v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	if _, _, err := authenticateSourceWebhook(c, secret, body, h, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := authenticateSourceWebhook(c, secret, body, h, now.Add(6*time.Minute)); err == nil {
		t.Fatal("expired signature accepted")
	}
	if _, _, err := authenticateSourceWebhook(c, secret, []byte(`{"changed":true}`), h, now); err == nil {
		t.Fatal("tampered body accepted")
	}
}

type webhookTagSource struct {
	identity model.GitSourceIdentity
	revision service.SourceRevision
	calls    int
}

func (s *webhookTagSource) Identity(context.Context, *connection.Connection, string) (model.GitSourceIdentity, error) {
	s.calls++
	return s.identity, nil
}
func (s *webhookTagSource) Revision(context.Context, *connection.Connection, string, string) (service.SourceRevision, error) {
	s.calls++
	return s.revision, nil
}
func TestTagWebhookPeelsOnceAndAuthenticatedRetriesKeepOriginalIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	codec, _ := crypto.NewCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	connections := &connection.Service{Store: connection.NewMemory(), Codec: codec}
	conn := &connection.Connection{Name: "tag source", Provider: "github", Enabled: true, Repositories: []string{"acme/shop"}}
	secret := strings.Repeat("fixture-secret-", 3)
	if err := connections.Create(ctx, conn, secret); err != nil {
		t.Fatal(err)
	}
	source := &webhookTagSource{identity: model.GitSourceIdentity{Provider: "github", Host: "github.com", ConnectionID: conn.ID, RepositoryID: "19", Repository: "acme/shop"}, revision: service.SourceRevision{FullRef: "refs/tags/release/v1", Commit: strings.Repeat("a", 40), Object: strings.Repeat("b", 40)}}
	triggers := &service.TriggerService{Store: memory.NewTriggerStore()}
	rule := &model.TriggerRule{ID: "tag-rule", AppID: "app", Enabled: true, Source: source.identity, RefKind: "tag", Match: "exact", Include: []string{"release/v1"}, Approval: "review"}
	if err := triggers.SaveRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	h := &Handler{SourceConnections: connections, Automation: &service.Automation{Triggers: triggers, Source: source}}
	router := gin.New()
	router.POST("/hook/:id", h.SourceWebhook)
	body := fmt.Sprintf(`{"ref":"refs/tags/release/v1","after":%q,"repository":{"id":19,"full_name":"acme/shop"}}`, source.revision.Object)
	send := func(id, body string, valid bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/hook/"+conn.ID, strings.NewReader(body))
		req.Header.Set("X-GitHub-Event", "push")
		req.Header.Set("X-GitHub-Delivery", id)
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(body))
		signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if !valid {
			signature = "sha256=invalid"
		}
		req.Header.Set("X-Hub-Signature-256", signature)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	var result struct {
		Delivery  model.TriggerDelivery `json:"delivery"`
		Duplicate bool                  `json:"duplicate"`
	}
	w := send("original", body, true)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Delivery.Event.CommitSHA != source.revision.Commit || result.Delivery.Event.ObjectSHA != source.revision.Object || len(result.Delivery.Decisions) != 1 {
		t.Fatal("tag identity not normalized", result)
	}
	originalID := result.Delivery.ID
	originalObject := source.revision.Object
	calls := source.calls
	source.revision.Object = strings.Repeat("c", 40) // tag re-annotated after the delivery was recorded
	if w := send("original", body, false); w.Code != 401 {
		t.Fatal("dedupe bypassed authentication", w.Code)
	}
	w = send("original", body, true)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	json.Unmarshal(w.Body.Bytes(), &result)
	if !result.Duplicate || result.Delivery.ID != originalID || result.Delivery.Event.ObjectSHA != originalObject || source.calls != calls {
		t.Fatal("retry consulted moving provider state", result, source.calls, calls)
	}
	changed := strings.ReplaceAll(body, originalObject, strings.Repeat("d", 40))
	if w := send("original", changed, true); w.Code != 409 {
		t.Fatal("same identity accepted another body", w.Code)
	}
	if w := send("new-delivery", body, true); w.Code != 409 {
		t.Fatal("old unrecorded tag substituted current tag", w.Code)
	}
}
