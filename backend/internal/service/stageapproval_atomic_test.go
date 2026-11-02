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

// Interrupt the service immediately after the store commits the final vote.
// PostgreSQL honors cancellation on the subsequent read, so emulate that
// boundary without claiming a killed worker process.
type interruptedApprovalVoteStore struct {
	store.StageApprovalStore
	cancel context.CancelFunc
}

func (s *interruptedApprovalVoteStore) AddVote(ctx context.Context, vote *model.StageApprovalVote) (bool, int, error) {
	added, count, err := s.StageApprovalStore.AddVote(ctx, vote)
	if err == nil {
		s.cancel()
	}
	return added, count, err
}
func (s *interruptedApprovalVoteStore) GetGate(ctx context.Context, runID, stageID string) (*model.StageApproval, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.StageApprovalStore.GetGate(ctx, runID, stageID)
}
func TestStageApprovalAtomicFinalVoteSurvivesResponseInterruption(t *testing.T) {
	base := memory.New().StageApprovals
	service := NewStageApprovalService(base)
	if _, err := service.Await(context.Background(), "owned-run", "review", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Approve(context.Background(), "owned-run", "review", "first", "first@example.invalid", "first review"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	interrupted := NewStageApprovalService(&interruptedApprovalVoteStore{StageApprovalStore: base, cancel: cancel})
	if _, err := interrupted.Approve(ctx, "owned-run", "review", "final", "final@example.invalid", "final review"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected interrupted response, got %v", err)
	}
	persisted, err := base.GetGate(context.Background(), "owned-run", "review")
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != model.StageApprovalApproved || persisted.ApprovalCount() != 2 || persisted.ResolvedBy != "final@example.invalid" || persisted.ResolvedAt == nil {
		t.Fatalf("final vote committed without durable quorum resolution: %+v", persisted)
	}
}

// Represent an older persisted awaiting gate with votes already at quorum.
// The underlying store still supplies the terminal-decision fence; PostgreSQL
// seeding/reconstruction is verified separately by its owned integration test.
type legacyQuorumApprovalStore struct{ store.StageApprovalStore }

func (s *legacyQuorumApprovalStore) GetGate(ctx context.Context, runID, stageID string) (*model.StageApproval, error) {
	gate, err := s.StageApprovalStore.GetGate(ctx, runID, stageID)
	if err != nil {
		return nil, err
	}
	gate.Votes = []model.StageApprovalVote{
		{ID: "legacy-1", StageApprovalID: gate.ID, ApproverSub: "one", ApproverEmail: "one@example.invalid"},
		{ID: "legacy-2", StageApprovalID: gate.ID, ApproverSub: "two", ApproverEmail: "two@example.invalid"},
	}
	return gate, nil
}
func TestStageApprovalRecoversLegacyAwaitingQuorum(t *testing.T) {
	for _, method := range []string{"get", "await"} {
		t.Run(method, func(t *testing.T) {
			base := memory.New().StageApprovals
			legacy := &legacyQuorumApprovalStore{StageApprovalStore: base}
			service := NewStageApprovalService(legacy)
			if _, err := service.Await(context.Background(), "owned-run", "review", 2); err != nil {
				t.Fatal(err)
			}
			var gate *model.StageApproval
			var err error
			if method == "get" {
				gate, err = service.Get(context.Background(), "owned-run", "review")
			} else {
				gate, err = service.Await(context.Background(), "owned-run", "review", 99)
			}
			if err != nil || gate.Status != model.StageApprovalApproved || gate.RequiredApprovers != 2 || gate.ApprovalCount() != 2 || gate.ResolvedBy != "" || gate.ResolvedAt == nil {
				t.Fatalf("legacy quorum not recovered with original policy and unknown prior resolver: %+v, %v", gate, err)
			}
			first := *gate.ResolvedAt
			again, err := service.Get(context.Background(), "owned-run", "review")
			if err != nil || again.ResolvedAt == nil || !again.ResolvedAt.Equal(first) {
				t.Fatalf("recovery re-stamped terminal decision: %+v, %v", again, err)
			}
		})
	}
}
func TestStageApprovalLegacyRecoveryCannotReplaceRejection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	base := memory.New().StageApprovals
	opener := NewStageApprovalService(base)
	if _, err := opener.Await(ctx, "owned-run", "review", 2); err != nil {
		t.Fatal(err)
	}
	paused := &delayedStageApprovalStore{StageApprovalStore: &legacyQuorumApprovalStore{StageApprovalStore: base}, status: model.StageApprovalApproved, reached: make(chan struct{}), release: make(chan struct{})}
	service := NewStageApprovalService(paused)
	type result struct {
		gate *model.StageApproval
		err  error
	}
	done := make(chan result, 1)
	go func() { g, err := service.Get(ctx, "owned-run", "review"); done <- result{g, err} }()
	select {
	case <-paused.reached:
	case <-ctx.Done():
		t.Fatal("recovery did not reach conditional resolution")
	}
	winner, err := opener.Reject(ctx, "owned-run", "review", "winner@example.invalid", "veto")
	close(paused.release)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got.err != nil || got.gate == nil || got.gate.Status != model.StageApprovalRejected || got.gate.ResolvedBy != winner.ResolvedBy || got.gate.ResolvedAt == nil || !got.gate.ResolvedAt.Equal(*winner.ResolvedAt) {
			t.Fatalf("recovery replaced/reported the wrong terminal winner: %+v %v", got.gate, got.err)
		}
	case <-ctx.Done():
		t.Fatal("recovery did not finish")
	}
}
