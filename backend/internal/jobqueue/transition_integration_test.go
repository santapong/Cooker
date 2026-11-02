//go:build integration

package jobqueue_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/jobqueue"
)

func TestLive_RecoveryDomainFailureRollsBackAndRetries(t *testing.T) {
	db := dialLive(t)
	st := jobqueue.NewPostgres(db)
	ctx := context.Background()
	j, _ := st.Enqueue(ctx, "deploy", nil, jobqueue.EnqueueOptions{ConcurrencyKey: "same-app"})
	_, _ = st.Dequeue(ctx, "old-owner", nil)
	st.OnTransition = func(context.Context, *sql.Tx, *jobqueue.Job) error {
		return errors.New("domain store temporarily unavailable")
	}
	if _, err := st.RecoverExpired(ctx, time.Now().Add(time.Second)); err == nil {
		t.Fatal("recovery swallowed domain failure")
	}
	got, _ := st.Get(ctx, j.ID)
	if got.Status != jobqueue.StatusRunning || got.LockedBy != "old-owner" {
		t.Fatal("partial recovery committed")
	}
	st.OnTransition = func(ctx context.Context, tx *sql.Tx, j *jobqueue.Job) error {
		_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS recovery_evidence (id text PRIMARY KEY, status text);`)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO recovery_evidence (id,status) VALUES ($1,$2) ON CONFLICT (id) DO UPDATE SET status=EXCLUDED.status`, j.ID, j.Status)
		return err
	}
	if _, err := st.RecoverExpired(ctx, time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := st.Resolve(ctx, j.ID, jobqueue.StatusSucceeded, "verified running service revision"); err != nil {
		t.Fatal(err)
	}
	if err := st.Resolve(ctx, j.ID, jobqueue.StatusFailed, "competing stale operator outcome"); !errors.Is(err, jobqueue.ErrNotLocked) {
		t.Fatal(err)
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM recovery_evidence WHERE id=$1`, j.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "succeeded" {
		t.Fatalf("domain outcome disagrees with queue: %s", status)
	}
}
