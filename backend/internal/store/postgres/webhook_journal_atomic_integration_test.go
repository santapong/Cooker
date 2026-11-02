//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

func TestWebhookJournalPostgresDecisionCommitIsAtomicWithInboxAndIntent(t *testing.T) {
	db := claimTestDB(t)
	ctx := context.Background()
	s := NewTriggerStore(db)
	d, i := triggerRecordFixture(t, s)
	if _, err := db.Exec(`CREATE FUNCTION reject_fixture_decision() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture journal unavailable'; END $$; CREATE TRIGGER reject_fixture_decision BEFORE INSERT ON trigger_delivery_decisions FOR EACH ROW EXECUTE FUNCTION reject_fixture_decision()`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.RecordDelivery(ctx, d, []*model.TriggerIntent{i}); err == nil {
		t.Fatal("failed journal still admitted inbox")
	}
	if _, err := s.GetDelivery(ctx, d.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("partial inbox persisted", err)
	}
	if _, err := s.GetIntent(ctx, i.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("partial intent persisted", err)
	}
}

func TestWebhookJournalPostgresMigrationCutoverDoesNotInventLegacyHistory(t *testing.T) {
	db := claimTestDB(t)
	ctx := context.Background()
	s := NewTriggerStore(db)
	d, _ := triggerRecordFixture(t, s)
	if err := rollbackMigration(ctx, db, "036_trigger_audit_journal"); err != nil {
		t.Fatal(err)
	}
	// Seed rows through the old schema, before the journal exists. One retained
	// decision can be inspected; the already compacted row remains unknown.
	event, _ := json.Marshal(d.Event)
	decisions := []byte(`[{"ruleId":"rule","ruleVersion":2,"status":"ignored","reason":"ref matches an exclusion"}]`)
	for _, row := range []struct {
		id, providerID string
		decisions      []byte
		archived       bool
	}{{"legacy-retained", "legacy-provider", decisions, false}, {"legacy-compacted", "compacted-provider", []byte(`[]`), true}} {
		if _, err := db.Exec(`INSERT INTO webhook_deliveries(id,provider,connection_id,delivery_id,payload_hash,event,decisions,created_at,archived) VALUES($1,'github','connection',$2,'legacy-hash',$3,$4,$5,$6)`, row.id, row.providerID, event, row.decisions, time.Now().Add(-time.Hour), row.archived); err != nil {
			t.Fatal(err)
		}
	}
	before := time.Now().UTC()
	if err := applyMigrations(ctx, db); err != nil {
		t.Fatal(err)
	}
	var checkpoint time.Time
	if err := db.QueryRow(`SELECT started_at FROM trigger_audit_checkpoint WHERE id='v1'`).Scan(&checkpoint); err != nil || checkpoint.Before(before) || checkpoint.After(time.Now()) {
		t.Fatal("cutover is not the schema observation time", checkpoint, err)
	}
	for _, table := range []string{"trigger_delivery_decisions", "webhook_attempts"} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatal("migration invented historical records", table, count, err)
		}
	}
	page, err := s.ListTriggerHistory(ctx, store.TriggerHistoryQuery{DeliveryID: "legacy-retained"})
	if err != nil || page.Total != 1 || page.Items[0].MetadataCoverage != "legacy" || page.Items[0].TargetSource != "current_rule" || page.Items[0].Status != "ignored" {
		t.Fatal(page, err)
	}
	page, err = s.ListTriggerHistory(ctx, store.TriggerHistoryQuery{DeliveryID: "legacy-compacted"})
	if err != nil || page.Total != 1 || page.Items[0].Status != "archived" || page.Items[0].AppID != "" {
		t.Fatal("lost historical target was reconstructed", page, err)
	}
	if err := applyMigrations(ctx, db); err != nil {
		t.Fatal(err)
	}
	var replayed time.Time
	if err := db.QueryRow(`SELECT started_at FROM trigger_audit_checkpoint WHERE id='v1'`).Scan(&replayed); err != nil || !checkpoint.Equal(replayed) {
		t.Fatal("migration replay changed coverage start", replayed, err)
	}
	if _, _, err := s.RecordDelivery(ctx, d, nil); err != nil {
		t.Fatal("new delivery cannot record normalized decisions", err)
	}
	page, err = s.ListTriggerHistory(ctx, store.TriggerHistoryQuery{DeliveryID: d.ID})
	if err != nil || page.Total != 1 || page.Items[0].MetadataCoverage != "retained_decision" || page.Items[0].TargetSource != "decision" {
		t.Fatal(page, err)
	}
	// The optional audit schema can be removed on this disposable fixture;
	// existing inbox rows remain readable by the previous deployment.
	if err := rollbackMigration(ctx, db, "036_trigger_audit_journal"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetDelivery(ctx, "legacy-retained"); err != nil {
		t.Fatal("audit rollback removed existing inbox", err)
	}
}
