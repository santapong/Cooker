package memory

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

func memoryRunSource() *model.PipelineRunSource {
	return &model.PipelineRunSource{Source: model.GitSourceIdentity{Provider: "github", ConnectionID: "approved", Host: "github.com", RepositoryID: "123", Repository: "owned/native"}, FullRef: "refs/heads/main", CommitSHA: strings.Repeat("a", 40), SourcePlanDigest: strings.Repeat("b", 64)}
}

func TestRunSourceMemoryIsolationAndLifecyclePersistence(t *testing.T) {
	ctx := context.Background()
	st := New()
	run := &model.PipelineRun{ID: "source-run", PipelineID: "native", Status: model.RunStatusPending, Source: memoryRunSource(), StageRuns: []model.StageRun{{StageID: "build", Logs: "private-log-canary"}}}
	if err := st.Runs.Create(ctx, run); err != nil {
		t.Fatal(err)
	}
	run.Source.CommitSHA = strings.Repeat("c", 40)
	got, _ := st.Runs.Get(ctx, run.ID)
	if got.Source.CommitSHA != strings.Repeat("a", 40) || got.Source.CheckedOutAt != nil {
		t.Fatal("create aliased caller or stamped checkout")
	}
	checked := time.Now().UTC()
	got.Source.CheckedOutAt = &checked
	if err := st.Runs.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	for _, read := range []func() (*model.PipelineRun, error){
		func() (*model.PipelineRun, error) { return st.Runs.Get(ctx, run.ID) },
		func() (*model.PipelineRun, error) { return st.Runs.GetSummary(ctx, run.ID) },
		func() (*model.PipelineRun, error) {
			rows, err := st.Runs.List(ctx, run.PipelineID, 10, 0)
			return rows[0], err
		},
	} {
		copy, err := read()
		if err != nil || !copy.Source.CheckedOutAt.Equal(checked) {
			t.Fatal("read lost source", err)
		}
		copy.Source.CommitSHA = strings.Repeat("f", 40)
		*copy.Source.CheckedOutAt = checked.Add(time.Hour)
	}
	legacy, _ := st.Runs.Get(ctx, run.ID)
	legacy.Source = nil
	legacy.Status = model.RunStatusRunning
	if err := st.Runs.Update(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	if err := st.Runs.UpdateProgress(ctx, run.ID, []model.StageRun{{StageID: "build", Status: model.RunStatusSuccess, Logs: "private-log-canary"}}); err != nil {
		t.Fatal(err)
	}
	st.Runs.UpdateHeartbeat(ctx, run.ID, time.Now())
	st.Runs.UpdateStatus(ctx, run.ID, model.RunStatusSuccess, nil, "")
	got, _ = st.Runs.Get(ctx, run.ID)
	if got.Source.CommitSHA != strings.Repeat("a", 40) || !got.Source.CheckedOutAt.Equal(checked) || got.StageRuns[0].Logs != "" {
		t.Fatal("lifecycle flush altered source")
	}
	got.Source.SourcePlanDigest = strings.Repeat("f", 64)
	got.Status = model.RunStatusFailed
	if err := st.Runs.Update(ctx, got); !errors.Is(err, store.ErrConflict) {
		t.Fatal("replaced accepted source lineage", err)
	}
	stored, _ := st.Runs.Get(ctx, run.ID)
	if stored.Status != model.RunStatusSuccess {
		t.Fatal("rejected receipt update partially applied lifecycle")
	}
	if err := st.Runs.Create(ctx, &model.PipelineRun{ID: "legacy", PipelineID: "native"}); err != nil {
		t.Fatal(err)
	}
	old, _ := st.Runs.Get(ctx, "legacy")
	if old.Source != nil {
		t.Fatal("legacy source reconstructed")
	}
}
