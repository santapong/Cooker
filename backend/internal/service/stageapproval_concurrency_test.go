package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
)

// Pause a real service operation at its store boundary, then let the opposing
// operation settle the same gate. The original awaiting read is deliberately
// stale; the store must preserve the winning decision and its identity.
type delayedStageApprovalStore struct {
	store.StageApprovalStore
	status  model.StageApprovalStatus
	vote    bool
	reached chan struct{}
	release chan struct{}
}

func (s *delayedStageApprovalStore) wait(ctx context.Context) error {
	close(s.reached)
	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *delayedStageApprovalStore) UpdateGateStatus(ctx context.Context, id string, status model.StageApprovalStatus, by string, at *time.Time) error {
	if status == s.status {
		if err := s.wait(ctx); err != nil {
			return err
		}
	}
	return s.StageApprovalStore.UpdateGateStatus(ctx, id, status, by, at)
}

func (s *delayedStageApprovalStore) AddVote(ctx context.Context, vote *model.StageApprovalVote) (bool, int, error) {
	if s.vote || s.status == model.StageApprovalApproved {
		if err := s.wait(ctx); err != nil {
			return false, 0, err
		}
	}
	return s.StageApprovalStore.AddVote(ctx, vote)
}

func TestStageApprovalConcurrentDecisionPreservesFirstResolution(t *testing.T) {
	for _, tc := range []struct {
		name, delayedAction string
		pauseStatus         model.StageApprovalStatus
		pauseVote           bool
		winner              model.StageApprovalStatus
		votes               int
	}{
		{"reject-after-approval", "reject", model.StageApprovalRejected, false, model.StageApprovalApproved, 1},
		{"approval-after-rejection", "approve", model.StageApprovalApproved, false, model.StageApprovalRejected, 0},
		{"vote-after-rejection", "approve", "", true, model.StageApprovalRejected, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			base := memory.New().StageApprovals
			paused := &delayedStageApprovalStore{StageApprovalStore: base, status: tc.pauseStatus, vote: tc.pauseVote, reached: make(chan struct{}), release: make(chan struct{})}
			delayed, winner := NewStageApprovalService(paused), NewStageApprovalService(base)
			if _, err := winner.Await(ctx, "owned-run", "review", 1); err != nil {
				t.Fatal(err)
			}
			type result struct {
				gate *model.StageApproval
				err  error
			}
			done := make(chan result, 1)
			go func() {
				var gate *model.StageApproval
				var err error
				if tc.delayedAction == "reject" {
					gate, err = delayed.Reject(ctx, "owned-run", "review", "late@example.invalid", "late veto")
				} else {
					gate, err = delayed.Approve(ctx, "owned-run", "review", "late", "late@example.invalid", "late vote")
				}
				done <- result{gate, err}
			}()
			select {
			case <-paused.reached:
			case <-ctx.Done():
				t.Fatal("operation did not reach the interleaving boundary")
			}
			var winningGate *model.StageApproval
			var err error
			if tc.winner == model.StageApprovalApproved {
				winningGate, err = winner.Approve(ctx, "owned-run", "review", "winner", "winner@example.invalid", "reviewed")
			} else {
				winningGate, err = winner.Reject(ctx, "owned-run", "review", "winner@example.invalid", "reviewed")
			}
			close(paused.release)
			if err != nil {
				t.Fatal(err)
			}
			select {
			case got := <-done:
				if !errors.Is(got.err, ErrGateResolved) || got.gate == nil || got.gate.Status != tc.winner {
					t.Fatalf("late decision was not an honest conflict: gate=%+v err=%v", got.gate, got.err)
				}
			case <-ctx.Done():
				t.Fatal("late decision did not finish")
			}
			persisted, err := base.GetGate(ctx, "owned-run", "review")
			if err != nil || persisted.Status != tc.winner || persisted.ResolvedBy != winningGate.ResolvedBy || persisted.ResolvedAt == nil || winningGate.ResolvedAt == nil || !persisted.ResolvedAt.Equal(*winningGate.ResolvedAt) || persisted.ApprovalCount() != tc.votes {
				t.Fatalf("winning decision or vote set changed: %+v err=%v", persisted, err)
			}
		})
	}
}
