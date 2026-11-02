//go:build integration

package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/auth"
	cookercrypto "github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/deploy/deployer"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/logstore"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/secrets/database"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/source/connection"
	"github.com/santapong/cooker/internal/source/gitea"
	"github.com/santapong/cooker/internal/source/gittransport"
	"github.com/santapong/cooker/internal/store"
	storepg "github.com/santapong/cooker/internal/store/postgres"
)

// This is real local Gitea, Git HTTPS checkout, handler/automation logic,
// PostgreSQL queue and Docker deployment. The administrator identity and local
// governance-disabled policy are explicit fixture prerequisites, not OIDC UAT.
func TestGiteaWebhookToDockerLive(t *testing.T) {
	if os.Getenv("COOKER_GITEA_FLOW_ACCEPTANCE") != "true" {
		t.Skip("set COOKER_GITEA_FLOW_ACCEPTANCE=true for disposable full-flow acceptance")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	f := newAutomationLiveFixture(t, ctx)
	gin.SetMode(gin.TestMode)
	h := &Handler{}
	router := gin.New()
	router.Use(gin.Recovery())
	type deliveryCapture struct {
		body   []byte
		header http.Header
		status int
	}
	captured := make(chan deliveryCapture, 4)
	router.POST("/webhooks/connections/:id", func(c *gin.Context) {
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, 2<<20))
		if err != nil {
			c.AbortWithStatus(400)
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		h.SourceWebhook(c)
		if c.Request.Header.Get("X-Gitea-Event") == "push" {
			select {
			case captured <- deliveryCapture{body: body, header: c.Request.Header.Clone(), status: c.Writer.Status()}:
			default:
			}
		}
	})
	adminKey := uuid.NewString()
	admin := router.Group("/api/v1", func(c *gin.Context) {
		if c.GetHeader("X-Fixture-Admin") == adminKey {
			c.Set("user", &auth.Claims{Subject: "fixture-admin", Roles: []string{"admin"}, ACR: "fixture-mfa"})
		}
		c.Next()
	}, auth.RequireRole(auth.RoleAdmin), auth.RequireMFA("fixture-mfa"))
	admin.POST("/admin/source-connections", h.CreateSourceConnection)
	admin.POST("/apps", h.CreateApp)
	admin.POST("/admin/trigger-rules/preview", h.PreviewTriggerRule)
	admin.POST("/admin/trigger-rules", h.SaveTriggerRule)
	cooker := httptest.NewTLSServer(router)
	defer cooker.Close()
	cookerCA := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cooker.Certificate().Raw})
	caFile := filepath.Join(t.TempDir(), "receiver-ca.pem")
	if err := os.WriteFile(caFile, cookerCA, 0644); err != nil {
		t.Fatal(err)
	}
	f.request(cooker.Client(), "POST", cooker.URL+"/api/v1/admin/source-connections", nil, nil, 401, nil)
	// Host networking is used only to let this disposable provider reach the
	// loopback TLS receiver. Its own HTTP/SSH/public listeners are constrained.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listenAddr := listener.Addr().String()
	_, port, _ := net.SplitHostPort(listenAddr)
	listener.Close()
	image := "gitea/gitea:1.26.4-rootless"
	f.docker("image", "inspect", "--format", "{{.Id}}", image)
	giteaID := f.container("--name", f.project+"-gitea", "--network", "host", "--cpus", "2", "--memory", "768m", "--mount", "type=bind,source="+caFile+",target=/tmp/cooker-receiver-ca.pem,readonly",
		"-e", "SSL_CERT_FILE=/tmp/cooker-receiver-ca.pem", "-e", "GITEA__database__DB_TYPE=sqlite3", "-e", "GITEA__security__INSTALL_LOCK=true", "-e", "GITEA__service__DISABLE_REGISTRATION=true",
		"-e", "GITEA__server__HTTP_ADDR=127.0.0.1", "-e", "GITEA__server__HTTP_PORT="+port, "-e", "GITEA__server__ROOT_URL=http://"+listenAddr+"/", "-e", "GITEA__server__DISABLE_SSH=true",
		"-e", "GITEA__webhook__ALLOWED_HOST_LIST=127.0.0.1", "-e", "GITEA__webhook__SKIP_TLS_VERIFY=false", "-e", "GITEA__log__LEVEL=Error", image)
	base := "http://" + listenAddr
	setupClient := &http.Client{Timeout: 10 * time.Second}
	f.wait("Gitea ready", func() bool {
		resp, err := setupClient.Get(base + "/api/v1/version")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode == 200
	})
	password := "fixture-" + uuid.NewString()
	user := "fixture"
	f.docker("exec", giteaID, "gitea", "admin", "user", "create", "--username", user, "--password", password, "--email", "fixture@example.invalid", "--admin", "--must-change-password=false")
	setupHeaders := map[string]string{"Authorization": fixtureBasic(user, password)}
	providerRequest := func(method, path string, payload any, want int, result any) []byte {
		return f.request(setupClient, method, base+"/api/v1"+path, payload, setupHeaders, want, result)
	}
	var providerVersion struct {
		Version string `json:"version"`
	}
	providerRequest("GET", "/version", nil, 200, &providerVersion)
	providerRequest("POST", "/user/repos", map[string]any{"name": "repo", "private": true, "auto_init": true, "default_branch": "main"}, 201, nil)
	redisDigest := f.docker("image", "inspect", "--format", "{{index .RepoDigests 0}}", "redis:7-alpine")
	if !strings.Contains(redisDigest, "@sha256:") {
		t.Fatal("Redis image has no immutable digest")
	}
	compose := "services:\n  redis:\n    image: " + redisDigest + "\n    healthcheck:\n      test: [CMD, redis-cli, ping]\n      interval: 1s\n      timeout: 1s\n      retries: 30\n"
	var composeCommit struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	providerRequest("POST", "/repos/fixture/repo/contents/compose.yaml", map[string]any{"content": base64.StdEncoding.EncodeToString([]byte(compose)), "branch": "main", "message": "reviewed Redis configuration"}, 201, &composeCommit)
	var pat struct {
		SHA1 string `json:"sha1"`
	}
	providerRequest("POST", "/users/fixture/tokens", map[string]any{"name": "source-flow", "scopes": []string{"read:repository"}}, 201, &pat)
	if pat.SHA1 == "" {
		t.Fatal("provider fixture PAT missing")
	}
	upstream, _ := url.Parse(base)
	sourceProxy := httptest.NewTLSServer(httputil.NewSingleHostReverseProxy(upstream))
	defer sourceProxy.Close()
	sourceCA := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: sourceProxy.Certificate().Raw}))
	transport := gittransport.Config{Origin: sourceProxy.URL, APIOrigin: sourceProxy.URL, AllowPrivateNetwork: true, CAPEM: sourceCA}
	anonymous, err := gitea.NewClient(transport, gittransport.Credentials{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := anonymous.Repository(ctx, "fixture/repo"); err == nil {
		t.Fatal("fixture repository was not private")
	}
	// Install current schema in a separate owned PostgreSQL; no direct trigger
	// rules, intents, hashes or jobs are seeded.
	dsn := f.postgres()
	st, err := storepg.NewStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	key, err := cookercrypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	codec, err := cookercrypto.NewCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	connections := &connection.Service{Store: &connection.Postgres{DB: db}, Codec: codec}
	source := &service.SourceAccess{Connections: connections}
	logs := logstore.NewPostgres(db, logstore.Limits{})
	executor := service.NewExecutor(service.WithComposeDeployer(deployer.NewCompose()), service.WithLogStore(logs), service.WithRunUpdater(func(ctx context.Context, r *model.PipelineRun) error {
		return st.Runs.UpdateProgress(ctx, r.ID, r.StageRuns)
	}))
	d := service.NewAppDeployer(executor, "")
	d.Apps, d.Deploys = st.Apps, st.AppDeploys
	d.SourceCheckout = source.Checkout
	d.SaveRun = st.Runs.Update
	d.EnvResolver = &service.AppEnvResolver{Environments: st.Environments, Secrets: database.New(st.Environments, codec)}
	d.SavePipeline = func(ctx context.Context, p *model.Pipeline) error {
		old, err := st.Pipelines.Get(ctx, p.ID)
		if errors.Is(err, store.ErrNotFound) {
			return st.Pipelines.Create(ctx, p)
		}
		if err != nil {
			return err
		}
		p.Version = old.Version
		return st.Pipelines.Update(ctx, p)
	}
	d.CheckExecution = func(app *model.App, builds bool) error {
		if builds || app.DeployTarget.Kind != model.DeployTargetDockerHost {
			return flowError("fixture permits only existing Docker images")
		}
		return nil
	}
	detector := service.NewAppDetector()
	detector.SourceCheckout = source.Checkout
	detector.CheckExecution = d.CheckExecution
	jobs := jobqueue.NewPostgres(db)
	jobs.OnTransition = storepg.SyncJobRun
	jobs.OnEnqueue = storepg.SyncTriggerAdmission
	q := &service.AppDeployQueue{Jobs: jobs, Store: st, Deployer: d, ResolveSource: source.ResolveApp, RequireImagePins: true, Deadline: 2 * time.Minute}
	q.PrepareImages = func(ctx context.Context, p *service.AppDeployPlan) (*service.AppImageResolution, error) {
		return d.PrepareImages(ctx, p, service.RegistryImageResolver(nil))
	}
	a := &service.Automation{Triggers: &service.TriggerService{Store: storepg.NewTriggerStore(db)}, Connections: connections, Store: st, Queue: q, Source: source, Inspect: detector.InspectAutomation, ResolveEnvironment: d.EnvResolver.Resolve}
	a.Authorize = func(_ context.Context, r *model.TriggerRule, _ *model.App) error {
		if !r.Enabled || r.ActorSubject != "cooker:trigger:"+r.ID || len(r.ActorGroups) != 0 {
			return flowError("fixture machine authority rejected")
		}
		return nil
	}
	q.ValidateTrigger = a.ValidateQueued
	h.Store, h.Codec, h.Secrets, h.SourceConnections, h.Automation, h.AppDeployer, h.AppDeployQueue, h.Jobs = st, codec, d.EnvResolver.Secrets, connections, a, d, q, jobs
	apiHeaders := map[string]string{"X-Fixture-Admin": adminKey}
	webhookSecret := "fixture-webhook-" + uuid.NewString()
	var conn connection.Connection
	public := f.request(cooker.Client(), "POST", cooker.URL+"/api/v1/admin/source-connections", map[string]any{"name": "Private Gitea fixture", "provider": "gitea", "origin": sourceProxy.URL, "apiOrigin": sourceProxy.URL, "authMode": "token", "username": user, "token": pat.SHA1, "webhookSecret": webhookSecret, "repositories": []string{"fixture/repo"}, "enabled": true, "allowPrivateNetwork": true, "caPEM": sourceCA}, apiHeaders, 201, &conn)
	if bytes.Contains(public, []byte(pat.SHA1)) || bytes.Contains(public, []byte(webhookSecret)) {
		t.Fatal("connection API displayed credentials")
	}
	var app model.App
	f.request(cooker.Client(), "POST", cooker.URL+"/api/v1/apps", &model.App{Name: f.project, GitHubRepo: "fixture/repo", Branch: "main", BuildPlan: &model.BuildPlan{Kind: model.BuildPlanCompose, Path: "compose.yaml", SourceConnectionID: conn.ID, EnvironmentScope: service.ServiceEnvironmentScope, ServiceEnvironment: map[string][]string{}}, DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost, Prefix: f.project}}, apiHeaders, 201, &app)
	rule := model.TriggerRule{ID: uuid.NewString(), Name: "main to local Redis", AppID: app.ID, Enabled: true, Source: model.GitSourceIdentity{ConnectionID: conn.ID}, RefKind: "branch", Match: "exact", Include: []string{"main"}, Approval: "automatic"}
	review := triggerRequest{Rule: rule, Ref: "refs/heads/main"}
	var preview service.TriggerPreview
	f.request(cooker.Client(), "POST", cooker.URL+"/api/v1/admin/trigger-rules/preview", review, apiHeaders, 200, &preview)
	if preview.Envelope.Commit != composeCommit.Commit.SHA || preview.Rule.PlanHash == "" || preview.Envelope.Hash == "" {
		t.Fatal("real source preview missing reviewed identity")
	}
	f.request(cooker.Client(), "POST", cooker.URL+"/api/v1/admin/trigger-rules", review, apiHeaders, 409, nil)
	review.Rule = *preview.Rule
	review.ExpectedPlanHash = preview.Rule.PlanHash
	review.ExpectedEnvelopeHash = preview.Envelope.Hash
	var saved model.TriggerRule
	f.request(cooker.Client(), "POST", cooker.URL+"/api/v1/admin/trigger-rules", review, apiHeaders, 200, &saved)
	if saved.ActorSubject != "cooker:trigger:"+rule.ID || saved.Version < 1 {
		t.Fatal("saved rule machine authority missing")
	}
	var hook struct {
		ID int64 `json:"id"`
	}
	providerRequest("POST", "/repos/fixture/repo/hooks", map[string]any{"type": "gitea", "config": map[string]string{"url": cooker.URL + "/webhooks/connections/" + conn.ID, "content_type": "json", "secret": webhookSecret}, "events": []string{"push"}, "active": true, "branch_filter": "main"}, 201, &hook)
	var change struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	providerRequest("POST", "/repos/fixture/repo/contents/marker.txt", map[string]any{"content": base64.StdEncoding.EncodeToString([]byte("application source changed; deployment configuration unchanged\n")), "branch": "main", "message": "trigger reviewed deployment"}, 201, &change)
	var delivery deliveryCapture
	select {
	case delivery = <-captured:
	case <-time.After(45 * time.Second):
		t.Fatal("actual Gitea push webhook did not reach loopback TLS receiver")
	}
	if delivery.status != 202 || delivery.header.Get("X-Gitea-Signature") == "" {
		t.Fatalf("provider webhook status=%d", delivery.status)
	}
	actualID := delivery.header.Get("X-Gitea-Delivery")
	deliveryID := service.TriggerDeliveryID("gitea", conn.ID, actualID)
	recorded, err := a.Triggers.Store.GetDelivery(ctx, deliveryID)
	if err != nil {
		t.Fatal(err)
	}
	if recorded.Event.CommitSHA != change.Commit.SHA || len(recorded.Decisions) != 1 || recorded.Decisions[0].IntentID == "" {
		t.Fatal("authenticated event did not produce exact revision intent")
	}
	intent, err := a.Triggers.Store.GetIntent(ctx, recorded.Decisions[0].IntentID)
	if err != nil || intent.Status != model.TriggerPending {
		t.Fatal("delivery did not persist pending intent", err)
	}
	// Replay the actual provider bytes, first with an invalid signature. A
	// rejected retry cannot bypass authentication via an existing inbox ID.
	replay := func(signature string, want int) {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, "POST", cooker.URL+"/webhooks/connections/"+conn.ID, bytes.NewReader(delivery.body))
		req.Header = delivery.header.Clone()
		req.Header.Set("X-Gitea-Signature", signature)
		resp, err := cooker.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		if resp.StatusCode != want {
			t.Fatalf("webhook replay expected %d got %d", want, resp.StatusCode)
		}
	}
	replay(strings.Repeat("0", 64), 401)
	replay(delivery.header.Get("X-Gitea-Signature"), 202)
	workersCtx, stop := context.WithCancel(ctx)
	automationDone := make(chan struct{})
	go func() { defer close(automationDone); a.Run(workersCtx) }()
	registry := jobqueue.NewRegistry()
	registry.Register(service.JobKindAppDeploy, q.Handle)
	pool, err := jobqueue.NewPool(jobqueue.PoolOptions{Store: jobs, Registry: registry, Size: 1, PollEvery: 50 * time.Millisecond, HeartbeatEvery: time.Second, LeaseTimeout: 10 * time.Second, OnHeartbeat: service.HeartbeatJobRun(st), OnRecovery: service.RecoverJobRun(st), ClaimContext: func(ctx context.Context, j *jobqueue.Job) context.Context {
		return store.WithRunClaim(ctx, j.ID, j.LockedBy, nil)
	}})
	if err != nil {
		stop()
		<-automationDone
		t.Fatal(err)
	}
	poolDone := make(chan error, 1)
	go func() { poolDone <- pool.Run(workersCtx) }()
	defer func() { stop(); <-poolDone; <-automationDone }()
	var completed *jobqueue.Job
	f.wait("durable Gitea intent deployment", func() bool {
		current, err := a.Triggers.Store.GetIntent(ctx, intent.ID)
		if err != nil {
			return false
		}
		if current.Status == model.TriggerAwaitingReview || current.Status == model.TriggerFailedPreparation {
			t.Fatalf("intent requires review: %s", current.Reason)
		}
		if current.Receipt == nil {
			return false
		}
		intent = current
		completed, err = jobs.Get(ctx, current.Receipt.JobID)
		return err == nil && completed.Status != jobqueue.StatusPending && completed.Status != jobqueue.StatusRunning
	})
	if completed.Status != jobqueue.StatusSucceeded {
		t.Fatalf("real deployment outcome=%s reason=%s", completed.Status, completed.LastError)
	}
	run, err := st.Runs.Get(ctx, intent.RunID)
	if err != nil || run.Status != model.RunStatusSuccess || run.StartedByUserSub != saved.ActorSubject {
		t.Fatal("durable run/actor mismatch", err)
	}
	var plan service.AppDeployPlan
	if err := json.Unmarshal(completed.Payload, &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Commit != change.Commit.SHA || plan.Trigger == nil || plan.Trigger.IntentID != intent.ID || plan.Images == nil {
		t.Fatal("immutable trigger/source/image plan missing")
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM jobs").Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate webhook created jobs=%d err=%v", count, err)
	}
	runtime := &service.RuntimeService{Store: st, SecretGenerationBound: true}
	status, err := runtime.Status(ctx, &app, "redis")
	if err != nil || !status.Healthy || !strings.Contains(status.Image, "@sha256:") {
		t.Fatal("deployed Redis did not report pinned healthy image", err)
	}
	if f.docker("exec", f.project+"-redis-1", "redis-cli", "ping") != "PONG" {
		t.Fatal("Redis did not execute")
	}
	logCtx, logCancel := context.WithTimeout(ctx, time.Second)
	var runtimeLogs bytes.Buffer
	_ = runtime.StreamDeploymentLogs(logCtx, &app, "redis", jobs, d.EnvResolver, &runtimeLogs)
	logCancel()
	if runtimeLogs.Len() == 0 {
		t.Fatal("actual deployed service logs unavailable")
	}
	reopened, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	entries, err := logstore.NewPostgres(reopened, logstore.Limits{}).Read(ctx, intent.RunID, "deploy-redis", 0)
	if err != nil || len(entries) == 0 {
		t.Fatal("durable stage replay unavailable", err)
	}
	for _, secret := range []string{pat.SHA1, webhookSecret, password} {
		if bytes.Contains(completed.Payload, []byte(secret)) || strings.Contains(runtimeLogs.String(), secret) {
			t.Fatal("fixture credential leaked into public execution evidence")
		}
	}
	f.report["providerVersion"], f.report["actualProviderWebhook"], f.report["privateRepositoryAnonymousDenied"] = providerVersion.Version, true, true
	f.report["sourceHTTPSCustomCATrust"], f.report["webhookHTTPSCustomCATrust"], f.report["sourceCommit"], f.report["redisImage"] = true, true, change.Commit.SHA, redisDigest
	f.report["ruleId"], f.report["intentId"], f.report["runId"], f.report["jobId"] = saved.ID, intent.ID, intent.RunID, completed.ID
	f.report["realPreviewSaveHashes"], f.report["missingReviewRejected"], f.report["invalidSignatureReplayRejected"], f.report["duplicateDeliverySingleJob"] = true, true, true, true
	f.report["healthyDockerRedisPong"], f.report["durableStageReplay"], f.report["immutableContainerLogs"] = true, true, true
	f.report["seededPrerequisites"] = []string{"Disposable Gitea administrator and private repository via provider API", "Generated read-only repository token and webhook secret", "Fixture authenticated admin+MFA claims; no real OIDC session", "Governance-disabled machine actor policy matching server checks", "Fresh PostgreSQL schema; no rules, intents, plans, hashes or jobs seeded"}
	t.Logf("verified Gitea %s private HTTPS source -> actual signed HTTPS webhook -> reviewed rule -> durable intent/job -> healthy digest-pinned Redis; delivery=%s intent=%s run=%s", providerVersion.Version, actualID, intent.ID, intent.RunID)
}
