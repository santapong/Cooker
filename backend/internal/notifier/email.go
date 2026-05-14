package notifier

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"
)

// EmailConfig is the JSON shape stored in Target.Config for kind=email.
type EmailConfig struct {
	// SMTP server connection. Host is required; Port defaults to 587.
	Host string `json:"host"`
	Port int    `json:"port,omitempty"`
	// Auth credentials. Optional: leave both empty for unauthenticated
	// relays (uncommon but supported for internal SMTP gateways).
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	// Envelope and message addresses.
	From string   `json:"from"`
	To   []string `json:"to"`
}

// SMTPSender is the seam tests stub. Production uses net/smtp via
// netSMTPSender; tests pass a fake that just records the inputs.
type SMTPSender interface {
	SendMail(addr string, auth smtp.Auth, from string, to []string, msg []byte) error
}

type netSMTPSender struct{}

func (netSMTPSender) SendMail(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
	return smtp.SendMail(addr, auth, from, to, msg)
}

// EmailNotifier sends event notifications via SMTP.
type EmailNotifier struct {
	sender SMTPSender
}

func NewEmailNotifier() *EmailNotifier {
	return &EmailNotifier{sender: netSMTPSender{}}
}

// WithSender swaps the SMTP sender (tests).
func (e *EmailNotifier) WithSender(s SMTPSender) *EmailNotifier {
	e.sender = s
	return e
}

func (e *EmailNotifier) Kind() string { return "email" }

func (e *EmailNotifier) Send(_ context.Context, target Target, event Event) error {
	var cfg EmailConfig
	if err := target.DecodeConfig(&cfg); err != nil {
		return fmt.Errorf("email: decode config: %w", err)
	}
	if cfg.Host == "" {
		return fmt.Errorf("email: target %s missing host", target.ID)
	}
	if cfg.From == "" {
		return fmt.Errorf("email: target %s missing from", target.ID)
	}
	if len(cfg.To) == 0 {
		return fmt.Errorf("email: target %s missing recipients", target.ID)
	}
	port := cfg.Port
	if port == 0 {
		port = 587
	}
	addr := fmt.Sprintf("%s:%d", cfg.Host, port)
	var auth smtp.Auth
	if cfg.Username != "" || cfg.Password != "" {
		auth = smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
	}
	subject := event.Title()
	body := event.Body()
	msg := buildMessage(cfg.From, cfg.To, subject, body)
	return e.sender.SendMail(addr, auth, cfg.From, cfg.To, msg)
}

// buildMessage produces an RFC-822-shaped email body. No HTML, no
// MIME parts — plain text keeps the dependency surface tiny and
// matches the most resilient "notification" use case.
func buildMessage(from string, to []string, subject, body string) []byte {
	var b strings.Builder
	b.WriteString("From: ")
	b.WriteString(from)
	b.WriteString("\r\n")
	b.WriteString("To: ")
	b.WriteString(strings.Join(to, ", "))
	b.WriteString("\r\n")
	b.WriteString("Subject: ")
	b.WriteString(subject)
	b.WriteString("\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return []byte(b.String())
}
