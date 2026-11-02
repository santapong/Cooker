package memory

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

func checkoutReceiptSource() *model.PipelineRunSource {
	return &model.PipelineRunSource{Source: model.GitSourceIdentity{Provider: "github", ConnectionID: "approved", Host: "github.com", RepositoryID: "owned-123", Repository: "owned/native"}, FullRef: "refs/heads/main", CommitSHA: strings.Repeat("a", 40), SourcePlanDigest: strings.Repeat("b", 64)}
}
func checkoutReceiptFixture(t *testing.T) (*store.Store, *jobqueue.Memory, *jobqueue.Job, context.Context, *model.PipelineRun, *model.PipelineRunSource) {
	t.Helper()
	ctx := context.Background()
	st := New()
	jobs := jobqueue.NewMemory()
	ts := time.Date(2026, 10, 3, 2, 3, 4, 123456789, time.UTC)
	r := &model.PipelineRun{ID: "owned-checkout", PipelineID: "owned-pipeline", Status: model.RunStatusRunning, CreatedAt: ts.Add(-time.Second), StartedAt: &ts, Source: checkoutReceiptSource(), StartedByUserSub: "fixture-admin", StartedByEmail: "fixture@invalid.local", StartedByGroups: []string{"admin"}, StartedByTokenHash: strings.Repeat("c", 64), PipelineVersion: 1, Variables: map[string]string{"CONFIG_NAME": "owned"}, StageRuns: []model.StageRun{{StageID: "build", Status: model.RunStatusRunning, StartedAt: &ts, Logs: "existing bounded evidence", Outputs: map[string]string{"proof": "preserve"}}}}
	if err := st.Runs.Create(ctx, r); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]string{"runId": r.ID})
	if _, err := jobs.Enqueue(ctx, "pipeline-run", payload, jobqueue.EnqueueOptions{ID: "run:" + r.ID, ConcurrencyKey: "pipeline:" + r.PipelineID}); err != nil {
		t.Fatal(err)
	}
	j, err := jobs.Dequeue(ctx, "owned-checkout-worker", []string{"pipeline-run"})
	if err != nil || j == nil {
		t.Fatal("real memory claim unavailable")
	}
	claim := store.WithRunClaim(ctx, j.ID, j.LockedBy, func(c context.Context, w func() error) error { return jobs.WithClaim(c, j.LockedBy, j.ID, w) })
	observed := r.Source.Clone()
	checked := ts.Add(time.Minute)
	observed.CheckedOutAt = &checked
	return st, jobs, j, claim, r, observed
}
func TestSourceCheckoutMemoryReceiptPreservesOtherFieldsAndIsolation(t *testing.T) {
	st, _, _, claim, _, observed := checkoutReceiptFixture(t)
	recorder := st.Runs.(store.RunSourceCheckoutRecorder)
	before, _ := st.Runs.Get(claim, "owned-checkout")
	if err := recorder.RecordSourceCheckout(claim, before.ID, observed); err != nil {
		t.Fatal(err)
	}
	after, _ := st.Runs.Get(claim, before.ID)
	want := before.Clone()
	want.Source = observed.Clone()
	if !reflect.DeepEqual(after, want) {
		t.Fatal("source-only write changed unrelated run evidence")
	}
	if err := recorder.RecordSourceCheckout(claim, before.ID, observed); err != nil {
		t.Fatal("same observation was not idempotent", err)
	}
	checked := *observed.CheckedOutAt
	observed.Source.Repository = "foreign/changed"
	*observed.CheckedOutAt = checked.Add(time.Second)
	*after.Source.CheckedOutAt = checked.Add(time.Hour)
	after.Source.CommitSHA = strings.Repeat("d", 40)
	again, _ := st.Runs.Get(claim, before.ID)
	if !reflect.DeepEqual(again, want) {
		t.Fatal("input or returned receipt pointer bypassed store isolation")
	}
}
func TestSourceCheckoutMemoryReceiptDeniesMissingStaleAndContradictoryAuthority(t *testing.T) {
	cases := []struct {
		name   string
		change func(*store.Store, *jobqueue.Memory, *jobqueue.Job, context.Context, *model.PipelineRunSource) (context.Context, string, *model.PipelineRunSource)
	}{
		{"no-claim", func(s *store.Store, q *jobqueue.Memory, j *jobqueue.Job, c context.Context, o *model.PipelineRunSource) (context.Context, string, *model.PipelineRunSource) {
			return context.Background(), "owned-checkout", o
		}},
		{"no-guard", func(s *store.Store, q *jobqueue.Memory, j *jobqueue.Job, c context.Context, o *model.PipelineRunSource) (context.Context, string, *model.PipelineRunSource) {
			return store.WithRunClaim(context.Background(), j.ID, j.LockedBy, nil), "owned-checkout", o
		}},
		{"wrong-owner", func(s *store.Store, q *jobqueue.Memory, j *jobqueue.Job, c context.Context, o *model.PipelineRunSource) (context.Context, string, *model.PipelineRunSource) {
			bad := store.WithRunClaim(context.Background(), j.ID, "foreign", func(x context.Context, w func() error) error { return q.WithClaim(x, "foreign", j.ID, w) })
			return bad, "owned-checkout", o
		}},
		{"wrong-job", func(s *store.Store, q *jobqueue.Memory, j *jobqueue.Job, c context.Context, o *model.PipelineRunSource) (context.Context, string, *model.PipelineRunSource) {
			return store.WithRunClaim(context.Background(), "run:foreign", j.LockedBy, func(x context.Context, w func() error) error { return q.WithClaim(x, j.LockedBy, j.ID, w) }), "owned-checkout", o
		}},
		{"wrong-run", func(s *store.Store, q *jobqueue.Memory, j *jobqueue.Job, c context.Context, o *model.PipelineRunSource) (context.Context, string, *model.PipelineRunSource) {
			return c, "foreign", o
		}},
		{"stale-claim", func(s *store.Store, q *jobqueue.Memory, j *jobqueue.Job, c context.Context, o *model.PipelineRunSource) (context.Context, string, *model.PipelineRunSource) {
			if q.Suspend(context.Background(), j.LockedBy, j.ID, "fixture ownership revoked") != nil {
				t.Fatal("revoke failed")
			}
			return c, "owned-checkout", o
		}},
		{"missing-time", func(s *store.Store, q *jobqueue.Memory, j *jobqueue.Job, c context.Context, o *model.PipelineRunSource) (context.Context, string, *model.PipelineRunSource) {
			o.CheckedOutAt = nil
			return c, "owned-checkout", o
		}},
		{"wrong-lineage", func(s *store.Store, q *jobqueue.Memory, j *jobqueue.Job, c context.Context, o *model.PipelineRunSource) (context.Context, string, *model.PipelineRunSource) {
			o.CommitSHA = strings.Repeat("e", 40)
			return c, "owned-checkout", o
		}},
		{"missing-durable-source", func(s *store.Store, q *jobqueue.Memory, j *jobqueue.Job, c context.Context, o *model.PipelineRunSource) (context.Context, string, *model.PipelineRunSource) {
			s.Runs.(*runs).mu.Lock()
			s.Runs.(*runs).m["owned-checkout"].Source = nil
			s.Runs.(*runs).mu.Unlock()
			return c, "owned-checkout", o
		}},
		{"attention-run", func(s *store.Store, q *jobqueue.Memory, j *jobqueue.Job, c context.Context, o *model.PipelineRunSource) (context.Context, string, *model.PipelineRunSource) {
			if s.Runs.UpdateStatus(c, "owned-checkout", model.RunStatusNeedsAttention, nil, "uncertain") != nil {
				t.Fatal("status setup failed")
			}
			return c, "owned-checkout", o
		}},
		{"terminal-run", func(s *store.Store, q *jobqueue.Memory, j *jobqueue.Job, c context.Context, o *model.PipelineRunSource) (context.Context, string, *model.PipelineRunSource) {
			now := time.Now().UTC()
			if s.Runs.UpdateStatus(c, "owned-checkout", model.RunStatusSuccess, &now, "") != nil {
				t.Fatal("terminal setup failed")
			}
			return c, "owned-checkout", o
		}},
		{"cancelled-context", func(s *store.Store, q *jobqueue.Memory, j *jobqueue.Job, c context.Context, o *model.PipelineRunSource) (context.Context, string, *model.PipelineRunSource) {
			cancelled, cancel := context.WithCancel(c)
			cancel()
			return cancelled, "owned-checkout", o
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, jobs, j, claim, _, source := checkoutReceiptFixture(t)
			c, id, source := tc.change(st, jobs, j, claim, source)
			before, _ := st.Runs.Get(context.Background(), "owned-checkout")
			err := st.Runs.(store.RunSourceCheckoutRecorder).RecordSourceCheckout(c, id, source)
			if err == nil {
				t.Fatal("invalid observation was accepted")
			}
			after, _ := st.Runs.Get(context.Background(), "owned-checkout")
			if !reflect.DeepEqual(before, after) {
				t.Fatal("rejected observation changed durable evidence")
			}
		})
	}
	t.Run("changed-observation-time", func(t *testing.T) {
		st, _, _, claim, _, o := checkoutReceiptFixture(t)
		recorder := st.Runs.(store.RunSourceCheckoutRecorder)
		if recorder.RecordSourceCheckout(claim, "owned-checkout", o) != nil {
			t.Fatal("initial record failed")
		}
		before, _ := st.Runs.Get(claim, "owned-checkout")
		changed := o.Clone()
		*changed.CheckedOutAt = changed.CheckedOutAt.Add(time.Nanosecond)
		if !errors.Is(recorder.RecordSourceCheckout(claim, "owned-checkout", changed), model.ErrInvalidRunSource) {
			t.Fatal("checkout time changed")
		}
		after, _ := st.Runs.Get(claim, "owned-checkout")
		if !reflect.DeepEqual(before, after) {
			t.Fatal("changed-time rejection overwrote evidence")
		}
	})
}
func TestSourceCheckoutMemoryReceiptConcurrentProgressAndHeartbeat(t *testing.T) {
	st, _, _, claim, _, observed := checkoutReceiptFixture(t)
	recorder := st.Runs.(store.RunSourceCheckoutRecorder)
	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for k := 0; k < 3; k++ {
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				var err error
				switch k {
				case 0:
					err = recorder.RecordSourceCheckout(claim, "owned-checkout", observed)
				case 1:
					err = st.Runs.UpdateProgress(claim, "owned-checkout", []model.StageRun{{StageID: "build", Status: model.RunStatusRunning, Outputs: map[string]string{"iteration": strconv.Itoa(i)}}})
				case 2:
					err = st.Runs.UpdateHeartbeat(claim, "owned-checkout", observed.CheckedOutAt.Add(time.Duration(i)*time.Microsecond))
				}
				if err != nil {
					errs <- err
					return
				}
			}
		}(k)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	after, _ := st.Runs.Get(claim, "owned-checkout")
	if after.Source.CheckedOutAt == nil || !after.Source.CheckedOutAt.Equal(*observed.CheckedOutAt) || after.StageRuns[0].Outputs["iteration"] != "49" || after.HeartbeatAt == nil || !after.HeartbeatAt.Equal(observed.CheckedOutAt.Add(49*time.Microsecond)) {
		t.Fatal("source recorder erased concurrent progress/heartbeat")
	}
}
