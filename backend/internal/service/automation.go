package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/source/connection"
	"github.com/santapong/cooker/internal/store"
)

type TriggerExecution struct {
	IntentID     string `json:"intentId"`
	RuleID       string `json:"ruleId"`
	RuleVersion  int    `json:"ruleVersion"`
	EnvelopeHash string `json:"envelopeHash"`
}
type triggerExecutionKey struct{}

type SourceRevision struct{ FullRef, Commit, Object string }
type AutomationSource interface {
	Identity(context.Context, *connection.Connection, string) (model.GitSourceIdentity, error)
	Revision(context.Context, *connection.Connection, string, string) (SourceRevision, error)
}

// Automation bridges the authenticated durable inbox and the durable App queue.
// Admission can be repeated after a crash: its stable job ID recovers the same
// receipt. External deployment is never performed by the webhook request.
type Automation struct {
	Triggers                 *TriggerService
	Connections              *connection.Service
	Store                    *store.Store
	Queue                    *AppDeployQueue
	Source                   AutomationSource
	Inspect                  func(context.Context, *model.App, map[string]string) (*AutomationEnvelope, error)
	ResolveEnvironment       func(context.Context, *model.App) (map[string]string, error)
	Authorize                func(context.Context, *model.TriggerRule, *model.App) error
	Native                   *PipelineAutomation
	RegistryPreflight        func(context.Context, *model.App) error
	RequireRegistryPreflight bool
}

type TriggerPreview struct {
	Rule     *model.TriggerRule  `json:"rule"`
	Envelope *AutomationEnvelope `json:"envelope"`
}

func (a *Automation) prepareApp(ctx context.Context, r *model.TriggerRule) (*model.App, *connection.Connection, int, error) {
	app, err := a.Store.Apps.Get(ctx, r.AppID)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("target App is unavailable")
	}
	if err := a.checkRegistryPreflight(ctx, app); err != nil {
		return nil, nil, 0, err
	}
	if err := ValidateRuntimeEnvironmentBinding(ctx, a.Store.Environments, app.EnvironmentID); err != nil {
		return nil, nil, 0, err
	}
	conn, err := a.Connections.Store.Get(ctx, r.Source.ConnectionID)
	if err != nil || !conn.Allows(app.GitHubRepo) {
		return nil, nil, 0, fmt.Errorf("source connection does not allow this repository")
	}
	if app.BuildPlan == nil || app.BuildPlan.Kind != model.BuildPlanCompose {
		return nil, nil, 0, fmt.Errorf("select and inspect a Compose plan before enabling automation")
	}
	copy := store.CloneTrigger(app)
	copy.AutoDeploy = false
	copy.BuildPlan.SourceConnectionID = conn.ID
	copy.BuildPlan.SourceConnectionVersion = conn.Version
	copy.BuildPlan.InstallationID = conn.InstallationID
	identity, err := a.Source.Identity(ctx, conn, app.GitHubRepo)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("cannot verify source repository identity")
	}
	if r.Source.RepositoryID != "" && !r.Source.SameRepository(identity) {
		return nil, nil, 0, fmt.Errorf("repository identity changed; reconnect and review it")
	}
	copy.BuildPlan.SourceRepositoryID = identity.RepositoryID
	r.Source = identity
	envVersion := 0
	if app.EnvironmentID != "" {
		env, err := a.Store.Environments.Get(ctx, app.EnvironmentID)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("environment is unavailable")
		}
		envVersion = env.Version
	}
	return copy, conn, envVersion, nil
}

// Preview derives every authority-bearing field server-side. A client may
// select a connection/rule, but cannot supply an actor or an approval envelope.
func (a *Automation) Preview(ctx context.Context, r *model.TriggerRule, fullRef string) (*TriggerPreview, error) {
	if a == nil || r == nil {
		return nil, fmt.Errorf("automation is unavailable")
	}
	kind, target := r.Target()
	if kind == "pipeline" {
		if a.Native == nil {
			return nil, fmt.Errorf("native automation is unavailable")
		}
		return a.Native.Preview(ctx, r, fullRef)
	}
	if kind != "app" || target == "" || (r.AppID != "" && r.AppID != target) {
		return nil, fmt.Errorf("trigger target is unavailable")
	}
	r.AppID = target
	app, conn, envVersion, err := a.prepareApp(ctx, r)
	if err != nil {
		return nil, err
	}
	if err := ValidateTriggerRule(r); err != nil {
		return nil, err
	}
	event := model.VerifiedGitEvent{Source: r.Source, FullRef: fullRef}
	check := *r
	check.Enabled = true
	if ok, _ := TriggerRuleMatches(&check, event); !ok {
		return nil, fmt.Errorf("preview ref must match this rule")
	}
	rev, err := a.Source.Revision(ctx, conn, app.GitHubRepo, fullRef)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve the selected source ref")
	}
	r.AppVersion = app.Version
	r.EnvironmentID = app.EnvironmentID
	r.EnvironmentVersion = envVersion
	r.SourceVersion = conn.Version
	r.ActorSubject = "cooker:trigger:" + r.ID
	r.ActorGroups = nil
	r.PlanHash = automationPlanHash(app, envVersion, conn.Version)
	app.BuildPlan.Commit = rev.Commit
	envelope, err := a.inspect(ctx, app, envVersion)
	if err != nil {
		return nil, err
	}
	r.ApprovedEnvelopeHash = envelope.Hash
	if a.Authorize == nil {
		return nil, fmt.Errorf("automation authorization is unavailable")
	}
	if err := a.Authorize(ctx, r, app); err != nil {
		return nil, fmt.Errorf("automation actor is not authorized for this App and environment")
	}
	return &TriggerPreview{Rule: r, Envelope: envelope}, nil
}

func (a *Automation) inspect(ctx context.Context, app *model.App, version int) (*AutomationEnvelope, error) {
	if a.Inspect == nil {
		return nil, fmt.Errorf("automation inspection is unavailable")
	}
	ctx = WithLogSecrets(WithEnvironmentVersion(ctx, app.EnvironmentID, version), nil)
	env := map[string]string{}
	if app.EnvironmentID != "" {
		if a.ResolveEnvironment == nil {
			return nil, fmt.Errorf("environment resolver is unavailable")
		}
		var err error
		env, err = a.ResolveEnvironment(ctx, app)
		if err != nil {
			return nil, fmt.Errorf("cannot resolve the approved environment")
		}
	}
	return a.Inspect(ctx, app, env)
}

func (a *Automation) checkedIntent(ctx context.Context, i *model.TriggerIntent) (*model.App, error) {
	r, err := a.Triggers.Store.GetRule(ctx, i.Rule.ID)
	if err != nil || !r.Enabled || r.Version != i.Rule.Version {
		return nil, fmt.Errorf("rule changed or was disabled; review a new intent")
	}
	app, conn, ev, err := a.prepareApp(ctx, r)
	if err != nil {
		return nil, err
	}
	if r.ActorSubject != "cooker:trigger:"+r.ID || len(r.ActorGroups) > 0 || r.AppVersion != app.Version || r.EnvironmentID != app.EnvironmentID || r.EnvironmentVersion != ev || r.SourceVersion != conn.Version || r.PlanHash != automationPlanHash(app, ev, conn.Version) {
		return nil, fmt.Errorf("app, environment, or connection changed; review the automation settings")
	}
	if !r.Source.SameRepository(i.Event.Source) {
		return nil, fmt.Errorf("event source is outside the approved repository")
	}
	if a.Authorize == nil || a.Authorize(ctx, r, app) != nil {
		return nil, fmt.Errorf("automation actor is no longer authorized")
	}
	app.BuildPlan.Commit = i.Event.CommitSHA
	envelope, err := a.inspect(ctx, app, ev)
	if err != nil {
		return nil, err
	}
	if envelope.Hash != r.ApprovedEnvelopeHash {
		return nil, fmt.Errorf("deployment configuration changed; inspect and approve its new configuration")
	}
	return app, nil
}

// Dispatch leaves unavailable source/authorization/configuration as reviewable
// work. It never turns a temporary provider failure into an unbounded retry.
func (a *Automation) Dispatch(ctx context.Context, i *model.TriggerIntent) error {
	if i == nil || i.Status != model.TriggerPending {
		return nil
	}
	if kind, _ := i.Rule.Target(); kind == "pipeline" {
		if a.Native == nil {
			return fmt.Errorf("native automation is unavailable")
		}
		return a.Native.Dispatch(ctx, i)
	} else if kind != "app" {
		return fmt.Errorf("trigger target is unavailable")
	}
	if a.Queue == nil || a.Queue.Jobs == nil {
		return fmt.Errorf("durable deployment queue is unavailable")
	}
	if atomic, ok := a.Queue.Jobs.(interface{ AtomicTriggerAdmission() bool }); !ok || !atomic.AtomicTriggerAdmission() {
		return fmt.Errorf("atomic trigger admission is unavailable")
	}
	// Queue acceptance may have committed before the process lost its response.
	if job, err := a.Queue.Jobs.Get(ctx, "app-run:"+i.RunID); err == nil {
		r, err := receiptForPlan(job, i.Rule.AppID, i.Event.CommitSHA)
		if err != nil {
			return err
		}
		return a.recordReceipt(ctx, i, r)
	} else if !errors.Is(err, jobqueue.ErrNotFound) {
		return err
	}
	app, err := a.checkedIntent(ctx, i)
	if err != nil {
		return a.Triggers.Store.TransitionIntent(ctx, i.ID, i.Version, model.TriggerAwaitingReview, err.Error(), nil)
	}
	conn, err := a.Connections.Store.Get(ctx, i.Rule.Source.ConnectionID)
	if err != nil {
		return err
	}
	rev, err := a.Source.Revision(ctx, conn, app.GitHubRepo, i.Event.FullRef)
	if err != nil {
		return a.Triggers.Store.TransitionIntent(ctx, i.ID, i.Version, model.TriggerAwaitingReview, "source ref cannot be verified; retry after reviewing provider access", nil)
	}
	if !sameAcceptedRevision(i.Event, rev) {
		status := model.TriggerSuperseded
		if i.Rule.RefKind == "tag" {
			status = model.TriggerAwaitingReview
		}
		return a.Triggers.Store.TransitionIntent(ctx, i.ID, i.Version, status, "source ref changed; this delivery will not deploy a different revision", nil)
	}
	ctx = context.WithValue(ctx, triggerExecutionKey{}, &TriggerExecution{IntentID: i.ID, RuleID: i.Rule.ID, RuleVersion: i.Rule.Version, EnvelopeHash: i.Rule.ApprovedEnvelopeHash})
	actor := &model.PipelineRun{StartedByUserSub: i.Rule.ActorSubject, StartedByEmail: "automation:" + i.Rule.ID}
	r, err := a.Queue.EnqueueIntent(ctx, app, i.Event.CommitSHA, actor, i.ID)
	if err != nil {
		return err
	}
	return a.recordReceipt(ctx, i, r)
}

func (a *Automation) recordReceipt(ctx context.Context, i *model.TriggerIntent, r *AppDeployReceipt) error {
	current, err := a.Triggers.Store.GetIntent(ctx, i.ID)
	if err != nil {
		return err
	}
	if current.Status == model.TriggerQueued && current.Receipt != nil && current.Receipt.RunID == r.RunID && current.Receipt.JobID == r.JobID && current.Receipt.PlanDigest == r.PlanDigest {
		return nil
	}
	return fmt.Errorf("deployment receipt was not committed with the queue admission")
}

func (a *Automation) ValidateQueued(ctx context.Context, p *AppDeployPlan) error {
	if p == nil || p.App == nil {
		return fmt.Errorf("queued App plan is required")
	}
	if p.Trigger == nil {
		return nil
	}
	i, err := a.Triggers.Store.GetIntent(ctx, p.Trigger.IntentID)
	if err != nil {
		return err
	}
	if i.Status != model.TriggerQueued || i.Rule.ID != p.Trigger.RuleID || i.Rule.Version != p.Trigger.RuleVersion || i.RunID != p.RunID || i.Event.CommitSHA != p.Commit || i.Rule.ApprovedEnvelopeHash != p.Trigger.EnvelopeHash {
		return fmt.Errorf("queued trigger identity mismatch")
	}
	app, err := a.checkedIntent(ctx, i)
	if err != nil {
		return err
	}
	if app.ID != p.App.ID || app.Version != p.App.Version || p.Actor == nil || p.Actor.StartedByUserSub != i.Rule.ActorSubject || len(p.Actor.StartedByGroups) > 0 || p.Actor.StartedByTokenHash != "" {
		return fmt.Errorf("queued actor or target mismatch")
	}
	// A job can wait behind a running deployment while this branch advances.
	// Recheck at worker admission, after envelope inspection, so an old queued
	// event cannot roll the workload back merely because Dispatch once matched.
	conn, err := a.Connections.Store.Get(ctx, i.Rule.Source.ConnectionID)
	if err != nil || conn.Version != i.Rule.SourceVersion {
		return fmt.Errorf("source connection changed after queueing")
	}
	rev, err := a.Source.Revision(ctx, conn, app.GitHubRepo, i.Event.FullRef)
	if err != nil || !sameAcceptedRevision(i.Event, rev) {
		return fmt.Errorf("source ref changed or is unavailable after queueing")
	}
	return nil
}

// CheckRef recovers a missed event through the same inbox and logical dedupe.
// The caller selects an explicit full ref; glob scans can enumerate this method
// with bounded pagination without inventing a separate execution path.
func (a *Automation) CheckRef(ctx context.Context, r *model.TriggerRule, fullRef string) (*model.TriggerDelivery, bool, error) {
	if a == nil || r == nil {
		return nil, false, fmt.Errorf("source automation is unavailable")
	}
	if kind, _ := r.Target(); kind == "pipeline" {
		if a.Native == nil {
			return nil, false, fmt.Errorf("native automation is unavailable")
		}
		return a.Native.CheckRef(ctx, r, fullRef)
	}
	if _, err := a.reviewedRecoveryApp(ctx, r, fullRef); err != nil {
		return nil, false, err
	}
	// prepareApp derives identity on its argument. Preserve the reviewed rule for
	// the final comparison instead of modifying the caller's authority snapshot.
	review := store.CloneTrigger(r)
	app, conn, environmentVersion, err := a.prepareApp(ctx, review)
	if err != nil {
		return nil, false, err
	}
	if !review.Source.SameRepository(r.Source) || conn.Version != r.SourceVersion || app.Version != r.AppVersion || environmentVersion != r.EnvironmentVersion || automationPlanHash(app, environmentVersion, conn.Version) != r.PlanHash {
		return nil, false, fmt.Errorf("source or deployment configuration requires review")
	}
	rev, err := a.Source.Revision(ctx, conn, app.GitHubRepo, fullRef)
	if err != nil || rev.FullRef != fullRef {
		return nil, false, fmt.Errorf("cannot verify the selected source ref")
	}
	b, _ := json.Marshal([]string{r.Source.ConnectionID, r.Source.RepositoryID, fullRef, rev.Commit, rev.Object})
	h := sha256.Sum256(b)
	sum := hex.EncodeToString(h[:])
	event := model.VerifiedGitEvent{Source: r.Source, DeliveryID: "reconcile:" + r.ID + ":" + fmt.Sprint(r.Version) + ":" + sum, Event: "push", FullRef: fullRef, CommitSHA: rev.Commit, PayloadHash: sum}
	if strings.HasPrefix(fullRef, "refs/tags/") {
		event.ObjectSHA = rev.Object
	}
	if err := validateVerifiedGitEvent(event); err != nil {
		return nil, false, fmt.Errorf("cannot verify the selected source ref")
	}
	app.BuildPlan.Commit = rev.Commit
	envelope, err := a.inspect(ctx, app, environmentVersion)
	if err != nil || envelope == nil || envelope.Hash != r.ApprovedEnvelopeHash {
		return nil, false, fmt.Errorf("deployment inputs require another review")
	}
	currentApp, err := a.reviewedRecoveryApp(ctx, r, fullRef)
	if err != nil {
		return nil, false, err
	}
	if err := a.checkRegistryPreflight(ctx, currentApp); err != nil {
		return nil, false, err
	}
	if ctx.Err() != nil {
		return nil, false, fmt.Errorf("source recovery was cancelled")
	}
	return a.Triggers.AcceptVerified(ctx, event)
}

func (a *Automation) Run(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		items, err := a.Triggers.Store.PendingIntents(ctx, 20)
		if err == nil {
			for _, i := range items {
				work, cancel := context.WithTimeout(ctx, 90*time.Second)
				_ = a.Dispatch(work, i)
				cancel()
				if ctx.Err() != nil {
					return
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func fullTriggerRef(kind, name string) string {
	if kind == "tag" {
		return "refs/tags/" + strings.TrimPrefix(name, "refs/tags/")
	}
	return "refs/heads/" + strings.TrimPrefix(name, "refs/heads/")
}

func (a *Automation) ApproveIntent(ctx context.Context, i *model.TriggerIntent, reviewer string) error {
	if reviewer == "" {
		return fmt.Errorf("reviewer is required")
	}
	if i == nil {
		return store.ErrConflict
	}
	if kind, _ := i.Rule.Target(); kind == "pipeline" {
		if a.Native == nil {
			return fmt.Errorf("native automation is unavailable")
		}
		return a.Native.ApproveIntent(ctx, i, reviewer)
	} else if kind != "app" {
		return fmt.Errorf("trigger target is unavailable")
	}
	if _, err := a.checkedIntent(ctx, i); err != nil {
		return err
	}
	return a.Triggers.Store.TransitionIntent(ctx, i.ID, i.Version, model.TriggerPending, "approved by "+reviewer+"; awaiting final source validation", nil)
}

func sameAcceptedRevision(event model.VerifiedGitEvent, revision SourceRevision) bool {
	if revision.FullRef != event.FullRef || revision.Commit != event.CommitSHA {
		return false
	}
	if strings.HasPrefix(event.FullRef, "refs/tags/") {
		return event.ObjectSHA != "" && revision.Object == event.ObjectSHA
	}
	return true
}
