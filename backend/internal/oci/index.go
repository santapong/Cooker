package oci

import "fmt"

// ValidateManifest checks that a Manifest has valid required fields.
func ValidateManifest(m *Manifest) []string {
	var errors []string

	if m.SchemaVersion != 2 {
		errors = append(errors, "schemaVersion must be 2")
	}
	if m.MediaType != MediaTypeImageManifest && m.MediaType != MediaTypeDockerManifestV2 {
		errors = append(errors, "invalid manifest mediaType: "+m.MediaType)
	}
	if m.Config.Digest == "" {
		errors = append(errors, "config digest is required")
	}
	if m.Config.MediaType == "" {
		errors = append(errors, "config mediaType is required")
	}
	if len(m.Layers) == 0 {
		errors = append(errors, "at least one layer is required")
	}
	for i, l := range m.Layers {
		if l.Digest == "" {
			errors = append(errors, fmt.Sprintf("layer[%d]: digest is required", i))
		}
		if l.Size <= 0 {
			errors = append(errors, fmt.Sprintf("layer[%d]: size must be positive", i))
		}
	}

	return errors
}

// ValidateIndex checks that an Image Index has valid required fields.
func ValidateIndex(idx *Index) []string {
	var errors []string

	if idx.SchemaVersion != 2 {
		errors = append(errors, "schemaVersion must be 2")
	}
	if idx.MediaType != MediaTypeImageIndex && idx.MediaType != MediaTypeDockerManifestList {
		errors = append(errors, "invalid index mediaType: "+idx.MediaType)
	}
	if len(idx.Manifests) == 0 {
		errors = append(errors, "at least one manifest is required in index")
	}
	for i, m := range idx.Manifests {
		if m.Digest == "" {
			errors = append(errors, fmt.Sprintf("manifests[%d]: digest is required", i))
		}
		if m.Platform == nil {
			errors = append(errors, fmt.Sprintf("manifests[%d]: platform is recommended for index entries", i))
		}
	}

	return errors
}
