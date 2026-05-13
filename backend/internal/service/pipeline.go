// Package service implements the business logic for Cooker.
package service

import (
	"fmt"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/pkg/dagrunner"
)

// ValidatePipelineDAG validates that a pipeline's stages and edges form a valid DAG.
func ValidatePipelineDAG(p *model.Pipeline) []string {
	dag := dagrunner.NewDAG()

	// Build adjacency from edges
	deps := make(map[string][]string)
	for _, e := range p.Edges {
		deps[e.Target] = append(deps[e.Target], e.Source)
	}

	for _, s := range p.Stages {
		dag.AddNode(s.ID, deps[s.ID])
	}

	return dag.Validate()
}

// BuildDAGFromPipeline converts a pipeline into an executable DAG.
func BuildDAGFromPipeline(p *model.Pipeline) (*dagrunner.DAG, error) {
	errors := ValidatePipelineDAG(p)
	if len(errors) > 0 {
		return nil, fmt.Errorf("invalid pipeline: %v", errors)
	}

	dag := dagrunner.NewDAG()
	deps := make(map[string][]string)
	for _, e := range p.Edges {
		deps[e.Target] = append(deps[e.Target], e.Source)
	}

	for _, s := range p.Stages {
		dag.AddNode(s.ID, deps[s.ID])
	}

	return dag, nil
}
