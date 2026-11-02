package keyrotation

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestCommandCancellationClosesPartialPrivatePipe(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var output bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- RunCommand(ctx, []string{"inventory"}, reader, &output) }()
	if _, err := writer.Write([]byte(`{"databaseUrl":"PRIVATE-CANARY`)); err != nil {
		t.Fatal("partial input was not read")
	}
	cancel()
	select {
	case code := <-done:
		if code != 2 || !strings.Contains(output.String(), ErrInput.Error()) || strings.Contains(output.String(), "PRIVATE-CANARY") {
			t.Fatal("cancelled private input did not fail safely")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled input stayed blocked")
	}
	if _, err := writer.Write([]byte("more")); err == nil {
		t.Fatal("cancelled reader was not closed")
	}
}

func TestCommandDeadlineIncludesPrivateInput(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	var output bytes.Buffer
	if code := RunCommand(ctx, []string{"rotate"}, reader, &output); code != 2 || !strings.Contains(output.String(), ErrInput.Error()) {
		t.Fatal("private input deadline did not return fixed refusal")
	}
}

func TestPrivateInputPrecancelledAndOversized(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if data, err := readPrivateInput(ctx, strings.NewReader("PRIVATE-CANARY")); data != nil || err != ErrInput {
		t.Fatal("pre-cancelled read accepted data")
	}
	var output bytes.Buffer
	if code := RunCommand(context.Background(), []string{"inventory"}, strings.NewReader(strings.Repeat("X", 65537)), &output); code != 2 {
		t.Fatal("oversized private input accepted")
	}
}
