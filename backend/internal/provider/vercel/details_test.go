package vercel

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestProjectDetailsSafeProjectionAndExactProduction(t *testing.T) {
	c := clientFixture(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/v9/projects/prj_one" || req.URL.Query().Get("teamId") != "team_123" {
			t.Fatal("wrong scoped endpoint")
		}
		return clientResponse(`{"id":"prj_one","accountId":"team_123","targets":{"production":{"id":"dpl_actual","env":["private-secret"],"meta":{"secret":"private-secret"}},"preview":{"id":"dpl_not_production"}},"link":{"type":"github","org":"acme","repo":"app","productionBranch":"main","gitCredentialId":"private-secret","deployHooks":[{"url":"https://secret.example/hook"}]},"services":[{"serviceName":"web","serviceType":"web","framework":"nextjs","runtime":"node","env":{"KEY":"private-secret"}},{"serviceName":"worker","serviceType":"worker","runtime":"python"}],"env":[{"key":"PASSWORD","value":"private-secret"}]}`), nil
	})
	got, err := c.ProjectDetails(context.Background(), clientTestToken, "team_123", "prj_one")
	if err != nil {
		t.Fatal(err)
	}
	if got.ProductionDeploymentID != "dpl_actual" || got.Repository != "acme/app" || got.ProductionBranch != "main" || !got.ServicesKnown || len(got.Services) != 2 || got.Services[1].Type != "worker" || got.ObservedAt.IsZero() {
		t.Fatalf("wrong safe projection: %+v", got)
	}
	data, _ := json.Marshal(got)
	for _, secret := range []string{"private-secret", "gitCredentialId", "deployHooks", "PASSWORD", "team_123", "env", "meta"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("unsafe field leaked: %s", secret)
		}
	}
}
func TestDetailsServicesTruthAndSchemaVariants(t *testing.T) {
	for _, tc := range []struct {
		name, services string
		known          bool
		count          int
		wantType       string
	}{
		{"missing", "", false, 0, ""}, {"null", `,"services":null`, false, 0, ""}, {"explicit empty", `,"services":[]`, true, 0, ""},
		{"legacy", `,"services":[{"name":"web","schema":"experimentalServices","type":"web","runtime":"node","builder":{"env":{"TOKEN":"secret"}}}]`, true, 1, "web"},
		{"v2", `,"services":[{"name":"app","schema":"experimentalServicesV2","framework":"vite","root":".","builder":{"use":"secret"}}]`, true, 1, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := clientFixture(t, func(req *http.Request) (*http.Response, error) {
				if req.URL.Path != "/v13/deployments/dpl_one" {
					t.Fatal("wrong endpoint")
				}
				return clientResponse(`{"id":"dpl_one","projectId":"prj_one","ownerId":"user_fixture","name":"App","readyState":"READY","createdAt":1700000000000,"target":null,"url":"app.vercel.app","alias":["app.vercel.app","https://app.vercel.app","https://user:secret@app.vercel.app","https://app.vercel.app/?token=secret","http://app.vercel.app"],"aliasAssigned":true,"inspectorUrl":"https://vercel.com/acme/app/abc","buildingAt":1700000000100,"ready":1700000000200,"env":["secret"],"meta":{"TOKEN":"secret"}` + tc.services + `}`), nil
			})
			got, err := c.DeploymentDetails(context.Background(), clientTestToken, "", "dpl_one")
			if err != nil {
				t.Fatal(err)
			}
			d := got.Details
			if got.Target != "preview" || d.ServicesKnown != tc.known || len(d.Services) != tc.count || len(d.Aliases) != 1 || !d.AliasAssigned || d.InspectorURL != "https://vercel.com/acme/app/abc" || d.BuildingAt == nil || d.ReadyAt == nil {
				t.Fatalf("bad details %+v", got)
			}
			if tc.count > 0 && d.Services[0].Type != tc.wantType {
				t.Fatalf("invented service kind: %+v", d.Services)
			}
			data, _ := json.Marshal(got)
			if strings.Contains(string(data), "secret") || strings.Contains(string(data), "ownerId") || strings.Contains(string(data), "builder") {
				t.Fatal("excluded provider data leaked")
			}
		})
	}
}
func TestDetailIdentityMismatchAndReducedData(t *testing.T) {
	for _, body := range []string{
		`{"id":"dpl_other","projectId":"prj_one"}`, `{"id":"dpl_one"}`, `{"id":"dpl_one","projectId":"prj_one","project":{"id":"prj_other"}}`,
		`{"id":"dpl_one","projectId":"prj_one","services":[{"name":"web","schema":"unrecognized"}]}`, `{"id":"dpl_one","projectId":"prj_one","services":[{}]}`,
	} {
		c := clientFixture(t, func(*http.Request) (*http.Response, error) { return clientResponse(body), nil })
		_, err := c.DeploymentDetails(context.Background(), clientTestToken, "", "dpl_one")
		clientRequireCode(t, err, "invalid_response")
	}
	c := clientFixture(t, func(*http.Request) (*http.Response, error) {
		return clientResponse(`{"id":"dpl_one","project":{"id":"prj_one","name":"Public"},"aliasAssigned":false,"readyState":"READY"}`), nil
	})
	got, err := c.DeploymentDetails(context.Background(), clientTestToken, "", "dpl_one")
	if err != nil || got.Details.ServicesKnown || got.ProjectID != "prj_one" {
		t.Fatal("safe public union with unknown services not supported")
	}
	for _, body := range []string{`{"id":"prj_other"}`, `{"id":"prj_one","accountId":"team_other"}`, `{"id":"prj_one","targets":{"production":{"id":"../unsafe"}}}`} {
		c := clientFixture(t, func(*http.Request) (*http.Response, error) { return clientResponse(body), nil })
		_, err := c.ProjectDetails(context.Background(), clientTestToken, "team_123", "prj_one")
		clientRequireCode(t, err, "invalid_response")
	}
}
func TestDetailsFailureUsesSafeErrorAndNoRedirect(t *testing.T) {
	for _, status := range []int{403, 429, 302, 500} {
		c := clientFixture(t, func(*http.Request) (*http.Response, error) {
			r := clientResponse(`{"error":"private-response ` + clientTestToken + `"}`)
			r.StatusCode = status
			r.Header.Set("Retry-After", "600")
			return r, nil
		})
		_, err := c.ProjectDetails(context.Background(), clientTestToken, "", "prj_one")
		if err == nil || strings.Contains(err.Error(), clientTestToken) || strings.Contains(err.Error(), "private-response") {
			t.Fatal("unsafe failure")
		}
	}
	c := clientFixture(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid detail ID reached network")
		return nil, nil
	})
	_, err := c.DeploymentDetails(context.Background(), clientTestToken, "", "../wrong")
	clientRequireCode(t, err, "invalid_response")
}

func TestDeploymentDetailGitSourceSafeOwnerAndPublicProjection(t *testing.T) {
	sha := strings.Repeat("A", 40)
	for _, tc := range []struct {
		name, identity, source string
		branch, commit         string
	}{
		{"owner", `"projectId":"prj_one","ownerId":"user_fixture"`, `{"type":"github","ref":"release/2026-10","sha":"` + sha + `","repoId":123,"gitUrl":"https://user:private-canary@github.com/org/repo","commitMetadata":{"author":{"email":"private-canary"},"message":"private-canary"}}`, "release/2026-10", strings.ToLower(sha)},
		{"public reduced", `"project":{"id":"prj_one","name":"App"}`, `{"type":"gitlab","ref":"main","sha":"` + strings.Repeat("b", 64) + `","projectId":42}`, "main", strings.Repeat("b", 64)},
		{"missing ref", `"projectId":"prj_one"`, `{"type":"github","ref":null,"sha":"` + sha + `"}`, "", strings.ToLower(sha)},
		{"missing source", `"projectId":"prj_one"`, `null`, "", ""},
		{"invalid sha and control ref", `"projectId":"prj_one"`, `{"type":"github","ref":"main\nprivate-canary","sha":"private-canary"}`, "", ""},
		{"short sha", `"projectId":"prj_one"`, `{"type":"github","ref":"main","sha":"abcdef1"}`, "main", ""},
		{"unknown source type", `"projectId":"prj_one"`, `{"type":"unknown","ref":"main","sha":"` + sha + `"}`, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := clientFixture(t, func(*http.Request) (*http.Response, error) {
				return clientResponse(`{"id":"dpl_git",` + tc.identity + `,"readyState":"READY","gitSource":` + tc.source + `,"meta":{"githubCommitRef":"not-trusted-fallback","secret":"private-canary"},"env":["private-canary"],"logs":["private-canary"]}`), nil
			})
			got, err := c.DeploymentDetails(context.Background(), clientTestToken, "", "dpl_git")
			if err != nil {
				t.Fatal(err)
			}
			if got.GitBranch != tc.branch || got.GitCommitSHA != tc.commit {
				t.Fatalf("safe Git identity mismatch: branch=%q commit=%q", got.GitBranch, got.GitCommitSHA)
			}
			b, _ := json.Marshal(got)
			for _, raw := range []string{"private-canary", "gitSource", "gitUrl", "commitMetadata", "not-trusted-fallback"} {
				if strings.Contains(string(b), raw) {
					t.Fatalf("unapproved source payload persisted: %s", raw)
				}
			}
		})
	}
	// Anonymous reduced responses cannot bind the Git identity to a selected
	// project, even if an unexpected gitSource field is present.
	c := clientFixture(t, func(*http.Request) (*http.Response, error) {
		return clientResponse(`{"id":"dpl_git","readyState":"READY","gitSource":{"type":"github","ref":"main","sha":"` + sha + `"}}`), nil
	})
	_, err := c.DeploymentDetails(context.Background(), clientTestToken, "", "dpl_git")
	clientRequireCode(t, err, "invalid_response")
}
