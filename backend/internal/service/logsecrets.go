package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/santapong/cooker/internal/model"
)

type logSecretsKey struct{}
type logSecretSet struct {
	mu     sync.RWMutex
	values []string
}

func logSecretValues(ctx context.Context) []string {
	set, _ := ctx.Value(logSecretsKey{}).(*logSecretSet)
	if set == nil {
		return nil
	}
	set.mu.RLock()
	defer set.mu.RUnlock()
	return append([]string(nil), set.values...)
}

// RegisterLogSecrets protects credentials resolved by an adapter after the run
// starts. Call before handing credentials to external tools. The caller must
// initialize the attempt with WithLogSecrets, even when its list is empty.
func RegisterLogSecrets(ctx context.Context, values ...string) {
	set, _ := ctx.Value(logSecretsKey{}).(*logSecretSet)
	if set == nil {
		return
	}
	set.mu.Lock()
	defer set.mu.Unlock()
	seen := map[string]bool{}
	for _, v := range set.values {
		seen[v] = true
	}
	for _, v := range values {
		if v != "" && !seen[v] {
			set.values = append(set.values, v)
			seen[v] = true
		}
	}
	sort.Slice(set.values, func(i, j int) bool { return len(set.values[i]) > len(set.values[j]) })
}

type redactedLogError struct {
	cause error
	text  string
}

func (e redactedLogError) Error() string { return e.text }
func (e redactedLogError) Unwrap() error { return e.cause }
func redactLogError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	return redactedLogError{cause: err, text: RedactLogText(ctx, err.Error())}
}

// WithLogSecrets binds resolved secrets to this attempt only. Never persist this
// context or its values. Plain configuration values should not be included.
func WithLogSecrets(ctx context.Context, values []string) context.Context {
	seen := map[string]bool{}
	var clean []string
	previous := logSecretValues(ctx)
	for _, group := range [][]string{previous, values} {
		for _, v := range group {
			if v != "" && !seen[v] {
				seen[v] = true
				clean = append(clean, v)
			}
		}
	}
	sort.Slice(clean, func(i, j int) bool { return len(clean[i]) > len(clean[j]) })
	return context.WithValue(ctx, logSecretsKey{}, &logSecretSet{values: clean})
}

// RedactLogText removes known secret values from complete diagnostic text.
// It cannot recognize arbitrary encodings, hashes or secrets unknown to Cooker.
func RedactLogText(ctx context.Context, text string) string {
	values := logSecretValues(ctx)
	for _, v := range values {
		text = strings.ReplaceAll(text, v, "[REDACTED]")
	}
	return text
}

// Public stage outputs/artifact references must not become a second secret
// export channel. Reject such an output before a dependent stage consumes it.
func protectStageMetadata(ctx context.Context, sr *model.StageRun) error {
	if sr.RuntimeReceipt != nil && !runtimeReceiptMetadataSafe(ctx, sr.RuntimeReceipt) {
		sr.RuntimeReceipt = nil
		sr.RuntimeBindingWarning = model.RuntimeBindingUnavailable
	}
	leaked := false
	protect := func(value string) string {
		safe := RedactLogText(ctx, value)
		if safe != value {
			leaked = true
		}
		return safe
	}
	for k, v := range sr.Outputs {
		if protect(k) != k {
			delete(sr.Outputs, k)
			continue
		}
		sr.Outputs[k] = protect(v)
	}
	for i := range sr.Artifacts {
		sr.Artifacts[i].Type = protect(sr.Artifacts[i].Type)
		sr.Artifacts[i].Ref = protect(sr.Artifacts[i].Ref)
		sr.Artifacts[i].Digest = protect(sr.Artifacts[i].Digest)
	}
	if leaked {
		return fmt.Errorf("stage produced a secret in public output metadata; use a secret binding instead")
	}
	return nil
}

// SecretLogWriter redacts before any capture, persistence or broadcast. It
// retains at most the longest-secret suffix between writes, so secrets split
// across writes (including newlines) cannot escape. Call Close at stream end;
// it flushes the suffix without closing the underlying destination.
type SecretLogWriter struct {
	mu      sync.Mutex
	out     io.Writer
	values  []string
	pending []byte
	max     int
	closed  bool
	ctx     context.Context
}

func NewSecretLogWriter(ctx context.Context, out io.Writer) *SecretLogWriter {
	values := logSecretValues(ctx)
	w := &SecretLogWriter{out: out, values: values, max: 1, ctx: ctx}
	for _, v := range values {
		if len(v) > w.max {
			w.max = len(v)
		}
	}
	if w.out == nil {
		w.out = io.Discard
	}
	return w
}

func (w *SecretLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, io.ErrClosedPipe
	}
	w.values = logSecretValues(w.ctx)
	for _, v := range w.values {
		w.max = max(w.max, len(v))
	}
	// Bound temporary allocations even when an adapter writes one huge chunk.
	for offset := 0; offset < len(p); {
		n := min(len(p)-offset, 32*1024)
		w.pending = append(w.pending, p[offset:offset+n]...)
		if err := w.drain(false); err != nil {
			return offset, err
		}
		offset += n
	}
	return len(p), nil
}

func (w *SecretLogWriter) drain(final bool) error {
	safe := len(w.pending) - w.max + 1
	if final {
		safe = len(w.pending)
	}
	if safe <= 0 {
		return nil
	}
	var result bytes.Buffer
	i := 0
	for i < safe {
		matched := false
		for _, secret := range w.values {
			if bytes.HasPrefix(w.pending[i:], []byte(secret)) {
				result.WriteString("[REDACTED]")
				i += len(secret)
				matched = true
				break
			}
		}
		if !matched {
			result.WriteByte(w.pending[i])
			i++
		}
	}
	if _, err := io.Copy(w.out, &result); err != nil {
		return err
	}
	copy(w.pending, w.pending[i:])
	w.pending = w.pending[:len(w.pending)-i]
	return nil
}

func (w *SecretLogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	return w.drain(true)
}

// newStageOutput ensures all output legs see the same redacted bytes. The
// adapter may write stdout and stderr concurrently; SecretLogWriter serializes
// these writes before the non-concurrent capped buffer and line splitter.
func (e *Executor) newStageOutput(ctx context.Context, runID string, stage *model.Stage, sr *model.StageRun) (io.Writer, func()) {
	logs := newCappedBuffer(stageLogCap)
	var out io.Writer = logs
	lw := e.newStageLineWriter(runID, stage)
	if lw != nil {
		out = io.MultiWriter(logs, lw)
	}
	w := NewSecretLogWriter(ctx, out)
	return w, func() {
		_ = w.Close() // capture/stream legs consume complete writes best-effort
		if lw != nil {
			lw.flush()
		}
		sr.Logs = logs.String()
	}
}
