package oci

import "testing"

func TestIsOCIManifest(t *testing.T) {
	tests := []struct {
		name      string
		mediaType string
		want      bool
	}{
		{"OCI manifest", MediaTypeImageManifest, true},
		{"Docker manifest v2", MediaTypeDockerManifestV2, true},
		{"OCI index", MediaTypeImageIndex, false},
		{"Docker manifest list", MediaTypeDockerManifestList, false},
		{"empty string", "", false},
		{"random string", "text/plain", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsOCIManifest(tt.mediaType); got != tt.want {
				t.Errorf("IsOCIManifest(%q) = %v, want %v", tt.mediaType, got, tt.want)
			}
		})
	}
}

func TestIsOCIIndex(t *testing.T) {
	tests := []struct {
		name      string
		mediaType string
		want      bool
	}{
		{"OCI index", MediaTypeImageIndex, true},
		{"Docker manifest list", MediaTypeDockerManifestList, true},
		{"OCI manifest", MediaTypeImageManifest, false},
		{"Docker manifest v2", MediaTypeDockerManifestV2, false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsOCIIndex(tt.mediaType); got != tt.want {
				t.Errorf("IsOCIIndex(%q) = %v, want %v", tt.mediaType, got, tt.want)
			}
		})
	}
}
