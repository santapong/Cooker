package neon

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestMemoryConnectionUpdatePreservesNewestRefreshAndPublicClones(t *testing.T) {
	s, _, c := connectionCoreFixture(t)
	proposed, err := s.Store.Get(context.Background(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := s.Sync(context.Background(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	proposed.Name = "Renamed"
	proposed.UpdatedAt = time.Now().UTC()
	if err = s.Store.Update(context.Background(), proposed, proposed.Version); err != nil {
		t.Fatal(err)
	}
	latest, err := s.Get(context.Background(), c.ID)
	if err != nil || latest.Name != "Renamed" || latest.Snapshot == nil || !latest.LastSuccessAt.Equal(*updated.LastSuccessAt) {
		t.Fatal("stale config proposal overwrote successful refresh", latest, err)
	}
	latest.ProjectIDs[0] = "project-two"
	latest.Snapshot.Projects[0].Name = "mutated"
	latest.Snapshot.Branches[0].Name = "mutated"
	*latest.LastSuccessAt = time.Time{}
	again, err := s.Get(context.Background(), c.ID)
	if err != nil || again.ProjectIDs[0] != "project-one" || again.Snapshot.Projects[0].Name == "mutated" || again.Snapshot.Branches[0].Name == "mutated" || again.LastSuccessAt.IsZero() {
		t.Fatal("nested public mutation reached store", again, err)
	}
}

func TestMemoryConnectionLeaseExpiryAndReplacedClaimCannotFinish(t *testing.T) {
	s, _, c := connectionCoreFixture(t)
	now := time.Now().UTC()
	if err := s.Store.Claim(context.Background(), c.ID, c.Version, "lease-one", now, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	requireConnectionCode(t, s.Store.Finish(context.Background(), c.ID, c.Version, "lease-one", nil, "provider_unavailable", now.Add(time.Second)), "conflict")
	if err := s.Store.Claim(context.Background(), c.ID, c.Version, "lease-two", now.Add(time.Second), now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	requireConnectionCode(t, s.Store.Finish(context.Background(), c.ID, c.Version, "lease-one", nil, "provider_unavailable", now.Add(time.Second)), "conflict")
	if err := s.Store.Finish(context.Background(), c.ID, c.Version, "lease-two", nil, "canceled", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	r, err := s.Store.Get(context.Background(), c.ID)
	if err != nil || r.LastSuccessAt != nil || r.Snapshot != nil || r.Lease != "" || r.ErrorCode != "canceled" {
		t.Fatal("failed lease fabricated success", r, err)
	}
}

func TestMemoryConnectionCountBoundAndPublicClockSanitization(t *testing.T) {
	s, _, c := connectionCoreFixture(t)
	base, err := s.Store.Get(context.Background(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	for index := 1; index < MaxNeonConnections; index++ {
		r := cloneConnectionRecord(base)
		r.ID = fmt.Sprintf("nconn_limit_%d", index)
		if err = s.Store.Create(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}
	last := cloneConnectionRecord(base)
	last.ID = "nconn_over_limit"
	requireConnectionCode(t, s.Store.Create(context.Background(), last), "connection_limit")
	rows, err := s.Store.List(context.Background())
	if err != nil || len(rows) != MaxNeonConnections {
		t.Fatal("connection inventory truncated", len(rows), err)
	}
	s, _, c = connectionCoreFixture(t)
	if _, err = s.Sync(context.Background(), c.ID); err != nil {
		t.Fatal(err)
	}
	good, err := s.Store.Get(context.Background(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"missing-success", "future-success", "future-observation", "future-attempt"} {
		r := cloneConnectionRecord(good)
		switch bad {
		case "missing-success":
			r.LastSuccessAt = nil
		case "future-success":
			value := time.Now().UTC().Add(time.Hour)
			r.LastSuccessAt = &value
		case "future-observation":
			r.Snapshot.ObservedAt = time.Now().UTC().Add(time.Hour)
		case "future-attempt":
			value := time.Now().UTC().Add(time.Hour)
			r.LastAttemptAt = &value
		}
		public := publicConnection(r, time.Now().UTC())
		if public.Snapshot != nil || public.LastSuccessAt != nil || public.State != "unavailable" || public.ErrorCode != "invalid_response" {
			t.Fatal("invalid successful clock remained public", bad, public)
		}
	}
}
