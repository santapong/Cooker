package pusher

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/santapong/cooker/internal/build/artifact"
)

func testArtifact(t *testing.T, multi bool) (*artifact.Image, string) {
	t.Helper()
	a, err := artifact.NewImage()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	p, err := layout.Write(a.OutputDir(), empty.Index)
	if err != nil {
		t.Fatal(err)
	}
	var digest v1.Hash
	if multi {
		idx := mutate.AppendManifests(empty.Index, mutate.IndexAddendum{Add: empty.Image})
		if err = p.AppendIndex(idx); err != nil {
			t.Fatal(err)
		}
		digest, err = idx.Digest()
	} else {
		if err = p.AppendImage(empty.Image); err != nil {
			t.Fatal(err)
		}
		digest, err = empty.Image.Digest()
	}
	if err != nil {
		t.Fatal(err)
	}
	return a, digest.String()
}
func TestCranePublishesArtifactAndIndexWithRegistryAuth(t *testing.T) {
	for _, multi := range []bool{false, true} {
		t.Run(fmt.Sprint("multi=", multi), func(t *testing.T) {
			a, digest := testArtifact(t, multi)
			reg := registry.New()
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				u, p, ok := r.BasicAuth()
				if !ok || u != "fixture" || p != "fixture-password" {
					w.Header().Set("WWW-Authenticate", `Basic realm="fixture"`)
					w.WriteHeader(401)
					return
				}
				reg.ServeHTTP(w, r)
			}))
			defer ts.Close()
			host := strings.TrimPrefix(ts.URL, "http://")
			resolve := func(_ context.Context, h string) (authn.Authenticator, error) {
				if h != host {
					return nil, fmt.Errorf("unexpected registry host %s", h)
				}
				return authn.FromConfig(authn.AuthConfig{Username: "fixture", Password: "fixture-password"}), nil
			}
			res, err := NewCrane().Push(context.Background(), Request{Source: "never-contact.invalid/source:v1", Target: host + "/fixture/image:v1", Artifact: a, RegistryAuth: resolve})
			if err != nil {
				t.Fatal(err)
			}
			if res.Digest != digest {
				t.Fatalf("digest=%s want=%s", res.Digest, digest)
			}
			ref, _ := name.ParseReference(host + "/fixture/image:v1")
			desc, err := remote.Get(ref, remote.WithAuth(authn.FromConfig(authn.AuthConfig{Username: "fixture", Password: "fixture-password"})))
			if err != nil || desc.Digest.String() != digest {
				t.Fatalf("published descriptor=%v err=%v", desc, err)
			}
			if desc.MediaType.IsIndex() != multi {
				t.Fatalf("index not preserved: %s", desc.MediaType)
			}
		})
	}
}
func TestCraneMissingArtifactDoesNotFallbackToSource(t *testing.T) {
	a, _ := testArtifact(t, false)
	_ = a.Close()
	_, err := NewCrane().Push(context.Background(), Request{Source: "127.0.0.1:1/image:v1", Target: "127.0.0.1:1/image:v1", Artifact: a})
	if err == nil || !strings.Contains(err.Error(), "load build artifact") {
		t.Fatalf("expected artifact failure, got %v", err)
	}
}
func TestDockerRejectsArtifact(t *testing.T) {
	a, _ := testArtifact(t, false)
	_, err := (&DockerSock{Bin: "true"}).Push(context.Background(), Request{Artifact: a, Target: "fixture/image:v1"})
	if err == nil {
		t.Fatal("Docker accepted an OCI artifact it cannot consume")
	}
}

func TestNoopRejectsRealBuildArtifact(t *testing.T) {
	a, _ := testArtifact(t, false)
	if _, err := (Noop{}).Push(context.Background(), Request{Artifact: a, Target: "fixture/image:v1"}); err == nil {
		t.Fatal("noop falsely claimed real artifact publication")
	}
}
