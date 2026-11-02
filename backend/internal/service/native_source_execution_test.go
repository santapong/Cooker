package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/santapong/cooker/internal/build/builder"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
)

type nativeBytesBuilder struct {
	calls                                 int
	root, contextDir, dockerfile, content string
}

func (b *nativeBytesBuilder) Build(_ context.Context, request builder.Request) (builder.Result, error) {
	b.calls++
	b.contextDir, b.dockerfile = request.ContextDir, request.Dockerfile
	b.root = filepath.Dir(request.ContextDir)
	data, err := os.ReadFile(filepath.Join(request.ContextDir, "commit.txt"))
	if err != nil {
		return builder.Result{}, err
	}
	b.content = string(data)
	if _, err = os.Stat(filepath.Join(b.root, ".git")); !os.IsNotExist(err) {
		return builder.Result{}, errors.New("source metadata exported")
	}
	dockerfile, err := os.ReadFile(filepath.Join(request.ContextDir, request.Dockerfile))
	if err != nil || !strings.Contains(string(dockerfile), "FROM scratch") {
		return builder.Result{}, errors.New("accepted Dockerfile unavailable")
	}
	h := sha256.Sum256(data)
	return builder.Result{ImageID: "sha256:" + hex.EncodeToString(h[:]), Tags: request.Tags}, nil
}

func TestNativeSourceQueueWorkerConsumesAcceptedExportAndRecordsCheckout(t *testing.T) {
	s, p, f := nativeSourceSetup(t)
	ctx := context.Background()
	jobs := jobqueue.NewMemory()
	e := NewJobQueueEnqueuer(jobs, s.Store.Pipelines)
	e.AdmissionGuard = memory.NewTriggerStore()
	e.PrepareSource = func(ctx context.Context, p *model.Pipeline) (*model.PipelineSourcePlan, error) {
		return s.Prepare(ctx, p, p.Source.FullRef, "manual")
	}
	run := pendingPipelineRun(p, "owned-native-run")
	original, _ := json.Marshal(p)
	if err := e.EnqueuePlannedRun(ctx, p, run); err != nil {
		t.Fatal(err)
	}
	j, err := jobs.Get(ctx, "run:"+run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var accepted runJobPayload
	if json.Unmarshal(j.Payload, &accepted) != nil || accepted.Source == nil || accepted.Run.Source == nil || accepted.Run.Source.CheckedOutAt != nil {
		t.Fatal("admission source receipt invalid")
	}
	before := f.checkouts
	f.commit = strings.Repeat("b", 40)
	if err := e.EnqueuePlannedRun(ctx, p, run); err != nil {
		t.Fatal(err)
	}
	if f.checkouts != before || f.revisions != 1 {
		t.Fatal("accepted retry reselected source")
	}
	claimed, err := jobs.Dequeue(ctx, "native-source-export-worker", []string{JobKindPipelineRun})
	if err != nil || claimed == nil || claimed.ID != j.ID || claimed.Attempts != 1 {
		t.Fatal("accepted source was not claimed by a real worker", err)
	}
	claimCtx := store.WithRunClaim(ctx, claimed.ID, claimed.LockedBy, func(ctx context.Context, write func() error) error {
		return jobs.WithClaim(ctx, claimed.LockedBy, claimed.ID, write)
	})
	b := &nativeBytesBuilder{}
	runner := NewJobQueueRunner(s.Store, NewExecutor(WithBuilder(b)), nil)
	runner.SourcePreparer = s
	if err := runner.Handle(claimCtx, claimed); err != nil {
		t.Fatal(err)
	}
	stored, err := s.Store.Runs.Get(ctx, run.ID)
	if err != nil || stored.Status != model.RunStatusSuccess || stored.Source == nil || stored.Source.CommitSHA != strings.Repeat("a", 40) || stored.Source.CheckedOutAt == nil {
		t.Fatal("accepted source execution receipt missing")
	}
	if b.calls != 1 || b.content != "exact-commit-content" || b.dockerfile != "../docker/api.Dockerfile" || !filepath.IsAbs(b.contextDir) || f.revisions != 1 {
		t.Fatal("worker did not consume the exact accepted source")
	}
	if _, err := os.Stat(b.root); !os.IsNotExist(err) {
		t.Fatal("worker retained source export")
	}
	public, _ := json.Marshal(stored.Source)
	if strings.Contains(string(public), b.root) || strings.Contains(string(public), "exact-commit-content") || strings.Contains(string(public), "FROM scratch") {
		t.Fatal("private export entered public receipt")
	}
	after, _ := json.Marshal(p)
	if string(after) != string(original) {
		t.Fatal("worker mutated authored paths")
	}
	if err := runner.Handle(claimCtx, claimed); err != nil {
		t.Fatal(err)
	}
	if b.calls != 1 {
		t.Fatal("terminal job replayed stages")
	}
}

func TestNativeSourceConcurrentAdmissionPreparesOnce(t *testing.T) {
	s, p, f := nativeSourceSetup(t)
	ctx := context.Background()
	jobs := jobqueue.NewMemory()
	e := NewJobQueueEnqueuer(jobs, s.Store.Pipelines)
	e.AdmissionGuard = memory.NewTriggerStore()
	prepares, pins := 0, 0
	e.PrepareSource = func(ctx context.Context, p *model.Pipeline) (*model.PipelineSourcePlan, error) {
		prepares++
		return s.Prepare(ctx, p, p.Source.FullRef, "manual")
	}
	e.PrepareSecrets = func(context.Context, *model.Pipeline) (*PipelineSecretPlan, error) { pins++; return nil, nil }
	run := pendingPipelineRun(p, "concurrent-native-run")
	var wg sync.WaitGroup
	errors := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errors <- e.EnqueuePlannedRun(ctx, p, run.Clone()) }()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if prepares != 1 || pins != 1 || f.checkouts != 1 {
		t.Fatal("concurrent duplicate repeated source or secret preparation")
	}
}

func TestNativeSourceCannotUseHostPathsOrMissingAuthority(t *testing.T) {
	s, p, _ := nativeSourceSetup(t)
	ctx := context.Background()
	b := &nativeBytesBuilder{}
	exec := NewExecutor(WithBuilder(b))
	if _, err := exec.Execute(ctx, p, pendingPipelineRun(p, "inline-denied")); err == nil || b.calls != 0 {
		t.Fatal("inline source used host paths")
	}
	e := NewJobQueueEnqueuer(jobqueue.NewMemory(), s.Store.Pipelines)
	e.AdmissionGuard = memory.NewTriggerStore()
	pins := 0
	e.PrepareSecrets = func(context.Context, *model.Pipeline) (*PipelineSecretPlan, error) { pins++; return nil, nil }
	if err := e.EnqueuePlannedRun(ctx, p, pendingPipelineRun(p, "queue-denied")); err == nil || pins != 0 {
		t.Fatal("missing source authority reached secret preparation")
	}
}

func TestNativeSourceWorkerRejectionRunsNoStagesAndDoesNotClaimCheckout(t *testing.T) {
	s, p, f := nativeSourceSetup(t)
	ctx := context.Background()
	jobs := jobqueue.NewMemory()
	e := NewJobQueueEnqueuer(jobs, s.Store.Pipelines)
	e.AdmissionGuard = memory.NewTriggerStore()
	e.PrepareSource = func(ctx context.Context, p *model.Pipeline) (*model.PipelineSourcePlan, error) {
		return s.Prepare(ctx, p, p.Source.FullRef, "manual")
	}
	run := pendingPipelineRun(p, "revoked-native-run")
	if err := e.EnqueuePlannedRun(ctx, p, run); err != nil {
		t.Fatal(err)
	}
	j, err := jobs.Get(ctx, "run:"+run.ID)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Connections.Store.Get(ctx, p.Source.ConnectionID)
	if err != nil {
		t.Fatal(err)
	}
	c.Enabled = false
	if err = s.Connections.Store.Update(ctx, c); err != nil {
		t.Fatal(err)
	}
	b := &nativeBytesBuilder{}
	runner := NewJobQueueRunner(s.Store, NewExecutor(WithBuilder(b)), nil)
	runner.SourcePreparer = s
	before := f.checkouts
	if err := runner.Handle(ctx, j); err == nil || errors.Is(err, jobqueue.ErrOutcomeUnknown) {
		t.Fatal("pre-stage rejection was not determinate")
	}
	stored, err := s.Store.Runs.Get(ctx, run.ID)
	if err != nil || stored.Status != model.RunStatusFailed || stored.Source == nil || stored.Source.CheckedOutAt != nil || b.calls != 0 || f.checkouts != before {
		t.Fatal("revoked source reached execution")
	}
}
