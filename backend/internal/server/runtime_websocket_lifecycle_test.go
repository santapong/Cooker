package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/santapong/cooker/internal/logstore"
)

type runtimeRequestIdentity struct {
	app, service, run, replica, origin, ticket string
}

type runtimeRequestIdentityKey struct{}

// Synchronous delivery avoids losing the flood in the backend's own queue;
// the real connection and its bounded client queue still handle every frame.
// Publication is bounded even if the hub stops consuming during shutdown.
type runtimeLifecycleBackend struct {
	frames chan BroadcastMessage
	once   sync.Once
}

func (b *runtimeLifecycleBackend) Publish(msg BroadcastMessage) error {
	select {
	case b.frames <- msg:
		return nil
	case <-time.After(250 * time.Millisecond):
		return errors.New("bounded test publication expired")
	}
}

func (b *runtimeLifecycleBackend) Subscribe() <-chan BroadcastMessage { return b.frames }
func (b *runtimeLifecycleBackend) Close() error {
	b.once.Do(func() { close(b.frames) })
	return nil
}

func runtimeWait(t *testing.T, done <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("%s did not finish", name)
	}
}

func TestRuntimeLogsConnectionLifetime(t *testing.T) {
	for _, ending := range []string{"disconnect", "hub-close", "slow-client"} {
		t.Run(ending, func(t *testing.T) {
			const origin = "http://runtime.fixture"
			const frame = "recorded runtime marker [REDACTED]\n"
			identity := runtimeRequestIdentity{"app-1", "deploy", "run-1", "replica-1", origin, "owned-ticket"}
			backend := &runtimeLifecycleBackend{frames: make(chan BroadcastMessage)}
			h := NewWebSocketHubWithBackend([]string{origin}, backend)
			runDone := make(chan struct{})
			go func() { defer close(runDone); h.Run() }()
			returned, stopped, flood := make(chan struct{}), make(chan struct{}), make(chan struct{})
			started := make(chan error, 1)
			var producerContext context.Context
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("ticket") != identity.ticket {
					http.Error(w, "ticket required", http.StatusUnauthorized)
					return
				}
				requestDone := r.Context().Done()
				r = r.WithContext(context.WithValue(r.Context(), runtimeRequestIdentityKey{}, identity))
				h.HandleRuntimeLogs(w, r, identity.app, identity.service, func(ctx context.Context, out io.Writer) error {
					defer close(stopped)
					// net/http, rather than a manually cancelled context, proves
					// that the actual handler has returned before any log write.
					<-requestDone
					if ctx.Err() != nil {
						started <- errors.New("producer cancelled when HTTP handler returned")
						return ctx.Err()
					}
					if ctx.Value(runtimeRequestIdentityKey{}) != identity || r.Header.Get("Origin") != identity.origin || r.URL.Query().Get("ticket") != identity.ticket {
						started <- errors.New("request identity was not preserved")
						return errors.New("request identity was not preserved")
					}
					producerContext = ctx
					if _, err := io.WriteString(out, frame); err != nil {
						started <- err
						return err
					}
					started <- nil
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-flood:
					}
					payload := []byte(strings.Repeat("x", 64<<10))
					for i := 0; i < 600 && ctx.Err() == nil; i++ {
						if _, err := out.Write(payload); err != nil {
							return err
						}
					}
					<-ctx.Done()
					return ctx.Err()
				})
				close(returned)
			}))
			var conn *websocket.Conn
			t.Cleanup(func() {
				if conn != nil {
					_ = conn.Close()
				}
				_ = h.Close()
				srv.Close()
				runtimeWait(t, runDone, "hub loop")
			})
			var err error
			conn, _, err = websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"?ticket="+identity.ticket, http.Header{"Origin": {origin}})
			if err != nil {
				t.Fatalf("real WebSocket upgrade: %v", err)
			}
			runtimeWait(t, returned, "HTTP handler")
			select {
			case err := <-started:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("producer did not observe actual handler return")
			}
			_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			kind, data, err := conn.ReadMessage()
			if err != nil || kind != websocket.TextMessage || string(data) != frame {
				t.Fatalf("real runtime frame: kind=%d data=%q error=%v", kind, data, err)
			}
			switch ending {
			case "disconnect":
				_ = conn.Close()
			case "hub-close":
				if err := h.Close(); err != nil {
					t.Fatal(err)
				}
			case "slow-client":
				// Stop reading the real socket. Its writer stalls; publishing
				// beyond the bounded client queue must cancel the producer.
				close(flood)
			}
			runtimeWait(t, stopped, "runtime producer")
			if producerContext.Err() != context.Canceled {
				t.Fatalf("producer cancellation = %v", producerContext.Err())
			}
			if ending == "slow-client" {
				h.mu.RLock()
				remaining := len(h.clients["runtime-logs:"+identity.app+":"+identity.service])
				h.mu.RUnlock()
				if remaining != 0 {
					t.Fatalf("slow connection remains registered: %d", remaining)
				}
			}
		})
	}
}

func TestRuntimeLogsRejectsWrongOrigin(t *testing.T) {
	h := NewWebSocketHub([]string{"http://runtime.fixture"})
	go h.Run()
	defer h.Close()
	var produced atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleRuntimeLogs(w, r, "app-1", "deploy", func(context.Context, io.Writer) error {
			produced.Store(true)
			return nil
		})
	}))
	defer srv.Close()
	conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), http.Header{"Origin": {"http://other.fixture"}})
	if conn != nil {
		_ = conn.Close()
	}
	if err == nil || response == nil || response.StatusCode != http.StatusForbidden || produced.Load() {
		t.Fatalf("wrong origin admitted: error=%v produced=%v", err, produced.Load())
	}
	_ = response.Body.Close()
}

func TestRuntimeLogsClosedHubReturnsWithoutProducer(t *testing.T) {
	h := NewWebSocketHub([]string{"http://runtime.fixture"})
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	returned := make(chan struct{})
	var produced atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleRuntimeLogs(w, r, "app-1", "deploy", func(context.Context, io.Writer) error {
			produced.Store(true)
			return nil
		})
		close(returned)
	}))
	defer srv.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), http.Header{"Origin": {"http://runtime.fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	runtimeWait(t, returned, "closed-hub handler")
	if produced.Load() {
		t.Fatal("closed hub started a producer")
	}
}

type shutdownBacklog struct {
	reading, release chan struct{}
}

func (*shutdownBacklog) Append(context.Context, string, string, logstore.Entry) error { return nil }
func (s *shutdownBacklog) Read(context.Context, string, string, int) ([]logstore.Entry, error) {
	close(s.reading)
	<-s.release
	return []logstore.Entry{{Seq: 1, Line: "recorded backlog"}}, nil
}

func TestStageLogsShutdownDuringReplayReturns(t *testing.T) {
	for _, closedBeforeRequest := range []bool{false, true} {
		t.Run(map[bool]string{false: "during-replay", true: "before-request"}[closedBeforeRequest], func(t *testing.T) {
			h := NewWebSocketHub([]string{"http://runtime.fixture"})
			runDone := make(chan struct{})
			go func() { defer close(runDone); h.Run() }()
			backlog := &shutdownBacklog{make(chan struct{}), make(chan struct{})}
			h.SetLogStore(backlog)
			returned := make(chan struct{})
			if closedBeforeRequest {
				_ = h.Close()
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				h.HandleStageLogs(w, r, "run-1", "build")
				close(returned)
			}))
			defer srv.Close()
			defer h.Close()
			conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), http.Header{"Origin": {"http://runtime.fixture"}})
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if !closedBeforeRequest {
				runtimeWait(t, backlog.reading, "backlog read")
				_ = h.Close()
				close(backlog.release)
			}
			runtimeWait(t, returned, "stage log handler")
			runtimeWait(t, runDone, "stage log hub")
		})
	}
}
