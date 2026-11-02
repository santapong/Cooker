package vercel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEcosystemIntegrationClientExactScopedReadAndSafeProjection(t *testing.T) {
	for _, team := range []string{"", "team_exact"} {
		t.Run("scope_"+team, func(t *testing.T) {
			calls := 0
			c := clientFixture(t, func(req *http.Request) (*http.Response, error) {
				calls++
				want := "view=account"
				if team != "" {
					want = "teamId=" + team + "&view=account"
				}
				if req.URL.Path != "/v1/integrations/configurations" || req.URL.RawQuery != want {
					t.Fatalf("unexpected installation read: %s", req.URL)
				}
				deadline, ok := req.Context().Deadline()
				if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 15*time.Second {
					t.Fatal("installation request lacks its 15-second bound")
				}
				return clientResponse(`[{"id":"icfg_neon","integrationId":"int_neon","slug":"neon","installationType":"marketplace","status":"ready","integration":{"name":"Neon","icon":"https://untrusted.invalid/icon"},"scopes":["read-write:env"],"env":{"DATABASE_URL":"ecosystem-secret-canary"},"metadata":{"password":"ecosystem-secret-canary"},"resources":[{"id":"ecosystem-secret-canary"}],"billing":{"token":"ecosystem-secret-canary"},"userId":"user_creator","url":"https://untrusted.invalid/ecosystem-secret-canary"}]`), nil
			})
			items, err := c.Integrations(context.Background(), clientTestToken, team)
			if err != nil || calls != 1 || len(items) != 1 {
				t.Fatalf("installation inventory = %#v, %v; calls=%d", items, err, calls)
			}
			item := items[0]
			if item.ID != "icfg_neon" || item.IntegrationID != "int_neon" || item.Name != "Neon" || item.InstallationType != "marketplace" || item.ProviderStatus != "ready" || item.ProjectAccess != "all_projects" || len(item.ProjectIDs) != 0 {
				t.Fatalf("incorrect installation projection: %#v", item)
			}
			encoded, err := json.Marshal(items)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{"ecosystem-secret-canary", "scopes", "DATABASE_URL", "metadata", "resources", "billing", "user_creator", "untrusted.invalid", "icon"} {
				if strings.Contains(string(encoded), forbidden) {
					t.Fatalf("forbidden provider field serialized: %q", forbidden)
				}
			}
		})
	}
}

func TestEcosystemIntegrationClientPreservesAccessAndEnumMeaning(t *testing.T) {
	c := clientFixture(t, func(*http.Request) (*http.Response, error) {
		return clientResponse(`[{"id":"icfg_all","integrationId":"int_same"},{"id":"icfg_none","integrationId":"int_same","projects":[],"installationType":"external"},{"id":"icfg_selected","integrationId":"int_same","projects":["prj_two","prj_one","prj_two"],"installationType":"marketplace","status":"suspended"},{"id":"icfg_future","integrationId":"int_future","projects":["prj_one"],"installationType":"future-native","status":"future-status"}]`), nil
	})
	items, err := c.Integrations(context.Background(), clientTestToken, "")
	if err != nil || len(items) != 4 {
		t.Fatalf("inventory = %#v, %v", items, err)
	}
	byID := map[string]IntegrationInstallation{}
	for _, item := range items {
		byID[item.ID] = item
	}
	if byID["icfg_all"].ProjectAccess != "all_projects" || byID["icfg_all"].InstallationType != "external" || byID["icfg_all"].ProviderStatus != "unknown" || byID["icfg_none"].ProjectAccess != "selected_projects" || byID["icfg_none"].ProjectIDs == nil || len(byID["icfg_none"].ProjectIDs) != 0 {
		t.Fatal("missing access, explicit empty access, or default external meaning collapsed")
	}
	if !reflect.DeepEqual(byID["icfg_selected"].ProjectIDs, []string{"prj_one", "prj_two"}) || byID["icfg_selected"].ProviderStatus != "suspended" || byID["icfg_future"].InstallationType != "unknown" || byID["icfg_future"].ProviderStatus != "unknown" {
		t.Fatal("normalized access or unknown enum meaning changed", byID)
	}
	c = clientFixture(t, func(*http.Request) (*http.Response, error) { return clientResponse(`[]`), nil })
	items, err = c.Integrations(context.Background(), clientTestToken, "")
	if err != nil || items == nil || len(items) != 0 {
		t.Fatal("successful empty array is not authoritative empty", items, err)
	}
}

func TestEcosystemIntegrationClientRejectsAmbiguousOrCrossAccountInventory(t *testing.T) {
	for _, test := range []struct{ name, body string }{
		{"null list", `null`},
		{"envelope", `{"configurations":[]}`},
		{"malformed", `[{"id":`},
		{"invalid identity", `[{"id":"../foreign","integrationId":"int_one"}]`},
		{"missing integration identity", `[{"id":"icfg_one"}]`},
		{"duplicate conflicting identity", `[{"id":"icfg_one","integrationId":"int_one"},{"id":"icfg_one","integrationId":"int_other"}]`},
		{"explicit null access", `[{"id":"icfg_one","integrationId":"int_one","projects":null}]`},
		{"malformed access", `[{"id":"icfg_one","integrationId":"int_one","projects":"prj_one"}]`},
		{"invalid project identity", `[{"id":"icfg_one","integrationId":"int_one","projects":["../foreign"]}]`},
		{"different team", `[{"id":"icfg_one","integrationId":"int_one","teamId":"team_other"}]`},
		{"null team", `[{"id":"icfg_one","integrationId":"int_one","teamId":null}]`},
		{"different owner", `[{"id":"icfg_one","integrationId":"int_one","ownerId":"team_other"}]`},
		{"malformed type", `[{"id":"icfg_one","integrationId":"int_one","installationType":null}]`},
		{"malformed status", `[{"id":"icfg_one","integrationId":"int_one","status":{}}]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			c := clientFixture(t, func(*http.Request) (*http.Response, error) { calls++; return clientResponse(test.body), nil })
			items, err := c.Integrations(context.Background(), clientTestToken, "team_exact")
			clientRequireCode(t, err, "invalid_response")
			if items != nil || calls != 1 {
				t.Fatal("ambiguous inventory returned partial data or retried", items, calls)
			}
		})
	}
}

func TestEcosystemIntegrationClientBoundsAndSafeFailures(t *testing.T) {
	rows := make([]map[string]string, 201)
	for i := range rows {
		rows[i] = map[string]string{"id": fmt.Sprintf("icfg_%03d", i), "integrationId": "int_one"}
	}
	tooMany, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	boundary, err := json.Marshal(rows[:200])
	if err != nil {
		t.Fatal(err)
	}
	c := clientFixture(t, func(*http.Request) (*http.Response, error) { return clientResponse(string(boundary)), nil })
	if items, err := c.Integrations(context.Background(), clientTestToken, ""); err != nil || len(items) != 200 {
		t.Fatal("supported boundary inventory was rejected or truncated", len(items), err)
	}
	for _, test := range []struct {
		name, body, code string
		status           int
	}{
		{"body cap", `[]` + strings.Repeat(" ", (1<<20)-1), "inventory_limit", 200},
		{"item cap", string(tooMany), "inventory_limit", 200},
		{"trailing JSON", `[] {"private-response":"` + clientTestToken + `"}`, "invalid_response", 200},
		{"forbidden", `{"error":"private-response ` + clientTestToken + `"}`, "forbidden", 403},
		{"rate limited", `{"error":"private-response ` + clientTestToken + `"}`, "rate_limited", 429},
		{"redirect", `{"error":"private-response"}`, "redirect_blocked", 302},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			body := &clientTrackedBody{Reader: strings.NewReader(test.body)}
			c := clientFixture(t, func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: test.status, Header: http.Header{"Retry-After": []string{"300"}, "Location": []string{"https://untrusted.invalid/"}}, Body: body}, nil
			})
			items, err := c.Integrations(context.Background(), clientTestToken, "")
			apiErr := clientRequireCode(t, err, test.code)
			if items != nil || calls != 1 || !body.closed {
				t.Fatal("failed read returned partial data, retried, or left its body open")
			}
			if test.status == 429 && apiErr.RetryAfter != 5*time.Minute {
				t.Fatal("rate-limit cooldown lost", apiErr)
			}
		})
	}
}

type ecosystemIntegrationFixture struct {
	*fixtureAPI
	muIntegration sync.Mutex
	items         []IntegrationInstallation
	err           error
	calls         int
	entered       chan struct{}
	release       chan struct{}
}

func (f *ecosystemIntegrationFixture) Integrations(ctx context.Context, _, _ string) ([]IntegrationInstallation, error) {
	f.muIntegration.Lock()
	f.calls++
	items, err, entered, release := f.items, f.err, f.entered, f.release
	f.muIntegration.Unlock()
	if entered != nil {
		select {
		case entered <- struct{}{}:
		default:
		}
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return items, err
}

func ecosystemInstallation(id, access string, projects ...string) IntegrationInstallation {
	return IntegrationInstallation{ID: id, IntegrationID: "int_neon", Name: "Neon", Slug: "neon", InstallationType: "marketplace", ProviderStatus: "ready", ProjectAccess: access, ProjectIDs: projects}
}

func TestEcosystemIntegrationServiceFailureClocksAndAuthoritativeEmpty(t *testing.T) {
	ctx := context.Background()
	svc, base, c := newFixture(t, NewMemory())
	c = selectProjects(t, svc, c, "prj_one")
	api := &ecosystemIntegrationFixture{fixtureAPI: base, err: &APIError{StatusCode: 403, Message: "ecosystem-secret-canary"}}
	svc.API = api
	first, err := svc.Sync(ctx, c.ID, true)
	if err != nil || first.LastSuccessAt == nil || first.SyncError != "" || first.Integrations == nil || first.Integrations.State != "unavailable" || first.Integrations.Complete || first.Integrations.ObservedAt != nil || first.Integrations.AttemptedAt == nil || first.Integrations.ErrorCode != "forbidden" {
		t.Fatalf("first optional failure corrupted core or fabricated observation: %#v %v", first, err)
	}
	api.err = nil
	api.items = []IntegrationInstallation{ecosystemInstallation("icfg_one", "all_projects")}
	good, err := svc.Sync(ctx, c.ID, true)
	if err != nil || good.Integrations.State != "available" || !good.Integrations.Complete || good.Integrations.ObservedAt == nil || len(good.Integrations.Items) != 1 {
		t.Fatal("successful optional-only adapter not observed", good, err)
	}
	before, err := svc.Store.Get(ctx, c.ID)
	if err != nil || len(before.Snapshot.Deployments) != 1 || before.Snapshot.Projects[0].Details != nil {
		t.Fatal("integration-only adapter required DetailsAPI or lost core", before, err)
	}
	api.err = errors.New("ecosystem-secret-canary")
	stale, err := svc.Sync(ctx, c.ID, true)
	if err != nil || stale.Integrations.State != "stale" || !stale.Integrations.Complete || !stale.Integrations.ObservedAt.Equal(*good.Integrations.ObservedAt) || stale.Integrations.AttemptedAt.Before(*good.Integrations.AttemptedAt) || len(stale.Integrations.Items) != 1 || stale.LastSuccessAt.Before(*good.LastSuccessAt) {
		t.Fatal("failed optional refresh lost retained observation or core success", stale, err)
	}
	encoded, err := json.Marshal(stale)
	if err != nil || strings.Contains(string(encoded), "ecosystem-secret-canary") {
		t.Fatal("adapter error leaked into public connection", err)
	}
	api.err, api.items = nil, []IntegrationInstallation{}
	empty, err := svc.Sync(ctx, c.ID, true)
	if err != nil || empty.Integrations.State != "available" || !empty.Integrations.Complete || empty.Integrations.ErrorCode != "" || empty.Integrations.Error != "" || empty.Integrations.ObservedAt.Before(*good.Integrations.ObservedAt) || empty.Integrations.Items == nil || len(empty.Integrations.Items) != 0 || api.calls != 4 {
		t.Fatal("successful empty did not clear retained rows and warning", empty, err)
	}
}

func TestEcosystemIntegrationOptionalAPIKeepsExistingAdaptersCompatible(t *testing.T) {
	t.Run("core only", func(t *testing.T) {
		svc, _, c := newFixture(t, NewMemory())
		c = selectProjects(t, svc, c, "prj_one")
		got, err := svc.Sync(context.Background(), c.ID, true)
		if err != nil || got.LastSuccessAt == nil || got.Integrations == nil || got.Integrations.ErrorCode != "unsupported" || got.Integrations.ObservedAt != nil || got.Integrations.AttemptedAt != nil || got.Integrations.Complete {
			t.Fatal("legacy core adapter no longer works without optional API", got, err)
		}
	})
	t.Run("details only", func(t *testing.T) {
		svc, api, c := detailService(t, NewMemory(), 1)
		got, err := svc.Sync(context.Background(), c.ID, true)
		r, readErr := svc.Store.Get(context.Background(), c.ID)
		if err != nil || readErr != nil || api.calls == 0 || r.Snapshot.Projects[0].Details == nil || got.Integrations.ErrorCode != "unsupported" || got.Integrations.AttemptedAt != nil {
			t.Fatal("existing DetailsAPI adapter lost detail enrichment", got, err, readErr)
		}
	})
}

type ecosystemIntegrationDetailsFixture struct {
	*detailFixture
	integrations *ecosystemIntegrationFixture
	overlapped   bool
}

func (f *ecosystemIntegrationDetailsFixture) Integrations(ctx context.Context, token, team string) ([]IntegrationInstallation, error) {
	f.muDetails.Lock()
	f.overlapped = f.active != 0
	f.muDetails.Unlock()
	return f.integrations.Integrations(ctx, token, team)
}

func TestEcosystemIntegrationReadRunsAfterBoundedDetailWorkers(t *testing.T) {
	svc, details, c := detailService(t, NewMemory(), 5)
	details.wait = time.Millisecond
	integrations := &ecosystemIntegrationFixture{items: []IntegrationInstallation{ecosystemInstallation("icfg_one", "all_projects")}}
	api := &ecosystemIntegrationDetailsFixture{detailFixture: details, integrations: integrations}
	svc.API = api
	got, err := svc.Sync(context.Background(), c.ID, true)
	if err != nil || api.overlapped || details.maximum > 4 || details.calls == 0 || integrations.calls != 1 || got.Integrations.State != "available" || len(got.Integrations.Items[0].ProjectIDs) != 5 {
		t.Fatal("installation read overlapped detail fanout or lost optional data", got, err)
	}
}

func TestEcosystemIntegrationRateLimitSharesOptionalCooldown(t *testing.T) {
	t.Run("installation cooldown fences manual retry", func(t *testing.T) {
		svc, base, c := newFixture(t, NewMemory())
		c = selectProjects(t, svc, c, "prj_one")
		api := &ecosystemIntegrationFixture{fixtureAPI: base, err: &APIError{StatusCode: 429, RetryAfter: 5 * time.Minute, Message: "ecosystem-secret-canary"}}
		svc.API = api
		got, err := svc.Sync(context.Background(), c.ID, true)
		if err != nil || got.LastSuccessAt == nil || got.RetryAt == nil || got.RetryAt.Before(time.Now().Add(4*time.Minute)) || got.Integrations.ErrorCode != "rate_limited" || api.calls != 1 {
			t.Fatal("optional cooldown lost core or retry boundary", got, err)
		}
		if _, err := svc.Sync(context.Background(), c.ID, true); !errors.Is(err, ErrBusy) || api.calls != 1 {
			t.Fatal("manual sync bypassed shared cooldown", err)
		}
	})
	t.Run("detail cooldown skips installation read", func(t *testing.T) {
		svc, details, c := detailService(t, NewMemory(), 1)
		details.detailError = &APIError{StatusCode: 429, RetryAfter: 7 * time.Minute}
		integrations := &ecosystemIntegrationFixture{items: []IntegrationInstallation{ecosystemInstallation("icfg_one", "all_projects")}}
		svc.API = &ecosystemIntegrationDetailsFixture{detailFixture: details, integrations: integrations}
		got, err := svc.Sync(context.Background(), c.ID, true)
		if err != nil || integrations.calls != 0 || got.RetryAt == nil || got.RetryAt.Before(time.Now().Add(6*time.Minute)) || got.Integrations.ErrorCode != "rate_limited" || got.Integrations.AttemptedAt != nil || got.Integrations.ObservedAt != nil {
			t.Fatal("integration read escaped detail cooldown or fabricated attempt", got, err)
		}
	})
}

func TestEcosystemIntegrationServiceRejectsUntrustedOptionalAdapterRows(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*IntegrationInstallation)
	}{
		{"foreign account", func(item *IntegrationInstallation) { item.ownerID = "user_other" }},
		{"unrecognized access", func(item *IntegrationInstallation) { item.ProjectAccess = "database_access" }},
		{"unbounded display", func(item *IntegrationInstallation) { item.Name = strings.Repeat("x", 257) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, base, c := newFixture(t, NewMemory())
			c = selectProjects(t, svc, c, "prj_one")
			item := ecosystemInstallation("icfg_one", "all_projects")
			test.mutate(&item)
			svc.API = &ecosystemIntegrationFixture{fixtureAPI: base, items: []IntegrationInstallation{item}}
			got, err := svc.Sync(context.Background(), c.ID, true)
			if err != nil || got.LastSuccessAt == nil || got.Integrations.ErrorCode != "invalid_response" || got.Integrations.Complete || len(got.Integrations.Items) != 0 || got.Integrations.ObservedAt != nil {
				t.Fatal("optional adapter bypassed account/access boundary", got, err)
			}
		})
	}
}

func TestEcosystemIntegrationProjectionAndMemoryAreDeeplyIsolated(t *testing.T) {
	ctx := context.Background()
	svc, base, c := newFixture(t, NewMemory())
	c = selectProjects(t, svc, c, "prj_one")
	api := &ecosystemIntegrationFixture{fixtureAPI: base, items: []IntegrationInstallation{
		ecosystemInstallation("icfg_all", "all_projects", "prj_unselected"),
		ecosystemInstallation("icfg_selected", "selected_projects", "prj_one", "prj_two"),
		ecosystemInstallation("icfg_denied", "selected_projects", "prj_two"),
		ecosystemInstallation("icfg_none", "selected_projects"),
	}}
	svc.API = api
	got, err := svc.Sync(ctx, c.ID, true)
	if err != nil || len(got.Integrations.Items) != 2 || got.Integrations.Items[0].ProjectAccess != "all_projects" {
		t.Fatal("incorrect selected access projection", got, err)
	}
	for _, item := range got.Integrations.Items {
		if !reflect.DeepEqual(item.ProjectIDs, []string{"prj_one"}) {
			t.Fatal("public inventory exposed unselected access", item)
		}
	}
	originalTime := *got.Integrations.ObservedAt
	got.Integrations.Items[0].ProjectIDs[0] = "prj_mutated"
	*got.Integrations.ObservedAt = time.Time{}
	r, err := svc.Store.Get(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	r.Snapshot.Integrations.Items[0].ProjectIDs[0] = "prj_store_mutated"
	*r.Snapshot.Integrations.AttemptedAt = time.Time{}
	view, err := svc.View(ctx)
	if err != nil || view.Connections[0].Integrations.Items[0].ProjectIDs[0] != "prj_one" || !view.Connections[0].Integrations.ObservedAt.Equal(originalTime) || view.Connections[0].Integrations.AttemptedAt.IsZero() {
		t.Fatal("public/store clone mutation reached persisted observation", view, err)
	}
	view.Connections[0].Integrations.Items[0].Name = "mutated view"
	again, err := svc.View(ctx)
	if err != nil || again.Connections[0].Integrations.Items[0].Name != "Neon" || api.items[1].ProjectIDs[1] != "prj_two" {
		t.Fatal("public filtering mutated another projection or provider rows", again, err)
	}
}

func TestEcosystemIntegrationSelectionChangeFencesInflightObservation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	svc, base, c := newFixture(t, NewMemory())
	c = selectProjects(t, svc, c, "prj_one")
	api := &ecosystemIntegrationFixture{fixtureAPI: base, items: []IntegrationInstallation{ecosystemInstallation("icfg_one", "all_projects")}, entered: make(chan struct{}, 1), release: make(chan struct{})}
	defer func() {
		select {
		case <-api.release:
		default:
			close(api.release)
		}
	}()
	svc.API = api
	done := make(chan error, 1)
	go func() { _, err := svc.Sync(ctx, c.ID, true); done <- err }()
	select {
	case <-api.entered:
	case <-time.After(2 * time.Second):
		close(api.release)
		t.Fatal("installation phase did not start")
	}
	c = selectProjects(t, svc, c, "prj_two")
	close(api.release)
	select {
	case err := <-done:
		if !errors.Is(err, ErrConflict) {
			t.Fatal("old selected-project observation committed", err)
		}
	case <-ctx.Done():
		t.Fatal("installation sync did not finish after releasing fixture")
	}
	view, err := svc.View(ctx)
	if err != nil || len(view.Projects) != 0 || view.Connections[0].Integrations != nil || view.Connections[0].LastSuccessAt != nil || view.Connections[0].ProjectIDs[0] != "prj_two" {
		t.Fatal("stale installation scope survived selection fence", view, err)
	}
}

func TestEcosystemIntegrationOldSnapshotJSONRemainsUnobserved(t *testing.T) {
	var old Snapshot
	if err := json.Unmarshal([]byte(`{"projects":[{"id":"prj_one","name":"Legacy"}],"deployments":[]}`), &old); err != nil || old.Integrations != nil {
		t.Fatal("legacy snapshot gained a fabricated integration observation", old, err)
	}
	public := publicConnection(&Record{Connection: Connection{ProjectIDs: []string{"prj_one"}}, Snapshot: old}, time.Now())
	encoded, err := json.Marshal(public)
	if err != nil || public.Integrations != nil || strings.Contains(string(encoded), `"integrations"`) {
		t.Fatal("legacy public JSON compatibility changed", public, err)
	}
}

func TestEcosystemIntegrationPublicProjectionSuppressesUnconfirmedRows(t *testing.T) {
	now := time.Now().UTC()
	zero := time.Time{}
	for _, test := range []struct {
		name     string
		complete bool
		observed *time.Time
	}{
		{name: "incomplete", observed: &now},
		{name: "missing_success_clock", complete: true},
		{name: "zero_success_clock", complete: true, observed: &zero},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := ecosystemInstallation("icfg_one", "all_projects")
			item.Name = "unconfirmed-installation-canary"
			inventory := &IntegrationInventory{Items: []IntegrationInstallation{item}, ObservedAt: test.observed, AttemptedAt: &now, Complete: test.complete, State: "available", ErrorCode: "forbidden", Error: "Integration inventory is unavailable"}
			public := publicConnection(&Record{Connection: Connection{ProjectIDs: []string{"prj_one"}}, Snapshot: Snapshot{Integrations: inventory}}, now)
			encoded, err := json.Marshal(public)
			if err != nil || strings.Contains(string(encoded), item.Name) {
				t.Fatal("unconfirmed installation escaped through public connection", err)
			}
			got := public.Integrations
			if got == nil || len(got.Items) != 0 || got.ObservedAt != nil || got.Complete || got.State != "unavailable" || got.AttemptedAt == nil || !got.AttemptedAt.Equal(now) || got.ErrorCode != inventory.ErrorCode || got.Error != inventory.Error {
				t.Fatal("unconfirmed observation did not retain only its failure context", got)
			}
			if len(inventory.Items) != 1 || inventory.State != "available" || inventory.Complete != test.complete {
				t.Fatal("public normalization mutated persisted input", inventory)
			}
		})
	}
}
