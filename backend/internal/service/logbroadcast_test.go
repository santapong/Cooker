package service

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/santapong/cooker/internal/builder"
	"github.com/santapong/cooker/internal/logstore"
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

// recordedBroadcasts is a thread-safe slice of (channel, frame) pairs
// captured from the executor's LogBroadcaster. Frames are now JSON
// envelopes (logstore.EncodeFrame), so the helpers decode the seq+line.
type recordedBroadcasts struct {
	mu  sync.Mutex
	got []recordedFrame
}

// recordedFrame is the decoded view of one wire envelope: the fields the
// tests assert on. Mirrors the pinned envelope shape.
type recordedFrame struct {
	channel string
	seq     int
	line    string
}

func (r *recordedBroadcasts) record(ch string, data []byte) {
	var f struct {
		Seq  int    `json:"seq"`
		Line string `json:"line"`
	}
	// Frames must be valid JSON envelopes; a decode failure is a test
	// bug we want to surface as a zero-value frame rather than silently
	// dropping.
	_ = json.Unmarshal(data, &f)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, recordedFrame{channel: ch, seq: f.Seq, line: f.Line})
}

func (r *recordedBroadcasts) snapshot() []recordedFrame {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]recordedFrame, len(r.got))
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
	if _, err := exec.Execute(context.Background(), p, run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The envelope's line carries NO trailing newline; seq increments
	// 1,2,3 with the partial flushed on stage exit as seq 3.
	want := []recordedFrame{
		{channel: "stage-logs:run-1:b", seq: 1, line: "hello"},
		{channel: "stage-logs:run-1:b", seq: 2, line: "world"},
		{channel: "stage-logs:run-1:b", seq: 3, line: "last partial"}, // flushed on stage exit
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
	if _, err := exec.Execute(context.Background(), p, run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := run.StageRuns[0].Logs; got != "line1\nline2\n" {
		t.Errorf("StageRun.Logs: got %q", got)
	}
}

func TestLineWriter_BuffersAcrossWrites(t *testing.T) {
	rec := &recordedBroadcasts{}
	lw := newLineWriter(rec.record, nil, "r", "s")

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
	// Lines carry no trailing newline on the wire; seq increments 1,2,3
	// across the Write boundary (single-writer invariant).
	want := []recordedFrame{
		{channel: "stage-logs:r:s", seq: 1, line: "alpha"},
		{channel: "stage-logs:r:s", seq: 2, line: "beta continued"},
		{channel: "stage-logs:r:s", seq: 3, line: "gamma"},
	}
	if len(got) != len(want) {
		t.Fatalf("broadcast count: got %d want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %+v want %+v", i, got[i], want[i])
		}
	}
}

// TestLineWriter_StoreOnlyAppendsWithoutBroadcast asserts the spec's
// "Append happens even when broadcast is nil" rule: a store-only
// lineWriter records stamped entries that Read can replay.
func TestLineWriter_StoreOnlyAppendsWithoutBroadcast(t *testing.T) {
	store := logstore.NewMemory(1<<20, 16)
	lw := newLineWriter(nil, store, "r", "s")

	if _, err := lw.Write([]byte("one\ntwo\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	lw.flush() // no partial; no-op

	got, err := store.Read(context.Background(), "r", "s", 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("store entries = %d, want 2 (%v)", len(got), got)
	}
	if got[0].Seq != 1 || got[0].Line != "one" {
		t.Errorf("entry[0] = %+v, want {Seq:1 Line:one}", got[0])
	}
	if got[1].Seq != 2 || got[1].Line != "two" {
		t.Errorf("entry[1] = %+v, want {Seq:2 Line:two}", got[1])
	}
}
