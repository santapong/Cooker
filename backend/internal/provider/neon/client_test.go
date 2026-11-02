package neon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

const fixtureKey = "synthetic-neon-api-key-not-a-real-credential"

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func fixtureClient(t *testing.T, fn func(*http.Request) (*http.Response, error)) *Client {
	t.Helper()
	c := NewClient()
	c.http.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.Scheme != "https" || req.URL.Host != "console.neon.tech" || req.URL.User != nil || !strings.HasPrefix(req.URL.Path, "/api/v2/") {
			t.Fatal("metadata request left fixed read-only origin")
		}
		if req.Header.Get("Authorization") != "Bearer "+fixtureKey || req.Header.Get("Cookie") != "" || strings.Contains(req.URL.String(), fixtureKey) {
			t.Fatal("unsafe authentication transport")
		}
		if req.Body != nil {
			t.Fatal("GET metadata request has a body")
		}
		return fn(req)
	})
	return c
}

func response(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}

func projectWire(id string) string {
	return fmt.Sprintf(`{"id":%q,"owner_id":"org-fixture","org_id":"org-fixture","name":"Cooker database","region_id":"aws-us-east-2","created_at":"2026-10-25T00:00:00Z","updated_at":"2026-10-25T00:01:00Z","proxy_host":"synthetic-private-host-canary","settings":{"secret":"synthetic-private-settings-canary"},"connection_uris":["postgres://synthetic-private-uri-canary"],"roles":[{"password":"synthetic-private-password-canary"}]}`, id)
}

func branchWire(id string) string {
	return fmt.Sprintf(`{"id":%q,"project_id":"blue-bird-123","name":"main","created_at":"2026-10-25T00:00:00Z","updated_at":"2026-10-25T00:01:00Z","current_state":"ready","default":true,"endpoints":[{"host":"synthetic-private-host-canary"}],"credentials":{"password":"synthetic-private-password-canary"}}`, id)
}

func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	var safe *Error
	if !errors.As(err, &safe) || safe.Code != code {
		t.Fatalf("wrong safe error: %v; want %s", err, code)
	}
	if strings.Contains(err.Error(), "canary") || strings.Contains(err.Error(), fixtureKey) {
		t.Fatal("provider or credential details entered error")
	}
}

func TestProjectsBoundedPaginationAndSafeMetadataProjection(t *testing.T) {
	calls := 0
	cursor := "2026-10-25T00:00:00+00:00"
	c := fixtureClient(t, func(req *http.Request) (*http.Response, error) {
		calls++
		query := req.URL.Query()
		if req.URL.Path != "/api/v2/projects" || query.Get("org_id") != "org-fixture" || query.Get("limit") != "100" || query.Get("timeout") != "10000" || query.Get("recoverable") != "false" {
			t.Fatal("wrong project scope/query")
		}
		if calls == 1 {
			if query.Has("cursor") {
				t.Fatal("invented first cursor")
			}
			return response(`{"projects":[` + projectWire("blue-bird-123") + `],"unavailable_project_ids":[],"pagination":{"cursor":` + strconv.Quote(cursor) + `},"integrations":{"private":"synthetic-private-integration-canary"}}`), nil
		}
		if calls != 2 || query.Get("cursor") != cursor {
			t.Fatal("project cursor was changed or retried")
		}
		return response(`{"projects":[` + projectWire("green-tree-456") + `]}`), nil
	})
	got, err := c.Projects(context.Background(), fixtureKey, "org-fixture")
	if err != nil || calls != 2 || got == nil || !got.Complete || got.ObservedAt.IsZero() || len(got.Projects) != 2 || got.OrganizationID != "org-fixture" {
		t.Fatal("complete project inventory unavailable", got, err, calls)
	}
	data, err := json.Marshal(got)
	if err != nil || strings.Contains(string(data), "canary") || strings.Contains(string(data), fixtureKey) || strings.Contains(string(data), "proxy_host") || strings.Contains(string(data), "connection_uris") {
		t.Fatal("metadata projection leaked excluded fields", string(data), err)
	}
}

func TestBranchesUseNextCursorAndPreserveOnlyMetadata(t *testing.T) {
	calls := 0
	cursor := "opaque+/cursor=="
	c := fixtureClient(t, func(req *http.Request) (*http.Response, error) {
		calls++
		query := req.URL.Query()
		if req.URL.Path != "/api/v2/projects/blue-bird-123/branches" || query.Get("sort_by") != "created_at" || query.Get("sort_order") != "asc" || query.Get("include_deleted") != "false" || query.Get("limit") != "100" {
			t.Fatal("wrong branch scope/query")
		}
		if calls == 1 {
			return response(`{"branches":[` + branchWire("br-first-123") + `],"pagination":{"next":` + strconv.Quote(cursor) + `,"sort_by":"created_at","sort_order":"asc"}}`), nil
		}
		if calls != 2 || query.Get("cursor") != cursor {
			t.Fatal("opaque branch cursor was changed or retried")
		}
		branch := strings.Replace(branchWire("br-child-456"), `"name":"main"`, `"name":"preview-label","parent_id":"br-first-123"`, 1)
		return response(`{"branches":[` + branch + `],"pagination":{"sort_by":"created_at","sort_order":"asc"}}`), nil
	})
	got, err := c.Branches(context.Background(), fixtureKey, "blue-bird-123")
	if err != nil || got == nil || !got.Complete || got.ObservedAt.IsZero() || calls != 2 || len(got.Branches) != 2 || got.Branches[1].ParentID != "br-first-123" {
		t.Fatal("branch metadata incomplete", got, err, calls)
	}
	data, err := json.Marshal(got)
	if err != nil || strings.Contains(string(data), "canary") || strings.Contains(string(data), "environment") || strings.Contains(string(data), "health") || strings.Contains(string(data), "ready") || strings.Contains(string(data), `"default"`) {
		t.Fatal("branch metadata invented bindings/health or leaked details", string(data), err)
	}
}

func TestMetadataRejectsInvalidPathScopeAndKeyBeforeNetwork(t *testing.T) {
	calls := 0
	c := fixtureClient(t, func(*http.Request) (*http.Response, error) { calls++; return response(`{}`), nil })
	for _, id := range []string{"", "../projects", "org-one?secret=x", "ORG", strings.Repeat("a", 61), "https://elsewhere.example", "project%2fother"} {
		_, err := c.Projects(context.Background(), fixtureKey, id)
		requireCode(t, err, "invalid_input")
		_, err = c.Branches(context.Background(), fixtureKey, id)
		requireCode(t, err, "invalid_input")
	}
	for _, key := range []string{"", "short", "private\r\nheader", "has space", strings.Repeat("k", 4097)} {
		_, err := c.Projects(context.Background(), key, "org-fixture")
		requireCode(t, err, "invalid_input")
	}
	if calls != 0 {
		t.Fatal("invalid metadata input reached provider")
	}
}

func TestMetadataErrorsAreSafeNoRetryAndRedirectIsRejected(t *testing.T) {
	for _, tt := range []struct {
		status int
		code   string
	}{{401, "unauthorized"}, {403, "forbidden"}, {404, "not_found"}, {429, "rate_limited"}, {500, "provider_error"}, {503, "provider_error"}, {302, "provider_error"}} {
		t.Run(strconv.Itoa(tt.status), func(t *testing.T) {
			calls := 0
			c := fixtureClient(t, func(*http.Request) (*http.Response, error) {
				calls++
				r := response(`{"message":"synthetic-private-body-canary","request_id":"synthetic-private-request-canary"}`)
				r.StatusCode = tt.status
				r.Header.Set("Location", "https://elsewhere.example/secret")
				r.Header.Set("Retry-After", "42")
				return r, nil
			})
			got, err := c.Projects(context.Background(), fixtureKey, "org-fixture")
			requireCode(t, err, tt.code)
			if got != nil || calls != 1 {
				t.Fatal("failure returned complete data, retried or followed redirect", got, calls)
			}
		})
	}
}

func TestMetadataTransportCancellationAndResponseBounds(t *testing.T) {
	calls := 0
	c := fixtureClient(t, func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("synthetic-private-transport-canary")
	})
	got, err := c.Projects(context.Background(), fixtureKey, "org-fixture")
	requireCode(t, err, "transport")
	if got != nil || calls != 1 {
		t.Fatal("transport retry or partial success")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.Projects(ctx, fixtureKey, "org-fixture")
	requireCode(t, err, "canceled")
	if calls != 1 {
		t.Fatal("already-canceled request reached transport")
	}
	c = fixtureClient(t, func(*http.Request) (*http.Response, error) { return response(strings.Repeat("x", maxBodyBytes+1)), nil })
	_, err = c.Projects(context.Background(), fixtureKey, "org-fixture")
	requireCode(t, err, "response_limit")
}

type cancelingMetadataBody struct {
	reader   io.Reader
	cancel   context.CancelFunc
	canceled bool
	closed   bool
}

func (b *cancelingMetadataBody) Read(p []byte) (int, error) {
	if !b.canceled {
		b.canceled = true
		b.cancel()
	}
	return b.reader.Read(p)
}

func (b *cancelingMetadataBody) Close() error { b.closed = true; return nil }

func TestMetadataCancellationDuringSuccessfulBodyReadCannotPublishObservation(t *testing.T) {
	for _, kind := range []string{"projects", "branches"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			payload := `{"projects":[` + projectWire("blue-bird-123") + `]}`
			if kind == "branches" {
				payload = `{"branches":[` + branchWire("br-first-123") + `]}`
			}
			body := &cancelingMetadataBody{reader: strings.NewReader(payload), cancel: cancel}
			calls := 0
			c := fixtureClient(t, func(*http.Request) (*http.Response, error) {
				calls++
				r := response("")
				r.Body = body
				return r, nil
			})
			if kind == "projects" {
				got, err := c.Projects(ctx, fixtureKey, "org-fixture")
				requireCode(t, err, "canceled")
				if got != nil {
					t.Fatal("canceled project read published an observation", got)
				}
			} else {
				got, err := c.Branches(ctx, fixtureKey, "blue-bird-123")
				requireCode(t, err, "canceled")
				if got != nil {
					t.Fatal("canceled branch read published an observation", got)
				}
			}
			if calls != 1 || !body.closed {
				t.Fatal("canceled successful body was retried or left open", calls, body.closed)
			}
		})
	}
}

func TestProjectPartialCoverageNeverReturnsAuthoritativeEmpty(t *testing.T) {
	for _, body := range []string{
		`{"projects":[],"unavailable_project_ids":["missing-project-123"]}`,
		`{"projects":[` + projectWire("blue-bird-123") + `],"unavailable_project_ids":["missing-project-123"]}`,
		`{"projects":[],"unavailable":["missing-project-123"]}`,
	} {
		c := fixtureClient(t, func(*http.Request) (*http.Response, error) { return response(body), nil })
		got, err := c.Projects(context.Background(), fixtureKey, "org-fixture")
		requireCode(t, err, "incomplete_coverage")
		if got != nil {
			t.Fatal("provider timeout returned complete or partial inventory", got)
		}
	}
	c := fixtureClient(t, func(*http.Request) (*http.Response, error) {
		return response(`{"projects":[],"unavailable_project_ids":[]}`), nil
	})
	got, err := c.Projects(context.Background(), fixtureKey, "org-fixture")
	if err != nil || got == nil || !got.Complete || len(got.Projects) != 0 {
		t.Fatal("verified empty inventory lost", got, err)
	}
}

func TestMetadataRejectsMalformedResponsesAndForeignScope(t *testing.T) {
	project := `{"projects":[` + projectWire("blue-bird-123") + `]}`
	for _, body := range []string{
		`null`, `[]`, `{"projects":null}`, `{"projects":[],"Projects":[]}`, `{"projects":[],"unavailable_project_ids":null}`,
		strings.Replace(project, `"owner_id":"org-fixture"`, `"owner_id":"org-other"`, 1),
		strings.Replace(project, `"org_id":"org-fixture"`, `"org_id":"org-other"`, 1),
		strings.Replace(project, `"created_at":"2026-10-25T00:00:00Z"`, `"created_at":"not-a-date"`, 1),
		strings.Replace(project, `"id":"blue-bird-123"`, `"id":"../foreign"`, 1),
		`{"projects":[],"pagination":{"next":"wrong-schema"}}`,
		`{"projects":[],"pagination":{"cursor":""}}`,
		`{"projects":[],"pagination":null}`,
	} {
		c := fixtureClient(t, func(*http.Request) (*http.Response, error) { return response(body), nil })
		if got, err := c.Projects(context.Background(), fixtureKey, "org-fixture"); err == nil || got != nil {
			t.Fatal("malformed or foreign projects accepted", got, err)
		}
	}
	branch := `{"branches":[` + branchWire("br-first-123") + `]}`
	for _, body := range []string{
		`{"branches":null}`, `{"branches":[],"pagination":{"cursor":"wrong-schema"}}`,
		strings.Replace(branch, `"project_id":"blue-bird-123"`, `"project_id":"foreign-project-123"`, 1),
		strings.Replace(branch, `"id":"br-first-123"`, `"id":"not-a-branch"`, 1),
		strings.Replace(branch, `"name":"main"`, `"name":"main","parent_id":"br-first-123"`, 1),
		`{"branches":[],"pagination":{"sort_order":"desc"}}`,
	} {
		c := fixtureClient(t, func(*http.Request) (*http.Response, error) { return response(body), nil })
		if got, err := c.Branches(context.Background(), fixtureKey, "blue-bird-123"); err == nil || got != nil {
			t.Fatal("malformed or foreign branches accepted", got, err)
		}
	}
}

func TestMetadataDuplicateCursorAndInventoryLimitsNeverTruncate(t *testing.T) {
	for _, mode := range []string{"duplicate-project", "repeated-cursor", "page-cap"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			c := fixtureClient(t, func(*http.Request) (*http.Response, error) {
				calls++
				id, cursor := fmt.Sprintf("project-%d", calls), fmt.Sprintf("cursor-%d", calls)
				if mode == "duplicate-project" {
					id = "same-project"
				}
				if mode == "repeated-cursor" {
					cursor = "same-cursor"
				}
				return response(`{"projects":[` + projectWire(id) + `],"pagination":{"cursor":` + strconv.Quote(cursor) + `}}`), nil
			})
			got, err := c.Projects(context.Background(), fixtureKey, "org-fixture")
			if got != nil || err == nil || calls > maxPages {
				t.Fatal("bounded incomplete campaign returned data", got, err, calls)
			}
			if mode == "page-cap" {
				requireCode(t, err, "inventory_limit")
				if calls != maxPages {
					t.Fatal("unexpected page limit", calls)
				}
			} else {
				requireCode(t, err, "incomplete_coverage")
			}
		})
	}
	items := []string{}
	for index := 0; index < pageSize; index++ {
		items = append(items, projectWire(fmt.Sprintf("project-%d", index)))
	}
	c := fixtureClient(t, func(*http.Request) (*http.Response, error) {
		return response(`{"projects":[` + strings.Join(items, ",") + `]}`), nil
	})
	got, err := c.Projects(context.Background(), fixtureKey, "org-fixture")
	requireCode(t, err, "incomplete_coverage")
	if got != nil {
		t.Fatal("full page without continuation proof was called complete")
	}
}
