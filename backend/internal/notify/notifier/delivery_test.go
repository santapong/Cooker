package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"
)

func TestSafeEventExcludesUnredactedData(t *testing.T) {
	const canary = "fixture-secret-do-not-export"
	for _, et := range []EventType{EventRunSucceeded, EventRunFailed, EventRunCancelled, EventDeploySucceeded, EventDeployFailed, EventDeployCancelled, EventBuildFailed, EventCanaryPromoted, EventCanaryAborted, EventCanaryFailed} {
		event, err := SafeEvent(Event{Type: et, RunID: "run-fixture", CanaryID: "canary-fixture", PipelineID: "pipe-fixture", AppID: "app-fixture", PipelineName: canary, AppName: canary, Environment: canary, Status: canary, Error: canary, URL: "https://" + canary})
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(event)
		if bytes.Contains(b, []byte(canary)) {
			t.Fatal("safe event leaked a configured value")
		}
		if event.ID == "" {
			t.Fatal("missing stable event identity")
		}
	}
	if _, err := SafeEvent(Event{Type: "unknown"}); err == nil {
		t.Fatal("unknown event accepted")
	}
	if _, err := SafeEvent(Event{Type: EventRunFailed, RunID: "http://credential@host"}); err == nil {
		t.Fatal("non-identifier accepted")
	}
}

func TestTerminalIdentityDeduplicatesCallbackAndReconciliation(t *testing.T) {
	first, _ := SafeEvent(Event{Type: EventBuildFailed, RunID: "run-one", AppID: "app-one"})
	repaired, _ := SafeEvent(Event{Type: EventDeployFailed, RunID: "run-one", AppID: "app-one", Timestamp: time.Now().Add(time.Hour)})
	if first.ID != repaired.ID {
		t.Fatal("same terminal operation got different identities")
	}
	canary, _ := SafeEvent(Event{Type: EventCanaryFailed, RunID: "run-one", CanaryID: "canary-one"})
	if canary.ID == first.ID {
		t.Fatal("canary event collided with deployment outcome")
	}
	if deliveryID(first.ID, "target-one") != deliveryID(repaired.ID, "target-one") {
		t.Fatal("delivery identity changed")
	}
}

func TestDispatcherDoesNotLogRawAdapterErrors(t *testing.T) {
	const canary = "fixture-receiver-body-secret"
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(previous)
	ts := NewMemoryTargetStore()
	_ = ts.Create(context.Background(), Target{ID: "target-one", Kind: "webhook", Enabled: true})
	r := NewRegistry()
	fake := &fakeNotifier{kind: "webhook", err: errors.New(canary)}
	r.Register(fake)
	err := NewDispatcher(ts, r).Dispatch(context.Background(), Event{Type: EventRunFailed, RunID: "run-one", Error: canary})
	if err == nil || strings.Contains(err.Error(), canary) || strings.Contains(logs.String(), canary) {
		t.Fatal("raw adapter error escaped")
	}
	if strings.Contains(fake.sent[0].Error, canary) {
		t.Fatal("raw execution error reached adapter")
	}
}

type ignoresCancellation struct{ released <-chan struct{} }

func (*ignoresCancellation) Kind() string                                { return "webhook" }
func (n *ignoresCancellation) Send(context.Context, Target, Event) error { <-n.released; return nil }

func TestBoundedSendMarksLateAdaptersUncertain(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err := boundedSend(ctx, &ignoresCancellation{released: release}, Target{}, Event{})
	if !safeFailure(err).Uncertain {
		t.Fatal("timed out adapter must remain uncertain")
	}
}

func TestProductionSMTPConnectionHonorsContextDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err == nil {
			defer conn.Close()
			<-release
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	err = netSMTPSender{}.SendMailContext(ctx, listener.Addr().String(), nil, "from@fixture.invalid", []string{"to@fixture.invalid"}, []byte("fixture"))
	if err == nil || time.Since(started) > time.Second {
		t.Fatal("SMTP connection ignored deadline")
	}
}
