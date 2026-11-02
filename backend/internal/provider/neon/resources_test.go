package neon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func databaseResourceWire(id int64, name string) string {
	return fmt.Sprintf(`{"id":%d,"branch_id":"br-fixture","name":%q,"owner_name":"synthetic-private-role-canary","created_at":"2026-10-26T00:00:00Z","updated_at":"2026-10-26T00:00:00Z","password":"synthetic-private-password-canary","connection_uri":"postgres://synthetic-private-uri-canary","metadata":{"token":"synthetic-private-metadata-canary"}}`, id, name)
}

func endpointResourceWire(id, kind, state string) string {
	return fmt.Sprintf(`{"id":%q,"project_id":"blue-bird-123","branch_id":"br-fixture","type":%q,"current_state":%q,"compute_release_version":"2026.10.26-build1","host":"synthetic-private-host-canary","proxy_host":"synthetic-private-proxy-canary","settings":{"password":"synthetic-private-settings-canary"},"connection_uris":["postgres://synthetic-private-uri-canary"],"roles":[{"password":"synthetic-private-password-canary"}]}`, id, kind, state)
}

// Normalize typed nils so an unsuccessful method cannot appear to have returned
// partial metadata through this common fixture helper.
func resourceFixtureRead(ctx context.Context, c *Client, kind string) (any, error) {
	if kind == "databases" {
		out, err := c.Databases(ctx, fixtureKey, "blue-bird-123", "br-fixture")
		if out == nil {
			return nil, err
		}
		return out, err
	}
	out, err := c.Endpoints(ctx, fixtureKey, "blue-bird-123")
	if out == nil {
		return nil, err
	}
	return out, err
}

func TestResourceDatabasesExactScopeIdentityAndReadableName(t *testing.T) {
	calls := 0
	c := fixtureClient(t, func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Path != "/api/v2/projects/blue-bird-123/branches/br-fixture/databases" || req.URL.RawQuery != "" {
			t.Fatal("invented database path or pagination", req.URL.Path, req.URL.RawQuery)
		}
		deadline, ok := req.Context().Deadline()
		if !ok || time.Until(deadline) > requestTimeout {
			t.Fatal("resource request lacks existing deadline")
		}
		return response(`{"databases":[` + databaseResourceWire(9007199254740993, " database name ") + `]}`), nil
	})
	out, err := c.Databases(context.Background(), fixtureKey, "blue-bird-123", "br-fixture")
	if err != nil || out == nil || !out.Complete || out.ObservedAt.IsZero() || calls != 1 || len(out.Databases) != 1 {
		t.Fatal("database observation unavailable", out, err, calls)
	}
	database := out.Databases[0]
	if out.ProjectID != "blue-bird-123" || out.BranchID != "br-fixture" || database.ProjectID != out.ProjectID || database.BranchID != out.BranchID || database.ID != "9007199254740993" || database.Name != " database name " {
		t.Fatal("database scope, integer precision or exact readable name changed", out)
	}
	encoded, err := json.Marshal(out)
	if err != nil || strings.Contains(string(encoded), "canary") || strings.Contains(string(encoded), fixtureKey) || strings.Contains(string(encoded), "owner_name") || strings.Contains(string(encoded), "connection_uri") {
		t.Fatal("excluded database fields escaped safe projection", string(encoded), err)
	}
}

func TestResourceEndpointsSafeProjectionAndUnknownLifecycle(t *testing.T) {
	calls := 0
	c := fixtureClient(t, func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Path != "/api/v2/projects/blue-bird-123/endpoints" || req.URL.RawQuery != "" {
			t.Fatal("invented endpoint path or query")
		}
		return response(`{"endpoints":[` + endpointResourceWire("ep-first", "read_write", "active") + `,` + endpointResourceWire("ep-second", "synthetic-private-type-canary", "synthetic-private-state-canary") + `]}`), nil
	})
	out, err := c.Endpoints(context.Background(), fixtureKey, "blue-bird-123")
	if err != nil || out == nil || !out.Complete || out.ObservedAt.IsZero() || calls != 1 || len(out.Endpoints) != 2 {
		t.Fatal("endpoint observation unavailable", out, err)
	}
	first, second := out.Endpoints[0], out.Endpoints[1]
	if first.ID != "ep-first" || first.ProjectID != "blue-bird-123" || first.BranchID != "br-fixture" || first.Type != "read_write" || first.CurrentState != "active" || first.ComputeReleaseVersion != "2026.10.26-build1" || second.Type != "unknown" || second.CurrentState != "unknown" {
		t.Fatal("endpoint identity or lifecycle normalization changed", out)
	}
	encoded, err := json.Marshal(out)
	if err != nil || strings.Contains(string(encoded), "canary") || strings.Contains(string(encoded), fixtureKey) || strings.Contains(string(encoded), "host") || strings.Contains(string(encoded), "connection_uris") || strings.Contains(string(encoded), "healthy") || strings.Contains(string(encoded), "postgresVersion") {
		t.Fatal("endpoint projection leaked fields or invented health/version", string(encoded), err)
	}
}

func TestResourceFullListsAllowOneThousandWithoutInventedPagination(t *testing.T) {
	for _, kind := range []string{"databases", "endpoints"} {
		for _, count := range []int{0, pageSize + 1, maxItems, maxItems + 1} {
			t.Run(kind+"/"+strconv.Itoa(count), func(t *testing.T) {
				rows := make([]string, 0, count)
				for i := 0; i < count; i++ {
					row := databaseResourceWire(int64(i+1), "database-"+strconv.Itoa(i))
					if kind == "endpoints" {
						row = endpointResourceWire("ep-fixture-"+strconv.Itoa(i), "read_only", "idle")
					}
					rows = append(rows, row)
				}
				calls := 0
				c := fixtureClient(t, func(req *http.Request) (*http.Response, error) {
					calls++
					if req.URL.RawQuery != "" {
						t.Fatal("full-list request added unsupported pagination")
					}
					return response(`{"` + kind + `":[` + strings.Join(rows, ",") + `]}`), nil
				})
				out, err := resourceFixtureRead(context.Background(), c, kind)
				if count > maxItems {
					requireCode(t, err, "inventory_limit")
					if out != nil {
						t.Fatal("oversized list returned partial metadata")
					}
				} else if err != nil || out == nil {
					t.Fatal("documented full list was treated as a 100-row page", count, out, err)
				}
				if calls != 1 {
					t.Fatal("resource list retried or invented another page", calls)
				}
			})
		}
	}
}

func TestResourceUnexpectedPagingAndPartialMarkersNeverClaimComplete(t *testing.T) {
	for _, kind := range []string{"databases", "endpoints"} {
		for _, marker := range []string{`"pagination":{"cursor":"secret-canary"}`, `"pagination":{}`, `"Pagination":{"next":"secret-canary"}`, `"next_cursor":"secret-canary"`, `"nextCursor":"secret-canary"`, `"has_more":false`, `"unavailable_database_ids":[]`, `"unavailable_endpoint_ids":["secret-canary"]`, `"truncated":true`, `"complete":false`, `"error":"secret-canary"`} {
			t.Run(kind+"/"+marker, func(t *testing.T) {
				calls := 0
				c := fixtureClient(t, func(*http.Request) (*http.Response, error) {
					calls++
					return response(`{"` + kind + `":[],` + marker + `}`), nil
				})
				out, err := resourceFixtureRead(context.Background(), c, kind)
				requireCode(t, err, "incomplete_coverage")
				if out != nil || calls != 1 {
					t.Fatal("partial marker became complete data or another request", out, calls)
				}
			})
		}
	}
}

func TestResourceInvalidInputNeverReachesTransport(t *testing.T) {
	calls := 0
	c := fixtureClient(t, func(*http.Request) (*http.Response, error) { calls++; return response(`{}`), nil })
	for _, project := range []string{"", "../private", "project%2fother", "Project", strings.Repeat("p", 61)} {
		_, err := c.Endpoints(context.Background(), fixtureKey, project)
		requireCode(t, err, "invalid_input")
		_, err = c.Databases(context.Background(), fixtureKey, project, "br-fixture")
		requireCode(t, err, "invalid_input")
	}
	for _, branch := range []string{"", "br-", "not-a-branch", "br-a/other", "br-a%2fother"} {
		_, err := c.Databases(context.Background(), fixtureKey, "blue-bird-123", branch)
		requireCode(t, err, "invalid_input")
	}
	for _, key := range []string{"", "short", "synthetic\nheader-canary"} {
		_, err := c.Endpoints(context.Background(), key, "blue-bird-123")
		requireCode(t, err, "invalid_input")
		_, err = c.Databases(context.Background(), key, "blue-bird-123", "br-fixture")
		requireCode(t, err, "invalid_input")
	}
	if calls != 0 {
		t.Fatal("invalid IDs or keys reached provider transport", calls)
	}
}

func TestResourceMalformedForeignOrDuplicateRowsReturnNoInventory(t *testing.T) {
	database := databaseResourceWire(1, "database")
	endpoint := endpointResourceWire("ep-first", "read_write", "active")
	cases := []struct{ kind, body, code string }{
		{"databases", `{"databases":null}`, "invalid_response"},
		{"endpoints", `{"endpoints":{}}`, "invalid_response"},
		{"databases", `{"databases":[]}{}`, "invalid_response"},
		{"endpoints", `{"endpoints":[],"Endpoints":[]}`, "invalid_response"},
		{"databases", `{"databases":[` + strings.Replace(database, `"id":1`, `"id":null`, 1) + `]}`, "invalid_response"},
		{"databases", `{"databases":[` + strings.Replace(database, `"id":1`, `"id":"1"`, 1) + `]}`, "invalid_response"},
		{"databases", `{"databases":[` + strings.Replace(database, `"id":1`, `"id":9223372036854775808`, 1) + `]}`, "invalid_response"},
		{"databases", `{"databases":[` + strings.Replace(database, `br-fixture`, `br-other`, 1) + `]}`, "scope_mismatch"},
		{"databases", `{"databases":[` + strings.Replace(database, `"name":`, `"project_id":"foreign-project","name":`, 1) + `]}`, "scope_mismatch"},
		{"databases", `{"databases":[` + database + `,` + databaseResourceWire(1, "other-name") + `]}`, "incomplete_coverage"},
		{"databases", `{"databases":[` + database + `,` + databaseResourceWire(2, "database") + `]}`, "incomplete_coverage"},
		{"endpoints", `{"endpoints":[` + strings.Replace(endpoint, `blue-bird-123`, `foreign-project`, 1) + `]}`, "scope_mismatch"},
		{"endpoints", `{"endpoints":[` + strings.Replace(endpoint, `ep-first`, `not-an-endpoint`, 1) + `]}`, "invalid_response"},
		{"endpoints", `{"endpoints":[` + strings.Replace(endpoint, `br-fixture`, `not-a-branch`, 1) + `]}`, "invalid_response"},
		{"endpoints", `{"endpoints":[` + endpoint + `,` + strings.Replace(endpoint, `br-fixture`, `br-other`, 1) + `]}`, "incomplete_coverage"},
		{"endpoints", `{"endpoints":[` + strings.Replace(endpoint, `"current_state":"active"`, `"current_state":false`, 1) + `]}`, "invalid_response"},
	}
	for i, tt := range cases {
		t.Run(tt.kind+"/"+strconv.Itoa(i), func(t *testing.T) {
			c := fixtureClient(t, func(*http.Request) (*http.Response, error) { return response(tt.body), nil })
			out, err := resourceFixtureRead(context.Background(), c, tt.kind)
			requireCode(t, err, tt.code)
			if out != nil {
				t.Fatal("bad response returned partial metadata", out)
			}
		})
	}
}

func TestResourceErrorsUseExistingSafeCodesWithoutRetry(t *testing.T) {
	for _, kind := range []string{"databases", "endpoints"} {
		for _, tt := range []struct {
			status int
			code   string
		}{{401, "unauthorized"}, {403, "forbidden"}, {404, "not_found"}, {429, "rate_limited"}, {500, "provider_error"}, {302, "provider_error"}} {
			t.Run(kind+"/"+strconv.Itoa(tt.status), func(t *testing.T) {
				calls := 0
				c := fixtureClient(t, func(*http.Request) (*http.Response, error) {
					calls++
					r := response(`{"message":"synthetic-private-error-canary"}`)
					r.StatusCode = tt.status
					r.Header.Set("Location", "https://elsewhere.example/secret-canary")
					r.Header.Set("Retry-After", "42")
					return r, nil
				})
				out, err := resourceFixtureRead(context.Background(), c, kind)
				requireCode(t, err, tt.code)
				if out != nil || calls != 1 {
					t.Fatal("provider error returned partial inventory or retried", out, calls)
				}
			})
		}
	}
}

func TestResourceCancellationNeverPublishesSuccessfulObservation(t *testing.T) {
	for _, kind := range []string{"databases", "endpoints"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			c := fixtureClient(t, func(*http.Request) (*http.Response, error) { calls++; return response(`{"` + kind + `":[]}`), nil })
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			out, err := resourceFixtureRead(ctx, c, kind)
			requireCode(t, err, "canceled")
			if out != nil || calls != 0 {
				t.Fatal("pre-canceled resource read reached provider")
			}
			ctx, cancel = context.WithCancel(context.Background())
			defer cancel()
			body := &cancelingMetadataBody{reader: strings.NewReader(`{"` + kind + `":[]}`), cancel: cancel}
			c = fixtureClient(t, func(*http.Request) (*http.Response, error) { calls++; r := response(""); r.Body = body; return r, nil })
			out, err = resourceFixtureRead(ctx, c, kind)
			requireCode(t, err, "canceled")
			if out != nil || calls != 1 || !body.closed {
				t.Fatal("canceled successful body published metadata or leaked body", out, calls, body.closed)
			}
		})
	}
}

func TestResourceTransportAndBodyBoundsReturnNoObservation(t *testing.T) {
	for _, kind := range []string{"databases", "endpoints"} {
		for _, tt := range []struct {
			body      string
			transport bool
			code      string
		}{{"", true, "transport"}, {strings.Repeat("x", maxBodyBytes+1), false, "response_limit"}} {
			t.Run(kind+"/"+tt.code, func(t *testing.T) {
				calls := 0
				c := fixtureClient(t, func(*http.Request) (*http.Response, error) {
					calls++
					if tt.transport {
						return nil, errors.New("synthetic-private-transport-canary")
					}
					return response(tt.body), nil
				})
				out, err := resourceFixtureRead(context.Background(), c, kind)
				requireCode(t, err, tt.code)
				if out != nil || calls != 1 {
					t.Fatal("transport/size failure published data or retried", out, calls)
				}
			})
		}
	}
}

func TestResourceComputeReleaseIsOptionalSafeMetadataOnly(t *testing.T) {
	for _, release := range []string{"https://private.example/secret-canary", strings.Repeat("x", 129), "version\nsecret-canary"} {
		wire := strings.Replace(endpointResourceWire("ep-first", "read_only", "init"), `"2026.10.26-build1"`, strconv.Quote(release), 1)
		c := fixtureClient(t, func(*http.Request) (*http.Response, error) { return response(`{"endpoints":[` + wire + `]}`), nil })
		out, err := c.Endpoints(context.Background(), fixtureKey, "blue-bird-123")
		if err != nil || out == nil || len(out.Endpoints) != 1 || out.Endpoints[0].ComputeReleaseVersion != "" {
			t.Fatal("optional release became authority or arbitrary metadata", out, err)
		}
		encoded, err := json.Marshal(out)
		if err != nil || strings.Contains(string(encoded), "canary") || strings.Contains(string(encoded), "computeReleaseVersion") {
			t.Fatal("unrecognized optional release escaped projection", string(encoded), err)
		}
	}
}
