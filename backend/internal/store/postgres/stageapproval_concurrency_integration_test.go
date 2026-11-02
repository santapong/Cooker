//go:build integration

package postgres

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

func TestStageApprovalPostgresConcurrentVotesAndTerminalFence(t *testing.T) {
	db := nativeApprovalTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	gates := NewStageApprovalStore(db)
	const voters = 16
	gate := &model.StageApproval{ID: "owned-gate", RunID: "owned-run", StageID: "review", Status: model.StageApprovalAwaiting, RequiredApprovers: voters + 1}
	if err := gates.CreateGate(ctx, gate); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	counts := make(chan int, voters)
	errorsSeen := make(chan error, voters)
	for i := 0; i < voters; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			added, count, err := gates.AddVote(ctx, &model.StageApprovalVote{ID: fmt.Sprint("vote-", i), StageApprovalID: gate.ID, ApproverSub: fmt.Sprint("subject-", i)})
			if err != nil || !added {
				errorsSeen <- fmt.Errorf("distinct vote not recorded: added=%t err=%v", added, err)
				return
			}
			counts <- count
		}(i)
	}
	close(start)
	wg.Wait()
	close(counts)
	close(errorsSeen)
	for err := range errorsSeen {
		t.Error(err)
	}
	seen := map[int]bool{}
	for count := range counts {
		seen[count] = true
	}
	for count := 1; count <= voters; count++ {
		if !seen[count] {
			t.Errorf("concurrent counts omit %d: %v", count, seen)
		}
	}
	if t.Failed() {
		t.FailNow()
	}
	added, count, err := gates.AddVote(ctx, &model.StageApprovalVote{ID: "duplicate", StageApprovalID: gate.ID, ApproverSub: "subject-0"})
	if err != nil || added || count != voters {
		t.Fatalf("duplicate changed the count: added=%t count=%d err=%v", added, count, err)
	}

	type decision struct {
		status model.StageApprovalStatus
		by     string
		at     time.Time
		err    error
	}
	results := make(chan decision, voters)
	start = make(chan struct{})
	for i := 0; i < voters; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			status := model.StageApprovalApproved
			if i%2 == 0 {
				status = model.StageApprovalRejected
			}
			d := decision{status: status, by: fmt.Sprint("actor-", i), at: time.Now().UTC().Truncate(time.Microsecond)}
			d.err = gates.UpdateGateStatus(ctx, gate.ID, d.status, d.by, &d.at)
			results <- d
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	var winner decision
	wins := 0
	for d := range results {
		if d.err == nil {
			winner, wins = d, wins+1
		} else if !errors.Is(d.err, store.ErrConflict) {
			t.Errorf("late decision did not conflict: %v", d.err)
		}
	}
	if wins != 1 {
		t.Fatalf("expected exactly one terminal decision; got %d", wins)
	}
	persisted, err := gates.GetGate(ctx, gate.RunID, gate.StageID)
	if err != nil || persisted.Status != winner.status || persisted.ResolvedBy != winner.by || persisted.ResolvedAt == nil || !persisted.ResolvedAt.Equal(winner.at) || persisted.ApprovalCount() != voters {
		t.Fatalf("winning decision/count changed: %+v err=%v", persisted, err)
	}
	if _, _, err := gates.AddVote(ctx, &model.StageApprovalVote{ID: "late-vote", StageApprovalID: gate.ID, ApproverSub: "late-subject"}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("resolved gate accepted a late vote: %v", err)
	}
	if err := gates.UpdateGateStatus(ctx, gate.ID, model.StageApprovalAwaiting, "reset", nil); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("resolved gate reopened: %v", err)
	}
	if err := gates.UpdateGateStatus(ctx, "missing", model.StageApprovalApproved, "", nil); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing gate lost NotFound semantics: %v", err)
	}
}
