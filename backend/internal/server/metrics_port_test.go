package server

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/config"
)

// hasMetricsRoute reports whether /metrics is registered on the router.
func hasMetricsRoute(router *gin.Engine) bool {
	for _, r := range router.Routes() {
		if r.Path == "/metrics" {
			return true
		}
	}
	return false
}

// TestMetrics_DedicatedPort_NotOnMainRouter asserts the W2 / M0-1 split:
// when COOKER_METRICS_PORT > 0, /metrics is NOT registered on the public
// app router and IS served on the dedicated metrics listener.
func TestMetrics_DedicatedPort_NotOnMainRouter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })
	// Mirror New(): with a dedicated port set, the main router must omit
	// /metrics.
	registerMetricsRoute(router, 9090)
	if hasMetricsRoute(router) {
		t.Fatal("/metrics must NOT be registered on the main app router when a dedicated metrics port is set")
	}

	// Pick free ports for both the app and metrics listeners.
	appAddr := freeAddr(t)
	metricsAddr := freeAddr(t)
	_, metricsPortStr, _ := net.SplitHostPort(metricsAddr)
	metricsPort := mustAtoi(t, metricsPortStr)

	srv := &Server{
		router: router,
		config: &config.Config{
			Observability: config.ObservabilityConfig{
				MetricsEnabled: true,
				MetricsPort:    metricsPort,
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- srv.RunContext(ctx, appAddr) }()

	// The dedicated metrics listener serves /metrics.
	body := getWithRetry(t, "http://"+metricsAddr+"/metrics")
	if !strings.Contains(body, "# HELP") && !strings.Contains(body, "# TYPE") {
		t.Fatalf("metrics listener did not return Prometheus exposition format; got: %.120q", body)
	}

	// The app listener does NOT serve /metrics (404).
	resp, err := http.Get("http://" + appAddr + "/metrics")
	if err != nil {
		t.Fatalf("GET app /metrics: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("app router /metrics should 404 when dedicated port is set; got %d", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-runErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("RunContext returned unexpected error: %v", err)
		}
	case <-time.After(shutdownTimeout + 5*time.Second):
		t.Fatal("RunContext did not return after shutdown")
	}
}

// TestMetrics_SinglePort_OnMainRouter asserts the back-compat path:
// with MetricsPort == 0, /metrics is registered on the main app router and
// no dedicated listener is stood up.
func TestMetrics_SinglePort_OnMainRouter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	registerMetricsRoute(router, 0)
	if !hasMetricsRoute(router) {
		t.Fatal("/metrics must be registered on the main app router when MetricsPort == 0")
	}

	appAddr := freeAddr(t)
	srv := &Server{
		router: router,
		config: &config.Config{
			Observability: config.ObservabilityConfig{
				MetricsEnabled: true,
				MetricsPort:    0, // single-port mode; no dedicated listener
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- srv.RunContext(ctx, appAddr) }()

	body := getWithRetry(t, "http://"+appAddr+"/metrics")
	if !strings.Contains(body, "# HELP") && !strings.Contains(body, "# TYPE") {
		t.Fatalf("main router /metrics did not return Prometheus exposition format; got: %.120q", body)
	}

	cancel()
	select {
	case err := <-runErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("RunContext returned unexpected error: %v", err)
		}
	case <-time.After(shutdownTimeout + 5*time.Second):
		t.Fatal("RunContext did not return after shutdown")
	}
}

// freeAddr returns a currently-free 127.0.0.1 host:port. There is an
// inherent race between closing and re-binding, but it's reliable enough
// for a unit test and matches the existing server_shutdown_test pattern.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			t.Fatalf("not a numeric port: %q", s)
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func getWithRetry(t *testing.T, url string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err != nil {
			lastErr = err
			time.Sleep(20 * time.Millisecond)
			continue
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return string(b)
		}
		lastErr = errors.New(resp.Status)
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("GET %s never succeeded: %v", url, lastErr)
	return ""
}
