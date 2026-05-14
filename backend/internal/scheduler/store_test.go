package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"
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
