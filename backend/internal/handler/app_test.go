package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
)

// captureSpawner is a test RunSpawner that records the runIDs it was
// asked to spawn but does NOT execute the work. This keeps the test
// hermetic: the production deploy path clones over the network
// (github.Clone shells out to `git clone`), which a unit test must not
// do. The handler still creates the stub run row synchronously *before*
// calling Spawn (the F-07 fix), so asserting on that row proves a real
// deploy was dispatched and attributed — exactly the symptom HS26-05-02
// describes ("a 202 fires with no run created"). The RunSpawner
// interface exists precisely so tests can inject a fake like this
// without importing the server package.
type captureSpawner struct{ runIDs []string }

func (s *captureSpawner) Spawn(_ context.Context, runID string, _ func(context.Context) error) error {
	s.runIDs = append(s.runIDs, runID)
	return nil
}

func (s *captureSpawner) SpawnWithDeadline(_ context.Context, runID string, _ time.Duration, _ func(context.Context) error) error {
	s.runIDs = append(s.runIDs, runID)
	return nil
}

// signedPush builds an HMAC-SHA256 X-Hub-Signature-256 header value for
// body under secret, matching what GitHub stamps on a push delivery.
func signedPush(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestApp_CreateAndList_RedactsWebhookSecret(t *testing.T) {
	h := newTestHandler(t)
	admin := &auth.Claims{Email: "a@example.com", Roles: []string{string(auth.RoleAdmin)}}

	r := gin.New()
	r.POST("/apps", withUser(h.CreateApp, admin))
	r.GET("/apps", withUser(h.ListApps, admin))

	create := model.App{
		Name:       "web",
		GitHubRepo: "acme/web",
		Branch:     "main",
		AutoDeploy: true,
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/apps", create))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/apps", nil))
	var list []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list size: %d", len(list))
	}
	if _, present := list[0]["webhookSecret"]; present {
		t.Error("webhookSecret must never appear in list response")
	}
	if hw, _ := list[0]["hasWebhook"].(bool); hw {
		t.Error("hasWebhook should be false for app without webhook")
	}
}

// seedWebhookApp creates an App through the real CreateApp handler and
// rotates its webhook secret through the real SetAppWebhookSecret handler
// (so encryption is exercised end-to-end), returning the created App and
// the plaintext secret. r must already have both routes bound.
func seedWebhookApp(t *testing.T, r *gin.Engine, name, repo string, autoDeploy bool) (model.App, string) {
	t.Helper()
	create := model.App{Name: name, GitHubRepo: repo, Branch: "main", AutoDeploy: autoDeploy}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/apps", create))
	if w.Code != http.StatusCreated {
		t.Fatalf("create app: %d %s", w.Code, w.Body.String())
	}
	var got model.App
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	secret := "github-hook-secret"
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPut, "/apps/"+got.ID+"/webhook", map[string]string{"secret": secret}))
	if w.Code != http.StatusOK {
		t.Fatalf("rotate: %d %s", w.Code, w.Body.String())
	}
	return got, secret
}

// TestGitHubWebhook_Valid_HMAC_TriggersDeploy proves HS26-05-02 is
// closed: a signature-verified push to a matching branch of an
// auto-deploy app now CREATES a run (attributed to the webhook) and
// dispatches it down the same path the manual Deploy button uses —
// rather than firing a bare 202 with nothing behind it.
func TestGitHubWebhook_Valid_HMAC_TriggersDeploy(t *testing.T) {
	h := newTestHandler(t)
	// Same wiring as the manual Deploy button: a real AppDeployer (noop
	// executor) plus a RunSpawner. captureSpawner records the dispatch
	// without running the (network-cloning) deploy, keeping the test
	// hermetic while still exercising the stub-run-row creation.
	h.AppDeployer = service.NewAppDeployer(service.NewExecutor(), "reg")
	h.AppDeployer.Deploys = h.Store.AppDeploys
	spawner := &captureSpawner{}
	h.Runs = spawner

	admin := &auth.Claims{Email: "a@example.com", Roles: []string{string(auth.RoleAdmin)}}
	r := gin.New()
	r.POST("/apps", withUser(h.CreateApp, admin))
	r.PUT("/apps/:id/webhook", withUser(h.SetAppWebhookSecret, admin))
	r.POST("/webhooks/github", h.GitHubWebhook)

	got, secret := seedWebhookApp(t, r, "api", "acme/api", true)

	payload := []byte(`{"ref":"refs/heads/main","after":"deadbeef","repository":{"full_name":"acme/api"}}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", signedPush(secret, payload))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("webhook: %d %s", w.Code, w.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["commit"] != "deadbeef" || body["branch"] != "main" {
		t.Errorf("body: %v", body)
	}
	// The 202 must carry a runId the client can subscribe to — the whole
	// point of HS26-05-02 is that a run is created, not just acknowledged.
	runID, _ := body["runId"].(string)
	if runID == "" {
		t.Fatalf("expected a runId in the webhook response, got %v", body)
	}

	// The deploy must have been dispatched onto the run coordinator for
	// exactly the run the response named.
	if len(spawner.runIDs) != 1 || spawner.runIDs[0] != runID {
		t.Fatalf("expected one spawned deploy for run %s, got %v", runID, spawner.runIDs)
	}

	// A run row must exist (the handler creates it before Spawn — the
	// F-07 fix), attributed to the webhook rather than a human. An empty
	// StartedByUserSub correctly signals "no human actor" to the deploy
	// governance hook, matching the manual DeployApp stub.
	run, err := h.Store.Runs.Get(context.Background(), runID)
	if err != nil {
		t.Fatalf("run %s not created by webhook: %v", runID, err)
	}
	if run.StartedByEmail != "webhook:github" {
		t.Errorf("run.StartedByEmail = %q, want webhook:github", run.StartedByEmail)
	}
	if run.StartedByUserSub != "" {
		t.Errorf("webhook run must have no human subject, got %q", run.StartedByUserSub)
	}
	if run.PipelineID != got.ID {
		t.Errorf("run.PipelineID = %q, want app id %q", run.PipelineID, got.ID)
	}
	if run.Status != model.RunStatusRunning {
		t.Errorf("run.Status = %q, want running", run.Status)
	}
}

// TestGitHubWebhook_AutoDeployDisabled_NoRun proves the inverse: a
// verified push to an app with AutoDeploy=false is acknowledged but
// creates NO run and dispatches NO deploy.
func TestGitHubWebhook_AutoDeployDisabled_NoRun(t *testing.T) {
	h := newTestHandler(t)
	h.AppDeployer = service.NewAppDeployer(service.NewExecutor(), "reg")
	h.AppDeployer.Deploys = h.Store.AppDeploys
	spawner := &captureSpawner{}
	h.Runs = spawner

	admin := &auth.Claims{Email: "a@example.com", Roles: []string{string(auth.RoleAdmin)}}
	r := gin.New()
	r.POST("/apps", withUser(h.CreateApp, admin))
	r.PUT("/apps/:id/webhook", withUser(h.SetAppWebhookSecret, admin))
	r.POST("/webhooks/github", h.GitHubWebhook)

	got, secret := seedWebhookApp(t, r, "api", "acme/api", false)

	payload := []byte(`{"ref":"refs/heads/main","after":"deadbeef","repository":{"full_name":"acme/api"}}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", signedPush(secret, payload))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	// Signature is valid, so this is a 200 "ignored", not an error.
	if w.Code != http.StatusOK {
		t.Fatalf("webhook: %d %s, want 200 ignored", w.Code, w.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["ignored"] != "autoDeploy disabled" {
		t.Errorf("expected ignored=autoDeploy disabled, got %v", body)
	}
	if _, hasRun := body["runId"]; hasRun {
		t.Errorf("auto-deploy=false must not return a runId: %v", body)
	}

	// No deploy dispatched and no run row created (the stub would be keyed
	// PipelineID=app.ID).
	if len(spawner.runIDs) != 0 {
		t.Fatalf("auto-deploy=false dispatched %d deploys; want 0", len(spawner.runIDs))
	}
	if runs, err := h.Store.Runs.List(context.Background(), got.ID, 0, 0); err != nil {
		t.Fatal(err)
	} else if len(runs) != 0 {
		t.Fatalf("auto-deploy=false created %d run rows; want 0", len(runs))
	}
}

func TestGitHubWebhook_Rejects_BadSignature(t *testing.T) {
	h := newTestHandler(t)
	admin := &auth.Claims{Email: "a@example.com", Roles: []string{string(auth.RoleAdmin)}}
	r := gin.New()
	r.POST("/apps", withUser(h.CreateApp, admin))
	r.PUT("/apps/:id/webhook", withUser(h.SetAppWebhookSecret, admin))
	r.POST("/webhooks/github", h.GitHubWebhook)

	create := model.App{Name: "svc", GitHubRepo: "acme/svc", Branch: "main", AutoDeploy: true}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/apps", create))
	var got model.App
	_ = json.Unmarshal(w.Body.Bytes(), &got)

	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPut, "/apps/"+got.ID+"/webhook", map[string]string{"secret": "right"}))

	payload := []byte(`{"ref":"refs/heads/main","repository":{"full_name":"acme/svc"}}`)
	mac := hmac.New(sha256.New, []byte("wrong"))
	mac.Write(payload)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", sig)

	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// TestGitHubWebhook_BadSignature_DispatchesNoDeploy proves the signature
// check is a hard gate BEFORE any deploy work: an invalid HMAC returns 401
// and never reaches triggerWebhookDeploy, so no run is spawned and no run row
// is created (C-webhook-logs — reject early on signature failure).
func TestGitHubWebhook_BadSignature_DispatchesNoDeploy(t *testing.T) {
	h := newTestHandler(t)
	h.AppDeployer = service.NewAppDeployer(service.NewExecutor(), "reg")
	h.AppDeployer.Deploys = h.Store.AppDeploys
	spawner := &captureSpawner{}
	h.Runs = spawner

	admin := &auth.Claims{Email: "a@example.com", Roles: []string{string(auth.RoleAdmin)}}
	r := gin.New()
	r.POST("/apps", withUser(h.CreateApp, admin))
	r.PUT("/apps/:id/webhook", withUser(h.SetAppWebhookSecret, admin))
	r.POST("/webhooks/github", h.GitHubWebhook)

	got, _ := seedWebhookApp(t, r, "api", "acme/api", true)

	payload := []byte(`{"ref":"refs/heads/main","after":"deadbeef","repository":{"full_name":"acme/api"}}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "push")
	// Signature over the WRONG secret — must be rejected before any deploy.
	req.Header.Set("X-Hub-Signature-256", signedPush("attacker-secret", payload))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("bad signature: got %d, want 401", w.Code)
	}
	if len(spawner.runIDs) != 0 {
		t.Errorf("bad signature dispatched %d deploys; want 0", len(spawner.runIDs))
	}
	if runs, err := h.Store.Runs.List(context.Background(), got.ID, 0, 0); err != nil {
		t.Fatal(err)
	} else if len(runs) != 0 {
		t.Errorf("bad signature created %d run rows; want 0", len(runs))
	}
}

func TestGitHubWebhook_UnknownRepo_Returns204(t *testing.T) {
	h := newTestHandler(t)
	r := gin.New()
	r.POST("/webhooks/github", h.GitHubWebhook)

	payload := []byte(`{"ref":"refs/heads/main","repository":{"full_name":"nobody/here"}}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", "sha256=00")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", w.Code)
	}
}
