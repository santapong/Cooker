package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/service"
)

func TestMemoryStoreCRUD(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	now := time.Now()
	if err := s.Create(ctx, Schedule{ID: "s1", PipelineID: "p", CronExpr: "* * * * *", NextRunAt: now, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, Schedule{ID: "s1"}); err == nil {
		t.Fatal("want duplicate-id error")
	}
	got, err := s.Get(ctx, "s1")
	if err != nil || got.PipelineID != "p" {
		t.Fatalf("Get: %+v err=%v", got, err)
	}
	due, err := s.DueBefore(ctx, now.Add(time.Minute))
	if err != nil || len(due) != 1 {
		t.Fatalf("DueBefore: %v len=%d", err, len(due))
	}
	if err := s.MarkFired(ctx, "s1", now, "r1", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(ctx, "s1")
	if got.LastRunID != "r1" || got.LastRunAt == nil {
		t.Fatalf("after MarkFired: %+v", got)
	}
	if err := s.Delete(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, "s1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestMemoryStoreDueBeforeRespectsEnabled(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	now := time.Now()
	_ = s.Create(ctx, Schedule{ID: "on", NextRunAt: now, Enabled: true})
	_ = s.Create(ctx, Schedule{ID: "off", NextRunAt: now, Enabled: false})
	due, _ := s.DueBefore(ctx, now.Add(time.Second))
	if len(due) != 1 || due[0].ID != "on" {
		t.Fatalf("due=%+v want only 'on'", due)
	}
}

func TestScheduleLoadLocationFallsBackToUTC(t *testing.T) {
	s := Schedule{Timezone: ""}
	if s.LoadLocation() != time.UTC {
		t.Fatal("empty TZ should yield UTC")
	}
	bad := Schedule{Timezone: "made/up"}
	if bad.LoadLocation() != time.UTC {
		t.Fatal("bad TZ should yield UTC")
	}
}

func TestScheduleEditsRejectStaleVersion(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()
	now := time.Now().UTC()
	if err := st.Create(ctx, Schedule{ID: "s", PipelineID: "p", NextRunAt: now, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	before, _ := st.Get(ctx, "s")
	if before.Version != 1 {
		t.Fatalf("initial version=%d", before.Version)
	}
	if err := st.MarkFired(ctx, "s", now, "r", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	before.Enabled = false
	if err := st.Update(ctx, before); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale edit=%v, want conflict", err)
	}
	after, _ := st.Get(ctx, "s")
	if after.Version != 2 || !after.Enabled || after.LastRunID != "r" || !after.NextRunAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("stale edit overwrote fire: %+v", after)
	}
	after.Enabled = false
	if err := st.Update(ctx, after); err != nil {
		t.Fatal(err)
	}
	updated, _ := st.Get(ctx, "s")
	if updated.Enabled || updated.Version != 3 || updated.LastRunID != "r" {
		t.Fatalf("fresh edit/history=%+v", updated)
	}
}

func TestOccurrenceIdentityUsesDueSlot(t *testing.T) {
	due := time.Date(2026, 10, 2, 13, 0, 0, 123456000, time.UTC)
	first := service.ScheduledOccurrence{ScheduleID: "s", ScheduleVersion: 1, ScheduledFor: due, PipelineID: "p"}
	retry := first
	retry.ScheduleVersion = 2
	retry.PipelineID = "changed"
	retry.AdmittedAt = due.Add(time.Hour)
	retry.ScheduledFor = due.In(time.FixedZone("ICT", 7*3600))
	if first.RunID() != retry.RunID() {
		t.Fatal("retry/edit/zone changed the occurrence identity")
	}
	retry.ScheduledFor = due.Add(time.Minute)
	if first.RunID() == retry.RunID() {
		t.Fatal("another due slot reused the occurrence")
	}
	retry = first
	retry.ScheduleID = "other"
	if first.RunID() == retry.RunID() {
		t.Fatal("another schedule reused the occurrence")
	}
}
