package service

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/build/pusher"
	"github.com/santapong/cooker/internal/model"
)

func TestCanaryBuildReturnsVerifiedImmutableImage(t *testing.T) {
	clone, _, calls := fakeCloneDockerfile(t)
	digest := "sha256:" + strings.Repeat("c", 64)
	d := &AppDeployer{Executor: NewExecutor(WithPusher(&mockPusher{res: pusher.Result{Digest: digest}})), Registry: "registry.example", cloneFn: clone}
	app := &model.App{ID: "fixture", Name: "fixture", GitHubRepo: "team/app", Branch: "main", DeployTarget: model.DeployTarget{Kind: model.DeployTargetKubernetes}, BuildPlan: &model.BuildPlan{Kind: model.BuildPlanDockerfile, Path: "Dockerfile"}}
	image, _, run, err := d.BuildAndPushImage(context.Background(), app, "canary-run", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if calls() != 1 || image != "registry.example/fixture@"+digest {
		t.Fatalf("canary did not use configured source and immutable image: %s", image)
	}
	if len(run.StageRuns) != 2 {
		t.Fatalf("canary build deployed unexpectedly: %d stages", len(run.StageRuns))
	}
}
func TestCanaryBuildRejectsUnverifiedImageAndUnsupportedBuildPlan(t *testing.T) {
	clone, _, _ := fakeCloneDockerfile(t)
	d := &AppDeployer{Executor: NewExecutor(WithPusher(&mockPusher{})), Registry: "registry.example", cloneFn: clone}
	app := &model.App{ID: "fixture", Name: "fixture", GitHubRepo: "team/app", DeployTarget: model.DeployTarget{Kind: model.DeployTargetKubernetes}, BuildPlan: &model.BuildPlan{Kind: model.BuildPlanDockerfile, Path: "Dockerfile"}}
	image, _, _, err := d.BuildAndPushImage(context.Background(), app, "run", io.Discard)
	if err == nil || image != "" {
		t.Fatalf("unverified image exposed: %s err=%v", image, err)
	}
	app.BuildPlan.Kind = model.BuildPlanBuildpack
	if _, _, _, err = d.BuildAndPushImage(context.Background(), app, "run", io.Discard); err == nil {
		t.Fatal("buildpack path silently treated as Dockerfile")
	}
}
func TestDeploymentHistoryUsesVerifiedImageAndRollbackDigest(t *testing.T) {
	digest := "sha256:" + strings.Repeat("d", 64)
	image := "registry.example/app@" + digest
	run := &model.PipelineRun{ID: "run", Status: model.RunStatusSuccess, StageRuns: []model.StageRun{{StageID: "push", Status: model.RunStatusSuccess, Outputs: map[string]string{"image": image, "digest": digest}}}}
	app := &model.App{ID: "app"}
	rec := deployRecordFromRun(app, nil, run, "registry.example/app:v1", model.AppDeployKindDeploy)
	if rec.ImageRef != image || rec.Digest != digest {
		t.Fatalf("history not immutable: %+v", rec)
	}
	rec = deployRecordFromRun(app, nil, &model.PipelineRun{}, image, model.AppDeployKindRollback)
	if rec.Digest != digest {
		t.Fatal("rollback digest missing")
	}
	rec = deployRecordFromRun(app, nil, run, "", model.AppDeployKindDeploy)
	if rec.ImageRef != "" {
		t.Fatal("multi-image history falsely claims one rollback image")
	}
}
