package governance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// PipelineDeployHook builds the executor's pre-stage governance check for
// pipeline-defined deploy stages (Milestone C). The hook:
//
//   1. Resolves the (service, env) target from the stage. Service name is
//      the pipeline's name (matches the existing AppDeployExtractor convention).
//      Env name is the Environment.Name keyed off Stage.EnvironmentID.
//   2. Constructs a PreresolvedActor from the run's StartedBy* fields.
//   3. Calls client.AuthorizeOnBehalf.
//   4. On enforce-deny returns an error so the executor fails the stage
//      with the policy reason. On advisory-deny (Enforced=false) returns
//      nil after logging the would-have-blocked event.
//
// Returns nil (no hook) when client is nil or delegation is disabled.
// In both cases the executor's WithDeployGovernanceHook(nil) path runs —
// pipeline deploys proceed without the gate. The HTTP middleware at
// /apps/:id/deploy still gates that surface independently.
func PipelineDeployHook(client *Client, st *store.Store, pipelineNameFor func(ctx context.Context, pipelineID string) (string, error)) func(ctx context.Context, run *model.PipelineRun, stage *model.Stage) error {
	if client == nil || !client.DelegationEnabled() {
		return nil
	}
	return func(ctx context.Context, run *model.PipelineRun, stage *model.Stage) error {
		// 0. No actor captured (pre-Milestone-C run row) → skip the check.
		//    The migration default is empty string, so legacy rows hit this.
		if run == nil || run.StartedByUserSub == "" {
			slog.Warn("governance: skipping executor hook (no actor on run)",
				"run", runIDOf(run), "stage", stage.Name)
			return nil
		}
		// 1. Resolve env name from stage.EnvironmentID.
		if stage.EnvironmentID == "" {
			// No environment link — nothing to gate.
			return nil
		}
		envName, err := lookupEnvName(ctx, st, stage.EnvironmentID)
		if err != nil {
			return fmt.Errorf("governance: lookup env: %w", err)
		}
		if envName == "" {
			return nil
		}
		// 2. Service name = pipeline name. Looked up via the supplied
		//    callback so this package doesn't reach into the pipeline
		//    store directly (keeps the dep graph clean).
		svc, err := pipelineNameFor(ctx, run.PipelineID)
		if err != nil || svc == "" {
			return fmt.Errorf("governance: lookup pipeline name: %w", err)
		}
		svc = strings.ToLower(svc)

		// 3. Build the pre-resolved actor.
		actor := PreresolvedActor{
			Kind:   "human", // pipeline runs start from an authenticated user; service-account runs are out of scope for v1.1
			ID:     run.StartedByUserSub,
			Groups: append([]string(nil), run.StartedByGroups...),
		}

		// 4. Call out.
		decision, err := client.AuthorizeOnBehalf(ctx, actor, svc, envName, run.ID)
		if err != nil {
			if errors.Is(err, ErrGovernanceUnreachable) {
				// Fail-closed: surface as a stage error.
				return fmt.Errorf("governance unreachable (fail-closed) for %s/%s: %w", svc, envName, err)
			}
			return fmt.Errorf("governance: authorize on behalf: %w", err)
		}
		if !decision.Allowed() {
			if !decision.Enforced {
				slog.Info("governance: advisory deny on pipeline deploy (would have blocked)",
					"run", run.ID, "stage", stage.Name,
					"service", svc, "env", envName,
					"reason", decision.Reason, "policy", decision.PolicyID, "audit", decision.AuditID,
					"mode", decision.EnforcementMode)
				return nil
			}
			return fmt.Errorf("denied by governance for %s/%s: %s (policy %s, audit %s)",
				svc, envName, decision.Reason, decision.PolicyID, decision.AuditID)
		}
		return nil
	}
}

func runIDOf(r *model.PipelineRun) string {
	if r == nil {
		return ""
	}
	return r.ID
}

func lookupEnvName(ctx context.Context, st *store.Store, envID string) (string, error) {
	if st == nil || st.Environments == nil {
		return "", nil
	}
	env, err := st.Environments.Get(ctx, envID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", nil
		}
		return "", err
	}
	if env == nil {
		return "", nil
	}
	return strings.ToLower(env.Name), nil
}
