// Package oci provides types and utilities for OCI (Open Container Initiative) compliance.
// These types mirror the OCI image-spec v1.1 from github.com/opencontainers/image-spec/specs-go/v1.
// Reference: https://github.com/opencontainers/image-spec/blob/main/manifest.md
package oci

// Descriptor describes the disposition of targeted content.
// Reference: OCI image-spec - Content Descriptors
// https://github.com/opencontainers/image-spec/blob/main/descriptor.md
type Descriptor struct {
	MediaType   string            `json:"mediaType"`
	Digest      string            `json:"digest"`    // e.g., "sha256:abc123..."
	Size        int64             `json:"size"`
	URLs        []string          `json:"urls,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Platform    *Platform         `json:"platform,omitempty"`
}

// Manifest represents an OCI Image Manifest.
// Reference: OCI image-spec - Image Manifest
// https://github.com/opencontainers/image-spec/blob/main/manifest.md
type Manifest struct {
	SchemaVersion int               `json:"schemaVersion"` // Must be 2
	MediaType     string            `json:"mediaType"`     // application/vnd.oci.image.manifest.v1+json
	Config        Descriptor        `json:"config"`
	Layers        []Descriptor      `json:"layers"`
	Annotations   map[string]string `json:"annotations,omitempty"`
}

// Index represents an OCI Image Index for multi-platform images.
// Reference: OCI image-spec - Image Index
// https://github.com/opencontainers/image-spec/blob/main/image-index.md
type Index struct {
	SchemaVersion int               `json:"schemaVersion"` // Must be 2
	MediaType     string            `json:"mediaType"`     // application/vnd.oci.image.index.v1+json
	Manifests     []Descriptor      `json:"manifests"`
	Annotations   map[string]string `json:"annotations,omitempty"`
}

// Platform describes the platform which the image in the manifest runs on.
type Platform struct {
	Architecture string `json:"architecture"` // e.g., "amd64", "arm64"
	OS           string `json:"os"`           // e.g., "linux", "windows"
	Variant      string `json:"variant,omitempty"`
}
