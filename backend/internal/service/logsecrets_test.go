package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/santapong/cooker/internal/logstore"
	"github.com/santapong/cooker/internal/model"
)

func TestSecretLogWriterEverySplit(t *testing.T) {
	const input = "visible database=example password=s3cret-with\nnewline done"
	for split := 0; split <= len(input); split++ {
		t.Run(fmt.Sprint(split), func(t *testing.T) {
			var out bytes.Buffer
			ctx := WithLogSecrets(context.Background(), []string{"s3cret-with\nnewline", "s3cret"})
			w := NewSecretLogWriter(ctx, &out)
			for _, chunk := range []string{input[:split], input[split:]} {
				if n, err := w.Write([]byte(chunk)); err != nil || n != len(chunk) {
					t.Fatalf("write %d %v", n, err)
				}
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			if got, want := out.String(), "visible database=example password=[REDACTED] done"; got != want {
				t.Fatalf("got %q want %q", got, want)
			}
		})
	}
}

func TestSecretLogWriterConcurrentAndBounded(t *testing.T) {
	var out bytes.Buffer
	w := NewSecretLogWriter(WithLogSecrets(context.Background(), []string{"password-value"}), &out)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_, _ = io.WriteString(w, "password-value\n")
			}
		}()
	}
	wg.Wait()
	if len(w.pending) > len("password-value")-1 {
		t.Fatalf("unbounded pending: %d", len(w.pending))
	}
	_ = w.Close()
	if strings.Contains(out.String(), "password-value") || strings.Count(out.String(), "[REDACTED]") != 600 {
		t.Fatal("concurrent stream leaked or lost secret replacement")
	}
	if _, err := w.Write([]byte("late")); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("closed write: %v", err)
	}
}

func TestStageOutputRedactsAllDestinationsAndPreservesPlainConfig(t *testing.T) {
	store := logstore.NewMemory(1<<20, 16)
	var frames [][]byte
	e := &Executor{logStore: store, logBroadcast: func(_ string, b []byte) { frames = append(frames, append([]byte(nil), b...)) }}
	sr := &model.StageRun{}
	w, finish := e.newStageOutput(WithLogSecrets(context.Background(), []string{"private-value"}), "run", &model.Stage{ID: "stage"}, sr)
	_, _ = io.WriteString(w, "database=public-name password=private-")
	_, _ = io.WriteString(w, "value\n")
	finish()
	entries, err := store.Read(context.Background(), "run", "stage", 0)
	if err != nil || len(entries) != 1 {
		t.Fatalf("history %v %v", entries, err)
	}
	if sr.Logs != "database=public-name password=[REDACTED]\n" || entries[0].Line != "database=public-name password=[REDACTED]" {
		t.Fatalf("unsafe capture/history %q %v", sr.Logs, entries)
	}
	for _, frame := range frames {
		if !json.Valid(frame) || bytes.Contains(frame, []byte("private-value")) {
			t.Fatalf("unsafe frame %q", frame)
		}
	}
}

func TestRedactedErrorPreservesCancellation(t *testing.T) {
	err := redactLogError(WithLogSecrets(context.Background(), []string{"private-value"}), fmt.Errorf("private-value: %w", context.Canceled))
	if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "private-value") {
		t.Fatalf("bad wrapped error %v", err)
	}
}

func TestSecretLogWriterProtectsLateRegistryCredential(t *testing.T) {
	ctx := WithLogSecrets(context.Background(), nil)
	var out bytes.Buffer
	w := NewSecretLogWriter(ctx, &out)
	RegisterLogSecrets(ctx, "registry-secret")
	_, _ = io.WriteString(w, "authorization failed: registry-")
	_, _ = io.WriteString(w, "secret")
	_ = w.Close()
	if got := out.String(); got != "authorization failed: [REDACTED]" {
		t.Fatalf("late credential leaked: %q", got)
	}
}

func TestStageOutputMetadataCannotExportKnownSecret(t *testing.T) {
	ctx := WithLogSecrets(context.Background(), []string{"private-token"})
	sr := &model.StageRun{Outputs: map[string]string{"result": "private-token"}, Artifacts: []model.Artifact{{Ref: "https://host/private-token"}}}
	if err := protectStageMetadata(ctx, sr); err == nil {
		t.Fatal("secret output accepted")
	}
	encoded, _ := json.Marshal(sr)
	if bytes.Contains(encoded, []byte("private-token")) {
		t.Fatalf("metadata leaked %s", encoded)
	}
}
