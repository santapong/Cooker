package oci

import (
	"strings"
	"testing"
)

func TestComputeDigest(t *testing.T) {
	content := "hello world"
	digest, size, err := ComputeDigest(strings.NewReader(content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if size != int64(len(content)) {
		t.Errorf("expected size %d, got %d", len(content), size)
	}

	if !strings.HasPrefix(digest, "sha256:") {
		t.Errorf("expected sha256 prefix, got %s", digest)
	}

	// SHA-256 of "hello world"
	expected := "sha256:b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if digest != expected {
		t.Errorf("expected digest %s, got %s", expected, digest)
	}
}

func TestComputeDigest_Empty(t *testing.T) {
	digest, size, err := ComputeDigest(strings.NewReader(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if size != 0 {
		t.Errorf("expected size 0, got %d", size)
	}
	if !strings.HasPrefix(digest, "sha256:") {
		t.Errorf("expected sha256 prefix, got %s", digest)
	}
}

func TestNewDescriptor(t *testing.T) {
	d := NewDescriptor(MediaTypeImageLayerGzip, "sha256:abc123", 4096)
	if d.MediaType != MediaTypeImageLayerGzip {
		t.Errorf("expected mediaType %s, got %s", MediaTypeImageLayerGzip, d.MediaType)
	}
	if d.Digest != "sha256:abc123" {
		t.Errorf("expected digest sha256:abc123, got %s", d.Digest)
	}
	if d.Size != 4096 {
		t.Errorf("expected size 4096, got %d", d.Size)
	}
}

func TestNewManifest(t *testing.T) {
	config := NewDescriptor(MediaTypeImageConfig, "sha256:cfg", 512)
	layers := []Descriptor{
		NewDescriptor(MediaTypeImageLayerGzip, "sha256:l1", 1024),
		NewDescriptor(MediaTypeImageLayerGzip, "sha256:l2", 2048),
	}

	m := NewManifest(config, layers)

	if m.SchemaVersion != 2 {
		t.Errorf("expected schemaVersion 2, got %d", m.SchemaVersion)
	}
	if m.MediaType != MediaTypeImageManifest {
		t.Errorf("expected mediaType %s, got %s", MediaTypeImageManifest, m.MediaType)
	}
	if m.Config.Digest != "sha256:cfg" {
		t.Errorf("expected config digest sha256:cfg, got %s", m.Config.Digest)
	}
	if len(m.Layers) != 2 {
		t.Errorf("expected 2 layers, got %d", len(m.Layers))
	}
}

func TestNewIndex(t *testing.T) {
	manifests := []Descriptor{
		{
			MediaType: MediaTypeImageManifest,
			Digest:    "sha256:m1",
			Size:      1024,
			Platform:  &Platform{Architecture: "amd64", OS: "linux"},
		},
		{
			MediaType: MediaTypeImageManifest,
			Digest:    "sha256:m2",
			Size:      1024,
			Platform:  &Platform{Architecture: "arm64", OS: "linux"},
		},
	}

	idx := NewIndex(manifests)

	if idx.SchemaVersion != 2 {
		t.Errorf("expected schemaVersion 2, got %d", idx.SchemaVersion)
	}
	if idx.MediaType != MediaTypeImageIndex {
		t.Errorf("expected mediaType %s, got %s", MediaTypeImageIndex, idx.MediaType)
	}
	if len(idx.Manifests) != 2 {
		t.Errorf("expected 2 manifests, got %d", len(idx.Manifests))
	}
}
