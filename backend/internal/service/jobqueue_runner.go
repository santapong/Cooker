package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/notify/notifier"
	"github.com/santapong/cooker/internal/store"
)

// JobKindPipelineRun is the Job.Kind string the worker registry
// binds to JobQueueRunner.Handle. Exposed as a constant so the
// enqueuer and the boot wiring use the same literal.
const JobKindPipelineRun = "pipeline-run"

// JobQueueRunner is the worker-side counterpart to the inline
// RunPipeline path: a worker picks up a job, JobQueueRunner.Handle
// loads the pipeline + run from the store, runs the executor,
// persists the result, and optionally dispatches a notification
// event. The run row is *always* updated (success or failure) so
// the existing /api/v1/pipelines/:id/runs/:runId polling endpoint
// reflects the outcome.
type JobQueueRunner struct {
	store             *store.Store
	executor          *Executor
	dispatcher        *notifier.Dispatcher // optional; nil = no notifications
	SourcePreparer    *PipelineSourcePreparer
	NativeAutomation  *PipelineAutomation
	RegistryAuthority *RegistryAuthorityService
}

// NewJobQueueRunner constructs a runner. dispatcher may be nil when
// no notifier is wired (in which case the Handle path skips the
// dispatch step entirely).
func NewJobQueueRunner(st *store.Store, exec *Executor, disp *notifier.Dispatcher) *JobQueueRunner {
	return &JobQueueRunner{store: st, executor: exec, dispatcher: disp}
}

// runJobPayload is the wire shape carried by a pipeline-run job.
// Encoded by service.JobQueueEnqueuer and decoded here.
type runJobPayload struct {
	PipelineID string                       `json:"pipelineId"`
	RunID      string                       `json:"runId"`
	Pipeline   *model.Pipeline              `json:"pipeline,omitempty"`
	Run        *model.PipelineRun           `json:"run,omitempty"`
	Secrets    *PipelineSecretPlan          `json:"secretVersions,omitempty"`
	Source     *model.PipelineSourcePlan    `json:"source,omitempty"`
	Trigger    *model.PipelineTriggerProof  `json:"trigger,omitempty"`
	Registry   *model.RegistryAuthorityPlan `json:"registryAuthority,omitempty"`
}

// Handle is the jobqueue.HandlerFunc registered for JobKindPipelineRun.
// Returning nil signals success; returning an error triggers the
// worker's Reschedule path (or terminal Fail when attempts hit
// max_attempts).
//
// Important: a successful Execute that returns a failed run.Status
// is NOT a job-handler error — the *run* failed, but the *job* did
// what it was supposed to do (run the executor, persist the result).
// Only infrastructure-level problems (store load failure, payload
// decode failure, executor panic) should bubble as job errors.
func (r *JobQueueRunner) Handle(ctx context.Context, job *jobqueue.Job) error {
	var payload runJobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return fmt.Errorf("jobqueue runner: decode payload: %w", err)
	}
	if payload.PipelineID == "" || payload.RunID == "" {
		return fmt.Errorf("jobqueue runner: missing pipelineId/runId in payload")
	}
	p := payload.Pipeline
	if p == nil {
		// Legacy jobs are accepted only against their captured version below.
		var err error
		p, err = r.store.Pipelines.Get(ctx, payload.PipelineID)
		if err != nil {
			return fmt.Errorf("jobqueue runner: get pipeline %s: %w", payload.PipelineID, err)
		}
	}
	if p.ID != payload.PipelineID {
		return fmt.Errorf("jobqueue runner: snapshot identity mismatch")
	}
	if payload.Run != nil {
		planned := payload.Run
		if payload.Pipeline == nil || planned.ID != payload.RunID || planned.PipelineID != payload.PipelineID || planned.PipelineVersion != p.Version || planned.Status != model.RunStatusPending || planned.CreatedAt.IsZero() || planned.StartedAt != nil || planned.FinishedAt != nil {
			return fmt.Errorf("jobqueue runner: invalid pending run snapshot")
		}
	}
	var sourceReceipt *model.PipelineRunSource
	if p.Source != nil {
		if payload.Pipeline == nil || payload.Run == nil {
			return fmt.Errorf("source-bound jobs require immutable pipeline and actor snapshots")
		}
		if payload.Trigger == nil && (payload.Source == nil || payload.Source.ReviewPolicy != "manual") {
			return fmt.Errorf("accepted automatic source requires its trigger authority")
		}
		if payload.Source == nil || ValidatePipelineSourcePlan(p, payload.Source) != nil {
			return fmt.Errorf("accepted native source metadata is unavailable")
		}
		var err error
		sourceReceipt, err = model.RunSourceFromAcceptedPlan(payload.Source)
		if err != nil {
			return fmt.Errorf("accepted native source receipt is invalid")
		}
		if payload.Run != nil {
			if payload.Run.Source != nil && payload.Run.Source.CheckedOutAt != nil {
				return fmt.Errorf("admission cannot claim verified checkout")
			}
			payload.Run = payload.Run.Clone()
			payload.Run.Source = sourceReceipt.Clone()
		}
	} else if payload.Source != nil || payload.Trigger != nil || payload.Run != nil && payload.Run.Source != nil {
		return fmt.Errorf("source metadata does not belong to this pipeline")
	}
	run, err := r.store.Runs.Get(ctx, payload.RunID)
	if errors.Is(err, store.ErrNotFound) && payload.Run != nil {
		// The queue is the admission record. View creation can race the HTTP
		// response or recovery; Create must never overwrite their newer state.
		if createErr := r.store.Runs.Create(ctx, payload.Run.Clone()); createErr != nil && !errors.Is(createErr, store.ErrConflict) {
			return fmt.Errorf("jobqueue runner: recreate accepted run: %w", createErr)
		}
		run, err = r.store.Runs.Get(ctx, payload.RunID)
	}
	if err != nil {
		return fmt.Errorf("jobqueue runner: get run %s: %w", payload.RunID, err)
	}
	if run.PipelineID != payload.PipelineID {
		return fmt.Errorf("jobqueue runner: run identity mismatch")
	}
	if planned := payload.Run; planned != nil && (run.PipelineVersion != planned.PipelineVersion || run.StartedByUserSub != planned.StartedByUserSub || run.StartedByEmail != planned.StartedByEmail || run.StartedByTokenHash != planned.StartedByTokenHash || !slices.Equal(run.StartedByGroups, planned.StartedByGroups)) {
		return fmt.Errorf("jobqueue runner: run actor or version differs from accepted plan")
	}
	if sourceReceipt != nil {
		merged, err := model.MergeRunSource(run.Source, sourceReceipt)
		if err != nil {
			return fmt.Errorf("run source differs from the accepted plan")
		}
		run.Source = merged
	} else if run.Source != nil {
		return fmt.Errorf("run source does not belong to this pipeline")
	}

	if run.Status == model.RunStatusSuccess || run.Status == model.RunStatusFailed || run.Status == model.RunStatusCancelled {
		return nil
	}
	if run.Status == model.RunStatusNeedsAttention {
		return jobqueue.ErrOutcomeUnknown
	}
	if payload.Pipeline == nil && run.PipelineVersion != 0 && run.PipelineVersion != p.Version {
		return fmt.Errorf("jobqueue runner: pipeline changed after run was queued")
	}
	if r.executor == nil {
		return fmt.Errorf("jobqueue runner: executor is unavailable")
	}

	// Execute. Per F2 (handler-layering audit), Execute owns terminal
	// status transitions; we persist the run as Execute returned it.
	// A per-pipeline RunDeadline bounds the execution the same way the
	// inline coordinator path does; without one the job inherits the
	// worker's ctx as before.
	execErr := error(nil)
	startedExecution := false
	// A new preflight rejection says nothing about effects from an earlier
	// attempt. Preserve uncertainty for runs whose execution already started.
	freshExecution := run.Status == model.RunStatusPending && run.StartedAt == nil
	for _, stage := range run.StageRuns {
		if stage.StartedAt != nil {
			freshExecution = false
		}
	}
	if r.executor != nil {
		execCtx := withAcceptedPipelineSecrets(ctx, payload.Secrets)
		if r.RegistryAuthority != nil || r.executor.registryAuthority != nil || payload.Registry != nil {
			// Workers never use inline preparation. An omitted runner option
			// must not turn absent durable evidence into fresh credentials.
			authority := r.RegistryAuthority
			if authority == nil {
				authority = r.executor.registryAuthority
			}
			execErr = validateNativeRegistryTransports(p)
			if execErr == nil && authority != nil {
				execErr = ValidatePipelineRuntimeEnvironments(execCtx, authority.Environments, p)
			}
			if execErr == nil && validateNativeRegistryPlan(p, payload.Registry) != nil {
				execErr = model.ErrInvalidRegistryAuthority
			} else if execErr == nil {
				execErr = authority.ValidateCurrent(execCtx, model.RegistrySubject{Kind: "pipeline", ID: p.ID}, payload.Registry)
			}
			execCtx = WithAcceptedRegistryAuthority(execCtx, model.RegistrySubject{Kind: "pipeline", ID: p.ID}, payload.Registry)
		}
		if d := PipelineRunDeadline(p); d > 0 {
			var cancel context.CancelFunc
			execCtx, cancel = context.WithTimeout(execCtx, d)
			defer cancel()
		}
		if execErr == nil && p.Source != nil {
			if payload.Source == nil || r.SourcePreparer == nil {
				execErr = fmt.Errorf("accepted native source is unavailable")
			} else if payload.Trigger != nil && (r.NativeAutomation == nil || r.NativeAutomation.ValidateQueued(execCtx, job, p, run, payload.Source, payload.Trigger) != nil) {
				execErr = fmt.Errorf("accepted native source authority is unavailable")
			} else {
				var prepared *PreparedPipelineSource
				prepared, execErr = r.SourcePreparer.Open(execCtx, p, payload.Source, payload.Trigger != nil)
				if execErr == nil {
					defer prepared.Close()
					checkedAt := prepared.CheckedOutAt
					confirmed := sourceReceipt.Clone()
					confirmed.CheckedOutAt = &checkedAt
					if run.Source.CheckedOutAt != nil {
						confirmed.CheckedOutAt = run.Source.Clone().CheckedOutAt
					}
					run.Source, execErr = model.MergeRunSource(run.Source, confirmed)
					if execErr == nil {
						if recorder, ok := r.store.Runs.(store.RunSourceCheckoutRecorder); ok {
							if err := recorder.RecordSourceCheckout(execCtx, run.ID, run.Source); err != nil {
								execErr = redactedLogError{cause: err, text: "verified checkout persistence is unavailable"}
							}
						} else {
							execErr = fmt.Errorf("verified checkout persistence is unavailable")
						}
					}
					if execErr == nil {
						execCtx = WithPipelineSourceCheckout(execCtx, prepared)
					}
				}
			}
		} else if execErr == nil && (payload.Source != nil || payload.Trigger != nil) {
			execErr = fmt.Errorf("source metadata does not belong to this pipeline")
		}
		if execErr == nil {
			startedExecution = true
			_, execErr = r.executor.Execute(execCtx, p, run)
		} else {
			r.executor.finalize(run, execErr, false)
		}
	}
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if updateErr := r.store.Runs.Update(recordCtx, run); updateErr != nil {
		return fmt.Errorf("%w: persist run outcome: %v", jobqueue.ErrOutcomeUnknown, updateErr)
	}
	var preflight *pipelinePreflightError
	if execErr != nil && freshExecution && (!startedExecution || errors.As(execErr, &preflight)) {
		// Authority/checkout rejection occurred before any native stage. Its
		// persisted failure is determinate, not an ambiguous deployment effect.
		return execErr
	}
	if ctx.Err() != nil || execErr != nil {
		return fmt.Errorf("%w: execution interrupted: %v", jobqueue.ErrOutcomeUnknown, execErr)
	}

	if _, queued := store.RunClaimFromContext(ctx); !queued {
		r.dispatchOutcome(ctx, p, run, execErr)
	}

	// Normal stage failures are already persisted on the run. Infrastructure
	// ambiguity returns ErrOutcomeUnknown above and is never auto-replayed.

	return execErr
}

// dispatchOutcome delegates to the shared NotifyRunOutcome helper so
// the queue path and the inline Spawn path (handler.RunPipeline)
// emit identically. A run flows through exactly one of the two
// paths, so there is no double-send. NotifyRunOutcome handles the
// nil-dispatcher and non-terminal-status no-ops.
func (r *JobQueueRunner) dispatchOutcome(_ context.Context, p *model.Pipeline, run *model.PipelineRun, execErr error) {
	NotifyRunOutcome(r.dispatcher, p, run, execErr)
}
