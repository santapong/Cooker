package notifier

import (
	"context"
	"encoding/json"
	"errors"
	"net/smtp"
	"strings"
	"testing"
)

type fakeSender struct {
	addr string
	from string
	to   []string
	msg  []byte
	err  error
}

func (f *fakeSender) SendMail(addr string, _ smtp.Auth, from string, to []string, msg []byte) error {
	f.addr = addr
	f.from = from
	f.to = to
	f.msg = msg
	return f.err
}

func TestEmailNotifierBuildsMessageAndDials(t *testing.T) {
	cfg := EmailConfig{Host: "smtp.example.com", From: "bot@cooker.local", To: []string{"a@x", "b@x"}}
	cfgJSON, _ := json.Marshal(cfg)
	target := Target{ID: "e1", Kind: "email", Enabled: true, Config: cfgJSON}

	fake := &fakeSender{}
	n := NewEmailNotifier().WithSender(fake)
	if err := n.Send(context.Background(), target, Event{Type: EventRunFailed, PipelineName: "x"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if fake.addr != "smtp.example.com:587" {
		t.Fatalf("addr=%q want default port 587", fake.addr)
	}
	if fake.from != cfg.From {
		t.Fatalf("from=%q", fake.from)
	}
	if len(fake.to) != 2 {
		t.Fatalf("to=%v", fake.to)
	}
	if !strings.Contains(string(fake.msg), "Subject: ") {
		t.Fatal("message missing Subject header")
	}
	if !strings.Contains(string(fake.msg), "Content-Type: text/plain") {
		t.Fatal("message missing Content-Type header")
	}
}

func TestEmailNotifierMissingFieldsError(t *testing.T) {
	cases := map[string]EmailConfig{
		"no-host": {From: "x@y", To: []string{"a@y"}},
		"no-from": {Host: "s", To: []string{"a@y"}},
		"no-to":   {Host: "s", From: "x@y"},
	}
	for name, c := range cases {
		cfgJSON, _ := json.Marshal(c)
		n := NewEmailNotifier().WithSender(&fakeSender{})
		err := n.Send(context.Background(), Target{ID: name, Kind: "email", Config: cfgJSON}, Event{Type: EventRunFailed})
		if err == nil {
			t.Errorf("%s: want missing-field error", name)
		}
	}
}

func TestEmailNotifierPropagatesSenderError(t *testing.T) {
	cfgJSON, _ := json.Marshal(EmailConfig{Host: "s", From: "x@y", To: []string{"a@y"}})
	want := errors.New("smtp: nope")
	fake := &fakeSender{err: want}
	n := NewEmailNotifier().WithSender(fake)
	err := n.Send(context.Background(), Target{ID: "e1", Kind: "email", Config: cfgJSON}, Event{Type: EventRunFailed})
	if !errors.Is(err, want) {
		t.Fatalf("want %v, got %v", want, err)
	}
}
