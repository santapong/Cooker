//go:build integration

package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/santapong/cooker/internal/build/builder"
	"github.com/santapong/cooker/internal/build/pusher"
	"github.com/santapong/cooker/internal/model"
)

// The fixture endpoints must belong to disposable test containers; this test
// publishes unique image tags and never deploys to an existing app or cloud.
func TestBuildKitCraneLiveHandoff(t *testing.T) {
	endpoint, registry := os.Getenv("COOKER_TEST_BUILDKIT_ADDR"), os.Getenv("COOKER_TEST_REGISTRY")
	if endpoint == "" || registry == "" {
		t.Skip("disposable BuildKit and registry endpoints required")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM scratch\nCOPY marker /marker\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "marker"), []byte("cooker-oci-handoff-20260928\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, multi := range []bool{false, true} {
		label := "single"
		platforms := []string{"linux/amd64"}
		if multi {
			label = "multi"
			platforms = append(platforms, "linux/arm64")
		}
		t.Run(label, func(t *testing.T) {
			ref := registry + "/cooker-handoff:" + label
			p := &model.Pipeline{ID: "handoff", Stages: []model.Stage{
				{ID: "build", Name: "Build", Type: model.StageTypeBuild, Config: model.StageConfig{Context: root, Tags: []string{ref}, Platforms: platforms}},
				{ID: "push", Name: "Publish", Type: model.StageTypePush, Config: model.StageConfig{Image: ref, Repository: ref}},
			}, Edges: []model.Edge{{Source: "build", Target: "push"}}}
			run := &model.PipelineRun{ID: "handoff-" + label, PipelineID: p.ID, Status: model.RunStatusPending, StageRuns: []model.StageRun{{StageID: "build", Status: model.RunStatusPending}, {StageID: "push", Status: model.RunStatusPending}}}
			bk := builder.NewBuildKit(endpoint)
			bk.TLSCA = os.Getenv("COOKER_TEST_BUILDKIT_TLS_CA")
			bk.TLSCert = os.Getenv("COOKER_TEST_BUILDKIT_TLS_CERT")
			bk.TLSKey = os.Getenv("COOKER_TEST_BUILDKIT_TLS_KEY")
			e := NewExecutor(WithBuilder(bk), WithPusher(pusher.NewCrane()))
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			result, err := e.Execute(ctx, p, run)
			if err != nil {
				for _, s := range run.StageRuns {
					t.Logf("stage=%s logs=%s", s.StageID, s.Logs)
				}
				t.Fatal(err)
			}
			if result.Status != model.RunStatusSuccess {
				t.Fatalf("unexpected run status %s", result.Status)
			}
			tag, err := name.ParseReference(ref)
			if err != nil {
				t.Fatal(err)
			}
			desc, err := remote.Get(tag, remote.WithContext(ctx))
			if err != nil {
				t.Fatal(err)
			}
			if desc.MediaType.IsIndex() != multi {
				t.Fatalf("multi=%v media=%s", multi, desc.MediaType)
			}
			var digest, image string
			for _, s := range run.StageRuns {
				if s.StageID == "push" {
					digest = s.Outputs["digest"]
					image = s.Outputs["image"]
				}
			}
			if digest == "" || digest != desc.Digest.String() || !strings.HasSuffix(image, "@"+digest) {
				t.Fatalf("published evidence differs: digest=%s image=%s remote=%s", digest, image, desc.Digest)
			}
			t.Logf("verified %s published digest=%s", label, digest)
		})
	}
}
