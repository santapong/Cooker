package render

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/deploytarget"
)

// rewiredTarget returns a Target whose HTTP client is pointed at srv
// and whose Token is set so requireConfig() passes.
func rewiredTarget(token string, srv *httptest.Server) *Target {
	rt := &rewriteTransport{base: srv.URL, inner: http.DefaultTransport}
	return &Target{
		Token: token,
		HTTP:  &http.Client{Transport: rt},
	}
}

// rewriteTransport rewrites every request's scheme+host to the target
// base, so tests stub the Render API without touching the package-level
// const.
type rewriteTransport struct {
	base  string
	inner http.RoundTripper
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.URL.Scheme = "http"
	cloned.URL.Host = strings.TrimPrefix(rt.base, "http://")
	return rt.inner.RoundTrip(cloned)
}

// TestRender_StatusURLDecodes is the regression test that locks the
// R-2 fix: the Render API returns the public URL nested under
// service.serviceDetails.url, NOT as a flat "serviceDetails.url" key.
// The old struct tag `json:"serviceDetails.url"` decoded nothing; the
// new nested struct must decode the URL correctly.
func TestRender_StatusURLDecodes(t *testing.T) {
	const appID = "my-render-svc"
	const svcID = "srv-abc123"
	const wantURL = "https://my-render-svc.onrender.com"

	// Render API shape for GET /services/<id>:
	//   {"service":{"suspended":"not_suspended","serviceDetails":{"url":"<url>"}}}
	svcPayload := map[string]any{
		"service": map[string]any{
			"suspended": "not_suspended",
			"serviceDetails": map[string]any{
				"url": wantURL,
			},
		},
	}

	mux := http.NewServeMux()
	// GET /v1/services?name=<appID> — return one matching service.
	mux.HandleFunc("/v1/services", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"service": map[string]any{"id": svcID, "name": appID}},
		})
	})
	// GET /v1/services/<id> — return the service detail with nested URL.
	mux.HandleFunc("/v1/services/"+svcID, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(svcPayload)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	target := rewiredTarget("tok", srv)

	st, err := target.Status(context.Background(), appID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.URL != wantURL {
		t.Errorf("Status.URL = %q, want %q — did the nested serviceDetails decode correctly?", st.URL, wantURL)
	}
	if !st.Healthy {
		t.Errorf("expected Healthy=true for not_suspended service")
	}
}

// TestRender_StatusURL_EmptyWhenSuspended verifies that Healthy=false
// is returned for a suspended service, and that URL is still decoded.
func TestRender_StatusURL_EmptyWhenSuspended(t *testing.T) {
	const appID = "paused-svc"
	const svcID = "srv-paused"
	const wantURL = "https://paused-svc.onrender.com"

	svcPayload := map[string]any{
		"service": map[string]any{
			"suspended": "suspended",
			"serviceDetails": map[string]any{
				"url": wantURL,
			},
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/services", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"service": map[string]any{"id": svcID, "name": appID}},
		})
	})
	mux.HandleFunc("/v1/services/"+svcID, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(svcPayload)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	target := rewiredTarget("tok", srv)

	st, err := target.Status(context.Background(), appID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Healthy {
		t.Error("expected Healthy=false for suspended service")
	}
	if st.URL != wantURL {
		t.Errorf("Status.URL = %q, want %q", st.URL, wantURL)
	}
}

// TestRender_RequireConfig ensures missing Token returns ErrUnavailable.
func TestRender_RequireConfig(t *testing.T) {
	target := New("", "")
	_, err := target.Status(context.Background(), "any")
	if err == nil {
		t.Fatal("expected error for empty token")
	}
	if !strings.Contains(err.Error(), deploytarget.ErrUnavailable.Error()) {
		t.Errorf("expected ErrUnavailable in error chain, got: %v", err)
	}
}

// TestRender_ServiceNotFound verifies that Status returns an error when
// the named service doesn't exist in the Render workspace.
func TestRender_ServiceNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/services", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{}) // empty list
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	target := rewiredTarget("tok", srv)

	_, err := target.Status(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent service, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestRender_Deploy_TriggersDeployEndpoint is a smoke test that Deploy
// calls POST /services/<id>/deploys.
func TestRender_Deploy_TriggersDeployEndpoint(t *testing.T) {
	const appID = "deploy-svc"
	const svcID = "srv-d1"

	deployCalled := false

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/services", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"service": map[string]any{"id": svcID, "name": appID}},
		})
	})
	mux.HandleFunc("/v1/services/"+svcID+"/deploys", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			deployCalled = true
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{"id": "dep-1"})
		}
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	target := rewiredTarget("tok", srv)

	spec := deploytarget.Spec{
		AppID: appID,
		Image: "registry.example.com/deploy-svc:v3",
	}
	if err := target.Deploy(context.Background(), spec); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if !deployCalled {
		t.Error("Deploy did not POST to /services/<id>/deploys")
	}
}
