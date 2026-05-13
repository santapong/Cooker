// Package service implements the business logic for Cooker.
package service

import (
	"fmt"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/pkg/dagrunner"
)

// ValidatePipelineDAG validates that a pipeline's stages and edges form a
// valid DAG. It checks for duplicate stage IDs and dangling edge references
// before delegating cycle detection to dagrunner.DAG.Validate(). These
// checks were previously duplicated in handler/pipeline.go (validateDAG);
// they now live here so service-level tests cover them — see handler-layering
// audit Finding 1.
func ValidatePipelineDAG(p *model.Pipeline) []string {
	var errs []string

	// Duplicate stage-ID detection. dagrunner.DAG.AddNode silently
	// overwrites on collision, so we must catch this before building the DAG.
	stageIDs := make(map[string]bool, len(p.Stages))
	for _, s := range p.Stages {
		if stageIDs[s.ID] {
			errs = append(errs, "duplicate stage ID: "+s.ID)
		}
		stageIDs[s.ID] = true
	}

	// Build adjacency from edges and check for dangling references.
	deps := make(map[string][]string)
	for _, e := range p.Edges {
		if !stageIDs[e.Source] {
			errs = append(errs, "edge references unknown source: "+e.Source)
		}
		if !stageIDs[e.Target] {
			errs = append(errs, "edge references unknown target: "+e.Target)
		}
		deps[e.Target] = append(deps[e.Target], e.Source)
	}

	// If we already found structural problems, skip the cycle check —
	// an unknown dependency would cause dagrunner to error on that node
	// rather than giving a clean "DAG contains a cycle" message.
	if len(errs) > 0 {
		return errs
	}

	dag := dagrunner.NewDAG()
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
