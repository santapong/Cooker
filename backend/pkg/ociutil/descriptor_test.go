package ociutil

import (
	"encoding/json"
	"testing"

	"github.com/santapong/cooker/internal/oci"
)

func TestParseManifest_Valid(t *testing.T) {
	m := oci.NewManifest(
		oci.NewDescriptor(oci.MediaTypeImageConfig, "sha256:abc123", 1024),
		[]oci.Descriptor{
			oci.NewDescriptor(oci.MediaTypeImageLayerGzip, "sha256:layer1", 2048),
		},
	)
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("failed to marshal manifest: %v", err)
	}

	parsed, err := ParseManifest(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if parsed.Config.Digest != "sha256:abc123" {
		t.Errorf("expected config digest sha256:abc123, got %s", parsed.Config.Digest)
	}
	if len(parsed.Layers) != 1 {
		t.Errorf("expected 1 layer, got %d", len(parsed.Layers))
	}
}

func TestParseManifest_InvalidJSON(t *testing.T) {
	_, err := ParseManifest([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseManifest_InvalidManifest(t *testing.T) {
	// Valid JSON but invalid manifest (schemaVersion 1, no layers)
	data := `{"schemaVersion":1,"mediaType":"invalid","config":{},"layers":[]}`
	_, err := ParseManifest([]byte(data))
	if err == nil {
		t.Fatal("expected error for invalid manifest")
	}
}

func TestParseIndex_Valid(t *testing.T) {
	idx := oci.NewIndex([]oci.Descriptor{
		{
			MediaType: oci.MediaTypeImageManifest,
			Digest:    "sha256:abc",
			Size:      1024,
			Platform:  &oci.Platform{Architecture: "amd64", OS: "linux"},
		},
	})
	data, err := json.Marshal(idx)
	if err != nil {
		t.Fatalf("failed to marshal index: %v", err)
	}

	parsed, err := ParseIndex(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(parsed.Manifests) != 1 {
		t.Errorf("expected 1 manifest, got %d", len(parsed.Manifests))
	}
}

func TestParseIndex_InvalidJSON(t *testing.T) {
	_, err := ParseIndex([]byte("{invalid"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseIndex_InvalidIndex(t *testing.T) {
	data := `{"schemaVersion":1,"mediaType":"invalid","manifests":[]}`
	_, err := ParseIndex([]byte(data))
	if err == nil {
		t.Fatal("expected error for invalid index")
	}
}

func TestTotalLayerSize(t *testing.T) {
	m := &oci.Manifest{
		Layers: []oci.Descriptor{
			{Size: 100},
			{Size: 200},
			{Size: 300},
		},
	}

	total := TotalLayerSize(m)
	if total != 600 {
		t.Errorf("expected total 600, got %d", total)
	}
}

func TestTotalLayerSize_Empty(t *testing.T) {
	m := &oci.Manifest{Layers: []oci.Descriptor{}}
	total := TotalLayerSize(m)
	if total != 0 {
		t.Errorf("expected total 0, got %d", total)
	}
}
