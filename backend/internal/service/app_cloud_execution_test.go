package service

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/deploy/deploytarget"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/source/github"
	"github.com/santapong/cooker/internal/store/memory"
)

type recordingCloud struct {
	specs           []deploytarget.Spec
	healthy         bool
	deploymentError error
	verifies        int
}

func (*recordingCloud) Kind() model.DeployTargetKind { return model.DeployTargetECS }
func (c *recordingCloud) Deploy(_ context.Context, s deploytarget.Spec) error {
	c.specs = append(c.specs, s)
	return c.deploymentError
}
func (c *recordingCloud) Status(context.Context, string) (deploytarget.Status, error) {
	return deploytarget.Status{Healthy: c.healthy}, nil
}
func (*recordingCloud) Logs(context.Context, string, io.Writer) error       { return nil }
func (*recordingCloud) Rollback(context.Context, string) error              { return nil }
func (c *recordingCloud) VerifyImage(context.Context, string, string) error { c.verifies++; return nil }

func TestAppDeployExecutesOnlyECSWorkloadAndPersistsGraphBeforeRunning(t *testing.T) {
	deploytarget.ResetForTest()
	t.Cleanup(deploytarget.ResetForTest)
	cloud := &recordingCloud{healthy: true}
	deploytarget.MustRegister(cloud)
	app := inspectionApp()
	cloudBinding(app)
	st := memory.New()
	if err := st.Environments.Create(context.Background(), &model.Environment{ID: "production", PlainVars: map[string]string{"SECRET": "test-secret", "CLOUD_DATABASE_URL": "postgres://cloud-existing"}}); err != nil {
		t.Fatal(err)
	}
	d := &AppDeployer{Executor: NewExecutor(), Registry: "registry.example.com", Deploys: st.AppDeploys, EnvResolver: &AppEnvResolver{Environments: st.Environments}, cloneFn: func(context.Context, github.CloneOptions) (string, error) { return inspectionFixture(t, nil), nil }}
	saved := false
	d.SavePipeline = func(_ context.Context, p *model.Pipeline) error {
		saved = true
		if len(cloud.specs) != 0 {
			t.Fatal("cloud mutated before graph was saved")
		}
		if !p.Stages[0].Config.ReviewOnly {
			t.Fatal("live secret-bearing graph persisted")
		}
		return nil
	}
	p, run, err := d.Deploy(context.Background(), app, "reviewed-run", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !saved || run.Status != model.RunStatusSuccess || len(cloud.specs) != 1 || cloud.verifies != 1 {
		t.Fatalf("dispatch/readiness mismatch: run=%s calls=%d", run.Status, len(cloud.specs))
	}
	if p.ID != "app-shop-reviewed-run" || run.PipelineID != p.ID {
		t.Fatal("deployment view identity mismatch")
	}
	spec := cloud.specs[0]
	if spec.AppID != "shop-prod-api" || spec.Env["DATABASE_URL"] != "postgres://cloud-existing" || len(spec.Ports) != 1 || spec.Ports[0] != 8080 {
		t.Fatalf("incorrect external binding or target inputs: %+v", spec)
	}
	records, _ := st.AppDeploys.ListByApp(context.Background(), app.ID, 10)
	if len(records) != 1 || records[0].Status != model.RunStatusSuccess {
		t.Fatal("terminal deployment history missing")
	}
}

func TestCloudDeploymentFailureAndReadinessTimeoutCannotSucceed(t *testing.T) {
	for _, failure := range []string{"deploy", "readiness"} {
		t.Run(failure, func(t *testing.T) {
			deploytarget.ResetForTest()
			t.Cleanup(deploytarget.ResetForTest)
			cloud := &recordingCloud{}
			if failure == "deploy" {
				cloud.deploymentError = errors.New("image pull denied")
			}
			deploytarget.MustRegister(cloud)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			err := NewExecutor().executeCloudDeploy(ctx, &model.Stage{Config: model.StageConfig{DeployRuntime: "ecs", RuntimeName: "shop-api", Image: "registry.example.com/shop:1"}}, io.Discard)
			if err == nil {
				t.Fatal("failed or unready cloud deployment reported success")
			}
		})
	}
}
