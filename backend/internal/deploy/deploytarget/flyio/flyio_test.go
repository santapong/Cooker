package flyio

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/deploy/deploytarget"
)

// newTestTarget builds a Target wired to the given httptest.Server.
func newTestTarget(srv *httptest.Server) *Target {
	t := New("tok", "iad")
	t.HTTP = srv.Client()
	// Override baseURL by repointing the client through the test server.
	// Because baseURL is a package-level const we use a thin wrapper
	// approach: the test server handles all paths verbatim.
	return t
}

// testServer builds a minimal Fly Machines API stub. handlers is a map
// of "<METHOD> <path>" -> handler func. Unregistered routes return 404.
func testServer(t *testing.T, routes map[string]http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	// httptest server receives requests at the original baseURL path.
	// We intercept by overriding the http.Client transport inside do().
	// Instead, we swap baseURL per-test by monkey-patching through the
	// do() method: since Target.do() uses baseURL directly we need a
	// real http.RoundTripper that rewrites the host.
	// For simplicity: return an httptest.Server and rewire via a
	// custom RoundTripper that changes the scheme+host.
	_ = mux // unused; see customTransport below
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		if h, ok := routes[key]; ok {
			h(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// rewiredTarget returns a Target whose HTTP client and baseURL constant
// are both pointed at the given server. Because baseURL is a package
// const we achieve the redirect by embedding a custom RoundTripper.
func rewiredTarget(token string, srv *httptest.Server) *Target {
	rt := &rewriteTransport{base: srv.URL, inner: http.DefaultTransport}
	return &Target{
		Token:  token,
		Region: "iad",
		HTTP:   &http.Client{Transport: rt},
	}
}

// rewriteTransport rewrites every request's scheme+host to the target
// base, while preserving path and query. This lets tests stub the
// Fly Machines API without changing the package-level const.
type rewriteTransport struct {
	base  string // e.g. "http://127.0.0.1:12345"
	inner http.RoundTripper
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	// Strip the original scheme+host and replace with test server base.
	cloned.URL.Scheme = "http"
	cloned.URL.Host = strings.TrimPrefix(rt.base, "http://")
	return rt.inner.RoundTrip(cloned)
}

// TestFly_F2_DeployUpdatesExistingMachine asserts that when machines
// already exist for an app, Deploy calls the update endpoint (POST
// /apps/<id>/machines/<machine_id>) rather than creating a new machine.
func TestFly_F2_DeployUpdatesExistingMachine(t *testing.T) {
	const appID = "my-app"
	const machineID = "abc123"

	createCalled := false
	updateCalled := false

	routes := map[string]http.HandlerFunc{
		// List machines — return one existing machine.
		"GET /v1/apps/" + appID + "/machines": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": machineID, "state": "started"},
			})
		},
		// Update existing machine endpoint.
		"POST /v1/apps/" + appID + "/machines/" + machineID: func(w http.ResponseWriter, r *http.Request) {
			updateCalled = true
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{"id": machineID})
		},
		// Create new machine endpoint — must NOT be called on re-deploy.
		"POST /v1/apps/" + appID + "/machines": func(w http.ResponseWriter, r *http.Request) {
			createCalled = true
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{"id": "new-machine"})
		},
	}
	srv := testServer(t, routes)
	target := rewiredTarget("tok", srv)

	spec := deploytarget.Spec{
		AppID: appID,
		Image: "registry.example.com/my-app:v2",
	}
	if err := target.Deploy(context.Background(), spec); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if createCalled {
		t.Error("Deploy called CREATE on re-deploy; expected UPDATE (F-2 regression)")
	}
	if !updateCalled {
		t.Error("Deploy did not call the per-machine update endpoint")
	}
}

// TestFly_F2_DeployCreatesOnFirstDeploy asserts that when no machines
// exist yet, Deploy takes the create path.
func TestFly_F2_DeployCreatesOnFirstDeploy(t *testing.T) {
	const appID = "new-app"

	createCalled := false

	routes := map[string]http.HandlerFunc{
		// List machines — empty (first deploy).
		"GET /v1/apps/" + appID + "/machines": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]any{})
		},
		// Create machine endpoint — should be called.
		"POST /v1/apps/" + appID + "/machines": func(w http.ResponseWriter, r *http.Request) {
			createCalled = true
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{"id": "m1"})
		},
	}
	srv := testServer(t, routes)
	target := rewiredTarget("tok", srv)

	spec := deploytarget.Spec{
		AppID: appID,
		Image: "registry.example.com/new-app:v1",
	}
	if err := target.Deploy(context.Background(), spec); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if !createCalled {
		t.Error("Deploy did not call CREATE on first deploy")
	}
}

// TestFly_F3_RollbackUsesPerMachineRestart asserts that Rollback calls
// POST /apps/<id>/machines/<machine_id>/restart (the real Fly API URL)
// and NOT the defunct /apps/<id>/machines/restart bulk endpoint.
func TestFly_F3_RollbackUsesPerMachineRestart(t *testing.T) {
	const appID = "crash-app"
	const machineID = "m99"

	bulkRestartCalled := false
	perMachineRestartCalled := false

	routes := map[string]http.HandlerFunc{
		"GET /v1/apps/" + appID + "/machines": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": machineID, "state": "stopped"},
			})
		},
		// Per-machine restart — the correct URL.
		"POST /v1/apps/" + appID + "/machines/" + machineID + "/restart": func(w http.ResponseWriter, r *http.Request) {
			perMachineRestartCalled = true
			w.WriteHeader(http.StatusOK)
		},
		// Bulk restart — the old (broken) URL. Must NOT be called.
		"POST /v1/apps/" + appID + "/machines/restart": func(w http.ResponseWriter, r *http.Request) {
			bulkRestartCalled = true
			http.NotFound(w, r)
		},
	}
	srv := testServer(t, routes)
	target := rewiredTarget("tok", srv)

	if err := target.Rollback(context.Background(), appID); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if bulkRestartCalled {
		t.Error("Rollback called the defunct bulk-restart endpoint (F-3 regression)")
	}
	if !perMachineRestartCalled {
		t.Error("Rollback did not call per-machine restart endpoint")
	}
}

// TestFly_RollbackNoMachines asserts a clear error when no machines exist.
func TestFly_RollbackNoMachines(t *testing.T) {
	const appID = "ghost-app"

	routes := map[string]http.HandlerFunc{
		"GET /v1/apps/" + appID + "/machines": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]any{})
		},
	}
	srv := testServer(t, routes)
	target := rewiredTarget("tok", srv)

	err := target.Rollback(context.Background(), appID)
	if err == nil {
		t.Fatal("expected error when no machines exist, got nil")
	}
	if !strings.Contains(err.Error(), "no machines found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestFly_RequireConfig ensures missing Token returns ErrUnavailable.
func TestFly_RequireConfig(t *testing.T) {
	target := New("", "")
	err := target.requireConfig()
	if err == nil {
		t.Fatal("expected error for empty token")
	}
	if !strings.Contains(err.Error(), "Token required") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestFly_Status_Healthy checks the status health aggregation.
func TestFly_Status_Healthy(t *testing.T) {
	const appID = "running-app"

	routes := map[string]http.HandlerFunc{
		"GET /v1/apps/" + appID + "/machines": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": "m1", "state": "started"},
				{"id": "m2", "state": "started"},
			})
		},
	}
	srv := testServer(t, routes)
	target := rewiredTarget("tok", srv)

	st, err := target.Status(context.Background(), appID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !st.Healthy {
		t.Error("expected Healthy=true when machines are started")
	}
	if st.Replicas != 2 {
		t.Errorf("Replicas = %d, want 2", st.Replicas)
	}
	wantURL := fmt.Sprintf("https://%s.fly.dev", appID)
	if st.URL != wantURL {
		t.Errorf("URL = %q, want %q", st.URL, wantURL)
	}
}
