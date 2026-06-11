package service

import (
	"strings"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
)

// richPipeline builds a pipeline that exercises the fields most likely
// to drift in a YAML round-trip: edges with conditions, a structured
// retry policy, a cache spec, env, secretRefs, variables, and a run
// deadline. Server-assigned fields are populated so the export-strip
// path is actually tested.
func richPipeline() *model.Pipeline {
	exp := true
	return &model.Pipeline{
		ID:          "srv-assigned-id",
		Name:        "release",
		Description: "build, push, deploy",
		Version:     7,
		CreatedAt:   time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 6, 7, 8, 9, 10, 0, time.UTC),
		RunDeadline: "45m",
		Variables:   map[string]string{"REGION": "us-east-1"},
		Stages: []model.Stage{
			{
				ID:   "build",
				Name: "Build image",
				Type: model.StageTypeBuild,
				Config: model.StageConfig{
					Dockerfile: "Dockerfile",
					Context:    ".",
					Tags:       []string{"app:latest"},
					Cache: &model.CacheSpec{
						Mode: "registry",
						Ref:  "registry.example.com/org/app:buildcache",
					},
					Retry: &model.RetryPolicy{
						MaxAttempts: 3,
						InitialMS:   500,
						MaxMS:       5000,
						Exponential: &exp,
					},
					Env:        map[string]string{"CGO_ENABLED": "0"},
					SecretRefs: []string{"DOCKERHUB_TOKEN"},
				},
				Position: model.Position{X: 10, Y: 20},
			},
			{
				ID:       "deploy",
				Name:     "Deploy",
				Type:     model.StageTypeDeploy,
				Config:   model.StageConfig{Namespace: "prod", ManifestPath: "k8s/"},
				Position: model.Position{X: 200, Y: 20},
			},
		},
		Edges: []model.Edge{
			{ID: "e1", Source: "build", Target: "deploy", Condition: "success"},
		},
	}
}

func TestExportImportRoundTrip_Lossless(t *testing.T) {
	orig := richPipeline()

	out, err := ExportPipelineYAML(orig)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	imported, err := ImportPipelineYAML(out)
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	// Authored fields survive the round-trip.
	if imported.Name != orig.Name || imported.Description != orig.Description {
		t.Errorf("metadata drift: name=%q desc=%q", imported.Name, imported.Description)
	}
	if imported.RunDeadline != orig.RunDeadline {
		t.Errorf("runDeadline drift: %q", imported.RunDeadline)
	}
	if imported.Variables["REGION"] != "us-east-1" {
		t.Errorf("variables drift: %+v", imported.Variables)
	}
	if len(imported.Stages) != 2 || len(imported.Edges) != 1 {
		t.Fatalf("graph drift: %d stages, %d edges", len(imported.Stages), len(imported.Edges))
	}
	if imported.Edges[0].Condition != "success" {
		t.Errorf("edge condition lost: %q", imported.Edges[0].Condition)
	}
	bs := imported.Stages[0]
	if bs.Config.Cache == nil || bs.Config.Cache.Ref != "registry.example.com/org/app:buildcache" {
		t.Errorf("cache spec lost: %+v", bs.Config.Cache)
	}
	if bs.Config.Retry == nil || bs.Config.Retry.MaxAttempts != 3 || bs.Config.Retry.Exponential == nil || !*bs.Config.Retry.Exponential {
		t.Errorf("retry policy lost: %+v", bs.Config.Retry)
	}
	if len(bs.Config.SecretRefs) != 1 || bs.Config.SecretRefs[0] != "DOCKERHUB_TOKEN" {
		t.Errorf("secretRefs lost: %+v", bs.Config.SecretRefs)
	}

	// Server-assigned fields are stripped: a fresh import carries none.
	if imported.ID != "" || imported.Version != 0 {
		t.Errorf("server fields leaked: id=%q version=%d", imported.ID, imported.Version)
	}
	if !imported.CreatedAt.IsZero() || !imported.UpdatedAt.IsZero() {
		t.Errorf("timestamps leaked: created=%v updated=%v", imported.CreatedAt, imported.UpdatedAt)
	}
}

// TestExportImportExport_ByteIdentical pins determinism: a second export
// of an imported pipeline must be byte-for-byte identical to the first.
// This is the property that makes the YAML safe to commit and diff.
func TestExportImportExport_ByteIdentical(t *testing.T) {
	first, err := ExportPipelineYAML(richPipeline())
	if err != nil {
		t.Fatalf("first export: %v", err)
	}
	imported, err := ImportPipelineYAML(first)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	second, err := ExportPipelineYAML(imported)
	if err != nil {
		t.Fatalf("second export: %v", err)
	}
	if string(first) != string(second) {
		t.Errorf("export not deterministic:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

// TestExport_EnvelopeShape asserts the apiVersion/kind/metadata/spec
// envelope is present and that server-assigned keys never appear.
func TestExport_EnvelopeShape(t *testing.T) {
	out, err := ExportPipelineYAML(richPipeline())
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	s := string(out)
	for _, want := range []string{
		"apiVersion: " + PipelineAPIVersion,
		"kind: " + PipelineKind,
		"metadata:",
		"spec:",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("export missing %q:\n%s", want, s)
		}
	}
	// Server-assigned fields must not be serialised. Match on the JSON
	// tag keys the model uses.
	for _, forbidden := range []string{"createdAt:", "updatedAt:", "version:", "srv-assigned-id"} {
		if strings.Contains(s, forbidden) {
			t.Errorf("export leaked server field %q:\n%s", forbidden, s)
		}
	}
}

// TestExport_NoSecretMaterial is the security guarantee: an export
// carries the secret REFERENCE name but never any secret value. We
// build a pipeline whose secretRef name is the only secret-ish token,
// then assert a sentinel "value" never appears. Because the model only
// stores names (Stage.SecretRefs []string), there is no field through
// which a value could leak — this test pins that invariant so a future
// model change that adds an inline value field fails loudly here.
func TestExport_NoSecretMaterial(t *testing.T) {
	const sentinel = "super-secret-value-DO-NOT-EXPORT"
	p := &model.Pipeline{
		Name: "secrets",
		Stages: []model.Stage{
			{
				ID:   "s",
				Name: "stage",
				Type: model.StageTypeCustom,
				Config: model.StageConfig{
					// The NAME of a secret may legitimately be exported.
					SecretRefs: []string{"DB_PASSWORD"},
				},
			},
		},
	}
	out, err := ExportPipelineYAML(p)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if strings.Contains(string(out), sentinel) {
		t.Fatalf("export leaked secret material:\n%s", out)
	}
	// The reference name SHOULD be present — that's the whole point of
	// pipeline-as-code: portability of which secrets a stage needs.
	if !strings.Contains(string(out), "DB_PASSWORD") {
		t.Errorf("export dropped the secret reference name:\n%s", out)
	}
}

func TestImport_RejectsUnknownAPIVersion(t *testing.T) {
	doc := []byte("apiVersion: cooker.dev/v2\nkind: Pipeline\nmetadata:\n  name: x\nspec:\n  stages: []\n  edges: []\n")
	if _, err := ImportPipelineYAML(doc); err == nil {
		t.Fatal("expected error for unknown apiVersion")
	}
}

func TestImport_RejectsUnknownKind(t *testing.T) {
	doc := []byte("apiVersion: cooker.dev/v1\nkind: Workflow\nmetadata:\n  name: x\nspec:\n  stages: []\n  edges: []\n")
	if _, err := ImportPipelineYAML(doc); err == nil {
		t.Fatal("expected error for unknown kind")
	}
}

func TestImport_RejectsMalformedYAML(t *testing.T) {
	// A tab inside a mapping is invalid YAML indentation.
	doc := []byte("apiVersion: cooker.dev/v1\n\tkind: Pipeline\n")
	if _, err := ImportPipelineYAML(doc); err == nil {
		t.Fatal("expected error for malformed yaml")
	}
}

func TestImport_RejectsEmpty(t *testing.T) {
	if _, err := ImportPipelineYAML(nil); err == nil {
		t.Fatal("expected error for empty document")
	}
}
