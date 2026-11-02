package vercel

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

type adoptionPublicationFixture struct{ publication *Publication }

func (f adoptionPublicationFixture) Get(_ context.Context, id string) (*Publication, error) {
	if f.publication == nil || f.publication.ID != id {
		return nil, ErrNotFound
	}
	out := *f.publication
	return &out, nil
}

type adoptionProjectFixture struct {
	assessment *StaticProjectAssessment
	entered    chan struct{}
	release    chan struct{}
}

func (f adoptionProjectFixture) StaticProject(ctx context.Context, _ string, _ string, id string) (*StaticProjectAssessment, error) {
	if f.entered != nil {
		close(f.entered)
		select {
		case <-f.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.assessment == nil || f.assessment.ProjectID != id {
		return nil, ErrNotFound
	}
	return cloneStaticAssessment(f.assessment), nil
}

func adoptionFixture(t *testing.T) (*LandingAdoptionService, *Connection, LandingAdoptionRequest) {
	t.Helper()
	memory := NewMemory()
	svc, _, c := newFixture(t, memory)
	c = selectProjects(t, svc, c, "prj_one")
	assessment, err := staticProjectAssessment([]byte(staticAssessmentWire()))
	if err != nil {
		t.Fatal(err)
	}
	publication := &Publication{ID: "publication_prior", AccountScope: "personal", AccountID: "user_fixture", TeamID: "", ProjectID: "prj_one", DeploymentID: "dpl_prior", Target: "production", Status: "succeeded", ArtifactSHA256: strings.Repeat("a", 64)}
	s := &LandingAdoptionService{Service: svc, Store: NewMemoryLandingAdoptions(memory), Projects: adoptionProjectFixture{assessment: assessment}, Publications: adoptionPublicationFixture{publication: publication}}
	req := LandingAdoptionRequest{ConnectionID: c.ID, ConnectionVersion: c.Version, ProjectID: "prj_one", ProvenancePublicationID: publication.ID, ConfirmManualUpdates: true}
	return s, c, req
}

func TestLandingAdoptionExplicitReadOnlySetupAndCloneIsolation(t *testing.T) {
	s, _, req := adoptionFixture(t)
	a, err := s.Create(context.Background(), req, "fixture-admin")
	if err != nil || !a.Enabled || a.Version != 1 || a.DeploymentOwner != LandingAdoptionOwner || a.SourceKind != LandingAdoptionSource || len(a.PermittedActions) != 1 || a.PermittedActions[0] != LandingAdoptionAction {
		t.Fatal("bad adoption", a, err)
	}
	a.PermittedActions[0] = "delete_project"
	a.Assessment.Reasons = append(a.Assessment.Reasons, "mutated")
	got, err := s.Get(context.Background(), a.ID)
	if err != nil || got.PermittedActions[0] != LandingAdoptionAction || len(got.Assessment.Reasons) != 0 {
		t.Fatal("caller mutation changed retained authority", got, err)
	}
	data, err := json.Marshal(got)
	if err != nil || strings.Contains(string(data), testToken) || strings.Contains(string(data), "synthetic-private") {
		t.Fatal("adoption leaked raw provider data", string(data), err)
	}
	// This service has no mutation or queue dependency. Its only provider API is
	// StaticProjectReadAPI, and setup cannot call a deployment/project POST.
}

func TestLandingAdoptionRejectsMissingConfirmationSelectionAndProvenance(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*LandingAdoptionService, *LandingAdoptionRequest)
	}{
		{"confirmation", func(_ *LandingAdoptionService, r *LandingAdoptionRequest) { r.ConfirmManualUpdates = false }},
		{"unselected", func(_ *LandingAdoptionService, r *LandingAdoptionRequest) { r.ProjectID = "prj_two" }},
		{"catalog ID", func(_ *LandingAdoptionService, r *LandingAdoptionRequest) { r.ProjectID = "vapp_imported" }},
		{"connection version", func(_ *LandingAdoptionService, r *LandingAdoptionRequest) { r.ConnectionVersion-- }},
		{"publication missing", func(_ *LandingAdoptionService, r *LandingAdoptionRequest) { r.ProvenancePublicationID = "missing" }},
		{"publication not succeeded", func(s *LandingAdoptionService, _ *LandingAdoptionRequest) {
			f := s.Publications.(adoptionPublicationFixture)
			f.publication.Status = "needs_attention"
		}},
		{"publication foreign account", func(s *LandingAdoptionService, _ *LandingAdoptionRequest) {
			f := s.Publications.(adoptionPublicationFixture)
			f.publication.AccountID = "user_other"
		}},
		{"publication foreign team", func(s *LandingAdoptionService, _ *LandingAdoptionRequest) {
			f := s.Publications.(adoptionPublicationFixture)
			f.publication.TeamID = "team_other"
		}},
		{"assessment unknown", func(s *LandingAdoptionService, _ *LandingAdoptionRequest) {
			f := s.Projects.(adoptionProjectFixture)
			f.assessment.GitLinkKnown = false
		}},
		{"assessment foreign owner", func(s *LandingAdoptionService, _ *LandingAdoptionRequest) {
			f := s.Projects.(adoptionProjectFixture)
			f.assessment.OwnerID = "user_other"
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _, req := adoptionFixture(t)
			tt.mutate(s, &req)
			if _, err := s.Create(context.Background(), req, "fixture-admin"); err == nil {
				t.Fatal("invalid authority accepted")
			}
			rows, err := s.List(context.Background())
			if err != nil || len(rows) != 0 {
				t.Fatal("rejected adoption persisted", rows, err)
			}
		})
	}
}

func TestLandingAdoptionConnectionChangeDuringAssessmentFencesSave(t *testing.T) {
	s, c, req := adoptionFixture(t)
	f := s.Projects.(adoptionProjectFixture)
	f.entered, f.release = make(chan struct{}), make(chan struct{})
	s.Projects = f
	done := make(chan error, 1)
	go func() { _, err := s.Create(context.Background(), req, "fixture-admin"); done <- err }()
	<-f.entered
	enabled := false
	if _, err := s.Service.Update(context.Background(), c.ID, UpdateRequest{Version: c.Version, Enabled: &enabled}); err != nil {
		t.Fatal(err)
	}
	close(f.release)
	if err := <-done; !errors.Is(err, ErrConflict) {
		t.Fatal("changed connection committed stale authority", err)
	}
	rows, err := s.List(context.Background())
	if err != nil || len(rows) != 0 {
		t.Fatal("stale assessment persisted", rows, err)
	}
}

func TestLandingAdoptionOverlappingConnectionsHaveOneActiveWriter(t *testing.T) {
	s, _, req := adoptionFixture(t)
	other, err := s.Service.Create(context.Background(), CreateRequest{Name: "overlap", Token: testToken})
	if err != nil {
		t.Fatal(err)
	}
	other = selectProjects(t, s.Service, other, "prj_one")
	requests := []LandingAdoptionRequest{req, {ConnectionID: other.ID, ConnectionVersion: other.Version, ProjectID: req.ProjectID, ProvenancePublicationID: req.ProvenancePublicationID, ConfirmManualUpdates: true}}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, request := range requests {
		wg.Add(1)
		go func(request LandingAdoptionRequest) {
			defer wg.Done()
			_, err := s.Create(context.Background(), request, "fixture-admin")
			results <- err
		}(request)
	}
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatal("overlap admitted multiple writers", success, conflicts)
	}
}

func TestLandingAdoptionVersionedReauthorizationAndDisableAfterDisconnect(t *testing.T) {
	s, c, req := adoptionFixture(t)
	a, err := s.Create(context.Background(), req, "fixture-admin")
	if err != nil {
		t.Fatal(err)
	}
	replacement := "rotated-fixture-token"
	c, err = s.Service.Update(context.Background(), c.ID, UpdateRequest{Version: c.Version, Token: &replacement})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Update(context.Background(), a.ID, LandingAdoptionUpdate{Version: a.Version, ConnectionVersion: a.ConnectionVersion, Enabled: true, ConfirmManualUpdates: true}, "fixture-admin"); !errors.Is(err, ErrConflict) {
		t.Fatal("old token version reauthorized", err)
	}
	a, err = s.Update(context.Background(), a.ID, LandingAdoptionUpdate{Version: a.Version, ConnectionVersion: c.Version, Enabled: true, ConfirmManualUpdates: true}, "fixture-admin")
	if err != nil || a.Version != 2 || a.ConnectionVersion != c.Version {
		t.Fatal("explicit reauthorization failed", a, err)
	}
	if err = s.Service.Delete(context.Background(), c.ID, c.Version); err != nil {
		t.Fatal(err)
	}
	a, err = s.Update(context.Background(), a.ID, LandingAdoptionUpdate{Version: a.Version, Enabled: false}, "fixture-admin")
	if err != nil || a.Enabled || a.Version != 3 || a.ProvenanceDeploymentID != "dpl_prior" {
		t.Fatal("disconnect prevented local revocation or erased evidence", a, err)
	}
	if _, err = s.Update(context.Background(), a.ID, LandingAdoptionUpdate{Version: 2, Enabled: false}, "fixture-admin"); !errors.Is(err, ErrConflict) {
		t.Fatal("stale revoke accepted", err)
	}
}
