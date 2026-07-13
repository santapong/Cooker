package oci

import "testing"

func TestValidateManifest_Valid(t *testing.T) {
	m := NewManifest(
		NewDescriptor(MediaTypeImageConfig, "sha256:abc123", 1024),
		[]Descriptor{
			NewDescriptor(MediaTypeImageLayerGzip, "sha256:layer1", 2048),
		},
	)

	errs := ValidateManifest(&m)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidateManifest_InvalidSchemaVersion(t *testing.T) {
	m := Manifest{
		SchemaVersion: 1,
		MediaType:     MediaTypeImageManifest,
		Config:        NewDescriptor(MediaTypeImageConfig, "sha256:abc", 100),
		Layers:        []Descriptor{NewDescriptor(MediaTypeImageLayerGzip, "sha256:l1", 200)},
	}

	errs := ValidateManifest(&m)
	if len(errs) == 0 {
		t.Fatal("expected errors for schemaVersion != 2")
	}
	found := false
	for _, e := range errs {
		if e == "schemaVersion must be 2" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'schemaVersion must be 2' error, got %v", errs)
	}
}

func TestValidateManifest_InvalidMediaType(t *testing.T) {
	m := Manifest{
		SchemaVersion: 2,
		MediaType:     "invalid/type",
		Config:        NewDescriptor(MediaTypeImageConfig, "sha256:abc", 100),
		Layers:        []Descriptor{NewDescriptor(MediaTypeImageLayerGzip, "sha256:l1", 200)},
	}

	errs := ValidateManifest(&m)
	if len(errs) == 0 {
		t.Fatal("expected errors for invalid mediaType")
	}
}

func TestValidateManifest_MissingConfigDigest(t *testing.T) {
	m := Manifest{
		SchemaVersion: 2,
		MediaType:     MediaTypeImageManifest,
		Config:        Descriptor{MediaType: MediaTypeImageConfig},
		Layers:        []Descriptor{NewDescriptor(MediaTypeImageLayerGzip, "sha256:l1", 200)},
	}

	errs := ValidateManifest(&m)
	found := false
	for _, e := range errs {
		if e == "config digest is required" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'config digest is required' error, got %v", errs)
	}
}

func TestValidateManifest_NoLayers(t *testing.T) {
	m := Manifest{
		SchemaVersion: 2,
		MediaType:     MediaTypeImageManifest,
		Config:        NewDescriptor(MediaTypeImageConfig, "sha256:abc", 100),
		Layers:        []Descriptor{},
	}

	errs := ValidateManifest(&m)
	found := false
	for _, e := range errs {
		if e == "at least one layer is required" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'at least one layer is required' error, got %v", errs)
	}
}

func TestValidateManifest_LayerMissingDigest(t *testing.T) {
	m := Manifest{
		SchemaVersion: 2,
		MediaType:     MediaTypeImageManifest,
		Config:        NewDescriptor(MediaTypeImageConfig, "sha256:abc", 100),
		Layers:        []Descriptor{{MediaType: MediaTypeImageLayerGzip, Size: 100}},
	}

	errs := ValidateManifest(&m)
	found := false
	for _, e := range errs {
		if e == "layer[0]: digest is required" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected layer digest error, got %v", errs)
	}
}

func TestValidateManifest_LayerInvalidSize(t *testing.T) {
	m := Manifest{
		SchemaVersion: 2,
		MediaType:     MediaTypeImageManifest,
		Config:        NewDescriptor(MediaTypeImageConfig, "sha256:abc", 100),
		Layers:        []Descriptor{{MediaType: MediaTypeImageLayerGzip, Digest: "sha256:l1", Size: 0}},
	}

	errs := ValidateManifest(&m)
	found := false
	for _, e := range errs {
		if e == "layer[0]: size must be positive" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected layer size error, got %v", errs)
	}
}

func TestValidateManifest_DockerMediaType(t *testing.T) {
	m := Manifest{
		SchemaVersion: 2,
		MediaType:     MediaTypeDockerManifestV2,
		Config:        NewDescriptor(MediaTypeDockerImageConfig, "sha256:abc", 100),
		Layers:        []Descriptor{NewDescriptor(MediaTypeDockerLayerGzip, "sha256:l1", 200)},
	}

	errs := ValidateManifest(&m)
	if len(errs) != 0 {
		t.Errorf("expected no errors for Docker manifest v2, got %v", errs)
	}
}

func TestValidateIndex_Valid(t *testing.T) {
	idx := NewIndex([]Descriptor{
		{
			MediaType: MediaTypeImageManifest,
			Digest:    "sha256:abc123",
			Size:      1024,
			Platform:  &Platform{Architecture: "amd64", OS: "linux"},
		},
	})

	errs := ValidateIndex(&idx)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidateIndex_InvalidSchemaVersion(t *testing.T) {
	idx := Index{
		SchemaVersion: 1,
		MediaType:     MediaTypeImageIndex,
		Manifests:     []Descriptor{{Digest: "sha256:abc", Platform: &Platform{Architecture: "amd64", OS: "linux"}}},
	}

	errs := ValidateIndex(&idx)
	if len(errs) == 0 {
		t.Fatal("expected errors for schemaVersion != 2")
	}
}

func TestValidateIndex_InvalidMediaType(t *testing.T) {
	idx := Index{
		SchemaVersion: 2,
		MediaType:     "invalid/type",
		Manifests:     []Descriptor{{Digest: "sha256:abc", Platform: &Platform{Architecture: "amd64", OS: "linux"}}},
	}

	errs := ValidateIndex(&idx)
	if len(errs) == 0 {
		t.Fatal("expected errors for invalid mediaType")
	}
}

func TestValidateIndex_NoManifests(t *testing.T) {
	idx := Index{
		SchemaVersion: 2,
		MediaType:     MediaTypeImageIndex,
		Manifests:     []Descriptor{},
	}

	errs := ValidateIndex(&idx)
	found := false
	for _, e := range errs {
		if e == "at least one manifest is required in index" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'at least one manifest is required' error, got %v", errs)
	}
}

func TestValidateIndex_MissingDigest(t *testing.T) {
	idx := Index{
		SchemaVersion: 2,
		MediaType:     MediaTypeImageIndex,
		Manifests:     []Descriptor{{Platform: &Platform{Architecture: "amd64", OS: "linux"}}},
	}

	errs := ValidateIndex(&idx)
	found := false
	for _, e := range errs {
		if e == "manifests[0]: digest is required" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected digest required error, got %v", errs)
	}
}

func TestValidateIndex_MissingPlatform(t *testing.T) {
	idx := Index{
		SchemaVersion: 2,
		MediaType:     MediaTypeImageIndex,
		Manifests:     []Descriptor{{Digest: "sha256:abc"}},
	}

	errs := ValidateIndex(&idx)
	found := false
	for _, e := range errs {
		if e == "manifests[0]: platform is recommended for index entries" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected platform recommendation, got %v", errs)
	}
}

func TestValidateIndex_DockerManifestList(t *testing.T) {
	idx := Index{
		SchemaVersion: 2,
		MediaType:     MediaTypeDockerManifestList,
		Manifests: []Descriptor{
			{Digest: "sha256:abc", Platform: &Platform{Architecture: "amd64", OS: "linux"}},
		},
	}

	errs := ValidateIndex(&idx)
	if len(errs) != 0 {
		t.Errorf("expected no errors for Docker manifest list, got %v", errs)
	}
}
