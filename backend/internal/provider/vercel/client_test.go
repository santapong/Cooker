package vercel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

const clientTestToken = "fixture-sensitive-token-never-return"

type clientRoundTrip func(*http.Request) (*http.Response, error)

func (f clientRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func clientFixture(t *testing.T, fn func(*http.Request) (*http.Response, error)) *Client {
	t.Helper()
	c := NewClient()
	c.http.Transport = clientRoundTrip(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.Scheme != "https" || req.URL.Host != "api.vercel.com" || req.URL.User != nil {
			t.Fatal("client attempted a request outside the fixed read-only API origin")
		}
		if req.Header.Get("Authorization") != "Bearer "+clientTestToken {
			t.Fatal("missing scoped bearer token")
		}
		if strings.Contains(req.URL.String(), clientTestToken) {
			t.Fatal("token placed in URL")
		}
		return fn(req)
	})
	return c
}

func clientResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}

func clientRequireCode(t *testing.T, err error, code string) *APIError {
	t.Helper()
	var actual *APIError
	if !errors.As(err, &actual) || actual.Code != code {
		t.Fatalf("expected safe error %q; got %v", code, err)
	}
	if strings.Contains(actual.Message, clientTestToken) || strings.Contains(actual.Error(), "private-response") {
		t.Fatal("provider details leaked in error")
	}
	return actual
}

func TestClientVerifyScopes(t *testing.T) {
	for _, test := range []struct {
		name, team, path, body string
		want                   Identity
	}{
		{"personal", "", "/v2/user", `{"user":{"id":"user_123","name":"Jane","username":"jane","email":"do-not-import"}}`, Identity{ID: "user_123", Name: "Jane", Scope: "personal"}},
		{"personal name fallback", "", "/v2/user", `{"user":{"id":"user_123","name":null,"username":"jane"}}`, Identity{ID: "user_123", Name: "jane", Scope: "personal"}},
		{"team", "team_123", "/v2/teams/team_123", `{"id":"team_123","name":"Acme","slug":"acme"}`, Identity{ID: "team_123", Name: "Acme", Scope: "team"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := clientFixture(t, func(req *http.Request) (*http.Response, error) {
				if req.URL.Path != test.path || req.URL.RawQuery != "" {
					t.Fatal("wrong identity endpoint")
				}
				return clientResponse(test.body), nil
			})
			got, err := c.Verify(context.Background(), clientTestToken, test.team)
			if err != nil || got != test.want {
				t.Fatalf("identity = %#v, %v", got, err)
			}
		})
	}
}

func TestClientRejectsDifferentOrInvalidIdentity(t *testing.T) {
	for _, test := range []struct{ body, team, code string }{
		{`{"id":"team_wrong","name":"Wrong"}`, "team_expected", "identity_mismatch"},
		{`{"user":{"id":"../wrong"}}`, "", "invalid_response"},
		{`{"id":"user_123"}`, "", "invalid_response"},
		{`null`, "", "invalid_response"},
	} {
		c := clientFixture(t, func(*http.Request) (*http.Response, error) { return clientResponse(test.body), nil })
		_, err := c.Verify(context.Background(), clientTestToken, test.team)
		clientRequireCode(t, err, test.code)
	}
}

func TestClientRejectsInvalidInputWithoutNetwork(t *testing.T) {
	c := clientFixture(t, func(*http.Request) (*http.Response, error) { t.Fatal("invalid input reached network"); return nil, nil })
	for _, token := range []string{"", "has space", "has\nnewline", "has\ttab", "héllo", strings.Repeat("x", 8193)} {
		_, err := c.Verify(context.Background(), token, "")
		clientRequireCode(t, err, "invalid_input")
	}
	for _, team := range []string{"a-slug", "../team_x", "team_x?next=evil", "team_\x00"} {
		_, err := c.Projects(context.Background(), clientTestToken, team)
		clientRequireCode(t, err, "invalid_input")
	}
	_, err := c.Deployments(context.Background(), clientTestToken, "", "../project")
	clientRequireCode(t, err, "invalid_input")
}

func TestClientProjectsDocumentedShapesAndCursors(t *testing.T) {
	for _, test := range []struct{ name, first, next string }{
		{"string continuation", `{"projects":[{"id":"prj_a","name":"A","framework":"nextjs","env":[{"value":"must-not-import"}]}],"pagination":{"next":"JBSWY3DPEHPK3PXP"}}`, "JBSWY3DPEHPK3PXP"},
		{"numeric timestamp", `{"projects":[{"id":"prj_a","name":"A","framework":"nextjs"}],"pagination":{"next":1700000000123}}`, "1700000000123"},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			c := clientFixture(t, func(req *http.Request) (*http.Response, error) {
				calls++
				q := req.URL.Query()
				if req.URL.Path != "/v10/projects" || q.Get("limit") != "100" || q.Get("teamId") != "team_123" || q.Has("until") {
					t.Fatal("incorrect projects query")
				}
				if calls == 1 {
					if q.Has("from") {
						t.Fatal("unexpected initial cursor")
					}
					return clientResponse(test.first), nil
				}
				if q.Get("from") != test.next {
					t.Fatal("project continuation was not preserved")
				}
				return clientResponse(`{"projects":[{"id":"prj_a","name":"duplicate"},{"id":"prj_b","name":"B","framework":null}],"pagination":{"next":null}}`), nil
			})
			got, err := c.Projects(context.Background(), clientTestToken, "team_123")
			want := []Project{{ID: "prj_a", Name: "A", Framework: "nextjs"}, {ID: "prj_b", Name: "B"}}
			if err != nil || !reflect.DeepEqual(got, want) || calls != 2 {
				t.Fatalf("projects = %#v, %v, calls %d", got, err, calls)
			}
			encoded, _ := json.Marshal(got)
			if strings.Contains(string(encoded), "must-not-import") {
				t.Fatal("environment imported")
			}
		})
	}
	for _, body := range []string{`[]`, `[{"id":"prj_a","name":"A","framework":null}]`, `{"projects":[],"pagination":{"next":null}}`} {
		c := clientFixture(t, func(*http.Request) (*http.Response, error) { return clientResponse(body), nil })
		got, err := c.Projects(context.Background(), clientTestToken, "")
		if err != nil || got == nil {
			t.Fatalf("bare/envelope result: %v", err)
		}
	}
}

func TestClientProjectsFailsClosedOnIncompletePagination(t *testing.T) {
	for _, test := range []struct{ name, body, code string }{
		{"missing list", `{"pagination":{"next":null}}`, "invalid_response"},
		{"missing pagination", `{"projects":[]}`, "invalid_response"},
		{"missing cursor", `{"projects":[],"pagination":{}}`, "invalid_response"},
		{"null", `null`, "invalid_response"},
		{"negative cursor", `{"projects":[],"pagination":{"next":-1}}`, "invalid_response"},
		{"fractional cursor", `{"projects":[],"pagination":{"next":1.1}}`, "invalid_response"},
		{"array cursor", `{"projects":[],"pagination":{"next":[]}}`, "invalid_response"},
		{"empty with next", `{"projects":[],"pagination":{"next":"NEXT"}}`, "invalid_response"},
		{"control cursor", `{"projects":[{"id":"prj_a"}],"pagination":{"next":"next\nsecret"}}`, "invalid_response"},
		{"duplicate cursor", `{"projects":[{"id":"prj_a"}],"pagination":{"next":"NEXT"}}`, "invalid_response"},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			c := clientFixture(t, func(*http.Request) (*http.Response, error) { calls++; return clientResponse(test.body), nil })
			got, err := c.Projects(context.Background(), clientTestToken, "")
			clientRequireCode(t, err, test.code)
			if got != nil || calls > 2 {
				t.Fatal("partial or unbounded project inventory")
			}
		})
	}
}

func clientProjectPage(start, count int, next any, bare bool) string {
	projects := make([]map[string]string, 0, count)
	for i := 0; i < count; i++ {
		projects = append(projects, map[string]string{"id": fmt.Sprintf("prj_%d", start+i), "name": "project"})
	}
	var value any = map[string]any{"projects": projects, "pagination": map[string]any{"next": next}}
	if bare {
		value = projects
	}
	data, _ := json.Marshal(value)
	return string(data)
}

func TestClientProjectBounds(t *testing.T) {
	for _, test := range []struct {
		name string
		body func(int) string
		max  int
	}{
		{"bare array completeness", func(int) string { return clientProjectPage(0, 100, nil, true) }, 1},
		{"oversized page", func(int) string { return clientProjectPage(0, 101, nil, false) }, 1},
		{"inventory count", func(call int) string { return clientProjectPage((call-1)*100, 100, fmt.Sprintf("next%d", call), false) }, 11},
		{"page count", func(call int) string { return clientProjectPage(0, 1, fmt.Sprintf("next%d", call), false) }, maxPages},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			c := clientFixture(t, func(*http.Request) (*http.Response, error) { calls++; return clientResponse(test.body(calls)), nil })
			got, err := c.Projects(context.Background(), clientTestToken, "")
			clientRequireCode(t, err, "inventory_limit")
			if got != nil || calls != test.max {
				t.Fatalf("bounded calls %d, expected %d", calls, test.max)
			}
		})
	}
}

func TestClientDeploymentProjectionAndPagination(t *testing.T) {
	calls := 0
	c := clientFixture(t, func(req *http.Request) (*http.Response, error) {
		calls++
		q := req.URL.Query()
		if req.URL.Path != "/v7/deployments" || q.Get("projectId") != "prj_a" || q.Get("teamId") != "team_123" || q.Has("from") {
			t.Fatal("incorrect deployment query")
		}
		if calls == 1 {
			if q.Get("limit") != "100" || q.Has("until") {
				t.Fatal("incorrect first page")
			}
			return clientResponse(`{"deployments":[{"uid":"dpl_a","projectId":"prj_a","name":"A","url":"a.vercel.app","created":1700000000123,"state":"READY","readyState":"CANCELED","target":null,"meta":{"githubCommitRef":"feature/new","githubCommitSha":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","sensitive":"not-projected"}}],"pagination":{"next":1690000000000}}`), nil
		}
		if q.Get("until") != "1690000000000" || q.Get("limit") != "99" {
			t.Fatal("deployment cursor or remaining bound incorrect")
		}
		return clientResponse(`{"deployments":[{"id":"dpl_b","name":"B","url":"https://b.vercel.app/","createdAt":1690000000000,"state":"ERROR","target":"production","customEnvironment":{"slug":"qa"},"meta":{"gitlabCommitRef":"main","gitlabCommitSha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}],"pagination":{"next":null}}`), nil
	})
	got, err := c.Deployments(context.Background(), clientTestToken, "team_123", "prj_a")
	if err != nil || len(got) != 2 || calls != 2 {
		t.Fatalf("deployments: %#v %v", got, err)
	}
	if got[0].State != "CANCELED" || got[0].Target != "preview" || got[0].URL != "https://a.vercel.app" || got[0].GitBranch != "feature/new" || got[0].GitCommitSHA != strings.Repeat("a", 40) || !got[0].CreatedAt.Equal(time.UnixMilli(1700000000123)) {
		t.Fatalf("first deployment: %#v", got[0])
	}
	if got[1].ProjectID != "prj_a" || got[1].State != "ERROR" || got[1].Target != "qa" || got[1].GitBranch != "main" {
		t.Fatalf("second deployment: %#v", got[1])
	}
	data, _ := json.Marshal(got)
	if strings.Contains(string(data), "not-projected") || !strings.Contains(string(data), `"gitCommitSHA"`) {
		t.Fatal("unexpected projected JSON")
	}
}

func clientDeploymentPage(start, count int, next any) string {
	items := make([]map[string]any, 0, count)
	for i := 0; i < count; i++ {
		items = append(items, map[string]any{"uid": fmt.Sprintf("dpl_%d", start+i), "projectId": "prj_a", "created": 1700000000000 - int64(start+i), "state": "READY"})
	}
	data, _ := json.Marshal(map[string]any{"deployments": items, "pagination": map[string]any{"next": next}})
	return string(data)
}

func TestClientDeploymentRecentWindow(t *testing.T) {
	calls := 0
	c := clientFixture(t, func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return clientResponse(clientDeploymentPage(0, 60, 1690000000000)), nil
		}
		if calls > 2 {
			t.Fatal("fetched beyond recent history window")
		}
		if req.URL.Query().Get("limit") != "40" {
			t.Fatal("remaining window not requested")
		}
		return clientResponse(clientDeploymentPage(60, 40, 1680000000000)), nil
	})
	got, err := c.Deployments(context.Background(), clientTestToken, "", "prj_a")
	if err != nil || len(got) != RecentDeploymentLimit || calls != 2 {
		t.Fatalf("window %d, %v", len(got), err)
	}
}

func TestClientDeploymentRejectsInvalidScopeAndPagination(t *testing.T) {
	for _, test := range []struct{ name, body string }{
		{"different project", `{"deployments":[{"uid":"dpl_a","projectId":"prj_wrong","created":1}],"pagination":{"next":null}}`},
		{"conflicting identity", `{"deployments":[{"id":"dpl_b","uid":"dpl_a","created":1}],"pagination":{"next":null}}`},
		{"missing timestamp", `{"deployments":[{"uid":"dpl_a"}],"pagination":{"next":null}}`},
		{"missing list", `{"pagination":{"next":null}}`},
		{"missing pagination", `{"deployments":[]}`},
		{"invalid timestamp cursor", `{"deployments":[],"pagination":{"next":"TOKEN"}}`},
		{"cycle", `{"deployments":[{"uid":"dpl_a","created":1}],"pagination":{"next":1}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			c := clientFixture(t, func(*http.Request) (*http.Response, error) { calls++; return clientResponse(test.body), nil })
			got, err := c.Deployments(context.Background(), clientTestToken, "", "prj_a")
			clientRequireCode(t, err, "invalid_response")
			if got != nil || calls > 2 {
				t.Fatal("partial or unbounded deployments")
			}
		})
	}
	c := clientFixture(t, func(*http.Request) (*http.Response, error) {
		return clientResponse(clientDeploymentPage(0, 101, nil)), nil
	})
	_, err := c.Deployments(context.Background(), clientTestToken, "", "prj_a")
	clientRequireCode(t, err, "inventory_limit")
}

type clientTrackedBody struct {
	io.Reader
	closed bool
}

func (b *clientTrackedBody) Close() error { b.closed = true; return nil }

func TestClientHTTPFailuresNeverExposeProviderBody(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 429, 500, 503, 302} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			body := &clientTrackedBody{Reader: strings.NewReader(`{"error":{"message":"` + clientTestToken + ` private-response"}}`)}
			calls := 0
			c := clientFixture(t, func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": []string{"123"}, "Location": []string{"https://attacker.invalid/"}}, Body: body}, nil
			})
			_, err := c.Projects(context.Background(), clientTestToken, "")
			var actual *APIError
			if !errors.As(err, &actual) {
				t.Fatalf("not APIError: %v", err)
			}
			clientRequireCode(t, err, actual.Code)
			if actual.StatusCode != status || actual.RetryAfter != 123*time.Second || calls != 1 || !body.closed {
				t.Fatal("status, backoff, redirect bound or cleanup failed")
			}
		})
	}
}

func TestClientMalformedLargeAndTransportErrorsAreSafe(t *testing.T) {
	for _, test := range []struct{ name, body, code string }{
		{"malformed", `{"private-response":"` + clientTestToken + `"`, "invalid_response"},
		{"oversized", strings.Repeat(" ", maxBodyBytes+1), "inventory_limit"},
		{"trailing JSON", `{"projects":[],"pagination":{"next":null}} {}`, "invalid_response"},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := clientFixture(t, func(*http.Request) (*http.Response, error) { return clientResponse(test.body), nil })
			_, err := c.Projects(context.Background(), clientTestToken, "")
			clientRequireCode(t, err, test.code)
		})
	}
	c := clientFixture(t, func(*http.Request) (*http.Response, error) {
		return nil, errors.New("private-response " + clientTestToken)
	})
	_, err := c.Verify(context.Background(), clientTestToken, "")
	clientRequireCode(t, err, "transport")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.Verify(ctx, clientTestToken, "")
	clientRequireCode(t, err, "canceled")
}

func TestClientRetryAfterBounds(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		value string
		want  time.Duration
	}{
		{"60", time.Minute}, {"-10", 0}, {"0", 0}, {"invalid", 0}, {"99999999999", 24 * time.Hour},
		{now.Add(2 * time.Minute).Format(http.TimeFormat), 2 * time.Minute},
		{now.Add(-time.Minute).Format(http.TimeFormat), 0},
		{now.Add(48 * time.Hour).Format(http.TimeFormat), 24 * time.Hour},
	} {
		if got := retryDelay(test.value, now); got != test.want {
			t.Fatalf("retry delay %q = %v", test.value, got)
		}
	}
}

func TestClientRateLimitResetFallback(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		value string
		want  time.Duration
	}{
		{strconv.FormatInt(now.Unix()+300, 10), 5 * time.Minute},
		{strconv.FormatInt(now.Unix()-300, 10), 0},
		{"invalid", 0}, {"9223372036854775807", 24 * time.Hour},
	} {
		if got := resetDelay(test.value, now); got != test.want {
			t.Fatalf("reset delay = %v; want %v", got, test.want)
		}
	}
	for _, status := range []int{429, 500} {
		c := clientFixture(t, func(*http.Request) (*http.Response, error) {
			r := clientResponse("private-response " + clientTestToken)
			r.StatusCode = status
			r.Header.Set("Retry-After", "30")
			r.Header.Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Unix()+600, 10))
			return r, nil
		})
		_, err := c.Verify(context.Background(), clientTestToken, "")
		var apiError *APIError
		if !errors.As(err, &apiError) {
			t.Fatal("missing typed error")
		}
		if status == 429 && (apiError.RetryAfter < 599*time.Second || apiError.RetryAfter > 600*time.Second) {
			t.Fatalf("rate limit reset ignored: %v", apiError.RetryAfter)
		}
		if status == 500 && apiError.RetryAfter != 30*time.Second {
			t.Fatal("unrelated response used rate limit reset")
		}
	}
}

func TestClientHonorsCallerDeadline(t *testing.T) {
	c := clientFixture(t, func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := c.Projects(ctx, clientTestToken, "")
	clientRequireCode(t, err, "timeout")
}

func TestClientSafeDeploymentLinks(t *testing.T) {
	for _, value := range []string{"a.vercel.app", "https://a.vercel.app", "https://app.acme.com/path"} {
		if got := deploymentURL(value); got == "" {
			t.Errorf("valid provider URL omitted: %s", value)
		}
	}
	for _, value := range []string{"javascript:alert(1)", "http://a.vercel.app", "//attacker.com", "https://user:password@a.vercel.app", "https://a.vercel.app:443", "https://127.0.0.1", "https://[::1]", "https://169.254.169.254/latest/meta-data", "https://localhost", "https://app.local", "https://app.internal", "https://a.vercel.app?token=secret", "https://a.vercel.app#token", "https://a.vercel.app\\@attacker.com", "https://a.vercel.app%2f@attacker.com", "https://a.vercel.app\n", "https://0177.0.0.1", "https://0x7f.0.0.0x1", "https://bad_host.com", "https://bad-.com", "https://a.com.", "https://a.com/#fragment"} {
		if got := deploymentURL(value); got != "" {
			t.Errorf("unsafe provider URL accepted: %q", value)
		}
	}
}

func TestClientDeploymentStateAndGitMetadata(t *testing.T) {
	for _, state := range []string{"READY", "ERROR", "CANCELED", "BLOCKED", "DELETED", "BUILDING", "QUEUED", "INITIALIZING"} {
		if deploymentState(strings.ToLower(state)) != state {
			t.Fatal("state lost")
		}
	}
	if deploymentState("private-response") != "UNKNOWN" {
		t.Fatal("unknown provider state leaked")
	}
	for _, prefix := range []string{"githubCommit", "gitlabCommit", "bitbucketCommit", "gitCommit"} {
		b, s := gitMetadata(map[string]string{prefix + "Ref": "main", prefix + "Sha": strings.Repeat("b", 40)})
		if b != "main" || s != strings.Repeat("b", 40) {
			t.Fatal("git metadata lost")
		}
	}
	b, s := gitMetadata(map[string]string{"githubCommitRef": "feature\nunsafe", "githubCommitSha": "not-a-sha"})
	if b != "" || s != "" {
		t.Fatal("invalid git metadata accepted")
	}
}

func TestClientCursorCannotChangeOriginOrQueryScope(t *testing.T) {
	cursor := "https://attacker.invalid/?teamId=team_other&token=fake"
	calls := 0
	c := clientFixture(t, func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return clientResponse(clientProjectPage(0, 1, cursor, false)), nil
		}
		if req.URL.Query().Get("from") != cursor || req.URL.Query().Get("teamId") != "team_123" {
			t.Fatal("cursor escaped query value")
		}
		return clientResponse(clientProjectPage(1, 1, nil, false)), nil
	})
	_, err := c.Projects(context.Background(), clientTestToken, "team_123")
	if err != nil {
		t.Fatal(err)
	}
}
