//go:build integration

package postgres

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/store"
)

func TestAppEnvironmentPreflightPostgresOutcomeAndRecovery(t *testing.T) {
	for _, mode := range []string{"complete", "lost-completion", "storage-fault", "reason-conflict", "stale-claim", "invalid-marker"} {
		t.Run(mode, func(t *testing.T) {
			f := newPreflightPGFixture(t, time.Now().UTC().Truncate(time.Microsecond), true)
			f.f.Reason = store.AppPreflightEnvironmentGeneration
			ctx := context.Background()
			if mode == "storage-fault" {
				if _, err := f.db.Exec(`CREATE FUNCTION environment_history_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic-outcome-storage-error'; END $$; CREATE TRIGGER environment_history_fault BEFORE INSERT ON app_deploys FOR EACH ROW EXECUTE FUNCTION environment_history_fault()`); err != nil {
					t.Fatal("fixture trigger unavailable")
				}
				if err := NewAppDeployStore(f.db).RecordAppPreflightFailure(f.ctx, f.f); err == nil {
					t.Fatal("history write failure committed")
				}
				f.assertPendingNoHistory(t)
				return
			}
			if mode == "stale-claim" {
				if err := f.jobs.Suspend(ctx, "worker", "app-run:run", "claim revoked"); err != nil {
					t.Fatal(err)
				}
				before, _ := NewRunStore(f.db).Get(ctx, f.f.RunID)
				beforeRows, err := NewAppDeployStore(f.db).ListByApp(ctx, f.f.AppID, 100)
				if err != nil {
					t.Fatal(err)
				}
				if err := NewAppDeployStore(f.db).RecordAppPreflightFailure(f.ctx, f.f); !errors.Is(err, store.ErrConflict) {
					t.Fatal("revoked worker wrote rejection")
				}
				after, _ := NewRunStore(f.db).Get(ctx, f.f.RunID)
				rows, _ := NewAppDeployStore(f.db).ListByApp(ctx, f.f.AppID, 100)
				if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(beforeRows, rows) {
					t.Fatal("revoked history write changed outcome")
				}
				return
			}
			if err := NewAppDeployStore(f.db).RecordAppPreflightFailure(f.ctx, f.f); err != nil {
				t.Fatal(err)
			}
			before, row := f.assertRejected(t)
			if mode == "reason-conflict" {
				wrong := f.f
				wrong.Reason = store.AppPreflightRegistryAuthority
				if err := NewAppDeployStore(f.db).RecordAppPreflightFailure(f.ctx, wrong); !errors.Is(err, store.ErrConflict) {
					t.Fatal("different rejection reason replaced decision")
				}
			} else if mode == "invalid-marker" {
				if _, err := f.db.Exec(`UPDATE app_deploys SET image_ref='possible-effect' WHERE id=$1`, row.ID); err != nil {
					t.Fatal(err)
				}
				if err := f.jobs.Suspend(ctx, "worker", "app-run:run", "completion lost"); err == nil {
					t.Fatal("partial marker was treated as determinate")
				}
				got, _ := f.jobs.Get(ctx, "app-run:run")
				if got.Status != jobqueue.StatusRunning {
					t.Fatal("invalid recovery did not roll back queue transition")
				}
				return
			} else {
				for range 3 {
					if err := NewAppDeployStore(f.db).RecordAppPreflightFailure(f.ctx, f.f); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "complete" {
					if err := f.jobs.Complete(ctx, "worker", "app-run:run"); err != nil {
						t.Fatal(err)
					}
				} else if err := f.jobs.Suspend(ctx, "worker", "app-run:run", "completion lost"); err != nil {
					t.Fatal(err)
				}
				job, _ := f.jobs.Get(ctx, "app-run:run")
				want := jobqueue.StatusSucceeded
				if mode == "lost-completion" {
					want = jobqueue.StatusNeedsAttention
				}
				if job.Status != want {
					t.Fatal("queue acknowledgement and business outcome confused")
				}
			}
			after, afterRow := f.assertRejected(t)
			if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(row, afterRow) || after.Error != store.AppPreflightEnvironmentSummary {
				t.Fatal("retry/recovery rewrote exact rejected decision")
			}
		})
	}
}
