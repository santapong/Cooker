package handler

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/validate"
)

// validatePipelineInput rejects malformed pipeline payloads. Called
// from CreatePipeline and UpdatePipeline before any store write.
func validatePipelineInput(p *model.Pipeline) error {
	if err := validate.Name("name", p.Name); err != nil {
		return err
	}
	if err := validate.Description("description", p.Description); err != nil {
		return err
	}
	if err := validate.RunDeadline(p.RunDeadline); err != nil {
		return err
	}
	for i, s := range p.Stages {
		if err := validate.StageType(s.Type); err != nil {
			return err
		}
		if err := validate.RetryPolicy(s.Config.Retry); err != nil {
			return fmt.Errorf("stage %d: %w", i, err)
		}
		if err := validate.CacheSpec(s.Config.Cache); err != nil {
			return fmt.Errorf("stage %d: %w", i, err)
		}
	}
	return nil
}

// ListPipelines returns all pipelines visible to the caller.
//
// @Summary      List pipelines
// @Tags         pipelines
// @Produce      json
// @Success      200  {array}   model.Pipeline
// @Failure      401  {object}  map[string]string
// @Security     BearerAuth
// @Router       /pipelines [get]
func (h *Handler) ListPipelines(c *gin.Context) {
	pipelines, err := h.Store.Pipelines.List(c.Request.Context())
	if abortStoreErr(c, err, "pipelines not found") {
		return
	}
	if pipelines == nil {
		pipelines = []*model.Pipeline{}
	}
	c.JSON(http.StatusOK, pipelines)
}

func (h *Handler) CreatePipeline(c *gin.Context) {
	var p model.Pipeline
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.createPipeline(c, &p)
}

// createPipeline runs the shared create path: per-field validation,
// DAG validation, server-field assignment (fresh ID + timestamps),
// nil-slice normalisation, and the store write. CreatePipeline (JSON
// body) and ImportPipeline (YAML body) both funnel through here so an
// imported pipeline is validated and persisted byte-for-byte the same
// way the editor's create is — one source of truth for the error
// envelope and the server-assigned fields. The caller is responsible
// for having populated p's authored fields; this overwrites any
// client-supplied ID/timestamps/version.
func (h *Handler) createPipeline(c *gin.Context, p *model.Pipeline) {
	if err := validatePipelineInput(p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if dagErrs := service.ValidatePipelineDAG(p); len(dagErrs) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"valid": false, "errors": dagErrs})
		return
	}

	p.ID = uuid.New().String()
	now := time.Now()
	p.CreatedAt = now
	p.UpdatedAt = now
	// Server owns the optimistic-concurrency token; a YAML/JSON body
	// must not be able to seed it.
	p.Version = 0

	p.SetDefaults()

	if err := h.Store.Pipelines.Create(c.Request.Context(), p); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, p)
}

func (h *Handler) GetPipeline(c *gin.Context) {
	p, err := h.Store.Pipelines.Get(c.Request.Context(), c.Param("id"))
	if abortStoreErr(c, err, "pipeline not found") {
		return
	}
	c.JSON(http.StatusOK, p)
}

// maxImportYAMLBytes caps the import body. A pipeline document is a
// handful of KB even with many stages; 1 MiB is generous headroom while
// stopping a single request from buffering an unbounded body into
// memory before the YAML parser sees it.
const maxImportYAMLBytes = 1 << 20

// pipelineFilenameRe strips a pipeline name down to a filesystem-safe
// slug for the suggested download filename. Anything outside the class
// collapses to a single hyphen.
var pipelineFilenameRe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// pipelineYAMLFilename derives "<slug>.cooker.yaml" from a pipeline
// name, falling back to "pipeline" when the slug is empty. The
// .cooker.yaml suffix signals the document is a Cooker pipeline-as-code
// file without claiming the bare .yaml association.
func pipelineYAMLFilename(name string) string {
	slug := strings.Trim(pipelineFilenameRe.ReplaceAllString(name, "-"), "-.")
	if slug == "" {
		slug = "pipeline"
	}
	const maxSlug = 80
	if len(slug) > maxSlug {
		slug = strings.Trim(slug[:maxSlug], "-.")
	}
	return slug + ".cooker.yaml"
}

// ExportPipeline renders a pipeline as a pipeline-as-code YAML document
// (product-plan Tier 2). Read-level RBAC: same gate as GET /pipelines/:id.
// Server-assigned fields (id, timestamps, version) are stripped by the
// service so the document is portable and version-controllable.
//
// @Summary      Export a pipeline as YAML
// @Tags         pipelines
// @Produce      application/yaml
// @Param        id   path      string  true  "Pipeline ID"
// @Success      200  {string}  string  "Pipeline-as-code YAML document"
// @Failure      404  {object}  map[string]string
// @Security     BearerAuth
// @Router       /pipelines/{id}/export [get]
func (h *Handler) ExportPipeline(c *gin.Context) {
	p, err := h.Store.Pipelines.Get(c.Request.Context(), c.Param("id"))
	if abortStoreErr(c, err, "pipeline not found") {
		return
	}
	out, err := service.ExportPipelineYAML(p)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", pipelineYAMLFilename(p.Name)))
	c.Data(http.StatusOK, "application/yaml; charset=utf-8", out)
}

// ImportPipeline creates a NEW pipeline from a pipeline-as-code YAML
// document (product-plan Tier 2). Same write gate as CreatePipeline.
//
// The body is the raw YAML document (Content-Type application/yaml or
// text/yaml). The envelope's apiVersion/kind are checked first; an
// unknown value, malformed YAML, or an empty body all 400. The parsed
// pipeline then runs the EXACT create path (validatePipelineInput +
// ValidatePipelineDAG + fresh ID/timestamps) so an invalid import fails
// with the same error envelope the editor sees, and a fresh ID is
// always minted — importing the same file twice yields two pipelines
// (no name-collision handling; create has none and the model has no
// unique-name constraint).
//
// @Summary      Import a pipeline from YAML
// @Tags         pipelines
// @Accept       application/yaml
// @Produce      json
// @Success      201  {object}  model.Pipeline
// @Failure      400  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Security     BearerAuth
// @Router       /pipelines/import [post]
func (h *Handler) ImportPipeline(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxImportYAMLBytes+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "could not read request body"})
		return
	}
	if len(body) > maxImportYAMLBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "pipeline document too large"})
		return
	}

	p, err := service.ImportPipelineYAML(body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.createPipeline(c, p)
}

func (h *Handler) UpdatePipeline(c *gin.Context) {
	id := c.Param("id")
	existing, err := h.Store.Pipelines.Get(c.Request.Context(), id)
	if abortStoreErr(c, err, "pipeline not found") {
		return
	}

	var p model.Pipeline
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validatePipelineInput(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	p.ID = id
	p.CreatedAt = existing.CreatedAt
	p.UpdatedAt = time.Now()
	// BC-H2: use the server-fetched version for the OCC WHERE clause so
	// the client cannot bypass the stale-check by supplying an arbitrary
	// version field. ErrConflict → 409 is handled by abortStoreErr.
	p.Version = existing.Version

	if err := h.Store.Pipelines.Update(c.Request.Context(), &p); err != nil {
		if abortStoreErr(c, err, "pipeline not found") {
			return
		}
	}
	c.JSON(http.StatusOK, p)
}

func (h *Handler) DeletePipeline(c *gin.Context) {
	if err := h.Store.Pipelines.Delete(c.Request.Context(), c.Param("id")); err != nil {
		if abortStoreErr(c, err, "pipeline not found") {
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

func (h *Handler) ValidatePipeline(c *gin.Context) {
	p, err := h.Store.Pipelines.Get(c.Request.Context(), c.Param("id"))
	if abortStoreErr(c, err, "pipeline not found") {
		return
	}
	dagErrs := service.ValidatePipelineDAG(p)
	if len(dagErrs) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"valid": false, "errors": dagErrs})
		return
	}
	c.JSON(http.StatusOK, gin.H{"valid": true, "errors": []string{}})
}
