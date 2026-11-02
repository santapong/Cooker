package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func modelTestRuntimeReceipt() *RuntimeReceipt {
	return &RuntimeReceipt{Version: 1, Runtime: "docker", RunID: "run-1", StageID: "deploy-web",
		ContainerID: strings.Repeat("a", 64), Name: "web", ImageReference: "registry.invalid/web:v1",
		ImageID: "sha256:" + strings.Repeat("b", 64), CapturedAt: time.Unix(1234, 0).UTC()}
}

func TestRuntimeReceiptCloneAndOptionalJSON(t *testing.T) {
	r := &PipelineRun{ID: "run-1", StageRuns: []StageRun{{StageID: "deploy-web", RuntimeReceipt: modelTestRuntimeReceipt()}}}
	clone := r.Clone()
	r.StageRuns[0].RuntimeReceipt.ContainerID = strings.Repeat("c", 64)
	r.StageRuns[0].RuntimeReceipt.CapturedAt = time.Now()
	if clone.StageRuns[0].RuntimeReceipt.ContainerID != strings.Repeat("a", 64) || !clone.StageRuns[0].RuntimeReceipt.CapturedAt.Equal(time.Unix(1234, 0)) {
		t.Fatal("receipt aliased across run snapshots")
	}
	clone.StageRuns[0].RuntimeReceipt.Name = "mutated"
	if r.StageRuns[0].RuntimeReceipt.Name != "web" {
		t.Fatal("receipt mutation leaked into original")
	}
	data, err := json.Marshal(StageRun{StageID: "legacy"})
	if err != nil || strings.Contains(string(data), "runtimeReceipt") || strings.Contains(string(data), "runtimeBindingWarning") {
		t.Fatal("legacy JSON acquired invented runtime metadata", err)
	}
	var legacy StageRun
	if err := json.Unmarshal([]byte(`{"stageId":"legacy","status":"success"}`), &legacy); err != nil || legacy.RuntimeReceipt != nil {
		t.Fatal("old stage JSON was not readable", err)
	}
	data, _ = json.Marshal(clone.StageRuns[0])
	var restored StageRun
	if err := json.Unmarshal(data, &restored); err != nil || restored.RuntimeReceipt == nil || !restored.RuntimeReceipt.Valid() {
		t.Fatal("typed receipt lost during JSON roundtrip", err)
	}
}

func TestRuntimeReceiptRejectsInvalidShape(t *testing.T) {
	for _, mutation := range []string{"version", "runtime", "short-id", "image-id", "name", "time", "run-bound", "control", "image-bound"} {
		t.Run(mutation, func(t *testing.T) {
			r := modelTestRuntimeReceipt()
			switch mutation {
			case "version":
				r.Version = 2
			case "runtime":
				r.Runtime = "compose"
			case "short-id":
				r.ContainerID = "abcd"
			case "image-id":
				r.ImageID = "sha256:abcd"
			case "name":
				r.Name = "web/other"
			case "time":
				r.CapturedAt = time.Time{}
			case "run-bound":
				r.RunID = strings.Repeat("a", 129)
			case "control":
				r.StageID = "deploy\nother"
			case "image-bound":
				r.ImageReference = strings.Repeat("a", 1025)
			}
			if r.Valid() {
				t.Fatal("invalid receipt shape accepted")
			}
		})
	}
}

func TestRuntimeReceiptTypedJSONDoesNotRetainUnknownPrivateFields(t *testing.T) {
	data, _ := json.Marshal(modelTestRuntimeReceipt())
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	wire["environment"] = map[string]string{"PASSWORD": "private-receipt-canary"}
	wire["labels"] = map[string]string{"arbitrary": "private-receipt-canary"}
	data, _ = json.Marshal(wire)
	var receipt RuntimeReceipt
	if err := json.Unmarshal(data, &receipt); err != nil || !receipt.Valid() {
		t.Fatal("typed receipt decode failed", err)
	}
	data, _ = json.Marshal(receipt)
	if strings.Contains(string(data), "private-receipt-canary") || strings.Contains(string(data), "environment") || strings.Contains(string(data), "labels") {
		t.Fatal("raw private fields persisted in typed receipt")
	}
}
