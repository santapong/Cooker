package neon

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type connectionResourcesFixture struct {
	*connectionAPIFixture
	calls   int
	after   func()
	err     error
	foreign bool
	partial bool
}

func (f *connectionResourcesFixture) Databases(_ context.Context, key, project, branch string) (*DatabaseInventory, error) {
	f.calls++
	if key != connectionFixtureKey {
		return nil, errors.New("wrong synthetic credential")
	}
	if f.err != nil {
		return nil, f.err
	}
	if f.after != nil {
		f.after()
	}
	p := project
	if f.foreign {
		p = "project-two"
	}
	return &DatabaseInventory{ProjectID: project, BranchID: branch, Complete: !f.partial, ObservedAt: time.Now().UTC(), Databases: []Database{{ID: "123", ProjectID: p, BranchID: branch, Name: "customer data"}}}, nil
}
func (f *connectionResourcesFixture) Endpoints(_ context.Context, key, project string) (*EndpointInventory, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if f.after != nil {
		f.after()
	}
	return &EndpointInventory{ProjectID: project, Complete: !f.partial, ObservedAt: time.Now().UTC(), Endpoints: []Endpoint{{ID: "ep-compute", ProjectID: project, BranchID: "br-one", Type: "read_write", CurrentState: "active", ComputeReleaseVersion: "v1.2"}}}, nil
}
func connectionResourcesSetup(t *testing.T) (*ConnectionService, *connectionResourcesFixture, *Connection) {
	t.Helper()
	s, api, c := connectionCoreFixture(t)
	f := &connectionResourcesFixture{connectionAPIFixture: api}
	s.API = f
	observed, err := s.Sync(context.Background(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	return s, f, observed
}

func TestConnectionResourcesSelectedScopeAndSafeMetadata(t *testing.T) {
	s, _, c := connectionResourcesSetup(t)
	databases, err := s.ConnectionDatabases(context.Background(), c.ID, c.Version, "project-one", "br-one")
	if err != nil || len(databases.Databases) != 1 || databases.Databases[0].Name != "customer data" || databases.Databases[0].ID != "123" {
		t.Fatal("safe database metadata missing", err)
	}
	endpoints, err := s.ConnectionEndpoints(context.Background(), c.ID, c.Version, "project-one")
	if err != nil || endpoints.Endpoints[0].BranchID != "br-one" || endpoints.Endpoints[0].CurrentState != "active" {
		t.Fatal("safe endpoint metadata missing", err)
	}
	data, _ := json.Marshal([]any{databases, endpoints})
	for _, forbidden := range []string{connectionFixtureKey, "password", "connectionString", "hostname", "healthy"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatal("metadata fabricated/exposed a capability")
		}
	}
}

func TestConnectionResourcesRejectVersionSelectionAndPauseBeforeProvider(t *testing.T) {
	s, f, c := connectionResourcesSetup(t)
	for _, tc := range []struct {
		version               int
		project, branch, code string
	}{
		{c.Version + 1, "project-one", "br-one", "conflict"}, {c.Version, "project-two", "br-one", "scope_mismatch"}, {c.Version, "project-one", "br-missing", "scope_mismatch"},
	} {
		_, err := s.ConnectionDatabases(context.Background(), c.ID, tc.version, tc.project, tc.branch)
		requireConnectionCode(t, err, tc.code)
	}
	if f.calls != 0 {
		t.Fatal("rejected scope contacted provider")
	}
	no := false
	paused, err := s.Update(context.Background(), c.ID, UpdateConnectionRequest{Version: c.Version, Enabled: &no})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ConnectionEndpoints(context.Background(), c.ID, paused.Version, "project-one")
	requireConnectionCode(t, err, "paused")
	if f.calls != 0 {
		t.Fatal("paused connection contacted provider")
	}
}

func TestConnectionResourcesChangedAuthorityCancellationAndFailuresCannotPublish(t *testing.T) {
	s, f, c := connectionResourcesSetup(t)
	no := false
	f.after = func() {
		if _, err := s.Update(context.Background(), c.ID, UpdateConnectionRequest{Version: c.Version, Enabled: &no}); err != nil {
			t.Fatal(err)
		}
	}
	_, err := s.ConnectionDatabases(context.Background(), c.ID, c.Version, "project-one", "br-one")
	requireConnectionCode(t, err, "conflict")
	s, f, c = connectionResourcesSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	f.after = cancel
	_, err = s.ConnectionDatabases(ctx, c.ID, c.Version, "project-one", "br-one")
	requireConnectionCode(t, err, "canceled")
	s, f, c = connectionResourcesSetup(t)
	f.err = errors.New("private-provider-canary " + connectionFixtureKey)
	_, err = s.ConnectionEndpoints(context.Background(), c.ID, c.Version, "project-one")
	requireConnectionCode(t, err, "provider_unavailable")
	f.err = nil
	f.foreign = true
	_, err = s.ConnectionDatabases(context.Background(), c.ID, c.Version, "project-one", "br-one")
	requireConnectionCode(t, err, "scope_mismatch")
	f.foreign = false
	f.partial = true
	_, err = s.ConnectionEndpoints(context.Background(), c.ID, c.Version, "project-one")
	requireConnectionCode(t, err, "invalid_response")
}

type cancelResourceReadStore struct {
	ConnectionStore
	reads  int
	cancel context.CancelFunc
}

func (s *cancelResourceReadStore) Get(ctx context.Context, id string) (*ConnectionRecord, error) {
	r, err := s.ConnectionStore.Get(ctx, id)
	s.reads++
	if s.reads == 2 {
		s.cancel()
	}
	return r, err
}
func TestConnectionResourcesCancellationDuringFinalStoreReadCannotPublish(t *testing.T) {
	for _, database := range []bool{true, false} {
		s, _, c := connectionResourcesSetup(t)
		ctx, cancel := context.WithCancel(context.Background())
		s.Store = &cancelResourceReadStore{ConnectionStore: s.Store, cancel: cancel}
		var err error
		if database {
			_, err = s.ConnectionDatabases(ctx, c.ID, c.Version, "project-one", "br-one")
		} else {
			_, err = s.ConnectionEndpoints(ctx, c.ID, c.Version, "project-one")
		}
		requireConnectionCode(t, err, "canceled")
		cancel()
	}
}
