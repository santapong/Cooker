package vercel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func staticClientFixture(t *testing.T, fn func(*http.Request) (*http.Response, error)) *Client {
	t.Helper()
	c := NewClient()
	c.http.Transport = clientRoundTrip(func(req *http.Request) (*http.Response, error) {
		if req.URL.Scheme != "https" || req.URL.Host != "api.vercel.com" || req.URL.User != nil {
			t.Fatal("request left fixed provider origin")
		}
		if req.Method != http.MethodGet && req.Method != http.MethodPost {
			t.Fatal("unexpected publishing method")
		}
		if req.Header.Get("Authorization") != "Bearer "+clientTestToken || strings.Contains(req.URL.String(), clientTestToken) {
			t.Fatal("unsafe token handling")
		}
		if req.Header.Get("Idempotency-Key") != "" || req.Header.Get("X-Idempotency-Key") != "" || req.URL.Query().Has("forceNew") {
			t.Fatal("unsupported deduplication behavior")
		}
		return fn(req)
	})
	return c
}

func staticInput() StaticDeploymentRequest {
	return StaticDeploymentRequest{ProjectID: "prj_new", Name: "cooker-landing-abc123", OperationID: "operation-123", ArtifactSHA256: strings.Repeat("a", 64), Files: []StaticFile{{Path: "index.html", Data: []byte("<h1>Cooker</h1>")}, {Path: "assets/logo.png", Data: []byte{0, 1, 2, 255}}}}
}

func staticFullResponse() string {
	return `{"id":"dpl_new","projectId":"prj_new","ownerId":"team_123","readyState":"READY","url":"cooker-landing-abc123.vercel.app","target":"production","meta":{"cookerOperationId":"operation-123","cookerArtifactSha256":"` + strings.Repeat("a", 64) + `","other":"never-import"}}`
}

func staticRequireUnknown(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrMutationUnknown) {
		t.Fatalf("missing uncertain outcome classification: %v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("missing safe API error: %v", err)
	}
	if strings.Contains(err.Error(), clientTestToken) || strings.Contains(err.Error(), "private-response") {
		t.Fatal("publishing error leaked provider details")
	}
}

func TestStaticCreateProjectUsesDedicatedStaticSettings(t *testing.T) {
	calls := 0
	c := staticClientFixture(t, func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Method != http.MethodPost || req.URL.Path != "/v11/projects" || req.URL.Query().Get("teamId") != "team_123" {
			t.Fatal("wrong project creation scope")
		}
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		want := map[string]any{"name": "cooker-landing-abc123", "framework": nil, "buildCommand": "", "installCommand": "", "outputDirectory": "."}
		if !reflect.DeepEqual(body, want) {
			t.Fatalf("unexpected project settings: %#v", body)
		}
		return clientResponse(`{"id":"prj_new","name":"cooker-landing-abc123","framework":null,"accountId":"team_123"}`), nil
	})
	got, err := c.CreateStaticProject(context.Background(), clientTestToken, "team_123", "cooker-landing-abc123")
	if err != nil || got.ID != "prj_new" || calls != 1 {
		t.Fatalf("project %#v, %v", got, err)
	}
}

func TestStaticCreateDeploymentPinsFilesAndMetadata(t *testing.T) {
	input := staticInput()
	calls := 0
	c := staticClientFixture(t, func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Method != http.MethodPost || req.URL.Path != "/v13/deployments" || req.URL.Query().Get("teamId") != "team_123" {
			t.Fatal("wrong deployment scope")
		}
		if req.Header.Get("Content-Type") != "application/json" {
			t.Fatal("JSON request required")
		}
		var body struct {
			Project, Name, Target string
			Files                 []struct{ File, Data, Encoding string }
			Meta                  map[string]string
		}
		data, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(data, &body); err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err = json.Unmarshal(data, &fields); err != nil {
			t.Fatal(err)
		}
		if len(fields) != 5 || body.Project != input.ProjectID || body.Name != input.Name || body.Target != "production" || len(body.Files) != 2 {
			t.Fatal("deployment request changed reviewed identity")
		}
		if body.Meta["cookerOperationId"] != input.OperationID || body.Meta["cookerArtifactSha256"] != input.ArtifactSHA256 || len(body.Meta) != 2 {
			t.Fatal("missing operation evidence")
		}
		for i, file := range body.Files {
			decoded, err := base64.StdEncoding.DecodeString(file.Data)
			if err != nil || file.Encoding != "base64" || file.File != input.Files[i].Path || !reflect.DeepEqual(decoded, input.Files[i].Data) {
				t.Fatal("file bytes changed during upload encoding")
			}
		}
		return clientResponse(staticFullResponse()), nil
	})
	got, err := c.CreateStaticDeployment(context.Background(), clientTestToken, "team_123", input)
	if err != nil || got.ID != "dpl_new" || got.ProjectID != "prj_new" || got.OwnerID != "team_123" || got.OperationID != input.OperationID || got.ArtifactSHA256 != input.ArtifactSHA256 || got.State != "READY" || got.URL != "https://cooker-landing-abc123.vercel.app" || got.Target != "production" || calls != 1 {
		t.Fatalf("deployment %#v, %v", got, err)
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "never-import") {
		t.Fatal("unrelated metadata imported")
	}
}

func TestStaticReducedAcknowledgementKeepsIDWithoutInventingEvidence(t *testing.T) {
	c := staticClientFixture(t, func(*http.Request) (*http.Response, error) {
		return clientResponse(`{"id":"dpl_new","readyState":"QUEUED"}`), nil
	})
	got, err := c.CreateStaticDeployment(context.Background(), clientTestToken, "team_123", staticInput())
	if err != nil || got.ID != "dpl_new" || got.State != "QUEUED" || got.ProjectID != "" || got.OwnerID != "" || got.OperationID != "" || got.ArtifactSHA256 != "" || got.Target != "" {
		t.Fatalf("reduced acknowledgement: %#v %v", got, err)
	}
}

func TestStaticAcknowledgedIDSurvivesInvalidAndTruncatedResponses(t *testing.T) {
	for _, body := range []string{
		`{"id":"dpl_new","readyState":"QUEUED","meta":`,
		`{"id":"dpl_new","readyState":42}`,
		`{"id":"dpl_new","readyState":"QUEUED","meta":{"cookerOperationId":false}}`,
		`{"id":"dpl_new","readyState":"QUEUED","id":"dpl_other"}`,
		`{"id":"dpl_new","readyState":"QUEUED"} {}`,
		`{"id":"dpl_new"}`,
	} {
		calls := 0
		c := staticClientFixture(t, func(*http.Request) (*http.Response, error) { calls++; return clientResponse(body), nil })
		got, err := c.CreateStaticDeployment(context.Background(), clientTestToken, "team_123", staticInput())
		staticRequireUnknown(t, err)
		if got.ID != "dpl_new" || calls != 1 {
			t.Fatalf("lost acknowledged ID or repeated submission: %#v", got)
		}
	}
	c := staticClientFixture(t, func(*http.Request) (*http.Response, error) {
		return clientResponse(`{"id":"prj_new","name":"cooker-landing-abc123","framework":`), nil
	})
	project, err := c.CreateStaticProject(context.Background(), clientTestToken, "team_123", "cooker-landing-abc123")
	staticRequireUnknown(t, err)
	if project.ID != "prj_new" {
		t.Fatal("project acknowledgement lost")
	}
}

type staticBrokenReader struct{ data []byte }

func (r *staticBrokenReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, errors.New("private-response " + clientTestToken)
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func TestStaticReadFailureAndOversizePreserveAcknowledgement(t *testing.T) {
	for _, large := range []bool{false, true} {
		body := `{"id":"dpl_new","readyState":"QUEUED","padding":"`
		var reader io.Reader = &staticBrokenReader{data: []byte(body)}
		if large {
			reader = strings.NewReader(body + strings.Repeat("x", maxBodyBytes))
		}
		closed := &clientTrackedBody{Reader: reader}
		c := staticClientFixture(t, func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: closed}, nil
		})
		got, err := c.CreateStaticDeployment(context.Background(), clientTestToken, "team_123", staticInput())
		staticRequireUnknown(t, err)
		if got.ID != "dpl_new" || !closed.closed {
			t.Fatal("acknowledgement or response cleanup lost")
		}
	}
}

func TestStaticNeverRetriesMutationsAndSeparatesRejectionFromUncertainty(t *testing.T) {
	for _, status := range []int{400, 401, 402, 403, 404, 409, 429, 500, 503, 302, 307, 308} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			c := staticClientFixture(t, func(*http.Request) (*http.Response, error) {
				calls++
				r := clientResponse(`{"id":"dpl_new","readyState":"QUEUED","error":"private-response ` + clientTestToken + `"}`)
				r.StatusCode = status
				r.Header.Set("Location", "https://attacker.invalid/")
				r.Header.Set("Retry-After", "60")
				return r, nil
			})
			got, err := c.CreateStaticDeployment(context.Background(), clientTestToken, "team_123", staticInput())
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != status {
				t.Fatalf("missing status: %v", err)
			}
			if status >= 400 && status < 500 {
				if errors.Is(err, ErrMutationUnknown) || got.ID != "" {
					t.Fatal("explicit rejection treated as acknowledgement")
				}
			} else {
				staticRequireUnknown(t, err)
				if got.ID != "dpl_new" {
					t.Fatal("lost uncertain acknowledgement")
				}
			}
			if calls != 1 || strings.Contains(err.Error(), clientTestToken) {
				t.Fatal("retry or secret leak")
			}
		})
	}
	calls := 0
	c := staticClientFixture(t, func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("private-response " + clientTestToken)
	})
	_, err := c.CreateStaticProject(context.Background(), clientTestToken, "team_123", "cooker-landing-abc123")
	staticRequireUnknown(t, err)
	if calls != 1 {
		t.Fatal("transport failure retried")
	}
}

func TestStaticRejectsMismatchedProviderEvidenceWithoutLosingID(t *testing.T) {
	for _, replacement := range [][2]string{
		{`"projectId":"prj_new"`, `"projectId":"prj_wrong"`},
		{`"ownerId":"team_123"`, `"ownerId":"team_wrong"`},
		{`"operation-123"`, `"operation-wrong"`},
		{strings.Repeat("a", 64), strings.Repeat("b", 64)},
		{`"target":"production"`, `"target":null`},
	} {
		body := strings.Replace(staticFullResponse(), replacement[0], replacement[1], 1)
		c := staticClientFixture(t, func(*http.Request) (*http.Response, error) { return clientResponse(body), nil })
		got, err := c.CreateStaticDeployment(context.Background(), clientTestToken, "team_123", staticInput())
		staticRequireUnknown(t, err)
		if got.ID != "dpl_new" {
			t.Fatal("mismatched evidence discarded acknowledged deployment")
		}
	}
}

func TestStaticGetDeploymentProjectsOnlySafeEvidence(t *testing.T) {
	c := staticClientFixture(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.Path != "/v13/deployments/dpl_new" || req.URL.Query().Get("teamId") != "team_123" {
			t.Fatal("invalid status request")
		}
		return clientResponse(staticFullResponse()), nil
	})
	got, err := c.GetStaticDeployment(context.Background(), clientTestToken, "team_123", "dpl_new")
	if err != nil || got.ID != "dpl_new" || got.OperationID != "operation-123" || got.ProjectID != "prj_new" {
		t.Fatalf("detail: %#v %v", got, err)
	}
	c = staticClientFixture(t, func(*http.Request) (*http.Response, error) {
		return clientResponse(`{"id":"dpl_new","readyState":"ERROR","project":{"id":"prj_new"},"team":{"id":"team_123"},"url":"javascript:alert(1)","env":["private-response"],"errorMessage":"private-response ` + clientTestToken + `"}`), nil
	})
	got, err = c.GetStaticDeployment(context.Background(), clientTestToken, "team_123", "dpl_new")
	if err != nil || got.ProjectID != "prj_new" || got.OwnerID != "team_123" || got.State != "ERROR" || got.URL != "" {
		t.Fatalf("nested safe evidence: %#v %v", got, err)
	}
}

func TestStaticGetErrorsNeverBecomeMutationUncertainty(t *testing.T) {
	for _, body := range []string{`{"id":"dpl_other","readyState":"READY"}`, `{"id":"dpl_new","readyState":"READY","ownerId":"team_wrong"}`, `{"id":"dpl_new","readyState":false}`, `{"id":"dpl_new","readyState":"READY","projectId":"prj_a","project":{"id":"prj_b"}}`} {
		c := staticClientFixture(t, func(*http.Request) (*http.Response, error) { return clientResponse(body), nil })
		_, err := c.GetStaticDeployment(context.Background(), clientTestToken, "team_123", "dpl_new")
		if err == nil || errors.Is(err, ErrMutationUnknown) {
			t.Fatalf("GET error classification: %v", err)
		}
	}
	c := staticClientFixture(t, func(*http.Request) (*http.Response, error) { return nil, errors.New("private-response") })
	_, err := c.GetStaticDeployment(context.Background(), clientTestToken, "team_123", "dpl_new")
	if err == nil || errors.Is(err, ErrMutationUnknown) {
		t.Fatal("GET transport error marked mutation")
	}
}

func TestStaticValidationRejectsExecutableOrAmbiguousArtifactsBeforeNetwork(t *testing.T) {
	c := staticClientFixture(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid artifact reached provider")
		return nil, nil
	})
	for _, name := range []string{"../index.html", "/index.html", "a//index.html", "a/../index.html", "a\\index.html", "a%2findex.html", "index.html?token=x", ".env", ".git/config", ".vercel/output/config.json", "api/index.js", "x/api/index.js", "functions/a.js", "node_modules/a.js", "vercel.json", "now.json", "package.json", "package-lock.json", "deno.json", "requirements.txt", "middleware.js", "proxy.js", "server.go", "script.sh", "archive.zip", "index.html\x00"} {
		t.Run(name, func(t *testing.T) {
			input := staticInput()
			input.Files = append(input.Files, StaticFile{Path: name, Data: []byte("x")})
			_, err := c.CreateStaticDeployment(context.Background(), clientTestToken, "team_123", input)
			if err == nil || errors.Is(err, ErrMutationUnknown) {
				t.Fatalf("local validation failed: %v", err)
			}
		})
	}
	for _, change := range []func(*StaticDeploymentRequest){
		func(v *StaticDeploymentRequest) { v.ProjectID = "project-name" },
		func(v *StaticDeploymentRequest) { v.Name = "Uppercase" },
		func(v *StaticDeploymentRequest) { v.OperationID = "bad/operation" },
		func(v *StaticDeploymentRequest) { v.ArtifactSHA256 = "not-a-digest" },
		func(v *StaticDeploymentRequest) { v.Files = nil },
		func(v *StaticDeploymentRequest) { v.Files = v.Files[1:] },
		func(v *StaticDeploymentRequest) { v.Files = append(v.Files, StaticFile{Path: "INDEX.HTML"}) },
		func(v *StaticDeploymentRequest) { v.Files[0].Data = make([]byte, MaxStaticFileBytes+1) },
		func(v *StaticDeploymentRequest) {
			v.Files = []StaticFile{{Path: "index.html", Data: make([]byte, MaxStaticFileBytes)}, {Path: "one.js", Data: make([]byte, MaxStaticFileBytes)}, {Path: "two.js", Data: []byte("x")}}
		},
		func(v *StaticDeploymentRequest) {
			for i := 0; i < MaxStaticFiles; i++ {
				v.Files = append(v.Files, StaticFile{Path: fmt.Sprintf("%d.js", i)})
			}
		},
	} {
		input := staticInput()
		change(&input)
		_, err := c.CreateStaticDeployment(context.Background(), clientTestToken, "team_123", input)
		if err == nil || errors.Is(err, ErrMutationUnknown) {
			t.Fatal("invalid artifact accepted")
		}
	}
	_, err := c.CreateStaticProject(context.Background(), clientTestToken, "team_123", "bad/name")
	if err == nil || errors.Is(err, ErrMutationUnknown) {
		t.Fatal("invalid project name accepted")
	}
	_, err = c.GetStaticDeployment(context.Background(), clientTestToken, "team_123", "../dpl_new")
	if err == nil {
		t.Fatal("invalid deployment ID accepted")
	}
}

func TestStaticValidLandingAssetPaths(t *testing.T) {
	for _, name := range []string{"index.html", "assets/tokens.css", "main.js", "assets/login-workshop-art.png", "examples/n8n-queue/compose.yaml", "examples/n8n-queue/env.example", "examples/n8n-queue/README.md", "manifest.json"} {
		if !staticPath(name) {
			t.Fatalf("valid landing asset rejected: %s", name)
		}
	}
}

func TestStaticCanceledBeforeSubmissionDoesNotReachProvider(t *testing.T) {
	calls := 0
	c := staticClientFixture(t, func(*http.Request) (*http.Response, error) {
		calls++
		t.Fatal("canceled request submitted")
		return nil, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.CreateStaticDeployment(ctx, clientTestToken, "team_123", staticInput())
	if err == nil || errors.Is(err, ErrMutationUnknown) || calls != 0 {
		t.Fatalf("pre-submission cancellation: %v", err)
	}
}
