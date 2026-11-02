package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/source/github"
	"github.com/santapong/cooker/internal/store"
)

const JobKindAppDeploy = "app-deploy"

// AppDeployPlan is immutable queue input. App contains configuration and secret
// references only; resolved credentials never enter the queue payload.
type AppDeployPlan struct {
	Version            int                          `json:"version"`
	RunID              string                       `json:"runId"`
	App                *model.App                   `json:"app"`
	Commit             string                       `json:"commit"`
	EnvironmentVersion int                          `json:"environmentVersion"`
	CreatedAt          time.Time                    `json:"createdAt"`
	Actor              *model.PipelineRun           `json:"actor,omitempty"`
	Trigger            *TriggerExecution            `json:"trigger,omitempty"`
	Images             *AppImageResolution          `json:"images,omitempty"`
	Secrets            *EnvironmentSecretPlan       `json:"secretVersions,omitempty"`
	Registry           *model.RegistryAuthorityPlan `json:"registryAuthority,omitempty"`
}

type AppDeployReceipt struct {
	RunID      string `json:"runId"`
	JobID      string `json:"jobId"`
	PipelineID string `json:"pipelineId"`
	PlanDigest string `json:"planDigest"`
	Commit     string `json:"commit"`
}

type AppDeployQueue struct {
	Jobs            jobqueue.Store
	Store           *store.Store
	Deployer        *AppDeployer
	ResolveCommit   func(context.Context, int64, string, string) (string, error)
	LogWriter       func(runID string) io.Writer
	Deadline        time.Duration
	ValidateTrigger func(context.Context, *AppDeployPlan) error
	ResolveSource   func(context.Context, *model.App, string) (string, error)
	// Production server wiring requires image evidence. Tests may inject a local
	// preparation function without accessing an external registry.
	RequireImagePins    bool
	RequireSecretPins   bool
	PrepareImages       func(context.Context, *AppDeployPlan) (*AppImageResolution, error)
	RegistryAuthority   *RegistryAuthorityService
	RequireRegistryPins bool
	AdmissionGuard      store.NativeTriggerAdmissionGuard
}

// Enqueue resolves the source before returning accepted, then persists one
// immutable job. The worker can reconstruct its run row after a crash between
// queue acceptance and HTTP run-view creation.
func (q *AppDeployQueue) Enqueue(ctx context.Context, app *model.App, commit string, actor *model.PipelineRun) (*AppDeployReceipt, error) {
	return q.enqueue(ctx, app, commit, actor, uuid.NewString())
}

// EnqueueIntent is idempotent across retries and process restarts. The caller
// supplies a server-issued intent identity, never an unverified webhook key.
func (q *AppDeployQueue) EnqueueIntent(ctx context.Context, app *model.App, commit string, actor *model.PipelineRun, intentID string) (*AppDeployReceipt, error) {
	if app == nil {
		return nil, fmt.Errorf("app is required")
	}
	if intentID == "" {
		return nil, fmt.Errorf("deployment intent is required")
	}
	runID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("cooker-intent:"+intentID)).String()
	if q != nil && q.Jobs != nil {
		existing, err := q.Jobs.Get(ctx, "app-run:"+runID)
		if err == nil {
			return receiptForPlan(existing, app.ID, commit)
		}
		if !errors.Is(err, jobqueue.ErrNotFound) {
			return nil, err
		}
	}
	return q.enqueue(ctx, app, commit, actor, runID)
}

func receiptForPlan(job *jobqueue.Job, appID, commit string) (*AppDeployReceipt, error) {
	var plan AppDeployPlan
	if job == nil {
		return nil, fmt.Errorf("deployment intent conflicts with an accepted plan")
	}
	if err := json.Unmarshal(job.Payload, &plan); err != nil || plan.Version != 1 || plan.RunID == "" || job.ID != "app-run:"+plan.RunID || plan.App == nil || job.Kind != JobKindAppDeploy || plan.App.ID != appID || !github.ValidCommit(plan.Commit) || (commit != "" && plan.Commit != commit) {
		return nil, fmt.Errorf("deployment intent conflicts with an accepted plan")
	}
	digest, err := jobqueue.PayloadDigest(job.Payload)
	if err != nil {
		return nil, err
	}
	return &AppDeployReceipt{RunID: plan.RunID, JobID: job.ID, PipelineID: ComposePipelineID(plan.RunID, appID, 0), PlanDigest: digest, Commit: plan.Commit}, nil
}

func (q *AppDeployQueue) enqueue(ctx context.Context, app *model.App, commit string, actor *model.PipelineRun, runID string) (*AppDeployReceipt, error) {
	if q == nil || q.Jobs == nil {
		return nil, fmt.Errorf("durable app deployment is not configured")
	}
	var receipt *AppDeployReceipt
	work := func(ctx context.Context) error {
		// Original accepted work wins over current configuration, credential
		// rotation or a repeated caller. Never repin an existing receipt.
		if existing, err := q.Jobs.Get(ctx, "app-run:"+runID); err == nil {
			if app == nil {
				return fmt.Errorf("app is required")
			}
			var getErr error
			receipt, getErr = receiptForPlan(existing, app.ID, commit)
			return getErr
		} else if !errors.Is(err, jobqueue.ErrNotFound) {
			return err
		}
		var err error
		receipt, err = q.enqueuePlan(ctx, app, commit, actor, runID)
		return err
	}
	if q.AdmissionGuard != nil {
		if err := q.AdmissionGuard.WithNativeTriggerAdmission(ctx, "app-run:"+runID, work); err != nil {
			return nil, err
		}
	} else {
		if q.RequireRegistryPins {
			return nil, fmt.Errorf("app admission serialization is unavailable")
		}
		if err := work(ctx); err != nil {
			return nil, err
		}
	}
	return receipt, nil
}

func (q *AppDeployQueue) enqueuePlan(ctx context.Context, app *model.App, commit string, actor *model.PipelineRun, runID string) (*AppDeployReceipt, error) {
	if q == nil || q.Jobs == nil || q.Store == nil || q.Deployer == nil {
		return nil, fmt.Errorf("durable app deployment is not configured")
	}
	if err := ValidateAppDeployment(app); err != nil {
		return nil, err
	}
	// Inputs belong to this admission before any injected provider callback.
	var cloneErr error
	app, cloneErr = cloneAppDeploymentInput(app)
	if cloneErr != nil {
		return nil, cloneErr
	}
	if actor != nil {
		actor = actor.Clone()
	}
	if err := ValidateRuntimeEnvironmentBinding(ctx, q.Store.Environments, app.EnvironmentID); err != nil {
		return nil, err
	}
	if app.Canary.Normalize().IsCanary() {
		return nil, fmt.Errorf("durable canary execution is not available yet")
	}
	if err := CheckAppTarget(app); err != nil {
		return nil, err
	}
	if q.Deployer.CheckExecution != nil {
		if err := q.Deployer.CheckExecution(app, false); err != nil {
			return nil, err
		}
	}
	registryPlan, err := q.prepareAppRegistry(ctx, app)
	if err != nil {
		return nil, err
	}
	var scopedEnvironment *model.Environment
	var scopedSecrets *EnvironmentSecretPlan
	if usesDockerfileEnvironment(app.BuildPlan) {
		if app.EnvironmentID != "" {
			scopedEnvironment, err = q.Store.Environments.Get(ctx, app.EnvironmentID)
			if err != nil || q.Deployer.EnvResolver == nil {
				return nil, fmt.Errorf("runtime environment admission is unavailable")
			}
		}
		scopedSecrets, err = q.Deployer.EnvResolver.pinDockerfileEnvironment(ctx, app, scopedEnvironment)
		if err != nil {
			return nil, err
		}
	}
	ref := app.Branch
	if ref == "" {
		ref = "main"
	}
	var installation int64
	if app.BuildPlan != nil {
		installation = app.BuildPlan.InstallationID
		if app.BuildPlan.Commit != "" {
			ref = app.BuildPlan.Commit
		}
	}
	if commit == "" {
		if q.ResolveSource == nil && q.ResolveCommit == nil {
			return nil, fmt.Errorf("source revision resolver is unavailable")
		}
		var err error
		if q.ResolveSource != nil {
			commit, err = q.ResolveSource(ctx, app, ref)
		} else {
			commit, err = q.ResolveCommit(ctx, installation, app.GitHubRepo, ref)
		}
		if err != nil {
			return nil, err
		}
	}
	if !github.ValidCommit(commit) {
		return nil, fmt.Errorf("deployment requires a full source commit SHA")
	}
	// JSON round-trip gives the queued plan independent nested maps and slices.
	b, err := json.Marshal(app.Redact())
	if err != nil {
		return nil, err
	}
	var snapshot model.App
	if err := json.Unmarshal(b, &snapshot); err != nil {
		return nil, err
	}
	snapshot.WebhookSecret = nil
	snapshot.AutoDeploy = false
	plan := AppDeployPlan{Version: 1, RunID: runID, App: &snapshot, Commit: commit, CreatedAt: time.Now().UTC(), Registry: registryPlan}
	if actor != nil {
		plan.Actor = actor.Clone()
	}
	if trigger, ok := ctx.Value(triggerExecutionKey{}).(*TriggerExecution); ok {
		cp := *trigger
		plan.Trigger = &cp
	}
	if app.EnvironmentID != "" {
		if scopedEnvironment != nil {
			plan.EnvironmentVersion = scopedEnvironment.Version
		} else {
			env, err := q.Store.Environments.Get(ctx, app.EnvironmentID)
			if err != nil {
				return nil, fmt.Errorf("cannot snapshot linked environment: %w", err)
			}
			plan.EnvironmentVersion = env.Version
		}
	}
	if plan.App.EnvironmentID != "" {
		if q.Deployer.EnvResolver == nil && q.RequireSecretPins {
			return nil, fmt.Errorf("secret admission is unavailable")
		}
		if usesDockerfileEnvironment(plan.App.BuildPlan) {
			plan.Secrets = scopedSecrets
			if err := q.Deployer.EnvResolver.validateDockerfileSecretEvidence(WithAcceptedSecretPlan(ctx, plan.Secrets), plan.App, scopedEnvironment); err != nil {
				return nil, err
			}
		} else if q.Deployer.EnvResolver != nil {
			plan.Secrets, err = q.Deployer.EnvResolver.PinEnvironmentSecrets(ctx, plan.App.EnvironmentID, plan.EnvironmentVersion, nil, true)
			if err != nil {
				return nil, err
			}
		}
	}
	if q.RequireImagePins && q.PrepareImages == nil {
		return nil, fmt.Errorf("image admission is unavailable")
	}
	if q.PrepareImages != nil {
		plan.Images, err = q.PrepareImages(ctx, &plan)
		if err != nil {
			return nil, err
		}
		if plan.Images == nil || plan.Images.Version != 1 {
			return nil, fmt.Errorf("image admission returned no immutable resolution")
		}
	}
	if plan.Registry != nil {
		if validateAppRegistryPlan(plan.App, plan.Registry) != nil {
			return nil, model.ErrInvalidRegistryAuthority
		}
		if err := q.RegistryAuthority.ValidateCurrent(ctx, plan.Registry.Subject, plan.Registry); err != nil {
			return nil, err
		}
	}
	payload, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	digest, err := jobqueue.PayloadDigest(payload)
	if err != nil {
		return nil, err
	}
	job, err := q.Jobs.Enqueue(ctx, JobKindAppDeploy, payload, jobqueue.EnqueueOptions{ID: "app-run:" + plan.RunID, ConcurrencyKey: "app:" + app.ID, MaxAttempts: 1})
	if err != nil {
		if existing, getErr := q.Jobs.Get(ctx, "app-run:"+plan.RunID); getErr == nil {
			return receiptForPlan(existing, app.ID, commit)
		}
		return nil, fmt.Errorf("cannot durably queue deployment: %w", err)
	}
	// This materializes the view early. Failure cannot undo accepted work;
	// the worker repeats the idempotent operation before execution.
	viewCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = q.ensureRun(viewCtx, &plan)
	return &AppDeployReceipt{RunID: plan.RunID, JobID: job.ID, PipelineID: ComposePipelineID(plan.RunID, app.ID, 0), PlanDigest: digest, Commit: commit}, nil
}

func (q *AppDeployQueue) ensureRun(ctx context.Context, p *AppDeployPlan) error {
	id := ComposePipelineID(p.RunID, p.App.ID, 0)
	graph := &model.Pipeline{ID: id, Name: "Deploy " + p.App.Name, Stages: []model.Stage{}, Edges: []model.Edge{}, CreatedAt: p.CreatedAt, UpdatedAt: p.CreatedAt}
	if err := q.Store.Pipelines.Create(ctx, graph); err != nil && !errors.Is(err, store.ErrConflict) {
		return err
	}
	run := &model.PipelineRun{ID: p.RunID, PipelineID: id, Status: model.RunStatusPending, CreatedAt: p.CreatedAt}
	if p.Actor != nil {
		run.StartedByUserSub, run.StartedByEmail = p.Actor.StartedByUserSub, p.Actor.StartedByEmail
		run.StartedByGroups, run.StartedByTokenHash = p.Actor.StartedByGroups, p.Actor.StartedByTokenHash
	}
	if err := q.Store.Runs.Create(ctx, run); err != nil && !errors.Is(err, store.ErrConflict) {
		return err
	}
	return nil
}

type sourceCommitKey struct{}
type deployActorKey struct{}

func (q *AppDeployQueue) Handle(ctx context.Context, job *jobqueue.Job) error {
	var plan AppDeployPlan
	if err := json.Unmarshal(job.Payload, &plan); err != nil {
		return fmt.Errorf("invalid deployment plan")
	}
	if plan.Version != 1 || plan.App == nil || plan.RunID == "" || !github.ValidCommit(plan.Commit) {
		return fmt.Errorf("invalid deployment plan identity")
	}
	if err := q.ensureRun(ctx, &plan); err != nil {
		return err
	}
	run, err := q.Store.Runs.Get(ctx, plan.RunID)
	if err != nil {
		return err
	}
	if reason, recorded := store.AppPreflightReasonFromRun(run); recorded {
		return q.recordPreflightFailureForReason(ctx, job, run, reason)
	}
	if run.Status == model.RunStatusSuccess || run.Status == model.RunStatusFailed || run.Status == model.RunStatusCancelled {
		return nil
	}
	if run.Status == model.RunStatusNeedsAttention {
		return jobqueue.ErrOutcomeUnknown
	}
	deadline := q.Deadline
	if deadline <= 0 {
		deadline = 30 * time.Minute
	}
	execCtx, cancel := context.WithTimeout(context.WithValue(context.WithValue(WithEnvironmentVersion(ctx, plan.App.EnvironmentID, plan.EnvironmentVersion), sourceCommitKey{}, plan.Commit), deployActorKey{}, run.Clone()), deadline)
	defer cancel()
	execCtx = WithAcceptedSecretPlan(execCtx, plan.Secrets)
	if err := q.validateAcceptedAppRegistry(execCtx, &plan); err != nil {
		return q.recordPreflightFailure(ctx, job, run)
	}
	if plan.Registry != nil {
		execCtx = WithAcceptedRegistryAuthority(execCtx, model.RegistrySubject{Kind: "app", ID: plan.App.ID}, plan.Registry)
	}
	if _, err := q.Store.Apps.Get(execCtx, plan.App.ID); err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("%w: cannot verify deployment app", jobqueue.ErrOutcomeUnknown)
		}
		return q.recordFailure(ctx, run, "app no longer exists")
	}
	if plan.App.EnvironmentID != "" {
		env, err := q.Store.Environments.Get(execCtx, plan.App.EnvironmentID)
		if err != nil || env == nil {
			return fmt.Errorf("%w: cannot verify deployment environment", jobqueue.ErrOutcomeUnknown)
		}
		if env.Version != plan.EnvironmentVersion {
			return q.recordPreflightFailureForReason(ctx, job, run, store.AppPreflightEnvironmentGeneration)
		}
	}
	if plan.Trigger != nil {
		if q.ValidateTrigger == nil {
			return q.recordFailure(ctx, run, "automation validation is unavailable")
		}
		if err := q.ValidateTrigger(execCtx, &plan); err != nil {
			return q.recordFailure(ctx, run, "automation changed or authorization was revoked; review before deploying")
		}
	}
	if q.RequireImagePins && plan.Images == nil {
		return q.recordFailure(ctx, run, "accepted plan has no image resolution; start a new deployment")
	}
	if plan.Images != nil {
		execCtx = context.WithValue(execCtx, appImageResolutionKey{}, plan.Images)
	}
	now := time.Now().UTC()
	run.Status, run.StartedAt, run.HeartbeatAt = model.RunStatusRunning, &now, &now
	if err := q.Store.Runs.Update(execCtx, run); err != nil {
		return err
	}
	logs := io.Discard
	if q.LogWriter != nil {
		logs = q.LogWriter(plan.RunID)
	}
	_, result, deployErr := q.Deployer.Deploy(execCtx, plan.App, plan.RunID, logs)
	if result == nil {
		if execCtx.Err() != nil {
			return jobqueue.ErrOutcomeUnknown
		}
		if deployErr != nil {
			return q.recordFailure(ctx, run, deployErr.Error())
		}
		return q.recordFailure(ctx, run, "deployer returned no execution result")
	}
	if execCtx.Err() != nil {
		return jobqueue.ErrOutcomeUnknown
	}
	// Preserve the actor captured at acceptance, including governance evidence.
	result.StartedByUserSub, result.StartedByEmail = run.StartedByUserSub, run.StartedByEmail
	result.StartedByGroups, result.StartedByTokenHash = run.StartedByGroups, run.StartedByTokenHash
	recordCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	if err := q.Store.Runs.Update(recordCtx, result); err != nil {
		return fmt.Errorf("%w: cannot persist deployment result", jobqueue.ErrOutcomeUnknown)
	}
	if execCtx.Err() != nil {
		return jobqueue.ErrOutcomeUnknown
	}
	return nil
}

func (q *AppDeployQueue) recordFailure(ctx context.Context, run *model.PipelineRun, reason string) error {
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	now := time.Now().UTC()
	run.Status, run.Error, run.FinishedAt = model.RunStatusFailed, reason, &now
	if err := q.Store.Runs.Update(recordCtx, run); err != nil {
		return fmt.Errorf("%w: cannot record failure", jobqueue.ErrOutcomeUnknown)
	}
	return nil
}

func (q *AppDeployQueue) recordPreflightFailure(ctx context.Context, job *jobqueue.Job, run *model.PipelineRun) error {
	return q.recordPreflightFailureForReason(ctx, job, run, store.AppPreflightRegistryAuthority)
}

func (q *AppDeployQueue) recordPreflightFailureForReason(ctx context.Context, job *jobqueue.Job, run *model.PipelineRun, reason store.AppPreflightReason) error {
	uncertain := func() error { return fmt.Errorf("%w: cannot record deployment rejection", jobqueue.ErrOutcomeUnknown) }
	recorder, ok := q.Store.AppDeploys.(store.AppPreflightFailureRecorder)
	if !ok || job == nil || q.Jobs == nil {
		return uncertain()
	}
	failure, err := store.DecodeAppPreflightFailure(job.Payload)
	failure.Reason = reason
	if err != nil || store.ValidateAppPreflightRun(failure, run) != nil {
		return uncertain()
	}
	// Memory's queue guard checks the live claim atomically. Its immutable payload
	// is checked here; PostgreSQL independently rechecks it under the job lock.
	acceptedJob, err := q.Jobs.Get(ctx, job.ID)
	if err != nil || acceptedJob.Kind != JobKindAppDeploy || acceptedJob.ID != "app-run:"+failure.RunID {
		return uncertain()
	}
	accepted, err := store.DecodeAppPreflightFailure(acceptedJob.Payload)
	if err != nil || !store.SameAppPreflightAcceptance(accepted, failure) {
		return uncertain()
	}
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if claim, claimed := store.RunClaimFromContext(recordCtx); claimed {
		if guarded, supported := q.Jobs.(interface {
			WithClaimPayload(context.Context, string, string, string, string, func() error) error
		}); supported {
			recordCtx = store.WithRunPayloadClaim(recordCtx, func(c context.Context, kind, digest string, write func() error) error {
				return guarded.WithClaimPayload(c, claim.Owner, claim.JobID, kind, digest, write)
			})
		}
	}
	if err := recorder.RecordAppPreflightFailure(recordCtx, failure); err != nil {
		return uncertain()
	}
	return nil
}
