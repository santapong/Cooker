package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/cooker-ci/cooker/internal/model"
)

// In-memory store for MVP. Will be replaced with PostgreSQL.
var pipelines = make(map[string]*model.Pipeline)

func ListPipelines(c *gin.Context) {
	result := make([]*model.Pipeline, 0, len(pipelines))
	for _, p := range pipelines {
		result = append(result, p)
	}
	c.JSON(http.StatusOK, result)
}

func CreatePipeline(c *gin.Context) {
	var p model.Pipeline
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	p.ID = uuid.New().String()
	now := time.Now()
	p.CreatedAt = now
	p.UpdatedAt = now

	if p.Variables == nil {
		p.Variables = make(map[string]string)
	}
	if p.Stages == nil {
		p.Stages = []model.Stage{}
	}
	if p.Edges == nil {
		p.Edges = []model.Edge{}
	}

	pipelines[p.ID] = &p
	c.JSON(http.StatusCreated, p)
}

func GetPipeline(c *gin.Context) {
	id := c.Param("id")
	p, ok := pipelines[id]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "pipeline not found"})
		return
	}
	c.JSON(http.StatusOK, p)
}

func UpdatePipeline(c *gin.Context) {
	id := c.Param("id")
	existing, ok := pipelines[id]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "pipeline not found"})
		return
	}

	var p model.Pipeline
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	p.ID = id
	p.CreatedAt = existing.CreatedAt
	p.UpdatedAt = time.Now()
	pipelines[id] = &p

	c.JSON(http.StatusOK, p)
}

func DeletePipeline(c *gin.Context) {
	id := c.Param("id")
	if _, ok := pipelines[id]; !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "pipeline not found"})
		return
	}
	delete(pipelines, id)
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

func ValidatePipeline(c *gin.Context) {
	id := c.Param("id")
	p, ok := pipelines[id]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "pipeline not found"})
		return
	}

	errors := validateDAG(p)
	if len(errors) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"valid": false, "errors": errors})
		return
	}
	c.JSON(http.StatusOK, gin.H{"valid": true, "errors": []string{}})
}

func RunPipeline(c *gin.Context) {
	id := c.Param("id")
	p, ok := pipelines[id]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "pipeline not found"})
		return
	}

	run := &model.PipelineRun{
		ID:         uuid.New().String(),
		PipelineID: p.ID,
		Status:     model.RunStatusPending,
		StageRuns:  make([]model.StageRun, 0),
		Variables:  p.Variables,
	}

	for _, stage := range p.Stages {
		run.StageRuns = append(run.StageRuns, model.StageRun{
			StageID: stage.ID,
			Status:  model.RunStatusPending,
		})
	}

	c.JSON(http.StatusAccepted, run)
}

func ListPipelineRuns(c *gin.Context) {
	c.JSON(http.StatusOK, []model.PipelineRun{})
}

func GetPipelineRun(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "pipeline run details placeholder"})
}

func CancelPipelineRun(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "cancelled"})
}

func GetStageLogs(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"logs": ""})
}

// validateDAG checks for cycles and unresolved references.
func validateDAG(p *model.Pipeline) []string {
	var errors []string

	stageIDs := make(map[string]bool)
	for _, s := range p.Stages {
		if stageIDs[s.ID] {
			errors = append(errors, "duplicate stage ID: "+s.ID)
		}
		stageIDs[s.ID] = true
	}

	for _, e := range p.Edges {
		if !stageIDs[e.Source] {
			errors = append(errors, "edge references unknown source: "+e.Source)
		}
		if !stageIDs[e.Target] {
			errors = append(errors, "edge references unknown target: "+e.Target)
		}
	}

	// Cycle detection using Kahn's algorithm
	inDegree := make(map[string]int)
	adj := make(map[string][]string)
	for _, s := range p.Stages {
		inDegree[s.ID] = 0
	}
	for _, e := range p.Edges {
		adj[e.Source] = append(adj[e.Source], e.Target)
		inDegree[e.Target]++
	}

	var queue []string
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	visited := 0
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		visited++
		for _, next := range adj[node] {
			inDegree[next]--
			if inDegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}

	if visited != len(p.Stages) {
		errors = append(errors, "pipeline contains a cycle")
	}

	return errors
}
