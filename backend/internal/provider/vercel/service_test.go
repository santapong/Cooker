package vercel

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/crypto"
)

const testToken = "fixture-token-do-not-use"

type fixtureAPI struct {
	mu          sync.Mutex
	identity    string
	failure     error
	entered     chan struct{}
	release     chan struct{}
	deployCalls int
}

func (f *fixtureAPI) Verify(_ context.Context, _ string, team string) (Identity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := f.identity
	if id == "" {
		id = "user_fixture"
	}
	scope := "personal"
	if team != "" {
		scope = "team"
		id = team
	}
	return Identity{ID: id, Name: "Fixture account", Scope: scope}, nil
}
func (f *fixtureAPI) Projects(context.Context, string, string) ([]Project, error) {
	return []Project{{ID: "prj_one", Name: "one", Framework: "nextjs"}, {ID: "prj_two", Name: "two"}}, nil
}
func (f *fixtureAPI) Deployments(ctx context.Context, _ string, _ string, project string) ([]Deployment, error) {
	f.mu.Lock()
	f.deployCalls++
	failure, entered, release := f.failure, f.entered, f.release
	f.mu.Unlock()
	if entered != nil {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if failure != nil {
		return nil, failure
	}
	return []Deployment{{ID: "dpl_" + project, ProjectID: project, Name: "Fixture", State: "READY", URL: "https://fixture.vercel.app", Target: "production", CreatedAt: time.Now().UTC()}}, nil
}
func newFixture(t *testing.T, st Store) (*Service, *fixtureAPI, *Connection) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	codec, err := crypto.NewCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	api := &fixtureAPI{}
	svc := &Service{Store: st, Codec: codec, API: api}
	c, err := svc.Create(context.Background(), CreateRequest{Name: "My Vercel", Token: testToken})
	if err != nil {
		t.Fatal(err)
	}
	return svc, api, c
}
func selectProjects(t *testing.T, s *Service, c *Connection, ids ...string) *Connection {
	t.Helper()
	got, err := s.Update(context.Background(), c.ID, UpdateRequest{Version: c.Version, ProjectIDs: &ids})
	if err != nil {
		t.Fatal(err)
	}
	return got
}
func TestConnectionScopeCredentialsAndRecentSync(t *testing.T) {
	ctx := context.Background()
	svc, api, c := newFixture(t, NewMemory())
	if _, err := svc.Sync(ctx, c.ID, true); !errors.Is(err, ErrDisabled) {
		t.Fatalf("empty selection must deny sync: %v", err)
	}
	c = selectProjects(t, svc, c, "prj_one")
	synced, err := svc.Sync(ctx, c.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if synced.LastSuccessAt == nil || synced.SyncError != "" || synced.Version != c.Version {
		t.Fatalf("bad sync receipt: %+v", synced)
	}
	view, err := svc.View(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Deployments) != 1 || view.Deployments[0].ProjectID != "prj_one" || view.Deployments[0].ConnectionID != c.ID {
		t.Fatalf("wrong scope: %+v", view)
	}
	record, err := svc.Store.Get(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(record.Credential), testToken) {
		t.Fatal("plaintext credential stored")
	}
	plain, err := svc.Codec.Open(record.Credential)
	if err != nil || string(plain) != testToken {
		t.Fatal("credential roundtrip failed")
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), testToken) || strings.Contains(string(encoded), "Credential") || strings.Contains(string(encoded), "Lease") {
		t.Fatal("public response leaked credential or lease")
	}
	svc.API = api
	api.mu.Lock()
	api.identity = "user_different"
	api.mu.Unlock()
	replacement := "rotated-fixture-token"
	if _, err = svc.Update(ctx, c.ID, UpdateRequest{Version: c.Version, Token: &replacement}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("different identity accepted: %v", err)
	}
	if _, err = svc.Update(ctx, c.ID, UpdateRequest{Version: c.Version - 1}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale edit accepted: %v", err)
	}
}
func TestSyncFailureRetainsSnapshotAndHonorsBackoff(t *testing.T) {
	ctx := context.Background()
	svc, api, c := newFixture(t, NewMemory())
	c = selectProjects(t, svc, c, "prj_one")
	good, err := svc.Sync(ctx, c.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	api.failure = &APIError{StatusCode: 429, Message: "Vercel rate limit reached", RetryAfter: 5 * time.Minute}
	failed, err := svc.Sync(ctx, c.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if failed.SyncError == "" || failed.RetryAt == nil || failed.RetryAt.Before(time.Now().Add(4*time.Minute)) || !failed.LastSuccessAt.Equal(*good.LastSuccessAt) {
		t.Fatal("failed refresh lost provenance or backoff")
	}
	if _, err = svc.Sync(ctx, c.ID, true); !errors.Is(err, ErrBusy) {
		t.Fatalf("manual refresh bypassed backoff: %v", err)
	}
	view, err := svc.View(ctx)
	if err != nil || len(view.Deployments) != 1 {
		t.Fatal("last good snapshot lost")
	}
	disabled := false
	c, err = svc.Update(ctx, c.ID, UpdateRequest{Version: c.Version, Enabled: &disabled})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Sync(ctx, c.ID, true); !errors.Is(err, ErrDisabled) {
		t.Fatal("paused connection synced")
	}
}
func TestChangingSelectionFencesInflightSyncAndDisconnect(t *testing.T) {
	ctx := context.Background()
	svc, api, c := newFixture(t, NewMemory())
	c = selectProjects(t, svc, c, "prj_one")
	api.entered = make(chan struct{})
	api.release = make(chan struct{})
	done := make(chan error, 1)
	go func() { _, err := svc.Sync(ctx, c.ID, true); done <- err }()
	<-api.entered
	if _, err := svc.Sync(ctx, c.ID, true); !errors.Is(err, ErrBusy) {
		t.Fatalf("concurrent sync not fenced: %v", err)
	}
	c = selectProjects(t, svc, c, "prj_two")
	close(api.release)
	if err := <-done; !errors.Is(err, ErrConflict) {
		t.Fatalf("stale sync committed: %v", err)
	}
	view, err := svc.View(ctx)
	if err != nil || len(view.Deployments) != 0 || view.Connections[0].LastSuccessAt != nil {
		t.Fatal("stale scope appeared in snapshot")
	}
	if err = svc.Delete(ctx, c.ID, c.Version-1); !errors.Is(err, ErrConflict) {
		t.Fatal("stale disconnect allowed")
	}
	if err = svc.Delete(ctx, c.ID, c.Version); err != nil {
		t.Fatal(err)
	}
	view, err = svc.View(ctx)
	if err != nil || len(view.Connections) != 0 {
		t.Fatal("disconnect retained connection")
	}
}
func TestUnselectedAndDuplicateProjectRejected(t *testing.T) {
	svc, _, c := newFixture(t, NewMemory())
	for _, ids := range [][]string{{"prj_foreign"}, {"prj_one", "prj_one"}} {
		if _, err := svc.Update(context.Background(), c.ID, UpdateRequest{Version: c.Version, ProjectIDs: &ids}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("bad selection accepted: %v", err)
		}
	}
}
func TestLeaseExpirationFencesOlderWorker(t *testing.T) {
	ctx := context.Background()
	svc, _, c := newFixture(t, NewMemory())
	c = selectProjects(t, svc, c, "prj_one")
	now := time.Now().UTC()
	lease, err := svc.Store.Claim(ctx, c.ID, c.Version, now, now.Add(-time.Second), true)
	if err != nil {
		t.Fatal(err)
	}
	newLease, err := svc.Store.Claim(ctx, c.ID, c.Version, now, now.Add(time.Minute), true)
	if err != nil {
		t.Fatal(err)
	}
	snap := &Snapshot{Projects: []Project{}, Deployments: []Deployment{}}
	if err = svc.Store.Finish(ctx, c.ID, c.Version, lease, snap, "", now, nil); !errors.Is(err, ErrConflict) {
		t.Fatal("expired worker committed")
	}
	if err = svc.Store.Finish(ctx, c.ID, c.Version, newLease, snap, "", now, nil); err != nil {
		t.Fatal(err)
	}
}
func TestBackgroundLoopSyncsSelectedProject(t *testing.T) {
	svc, _, c := newFixture(t, NewMemory())
	selectProjects(t, svc, c, "prj_one")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); svc.Run(ctx) }()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		view, err := svc.View(context.Background())
		if err == nil && len(view.Deployments) == 1 {
			cancel()
			<-done
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	t.Fatal("background synchronization did not run")
}

func TestReplaceUndecryptableCredentialAndPauseWithoutProviderAccess(t *testing.T) {
	svc, _, c := newFixture(t, NewMemory())
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	codec, err := crypto.NewCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	svc.Codec = codec
	disabled := false
	c, err = svc.Update(context.Background(), c.ID, UpdateRequest{Version: c.Version, Enabled: &disabled})
	if err != nil {
		t.Fatalf("cannot pause undecryptable connection: %v", err)
	}
	replacement := "replacement-fixture-token"
	c, err = svc.Update(context.Background(), c.ID, UpdateRequest{Version: c.Version, Token: &replacement})
	if err != nil {
		t.Fatal(err)
	}
	r, err := svc.Store.Get(context.Background(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Codec.Open(r.Credential)
	if err != nil || string(got) != replacement {
		t.Fatal("explicit replacement failed")
	}
}
