package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/santapong/cooker/internal/model"
)

// executeApproval is a persisted human-in-the-loop pause gate. It opens
// (or re-attaches to) a StageApproval row, broadcasts the stage as
// "awaiting" so the run page surfaces an Approve/Reject affordance, then
// blocks — polling the gate — until it is approved (stage succeeds),
// rejected (stage fails), or ctx is cancelled (run deadline / cancel /
// stage timeout fires and the stage fails). Closes HS26-05-03 (approval
// gate) — formerly a fail-loud stub. With no StageApprovalService wired the
// gate cannot be persisted, so the stage fails loudly rather than
// silently auto-passing a human gate.
func (e *Executor) executeApproval(ctx context.Context, runID string, stage *model.Stage, sr *model.StageRun) error {
	if e.stageApprovals == nil {
		return fmt.Errorf("approval stage %q: approval gate not configured", stage.Name)
	}

	gate, err := e.stageApprovals.Await(ctx, runID, stage.ID, stage.Config.RequiredApprovers)
	if err != nil {
		return fmt.Errorf("approval stage %q: open gate: %w", stage.Name, err)
	}

	// Persist a breadcrumb in the stage log so the run page (which tails
	// stage logs) explains why the stage is paused, and broadcast the
	// non-FSM "awaiting" status so the step rail tints + offers the
	// Approve/Reject buttons. The stage row itself stays "running" — the
	// stage FSM has no awaiting state, and the gate row is the source of
	// truth for the decision.
	if lw := e.newStageLineWriter(runID, stage); lw != nil {
		fmt.Fprintf(lw, "awaiting approval: %d distinct approver(s) required\n", gate.RequiredApprovers)
		lw.flush()
	}
	sr.Logs = fmt.Sprintf("awaiting approval: %d distinct approver(s) required\n", gate.RequiredApprovers)

	// If a previous attempt (pre-restart) already settled the gate, resolve
	// immediately without re-broadcasting awaiting.
	switch gate.Status {
	case model.StageApprovalApproved:
		return nil
	case model.StageApprovalRejected:
		return fmt.Errorf("approval stage %q: rejected by %s", stage.Name, gate.ResolvedBy)
	}

	e.broadcastStatus(runID, stage.ID, "awaiting")

	poll := e.approvalPoll
	if poll <= 0 {
		poll = defaultApprovalPoll
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			// Run deadline, operator cancel, or the per-stage timeout. Fail
			// the stage with the context cause; finalize() maps a canceled
			// run to Cancelled, a deadline to Failed.
			return fmt.Errorf("approval stage %q: %w", stage.Name, ctx.Err())
		case <-ticker.C:
			g, gerr := e.stageApprovals.Get(ctx, runID, stage.ID)
			if gerr != nil {
				// A transient store read error shouldn't fail the gate;
				// log and keep polling. A vanished gate (NotFound) is
				// unexpected mid-run but also non-fatal — keep waiting for
				// ctx to bound the loop.
				slog.Warn("approval stage: gate read failed", "run", runID, "stage", stage.Name, "err", gerr)
				continue
			}
			switch g.Status {
			case model.StageApprovalApproved:
				return nil
			case model.StageApprovalRejected:
				return fmt.Errorf("approval stage %q: rejected by %s", stage.Name, g.ResolvedBy)
			}
		}
	}
}

