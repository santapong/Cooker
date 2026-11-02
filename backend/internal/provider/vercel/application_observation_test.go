package vercel

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestApplicationObservationDoesNotProveOrdinaryInventory(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		observed   bool
	}{
		{"owner ordinary", `{"id":"dpl_one","projectId":"prj_one","ownerId":"user_fixture","projectSettings":{"framework":"nextjs","environmentVariables":{"TOKEN":"private-config-canary"}}}`, true},
		{"owner services", `{"id":"dpl_one","projectId":"prj_one","ownerId":"user_fixture","projectSettings":{"framework":"services"}}`, false},
		{"owner static", `{"id":"dpl_one","projectId":"prj_one","ownerId":"user_fixture","projectSettings":{"framework":null}}`, true},
		{"owner null inventory", `{"id":"dpl_one","projectId":"prj_one","ownerId":"user_fixture","projectSettings":{"framework":"nextjs"},"services":null}`, true},
		{"reduced public", `{"id":"dpl_one","project":{"id":"prj_one"},"projectSettings":{"framework":"nextjs"}}`, false},
		{"owner settings absent", `{"id":"dpl_one","projectId":"prj_one","ownerId":"user_fixture"}`, false},
		{"owner settings null", `{"id":"dpl_one","projectId":"prj_one","ownerId":"user_fixture","projectSettings":null}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := clientFixture(t, func(*http.Request) (*http.Response, error) { return clientResponse(tc.body), nil })
			d, err := client.DeploymentDetails(context.Background(), clientTestToken, "", "dpl_one")
			if err != nil {
				t.Fatal(err)
			}
			if d.Details.ApplicationObserved != tc.observed || d.Details.ServicesKnown || len(d.Details.Services) != 0 {
				t.Fatal("owner observation became a complete service inventory", d.Details)
			}
			encoded, _ := json.Marshal(d)
			if strings.Contains(string(encoded), "private-config-canary") || strings.Contains(string(encoded), "projectSettings") {
				t.Fatal("project build configuration escaped projection")
			}
		})
	}
}
