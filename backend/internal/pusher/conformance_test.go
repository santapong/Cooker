//go:build oci_conformance

// Package pusher conformance smoke test. Build-tagged so it only runs
// in the dedicated `oci-conformance` workflow (not in regular CI).
//
// Pre-requisite: a registry:2 instance reachable at $COOKER_OCI_REGISTRY
// (default localhost:5000). The workflow boots one as a service.
package pusher

import (
	"context"
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
