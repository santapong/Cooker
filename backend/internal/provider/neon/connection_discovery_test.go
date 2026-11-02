package neon

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type discoveryAPIFixture struct {
	MetadataAPI
	change   func(*ProjectInventory)
	returned *ProjectInventory
}

func (f *discoveryAPIFixture) Projects(ctx context.Context, key, org string) (*ProjectInventory, error) {
	data, err := f.MetadataAPI.Projects(ctx, key, org)
	if err != nil {
		return nil, err
	}
	if f.change != nil {
		f.change(data)
	}
	f.returned = data
	return data, nil
}

func TestConnectionDiscoveryEmptySelectionThenSelectAndSync(t *testing.T) {
	s, api, _ := connectionCoreFixture(t)
	c, err := s.Create(context.Background(), CreateConnectionRequest{Name: "Discover fixture", OrganizationID: "org-fixture", AccountMode: "unknown", APIKey: connectionFixtureKey, ProjectIDs: []string{}, AcknowledgeKeyPermissions: true})
	if err != nil {
		t.Fatal(err)
	}
	wrapper := &discoveryAPIFixture{MetadataAPI: api}
	s.API = wrapper
	before := api.projectCalls
	inventory, err := s.DiscoverProjects(context.Background(), c.ID, c.Version)
	if err != nil || inventory == nil || !inventory.Complete || inventory.OrganizationID != c.OrganizationID || len(inventory.Projects) != 2 || api.projectCalls != before+1 {
		t.Fatal("project discovery failed", inventory, err)
	}
	current, err := s.Get(context.Background(), c.ID)
	if err != nil || len(current.ProjectIDs) != 0 || current.Snapshot != nil || current.LastAttemptAt != nil || current.LastSuccessAt != nil || current.Version != c.Version {
		t.Fatal("discovery implicitly selected/refreshed", current, err)
	}
	selected := []string{inventory.Projects[1].ID}
	inventory.Projects[0].Name = "caller-mutated"
	if wrapper.returned.Projects[0].Name == "caller-mutated" {
		t.Fatal("returned inventory aliases adapter memory")
	}
	current, err = s.Update(context.Background(), c.ID, UpdateConnectionRequest{Version: c.Version, ProjectIDs: &selected})
	if err != nil {
		t.Fatal(err)
	}
	observed, err := s.Sync(context.Background(), c.ID)
	if err != nil || observed.Snapshot == nil || len(observed.Snapshot.Projects) != 1 || observed.Snapshot.Projects[0].ID != selected[0] || observed.Version != current.Version {
		t.Fatal("discovered selection did not sync", observed, err)
	}
}

func TestConnectionDiscoveryVersionPauseAndChangedConfigCannotPublish(t *testing.T) {
	s, api, c := connectionCoreFixture(t)
	before := api.projectCalls
	data, err := s.DiscoverProjects(context.Background(), c.ID, c.Version+1)
	requireConnectionCode(t, err, "conflict")
	if data != nil || api.projectCalls != before {
		t.Fatal("stale discovery contacted provider")
	}
	api.afterProjects = func() {
		name := "Renamed"
		if _, err := s.Update(context.Background(), c.ID, UpdateConnectionRequest{Version: c.Version, Name: &name}); err != nil {
			t.Fatal(err)
		}
	}
	data, err = s.DiscoverProjects(context.Background(), c.ID, c.Version)
	requireConnectionCode(t, err, "conflict")
	if data != nil {
		t.Fatal("changed generation published discovery")
	}
	api.afterProjects = nil
	current, err := s.Get(context.Background(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	no := false
	paused, err := s.Update(context.Background(), c.ID, UpdateConnectionRequest{Version: current.Version, Enabled: &no})
	if err != nil {
		t.Fatal(err)
	}
	before = api.projectCalls
	data, err = s.DiscoverProjects(context.Background(), c.ID, paused.Version)
	requireConnectionCode(t, err, "paused")
	if data != nil || api.projectCalls != before {
		t.Fatal("paused discovery contacted provider")
	}
}

type discoveryCancelStore struct {
	ConnectionStore
	reads  int
	cancel context.CancelFunc
}

func (s *discoveryCancelStore) Get(ctx context.Context, id string) (*ConnectionRecord, error) {
	r, err := s.ConnectionStore.Get(ctx, id)
	s.reads++
	if s.reads == 2 {
		s.cancel()
	}
	return r, err
}
func TestConnectionDiscoveryCancellationAndProviderFailuresCannotPublish(t *testing.T) {
	for _, stage := range []string{"provider", "final-store"} {
		s, api, c := connectionCoreFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		if stage == "provider" {
			api.afterProjects = cancel
		} else {
			s.Store = &discoveryCancelStore{ConnectionStore: s.Store, cancel: cancel}
		}
		data, err := s.DiscoverProjects(ctx, c.ID, c.Version)
		requireConnectionCode(t, err, "canceled")
		if data != nil {
			t.Fatal("canceled discovery was published", stage)
		}
		cancel()
	}
	s, api, c := connectionCoreFixture(t)
	api.projectError = errors.New("private-discovery-canary " + connectionFixtureKey)
	data, err := s.DiscoverProjects(context.Background(), c.ID, c.Version)
	requireConnectionCode(t, err, "provider_unavailable")
	if data != nil || strings.Contains(err.Error(), "canary") {
		t.Fatal("provider failure was published")
	}
}

func TestConnectionDiscoveryIncompleteForeignDuplicateAndOversizedMetadataRejected(t *testing.T) {
	for _, scenario := range []string{"partial", "foreign-org", "duplicate", "oversized", "future-clock"} {
		t.Run(scenario, func(t *testing.T) {
			s, api, c := connectionCoreFixture(t)
			s.API = &discoveryAPIFixture{MetadataAPI: api, change: func(data *ProjectInventory) {
				switch scenario {
				case "partial":
					data.Complete = false
				case "foreign-org":
					data.OrganizationID = "org-other"
				case "duplicate":
					data.Projects = append(data.Projects, data.Projects[0])
				case "oversized":
					data.Projects = make([]Project, maxItems+1)
				case "future-clock":
					data.ObservedAt = time.Now().UTC().Add(time.Hour)
				}
			}}
			data, err := s.DiscoverProjects(context.Background(), c.ID, c.Version)
			requireConnectionCode(t, err, "invalid_response")
			if data != nil {
				t.Fatal("incomplete discovery escaped", scenario)
			}
		})
	}
}
