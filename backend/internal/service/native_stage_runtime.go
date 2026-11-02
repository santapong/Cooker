package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/santapong/cooker/internal/build/buildplan"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

var (
	ErrNativeRuntimeRequest     = errors.New("invalid native runtime request")
	ErrNativeRuntimeNotFound    = errors.New("native deployment stage not found")
	ErrNativeRuntimeUnavailable = errors.New("native runtime inspection unavailable")
)

// NativeRuntimeSubject identifies retained authored execution, never an App,
// an imported provider flow, a mutable stage definition or a container name.
type NativeRuntimeSubject struct {
	Kind       string `json:"kind"`
	PipelineID string `json:"pipelineId"`
	RunID      string `json:"runId"`
	StageID    string `json:"stageId"`
}

// NativeRuntimeStatus keeps the deployment operation, exact runtime binding,
// current state and Docker HEALTHCHECK observation independent. This slice
// supports inspection only; it grants no log or deployment capability.
type NativeRuntimeStatus struct {
	Subject             NativeRuntimeSubject `json:"subject"`
	RecordedStageStatus model.RunStatus      `json:"recordedStageStatus"`
	Binding             string               `json:"binding"`
	ReasonCode          string               `json:"reasonCode,omitempty"`
	Runtime             string               `json:"runtime"`
	ContainerID         string               `json:"containerId,omitempty"`
	Name                string               `json:"name,omitempty"`
	ImageReference      string               `json:"imageReference,omitempty"`
	ImageID             string               `json:"imageId,omitempty"`
	State               string               `json:"state"`
	Health              string               `json:"health"`
	ObservedAt          *time.Time           `json:"observedAt,omitempty"`
	SupportedActions    []string             `json:"supportedActions"`
}

type nativeRuntimeBinding struct {
	subject         NativeRuntimeSubject
	receipt         model.RuntimeReceipt
	stageStatus     model.RunStatus
	pipelineVersion int
	// Selected accepted metadata only. Nil records that this stage has no
	// secret references, even when other stages use an environment union.
	secretPin *EnvironmentSecretPlan
}

func nativeRuntimeID(value string) bool {
	return value != "" && len(value) <= 128 && strings.TrimSpace(value) == value &&
		!strings.ContainsAny(value, "/\\?#") && !strings.ContainsFunc(value, unicode.IsControl)
}

func nativeRuntimeUnavailable(subject NativeRuntimeSubject, status model.RunStatus, reason string) NativeRuntimeStatus {
	return NativeRuntimeStatus{Subject: subject, RecordedStageStatus: status, Binding: "unavailable", ReasonCode: reason,
		Runtime: "docker", State: "unknown", Health: "unknown", SupportedActions: []string{}}
}

// NativeStageStatus inspects only a server-recorded full container ID after
// checking retained accepted inputs. It performs no execution, provider reads,
// secret discovery, App synthesis or inventory-wide container inspection.
func (r *RuntimeService) NativeStageStatus(ctx context.Context, subject NativeRuntimeSubject, jobs jobqueue.Store) (NativeRuntimeStatus, error) {
	if subject.Kind == "" {
		subject.Kind = "native-stage"
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	binding, unavailable, err := r.resolveNativeStage(ctx, subject, jobs)
	if err != nil || binding == nil {
		return unavailable, err
	}
	return r.inspectNativeStage(ctx, binding), nil
}

func (r *RuntimeService) resolveNativeStage(ctx context.Context, subject NativeRuntimeSubject, jobs jobqueue.Store) (*nativeRuntimeBinding, NativeRuntimeStatus, error) {
	if subject.Kind != "native-stage" || !nativeRuntimeID(subject.PipelineID) || !nativeRuntimeID(subject.RunID) || !nativeRuntimeID(subject.StageID) {
		return nil, NativeRuntimeStatus{}, ErrNativeRuntimeRequest
	}
	if r == nil || r.Store == nil || r.Store.Pipelines == nil || r.Store.Runs == nil || jobs == nil {
		return nil, NativeRuntimeStatus{}, ErrNativeRuntimeUnavailable
	}
	// The current definition establishes presence only. Editing it must not
	// replace the accepted version, stage, source or effective image below.
	current, err := r.Store.Pipelines.Get(ctx, subject.PipelineID)
	if errors.Is(err, store.ErrNotFound) || err == nil && current == nil {
		return nil, NativeRuntimeStatus{}, ErrNativeRuntimeNotFound
	}
	if err != nil {
		return nil, NativeRuntimeStatus{}, ErrNativeRuntimeUnavailable
	}
	if current.ID != subject.PipelineID {
		return nil, NativeRuntimeStatus{}, ErrNativeRuntimeNotFound
	}
	run, err := r.Store.Runs.GetSummary(ctx, subject.RunID)
	if errors.Is(err, store.ErrNotFound) || err == nil && run == nil {
		return nil, NativeRuntimeStatus{}, ErrNativeRuntimeNotFound
	}
	if err != nil {
		return nil, NativeRuntimeStatus{}, ErrNativeRuntimeUnavailable
	}
	if run.ID != subject.RunID || run.PipelineID != subject.PipelineID {
		return nil, NativeRuntimeStatus{}, ErrNativeRuntimeNotFound
	}
	unavailable := nativeRuntimeUnavailable(subject, "", "accepted_input_unavailable")
	j, err := jobs.Get(ctx, "run:"+subject.RunID)
	if errors.Is(err, jobqueue.ErrNotFound) {
		return nil, unavailable, nil
	}
	if err != nil {
		return nil, NativeRuntimeStatus{}, ErrNativeRuntimeUnavailable
	}
	if j == nil || j.ID != "run:"+subject.RunID || j.Kind != JobKindPipelineRun || j.ConcurrencyKey != "pipeline:"+subject.PipelineID || len(j.Payload) == 0 || len(j.Payload) > 8*1024*1024 {
		return nil, unavailable, nil
	}
	var accepted runJobPayload
	if json.Unmarshal(j.Payload, &accepted) != nil || !nativeAcceptedRun(subject, run, &accepted) {
		return nil, unavailable, nil
	}
	p := accepted.Pipeline
	if len(p.Stages) == 0 || len(p.Stages) > 1024 || len(p.Edges) > 4096 || len(run.StageRuns) != len(p.Stages) || len(accepted.Run.StageRuns) != len(p.Stages) || len(ValidatePipelineDAG(p)) != 0 {
		return nil, unavailable, nil
	}
	stages := map[string]*model.Stage{}
	for i := range p.Stages {
		stage := &p.Stages[i]
		if !nativeRuntimeID(stage.ID) || stages[stage.ID] != nil {
			return nil, unavailable, nil
		}
		stages[stage.ID] = stage
	}
	stage := stages[subject.StageID]
	if stage == nil {
		return nil, NativeRuntimeStatus{}, ErrNativeRuntimeNotFound
	}
	stageRuns := map[string]*model.StageRun{}
	receiptIDs := map[string]bool{}
	for i := range run.StageRuns {
		sr := &run.StageRuns[i]
		if stages[sr.StageID] == nil || stageRuns[sr.StageID] != nil {
			return nil, unavailable, nil
		}
		stageRuns[sr.StageID] = sr
		if rr := sr.RuntimeReceipt; rr != nil {
			if !rr.Valid() || rr.RunID != run.ID || rr.StageID != sr.StageID || receiptIDs[rr.ContainerID] {
				return nil, unavailable, nil
			}
			receiptIDs[rr.ContainerID] = true
		}
	}
	plannedIDs := map[string]bool{}
	for _, sr := range accepted.Run.StageRuns {
		if stages[sr.StageID] == nil || plannedIDs[sr.StageID] || sr.Status != model.RunStatusPending || sr.StartedAt != nil || sr.FinishedAt != nil || sr.RuntimeReceipt != nil || sr.RuntimeBindingWarning != "" || sr.Logs != "" || sr.Error != "" || len(sr.Outputs) != 0 || len(sr.Artifacts) != 0 {
			return nil, unavailable, nil
		}
		plannedIDs[sr.StageID] = true
	}
	sr := stageRuns[subject.StageID]
	unavailable.RecordedStageStatus = nativeRecordedStatus(sr.Status)
	if !nativeObservationStatus(j.Status, run.Status) {
		unavailable.ReasonCode = "recorded_run_unavailable"
		return nil, unavailable, nil
	}
	if stage.Type != model.StageTypeDeploy || stage.Config.DeployRuntime != "docker" || stage.Config.ReviewOnly {
		unavailable.ReasonCode = "unsupported_runtime"
		return nil, unavailable, nil
	}
	if sr.Status != model.RunStatusSuccess {
		unavailable.ReasonCode = "recorded_stage_unavailable"
		return nil, unavailable, nil
	}
	if !nativeAcceptedSource(p, run, &accepted) || !nativeAcceptedSecretMetadata(p, accepted.Secrets) {
		return nil, unavailable, nil
	}
	if sr.RuntimeReceipt == nil || sr.RuntimeBindingWarning != "" {
		unavailable.ReasonCode = model.RuntimeBindingUnavailable
		return nil, unavailable, nil
	}
	deps := map[string][]string{}
	for _, edge := range p.Edges {
		deps[edge.Target] = append(deps[edge.Target], edge.Source)
	}
	// Same-run successful ancestor outputs are the execution authority. A
	// later process's output feature flag must not change this reconstruction.
	// Only Image is interpolated by execution for this identity. RuntimeName
	// and ComposeServiceName are passed verbatim; unrelated execution fields
	// must not be replayed or resolved just to observe a recorded container.
	effectiveImage, err := buildplan.Interpolate(stage.Config.Image, buildplan.OutputResolver{Stages: collectStageOutputs(stageRuns, ancestorsOf(stage.ID, deps))})
	if err != nil {
		return nil, unavailable, nil
	}
	effectiveName := stage.Config.RuntimeName
	if effectiveName == "" {
		effectiveName = stage.Config.ComposeServiceName
	}
	if effectiveName == "" {
		effectiveName = "cooker-service"
	}
	rr := sr.RuntimeReceipt
	if rr.Name != effectiveName || rr.ImageReference != effectiveImage || !runtimeReceiptMetadataSafe(ctx, rr) {
		return nil, unavailable, nil
	}
	if _, err := name.ParseReference(rr.ImageReference); err != nil {
		return nil, unavailable, nil
	}
	var selected *EnvironmentSecretPlan
	if len(stage.Config.SecretRefs) != 0 {
		selected = cloneNativeStageSecretPin(accepted.Secrets.Environments[stage.EnvironmentID], stage.Config.SecretRefs)
	}
	return &nativeRuntimeBinding{subject: subject, receipt: *rr, stageStatus: sr.Status, pipelineVersion: p.Version, secretPin: selected}, NativeRuntimeStatus{}, nil
}

func nativeAcceptedRun(s NativeRuntimeSubject, run *model.PipelineRun, a *runJobPayload) bool {
	p, planned := a.Pipeline, a.Run
	return p != nil && planned != nil && a.PipelineID == s.PipelineID && a.RunID == s.RunID && p.ID == s.PipelineID && p.Version > 0 &&
		planned.ID == s.RunID && planned.PipelineID == s.PipelineID && planned.PipelineVersion == p.Version && planned.Status == model.RunStatusPending &&
		!planned.CreatedAt.IsZero() && planned.StartedAt == nil && planned.FinishedAt == nil && run.PipelineVersion == p.Version &&
		run.StartedByUserSub == planned.StartedByUserSub && run.StartedByEmail == planned.StartedByEmail && run.StartedByTokenHash == planned.StartedByTokenHash && slices.Equal(run.StartedByGroups, planned.StartedByGroups)
}

func nativeObservationStatus(job jobqueue.Status, run model.RunStatus) bool {
	if job == jobqueue.StatusRunning {
		return run == model.RunStatusRunning
	}
	return job == jobqueue.StatusSucceeded && (run == model.RunStatusSuccess || run == model.RunStatusFailed || run == model.RunStatusCancelled)
}

func nativeRecordedStatus(status model.RunStatus) model.RunStatus {
	switch status {
	case model.RunStatusPending, model.RunStatusRunning, model.RunStatusSuccess, model.RunStatusFailed, model.RunStatusCancelled, model.RunStatusSkipped, model.RunStatusNeedsAttention:
		return status
	default:
		return ""
	}
}

func nativeAcceptedSource(p *model.Pipeline, run *model.PipelineRun, a *runJobPayload) bool {
	if p.Source == nil {
		return a.Source == nil && a.Trigger == nil && a.Run.Source == nil && run.Source == nil
	}
	if ValidatePipelineSourcePlan(p, a.Source) != nil {
		return false
	}
	if a.Trigger == nil {
		if a.Source.ReviewPolicy != "manual" {
			return false
		}
	} else {
		proof := a.Trigger
		digest, err := model.PipelineSourcePlanDigest(a.Source)
		if err != nil || proof.IntentID == "" || proof.RuleID == "" || proof.RuleVersion < 1 || proof.TargetKind != "pipeline" || proof.TargetID != p.ID || proof.SourcePlanDigest != digest || proof.EnvelopeHash != a.Source.EnvelopeHash {
			return false
		}
	}
	expected, err := model.RunSourceFromAcceptedPlan(a.Source)
	if err != nil || a.Run.Source == nil || a.Run.Source.CheckedOutAt != nil || !reflect.DeepEqual(expected, a.Run.Source) || run.Source == nil || run.Source.CheckedOutAt == nil || run.Source.Validate() != nil {
		return false
	}
	actual := run.Source.Clone()
	actual.CheckedOutAt = nil
	return reflect.DeepEqual(expected, actual)
}

// Validate private admission metadata without resolving credentials or reading
// today's environment/provider. Generation resolution belongs to the log slice.
func nativeAcceptedSecretMetadata(p *model.Pipeline, plan *PipelineSecretPlan) bool {
	bindings, err := pipelineSecretBindings(p)
	if err != nil {
		return false
	}
	if len(bindings) == 0 {
		return plan == nil
	}
	if plan == nil || plan.Version != 1 || len(plan.Stages) != len(bindings) {
		return false
	}
	union := map[string][]string{}
	for id, expected := range bindings {
		got, ok := plan.Stages[id]
		if !ok || got.EnvironmentID != expected.EnvironmentID || !slices.Equal(got.Keys, expected.Keys) {
			return false
		}
		for _, key := range expected.Keys {
			if !slices.Contains(union[expected.EnvironmentID], key) {
				union[expected.EnvironmentID] = append(union[expected.EnvironmentID], key)
			}
		}
	}
	if len(plan.Environments) != len(union) {
		return false
	}
	for id, keys := range union {
		sort.Strings(keys)
		pin := plan.Environments[id]
		if pin == nil || pin.Version != 1 || pin.EnvironmentID != id || pin.EnvironmentVersion < 1 || !slices.Equal(pin.Keys, keys) {
			return false
		}
		switch pin.Mode {
		case "database":
			if len(pin.References) != 0 {
				return false
			}
		case "external":
			if len(pin.References) != len(keys) {
				return false
			}
			for _, key := range keys {
				ref, ok := pin.References[key]
				if !ok || ref.Validate() != nil || ref.Scope.EnvironmentID != id || ref.Scope.Key != key {
					return false
				}
			}
		default:
			return false
		}
	}
	return true
}

// Safe projection only: no Config.Env, raw labels, health command, Health.Log
// or raw errors. Configuration and the latest health result are independent;
// a configured check may have no State.Health yet, including before start.
const nativeRuntimeInspectFormat = `[` +
	`{{json .Id}},{{json .Name}},{{json .Image}},{{json .Config.Image}},` +
	`{{json (index .Config.Labels "io.cooker.run-id")}},{{json (index .Config.Labels "io.cooker.stage-id")}},` +
	`{{json .State.Status}},` +
	`{{if .Config.Healthcheck}}{{if .Config.Healthcheck.Test}}` +
	`{{if eq (index .Config.Healthcheck.Test 0) "NONE"}}"disabled"` +
	`{{else if or (eq (index .Config.Healthcheck.Test 0) "CMD") (eq (index .Config.Healthcheck.Test 0) "CMD-SHELL")}}"configured"` +
	`{{else}}"unknown"{{end}}{{else}}"unknown"{{end}}{{else}}"absent"{{end}},` +
	`{{if .State.Health}}{{json .State.Health.Status}}{{else}}""{{end}}]`

func (r *RuntimeService) inspectNativeStage(ctx context.Context, binding *nativeRuntimeBinding) NativeRuntimeStatus {
	result := nativeRuntimeUnavailable(binding.subject, binding.stageStatus, "runtime_unavailable")
	rr := binding.receipt
	b := &boundedRuntimeLogBuffer{limit: 64 * 1024}
	cmd := exec.CommandContext(ctx, r.dockerBin(), "container", "inspect", "--format", nativeRuntimeInspectFormat, rr.ContainerID)
	cmd.WaitDelay = 250 * time.Millisecond
	cmd.Stdout, cmd.Stderr = b, io.Discard
	if cmd.Run() != nil {
		return result
	}
	var fields []string
	if json.Unmarshal(b.Bytes(), &fields) != nil || len(fields) != 9 {
		return result
	}
	if fields[0] != rr.ContainerID || fields[1] != "/"+rr.Name || fields[2] != rr.ImageID || fields[3] != rr.ImageReference || fields[4] != rr.RunID || fields[5] != rr.StageID {
		result.ReasonCode = "runtime_identity_mismatch"
		return result
	}
	if !runtimeReceiptMetadataSafe(ctx, &rr) {
		return result
	}
	state, health := "unknown", "unknown"
	switch fields[6] {
	case "created", "running", "paused", "restarting", "removing", "exited", "dead":
		state = fields[6]
	}
	if fields[7] == "absent" || fields[7] == "disabled" {
		health = "not_configured"
	} else if fields[7] == "configured" && state == "running" {
		switch fields[8] {
		case "healthy", "unhealthy", "starting":
			health = fields[8]
		}
	}
	now := time.Now().UTC()
	return NativeRuntimeStatus{Subject: binding.subject, RecordedStageStatus: binding.stageStatus, Binding: "verified", Runtime: "docker",
		ContainerID: rr.ContainerID, Name: rr.Name, ImageReference: rr.ImageReference, ImageID: rr.ImageID,
		State: state, Health: health, ObservedAt: &now, SupportedActions: []string{"inspect"}}
}
