package vercel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type detailFixture struct {
	*fixtureAPI
	projects               int
	muDetails              sync.Mutex
	current                string
	detailError            error
	mismatch               bool
	active, maximum, calls int
	wait                   time.Duration
	entered                chan struct{}
	release                chan struct{}
}

func (f *detailFixture) Projects(context.Context, string, string) ([]Project, error) {
	n := f.projects
	if n == 0 {
		n = 1
	}
	out := []Project{}
	for i := 0; i < n; i++ {
		out = append(out, Project{ID: fmt.Sprintf("prj_%02d", i), Name: "Same display name"})
	}
	return out, nil
}
func (f *detailFixture) Deployments(_ context.Context, _, _, project string) ([]Deployment, error) {
	f.fixtureAPI.mu.Lock()
	failure := f.fixtureAPI.failure
	f.fixtureAPI.mu.Unlock()
	if failure != nil {
		return nil, failure
	}
	return []Deployment{{ID: "dpl_latest_" + project, ProjectID: project, Target: "preview", State: "READY", CreatedAt: time.Now().UTC()}, {ID: "dpl_not_current_" + project, ProjectID: project, Target: "production", State: "READY", CreatedAt: time.Now().UTC()}}, nil
}
func (f *detailFixture) begin(ctx context.Context) error {
	f.muDetails.Lock()
	f.active++
	f.calls++
	if f.active > f.maximum {
		f.maximum = f.active
	}
	delay, err, entered, release := f.wait, f.detailError, f.entered, f.release
	f.muDetails.Unlock()
	defer func() { f.muDetails.Lock(); f.active--; f.muDetails.Unlock() }()
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
			return ctx.Err()
		}
	}
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}
func (f *detailFixture) ProjectDetails(ctx context.Context, _, _, id string) (*ProjectDetails, error) {
	if err := f.begin(ctx); err != nil {
		return nil, err
	}
	f.muDetails.Lock()
	current := f.current
	f.muDetails.Unlock()
	if current == "" {
		current = "current"
	}
	return &ProjectDetails{ProductionDeploymentID: "dpl_" + current + "_" + id, Services: []ServiceDefinition{{Name: "web"}}, ServicesKnown: true, ObservedAt: time.Now().UTC(), ownerID: "user_fixture"}, nil
}
func (f *detailFixture) DeploymentDetails(ctx context.Context, _, _, id string) (*Deployment, error) {
	if err := f.begin(ctx); err != nil {
		return nil, err
	}
	f.muDetails.Lock()
	mismatch := f.mismatch
	f.muDetails.Unlock()
	project := id[len(id)-6:]
	if mismatch {
		project = "prj_foreign"
	}
	return &Deployment{ID: id, ProjectID: project, Name: "Observed", Target: "production", State: "READY", Details: &DeploymentDetails{Aliases: []string{"https://app.vercel.app"}, Services: []ServiceDefinition{{Name: "web", Runtime: "node"}}, ServicesKnown: true, ObservedAt: time.Now().UTC(), ownerID: "user_fixture"}}, nil
}
func detailService(t *testing.T, store Store, projects int) (*Service, *detailFixture, *Connection) {
	t.Helper()
	svc, base, c := newFixture(t, store)
	api := &detailFixture{fixtureAPI: base, projects: projects}
	svc.API = api
	ids := []string{}
	for i := 0; i < projects; i++ {
		ids = append(ids, fmt.Sprintf("prj_%02d", i))
	}
	c = selectProjects(t, svc, c, ids...)
	return svc, api, c
}
func TestEnrichmentBoundedConcurrencyAndExactCurrentProduction(t *testing.T) {
	svc, api, c := detailService(t, NewMemory(), 20)
	api.wait = 3 * time.Millisecond
	if _, err := svc.Sync(context.Background(), c.ID, true); err != nil {
		t.Fatal(err)
	}
	r, err := svc.Store.Get(context.Background(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if api.maximum > 4 || api.maximum < 2 || api.calls != 60 {
		t.Fatalf("concurrency/count max=%d calls=%d", api.maximum, api.calls)
	}
	if len(r.Snapshot.Deployments) != 40 {
		t.Fatal("current production changed bounded recent history")
	}
	for _, p := range r.Snapshot.Projects {
		if p.Details == nil || p.Details.ProductionDeploymentID != "dpl_current_"+p.ID || p.Details.ProductionDeployment == nil || p.Details.ProductionDeployment.ID != p.Details.ProductionDeploymentID || !p.Details.ProductionDeployment.Details.ServicesKnown {
			t.Fatalf("not exact production: %+v", p)
		}
	}
	// Memory reads and writes must deeply isolate nested observations.
	r.Snapshot.Projects[0].Details.ProductionDeployment.Details.Services[0].Name = "modified"
	again, _ := svc.Store.Get(context.Background(), c.ID)
	if again.Snapshot.Projects[0].Details.ProductionDeployment.Details.Services[0].Name == "modified" {
		t.Fatal("memory store shares nested details")
	}
}
func TestOptionalFailureRetainsOwnObservationAndCoreHistory(t *testing.T) {
	svc, api, c := detailService(t, NewMemory(), 1)
	ctx := context.Background()
	if _, err := svc.Sync(ctx, c.ID, true); err != nil {
		t.Fatal(err)
	}
	before, _ := svc.Store.Get(ctx, c.ID)
	api.detailError = errors.New("secret provider response must not escape")
	if _, err := svc.Sync(ctx, c.ID, true); err != nil {
		t.Fatal(err)
	}
	after, _ := svc.Store.Get(ctx, c.ID)
	p := after.Snapshot.Projects[0].Details
	if p.Error != detailsFailure || !p.ObservedAt.Equal(before.Snapshot.Projects[0].Details.ObservedAt) || len(p.Services) != 1 || len(after.Snapshot.Deployments) != 2 || !after.LastSuccessAt.After(*before.LastSuccessAt) {
		t.Fatal("optional failure lost successful core or prior details")
	}
	data, _ := json.Marshal(after.Snapshot)
	if string(data) == "" || strings.Contains(string(data), "secret provider") {
		t.Fatal("raw optional error leaked")
	}
	api.detailError = nil
	api.mismatch = true
	api.current = "changed"
	if _, err := svc.Sync(ctx, c.ID, true); err != nil {
		t.Fatal(err)
	}
	after, _ = svc.Store.Get(ctx, c.ID)
	d := after.Snapshot.Projects[0].Details.ProductionDeployment
	if d.ID != "dpl_changed_prj_00" || d.Details.ServicesKnown || len(d.Details.Services) != 0 || !d.Details.ObservedAt.IsZero() || d.Details.Error == "" {
		t.Fatal("different deployment reused previous observations")
	}
}
func TestOptionalRateLimitStopsFanoutAndPersistsCooldown(t *testing.T) {
	svc, api, c := detailService(t, NewMemory(), 20)
	api.detailError = &APIError{StatusCode: 429, RetryAfter: 5 * time.Minute, Message: "unsafe provider body"}
	api.wait = time.Millisecond
	receipt, err := svc.Sync(context.Background(), c.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if api.calls > 4 || receipt.LastSuccessAt == nil || receipt.RetryAt == nil || receipt.RetryAt.Before(time.Now().Add(4*time.Minute)) {
		t.Fatalf("rate-limit fanout/cooldown calls=%d receipt=%+v", api.calls, receipt)
	}
	r, _ := svc.Store.Get(context.Background(), c.ID)
	if len(r.Snapshot.Deployments) != 40 {
		t.Fatal("optional rate limit discarded core")
	}
	calls := api.calls
	if _, err = svc.Sync(context.Background(), c.ID, true); !errors.Is(err, ErrBusy) || calls != api.calls {
		t.Fatal("manual sync bypassed optional backoff")
	}
}
func TestEnrichmentFencedWhenConfigurationChanges(t *testing.T) {
	svc, api, c := detailService(t, NewMemory(), 1)
	api.entered = make(chan struct{}, 4)
	api.release = make(chan struct{})
	done := make(chan error, 1)
	go func() { _, err := svc.Sync(context.Background(), c.ID, true); done <- err }()
	<-api.entered
	if _, err := svc.Sync(context.Background(), c.ID, true); !errors.Is(err, ErrBusy) {
		t.Fatal("parallel sync allowed")
	}
	name := "Changed"
	if _, err := svc.Update(context.Background(), c.ID, UpdateRequest{Version: c.Version, Name: &name}); err != nil {
		t.Fatal(err)
	}
	close(api.release)
	if err := <-done; !errors.Is(err, ErrConflict) {
		t.Fatalf("stale details committed: %v", err)
	}
	r, _ := svc.Store.Get(context.Background(), c.ID)
	if len(r.Snapshot.Projects) != 0 {
		t.Fatal("stale snapshot persisted")
	}
}

func TestOptionalDeadlinePreservesCoreAndCoreFailureRetainsAllDetails(t *testing.T) {
	svc, api, c := detailService(t, NewMemory(), 1)
	if _, err := svc.Sync(context.Background(), c.ID, true); err != nil {
		t.Fatal(err)
	}
	before, _ := svc.Store.Get(context.Background(), c.ID)
	api.wait = time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := svc.Sync(ctx, c.ID, true); err != nil {
		t.Fatal(err)
	}
	after, _ := svc.Store.Get(context.Background(), c.ID)
	if len(after.Snapshot.Deployments) != 2 || after.Snapshot.Projects[0].Details.Error == "" || !after.Snapshot.Projects[0].Details.ObservedAt.Equal(before.Snapshot.Projects[0].Details.ObservedAt) {
		t.Fatal("optional deadline discarded core or observation")
	}
	api.fixtureAPI.failure = errors.New("core failure")
	calls := api.calls
	if _, err := svc.Sync(context.Background(), c.ID, true); err != nil {
		t.Fatal(err)
	}
	failed, _ := svc.Store.Get(context.Background(), c.ID)
	if api.calls != calls || !failed.LastSuccessAt.Equal(*after.LastSuccessAt) || failed.Snapshot.Projects[0].Details.ProductionDeployment == nil {
		t.Fatal("core failure replaced enriched snapshot")
	}
}

func TestCurrentProductionOutsideRecentHistoryKeepsGitSource(t *testing.T) {
	svc, _, c := newFixture(t, NewMemory())
	ctx := context.Background()
	sha := strings.Repeat("c", 40)
	svc.API = clientFixture(t, func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/v2/user":
			return clientResponse(`{"user":{"id":"user_fixture","name":"Fixture"}}`), nil
		case "/v10/projects":
			return clientResponse(`{"projects":[{"id":"prj_one","name":"App"}],"pagination":{"next":null}}`), nil
		case "/v7/deployments":
			return clientResponse(`{"deployments":[{"id":"dpl_preview","projectId":"prj_one","target":null,"createdAt":1700000000200}],"pagination":{"next":null}}`), nil
		case "/v9/projects/prj_one":
			return clientResponse(`{"id":"prj_one","accountId":"user_fixture","targets":{"production":{"id":"dpl_old"}}}`), nil
		case "/v13/deployments/dpl_old":
			return clientResponse(`{"id":"dpl_old","projectId":"prj_one","ownerId":"user_fixture","target":"production","createdAt":1700000000100,"gitSource":{"type":"github","ref":"release","sha":"` + sha + `"}}`), nil
		case "/v13/deployments/dpl_preview":
			return clientResponse(`{"id":"dpl_preview","projectId":"prj_one","target":null}`), nil
		case "/v2/deployments/dpl_old/aliases":
			return clientResponse(`{"aliases":[]}`), nil
		default:
			t.Fatalf("unexpected endpoint: %s", req.URL.Path)
			return nil, nil
		}
	})
	// The transport fixture checks its own canary token; replacing this fixture
	// credential uses the same verified identity without live provider access.
	token := clientTestToken
	var err error
	c, err = svc.Update(ctx, c.ID, UpdateRequest{Version: c.Version, Token: &token})
	if err != nil {
		t.Fatal(err)
	}
	c = selectProjects(t, svc, c, "prj_one")
	if _, err = svc.Sync(ctx, c.ID, true); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Store.Get(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	prod := got.Snapshot.Projects[0].Details.ProductionDeployment
	if len(got.Snapshot.Deployments) != 1 || prod == nil || prod.ID != "dpl_old" || prod.GitBranch != "release" || prod.GitCommitSHA != sha {
		t.Fatal("current production outside history lost explicit Git source")
	}
}
