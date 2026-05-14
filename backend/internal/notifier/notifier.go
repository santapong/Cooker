// Package notifier delivers run / deploy event notifications to
// configured external channels (Slack, Discord, generic webhook,
// email). Adapted from Dokploy's notifications utils but with an
// explicit Notifier interface + Dispatcher rather than the chained
// if-else dispatch used in Dokploy, so new channels are a single-
// file change.
//
// The package is decoupled from the executor: the executor builds
// an Event and calls Dispatcher.Dispatch (or enqueues a
// notification-send job, in the durable path). Channels run
// independently; one failing channel does not block the others.
package notifier

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/santapong/cooker/internal/observability"
)

// EventType identifies what happened. Adding new event types is
// backwards-compatible: an existing target with an empty
// event_types filter receives the new type; a target with an
// explicit filter ignores the new type until added by the operator.
type EventType string

const (
	EventRunSucceeded    EventType = "run.succeeded"
	EventRunFailed       EventType = "run.failed"
	EventRunCancelled    EventType = "run.cancelled"
	EventDeploySucceeded EventType = "deploy.succeeded"
	EventDeployFailed    EventType = "deploy.failed"
	EventBuildFailed     EventType = "build.failed"
)

// Event is everything a channel needs to render a message. Fields
// are intentionally flat: a JSON-tagged struct keeps the
// queue-payload shape simple, and channels that need richer markup
// (Slack blocks, Discord embeds) derive it from these fields.
type Event struct {
	Type         EventType `json:"type"`
	PipelineID   string    `json:"pipelineId,omitempty"`
	PipelineName string    `json:"pipelineName,omitempty"`
	RunID        string    `json:"runId,omitempty"`
	AppID        string    `json:"appId,omitempty"`
	AppName      string    `json:"appName,omitempty"`
	Environment  string    `json:"environment,omitempty"`
	Status       string    `json:"status,omitempty"`
	Error        string    `json:"error,omitempty"`
	// URL is a deep link back to Cooker for context (run detail,
	// app detail, etc.). Optional; channels render it as a link
	// when present.
	URL       string    `json:"url,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// Title returns a one-line summary suitable for an email subject or
// a Slack "text" field. Concrete channels are free to compose their
// own message but this gives them a sensible default.
func (e Event) Title() string { return renderTitle(e) }

// Body returns a longer text block suitable for an email body or a
// Discord embed description. Also a default that channels may
// override.
func (e Event) Body() string { return renderBody(e) }

// Notifier is implemented by each channel adapter (slack.go,
// discord.go, webhook.go, email.go). Send is called concurrently
// from the Dispatcher; implementations must be safe to use from
// multiple goroutines.
type Notifier interface {
	// Kind is the channel identifier matching the kind column in
	// notification_targets. Used by the Dispatcher to look up the
	// right adapter for a Target.
	Kind() string
	// Send delivers one event to one configured target. The target
	// argument carries the channel-specific configuration (parsed
	// from the Target.Config JSONB column). Returning an error
	// causes the Dispatcher to log + record the metric; the other
	// targets are not affected.
	Send(ctx context.Context, target Target, event Event) error
}

// Registry maps channel Kind to its Notifier implementation. One
// Registry per Dispatcher.
type Registry struct {
	mu        sync.RWMutex
	notifiers map[string]Notifier
}

// NewRegistry returns an empty Registry. Call Register for each
// channel you want enabled.
func NewRegistry() *Registry { return &Registry{notifiers: make(map[string]Notifier)} }

// Register binds a Notifier to its Kind. Panics on duplicate Kind
// so a config mismatch surfaces at boot, not at the first event.
func (r *Registry) Register(n Notifier) {
	if n == nil {
		panic("notifier: Register with nil Notifier")
	}
	if n.Kind() == "" {
		panic("notifier: Register with empty Kind")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.notifiers[n.Kind()]; exists {
		panic("notifier: duplicate notifier for kind " + n.Kind())
	}
	r.notifiers[n.Kind()] = n
}

// Lookup returns the Notifier for a Kind, or nil if none registered.
func (r *Registry) Lookup(kind string) Notifier {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.notifiers[kind]
}

// Dispatcher fans out Events to all matching enabled Targets.
type Dispatcher struct {
	store    TargetStore
	registry *Registry
	// SendTimeout caps each individual Notifier.Send call. Zero
	// disables the timeout (Send sees the parent ctx unchanged).
	SendTimeout time.Duration
}

// NewDispatcher wires a TargetStore and Registry into a Dispatcher.
func NewDispatcher(store TargetStore, reg *Registry) *Dispatcher {
	if store == nil {
		panic("notifier: NewDispatcher with nil TargetStore")
	}
	if reg == nil {
		panic("notifier: NewDispatcher with nil Registry")
	}
	return &Dispatcher{store: store, registry: reg, SendTimeout: 10 * time.Second}
}

// Dispatch loads enabled Targets, filters by event type, and calls
// each matching Notifier.Send concurrently. Returns a multi-error
// joining all per-target failures so callers can log them; an
// overall nil means every matching target succeeded (or no target
// matched, which is also a success — silent is a valid notifier
// state).
func (d *Dispatcher) Dispatch(ctx context.Context, event Event) error {
	targets, err := d.store.ListEnabled(ctx)
	if err != nil {
		return err
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	var wg sync.WaitGroup
	errCh := make(chan error, len(targets))
	for _, target := range targets {
		if !target.Matches(event.Type) {
			continue
		}
		n := d.registry.Lookup(target.Kind)
		if n == nil {
			slog.Warn("notifier: no adapter registered for kind", "kind", target.Kind, "target", target.ID)
			observability.IncNotifierSent(target.Kind, false)
			continue
		}
		wg.Add(1)
		go func(t Target, n Notifier) {
			defer wg.Done()
			sendCtx := ctx
			if d.SendTimeout > 0 {
				var cancel context.CancelFunc
				sendCtx, cancel = context.WithTimeout(ctx, d.SendTimeout)
				defer cancel()
			}
			if err := n.Send(sendCtx, t, event); err != nil {
				slog.Warn("notifier: send failed", "kind", t.Kind, "target", t.ID, "err", err)
				observability.IncNotifierSent(t.Kind, false)
				errCh <- err
				return
			}
			observability.IncNotifierSent(t.Kind, true)
		}(target, n)
	}
	wg.Wait()
	close(errCh)
	var errs []error
	for e := range errCh {
		errs = append(errs, e)
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}
