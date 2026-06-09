package memory

import (
	"context"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

func seedAuditEvents(t *testing.T) (*store.Store, time.Time) {
	t.Helper()
	st := New()
	ctx := context.Background()
	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	rows := []*model.AuditEvent{
		{Time: base, UserSub: "alice", Method: "POST", Path: "/api/v1/pipelines", Status: 201},
		{Time: base.Add(1 * time.Minute), UserSub: "bob", Method: "DELETE", Path: "/api/v1/apps/a1", Status: 204},
		{Time: base.Add(2 * time.Minute), UserSub: "alice", Method: "PUT", Path: "/api/v1/pipelines/p1", Status: 200},
		{Time: base.Add(3 * time.Minute), UserSub: "carol", Method: "POST", Path: "/api/v1/apps", Status: 201},
	}
	for _, r := range rows {
		if err := st.AuditEvents.Insert(ctx, r); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}
	return st, base
}

func TestAuditEvents_QueryNewestFirst(t *testing.T) {
	st, base := seedAuditEvents(t)
	got, err := st.AuditEvents.Query(context.Background(), store.AuditQuery{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d events, want 4", len(got))
	}
	if !got[0].Time.Equal(base.Add(3 * time.Minute)) {
		t.Fatalf("first event should be newest, got %v", got[0].Time)
	}
	if got[0].ID == 0 {
		t.Fatal("Insert should assign IDs")
	}
}

func TestAuditEvents_QueryFilters(t *testing.T) {
	st, base := seedAuditEvents(t)
	ctx := context.Background()
	from := base.Add(30 * time.Second)
	to := base.Add(150 * time.Second)

	cases := []struct {
		name string
		q    store.AuditQuery
		want []string // expected UserSub values, newest first
	}{
		{"by user", store.AuditQuery{UserSub: "alice"}, []string{"alice", "alice"}},
		{"by method", store.AuditQuery{Method: "DELETE"}, []string{"bob"}},
		{"by path prefix", store.AuditQuery{PathPrefix: "/api/v1/apps"}, []string{"carol", "bob"}},
		{"time window", store.AuditQuery{From: &from, To: &to}, []string{"alice", "bob"}},
		{"conjunctive", store.AuditQuery{UserSub: "alice", Method: "POST"}, []string{"alice"}},
		{"no match", store.AuditQuery{UserSub: "mallory"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := st.AuditEvents.Query(ctx, tc.q)
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d events, want %d", len(got), len(tc.want))
			}
			for i, sub := range tc.want {
				if got[i].UserSub != sub {
					t.Errorf("event %d: user %q, want %q", i, got[i].UserSub, sub)
				}
			}
		})
	}
}

func TestAuditEvents_QueryLimitOffset(t *testing.T) {
	st, base := seedAuditEvents(t)
	ctx := context.Background()

	page1, err := st.AuditEvents.Query(ctx, store.AuditQuery{Limit: 2})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	page2, err := st.AuditEvents.Query(ctx, store.AuditQuery{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(page1) != 2 || len(page2) != 2 {
		t.Fatalf("pages: %d, %d — want 2, 2", len(page1), len(page2))
	}
	if !page1[0].Time.Equal(base.Add(3*time.Minute)) || !page2[1].Time.Equal(base) {
		t.Fatal("paging must continue the newest-first order without gaps")
	}
	empty, err := st.AuditEvents.Query(ctx, store.AuditQuery{Offset: 99})
	if err != nil || len(empty) != 0 {
		t.Fatalf("offset past end: got %d events, err %v", len(empty), err)
	}
}

func TestAuditEvents_DeleteOlderThan(t *testing.T) {
	st, base := seedAuditEvents(t)
	ctx := context.Background()

	n, err := st.AuditEvents.DeleteOlderThan(ctx, base.Add(90*time.Second))
	if err != nil {
		t.Fatalf("DeleteOlderThan: %v", err)
	}
	if n != 2 {
		t.Fatalf("deleted %d, want 2", n)
	}
	rest, err := st.AuditEvents.Query(ctx, store.AuditQuery{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rest) != 2 {
		t.Fatalf("%d events remain, want 2", len(rest))
	}
	for _, e := range rest {
		if e.Time.Before(base.Add(90 * time.Second)) {
			t.Errorf("event at %v should have been swept", e.Time)
		}
	}
}

func TestAuditEvents_RingBound(t *testing.T) {
	st := New()
	ctx := context.Background()
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < auditRingMax+25; i++ {
		_ = st.AuditEvents.Insert(ctx, &model.AuditEvent{
			Time: base.Add(time.Duration(i) * time.Second), Method: "POST", Path: "/x",
		})
	}
	got, err := st.AuditEvents.Query(ctx, store.AuditQuery{Limit: 1})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if !got[0].Time.Equal(base.Add(time.Duration(auditRingMax+24) * time.Second)) {
		t.Fatal("ring must keep the newest events when full")
	}
	all, err := st.AuditEvents.Query(ctx, store.AuditQuery{Limit: auditRingMax * 2, Offset: 0})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(all) != auditRingMax {
		t.Fatalf("ring holds %d events, want %d", len(all), auditRingMax)
	}
}
