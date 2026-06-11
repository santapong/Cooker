package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
)

// yamlRequest builds a POST carrying a raw YAML body (not JSON-encoded).
func yamlRequest(target, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/yaml")
	return req
}

func seedPipeline(t *testing.T, h *Handler, p *model.Pipeline) {
	t.Helper()
	if err := h.Store.Pipelines.Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}
}

func TestExportPipeline_RoundTripsThroughHTTP(t *testing.T) {
	h := newTestHandler(t)
	seedPipeline(t, h, &model.Pipeline{
		ID:          "p1",
		Name:        "release",
		Description: "ship it",
		Stages: []model.Stage{
			{ID: "build", Name: "Build", Type: model.StageTypeBuild},
			{ID: "deploy", Name: "Deploy", Type: model.StageTypeDeploy},
		},
		Edges: []model.Edge{{ID: "e1", Source: "build", Target: "deploy", Condition: "success"}},
	})

	r := gin.New()
	r.GET("/pipelines/:id/export", h.ExportPipeline)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/pipelines/p1/export", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/yaml") {
		t.Errorf("content-type: got %q", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "release.cooker.yaml") {
		t.Errorf("content-disposition: got %q", cd)
	}

	// The exported bytes must re-import cleanly via the service.
	imported, err := service.ImportPipelineYAML(w.Body.Bytes())
	if err != nil {
		t.Fatalf("re-import exported bytes: %v", err)
	}
	if imported.Name != "release" || len(imported.Stages) != 2 {
		t.Errorf("round-trip drift: %+v", imported)
	}
}

func TestExportPipeline_NotFound(t *testing.T) {
	h := newTestHandler(t)
	r := gin.New()
	r.GET("/pipelines/:id/export", h.ExportPipeline)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/pipelines/missing/export", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestImportPipeline_CreatesNewPipeline(t *testing.T) {
	h := newTestHandler(t)
	r := gin.New()
	r.POST("/pipelines/import", h.ImportPipeline)

	doc := strings.Join([]string{
		"apiVersion: cooker.dev/v1",
		"kind: Pipeline",
		"metadata:",
		"  name: imported-pipe",
		"  description: from yaml",
		"spec:",
		"  stages:",
		"    - id: build",
		"      name: Build",
		"      type: build",
		"      position: {x: 0, y: 0}",
		"    - id: deploy",
		"      name: Deploy",
		"      type: deploy",
		"      position: {x: 200, y: 0}",
		"  edges:",
		"    - id: e1",
		"      source: build",
		"      target: deploy",
		"      condition: success",
		"",
	}, "\n")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, yamlRequest("/pipelines/import", doc))
	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d body=%s", w.Code, w.Body.String())
	}
	var created model.Pipeline
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if created.ID == "" {
		t.Error("import did not mint a fresh ID")
	}
	if created.Name != "imported-pipe" || len(created.Stages) != 2 {
		t.Errorf("imported pipeline wrong: %+v", created)
	}
	if created.CreatedAt.IsZero() {
		t.Error("import did not stamp CreatedAt")
	}

	// It is actually persisted and fetchable.
	got, err := h.Store.Pipelines.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("fetch imported: %v", err)
	}
	if got.Name != "imported-pipe" {
		t.Errorf("persisted name wrong: %q", got.Name)
	}
}

func TestImportPipeline_InvalidYAML_400(t *testing.T) {
	h := newTestHandler(t)
	r := gin.New()
	r.POST("/pipelines/import", h.ImportPipeline)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, yamlRequest("/pipelines/import", "this: is: not: valid: yaml: : :\n\t- broken"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestImportPipeline_UnknownAPIVersion_400(t *testing.T) {
	h := newTestHandler(t)
	r := gin.New()
	r.POST("/pipelines/import", h.ImportPipeline)

	doc := "apiVersion: cooker.dev/v2\nkind: Pipeline\nmetadata:\n  name: x\nspec:\n  stages: []\n  edges: []\n"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, yamlRequest("/pipelines/import", doc))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestImportPipeline_FailingDAG_UsesValidationEnvelope asserts that an
// import which parses but has an invalid graph fails with the SAME
// {"valid":false,"errors":[...]} envelope a JSON create returns — the
// import shares one validation path with the editor.
func TestImportPipeline_FailingDAG_UsesValidationEnvelope(t *testing.T) {
	h := newTestHandler(t)
	r := gin.New()
	r.POST("/pipelines/import", h.ImportPipeline)

	// Edge references an unknown target → ValidatePipelineDAG rejects it.
	doc := strings.Join([]string{
		"apiVersion: cooker.dev/v1",
		"kind: Pipeline",
		"metadata:",
		"  name: broken-graph",
		"spec:",
		"  stages:",
		"    - id: build",
		"      name: Build",
		"      type: build",
		"  edges:",
		"    - id: e1",
		"      source: build",
		"      target: ghost",
		"",
	}, "\n")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, yamlRequest("/pipelines/import", doc))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Valid  bool     `json:"valid"`
		Errors []string `json:"errors"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if resp.Valid {
		t.Error("expected valid=false")
	}
	found := false
	for _, e := range resp.Errors {
		if strings.Contains(e, "unknown target") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected DAG validation error envelope, got %v", resp.Errors)
	}
}

// TestImportPipeline_InvalidStageType_400 confirms the per-field
// validation path (validatePipelineInput) also runs on import.
func TestImportPipeline_InvalidStageType_400(t *testing.T) {
	h := newTestHandler(t)
	r := gin.New()
	r.POST("/pipelines/import", h.ImportPipeline)

	doc := strings.Join([]string{
		"apiVersion: cooker.dev/v1",
		"kind: Pipeline",
		"metadata:",
		"  name: bad-type",
		"spec:",
		"  stages:",
		"    - id: s1",
		"      name: Mystery",
		"      type: teleport",
		"  edges: []",
		"",
	}, "\n")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, yamlRequest("/pipelines/import", doc))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// --- RBAC ---------------------------------------------------------------
//
// The routes carry writeRole (import) and no role gate (export) in
// router.go. These tests exercise the RequireRole middleware directly to
// pin that a viewer cannot import while an operator can, matching the
// create-pipeline gate. Export is readable by any authenticated user, so
// it is not role-gated (consistent with GET /pipelines/:id).

func TestImportPipeline_ViewerForbidden(t *testing.T) {
	h := newTestHandler(t)
	viewer := &auth.Claims{Email: "v@example.com", Roles: []string{string(auth.RoleViewer)}}
	writeRole := auth.RequireRole(auth.RoleOperator, auth.RoleAdmin)

	r := gin.New()
	r.POST("/pipelines/import", withUser(func(c *gin.Context) {
		writeRole(c)
		if c.IsAborted() {
			return
		}
		h.ImportPipeline(c)
	}, viewer))

	doc := "apiVersion: cooker.dev/v1\nkind: Pipeline\nmetadata:\n  name: x\nspec:\n  stages: []\n  edges: []\n"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, yamlRequest("/pipelines/import", doc))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestImportPipeline_OperatorAllowed(t *testing.T) {
	h := newTestHandler(t)
	operator := &auth.Claims{Subject: "op", Email: "op@example.com", Roles: []string{string(auth.RoleOperator)}}
	writeRole := auth.RequireRole(auth.RoleOperator, auth.RoleAdmin)

	r := gin.New()
	r.POST("/pipelines/import", withUser(func(c *gin.Context) {
		writeRole(c)
		if c.IsAborted() {
			return
		}
		h.ImportPipeline(c)
	}, operator))

	doc := "apiVersion: cooker.dev/v1\nkind: Pipeline\nmetadata:\n  name: op-import\nspec:\n  stages: []\n  edges: []\n"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, yamlRequest("/pipelines/import", doc))
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for operator, got %d body=%s", w.Code, w.Body.String())
	}
}
