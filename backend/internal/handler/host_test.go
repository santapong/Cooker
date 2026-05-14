package handler

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
)

// testEd25519PEM returns a fresh PEM-encoded ed25519 private key.
// Used by SSH-host handler tests to exercise the real Put → Get
// secrets path without making the test brittle to key parsing.
func testEd25519PEM(t *testing.T) []byte {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

// withHostService attaches a HostService wired to the same store
// and secrets backend the handler already uses. The default
// newTestHandler doesn't set this, so any test that wants the
// SSH-host path must call this first.
func withHostService(h *Handler) *Handler {
	h.Hosts = service.NewHostService(h.Store, h.Secrets)
	return h
}

// TestCreateHost_SSH_RedactsPrivateKey is the critical security
// assertion: after POST /hosts with sshPrivateKeyPem, the response
// must not contain any byte sequence resembling the PEM body.
func TestCreateHost_SSH_RedactsPrivateKey(t *testing.T) {
	h := withHostService(newTestHandler(t))

	r := gin.New()
	r.POST("/hosts", h.CreateHost)
	r.GET("/hosts", h.ListHosts)
	r.GET("/hosts/:id", h.GetHost)

	keyPEM := testEd25519PEM(t)
	req := map[string]any{
		"name":             "remote-vm-1",
		"kind":             "ssh-docker",
		"sshEndpoint":      "1.2.3.4:22",
		"sshUser":          "deploy",
		"sshPort":          22,
		"sshStrictHostKey": true,
		"sshPrivateKeyPem": string(keyPEM),
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/hosts", req))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if strings.Contains(body, "PRIVATE KEY") {
		t.Fatalf("Create response leaked PEM block:\n%s", body)
	}
	if strings.Contains(body, "sshPrivateKeyPem") {
		t.Fatalf("Create response echoed sshPrivateKeyPem field:\n%s", body)
	}
	if strings.Contains(body, "sshPrivateKeyRef") {
		t.Fatalf("Create response leaked sshPrivateKeyRef:\n%s", body)
	}

	// Decode to assert hasSSHPrivateKey flag is true.
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if hk, _ := got["hasSSHPrivateKey"].(bool); !hk {
		t.Errorf("hasSSHPrivateKey should be true, got %v", got["hasSSHPrivateKey"])
	}

	// Now List + Get: same redaction must hold.
	id, _ := got["id"].(string)
	if id == "" {
		t.Fatal("no id in create response")
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/hosts/"+id, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("get: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "PRIVATE KEY") {
		t.Fatalf("Get response leaked PEM block:\n%s", w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/hosts", nil))
	if strings.Contains(w.Body.String(), "PRIVATE KEY") {
		t.Fatalf("List response leaked PEM block:\n%s", w.Body.String())
	}
}

func TestCreateHost_SSH_RejectsInvalidKey(t *testing.T) {
	h := withHostService(newTestHandler(t))
	r := gin.New()
	r.POST("/hosts", h.CreateHost)

	req := map[string]any{
		"name":             "vm",
		"kind":             "ssh-docker",
		"sshEndpoint":      "1.2.3.4:22",
		"sshUser":          "deploy",
		"sshStrictHostKey": true,
		"sshPrivateKeyPem": "not a real PEM",
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/hosts", req))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 invalid key, got %d %s", w.Code, w.Body.String())
	}
}

func TestCreateHost_SSH_RequiresKey(t *testing.T) {
	h := withHostService(newTestHandler(t))
	r := gin.New()
	r.POST("/hosts", h.CreateHost)

	req := map[string]any{
		"name":             "vm",
		"kind":             "ssh-docker",
		"sshEndpoint":      "1.2.3.4:22",
		"sshUser":          "deploy",
		"sshStrictHostKey": true,
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/hosts", req))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 missing key, got %d %s", w.Code, w.Body.String())
	}
}

func TestCreateHost_NonSSH_Unchanged(t *testing.T) {
	h := withHostService(newTestHandler(t))
	r := gin.New()
	r.POST("/hosts", h.CreateHost)

	req := model.Host{
		Name:           "legacy-docker",
		Kind:           model.HostKindDocker,
		Reachability:   model.HostDirect,
		DockerEndpoint: "tcp://10.0.0.3:2375",
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/hosts", req))
	if w.Code != http.StatusCreated {
		t.Fatalf("create docker host: %d %s", w.Code, w.Body.String())
	}
}

func TestUpdateHost_SSH_PreservesKeyRefWhenPemOmitted(t *testing.T) {
	h := withHostService(newTestHandler(t))
	r := gin.New()
	r.POST("/hosts", h.CreateHost)
	r.PUT("/hosts/:id", h.UpdateHost)
	r.GET("/hosts/:id", h.GetHost)

	keyPEM := testEd25519PEM(t)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/hosts", map[string]any{
		"name":             "vm",
		"kind":             "ssh-docker",
		"sshEndpoint":      "1.2.3.4:22",
		"sshUser":          "deploy",
		"sshStrictHostKey": true,
		"sshPrivateKeyPem": string(keyPEM),
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	id := created["id"].(string)

	// Update without sshPrivateKeyPem: key should be preserved.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPut, "/hosts/"+id, map[string]any{
		"name":             "vm-renamed",
		"kind":             "ssh-docker",
		"sshEndpoint":      "1.2.3.4:22",
		"sshUser":          "deploy",
		"sshStrictHostKey": true,
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	var updated map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &updated)
	if hk, _ := updated["hasSSHPrivateKey"].(bool); !hk {
		t.Fatalf("hasSSHPrivateKey lost after Update")
	}
}
