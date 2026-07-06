package server

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/audit"
	"github.com/santapong/cooker/internal/config"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
)

func TestAuditDestinations(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", []string{"stdout"}},
		{"stdout", []string{"stdout"}},
		{"db,stdout", []string{"db", "stdout"}},
		{" db , file ", []string{"db", "file"}},
		{",,", []string{"stdout"}},
	}
	for _, tc := range cases {
		if got := auditDestinations(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("auditDestinations(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
	if !auditHasDB("stdout,db") || auditHasDB("stdout,file") || auditHasDB("") {
		t.Fatal("auditHasDB misclassified a destination list")
	}
}

func TestNewAuditSink_Destinations(t *testing.T) {
	st := memory.New()

	t.Run("disabled", func(t *testing.T) {
		s, err := newAuditSink(config.AuditConfig{Enabled: false, Destination: "db"}, st)
		if err != nil || s != nil {
			t.Fatalf("disabled audit must yield nil sink: %v, %v", s, err)
		}
	})

	t.Run("unknown destination", func(t *testing.T) {
		if _, err := newAuditSink(config.AuditConfig{Enabled: true, Destination: "syslog"}, st); err == nil {
			t.Fatal("expected error for unknown destination")
		}
	})

	t.Run("db without audit store", func(t *testing.T) {
		bare := store.New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
		if _, err := newAuditSink(config.AuditConfig{Enabled: true, Destination: "db"}, bare); err == nil {
			t.Fatal("expected error when the store lacks audit support")
		}
	})

	t.Run("db sink persists through the store", func(t *testing.T) {
		s, err := newAuditSink(config.AuditConfig{Enabled: true, Destination: "db"}, st)
		if err != nil {
			t.Fatalf("newAuditSink: %v", err)
		}
		s.Emit(audit.Event{
			Time: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC), UserSub: "alice",
			Method: "POST", Path: "/api/v1/pipelines", Status: 201, Latency: "42ms", ClientIP: "10.0.0.1",
		})
		if err := s.Close(); err != nil { // Close drains the queue
			t.Fatalf("Close: %v", err)
		}
		got, err := st.AuditEvents.Query(context.Background(), store.AuditQuery{})
		if err != nil {
			t.Fatalf("Query: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("got %d persisted events, want 1", len(got))
		}
		e := got[0]
		if e.UserSub != "alice" || e.Method != "POST" || e.Status != 201 || e.LatencyMS != 42 || e.ClientIP != "10.0.0.1" {
			t.Fatalf("event mapped wrong: %+v", e)
		}
	})

	t.Run("comma list fans out", func(t *testing.T) {
		st2 := memory.New()
		s, err := newAuditSink(config.AuditConfig{Enabled: true, Destination: "db,stdout"}, st2)
		if err != nil {
			t.Fatalf("newAuditSink: %v", err)
		}
		s.Emit(audit.Event{Time: time.Now(), Method: "DELETE", Path: "/api/v1/apps/a1", Status: 204})
		if err := s.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		got, err := st2.AuditEvents.Query(context.Background(), store.AuditQuery{})
		if err != nil || len(got) != 1 {
			t.Fatalf("db leg of the fan-out missed the event: %d, %v", len(got), err)
		}
	})
}
