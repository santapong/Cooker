package notifier

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return netSMTPSender{}.SendMailContext(ctx, addr, auth, from, to, msg)
}

// net/smtp.SendMail has no context/deadline and can hang a delivery goroutine.
// Keep its STARTTLS/Auth behavior with a deadline-bound connection instead.
func (netSMTPSender) SendMailContext(ctx context.Context, addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer c.Close()
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	}
	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return err
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, recipient := range to {
		if err := c.Rcpt(recipient); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		_ = w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
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

func (e *EmailNotifier) Send(ctx context.Context, target Target, event Event) error {
	var cfg EmailConfig
	if err := target.DecodeConfig(&cfg); err != nil {
		return invalidConfig()
	}
	if cfg.Host == "" {
		return invalidConfig()
	}
	if cfg.From == "" {
		return invalidConfig()
	}
	if len(cfg.To) == 0 {
		return invalidConfig()
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
	var err error
	if bounded, ok := e.sender.(interface {
		SendMailContext(context.Context, string, smtp.Auth, string, []string, []byte) error
	}); ok {
		err = bounded.SendMailContext(ctx, addr, auth, cfg.From, cfg.To, msg)
	} else {
		err = e.sender.SendMail(addr, auth, cfg.From, cfg.To, msg)
	}
	if err != nil {
		return uncertainSend()
	}
	return nil
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
