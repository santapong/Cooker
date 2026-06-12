package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/source/github"
	"github.com/santapong/cooker/internal/validate"
)

// validateAppInput rejects malformed App payloads.
func validateAppInput(a *model.App) error {
	if err := validate.Name("name", a.Name); err != nil {
		return err
	}
	if err := validate.Description("description", a.Description); err != nil {
		return err
	}
	if err := validate.GitHubRepo(a.GitHubRepo); err != nil {
		return err
	}
	if err := validate.GitRefName("branch", a.Branch); err != nil {
		return err
	}
	// Canary config (OR-1): normalise first so omitted optional fields get
	// defaults, then reject out-of-range values (weight outside 1–99, an
	// unknown strategy). Persisted in the same request, satisfying the
	// "new request field => migration" rule (apps.canary_config, mig 024).
	a.Canary = a.Canary.Normalize()
	if err := a.Canary.Validate(); err != nil {
		return err
	}
	return nil
}

// ListApps returns all apps with webhook secrets redacted.
func (h *Handler) ListApps(c *gin.Context) {
	apps, err := h.Store.Apps.List(c.Request.Context())
	if abortStoreErr(c, err, "apps not found") {
		return
	}
	out := make([]*model.App, 0, len(apps))
	for _, a := range apps {
		out = append(out, a.Redact())
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) GetApp(c *gin.Context) {
	a, err := h.Store.Apps.Get(c.Request.Context(), c.Param("id"))
	if abortStoreErr(c, err, "app not found") {
		return
	}
	// Embed the live canary state (OR-1) when one is in flight so the
	// detail page renders the canary panel on first load without a second
	// round trip. Absent / errored canary state is simply omitted — it
	// must never block the app GET.
	if canary := h.activeCanary(c.Request.Context(), a.ID); canary != nil {
		redacted := a.Redact()
		c.JSON(http.StatusOK, appWithCanary{App: redacted, ActiveCanary: canary})
		return
	}
	c.JSON(http.StatusOK, a.Redact())
}

// appWithCanary is the GetApp response shape when a canary is active. It
// embeds *model.App so every existing field (including the canary
// *config* under "canary") serialises unchanged, and adds the live
// rollout *state* under "activeCanary". The two are distinct: "canary"
// is the policy, "activeCanary" is the in-flight progress.
type appWithCanary struct {
	*model.App
	ActiveCanary *model.AppCanary `json:"activeCanary"`
}

// activeCanary returns the app's progressing canary, or nil when none is
// in flight or the canary service isn't wired. Errors other than
// "no active canary" are swallowed — canary state is additive and must
// not fail the app read.
func (h *Handler) activeCanary(ctx context.Context, appID string) *model.AppCanary {
	if h.Canary == nil {
		return nil
	}
	canary, err := h.Canary.Status(ctx, appID)
	if err != nil {
		return nil
	}
	return canary
}

func (h *Handler) CreateApp(c *gin.Context) {
	var a model.App
	if err := c.ShouldBindJSON(&a); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateAppInput(&a); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if a.Branch == "" {
		a.Branch = "main"
	}
	// Secrets are set via PUT /apps/:id/webhook — never on Create.
	a.WebhookSecret = nil
	a.ID = uuid.New().String()
	now := time.Now()
	a.CreatedAt, a.UpdatedAt = now, now
	if err := h.Store.Apps.Create(c.Request.Context(), &a); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, a.Redact())
}

func (h *Handler) UpdateApp(c *gin.Context) {
	id := c.Param("id")
	existing, err := h.Store.Apps.Get(c.Request.Context(), id)
	if abortStoreErr(c, err, "app not found") {
		return
	}
	var a model.App
	if err := c.ShouldBindJSON(&a); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateAppInput(&a); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	a.ID = id
	a.CreatedAt = existing.CreatedAt
	a.WebhookSecret = existing.WebhookSecret // keep; rotate via dedicated endpoint
	a.UpdatedAt = time.Now()
	if err := h.Store.Apps.Update(c.Request.Context(), &a); err != nil {
		if abortStoreErr(c, err, "app not found") {
			return
		}
	}
	c.JSON(http.StatusOK, a.Redact())
}

func (h *Handler) DeleteApp(c *gin.Context) {
	if err := h.Store.Apps.Delete(c.Request.Context(), c.Param("id")); err != nil {
		if abortStoreErr(c, err, "app not found") {
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// SetAppWebhookSecret rotates the HMAC secret GitHub will send as
// X-Hub-Signature-256. The value is sealed with the handler's codec
// before it hits the store. Admin only.
func (h *Handler) SetAppWebhookSecret(c *gin.Context) {
	if !h.requireCodec(c) {
		return
	}
	claims := auth.GetUser(c)
	if !auth.CanRevealSecret(claims) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin role required"})
		return
	}
	var req struct {
		Secret string `json:"secret" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	a, err := h.Store.Apps.Get(c.Request.Context(), c.Param("id"))
	if abortStoreErr(c, err, "app not found") {
		return
	}
	sealed, err := h.Codec.Seal([]byte(req.Secret))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "seal: " + err.Error()})
		return
	}
	a.WebhookSecret = sealed
	a.UpdatedAt = time.Now()
	if err := h.Store.Apps.Update(c.Request.Context(), a); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "rotated"})
}

// DeployApp synthesises a Clone→Build→Push→Deploy run for the App
// and streams progress over the app-run:<runId> WebSocket channel.
// Returns immediately with a 202 and the run ID; the run executes
// in a background goroutine bounded by a fresh context (the HTTP
// request context would cancel as soon as we reply).
func (h *Handler) DeployApp(c *gin.Context) {
	a, err := h.Store.Apps.Get(c.Request.Context(), c.Param("id"))
	if abortStoreErr(c, err, "app not found") {
		return
	}
	if h.AppDeployer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "app deployer not configured",
		})
		return
	}

	// A run ID the client can subscribe to before the work starts.
	runID := uuid.New().String()
	channel := "app-run:" + runID

	// F-07 (store-parity audit): create a stub run row with
	// status=running *before* calling Spawn so that:
	//  (a) RunCoordinator.Spawn's first heartbeat (runs.go:78) finds
	//      the row and succeeds instead of silently no-oping against a
	//      missing row (runs.go:111).
	//  (b) If the process crashes mid-deploy the boot-time orphan sweep
	//      (SweepOrphans) can see and reap the stale row instead of
	//      silently losing the run.
	// The row is Updated with the final status at the end of
	// runAppDeployCtx. Mirrors the pipeline-run path in RunPipeline.
	// See docs/audits/2026-05-store-parity.md §F-07 and
	// internal/server/runs.go:78 (first heartbeat) and :111 (silent swallow).
	if h.Runs != nil && h.Store != nil {
		now := time.Now()
		stub := &model.PipelineRun{
			ID:         runID,
			PipelineID: a.ID,
			Status:     model.RunStatusRunning,
			StartedAt:  &now,
		}
		if createErr := h.Store.Runs.Create(c.Request.Context(), stub); createErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "create run: " + createErr.Error()})
			return
		}
	}

	// Canary path (OR-1): when the app opts into canary deploys and the
	// canary service is wired, the Deploy button starts a weighted canary
	// instead of a rolling replace. The worker mirrors runAppDeployCtx
	// (stub row + WS channel) but drives CanaryService.Start.
	canary := a.Canary.Normalize().IsCanary() && h.Canary != nil
	work := func(ctx context.Context) error {
		if canary {
			h.runCanaryStartCtx(ctx, a, runID, channel)
		} else {
			h.runAppDeployCtx(ctx, a, runID, channel)
		}
		return nil
	}
	if h.Runs != nil {
		h.Runs.Spawn(context.Background(), runID, work)
	} else if canary {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			_ = work(ctx)
		}()
	} else {
		go h.runAppDeploy(a, runID, channel)
	}

	deployStrategy := "rolling"
	if canary {
		deployStrategy = "canary"
	}
	c.JSON(http.StatusAccepted, gin.H{
		"appId":    a.ID,
		"runId":    runID,
		"channel":  channel,
		"status":   "running",
		"strategy": deployStrategy,
		"stream":   "/ws/app-run/" + runID,
		"repo":     a.GitHubRepo,
		"branch":   a.Branch,
		// pipelineId is the deterministic ID the compose deploy will
		// persist; the deployment view loads the grouped DAG from it.
		// Meaningful only for compose-based apps (harmless otherwise).
		"pipelineId":     service.ComposePipelineID(runID, a.ID, 0),
		"deploymentView": "/apps/" + a.ID + "/deployments/" + service.ComposePipelineID(runID, a.ID, 0) + "/" + runID,
	})
}

// runAppDeploy is the background worker for the legacy untracked path
// (used when no RunCoordinator is wired). Prefer runAppDeployCtx via
// the coordinator so heartbeats land in the run row.
func (h *Handler) runAppDeploy(a *model.App, runID, channel string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	h.runAppDeployCtx(ctx, a, runID, channel)
}

// runAppDeployCtx is the deploy worker bound to a caller-supplied ctx.
// The RunCoordinator owns the lifetime; this function should not
// install its own deadline so shutdown can cut off the work cleanly.
func (h *Handler) runAppDeployCtx(ctx context.Context, a *model.App, runID, channel string) {
	deployCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	sink := &wsLogSink{channel: channel, broadcast: h.WSBroadcast}
	sink.writef("[start] app=%s repo=%s branch=%s run=%s\n", a.Name, a.GitHubRepo, a.Branch, runID)

	pipeline, run, err := h.AppDeployer.Deploy(deployCtx, a, runID, sink)
	if err != nil {
		sink.writef("[error] %v\n", err)
	}
	// Persist the synthesized pipeline for the grouped compose DAG so the
	// deployment view can fetch its stages/edges/groups. Best-effort:
	// the run is the source of truth, the pipeline is for visualization.
	if pipeline != nil {
		if createErr := h.Store.Pipelines.Create(deployCtx, pipeline); createErr != nil {
			sink.writef("[warn] persist pipeline: %v\n", createErr)
		}
	}
	if run != nil {
		// The stub run row was Created before Spawn (F-07 fix), so
		// Update is always valid here and preserves any heartbeats
		// written by the coordinator during the deploy.
		if updateErr := h.Store.Runs.Update(deployCtx, run); updateErr != nil {
			sink.writef("[warn] persist run: %v\n", updateErr)
		}
		sink.writef("[final] status=%s\n", run.Status)
	}
	sink.writef("[end] run=%s\n", runID)
}

// wsLogSink writes log lines as WebSocket messages on the given
// channel. Zero-value broadcast drops writes so the deployer can
// run in contexts without a hub (tests).
type wsLogSink struct {
	channel   string
	broadcast func(channel string, data []byte)
}

func (w *wsLogSink) Write(p []byte) (int, error) {
	if w.broadcast != nil && w.channel != "" {
		w.broadcast(w.channel, append([]byte(nil), p...))
	}
	return len(p), nil
}

func (w *wsLogSink) writef(format string, args ...any) {
	_, _ = w.Write([]byte(fmt.Sprintf(format, args...)))
}

// GitHubWebhook receives push events from GitHub and, for Apps with
// a matching repo+branch and AutoDeploy=true, enqueues a deploy.
//
// Route: POST /webhooks/github (unauthenticated — HMAC is the
// authentication).
func (h *Handler) GitHubWebhook(c *gin.Context) {
	if !h.requireCodec(c) {
		return
	}
	// GitHub's documented webhook payload cap is 25 MiB; we set a
	// hard 10 MiB limit because cooker only consumes push events
	// where realistic payloads are much smaller (a hundred-commit
	// push is around 200 KiB). Reading a 1 GiB body unbounded was
	// the simplest path to OOM-killing the pod.
	const maxWebhookBody = 10 << 20
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxWebhookBody+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "read body"})
		return
	}
	if len(body) > maxWebhookBody {
		c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
			"error": "payload exceeds limit",
		})
		return
	}

	// GitHub only sends push events that matter for auto-deploy. We
	// look up the App by repository first to find the secret.
	if c.GetHeader("X-GitHub-Event") != "push" {
		c.JSON(http.StatusOK, gin.H{"ignored": c.GetHeader("X-GitHub-Event")})
		return
	}

	var ev github.PushEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parse: " + err.Error()})
		return
	}
	branch := ev.Branch()
	if branch == "" {
		// Tag pushes etc. — ignore.
		c.JSON(http.StatusOK, gin.H{"ignored": "non-branch push"})
		return
	}
	if ev.IsBranchDelete() {
		// Branch was deleted (`after` is all zeros / `deleted: true`).
		// Nothing to deploy — and pushing through to GetByRepo would
		// surface a misleading "deploy queued" response.
		c.JSON(http.StatusOK, gin.H{"ignored": "branch delete"})
		return
	}

	app, err := h.Store.Apps.GetByRepo(c.Request.Context(), ev.Repository.FullName, branch)
	if err != nil {
		// No matching App — nothing to do, but don't leak that the
		// repo is unknown. Return 204 so GitHub keeps retrying on
		// real errors.
		c.Status(http.StatusNoContent)
		return
	}

	if len(app.WebhookSecret) == 0 {
		c.JSON(http.StatusForbidden, gin.H{"error": "webhook not configured for this app"})
		return
	}
	secret, err := h.Codec.Open(app.WebhookSecret)
	if err != nil {
		slog.Warn("github webhook: failed to open secret", "app", app.ID, "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	sig := c.GetHeader("X-Hub-Signature-256")
	if err := github.VerifySignature(secret, body, sig); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "bad signature"})
		return
	}

	if !app.AutoDeploy {
		c.JSON(http.StatusOK, gin.H{"ignored": "autoDeploy disabled"})
		return
	}

	h.triggerWebhookDeploy(c, app, "github", branch, ev.After)
}

// triggerWebhookDeploy starts a real deploy for a signature-verified
// push to a matching branch of an auto-deploy app. It converges onto
// the exact path the manual "Deploy" button uses (DeployApp): a stub
// run row is created before Spawn (F-07: so the coordinator's first
// heartbeat lands and the orphan sweep can reap a crashed run), then
// the deploy executes in a coordinator-owned goroutine via
// runAppDeployCtx → AppDeployer.Deploy. Because the AppDeployer is
// configured with the AppDeploys store, the terminal run is recorded
// in app_deploys (migration 018) exactly like a manual deploy.
//
// The deploy is attributed to the webhook via the run's StartedByEmail
// ("webhook:<source>", e.g. "webhook:github"); StartedByUserSub is left
// empty, which the deploy-stage governance hook reads as "no human
// actor to gate" — matching the manual DeployApp stub, which also omits
// it. Orchestration lives here in the handler's service-adjacent helper
// rather than being duplicated in each provider's webhook; the response
// is the same 202 the webhooks have always returned, so the
// idempotency middleware (keyed on X-GitHub-Delivery etc.) keeps
// replaying it on redelivery and no double-deploy occurs.
//
// source is the provider slug ("github", "gitlab", "gitea",
// "bitbucket"). commit may be empty (Bitbucket Server push payloads
// don't always carry a top-level SHA); it's echoed in the response for
// operator triage only and never drives the deploy.
func (h *Handler) triggerWebhookDeploy(c *gin.Context, app *model.App, source, branch, commit string) {
	if h.AppDeployer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "app deployer not configured"})
		return
	}

	runID := uuid.New().String()
	channel := "app-run:" + runID

	// Stub run row before Spawn — identical F-07 rationale as DeployApp
	// and RollbackApp. Attribute the run to the webhook so deploy history
	// and any audit view can tell auto-deploys from manual clicks.
	if h.Runs != nil && h.Store != nil {
		now := time.Now()
		stub := &model.PipelineRun{
			ID:             runID,
			PipelineID:     app.ID,
			Status:         model.RunStatusRunning,
			StartedAt:      &now,
			StartedByEmail: "webhook:" + source,
		}
		if createErr := h.Store.Runs.Create(c.Request.Context(), stub); createErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "create run: " + createErr.Error()})
			return
		}
	}

	if h.Runs != nil {
		h.Runs.Spawn(context.Background(), runID, func(ctx context.Context) error {
			h.runAppDeployCtx(ctx, app, runID, channel)
			return nil
		})
	} else {
		go h.runAppDeploy(app, runID, channel)
	}

	resp := gin.H{
		"appId":   app.ID,
		"runId":   runID,
		"branch":  branch,
		"channel": channel,
		"status":  "running",
		"stream":  "/ws/app-run/" + runID,
		// Same deterministic compose-deploy ID DeployApp returns, so the
		// deployment view can load the grouped DAG (harmless for non-compose).
		"pipelineId":     service.ComposePipelineID(runID, app.ID, 0),
		"deploymentView": "/apps/" + app.ID + "/deployments/" + service.ComposePipelineID(runID, app.ID, 0) + "/" + runID,
	}
	if commit != "" {
		resp["commit"] = commit
	}
	c.JSON(http.StatusAccepted, resp)
}

// DetectAppBuild shallow-clones a repo the user is about to import and
// returns the detected build plan plus a suggested wizard recipe. The
// App doesn't exist yet, so this takes the repo coordinates directly.
func (h *Handler) DetectAppBuild(c *gin.Context) {
	if h.AppDetector == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "build detection not available"})
		return
	}
	var req struct {
		GitHubRepo string `json:"githubRepo"`
		Branch     string `json:"branch"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validate.GitHubRepo(req.GitHubRepo); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validate.GitRefName("branch", req.Branch); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	plan, recipe, err := h.AppDetector.DetectBuild(c.Request.Context(), req.GitHubRepo, req.Branch)
	if err != nil {
		// Clone failures (private repo, typo, network) are a property of
		// the user's input, not a server fault: 422 with the reason.
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": fmt.Sprintf("clone failed: %v", err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"plan":            plan,
		"suggestedRecipe": recipe,
	})
}

// ListAppDeploys returns the app's deploy/rollback history,
// newest-first (roadmap M3).
func (h *Handler) ListAppDeploys(c *gin.Context) {
	a, err := h.Store.Apps.Get(c.Request.Context(), c.Param("id"))
	if abortStoreErr(c, err, "app not found") {
		return
	}
	if h.Store.AppDeploys == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "deploy history not available"})
		return
	}
	limit := 20
	if v := c.Query("limit"); v != "" {
		if n, perr := strconv.Atoi(v); perr == nil && n > 0 {
			limit = n
		}
	}
	if limit > 100 {
		limit = 100
	}
	deploys, err := h.Store.AppDeploys.ListByApp(c.Request.Context(), a.ID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if deploys == nil {
		deploys = []*model.AppDeploy{}
	}
	c.JSON(http.StatusOK, gin.H{"deploys": deploys})
}

// resolveRollbackTarget picks the history row a rollback re-deploys.
// Explicit deployId wins (must belong to the app, be successful, and
// carry an image ref). Default: the SECOND most-recent successful
// deploy-kind row with an image — i.e. "the version before the one
// we're on".
func (h *Handler) resolveRollbackTarget(c *gin.Context, appID, deployID string) (*model.AppDeploy, bool) {
	if deployID != "" {
		d, err := h.Store.AppDeploys.Get(c.Request.Context(), deployID)
		if abortStoreErr(c, err, "deploy not found") {
			return nil, false
		}
		if d.AppID != appID {
			c.JSON(http.StatusNotFound, gin.H{"error": "deploy not found"})
			return nil, false
		}
		if d.Status != model.RunStatusSuccess || d.ImageRef == "" {
			c.JSON(http.StatusConflict, gin.H{"error": "deploy is not a successful single-image deploy; cannot roll back to it"})
			return nil, false
		}
		return d, true
	}
	history, err := h.Store.AppDeploys.ListByApp(c.Request.Context(), appID, 50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return nil, false
	}
	var candidates []*model.AppDeploy
	for _, d := range history {
		if d.Kind == model.AppDeployKindDeploy && d.Status == model.RunStatusSuccess && d.ImageRef != "" {
			candidates = append(candidates, d)
		}
	}
	if len(candidates) < 2 {
		c.JSON(http.StatusNotFound, gin.H{"error": "no earlier successful deploy to roll back to"})
		return nil, false
	}
	return candidates[1], true
}

// RollbackApp re-deploys a previously shipped image (roadmap M3).
// Deploy-only: no clone/build/push. v1 supports kubernetes targets
// and single-image (non-compose) history rows.
func (h *Handler) RollbackApp(c *gin.Context) {
	a, err := h.Store.Apps.Get(c.Request.Context(), c.Param("id"))
	if abortStoreErr(c, err, "app not found") {
		return
	}
	if h.AppDeployer == nil || h.Store.AppDeploys == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "rollback not available"})
		return
	}
	if a.DeployTarget.Kind != model.DeployTargetKubernetes {
		c.JSON(http.StatusConflict, gin.H{"error": "rollback supports kubernetes deploy targets only"})
		return
	}

	var req struct {
		DeployID string `json:"deployId"`
	}
	if c.Request.ContentLength > 0 {
		if bindErr := c.ShouldBindJSON(&req); bindErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": bindErr.Error()})
			return
		}
	}
	target, ok := h.resolveRollbackTarget(c, a.ID, req.DeployID)
	if !ok {
		return
	}

	runID := uuid.New().String()
	channel := "app-run:" + runID
	// Stub run row before Spawn — same F-07 rationale as DeployApp.
	if h.Runs != nil && h.Store != nil {
		now := time.Now()
		stub := &model.PipelineRun{
			ID:         runID,
			PipelineID: "app-" + a.ID + "-rollback-" + runID,
			Status:     model.RunStatusRunning,
			StartedAt:  &now,
		}
		if createErr := h.Store.Runs.Create(c.Request.Context(), stub); createErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "create run: " + createErr.Error()})
			return
		}
	}

	work := func(ctx context.Context) error {
		h.runRollbackCtx(ctx, a, target.ImageRef, runID, channel)
		return nil
	}
	if h.Runs != nil {
		h.Runs.Spawn(context.Background(), runID, work)
	} else {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			_ = work(ctx)
		}()
	}

	c.JSON(http.StatusAccepted, gin.H{
		"appId":   a.ID,
		"runId":   runID,
		"channel": channel,
		"status":  "running",
		"stream":  "/ws/app-run/" + runID,
		"rolledBackTo": gin.H{
			"deployId": target.ID,
			"imageRef": target.ImageRef,
		},
	})
}

// runRollbackCtx is the rollback worker; mirrors runAppDeployCtx.
func (h *Handler) runRollbackCtx(ctx context.Context, a *model.App, imageRef, runID, channel string) {
	deployCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	sink := &wsLogSink{channel: channel, broadcast: h.WSBroadcast}
	sink.writef("[start] rollback app=%s image=%s run=%s\n", a.Name, imageRef, runID)

	pipeline, run, err := h.AppDeployer.DeployImage(deployCtx, a, imageRef, runID, sink)
	if err != nil {
		sink.writef("[error] %v\n", err)
	}
	if pipeline != nil {
		if createErr := h.Store.Pipelines.Create(deployCtx, pipeline); createErr != nil {
			sink.writef("[warn] persist pipeline: %v\n", createErr)
		}
	}
	if run != nil {
		if updateErr := h.Store.Runs.Update(deployCtx, run); updateErr != nil {
			sink.writef("[warn] persist run: %v\n", updateErr)
		}
		sink.writef("[final] status=%s\n", run.Status)
	}
	sink.writef("[end] run=%s\n", runID)
}

// GetAppDrift compares the app's last successfully shipped image to
// the live cluster workload (roadmap M3, on-demand v1).
func (h *Handler) GetAppDrift(c *gin.Context) {
	a, err := h.Store.Apps.Get(c.Request.Context(), c.Param("id"))
	if abortStoreErr(c, err, "app not found") {
		return
	}
	var kc service.KubeWorkloadGetter
	if h.Kube != nil {
		kc = h.Kube
	}
	c.JSON(http.StatusOK, service.CheckDrift(c.Request.Context(), kc, h.Store.AppDeploys, a))
}
