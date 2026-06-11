package service

import (
	"errors"
	"fmt"

	"sigs.k8s.io/yaml"

	"github.com/santapong/cooker/internal/model"
)

// Pipeline-as-code envelope constants (product-plan Tier 2). The
// apiVersion/kind pair gives the YAML document a stable, forward-
// compatible shape so a future v2 can be rejected (or migrated)
// instead of silently mis-parsed.
const (
	// PipelineAPIVersion is the only apiVersion ImportPipelineYAML
	// accepts today. Bump (and branch) when the spec shape changes.
	PipelineAPIVersion = "cooker.dev/v1"
	// PipelineKind is the only kind ImportPipelineYAML accepts.
	PipelineKind = "Pipeline"
)

// ErrPipelineYAMLEmpty is returned by ImportPipelineYAML for an empty
// document. Callers map it to 400.
var ErrPipelineYAMLEmpty = errors.New("service: pipeline yaml is empty")

// pipelineMetadata carries the human-facing identity of an exported
// pipeline. Name/description are the only Pipeline fields that are
// authored rather than server-assigned, so they live in metadata
// (mirroring the Kubernetes object convention) while the graph lives
// in spec.
type pipelineMetadata struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// pipelineSpec is the serialisable body of an exported pipeline: the
// graph and its run-time policy, with every server-assigned field
// (id, timestamps, version) omitted. The fields reuse model.Pipeline's
// JSON tags via sigs.k8s.io/yaml, so the YAML shape automatically
// mirrors the API JSON — no second set of struct tags to drift.
type pipelineSpec struct {
	Stages      []model.Stage     `json:"stages"`
	Edges       []model.Edge      `json:"edges"`
	Variables   map[string]string `json:"variables,omitempty"`
	RunDeadline string            `json:"runDeadline,omitempty"`
}

// pipelineDocument is the full on-disk envelope: apiVersion + kind +
// metadata + spec. It is what ExportPipelineYAML marshals and
// ImportPipelineYAML unmarshals.
type pipelineDocument struct {
	APIVersion string           `json:"apiVersion"`
	Kind       string           `json:"kind"`
	Metadata   pipelineMetadata `json:"metadata"`
	Spec       pipelineSpec     `json:"spec"`
}

// ExportPipelineYAML renders a pipeline as a pipeline-as-code YAML
// document. Server-assigned fields (ID, CreatedAt, UpdatedAt, Version)
// are deliberately dropped: a re-import regenerates them, and keeping
// them would make the export environment-specific and the
// export→import→export round-trip non-deterministic.
//
// SECURITY: the document carries Stage.SecretRefs (the NAMES of
// Environment.Secrets entries) but never any secret value — the model
// itself only stores references, and resolution happens at run time.
// pipelineyaml_test.go asserts no secret material can appear here.
func ExportPipelineYAML(p *model.Pipeline) ([]byte, error) {
	doc := pipelineDocument{
		APIVersion: PipelineAPIVersion,
		Kind:       PipelineKind,
		Metadata: pipelineMetadata{
			Name:        p.Name,
			Description: p.Description,
		},
		Spec: pipelineSpec{
			Stages:      p.Stages,
			Edges:       p.Edges,
			Variables:   p.Variables,
			RunDeadline: p.RunDeadline,
		},
	}
	// Normalise nil slices to empty so the spec always renders the
	// `stages:`/`edges:` keys — a hand-edited file is then a closer
	// match to an exported one, and the round-trip stays byte-stable.
	if doc.Spec.Stages == nil {
		doc.Spec.Stages = []model.Stage{}
	}
	if doc.Spec.Edges == nil {
		doc.Spec.Edges = []model.Edge{}
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("service: marshal pipeline yaml: %w", err)
	}
	return out, nil
}

// ImportPipelineYAML parses a pipeline-as-code YAML document into a
// fresh *model.Pipeline carrying only authored fields (Name,
// Description, Stages, Edges, Variables, RunDeadline). It rejects an
// empty document, an unknown apiVersion/kind, and malformed YAML.
//
// It does NOT run DAG validation or assign an ID/timestamps — the
// handler routes the result through the same validate + create path as
// a JSON-authored pipeline so the import shares one source of truth for
// both validation errors and server-assigned fields.
func ImportPipelineYAML(data []byte) (*model.Pipeline, error) {
	if len(data) == 0 {
		return nil, ErrPipelineYAMLEmpty
	}

	var doc pipelineDocument
	// sigs.k8s.io/yaml converts YAML→JSON then json.Unmarshal, so the
	// same struct tags that drive the API decode this document. A
	// syntactically invalid document fails here with a clear message.
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("service: parse pipeline yaml: %w", err)
	}

	if doc.APIVersion != PipelineAPIVersion {
		return nil, fmt.Errorf(
			"service: unsupported apiVersion %q (want %q)",
			doc.APIVersion, PipelineAPIVersion)
	}
	if doc.Kind != PipelineKind {
		return nil, fmt.Errorf(
			"service: unsupported kind %q (want %q)",
			doc.Kind, PipelineKind)
	}

	p := &model.Pipeline{
		Name:        doc.Metadata.Name,
		Description: doc.Metadata.Description,
		Stages:      doc.Spec.Stages,
		Edges:       doc.Spec.Edges,
		Variables:   doc.Spec.Variables,
		RunDeadline: doc.Spec.RunDeadline,
	}
	return p, nil
}
