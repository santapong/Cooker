package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/model"
)

const updateFixture = `# demo stack
services:
  api:
    image: acme/api:1.0 # pinned
    ports:
      - "9000:9000"
    environment:
      - LOG_LEVEL=info
  db:
    image: postgres:16
`

func putCompose(t *testing.T, h *Handler, name, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := gin.New()
	r.PUT("/compose/services/:name", h.UpdateComposeService)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/compose/services/"+name, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func TestUpdateComposeService_RewritesTheFile(t *testing.T) {
	base := t.TempDir()
	file := filepath.Join(base, "docker-compose.yml")
	if err := os.WriteFile(file, []byte(updateFixture), 0o640); err != nil {
		t.Fatal(err)
	}
	h := &Handler{composeBaseDir: base}

	w := putCompose(t, h, "api", `{"composePath":"docker-compose.yml","image":"acme/api:1.1","ports":["9000:9000","9443:9443"],"environment":{"LOG_LEVEL":"debug","TRACE":"1"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Message string             `json:"message"`
		Service string             `json:"service"`
		Graph   model.ComposeGraph `json:"graph"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Service != "api" || resp.Message == "" {
		t.Errorf("resp = %+v", resp)
	}
	var api *model.ComposeService
	for i := range resp.Graph.Services {
		if resp.Graph.Services[i].Name == "api" {
			api = &resp.Graph.Services[i]
		}
	}
	if api == nil || api.Image != "acme/api:1.1" || len(api.Ports) != 2 || api.Environment["LOG_LEVEL"] != "debug" || api.Environment["TRACE"] != "1" {
		t.Errorf("returned graph api = %+v", api)
	}

	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	for _, want := range []string{"# demo stack", "acme/api:1.1", "9443:9443", "- LOG_LEVEL=debug", "- TRACE=1", "postgres:16"} {
		if !strings.Contains(text, want) {
			t.Errorf("file missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "acme/api:1.0") {
		t.Errorf("old image still on disk:\n%s", text)
	}
	if info, _ := os.Stat(file); info.Mode().Perm() != 0o640 {
		t.Errorf("file mode = %v, want 0640 preserved", info.Mode().Perm())
	}
	if leftovers, _ := filepath.Glob(filepath.Join(base, ".cooker-compose-*")); len(leftovers) != 0 {
		t.Errorf("temp files left behind: %v", leftovers)
	}
}

func TestUpdateComposeService_Errors(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "docker-compose.yml"), []byte(updateFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	h := &Handler{composeBaseDir: base}
	cases := []struct {
		name string
		svc  string
		body string
		code int
	}{
		{"unknown service", "ghost", `{"image":"x"}`, http.StatusNotFound},
		{"path escape", "api", `{"composePath":"../docker-compose.yml","image":"x"}`, http.StatusBadRequest},
		{"missing file", "api", `{"composePath":"nope.yml","image":"x"}`, http.StatusBadRequest},
		{"empty patch", "api", `{}`, http.StatusBadRequest},
		{"wrong types", "api", `{"ports":"9000:9000"}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := putCompose(t, h, tc.svc, tc.body)
			if w.Code != tc.code {
				t.Errorf("status = %d, want %d (body %s)", w.Code, tc.code, w.Body.String())
			}
		})
	}
	got, _ := os.ReadFile(filepath.Join(base, "docker-compose.yml"))
	if string(got) != updateFixture {
		t.Errorf("file changed by a failed request:\n%s", got)
	}
}
