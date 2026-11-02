package store

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/model"
)

func TestTriggerHistoryPresentationLifecycleAndSecrecy(t *testing.T) {
	for _, test := range []struct{ decision, intent, job, run, want string }{
		{"pending", "pending", "", "", "accepted"}, {"pending", "queued", "succeeded", "failed", "failed"}, {"pending", "queued", "failed", "", "failed"}, {"pending", "queued", "needs_attention", "running", "review_required"}, {"duplicate", "queued", "failed", "failed", "duplicate"}, {"ignored", "", "", "", "ignored"}, {"pending", "superseded", "", "", "stale"}, {"pending", "awaiting_review", "", "", "review_required"}, {"pending", "failed_preparation", "", "", "failed"}, {"rejected", "", "", "", "rejected"}, {"future", "", "", "", "unknown"},
	} {
		t.Run(test.want+test.decision+test.job, func(t *testing.T) {
			row := PresentTriggerHistory(model.TriggerHistoryRow{StoredStatus: test.decision, StoredReason: "synthetic-secret-canary", IntentStatus: test.intent, JobStatus: test.job, RunStatus: test.run})
			if row.Status != test.want {
				t.Fatalf("status=%q want %q", row.Status, test.want)
			}
			b, _ := json.Marshal(row)
			if strings.Contains(string(b), "synthetic-secret-canary") || row.StoredReason != "" || row.StoredStatus != "" {
				t.Fatal("private projection input retained")
			}
		})
	}
	row := PresentTriggerHistory(model.TriggerHistoryRow{StoredStatus: "ignored", StoredReason: "ref matches an exclusion"})
	if row.ReasonCode != "ref_excluded" {
		t.Fatalf("lost safe reason: %+v", row)
	}
}

func TestTriggerHistoryQueryBounds(t *testing.T) {
	if q, err := (TriggerHistoryQuery{}).Normalize(); err != nil || q.Limit != 25 {
		t.Fatal(q, err)
	}
	for _, q := range []TriggerHistoryQuery{{Limit: -1}, {Limit: 101}, {Offset: -1}, {Offset: 1000001}, {Provider: "vercel"}, {Status: "private-error"}, {Search: strings.Repeat("x", 257)}, {Ref: "refs/heads/main\n"}} {
		if _, err := q.Normalize(); err == nil {
			t.Fatalf("invalid filter accepted: %+v", q)
		}
	}
}
