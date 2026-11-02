package vercel

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func staticAssessmentWire() string {
	return `{"id":"prj_one","accountId":"user_fixture","name":"Cooker landing","framework":null,"buildCommand":"","installCommand":"","outputDirectory":".","rootDirectory":null,"link":null,"services":[],"targets":{"production":{"id":"dpl_prior"}},"env":[{"value":"synthetic-private-env-canary"}],"metadata":{"private":"synthetic-private-metadata-canary"}}`
}

func TestStaticProjectReadIsExactScopedAndSafe(t *testing.T) {
	calls := 0
	c := staticClientFixture(t, func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Method != http.MethodGet || req.URL.Path != "/v9/projects/prj_one" || req.URL.Query().Get("teamId") != "team_fixture" {
			t.Fatal("assessment left exact read-only project scope")
		}
		return clientResponse(strings.Replace(staticAssessmentWire(), "user_fixture", "team_fixture", 1)), nil
	})
	a, err := c.StaticProject(context.Background(), clientTestToken, "team_fixture", "prj_one")
	if err != nil || !a.Eligible || calls != 1 || a.ProductionDeploymentID != "dpl_prior" {
		t.Fatal("supported static assessment unavailable", a, err, calls)
	}
	data, err := json.Marshal(a)
	if err != nil || strings.Contains(string(data), "synthetic-private") || strings.Contains(string(data), `"buildCommand":`) || strings.Contains(string(data), `"rootDirectory":`) {
		t.Fatal("assessment retained raw private settings", string(data), err)
	}
}

func TestStaticProjectAssessmentUnknownAndUnsupportedRemainDistinct(t *testing.T) {
	tests := []struct{ name, from, to, reason string }{
		{"missing framework", `"framework":null,`, ``, "framework_unknown"},
		{"framework", `"framework":null`, `"framework":"nextjs"`, "framework_unsupported"},
		{"missing build", `"buildCommand":"",`, ``, "build_command_unknown"},
		{"null build", `"buildCommand":""`, `"buildCommand":null`, "build_command_unknown"},
		{"private command", `"buildCommand":""`, `"buildCommand":"synthetic-private-command-canary"`, "build_command_unsupported"},
		{"missing install", `"installCommand":"",`, ``, "install_command_unknown"},
		{"null install", `"installCommand":""`, `"installCommand":null`, "install_command_unknown"},
		{"output", `"outputDirectory":"."`, `"outputDirectory":"private-output-canary"`, "output_directory_unsupported"},
		{"missing root", `"rootDirectory":null,`, ``, "root_directory_unknown"},
		{"root", `"rootDirectory":null`, `"rootDirectory":"synthetic-private-root-canary"`, "root_directory_unsupported"},
		{"missing link", `"link":null,`, ``, "git_link_unknown"},
		{"Git link", `"link":null`, `"link":{"type":"github"}`, "git_linked"},
		{"missing services", `"services":[],`, ``, "services_unknown"},
		{"services", `"services":[]`, `"services":[{"serviceName":"web"}]`, "services_unsupported"},
		{"missing production", `"targets":{"production":{"id":"dpl_prior"}},`, ``, "production_deployment_unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := staticProjectAssessment([]byte(strings.Replace(staticAssessmentWire(), tt.from, tt.to, 1)))
			if err != nil || a.Eligible {
				t.Fatal("unsupported/unknown assessment became eligible", a, err)
			}
			found := false
			for _, reason := range a.Reasons {
				if reason == tt.reason {
					found = true
				}
			}
			if !found {
				t.Fatal("lost explicit reason", a.Reasons)
			}
			data, err := json.Marshal(a)
			if err != nil || strings.Contains(string(data), "canary") {
				t.Fatal("private setting entered assessment", string(data), err)
			}
		})
	}
}

func TestStaticProjectAssessmentRejectsMalformedOrAmbiguousProviderData(t *testing.T) {
	for _, wire := range []string{
		`null`, `[]`, `{"id":"prj_one","id":"prj_other"}`,
		strings.Replace(staticAssessmentWire(), `"services":[]`, `"services":null`, 1),
		strings.Replace(staticAssessmentWire(), `"services":[]`, `"services":{}`, 1),
		strings.Replace(staticAssessmentWire(), `"link":null`, `"link":false`, 1),
		strings.Replace(staticAssessmentWire(), `"buildCommand":""`, `"buildCommand":false`, 1),
		strings.Replace(staticAssessmentWire(), `"rootDirectory":null`, `"rootDirectory":7`, 1),
		strings.Replace(staticAssessmentWire(), `"id":"dpl_prior"`, `"id":"dpl_prior","ID":"dpl_other"`, 1),
		strings.Replace(staticAssessmentWire(), `"accountId":"user_fixture"`, `"accountId":""`, 1),
	} {
		if got, err := staticProjectAssessment([]byte(wire)); err == nil || got != nil {
			t.Fatal("malformed provider data accepted", got, err)
		}
	}
}

func TestStaticProjectReadRejectsForeignScopeAndKeepsErrorsReadOnly(t *testing.T) {
	for _, wire := range []string{staticAssessmentWire(), strings.Replace(staticAssessmentWire(), "prj_one", "prj_other", 1)} {
		c := staticClientFixture(t, func(*http.Request) (*http.Response, error) { return clientResponse(wire), nil })
		if _, err := c.StaticProject(context.Background(), clientTestToken, "team_fixture", "prj_one"); err == nil || errors.Is(err, ErrMutationUnknown) {
			t.Fatal("foreign project or account accepted", err)
		}
	}
	for _, status := range []int{401, 403, 429} {
		calls := 0
		c := staticClientFixture(t, func(*http.Request) (*http.Response, error) {
			calls++
			response := clientResponse(`{"error":"synthetic-private-body-canary"}`)
			response.StatusCode = status
			return response, nil
		})
		_, err := c.StaticProject(context.Background(), clientTestToken, "", "prj_one")
		if err == nil || errors.Is(err, ErrMutationUnknown) || strings.Contains(err.Error(), "canary") || calls != 1 {
			t.Fatal("unsafe assessment failure/retry", err, calls)
		}
	}
}
