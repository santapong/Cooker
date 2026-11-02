package service

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/build/artifact"
	"github.com/santapong/cooker/internal/build/builder"
	"github.com/santapong/cooker/internal/build/pusher"
	"github.com/santapong/cooker/internal/model"
)

func TestExecutorOwnsArtifactThroughPushAndCleansEveryOutcome(t *testing.T) {
	for _, fails := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failed push"}[fails], func(t *testing.T) {
			a, err := artifact.NewImage()
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			b := &mockBuilder{res: builder.Result{Tags: []string{"registry.example/app:v1"}, Artifact: a}}
			push := &mockPusher{res: pusher.Result{Digest: "sha256:" + strings.Repeat("a", 64)}}
			if fails {
				push.err = errors.New("fixture push failure")
			}
			e := NewExecutor(WithBuilder(b), WithPusher(push))
			p := &model.Pipeline{ID: "fixture", Stages: []model.Stage{
				{ID: "build", Name: "Build", Type: model.StageTypeBuild, Config: model.StageConfig{Context: ".", Tags: []string{"registry.example/app:v1"}}},
				{ID: "push", Name: "Publish", Type: model.StageTypePush, Config: model.StageConfig{Image: "registry.example/app:v1", Repository: "registry.example/app:v1"}},
			}, Edges: []model.Edge{{Source: "build", Target: "push"}}}
			run := &model.PipelineRun{ID: "run", PipelineID: p.ID, Status: model.RunStatusPending, StageRuns: []model.StageRun{{StageID: "build", Status: model.RunStatusPending}, {StageID: "push", Status: model.RunStatusPending}}}
			_, err = e.Execute(context.Background(), p, run)
			if (err != nil) != fails {
				t.Fatalf("error=%v expected failure=%v", err, fails)
			}
			if len(push.calls) != 1 || push.calls[0].Artifact != a {
				t.Fatal("exact private artifact was not handed to publisher")
			}
			if _, err = os.Stat(a.OutputDir()); !os.IsNotExist(err) {
				t.Fatalf("artifact not cleaned: %v", err)
			}
		})
	}
}
func TestArtifactHandoffDoesNotCrossRunOrGuessPaths(t *testing.T) {
	one, releaseOne := withBuildArtifacts(context.Background())
	defer releaseOne()
	two, releaseTwo := withBuildArtifacts(context.Background())
	defer releaseTwo()
	a, err := artifact.NewImage()
	if err != nil {
		t.Fatal(err)
	}
	if err = keepBuildArtifact(one, a, []string{"app:v1"}); err != nil {
		t.Fatal(err)
	}
	if findBuildArtifact(two, "app:v1") != nil || findBuildArtifact(two, a.OutputDir()) != nil {
		t.Fatal("run-scoped artifact escaped")
	}
}
func TestComposeImageOverrideIsPrivateAndIndependent(t *testing.T) {
	image := "registry.example/team/api@sha256:" + strings.Repeat("a", 64)
	one, closeOne, err := composeImageOverride("api", image)
	if err != nil {
		t.Fatal(err)
	}
	defer closeOne()
	two, closeTwo, err := composeImageOverride("worker", image)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTwo()
	if one == two || one == "" {
		t.Fatal("parallel stages share their overlay")
	}
	data, err := os.ReadFile(one)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), image) || strings.Contains(string(data), "worker") {
		t.Fatalf("wrong overlay: %s", data)
	}
	info, _ := os.Stat(one)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("overlay not private: %o", info.Mode().Perm())
	}
	closeOne()
	if _, err = os.Stat(one); !os.IsNotExist(err) {
		t.Fatal("overlay not cleaned")
	}
}
