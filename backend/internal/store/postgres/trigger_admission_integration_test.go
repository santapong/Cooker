//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
)

func TestTriggerAdmissionAtomicallyCommitsQueueAndReceipt(t *testing.T) {
	db := claimTestDB(t)
	ctx := context.Background()
	inbox := NewTriggerStore(db)
	d, i := triggerRecordFixture(t, inbox)
	if _, _, err := inbox.RecordDelivery(ctx, d, []*model.TriggerIntent{i}); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"runId": i.RunID, "commit": i.Event.CommitSHA, "app": model.App{ID: i.Rule.AppID}, "actor": model.PipelineRun{}, "trigger": map[string]any{"intentId": i.ID, "ruleId": i.Rule.ID, "ruleVersion": i.Rule.Version, "envelopeHash": i.Rule.ApprovedEnvelopeHash}})
	queue := jobqueue.NewPostgres(db)
	queue.OnEnqueue = func(ctx context.Context, tx *sql.Tx, j *jobqueue.Job) error {
		if err := SyncTriggerAdmission(ctx, tx, j); err != nil {
			return err
		}
		return fmt.Errorf("injected crash before commit")
	}
	if _, err := queue.Enqueue(ctx, "app-deploy", payload, jobqueue.EnqueueOptions{ID: "app-run:" + i.RunID, ConcurrencyKey: "app:" + i.Rule.AppID}); err == nil {
		t.Fatal("injected transaction failure ignored")
	}
	if _, err := queue.Get(ctx, "app-run:"+i.RunID); !errors.Is(err, jobqueue.ErrNotFound) {
		t.Fatal("failed transaction left runnable job", err)
	}
	got, err := inbox.GetIntent(ctx, i.ID)
	if err != nil || got.Status != model.TriggerPending || got.Receipt != nil {
		t.Fatal("failed transaction advanced receipt", got, err)
	}
	queue.OnEnqueue = SyncTriggerAdmission
	j, err := queue.Enqueue(ctx, "app-deploy", payload, jobqueue.EnqueueOptions{ID: "app-run:" + i.RunID, ConcurrencyKey: "app:" + i.Rule.AppID})
	if err != nil {
		t.Fatal(err)
	}
	// Fresh store handles model a receiver restart after committing but before
	// replying. Digest must survive the JSONB representation returned by Get.
	got, err = NewTriggerStore(db).GetIntent(ctx, i.ID)
	if err != nil || got.Status != model.TriggerQueued || got.Receipt == nil {
		t.Fatal(got, err)
	}
	persisted, err := jobqueue.NewPostgres(db).Get(ctx, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := jobqueue.PayloadDigest(persisted.Payload)
	if err != nil || digest != got.Receipt.PlanDigest {
		t.Fatal("receipt digest changed after restart", err)
	}
	if got.Receipt.RunID != i.RunID || got.Receipt.PipelineID != "app-app-run" {
		t.Fatalf("identity changed: %+v", got.Receipt)
	}
}

func TestTriggerAdmissionRejectsDisabledRuleWithoutQueueing(t *testing.T) {
	db := claimTestDB(t)
	ctx := context.Background()
	inbox := NewTriggerStore(db)
	d, i := triggerRecordFixture(t, inbox)
	if _, _, err := inbox.RecordDelivery(ctx, d, []*model.TriggerIntent{i}); err != nil {
		t.Fatal(err)
	}
	r, _ := inbox.GetRule(ctx, i.Rule.ID)
	r.Enabled = false
	if err := inbox.PutRule(ctx, r); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"runId": i.RunID, "commit": i.Event.CommitSHA, "app": model.App{ID: i.Rule.AppID}, "actor": model.PipelineRun{}, "trigger": map[string]any{"intentId": i.ID, "ruleId": i.Rule.ID, "ruleVersion": i.Rule.Version}})
	queue := jobqueue.NewPostgres(db)
	queue.OnEnqueue = SyncTriggerAdmission
	if _, err := queue.Enqueue(ctx, "app-deploy", payload, jobqueue.EnqueueOptions{ID: "rejected"}); err == nil {
		t.Fatal("disabled rule queued")
	}
	if _, err := queue.Get(ctx, "rejected"); !errors.Is(err, jobqueue.ErrNotFound) {
		t.Fatal("rejected job persisted")
	}
}

func TestTriggerAdmissionRejectsMismatchedJobIdentityAndTargetScope(t *testing.T) {
	db := claimTestDB(t)
	ctx := context.Background()
	inbox := NewTriggerStore(db)
	d, i := triggerRecordFixture(t, inbox)
	if _, _, err := inbox.RecordDelivery(ctx, d, []*model.TriggerIntent{i}); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"runId": i.RunID, "commit": i.Event.CommitSHA, "app": model.App{ID: i.Rule.AppID}, "actor": model.PipelineRun{}, "trigger": map[string]any{"intentId": i.ID, "ruleId": i.Rule.ID, "ruleVersion": i.Rule.Version, "envelopeHash": i.Rule.ApprovedEnvelopeHash}})
	queue := jobqueue.NewPostgres(db)
	queue.OnEnqueue = SyncTriggerAdmission
	for _, opts := range []jobqueue.EnqueueOptions{{ID: "wrong-job", ConcurrencyKey: "app:" + i.Rule.AppID}, {ID: "app-run:" + i.RunID, ConcurrencyKey: "app:another-target"}} {
		if _, err := queue.Enqueue(ctx, "app-deploy", payload, opts); err == nil {
			t.Fatal("mismatched queue identity admitted")
		}
		if _, err := queue.Get(ctx, opts.ID); !errors.Is(err, jobqueue.ErrNotFound) {
			t.Fatal("rejected admission left a runnable job", err)
		}
		got, err := inbox.GetIntent(ctx, i.ID)
		if err != nil || got.Status != model.TriggerPending || got.Receipt != nil {
			t.Fatal("rejected admission advanced intent", got, err)
		}
	}
}
