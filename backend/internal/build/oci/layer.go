package oci

import (
	"crypto/sha256"
	"fmt"
	"io"
)

// ComputeDigest computes the SHA-256 digest of a reader in OCI format.
// Returns a string in the form "sha256:<hex>".
func ComputeDigest(r io.Reader) (string, int64, error) {
	h := sha256.New()
	n, err := io.Copy(h, r)
	if err != nil {
		return "", 0, fmt.Errorf("computing digest: %w", err)
	}
	digest := fmt.Sprintf("sha256:%x", h.Sum(nil))
	return digest, n, nil
}

// NewDescriptor creates an OCI Descriptor from the given parameters.
func NewDescriptor(mediaType, digest string, size int64) Descriptor {
	return Descriptor{
		MediaType: mediaType,
		Digest:    digest,
		Size:      size,
	}
}

// NewManifest creates a valid OCI Image Manifest.
func NewManifest(config Descriptor, layers []Descriptor) Manifest {
	return Manifest{
		SchemaVersion: 2,
		MediaType:     MediaTypeImageManifest,
		Config:        config,
		Layers:        layers,
	}
}

// NewIndex creates a valid OCI Image Index for multi-platform images.
func NewIndex(manifests []Descriptor) Index {
	return Index{
		SchemaVersion: 2,
		MediaType:     MediaTypeImageIndex,
		Manifests:     manifests,
	}
}
