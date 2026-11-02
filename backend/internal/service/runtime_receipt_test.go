package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/deploy/deployer"
	"github.com/santapong/cooker/internal/model"
)

func serviceReceipt(req deployer.Request) *deployer.RuntimeReceipt {
	name := req.Name
	if name == "" {
		name = "cooker-service"
	}
	return &deployer.RuntimeReceipt{Version: 1, Runtime: "docker", RunID: req.Labels[RuntimeRunLabel], StageID: req.Labels[RuntimeStageLabel],
		ContainerID: strings.Repeat("a", 64), Name: name, ImageReference: req.Image,
		ImageID: "sha256:" + strings.Repeat("b", 64), CapturedAt: time.Now().UTC()}
}

func receiptStage(id, name string) *model.Stage {
	return &model.Stage{ID: id, Type: model.StageTypeDeploy, Config: model.StageConfig{
		DeployRuntime: "docker", RuntimeName: name, Image: "registry.invalid/web:v1"}}
}

func TestExecutorRuntimeReceiptTrustedIdentityAndCopy(t *testing.T) {
	var adapterReceipt *deployer.RuntimeReceipt
	dep := runtimeDeployCapture(func(_ context.Context, req deployer.Request) (deployer.Result, error) {
		if req.Labels[RuntimeRunLabel] != "run-1" || req.Labels[RuntimeStageLabel] != "deploy-web" {
			t.Fatal("missing executor-owned labels")
		}
		adapterReceipt = serviceReceipt(req)
		return deployer.Result{RuntimeReceipt: adapterReceipt}, nil
	})
	e := NewExecutor(WithDockerDeployer(dep))
	stage := receiptStage("deploy-web", "web")
	sr := &model.StageRun{StageID: stage.ID}
	if err := e.executeDeploy(context.Background(), "run-1", stage, sr); err != nil || sr.RuntimeReceipt == nil || sr.RuntimeBindingWarning != "" {
		t.Fatal("trusted receipt transfer failed", err)
	}
	adapterReceipt.Name = "changed"
	if sr.RuntimeReceipt.Name != "web" {
		t.Fatal("persisted receipt aliases adapter result")
	}
}

func TestExecutorRuntimeReceiptSubstitutionIsOptionalUnavailable(t *testing.T) {
	for _, mutation := range []string{"run", "stage", "name", "image", "short-id", "content-id", "version", "runtime", "time", "warning", "nil"} {
		t.Run(mutation, func(t *testing.T) {
			dep := runtimeDeployCapture(func(_ context.Context, req deployer.Request) (deployer.Result, error) {
				r := serviceReceipt(req)
				res := deployer.Result{RuntimeReceipt: r}
				switch mutation {
				case "run":
					r.RunID = "foreign-run"
				case "stage":
					r.StageID = "foreign-stage"
				case "name":
					r.Name = "another-workload"
				case "image":
					r.ImageReference = "registry.invalid/other:v1"
				case "short-id":
					r.ContainerID = "abcd"
				case "content-id":
					r.ImageID = "sha256:abcd"
				case "version":
					r.Version = 2
				case "runtime":
					r.Runtime = "compose"
				case "time":
					r.CapturedAt = time.Time{}
				case "warning":
					res.RuntimeBindingWarning = "private-adapter-warning-canary"
				case "nil":
					res.RuntimeReceipt = nil
				}
				return res, nil
			})
			sr := &model.StageRun{StageID: "deploy-web"}
			if err := NewExecutor(WithDockerDeployer(dep)).executeDeploy(context.Background(), "run-1", receiptStage(sr.StageID, "web"), sr); err != nil || sr.RuntimeReceipt != nil || sr.RuntimeBindingWarning != model.RuntimeBindingUnavailable {
				t.Fatal("invalid optional receipt changed operation or escaped", err)
			}
			data, _ := json.Marshal(sr)
			if strings.Contains(string(data), "private-adapter-warning-canary") {
				t.Fatal("raw adapter warning persisted")
			}
		})
	}
}

func TestExecutorRuntimeReceiptKnownSecretIsWithheldWithoutOperationFailure(t *testing.T) {
	secret := strings.Repeat("b", 64)
	ctx := WithLogSecrets(context.Background(), []string{secret})
	dep := runtimeDeployCapture(func(_ context.Context, req deployer.Request) (deployer.Result, error) {
		return deployer.Result{RuntimeReceipt: serviceReceipt(req)}, nil
	})
	sr := &model.StageRun{StageID: "deploy-web"}
	if err := NewExecutor(WithDockerDeployer(dep)).executeDeploy(ctx, "run-1", receiptStage(sr.StageID, "web"), sr); err != nil {
		t.Fatal("receipt secret withholding failed deployment", err)
	}
	if sr.RuntimeReceipt != nil || sr.RuntimeBindingWarning != model.RuntimeBindingUnavailable {
		t.Fatal("known secret entered typed receipt")
	}
	data, _ := json.Marshal(sr)
	if strings.Contains(string(data), secret) {
		t.Fatal("known secret escaped metadata")
	}
	if err := protectStageMetadata(ctx, sr); err != nil {
		t.Fatal("optional warning changed stage failure semantics", err)
	}
}

func TestRuntimeReceiptLateSecretRegistrationWithholdsWholeReceipt(t *testing.T) {
	ctx := WithLogSecrets(context.Background(), nil)
	req := deployer.Request{Kind: deployer.KindDockerRun, Name: "web", Image: "registry.invalid/web:v1", Labels: map[string]string{RuntimeRunLabel: "run-1", RuntimeStageLabel: "deploy-web"}}
	sr := &model.StageRun{StageID: "deploy-web"}
	assignRuntimeReceipt(ctx, "run-1", sr.StageID, req, deployer.Result{RuntimeReceipt: serviceReceipt(req)}, sr)
	RegisterLogSecrets(ctx, strings.Repeat("a", 64))
	if err := protectStageMetadata(ctx, sr); err != nil || sr.RuntimeReceipt != nil || sr.RuntimeBindingWarning != model.RuntimeBindingUnavailable {
		t.Fatal("late credential registration did not withhold optional receipt", err)
	}
	sr.Outputs = map[string]string{"bad": strings.Repeat("a", 64)}
	if err := protectStageMetadata(ctx, sr); err == nil {
		t.Fatal("existing artifact/output failure was weakened")
	}
}

func TestExecutorRuntimeReceiptSameNameDistinctStageIDs(t *testing.T) {
	dep := runtimeDeployCapture(func(_ context.Context, req deployer.Request) (deployer.Result, error) {
		r := serviceReceipt(req)
		if r.StageID == "second" {
			r.ContainerID = strings.Repeat("c", 64)
		}
		return deployer.Result{RuntimeReceipt: r}, nil
	})
	e := NewExecutor(WithDockerDeployer(dep))
	first, second := &model.StageRun{StageID: "first"}, &model.StageRun{StageID: "second"}
	for _, sr := range []*model.StageRun{first, second} {
		if err := e.executeDeploy(context.Background(), "same-run", receiptStage(sr.StageID, "same-name"), sr); err != nil || sr.RuntimeReceipt == nil {
			t.Fatal("same name rejected unambiguous stage receipt", err)
		}
	}
	if first.RuntimeReceipt.ContainerID == second.RuntimeReceipt.ContainerID || first.RuntimeReceipt.StageID == second.RuntimeReceipt.StageID {
		t.Fatal("same name collapsed exact identities")
	}
}

func TestExecutorRuntimeReceiptRetryClearsStaleAttemptAndPersistsNew(t *testing.T) {
	stage := *receiptStage("deploy-web", "web")
	stage.Config.Retry = &model.RetryPolicy{MaxAttempts: 2, InitialMS: 1, MaxMS: 1}
	p := &model.Pipeline{ID: "pipe", Stages: []model.Stage{stage}}
	run := &model.PipelineRun{ID: "run-1", PipelineID: p.ID, Status: model.RunStatusPending, StageRuns: []model.StageRun{{StageID: stage.ID, Status: model.RunStatusPending, RuntimeReceipt: &model.RuntimeReceipt{ContainerID: "stale"}, RuntimeBindingWarning: model.RuntimeBindingUnavailable}}}
	calls := 0
	dep := runtimeDeployCapture(func(_ context.Context, req deployer.Request) (deployer.Result, error) {
		calls++
		if run.StageRuns[0].RuntimeReceipt != nil || run.StageRuns[0].RuntimeBindingWarning != "" {
			t.Error("stale receipt retained before retry")
			return deployer.Result{}, errors.New("fixture detected stale receipt")
		}
		r := serviceReceipt(req)
		if calls == 1 {
			return deployer.Result{RuntimeReceipt: r}, errors.New("transient deployment error")
		}
		r.ContainerID = strings.Repeat("c", 64)
		return deployer.Result{RuntimeReceipt: r}, nil
	})
	var mu sync.Mutex
	var snapshots []*model.PipelineRun
	e := NewExecutor(WithDockerDeployer(dep), WithRunUpdater(func(_ context.Context, snapshot *model.PipelineRun) error {
		mu.Lock()
		defer mu.Unlock()
		snapshots = append(snapshots, snapshot.Clone())
		return nil
	}))
	result, err := e.Execute(context.Background(), p, run)
	if err != nil || result.Status != model.RunStatusSuccess || calls != 2 || run.StageRuns[0].RuntimeReceipt == nil || run.StageRuns[0].RuntimeReceipt.ContainerID != strings.Repeat("c", 64) {
		t.Fatal("retry did not preserve only new successful receipt", err)
	}
	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, snapshot := range snapshots {
		if sr := snapshot.StageRuns[0]; sr.Status == model.RunStatusSuccess && sr.RuntimeReceipt != nil && sr.RuntimeReceipt.ContainerID == strings.Repeat("c", 64) {
			found = true
		}
	}
	if !found {
		t.Fatal("successful receipt not retained in progress snapshot")
	}
}

func TestExecutorRuntimeReceiptGovernanceDenialClearsBeforeAdapter(t *testing.T) {
	stage := *receiptStage("deploy-web", "web")
	p := &model.Pipeline{ID: "pipe", Stages: []model.Stage{stage}}
	run := &model.PipelineRun{ID: "run-1", PipelineID: p.ID, Status: model.RunStatusPending, StageRuns: []model.StageRun{{StageID: stage.ID, Status: model.RunStatusPending, RuntimeReceipt: &model.RuntimeReceipt{ContainerID: "stale"}}}}
	e := NewExecutor(WithDeployGovernanceHook(func(_ context.Context, run *model.PipelineRun, _ *model.Stage) error {
		if run.StageRuns[0].RuntimeReceipt != nil {
			t.Error("stale receipt visible at governance gate")
		}
		return errors.New("fixture governance denial")
	}))
	_, err := e.Execute(context.Background(), p, run)
	if err == nil || run.StageRuns[0].RuntimeReceipt != nil {
		t.Fatal("denied retry retained receipt")
	}
}
