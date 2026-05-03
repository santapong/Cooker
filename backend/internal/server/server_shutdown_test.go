package server

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// TestServer_RunContext_DrainsInflight asserts that cancelling the
// run context lets in-flight requests complete before the server
// returns from RunContext.
func TestServer_RunContext_DrainsInflight(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	requestStarted := make(chan struct{})
	var completed atomic.Bool
	router.GET("/slow", func(c *gin.Context) {
		close(requestStarted)
		time.Sleep(200 * time.Millisecond)
		completed.Store(true)
		c.String(http.StatusOK, "done")
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	srv := &Server{router: router}
	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- srv.RunContext(ctx, addr) }()

	// Wait until the server is accepting connections.
	deadline := time.Now().Add(2 * time.Second)
	var resp *http.Response
	clientDone := make(chan error, 1)
	go func() {
		var err error
		for time.Now().Before(deadline) {
			resp, err = http.Get("http://" + addr + "/slow")
			if err == nil {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		clientDone <- err
	}()

	<-requestStarted
	cancel()

	if err := <-clientDone; err != nil {
		t.Fatalf("client request failed: %v", err)
	}
	if resp != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	select {
	case err := <-runErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("RunContext returned unexpected error: %v", err)
		}
	case <-time.After(shutdownTimeout + 5*time.Second):
		t.Fatal("RunContext did not return after shutdown")
	}
	if !completed.Load() {
		t.Fatal("in-flight request was cut off; should have drained")
	}
}
