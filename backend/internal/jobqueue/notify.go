package jobqueue

import (
	"context"
	"log/slog"
	"time"

	"github.com/lib/pq"
)

// NotifyChannel is the Postgres LISTEN channel the worker pool
// subscribes to. Enqueue issues NOTIFY <channel> after each insert
// so idle workers wake up immediately instead of waiting on the
// fallback poll tick.
const NotifyChannel = "cooker_jobs_new"

// Notifier is the wake-up signal used by the Pool. Implementations
// must be safe to call from multiple goroutines: Wait may be called
// from each worker concurrently, and Close from the boot path.
type Notifier interface {
	// Wait blocks until a wake signal arrives or timeout elapses.
	// Returns true if a signal arrived, false on timeout. ctx cancel
	// also returns false. Spurious wakeups are permitted; callers
	// must re-check the queue state regardless of return value.
	Wait(ctx context.Context, timeout time.Duration) bool
	Close() error
}

// PqListener wraps lib/pq's NewListener to deliver NOTIFY events to
// the Pool. Reconnects are handled by lib/pq internally; a slog Warn
// is emitted when the listener's state changes for operational
// visibility.
type PqListener struct {
	l *pq.Listener
}

// NewPqListener opens a dedicated Postgres connection (separate from
// the main connection pool) and subscribes to NotifyChannel. The
// connection string is the same DSN used elsewhere.
func NewPqListener(dsn string) (*PqListener, error) {
	minReconnect := 2 * time.Second
	maxReconnect := 30 * time.Second
	l := pq.NewListener(dsn, minReconnect, maxReconnect, func(ev pq.ListenerEventType, err error) {
		switch ev {
		case pq.ListenerEventConnected:
			slog.Info("jobqueue: listener connected")
		case pq.ListenerEventDisconnected:
			slog.Warn("jobqueue: listener disconnected", "err", err)
		case pq.ListenerEventReconnected:
			slog.Info("jobqueue: listener reconnected")
		case pq.ListenerEventConnectionAttemptFailed:
			slog.Warn("jobqueue: listener connect attempt failed", "err", err)
		}
	})
	if err := l.Listen(NotifyChannel); err != nil {
		_ = l.Close()
		return nil, err
	}
	return &PqListener{l: l}, nil
}

// Wait implements Notifier.
func (p *PqListener) Wait(ctx context.Context, timeout time.Duration) bool {
	select {
	case <-p.l.Notify:
		return true
	case <-time.After(timeout):
		return false
	case <-ctx.Done():
		return false
	}
}

// Close stops listening and releases the underlying connection.
func (p *PqListener) Close() error { return p.l.Close() }

// chanNotifier is an in-memory Notifier used by tests. Send a value
// on Wake to simulate a NOTIFY. Bufferred(1) so a Wake without a
// waiting receiver is not lost.
type chanNotifier struct {
	wake chan struct{}
}

// NewChanNotifier returns an in-memory Notifier suitable for tests.
func NewChanNotifier() *chanNotifier { return &chanNotifier{wake: make(chan struct{}, 1)} }

// Wake delivers one wake signal. Non-blocking; drops if the channel
// is already full (a previous wake hasn't been consumed yet — the
// queued one is sufficient).
func (c *chanNotifier) Wake() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *chanNotifier) Wait(ctx context.Context, timeout time.Duration) bool {
	select {
	case <-c.wake:
		return true
	case <-time.After(timeout):
		return false
	case <-ctx.Done():
		return false
	}
}

func (c *chanNotifier) Close() error { return nil }
