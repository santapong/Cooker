//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/store"
)

// The parent and abruptly exiting child share only one random owned fixture
// schema. A fresh SQL pool cannot accept an owner DSN or arbitrary search path.
func atomicApprovalFreshDB(t *testing.T, schema string) *sql.DB {
	t.Helper()
	u, err := nativeApprovalFixtureURL(os.Getenv("COOKER_NATIVE_APPROVAL_TEST_DATABASE_URL"))
	if err != nil || !regexp.MustCompile(`^native_approval_[a-f0-9]{32}$`).MatchString(schema) {
		t.Fatal("refusing non-owned atomic approval fixture")
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal("opening owned approval pool failed")
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func atomicApprovalSchema(t *testing.T, db *sql.DB) string {
	t.Helper()
	var schema string
	if err := db.QueryRow(`SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	return schema
}

func atomicApprovalSeed(t *testing.T, db *sql.DB, id string, threshold int) *model.StageApproval {
	t.Helper()
	gate := &model.StageApproval{ID: id, RunID: id + "-run", StageID: "review", Status: model.StageApprovalAwaiting, RequiredApprovers: threshold}
	if err := NewStageApprovalStore(db).CreateGate(context.Background(), gate); err != nil {
		t.Fatal(err)
	}
	return gate
}

func atomicApprovalAssertListClosure(t *testing.T, gates *StageApprovalStore, expected *model.StageApproval) {
	t.Helper()
	listed, err := gates.ListGates(context.Background(), expected.RunID)
	if err != nil || len(listed) != 1 {
		t.Fatal("gate list lost committed closure", err)
	}
	actual := listed[0]
	if actual.ID != expected.ID || actual.RunID != expected.RunID || actual.StageID != expected.StageID || actual.Status != expected.Status || actual.RequiredApprovers != expected.RequiredApprovers || actual.ResolvedBy != expected.ResolvedBy || actual.ResolvedAt == nil || expected.ResolvedAt == nil || !actual.ResolvedAt.Equal(*expected.ResolvedAt) || actual.ApprovalCount() != expected.ApprovalCount() || !actual.UpdatedAt.Equal(expected.UpdatedAt) || !actual.CreatedAt.Equal(expected.CreatedAt) {
		t.Fatalf("gate list disagrees with committed closure: expected id=%s status=%s quorum=%d resolver=%q resolvedAt=%v updatedAt=%v votes=%d; actual id=%s status=%s quorum=%d resolver=%q resolvedAt=%v updatedAt=%v votes=%d", expected.ID, expected.Status, expected.RequiredApprovers, expected.ResolvedBy, expected.ResolvedAt, expected.UpdatedAt, expected.ApprovalCount(), actual.ID, actual.Status, actual.RequiredApprovers, actual.ResolvedBy, actual.ResolvedAt, actual.UpdatedAt, actual.ApprovalCount())
	}
	admitted := map[string]string{}
	for _, vote := range expected.Votes {
		admitted[vote.ID] = vote.ApproverSub
	}
	for _, vote := range actual.Votes {
		if subject, ok := admitted[vote.ID]; !ok || subject != vote.ApproverSub {
			t.Fatal("gate list substituted an admitted voter")
		}
	}
}

func atomicApprovalVote(gate *model.StageApproval, identity string) *model.StageApprovalVote {
	return &model.StageApprovalVote{ID: gate.ID + "-vote-" + identity, StageApprovalID: gate.ID, ApproverSub: identity, ApproverEmail: identity + "@fixture.invalid", Note: "Owned review"}
}

func TestStageApprovalPostgresFinalVoteSurvivesImmediateProcessExit(t *testing.T) {
	const childMode = "final-vote-exit"
	if os.Getenv("COOKER_STAGE_APPROVAL_ATOMIC_CHILD") == childMode {
		db := atomicApprovalFreshDB(t, os.Getenv("COOKER_STAGE_APPROVAL_ATOMIC_SCHEMA"))
		gate := &model.StageApproval{ID: "owned-process-exit"}
		added, count, err := NewStageApprovalStore(db).AddVote(context.Background(), atomicApprovalVote(gate, "final"))
		if err != nil || !added || count != 2 {
			t.Fatalf("final owned vote not committed: added=%t count=%d err=%v", added, count, err)
		}
		// No Get, service resolution, deferred Close or test cleanup occurs.
		// The process exits at the original crash window immediately after AddVote.
		os.Exit(0)
	}
	db := nativeApprovalTestDB(t)
	gate := atomicApprovalSeed(t, db, "owned-process-exit", 2)
	added, count, err := NewStageApprovalStore(db).AddVote(context.Background(), atomicApprovalVote(gate, "first"))
	if err != nil || !added || count != 1 {
		t.Fatal("first owned vote was not recorded", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStageApprovalPostgresFinalVoteSurvivesImmediateProcessExit$", "-test.v=false")
	cmd.Env = append(os.Environ(), "COOKER_STAGE_APPROVAL_ATOMIC_CHILD="+childMode, "COOKER_STAGE_APPROVAL_ATOMIC_SCHEMA="+atomicApprovalSchema(t, db))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("owned final-vote child failed: %v; output=%s", err, output)
	}
	fresh := atomicApprovalFreshDB(t, atomicApprovalSchema(t, db))
	persisted, err := NewStageApprovalStore(fresh).GetGate(ctx, gate.RunID, gate.StageID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != model.StageApprovalApproved || persisted.ApprovalCount() != 2 || persisted.ResolvedBy != "final@fixture.invalid" || persisted.ResolvedAt == nil {
		t.Fatalf("final vote committed without its terminal gate: status=%s votes=%d resolver=%q resolved=%t", persisted.Status, persisted.ApprovalCount(), persisted.ResolvedBy, persisted.ResolvedAt != nil)
	}
	seen := map[string]bool{}
	for _, vote := range persisted.Votes {
		seen[vote.ApproverSub] = true
	}
	if !seen["first"] || !seen["final"] {
		t.Fatal("fresh pool lost exact admitted voters")
	}
	atomicApprovalAssertListClosure(t, NewStageApprovalStore(fresh), persisted)
}

func TestStageApprovalPostgresAtomicDuplicateAndLateVote(t *testing.T) {
	db := nativeApprovalTestDB(t)
	gates := NewStageApprovalStore(db)
	gate := atomicApprovalSeed(t, db, "owned-duplicates", 2)
	first := atomicApprovalVote(gate, "first")
	if added, count, err := gates.AddVote(context.Background(), first); err != nil || !added || count != 1 {
		t.Fatal("initial vote failed", err)
	}
	duplicate := *first
	duplicate.ID = "owned-duplicate-attempt"
	if added, count, err := gates.AddVote(context.Background(), &duplicate); err != nil || added || count != 1 {
		t.Fatalf("duplicate awaiting vote changed count: added=%t count=%d err=%v", added, count, err)
	}
	before, err := gates.GetGate(context.Background(), gate.RunID, gate.StageID)
	if err != nil || before.Status != model.StageApprovalAwaiting || before.ResolvedAt != nil || before.ResolvedBy != "" {
		t.Fatal("duplicate prematurely resolved gate", err)
	}
	if added, count, err := gates.AddVote(context.Background(), atomicApprovalVote(gate, "final")); err != nil || !added || count != 2 {
		t.Fatal("final vote failed", err)
	}
	settled, err := gates.GetGate(context.Background(), gate.RunID, gate.StageID)
	if err != nil || settled.Status != model.StageApprovalApproved || settled.ApprovalCount() != 2 || settled.ResolvedBy != "final@fixture.invalid" || settled.ResolvedAt == nil {
		t.Fatal("final vote did not atomically approve", err)
	}
	for _, identity := range []string{"late", "final"} {
		if added, _, err := gates.AddVote(context.Background(), atomicApprovalVote(gate, identity)); added || !errors.Is(err, store.ErrConflict) {
			t.Fatal("resolved gate accepted a late/duplicate vote", err)
		}
	}
	if err := gates.UpdateGateStatus(context.Background(), gate.ID, model.StageApprovalRejected, "late-rejector", nil); !errors.Is(err, store.ErrConflict) {
		t.Fatal("rejection replaced approved winner", err)
	}
	after, err := gates.GetGate(context.Background(), gate.RunID, gate.StageID)
	if err != nil || after.Status != settled.Status || after.ResolvedBy != settled.ResolvedBy || after.ResolvedAt == nil || !after.ResolvedAt.Equal(*settled.ResolvedAt) || after.ApprovalCount() != 2 {
		t.Fatal("late operations changed terminal evidence", err)
	}
}

func TestStageApprovalPostgresCanceledBlockedVoteLeavesNoAdmission(t *testing.T) {
	db := nativeApprovalTestDB(t)
	gate := atomicApprovalSeed(t, db, "owned-canceled-vote", 2)
	gates := NewStageApprovalStore(db)
	if _, _, err := gates.AddVote(context.Background(), atomicApprovalVote(gate, "first")); err != nil {
		t.Fatal(err)
	}
	lock, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback()
	var status string
	if err := lock.QueryRow(`SELECT status FROM stage_approvals WHERE id=$1 FOR UPDATE`, gate.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	worker := atomicApprovalFreshDB(t, atomicApprovalSchema(t, db))
	var pid int
	if err := worker.QueryRow(`SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	type result struct {
		added bool
		count int
		err   error
	}
	done := make(chan result, 1)
	go func() {
		added, count, err := NewStageApprovalStore(worker).AddVote(ctx, atomicApprovalVote(gate, "canceled-final"))
		done <- result{added, count, err}
	}()
	// Observe the actual PostgreSQL lock wait before cancellation; no timing-only
	// assumption that the final vote has reached its transaction is sufficient.
	deadline := time.Now().Add(5 * time.Second)
	for {
		var blocked bool
		if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock')`, pid).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("owned canceled vote did not reach gate lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	outcome := <-done
	if outcome.err == nil || outcome.added || outcome.count != 0 {
		t.Fatal("canceled blocked vote reported admission")
	}
	if err := lock.Rollback(); err != nil {
		t.Fatal(err)
	}
	current, err := gates.GetGate(context.Background(), gate.RunID, gate.StageID)
	if err != nil || current.Status != model.StageApprovalAwaiting || current.ApprovalCount() != 1 || current.ResolvedAt != nil || current.ResolvedBy != "" {
		t.Fatal("canceled vote changed persisted gate", err)
	}
}

func TestStageApprovalPostgresResolutionFailureRollsBackFinalVote(t *testing.T) {
	db := nativeApprovalTestDB(t)
	gate := atomicApprovalSeed(t, db, "owned-failed-resolution", 2)
	gates := NewStageApprovalStore(db)
	if _, _, err := gates.AddVote(context.Background(), atomicApprovalVote(gate, "first")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE FUNCTION owned_block_approval_resolution() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'owned resolution unavailable'; END $$; CREATE TRIGGER owned_block_approval_resolution BEFORE UPDATE ON stage_approvals FOR EACH ROW WHEN (NEW.status='approved') EXECUTE FUNCTION owned_block_approval_resolution()`); err != nil {
		t.Fatal(err)
	}
	if added, _, err := gates.AddVote(context.Background(), atomicApprovalVote(gate, "final")); err == nil || added {
		t.Fatal("failed resolution committed its vote")
	}
	current, err := gates.GetGate(context.Background(), gate.RunID, gate.StageID)
	if err != nil || current.Status != model.StageApprovalAwaiting || current.ApprovalCount() != 1 || current.ResolvedAt != nil {
		t.Fatal("resolution failure split vote and gate commits", err)
	}
	if _, err := db.Exec(`DROP TRIGGER owned_block_approval_resolution ON stage_approvals; DROP FUNCTION owned_block_approval_resolution()`); err != nil {
		t.Fatal(err)
	}
	if added, count, err := gates.AddVote(context.Background(), atomicApprovalVote(gate, "final")); err != nil || !added || count != 2 {
		t.Fatal("rollback did not permit the exact vote retry", err)
	}
}

func TestStageApprovalPostgresFinalVoteVersusRejectPreservesFirstTerminal(t *testing.T) {
	db := nativeApprovalTestDB(t)
	gates := NewStageApprovalStore(db)
	for i := 0; i < 12; i++ {
		t.Run(fmt.Sprint("race-", i), func(t *testing.T) {
			gate := atomicApprovalSeed(t, db, fmt.Sprint("owned-race-", i), 2)
			if _, _, err := gates.AddVote(context.Background(), atomicApprovalVote(gate, "first")); err != nil {
				t.Fatal(err)
			}
			start := make(chan struct{})
			var wg sync.WaitGroup
			wg.Add(2)
			var added bool
			var count int
			var voteErr, rejectErr error
			rejectionAt := time.Now().UTC().Truncate(time.Microsecond)
			go func() {
				defer wg.Done()
				<-start
				added, count, voteErr = gates.AddVote(context.Background(), atomicApprovalVote(gate, "final"))
			}()
			go func() {
				defer wg.Done()
				<-start
				rejectErr = gates.UpdateGateStatus(context.Background(), gate.ID, model.StageApprovalRejected, "rejector@fixture.invalid", &rejectionAt)
			}()
			close(start)
			wg.Wait()
			persisted, err := gates.GetGate(context.Background(), gate.RunID, gate.StageID)
			if err != nil || persisted.ResolvedAt == nil {
				t.Fatal("terminal evidence unavailable", err)
			}
			switch persisted.Status {
			case model.StageApprovalApproved:
				if voteErr != nil || !added || count != 2 || !errors.Is(rejectErr, store.ErrConflict) || persisted.ResolvedBy != "final@fixture.invalid" || persisted.ApprovalCount() != 2 {
					t.Fatalf("atomic approval winner changed: vote=%v reject=%v votes=%d", voteErr, rejectErr, persisted.ApprovalCount())
				}
			case model.StageApprovalRejected:
				if rejectErr != nil || !errors.Is(voteErr, store.ErrConflict) || added || persisted.ResolvedBy != "rejector@fixture.invalid" || persisted.ApprovalCount() != 1 || !persisted.ResolvedAt.Equal(rejectionAt) {
					t.Fatalf("rejection winner changed: vote=%v reject=%v votes=%d", voteErr, rejectErr, persisted.ApprovalCount())
				}
			default:
				t.Fatal("neither final operation resolved its gate")
			}
		})
	}
}

func TestStageApprovalPostgresLegacyQuorumRecoveredByFreshService(t *testing.T) {
	for _, method := range []string{"await", "get"} {
		t.Run(method, func(t *testing.T) {
			db := nativeApprovalTestDB(t)
			gate := atomicApprovalSeed(t, db, "owned-legacy-quorum", 2)
			for _, identity := range []string{"first", "final"} {
				vote := atomicApprovalVote(gate, identity)
				if _, err := db.Exec(`INSERT INTO stage_approval_votes(id,stage_approval_id,approver_sub,approver_email,note) VALUES($1,$2,$3,$4,$5)`, vote.ID, vote.StageApprovalID, vote.ApproverSub, vote.ApproverEmail, vote.Note); err != nil {
					t.Fatal(err)
				}
			}
			fresh := atomicApprovalFreshDB(t, atomicApprovalSchema(t, db))
			s := service.NewStageApprovalService(NewStageApprovalStore(fresh))
			before := time.Now().UTC().Add(-time.Second)
			var recovered *model.StageApproval
			var err error
			if method == "await" {
				recovered, err = s.Await(context.Background(), gate.RunID, gate.StageID, 99)
			} else {
				recovered, err = s.Get(context.Background(), gate.RunID, gate.StageID)
			}
			if err != nil || recovered.Status != model.StageApprovalApproved || recovered.ApprovalCount() != 2 || recovered.RequiredApprovers != 2 || recovered.ResolvedBy != "" || recovered.ResolvedAt == nil || recovered.ResolvedAt.Before(before) {
				t.Fatalf("legacy quorum did not recover truthfully: %+v err=%v", recovered, err)
			}
			atomicApprovalAssertListClosure(t, NewStageApprovalStore(fresh), recovered)
			again, err := s.Get(context.Background(), gate.RunID, gate.StageID)
			if err != nil || again.ResolvedAt == nil || !again.ResolvedAt.Equal(*recovered.ResolvedAt) || again.ResolvedBy != "" || again.ApprovalCount() != 2 {
				t.Fatal("repeated recovery restamped legacy evidence", err)
			}
		})
	}
}

func TestStageApprovalPostgresDeferredCommitFailureRollsBackClosure(t *testing.T) {
	db := nativeApprovalTestDB(t)
	gate := atomicApprovalSeed(t, db, "owned-failed-commit", 2)
	gates := NewStageApprovalStore(db)
	if _, _, err := gates.AddVote(context.Background(), atomicApprovalVote(gate, "first")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE FUNCTION owned_block_approval_commit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'owned commit unavailable'; END $$; CREATE CONSTRAINT TRIGGER owned_block_approval_commit AFTER UPDATE ON stage_approvals DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.status='approved') EXECUTE FUNCTION owned_block_approval_commit()`); err != nil {
		t.Fatal(err)
	}
	if added, _, err := gates.AddVote(context.Background(), atomicApprovalVote(gate, "final")); err == nil || added {
		t.Fatal("failed commit reported its final vote accepted")
	}
	current, err := gates.GetGate(context.Background(), gate.RunID, gate.StageID)
	if err != nil || current.Status != model.StageApprovalAwaiting || current.ApprovalCount() != 1 || current.ResolvedAt != nil || current.ResolvedBy != "" {
		t.Fatal("failed commit persisted a partial closure", err)
	}
	if _, err := db.Exec(`DROP TRIGGER owned_block_approval_commit ON stage_approvals; DROP FUNCTION owned_block_approval_commit()`); err != nil {
		t.Fatal(err)
	}
	if added, count, err := gates.AddVote(context.Background(), atomicApprovalVote(gate, "final")); err != nil || !added || count != 2 {
		t.Fatal("commit rollback did not allow exact final-vote retry", err)
	}
}

func TestStageApprovalPostgresSkippedResolutionUpdateCannotCommitFinalVote(t *testing.T) {
	db := nativeApprovalTestDB(t)
	gate := atomicApprovalSeed(t, db, "owned-skipped-resolution", 2)
	gates := NewStageApprovalStore(db)
	if _, _, err := gates.AddVote(context.Background(), atomicApprovalVote(gate, "first")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE FUNCTION owned_skip_approval_resolution() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NULL; END $$; CREATE TRIGGER owned_skip_approval_resolution BEFORE UPDATE ON stage_approvals FOR EACH ROW WHEN (NEW.status='approved') EXECUTE FUNCTION owned_skip_approval_resolution()`); err != nil {
		t.Fatal(err)
	}
	if added, _, err := gates.AddVote(context.Background(), atomicApprovalVote(gate, "final")); err == nil || added {
		t.Fatal("skipped quorum update committed its final vote")
	}
	current, err := gates.GetGate(context.Background(), gate.RunID, gate.StageID)
	if err != nil || current.Status != model.StageApprovalAwaiting || current.ApprovalCount() != 1 || current.ResolvedAt != nil || current.ResolvedBy != "" {
		t.Fatal("skipped update persisted a partial closure", err)
	}
}

func atomicApprovalSeedLegacy(t *testing.T, db *sql.DB, id string) *model.StageApproval {
	t.Helper()
	gate := atomicApprovalSeed(t, db, id, 2)
	for _, identity := range []string{"first", "final"} {
		vote := atomicApprovalVote(gate, identity)
		if _, err := db.Exec(`INSERT INTO stage_approval_votes(id,stage_approval_id,approver_sub,approver_email,note) VALUES($1,$2,$3,$4,$5)`, vote.ID, vote.StageApprovalID, vote.ApproverSub, vote.ApproverEmail, vote.Note); err != nil {
			t.Fatal(err)
		}
	}
	return gate
}

func TestStageApprovalPostgresLegacyVoteRepair(t *testing.T) {
	for _, identity := range []string{"first", "late-distinct"} {
		t.Run(identity, func(t *testing.T) {
			db := nativeApprovalTestDB(t)
			gate := atomicApprovalSeedLegacy(t, db, "owned-direct-legacy")
			gates := NewStageApprovalStore(db)
			added, count, err := gates.AddVote(context.Background(), atomicApprovalVote(gate, identity))
			if err != nil || added || count != 2 {
				t.Fatalf("legacy quorum repair admitted another vote: added=%t count=%d err=%v", added, count, err)
			}
			fresh := NewStageApprovalStore(atomicApprovalFreshDB(t, atomicApprovalSchema(t, db)))
			repaired, err := fresh.GetGate(context.Background(), gate.RunID, gate.StageID)
			if err != nil || repaired.Status != model.StageApprovalApproved || repaired.ApprovalCount() != 2 || repaired.RequiredApprovers != 2 || repaired.ResolvedBy != "" || repaired.ResolvedAt == nil {
				t.Fatalf("legacy repair invented a final voter or changed policy: %+v err=%v", repaired, err)
			}
			atomicApprovalAssertListClosure(t, fresh, repaired)
		})
	}
}

func TestStageApprovalPostgresLegacyApproveRecoversBeforeVote(t *testing.T) {
	for _, identity := range []string{"first", "late-distinct"} {
		t.Run(identity, func(t *testing.T) {
			db := nativeApprovalTestDB(t)
			gate := atomicApprovalSeedLegacy(t, db, "owned-service-legacy")
			gates := NewStageApprovalStore(db)
			s := service.NewStageApprovalService(gates)
			result, err := s.Approve(context.Background(), gate.RunID, gate.StageID, identity, identity+"@fixture.invalid", "Owned late note")
			if !errors.Is(err, service.ErrGateResolved) || result == nil || result.Status != model.StageApprovalApproved || result.ApprovalCount() != 2 || result.ResolvedBy != "" {
				t.Fatalf("legacy approval accepted a late decision or invented resolver: result=%+v err=%v", result, err)
			}
			fresh := NewStageApprovalStore(atomicApprovalFreshDB(t, atomicApprovalSchema(t, db)))
			persisted, err := fresh.GetGate(context.Background(), gate.RunID, gate.StageID)
			if err != nil || persisted.ApprovalCount() != 2 || persisted.ResolvedBy != "" {
				t.Fatal("late approval changed historical quorum", err)
			}
			atomicApprovalAssertListClosure(t, fresh, result)
		})
	}
}

type atomicApprovalRejectInterleaveStore struct {
	store.StageApprovalStore
	beforeCAS func(context.Context) error
}

func (s *atomicApprovalRejectInterleaveStore) UpdateGateStatus(ctx context.Context, id string, status model.StageApprovalStatus, by string, at *time.Time) error {
	if s.beforeCAS != nil && status == model.StageApprovalRejected {
		if err := s.beforeCAS(ctx); err != nil {
			return err
		}
	}
	return s.StageApprovalStore.UpdateGateStatus(ctx, id, status, by, at)
}

func TestStageApprovalPostgresServiceRejectReturnsStoredClosure(t *testing.T) {
	for _, interleave := range []bool{false, true} {
		t.Run(fmt.Sprint("partial-during-CAS-", interleave), func(t *testing.T) {
			db := nativeApprovalTestDB(t)
			gate := atomicApprovalSeed(t, db, "owned-reject-observation", 3)
			gates := NewStageApprovalStore(db)
			if _, _, err := gates.AddVote(context.Background(), atomicApprovalVote(gate, "first")); err != nil {
				t.Fatal(err)
			}
			wrapped := &atomicApprovalRejectInterleaveStore{StageApprovalStore: gates}
			if interleave {
				wrapped.beforeCAS = func(ctx context.Context) error {
					added, count, err := gates.AddVote(ctx, atomicApprovalVote(gate, "second"))
					if err != nil || !added || count != 2 {
						return fmt.Errorf("owned partial vote interleave failed: added=%t count=%d err=%v", added, count, err)
					}
					return nil
				}
			}
			s := service.NewStageApprovalService(wrapped)
			rejected, err := s.Reject(context.Background(), gate.RunID, gate.StageID, "rejector@fixture.invalid", "Owned veto")
			if err != nil || rejected == nil || rejected.Status != model.StageApprovalRejected || rejected.ResolvedBy != "rejector@fixture.invalid" {
				t.Fatal("owned service rejection did not settle", err)
			}
			expectedVotes := 1
			if interleave {
				expectedVotes = 2
			}
			if rejected.ApprovalCount() != expectedVotes {
				t.Fatalf("rejection returned an outdated vote snapshot: votes=%d expected=%d", rejected.ApprovalCount(), expectedVotes)
			}
			fresh := NewStageApprovalStore(atomicApprovalFreshDB(t, atomicApprovalSchema(t, db)))
			atomicApprovalAssertListClosure(t, fresh, rejected)
			persisted, err := fresh.GetGate(context.Background(), gate.RunID, gate.StageID)
			if err != nil || persisted.ResolvedAt == nil || rejected.ResolvedAt == nil || !persisted.ResolvedAt.Equal(*rejected.ResolvedAt) || !persisted.UpdatedAt.Equal(rejected.UpdatedAt) {
				t.Fatal("reject result differs from persisted Get closure", err)
			}
		})
	}
}
