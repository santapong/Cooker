package store

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
)

func TestAppPreflightEnvironmentReasonRequiresAcceptedBinding(t *testing.T) {
	for _, mode := range []string{"bound", "bound-zero", "no-binding", "no-version", "negative-version", "private-binding", "unknown-reason", "mutated-binding"} {
		t.Run(mode, func(t *testing.T) {
			version := 1
			if mode == "bound-zero" {
				version = 0
			}
			payload, _ := json.Marshal(map[string]any{"version": 1, "runId": "run", "app": map[string]any{"id": "app", "environmentId": "env"}, "environmentVersion": version, "commit": strings.Repeat("a", 40), "createdAt": time.Now().UTC()})
			accepted, err := DecodeAppPreflightFailure(payload)
			if err != nil {
				t.Fatal(err)
			}
			f := accepted
			f.Reason = AppPreflightEnvironmentGeneration
			switch mode {
			case "no-binding":
				f.EnvironmentID = ""
			case "no-version":
				f.EnvironmentVersionPresent = false
			case "negative-version":
				f.EnvironmentVersion = -1
			case "private-binding":
				f.EnvironmentID = "_registries"
			case "unknown-reason":
				f.Reason = "provider-error-canary"
			case "mutated-binding":
				f.EnvironmentID = "other"
			}
			if same := SameAppPreflightAcceptance(f, accepted); same != (mode == "bound" || mode == "bound-zero") {
				t.Fatal("unaccepted binding/reason acquired determinate failure authority")
			}
		})
	}
}

func TestAppPreflightReasonInferenceIsBounded(t *testing.T) {
	for _, reason := range []AppPreflightReason{AppPreflightRegistryAuthority, AppPreflightEnvironmentGeneration} {
		run := &model.PipelineRun{Status: model.RunStatusFailed, Error: AppPreflightSummary(reason)}
		if got, ok := AppPreflightReasonFromRun(run); !ok || got != reason {
			t.Fatal("known outcome unavailable")
		}
	}
	for _, run := range []*model.PipelineRun{nil, {Status: model.RunStatusPending, Error: AppPreflightEnvironmentSummary}, {Status: model.RunStatusFailed, Error: "provider-error-canary"}, {Status: model.RunStatusFailed, Error: AppPreflightEnvironmentSummary + " extra"}} {
		if _, ok := AppPreflightReasonFromRun(run); ok {
			t.Fatal("arbitrary run gained determinate marker semantics")
		}
	}
}

func TestAppPreflightLegacyVersionPresenceMatchesDecoder(t *testing.T) {
	for _, payload := range []string{`{"version":null}`, `{"Version":null}`, `{"VERSION":0}`, `{"vErSiOn":2}`, `{"version":"1"}`, `{"version":1}`, `null`, `{"runId":`} {
		if IsLegacyAppDeploymentPayload([]byte(payload)) {
			t.Fatal("explicit/malformed version gained legacy recovery")
		}
	}
	if !IsLegacyAppDeploymentPayload([]byte(`{"runId":"legacy"}`)) {
		t.Fatal("unversioned legacy record rejected")
	}
}
