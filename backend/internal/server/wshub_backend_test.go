package server

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestEncodeDecodeBroadcast_RoundTrip(t *testing.T) {
	cases := []BroadcastMessage{
		{Channel: "app-run:abc", Data: []byte("hello world")},
		{Channel: "pipeline-run:123e4567-e89b-12d3-a456-426614174000", Data: []byte{}},
		{Channel: "", Data: []byte("payload only")},
		{Channel: "x", Data: bytes.Repeat([]byte("y"), 8192)},
	}
	for _, in := range cases {
		t.Run(in.Channel, func(t *testing.T) {
			payload, err := encodeBroadcast(in)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			out, err := decodeBroadcast(payload)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if out.Channel != in.Channel {
				t.Errorf("channel = %q, want %q", out.Channel, in.Channel)
			}
			if !bytes.Equal(out.Data, in.Data) {
				t.Errorf("data mismatch (len got=%d want=%d)", len(out.Data), len(in.Data))
			}
		})
	}
}

func TestEncodeBroadcast_RejectsOversizedChannel(t *testing.T) {
	huge := strings.Repeat("c", 65536) // > uint16 max
	if _, err := encodeBroadcast(BroadcastMessage{Channel: huge}); err == nil {
		t.Fatal("expected error for oversized channel")
	}
}

func TestDecodeBroadcast_RejectsTruncated(t *testing.T) {
	cases := [][]byte{
		nil,                    // empty
		{0x01},                 // 1 byte (need at least 2)
		{0x00, 0x05},           // claims 5-byte channel but no body
		{0x00, 0x05, 'a', 'b'}, // claims 5-byte channel, only 2 bytes follow
	}
	for i, c := range cases {
		if _, err := decodeBroadcast(c); err == nil {
			t.Errorf("case %d: expected error for truncated payload %v", i, c)
		}
	}
}

func TestNextBackoff_DoublesAndCaps(t *testing.T) {
	max := 10 * time.Second
	cases := []struct {
		in, want time.Duration
	}{
		{500 * time.Millisecond, 1 * time.Second},
		{1 * time.Second, 2 * time.Second},
		{4 * time.Second, 8 * time.Second},
		{8 * time.Second, max}, // would double to 16s, capped at 10s
		{max, max},             // already at max stays at max
	}
	for _, c := range cases {
		if got := nextBackoff(c.in, max); got != c.want {
			t.Errorf("nextBackoff(%s, %s) = %s; want %s", c.in, max, got, c.want)
		}
	}
}

func TestSleepJitter_ReturnsTrueOnTimer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	start := time.Now()
	if !sleepJitter(ctx, 10*time.Millisecond) {
		t.Fatal("expected sleepJitter to return true when timer fires")
	}
	elapsed := time.Since(start)
	if elapsed < 10*time.Millisecond {
		t.Errorf("returned in %s; expected >= 10ms", elapsed)
	}
	// Upper bound: base + jitter (50%) + scheduler slack
	if elapsed > 200*time.Millisecond {
		t.Errorf("returned in %s; expected < 200ms", elapsed)
	}
}

func TestSleepJitter_ReturnsFalseOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	if sleepJitter(ctx, 10*time.Second) {
		t.Fatal("expected sleepJitter to return false when ctx is cancelled")
	}
	elapsed := time.Since(start)
	// Should bail out well before the 10-second timer.
	if elapsed > 500*time.Millisecond {
		t.Errorf("did not bail out promptly on ctx cancel: %s", elapsed)
	}
}
