package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/templates"
	"github.com/santapong/cooker/internal/validate"
)

// Templates is the optional handle on the pipeline-template catalog.
// nil falls back to a 503 on the /templates endpoints so dev-mode
// boots without a configured catalog still serve the rest of the API.
// Wired in server.New when DATABASE_URL is non-empty.
var templatesStoreField = "Templates"

func init() { _ = templatesStoreField }

// ListTemplates returns the enabled catalog. Available to anyone
// authenticated; the gallery is metadata only.
func (h *Handler) ListTemplates(c *gin.Context) {
	if h.Templates == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "templates store not configured"})
		return
	}
	list, err := h.Templates.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if list == nil {
		list = []templates.Template{}
	}
	c.JSON(http.StatusOK, list)
}

// GetTemplate returns a single template including its schema.
func (h *Handler) GetTemplate(c *gin.Context) {
	if h.Templates == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "templates store not configured"})
		return
	}
	tpl, err := h.Templates.Get(c.Request.Context(), c.Param("id"))
	if errors.Is(err, templates.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "template not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, tpl)
}

// createFromTemplateRequest is the JSON body of POST
// /pipelines/from-template/:id. Name overrides the template's; the
// rest of the pipeline (stages, edges, variables) is deep-copied
// from the template's schema with fresh IDs assigned.
type createFromTemplateRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description,omitempty"`
}

// CreatePipelineFromTemplate materialises a new Pipeline row from a
// template. The template's stored Schema is deserialised as a Pipeline,
// fresh IDs are assigned to the new pipeline + each stage + each edge,
// and the result is validated through the existing
// service.ValidatePipelineDAG seam before persistence.
func (h *Handler) CreatePipelineFromTemplate(c *gin.Context) {
	if h.Templates == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "templates store not configured"})
		return
	}
	tpl, err := h.Templates.Get(c.Request.Context(), c.Param("id"))
	if errors.Is(err, templates.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "template not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var req createFromTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validate.Name("name", req.Name); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var schema model.Pipeline
	if err := json.Unmarshal(tpl.Schema, &schema); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "template schema is not a valid Pipeline: " + err.Error()})
		return
	}

	// Deep-copy with fresh IDs. Stage IDs are re-mapped via a lookup
	// so edges still point at the right (renamed) stages.
	newPipeline := model.Pipeline{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Description: req.Description,
		Variables:   make(map[string]string, len(schema.Variables)),
		Stages:      make([]model.Stage, 0, len(schema.Stages)),
		Edges:       make([]model.Edge, 0, len(schema.Edges)),
	}
	if newPipeline.Description == "" {
		newPipeline.Description = schema.Description
	}
	for k, v := range schema.Variables {
		newPipeline.Variables[k] = v
	}
	stageIDMap := make(map[string]string, len(schema.Stages))
	for _, s := range schema.Stages {
		oldID := s.ID
		s.ID = uuid.New().String()
		stageIDMap[oldID] = s.ID
		newPipeline.Stages = append(newPipeline.Stages, s)
	}
	for _, e := range schema.Edges {
		e.Source = stageIDMap[e.Source]
		e.Target = stageIDMap[e.Target]
		newPipeline.Edges = append(newPipeline.Edges, e)
	}

	// Re-validate the materialised pipeline. Catches broken
	// templates that would have failed Pipeline create anyway.
	if errs := service.ValidatePipelineDAG(&newPipeline); len(errs) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"valid": false, "errors": errs})
		return
	}

	now := time.Now()
	newPipeline.CreatedAt = now
	newPipeline.UpdatedAt = now

	if err := h.Store.Pipelines.Create(c.Request.Context(), &newPipeline); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"templateId": tpl.ID,
		"pipeline":   newPipeline,
	})
}

// adminTemplateRequest is the JSON body of POST /admin/templates and
// PUT /admin/templates/:id. Schema is the Pipeline-shaped JSONB blob
// the gallery materialises via CreatePipelineFromTemplate; we do not
// re-validate it here because operators may stage a template before
// the referenced builders/clusters exist. ValidatePipelineDAG runs at
// create-from-template time, which is the right boundary.
type adminTemplateRequest struct {
	Name        string          `json:"name" binding:"required"`
	Description string          `json:"description,omitempty"`
	Category    string          `json:"category,omitempty"`
	Schema      json.RawMessage `json:"schema" binding:"required"`
	IconURL     string          `json:"iconUrl,omitempty"`
	Enabled     *bool           `json:"enabled,omitempty"`
}

// CreateTemplate is the admin-only Create endpoint. Operators who
// want to seed via SQL can keep doing so; this endpoint exists so
// the (future) settings UI doesn't need DB access.
func (h *Handler) CreateTemplate(c *gin.Context) {
	if h.Templates == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "templates store not configured"})
		return
	}
	var req adminTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validate.Name("name", req.Name); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	tpl := templates.Template{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Description: req.Description,
		Category:    req.Category,
		Schema:      req.Schema,
		IconURL:     req.IconURL,
		Enabled:     enabled,
	}
	if err := h.Templates.Create(c.Request.Context(), tpl); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, tpl)
}

// UpdateTemplate replaces a template by ID. 404 if the template
// doesn't exist (the store reports ErrNotFound from the UPDATE
// rows-affected check).
func (h *Handler) UpdateTemplate(c *gin.Context) {
	if h.Templates == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "templates store not configured"})
		return
	}
	id := c.Param("id")
	existing, err := h.Templates.Get(c.Request.Context(), id)
	if errors.Is(err, templates.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "template not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var req adminTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validate.Name("name", req.Name); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	existing.Name = req.Name
	existing.Description = req.Description
	existing.Category = req.Category
	existing.Schema = req.Schema
	existing.IconURL = req.IconURL
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	if err := h.Templates.Update(c.Request.Context(), existing); err != nil {
		if errors.Is(err, templates.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "template not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, existing)
}

// DeleteTemplate removes a template by ID. 404 on unknown.
func (h *Handler) DeleteTemplate(c *gin.Context) {
	if h.Templates == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "templates store not configured"})
		return
	}
	err := h.Templates.Delete(c.Request.Context(), c.Param("id"))
	if errors.Is(err, templates.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "template not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}
