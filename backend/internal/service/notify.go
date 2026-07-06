package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/notifier"
)

// notifyTimeout bounds one dispatch fan-out. Matches the adapters'
// own per-request HTTP timeouts so a slow channel can't hold the
// caller's goroutine past it.
const notifyTimeout = 10 * time.Second

// notifyCtx returns a context for a dispatch that survives the
// caller's cancellation: a run that failed BECAUSE its context hit
// the deadline is exactly the event that must still go out.
func notifyCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), notifyTimeout)
}

// NotifyRunOutcome emits the terminal-run event for a pipeline run.
// Shared by the job-queue runner and the inline Spawn path in
// handler.RunPipeline so both dispatch identically — a run flows
// through exactly one of those paths, never both. Nil dispatcher or
// a non-terminal status is a silent no-op (defensive: a future
// regression should skip notification rather than emit a misleading
// event).
func NotifyRunOutcome(disp *notifier.Dispatcher, p *model.Pipeline, run *model.PipelineRun, execErr error) {
	if disp == nil || p == nil || run == nil {
		return
	}
	event := notifier.Event{
		PipelineID:   p.ID,
		PipelineName: p.Name,
		RunID:        run.ID,
		Status:       string(run.Status),
		Timestamp:    time.Now().UTC(),
	}
	switch {
	case execErr != nil:
		event.Type = notifier.EventRunFailed
		event.Error = execErr.Error()
	case run.Status == model.RunStatusCancelled:
		event.Type = notifier.EventRunCancelled
	case run.Status == model.RunStatusSuccess:
		event.Type = notifier.EventRunSucceeded
	case run.Status == model.RunStatusFailed:
		event.Type = notifier.EventRunFailed
		event.Error = run.Error
	default:
		return
	}
	dispatch(disp, event, "run", run.ID)
}

// NotifyDeployOutcome emits the terminal event for an app deploy or
// rollback. success=false with a build-stage failure should pass
// buildFailed=true so the event reads as build.failed rather than
// deploy.failed.
func NotifyDeployOutcome(disp *notifier.Dispatcher, app *model.App, runID, errMsg string, success, buildFailed bool) {
	if disp == nil || app == nil {
		return
	}
	event := notifier.Event{
		AppID:       app.ID,
		AppName:     app.Name,
		Environment: app.DeployTarget.Namespace,
		RunID:       runID,
		Error:       errMsg,
		Timestamp:   time.Now().UTC(),
	}
	switch {
	case success:
		event.Type = notifier.EventDeploySucceeded
	case buildFailed:
		event.Type = notifier.EventBuildFailed
	default:
		event.Type = notifier.EventDeployFailed
	}
	dispatch(disp, event, "app", app.ID)
}

// NotifyCanary emits a canary state-change event (promoted /
// aborted / failed).
func NotifyCanary(disp *notifier.Dispatcher, app *model.App, c *model.AppCanary, t notifier.EventType, errMsg string) {
	if disp == nil || app == nil || c == nil {
		return
	}
	dispatch(disp, notifier.Event{
		Type:        t,
		AppID:       app.ID,
		AppName:     app.Name,
		Environment: app.DeployTarget.Namespace,
		RunID:       c.RunID,
		Status:      string(c.Status),
		Error:       errMsg,
		Timestamp:   time.Now().UTC(),
	}, "app", app.ID)
}

// dispatch fans the event out on its own bounded context and logs
// (never propagates) channel failures — notifications are
// best-effort by design.
func dispatch(disp *notifier.Dispatcher, event notifier.Event, idKey, id string) {
	ctx, cancel := notifyCtx()
	defer cancel()
	if err := disp.Dispatch(ctx, event); err != nil {
		slog.Warn("notifier: dispatch failed", "event", string(event.Type), idKey, id, "err", err)
	}
}
