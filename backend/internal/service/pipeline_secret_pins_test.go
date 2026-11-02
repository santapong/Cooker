package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
)

func TestSecretPinsNativeQueueExecutesOriginalStageSubset(t *testing.T) {
	st, resolver, manager, env := immutableSecretFixture(t)
	p, run := singleStagePipeline(model.StageTypeTest, model.StageConfig{Image: "owned-fixture", SecretRefs: []string{"TOKEN"}, Env: map[string]string{"PUBLIC": "setting"}})
	p.Stages[0].EnvironmentID = env.ID
	if err := st.Pipelines.Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	run.PipelineVersion = p.Version
	runner := &fakeStageRunner{}
	exec := NewExecutor(WithEnvironmentResolver(resolver), WithStageRunner(runner))
	jobs := jobqueue.NewMemory()
	enq := NewJobQueueEnqueuer(jobs, st.Pipelines)
	enq.PrepareSecrets = exec.PreparePipelineSecrets
	if err := enq.EnqueuePlannedRun(context.Background(), p, run); err != nil {
		t.Fatal(err)
	}
	job, err := jobs.Get(context.Background(), "run:"+run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(job.Payload), "canary-") {
		t.Fatal("native plan contains plaintext")
	}
	var payload runJobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Secrets == nil || len(payload.Secrets.Environments[env.ID].Keys) != 1 {
		t.Fatal("stage secret subset was not pinned")
	}
	manager.latest = "2"
	manager.data[env.ID]["TOKEN"] = []byte("canary-rotated-secret")
	lists := manager.lists
	if err := NewJobQueueRunner(st, exec, nil).Handle(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if runner.callCount() != 1 || runner.calls[0].Env["TOKEN"] != "canary-original-secret" || runner.calls[0].Env["OTHER"] != "" || runner.calls[0].Env["PUBLIC"] != "setting" || manager.lists != lists || manager.latestReads != 0 {
		t.Fatal("native execution escaped admitted stage keys/versions")
	}
	stored, _ := st.Pipelines.Get(context.Background(), p.ID)
	if stored.Stages[0].Config.Env["TOKEN"] != "" || p.Stages[0].Config.Env["TOKEN"] != "" {
		t.Fatal("ephemeral secret changed the stored definition")
	}
	actual, _ := st.Runs.Get(context.Background(), run.ID)
	data, _ := json.Marshal(actual)
	if actual.Status != model.RunStatusSuccess || strings.Contains(string(data), "canary-") {
		t.Fatal("unsafe run persistence")
	}
}

func TestSecretPinsNativeMissingOrSubstitutedPlansFailBeforeExecution(t *testing.T) {
	_, resolver, _, env := immutableSecretFixture(t)
	p, _ := singleStagePipeline(model.StageTypeTest, model.StageConfig{Image: "owned-fixture", SecretRefs: []string{"TOKEN"}})
	p.Stages[0].EnvironmentID = env.ID
	for _, scenario := range []string{"missing", "stage", "extra-key", "environment", "deleted-version"} {
		t.Run(scenario, func(t *testing.T) {
			runner := &fakeStageRunner{}
			exec := NewExecutor(WithEnvironmentResolver(resolver), WithStageRunner(runner))
			plan, err := exec.PreparePipelineSecrets(context.Background(), p)
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "missing":
				plan = nil
			case "stage":
				binding := plan.Stages[p.Stages[0].ID]
				binding.EnvironmentID = "other"
				plan.Stages[p.Stages[0].ID] = binding
			case "extra-key":
				plan.Environments[env.ID].Keys = append(plan.Environments[env.ID].Keys, "OTHER")
				ref, pinErr := resolver.Secrets.(*versionedSecretFixture).Pin(context.Background(), env.ID, "OTHER")
				if pinErr != nil {
					t.Fatal(pinErr)
				}
				plan.Environments[env.ID].References["OTHER"] = ref
			case "environment":
				plan.Environments[env.ID].EnvironmentVersion++
			case "deleted-version":
				ref := plan.Environments[env.ID].References["TOKEN"]
				ref.Version = "999"
				plan.Environments[env.ID].References["TOKEN"] = ref
			}
			_, run := singleStagePipeline(model.StageTypeTest, model.StageConfig{})
			if _, err := exec.Execute(withAcceptedPipelineSecrets(context.Background(), plan), p, run); err == nil || runner.callCount() != 0 {
				t.Fatal("unverified plan reached execution")
			}
		})
	}
}

func TestSecretPinsNativeUnsupportedStagesAndAdmissionFailClosed(t *testing.T) {
	_, resolver, _, env := immutableSecretFixture(t)
	exec := NewExecutor(WithEnvironmentResolver(resolver))
	for _, kind := range []model.StageType{model.StageTypeBuild, model.StageTypePush, model.StageTypeApproval, model.StageTypeGitOpsCommit, model.StageTypeDeploy} {
		t.Run(string(kind), func(t *testing.T) {
			p, _ := singleStagePipeline(kind, model.StageConfig{SecretRefs: []string{"TOKEN"}})
			p.Stages[0].EnvironmentID = env.ID
			if _, err := exec.PreparePipelineSecrets(context.Background(), p); err == nil {
				t.Fatal("unsupported secret transport accepted")
			}
		})
	}
	p, run := singleStagePipeline(model.StageTypeTest, model.StageConfig{Image: "owned-fixture", SecretRefs: []string{"TOKEN"}})
	p.Stages[0].EnvironmentID = env.ID
	if err := NewJobQueueEnqueuer(jobqueue.NewMemory()).EnqueuePlannedRun(context.Background(), p, run); err == nil {
		t.Fatal("native secret plan accepted without preparation")
	}
	p.Stages[0].Config.Env = map[string]string{"TOKEN": "literal-copy"}
	if _, err := exec.PreparePipelineSecrets(context.Background(), p); err == nil {
		t.Fatal("explicit config shadowed secret permission")
	}
}
