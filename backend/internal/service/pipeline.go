// Package service implements the business logic for Cooker.
package service

import (
	"fmt"

	"github.com/santapong/cooker/internal/buildplan"
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

	// Build adjacency from edges and check for dangling references and
	// unsupported conditions.
	deps := make(map[string][]string)
	for _, e := range p.Edges {
		if !stageIDs[e.Source] {
			errs = append(errs, "edge references unknown source: "+e.Source)
		}
		if !stageIDs[e.Target] {
			errs = append(errs, "edge references unknown target: "+e.Target)
		}
		// Primitive #2 (replaces the T4 forward-compat refusal): the
		// executor now evaluates success/failure/always per edge; only
		// genuinely unknown values are rejected, because they would
		// fail closed at run time and confuse the pipeline author.
		switch e.Condition {
		case "", "success", "failure", "always":
		default:
			errs = append(errs, fmt.Sprintf(
				"service: edge %s->%s: condition %q is not one of success, failure, always",
				e.Source, e.Target, e.Condition,
			))
		}
		deps[e.Target] = append(deps[e.Target], e.Source)
	}

	// Inter-stage outputs reference validation (Primitive #3,
	// dag-adaptation-2026.md §7.3 / DR-3). Each stage's data-bearing string
	// fields may contain ${stages.<id>.<key>} tokens. A referenced <id>
	// must be (a) a declared stage and (b) an ancestor of the referencing
	// stage in the dependency graph — otherwise its outputs cannot exist
	// when this stage runs. Output-KEY existence is runtime-only and is NOT
	// validated here (keys only materialise during execution).
	for i := range p.Stages {
		s := &p.Stages[i]
		var refs []string
		seen := make(map[string]bool)
		for _, field := range stageInterpolatableStrings(s) {
			for _, id := range buildplan.References(field) {
				if !seen[id] {
					seen[id] = true
					refs = append(refs, id)
				}
			}
		}
		if len(refs) == 0 {
			continue
		}
		ancestors := ancestorsOf(s.ID, deps)
		for _, id := range refs {
			switch {
			case !stageIDs[id]:
				errs = append(errs, fmt.Sprintf(
					"service: stage %q references unknown stage %q in ${stages.%s.*}",
					s.ID, id, id))
			case id == s.ID:
				errs = append(errs, fmt.Sprintf(
					"service: stage %q references its own outputs ${stages.%s.*}",
					s.ID, id))
			case !ancestors[id]:
				errs = append(errs, fmt.Sprintf(
					"service: stage %q references ${stages.%s.*} but %q is not an ancestor (no path of edges from %q to %q)",
					s.ID, id, id, id, s.ID))
			}
		}
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

// stageInterpolatableStrings returns the data-bearing string fields of a
// stage that the executor runs ${stages.<id>.<key>} interpolation over.
// It mirrors interpolateStageConfig's field set (executor.go) so validation
// scans exactly what runtime substitutes — Script is deliberately excluded
// (it is exec'd, never interpolated), and HelmValues is out of scope.
func stageInterpolatableStrings(s *model.Stage) []string {
	c := s.Config
	fields := []string{
		c.Registry, c.Repository, c.Image, c.Context, c.Dockerfile,
		c.ManifestPath, c.HelmChart, c.Namespace,
		c.GitOpsRepo, c.GitOpsBranch, c.GitOpsPath, c.GitOpsMessage, c.GitOpsContent,
	}
	fields = append(fields, c.Tags...)
	fields = append(fields, c.Command...)
	fields = append(fields, c.Platforms...)
	for _, v := range c.BuildArgs {
		fields = append(fields, v)
	}
	for _, v := range c.Env {
		fields = append(fields, v)
	}
	return fields
}

// ancestorsOf returns the set of stage IDs reachable backwards from target
// via the dependency edges (deps maps target -> direct sources). It is a
// BFS over the reversed graph; target itself is not included. Cycles are
// tolerated (the visited set bounds the walk) so this is safe to call
// before the dagrunner cycle check has run.
func ancestorsOf(target string, deps map[string][]string) map[string]bool {
	ancestors := make(map[string]bool)
	queue := append([]string(nil), deps[target]...)
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if ancestors[cur] {
			continue
		}
		ancestors[cur] = true
		queue = append(queue, deps[cur]...)
	}
	return ancestors
}
