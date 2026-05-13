package service

import (
	"context"
	"sync"
	"testing"

	"github.com/santapong/cooker/internal/builder"
	"github.com/santapong/cooker/internal/model"
)

// TestStageLogChannel locks down the wire-format string. The server
// package's wshub_logs.go formats the same string (duplicated to avoid
// an import cycle), so any change here MUST be made there as well.
func TestStageLogChannel(t *testing.T) {
	got := StageLogChannel("run-1", "stage-x")
	want := "stage-logs:run-1:stage-x"
	if got != want {
		t.Fatalf("StageLogChannel: got %q want %q", got, want)
	}
}

// streamingBuilder is a builder.Builder that writes a fixed payload to
// req.LogWriter before returning. Lets us drive the executor's tee
// behaviour in tests without needing a real Kaniko/Buildah toolchain.
type streamingBuilder struct {
	payload []byte
	res     builder.Result
}

func (b *streamingBuilder) Build(_ context.Context, req builder.Request) (builder.Result, error) {
	if req.LogWriter != nil && len(b.payload) > 0 {
		_, _ = req.LogWriter.Write(b.payload)
	}
	return b.res, nil
}

// recordedBroadcasts is a thread-safe slice of (channel, line) pairs
// captured from the executor's LogBroadcaster.
type recordedBroadcasts struct {
	mu  sync.Mutex
	got []recordedLine
}

type recordedLine struct {
	channel string
	line    string
}

func (r *recordedBroadcasts) record(ch string, line []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, recordedLine{channel: ch, line: string(line)})
}

func (r *recordedBroadcasts) snapshot() []recordedLine {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]recordedLine, len(r.got))
	copy(out, r.got)
	return out
}

func TestExecutor_BuildStage_BroadcastsLogLines(t *testing.T) {
	rec := &recordedBroadcasts{}

	sb := &streamingBuilder{
		payload: []byte("hello\nworld\nlast partial"),
		res:     builder.Result{ImageID: "sha256:abc", Tags: []string{"app:v1"}},
	}

	p := &model.Pipeline{
		ID: "pipe-1",
		Stages: []model.Stage{
			{ID: "b", Name: "Build", Type: model.StageTypeBuild, Config: model.StageConfig{
				Dockerfile: "Dockerfile",
				Context:    "/tmp/ctx",
				Tags:       []string{"app:v1"},
			}},
		},
	}
	run := &model.PipelineRun{
		ID:         "run-1",
		PipelineID: "pipe-1",
		Status:     model.RunStatusPending,
		StageRuns:  []model.StageRun{{StageID: "b", Status: model.RunStatusPending}},
	}

	exec := NewExecutor(
		WithBuilder(sb),
		WithLogBroadcaster(rec.record),
	)
	if err := exec.Execute(context.Background(), p, run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []recordedLine{
		{channel: "stage-logs:run-1:b", line: "hello\n"},
		{channel: "stage-logs:run-1:b", line: "world\n"},
		{channel: "stage-logs:run-1:b", line: "last partial\n"}, // flushed on stage exit
	}

	got := rec.snapshot()
	if len(got) != len(want) {
		t.Fatalf("broadcast count: got %d want %d (got=%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("broadcast[%d]: got %+v want %+v", i, got[i], want[i])
		}
	}

	// On-disk capture should still include every line — broadcasting is
	// additive, not a replacement for sr.Logs.
	logs := run.StageRuns[0].Logs
	if logs != "hello\nworld\nlast partial" {
		t.Errorf("StageRun.Logs: got %q", logs)
	}
}

func TestExecutor_BuildStage_NoBroadcasterIsNoOp(t *testing.T) {
	// When no broadcaster is wired the executor must keep behaving
	// exactly like the historical path: the on-disk capture works,
	// nothing panics.
	sb := &streamingBuilder{payload: []byte("line1\nline2\n")}

	p := &model.Pipeline{
		ID: "pipe-2",
		Stages: []model.Stage{
			{ID: "b", Name: "Build", Type: model.StageTypeBuild, Config: model.StageConfig{
				Dockerfile: "Dockerfile",
				Context:    "/tmp/ctx",
				Tags:       []string{"app:v1"},
			}},
		},
	}
	run := &model.PipelineRun{
		ID:         "run-2",
		PipelineID: "pipe-2",
		Status:     model.RunStatusPending,
		StageRuns:  []model.StageRun{{StageID: "b", Status: model.RunStatusPending}},
	}

	exec := NewExecutor(WithBuilder(sb))
	if err := exec.Execute(context.Background(), p, run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := run.StageRuns[0].Logs; got != "line1\nline2\n" {
		t.Errorf("StageRun.Logs: got %q", got)
	}
}

func TestLineWriter_BuffersAcrossWrites(t *testing.T) {
	rec := &recordedBroadcasts{}
	lw := newLineWriter(rec.record, "stage-logs:r:s")

	// First write: a complete line plus a partial.
	if _, err := lw.Write([]byte("alpha\nbeta")); err != nil {
		t.Fatalf("write 1: %v", err)
	}
	// Second write: completes "beta", adds another partial.
	if _, err := lw.Write([]byte(" continued\ngamma")); err != nil {
		t.Fatalf("write 2: %v", err)
	}
	// Flush emits the trailing partial.
	lw.flush()

	got := rec.snapshot()
	want := []recordedLine{
		{channel: "stage-logs:r:s", line: "alpha\n"},
		{channel: "stage-logs:r:s", line: "beta continued\n"},
		{channel: "stage-logs:r:s", line: "gamma\n"},
	}
	if len(got) != len(want) {
		t.Fatalf("broadcast count: got %d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %+v want %+v", i, got[i], want[i])
		}
	}
}
