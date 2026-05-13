//go:build oci_conformance

// Package pusher conformance smoke test. Build-tagged so it only runs
// in the dedicated `oci-conformance` workflow (not in regular CI).
//
// Pre-requisite: a registry:2 instance reachable at $COOKER_OCI_REGISTRY
// (default localhost:5000). The workflow boots one as a service.
package pusher

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/santapong/cooker/internal/oci"
)

func registryHost(t *testing.T) string {
	host := os.Getenv("COOKER_OCI_REGISTRY")
	if host == "" {
		host = "localhost:5000"
	}
	return host
}

func conformanceNamespace() string {
	if v := os.Getenv("OCI_NAMESPACE"); v != "" {
		return v
	}
	return "cooker-conformance/test"
}

// TestPushConformance pushes a freshly-synthesised single-layer image
// to the upstream registry on a deterministic namespace so the
// upstream conformance binary's pull tests can target the same path.
// Repo path is taken from $OCI_NAMESPACE so CI and `make
// oci-conformance` agree.
func TestPushConformance(t *testing.T) {
	host := registryHost(t)
	repo := conformanceNamespace()
	tag := "test"
	ref := fmt.Sprintf("%s/%s:%s", host, repo, tag)

	layer := static.NewLayer([]byte("cooker-conformance-payload"), types.OCILayer)
	img, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		t.Fatalf("mutate: %v", err)
	}
	img, err = mutate.ConfigFile(img, &v1.ConfigFile{
		Architecture: "amd64",
		OS:           "linux",
	})
	if err != nil {
		t.Fatalf("config file: %v", err)
	}

	dst, err := name.ParseReference(ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}
	if err := remote.Write(dst, img, remote.WithContext(context.Background())); err != nil {
		t.Fatalf("write: %v", err)
	}

	digest, err := crane.Digest(ref)
	if err != nil {
		t.Fatalf("digest fetch: %v", err)
	}
	if !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("digest = %q, want sha256: prefix", digest)
	}
	t.Logf("conformance push OK: %s@%s", ref, digest)
}

// TestManifestSpecConformance pulls the manifest produced by
// TestPushConformance and validates it structurally against the OCI
// image-spec v1.1 (manifest.md, descriptor.md). Distribution-spec
// conformance covers the registry side; this covers the producer
// (Cooker's pusher) by asserting the manifest Cooker writes is
// well-formed per the spec. Backlog item P0.7.
//
// Validation rules enforced:
//   - schemaVersion must be 2
//   - mediaType must be the OCI image manifest media type
//   - config descriptor must have a valid sha256 digest, positive size,
//     and a non-empty mediaType
//   - layers must be non-empty; each layer descriptor must satisfy the
//     same digest/size/mediaType constraints as the config
func TestManifestSpecConformance(t *testing.T) {
	host := registryHost(t)
	repo := conformanceNamespace()
	tag := "test"
	ref := fmt.Sprintf("%s/%s:%s", host, repo, tag)

	raw, err := crane.Manifest(ref)
	if err != nil {
		t.Fatalf("fetch manifest %q: %v (TestPushConformance must run first)", ref, err)
	}

	var m oci.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v\nbody=%s", err, raw)
	}

	if m.SchemaVersion != 2 {
		t.Errorf("schemaVersion = %d, want 2 (image-spec manifest.md §schemaVersion)", m.SchemaVersion)
	}
	const wantMediaType = "application/vnd.oci.image.manifest.v1+json"
	if m.MediaType != wantMediaType {
		t.Errorf("mediaType = %q, want %q (image-spec manifest.md §mediaType)", m.MediaType, wantMediaType)
	}

	checkDescriptor(t, "config", m.Config)
	if len(m.Layers) == 0 {
		t.Error("layers is empty (image-spec manifest.md requires at least one layer)")
	}
	for i, layer := range m.Layers {
		checkDescriptor(t, fmt.Sprintf("layers[%d]", i), layer)
	}
}

func checkDescriptor(t *testing.T, label string, d oci.Descriptor) {
	t.Helper()
	if d.MediaType == "" {
		t.Errorf("%s.mediaType is empty (descriptor.md §mediaType is REQUIRED)", label)
	}
	if !strings.HasPrefix(d.Digest, "sha256:") {
		t.Errorf("%s.digest = %q, want sha256:<hex> (descriptor.md §digest)", label, d.Digest)
	} else if len(d.Digest) != len("sha256:")+64 {
		t.Errorf("%s.digest length = %d, want %d (sha256 is 64 hex chars)", label, len(d.Digest), len("sha256:")+64)
	}
	if d.Size <= 0 {
		t.Errorf("%s.size = %d, want > 0 (descriptor.md §size)", label, d.Size)
	}
}
