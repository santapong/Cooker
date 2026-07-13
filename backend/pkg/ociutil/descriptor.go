// Package ociutil provides utility functions for working with OCI descriptors.
package ociutil

import (
	"encoding/json"
	"fmt"

	"github.com/santapong/cooker/internal/build/oci"
)

// ParseManifest attempts to parse JSON as an OCI Manifest.
func ParseManifest(data []byte) (*oci.Manifest, error) {
	var m oci.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}
	if errs := oci.ValidateManifest(&m); len(errs) > 0 {
		return nil, fmt.Errorf("invalid manifest: %v", errs)
	}
	return &m, nil
}

// ParseIndex attempts to parse JSON as an OCI Image Index.
func ParseIndex(data []byte) (*oci.Index, error) {
	var idx oci.Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parsing index: %w", err)
	}
	if errs := oci.ValidateIndex(&idx); len(errs) > 0 {
		return nil, fmt.Errorf("invalid index: %v", errs)
	}
	return &idx, nil
}

// TotalLayerSize sums the sizes of all layers in a manifest.
func TotalLayerSize(m *oci.Manifest) int64 {
	var total int64
	for _, l := range m.Layers {
		total += l.Size
	}
	return total
}
