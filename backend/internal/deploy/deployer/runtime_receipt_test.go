package deployer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// This stub is an owned executable, never Docker or a daemon. Its recorded
// exact arguments prove capture addresses the actual start ID and safe format.
func receiptDockerCLI(t *testing.T, start string, fields []string, mode string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	bin, calls := filepath.Join(dir, "docker-receipt"), filepath.Join(dir, "calls")
	data, _ := json.Marshal(fields)
	for name, value := range map[string]string{"start": start, "inspect": string(data)} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("COOKER_RECEIPT_DIR", dir)
	t.Setenv("COOKER_RECEIPT_MODE", mode)
	script := `#!/bin/sh
printf '%s\n' "$@" >> "$COOKER_RECEIPT_DIR/calls"
case "$1" in
 rm) exit 0;;
 run)
  if [ "$COOKER_RECEIPT_MODE" = start-fails ]; then exit 42; fi
  if [ "$COOKER_RECEIPT_MODE" = start-concurrent ]; then
   (count=0; while [ "$count" -lt 512 ]; do printf '%s\n' receipt-stderr-marker >&2; count=$((count + 1)); done) &
   cat "$COOKER_RECEIPT_DIR/start"
   wait; exit 0
  fi
  cat "$COOKER_RECEIPT_DIR/start"; exit 0;;
 container)
  [ "$2" = inspect ] && [ "$3" = --format ] || exit 91
  if [ "$COOKER_RECEIPT_MODE" = inspect-fails ]; then printf '%s' secret-inspect-canary >&2; exit 43; fi
  if [ "$COOKER_RECEIPT_MODE" = inspect-waits ]; then exec sleep 60; fi
  if [ "$COOKER_RECEIPT_MODE" = inspect-descendant ]; then
   (
    count=0
    while [ ! -f "$COOKER_RECEIPT_DIR/descendant-release" ] && [ "$count" -lt 100 ]; do
     sleep 0.02; count=$((count + 1))
    done
    printf '%s' done > "$COOKER_RECEIPT_DIR/descendant-done"
   ) &
  fi
  cat "$COOKER_RECEIPT_DIR/inspect"; exit 0;;
 *) exit 92;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return bin, calls
}

func receiptRequest() Request {
	return Request{Kind: KindDockerRun, Name: "web", Image: "registry.invalid/web:v1",
		Labels: map[string]string{dockerRuntimeRunLabel: "trusted-run", dockerRuntimeStageLabel: "deploy-web"}}
}

func receiptInspectFields() []string {
	return []string{strings.Repeat("a", 64), "/web", "sha256:" + strings.Repeat("b", 64), "registry.invalid/web:v1", "trusted-run", "deploy-web"}
}

func TestDockerRuntimeReceiptWriterOverflowIsBoundedAndNonfatal(t *testing.T) {
	b := &runtimeReceiptBuffer{}
	if _, bypass := any(b).(io.ReaderFrom); bypass {
		t.Fatal("io.Copy could bypass the bounded Write method")
	}
	data := []byte(strings.Repeat("a", runtimeReceiptByteLimit+1024))
	if n, err := b.Write(data); err != nil || n != len(data) || b.Len() != runtimeReceiptByteLimit || !b.truncated {
		t.Fatal("optional capture bound changed process write outcome", err)
	}
	if n, err := b.Write([]byte("extra")); err != nil || n != 5 || b.Len() != runtimeReceiptByteLimit {
		t.Fatal("overflow accumulated unbounded bytes", err)
	}
}

func TestDockerRuntimeReceiptCapturesActualStartIDAndSafeFields(t *testing.T) {
	bin, calls := receiptDockerCLI(t, strings.Repeat("a", 64)+"\n", receiptInspectFields(), "")
	var output bytes.Buffer
	req := receiptRequest()
	req.LogWriter = &output
	before := time.Now()
	res, err := (&DockerRun{Bin: bin}).Deploy(context.Background(), req)
	if err != nil || res.RuntimeReceipt == nil || res.RuntimeBindingWarning != "" {
		t.Fatal("actual start receipt missing", err)
	}
	r := res.RuntimeReceipt
	if r.ContainerID != strings.Repeat("a", 64) || r.Name != req.Name || r.RunID != "trusted-run" || r.StageID != "deploy-web" || r.ImageReference != req.Image || r.ImageID != receiptInspectFields()[2] || r.Version != 1 || r.Runtime != "docker" || r.CapturedAt.Before(before) || r.CapturedAt.After(time.Now()) {
		t.Fatal("receipt identity/time mismatch")
	}
	data, _ := os.ReadFile(calls)
	if !strings.Contains(string(data), "container\ninspect\n--format\n"+runtimeReceiptInspectFormat+"\n"+r.ContainerID+"\n") || strings.Contains(string(data), ".Config.Env") || strings.Contains(string(data), "{{json .Config.Labels}}") {
		t.Fatal("capture did not use exact ID and narrow safe projection")
	}
	if !strings.Contains(output.String(), strings.Repeat("a", 64)) || !strings.Contains(output.String(), "Started container web") {
		t.Fatal("existing progress output lost")
	}
}

func TestDockerRuntimeReceiptCaptureFailurePreservesDeployment(t *testing.T) {
	for _, mode := range []string{"inspect-fails", "short-start", "multiple-start", "start-overflow", "inspect-overflow", "unlabelled"} {
		t.Run(mode, func(t *testing.T) {
			start, fields := strings.Repeat("a", 64)+"\n", receiptInspectFields()
			switch mode {
			case "short-start":
				start = "aaaa\n"
			case "multiple-start":
				start += strings.Repeat("c", 64) + "\n"
			case "start-overflow":
				start = strings.Repeat("a", runtimeReceiptByteLimit+1)
			case "inspect-overflow":
				fields[1] = strings.Repeat("x", runtimeReceiptByteLimit+1)
			}
			bin, calls := receiptDockerCLI(t, start, fields, mode)
			var output bytes.Buffer
			req := receiptRequest()
			req.LogWriter = &output
			if mode == "unlabelled" {
				delete(req.Labels, dockerRuntimeStageLabel)
			}
			res, err := (&DockerRun{Bin: bin}).Deploy(context.Background(), req)
			if err != nil || res.RuntimeReceipt != nil || res.RuntimeBindingWarning != RuntimeBindingUnavailable || len(res.AppliedResources) != 1 {
				t.Fatal("optional capture changed successful start", err)
			}
			if strings.Contains(output.String(), "secret-inspect-canary") {
				t.Fatal("raw capture stderr escaped")
			}
			if mode == "short-start" || mode == "multiple-start" || mode == "start-overflow" || mode == "unlabelled" {
				data, _ := os.ReadFile(calls)
				if strings.Contains(string(data), "container\ninspect\n") {
					t.Fatal("invalid start identity reached inspect")
				}
			}
		})
	}
}

func TestDockerRuntimeReceiptRejectsIdentitySubstitution(t *testing.T) {
	for i := range receiptInspectFields() {
		t.Run([]string{"container", "name", "image-content", "image-ref", "run", "stage"}[i], func(t *testing.T) {
			fields := receiptInspectFields()
			fields[i] = "substituted"
			bin, _ := receiptDockerCLI(t, strings.Repeat("a", 64), fields, "")
			res, err := (&DockerRun{Bin: bin}).Deploy(context.Background(), receiptRequest())
			if err != nil || res.RuntimeReceipt != nil || res.RuntimeBindingWarning != RuntimeBindingUnavailable {
				t.Fatal("substituted identity accepted or operation changed", err)
			}
		})
	}
}

func TestDockerRuntimeReceiptStartFailureRemainsFailure(t *testing.T) {
	bin, calls := receiptDockerCLI(t, strings.Repeat("a", 64), receiptInspectFields(), "start-fails")
	res, err := (&DockerRun{Bin: bin}).Deploy(context.Background(), receiptRequest())
	data, _ := os.ReadFile(calls)
	if err == nil || res.RuntimeReceipt != nil || len(res.AppliedResources) != 0 || strings.Contains(string(data), "container\ninspect\n") {
		t.Fatal("failed start was reclassified as optional capture")
	}
}

func TestDockerRuntimeReceiptCaptureCancellationIsNonfatal(t *testing.T) {
	bin, calls := receiptDockerCLI(t, strings.Repeat("a", 64), receiptInspectFields(), "inspect-waits")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		result Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() { res, err := (&DockerRun{Bin: bin}).Deploy(ctx, receiptRequest()); done <- outcome{res, err} }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, _ := os.ReadFile(calls)
		if strings.Contains(string(data), "container\ninspect\n--format\n") {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			<-done
			t.Fatal("owned stub never reached post-start capture")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	got := <-done
	res, err := got.result, got.err
	if err != nil || res.RuntimeReceipt != nil || res.RuntimeBindingWarning != RuntimeBindingUnavailable || len(res.AppliedResources) != 1 {
		t.Fatal("capture cancellation changed successful deployment", err)
	}
}

func TestDockerRuntimeReceiptDescendantPipeWaitIsBoundedAndNonfatal(t *testing.T) {
	bin, calls := receiptDockerCLI(t, strings.Repeat("a", 64), receiptInspectFields(), "inspect-descendant")
	dir := filepath.Dir(calls)
	// The task-owned child has a finite self-exit even if a regression blocks
	// the adapter. Release it and observe its final marker before TempDir cleanup.
	t.Cleanup(func() {
		if err := os.WriteFile(filepath.Join(dir, "descendant-release"), []byte("release"), 0600); err != nil {
			t.Error(err)
			return
		}
		deadline := time.Now().Add(3 * time.Second)
		for {
			if data, err := os.ReadFile(filepath.Join(dir, "descendant-done")); err == nil && string(data) == "done" {
				return
			}
			if time.Now().After(deadline) {
				t.Error("owned descendant did not finish after release")
				return
			}
			time.Sleep(time.Millisecond)
		}
	})
	res, err := (&DockerRun{Bin: bin}).Deploy(context.Background(), receiptRequest())
	if err != nil || res.RuntimeReceipt != nil || res.RuntimeBindingWarning != RuntimeBindingUnavailable || len(res.AppliedResources) != 1 {
		t.Fatal("descendant pipe wait changed successful operation or bypassed optional bound", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "descendant-done")); !os.IsNotExist(err) {
		t.Fatal("capture waited for the child instead of its finite pipe deadline")
	}
}

// This generic writer rejects concurrent callers rather than promising
// thread safety. Atomics make a detected regression safe under the race check.
type receiptSerialOnlyLogWriter struct {
	writing    atomic.Bool
	overlapped atomic.Bool
	data       bytes.Buffer
}

func (w *receiptSerialOnlyLogWriter) Write(p []byte) (int, error) {
	if !w.writing.CompareAndSwap(false, true) {
		w.overlapped.Store(true)
		return 0, errors.New("concurrent progress write")
	}
	defer w.writing.Store(false)
	// Keep both owned stdout/stderr copy goroutines observable without
	// making the underlying writer concurrently writable.
	time.Sleep(10 * time.Millisecond)
	return w.data.Write(p)
}

func TestDockerRuntimeReceiptSerializesConcurrentStartProgress(t *testing.T) {
	bin, _ := receiptDockerCLI(t, strings.Repeat("a", 64)+"\n", receiptInspectFields(), "start-concurrent")
	output := &receiptSerialOnlyLogWriter{}
	req := receiptRequest()
	req.LogWriter = output
	res, err := (&DockerRun{Bin: bin}).Deploy(context.Background(), req)
	if err != nil || output.overlapped.Load() {
		t.Fatal("generic progress writer received concurrent stdout/stderr writes", err)
	}
	if res.RuntimeReceipt == nil || res.RuntimeBindingWarning != "" || res.RuntimeReceipt.ContainerID != strings.Repeat("a", 64) {
		t.Fatal("stderr changed the actual stdout start ID capture")
	}
	if !strings.Contains(output.data.String(), "receipt-stderr-marker") || !strings.Contains(output.data.String(), strings.Repeat("a", 64)) {
		t.Fatal("stdout or stderr progress was lost")
	}
}
