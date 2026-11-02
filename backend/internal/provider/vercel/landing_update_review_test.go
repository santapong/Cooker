package vercel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

type landingReviewReadFixture struct {
	assessment                                *StaticProjectAssessment
	deployment                                StaticDeploymentResult
	aliases                                   *DeploymentAliasDetails
	projectCalls, deploymentCalls, aliasCalls int
	projectTimeOffset, aliasTimeOffset        time.Duration
	changeProduction                          bool
	afterAliases                              func()
}

func (f *landingReviewReadFixture) StaticProject(_ context.Context, _ string, _ string, id string) (*StaticProjectAssessment, error) {
	f.projectCalls++
	if f.assessment == nil || f.assessment.ProjectID != id {
		return nil, ErrNotFound
	}
	out := cloneStaticAssessment(f.assessment)
	out.ObservedAt = time.Now().UTC().Add(f.projectTimeOffset)
	if f.changeProduction && f.projectCalls > 1 {
		out.ProductionDeploymentID = "dpl_changed"
	}
	return out, nil
}

func (f *landingReviewReadFixture) GetStaticDeployment(_ context.Context, _ string, _ string, _ string) (StaticDeploymentResult, error) {
	f.deploymentCalls++
	return f.deployment, nil
}

func (f *landingReviewReadFixture) DeploymentAliases(_ context.Context, _ string, _ string, _ string) (*DeploymentAliasDetails, error) {
	f.aliasCalls++
	out := cloneAliasDetails(f.aliases)
	// Preserve unknown versus confirmed-empty provider metadata in the fixture.
	if out != nil && f.aliases.Aliases == nil {
		out.Aliases = nil
	}
	if out != nil {
		out.ObservedAt = time.Now().UTC().Add(f.aliasTimeOffset)
	}
	if f.afterAliases != nil {
		f.afterAliases()
	}
	return out, nil
}

func newLandingReviewReadFixture(a *LandingAdoption) *landingReviewReadFixture {
	return &landingReviewReadFixture{assessment: cloneStaticAssessment(a.Assessment), deployment: StaticDeploymentResult{ID: a.Assessment.ProductionDeploymentID, ProjectID: a.ProjectID, OwnerID: a.AccountID, Target: "production", State: "READY", URL: "https://synthetic-private:secret@private.example/"}, aliases: &DeploymentAliasDetails{DeploymentID: a.Assessment.ProductionDeploymentID, Aliases: []string{"https://zeta.vercel.app/", "alpha.vercel.app", "https://alpha.vercel.app"}}}
}

func landingUpdateReviewFixture(t *testing.T) (*LandingUpdateReviewService, *LandingAdoptionService, *LandingAdoption, *Connection, *landingReviewReadFixture) {
	t.Helper()
	adoptions, c, req := adoptionFixture(t)
	a, err := adoptions.Create(context.Background(), req, "fixture-adoption-admin")
	if err != nil {
		t.Fatal(err)
	}
	read := newLandingReviewReadFixture(a)
	memory := adoptions.Service.Store.(*Memory)
	store := NewMemoryLandingUpdateReviews(memory, adoptions.Store.(*MemoryLandingAdoptions))
	s := &LandingUpdateReviewService{Service: adoptions.Service, Adoptions: adoptions.Store, Store: store, Projects: read, Deployments: read, Aliases: read, Publications: adoptions.Publications}
	return s, adoptions, a, c, read
}

func landingReviewRequest(a *LandingAdoption) LandingUpdateReviewRequest {
	return LandingUpdateReviewRequest{AdoptionID: a.ID, AdoptionVersion: a.Version, ConnectionVersion: a.ConnectionVersion}
}

func TestLandingUpdateReviewServerArtifactBaselineAndNoSubmission(t *testing.T) {
	s, _, a, _, api := landingUpdateReviewFixture(t)
	r, err := s.Create(context.Background(), landingReviewRequest(a), "fixture-review-admin")
	if err != nil {
		t.Fatal(err)
	}
	artifact, _, err := Landing()
	if err != nil || r.ArtifactSHA256 != artifact.SHA256 || r.ArtifactFileCount != artifact.FileCount || r.ArtifactBytes != artifact.Bytes || r.BaselineDeploymentID != a.Assessment.ProductionDeploymentID || r.AdoptionVersion != a.Version || r.ConnectionVersion != a.ConnectionVersion || r.Actor != "fixture-review-admin" || r.ExpiresAt.Sub(r.CreatedAt) != LandingUpdateReviewTTL || !strings.HasPrefix(r.ID, landingUpdateReviewIDPrefix) {
		t.Fatal("review was not server-issued from exact source/binding", r, err)
	}
	if api.projectCalls != 2 || api.deploymentCalls != 1 || api.aliasCalls != 1 {
		t.Fatal("unexpected read campaign", api.projectCalls, api.deploymentCalls, api.aliasCalls)
	}
	if !reflect.DeepEqual(r.BaselineAliases, []string{"https://alpha.vercel.app", "https://zeta.vercel.app"}) {
		t.Fatal("aliases were not canonical sorted exact set", r.BaselineAliases)
	}
	data, err := json.Marshal(r)
	if err != nil || strings.Contains(string(data), "fixture-review-admin") || strings.Contains(string(data), "synthetic-private") || strings.Contains(string(data), testToken) {
		t.Fatal("review exposed actor/private deployment data/token", string(data), err)
	}
	var payload map[string]any
	if json.Unmarshal(data, &payload) != nil || payload["submissionAvailable"] != false {
		t.Fatal("review advertised executable capability", string(data))
	}
	// The derived false flag cannot be activated by stored/browser JSON.
	payload["submissionAvailable"] = true
	data, _ = json.Marshal(payload)
	var decoded LandingUpdateReview
	if json.Unmarshal(data, &decoded) != nil {
		t.Fatal("bad fixture JSON")
	}
	data, _ = json.Marshal(&decoded)
	if !strings.Contains(string(data), `"submissionAvailable":false`) {
		t.Fatal("caller flag enabled submission", string(data))
	}
}

func TestLandingUpdateReviewRejectsWrongVersionsAndProviderIdentity(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*LandingUpdateReviewService, *LandingUpdateReviewRequest, *landingReviewReadFixture)
	}{
		{"adoption version", func(_ *LandingUpdateReviewService, req *LandingUpdateReviewRequest, _ *landingReviewReadFixture) {
			req.AdoptionVersion++
		}},
		{"connection version", func(_ *LandingUpdateReviewService, req *LandingUpdateReviewRequest, _ *landingReviewReadFixture) {
			req.ConnectionVersion++
		}},
		{"unknown assessment", func(_ *LandingUpdateReviewService, _ *LandingUpdateReviewRequest, f *landingReviewReadFixture) {
			f.assessment.GitLinkKnown = false
		}},
		{"foreign assessment", func(_ *LandingUpdateReviewService, _ *LandingUpdateReviewRequest, f *landingReviewReadFixture) {
			f.assessment.OwnerID = "user_other"
		}},
		{"stale assessment", func(_ *LandingUpdateReviewService, _ *LandingUpdateReviewRequest, f *landingReviewReadFixture) {
			f.projectTimeOffset = -time.Minute
		}},
		{"future assessment", func(_ *LandingUpdateReviewService, _ *LandingUpdateReviewRequest, f *landingReviewReadFixture) {
			f.projectTimeOffset = time.Minute
		}},
		{"foreign deployment", func(_ *LandingUpdateReviewService, _ *LandingUpdateReviewRequest, f *landingReviewReadFixture) {
			f.deployment.ProjectID = "prj_other"
		}},
		{"missing deployment owner", func(_ *LandingUpdateReviewService, _ *LandingUpdateReviewRequest, f *landingReviewReadFixture) {
			f.deployment.OwnerID = ""
		}},
		{"preview deployment", func(_ *LandingUpdateReviewService, _ *LandingUpdateReviewRequest, f *landingReviewReadFixture) {
			f.deployment.Target = "preview"
		}},
		{"failed deployment", func(_ *LandingUpdateReviewService, _ *LandingUpdateReviewRequest, f *landingReviewReadFixture) {
			f.deployment.State = "ERROR"
		}},
		{"missing aliases", func(_ *LandingUpdateReviewService, _ *LandingUpdateReviewRequest, f *landingReviewReadFixture) {
			f.aliases = nil
		}},
		{"unknown aliases", func(_ *LandingUpdateReviewService, _ *LandingUpdateReviewRequest, f *landingReviewReadFixture) {
			f.aliases.Aliases = nil
		}},
		{"foreign aliases", func(_ *LandingUpdateReviewService, _ *LandingUpdateReviewRequest, f *landingReviewReadFixture) {
			f.aliases.DeploymentID = "dpl_other"
		}},
		{"stale aliases", func(_ *LandingUpdateReviewService, _ *LandingUpdateReviewRequest, f *landingReviewReadFixture) {
			f.aliasTimeOffset = -time.Minute
		}},
		{"alias error", func(_ *LandingUpdateReviewService, _ *LandingUpdateReviewRequest, f *landingReviewReadFixture) {
			f.aliases.Error = "unavailable"
		}},
		{"changed production", func(_ *LandingUpdateReviewService, _ *LandingUpdateReviewRequest, f *landingReviewReadFixture) {
			f.changeProduction = true
		}},
		{"lost provenance", func(s *LandingUpdateReviewService, _ *LandingUpdateReviewRequest, _ *landingReviewReadFixture) {
			f := s.Publications.(adoptionPublicationFixture)
			f.publication.Status = "needs_attention"
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _, a, _, api := landingUpdateReviewFixture(t)
			req := landingReviewRequest(a)
			tt.mutate(s, &req, api)
			if _, err := s.Create(context.Background(), req, "fixture-review-admin"); err == nil {
				t.Fatal("unconfirmed review issued")
			}
			rows, err := s.List(context.Background())
			if err != nil || len(rows) != 0 {
				t.Fatal("rejected review persisted", rows, err)
			}
		})
	}
}

func TestLandingUpdateReviewAliasSafetyAndAuthoritativeEmpty(t *testing.T) {
	for _, raw := range []string{"http://site.vercel.app", "https://user:password@site.vercel.app", "https://site.vercel.app/path", "https://site.vercel.app?secret=x", "https://site.vercel.app#fragment", "https://127.0.0.1", "https://private.internal"} {
		s, _, a, _, api := landingUpdateReviewFixture(t)
		api.aliases.Aliases = []string{raw}
		if _, err := s.Create(context.Background(), landingReviewRequest(a), "fixture-review-admin"); err == nil {
			t.Fatal("unsafe alias entered review")
		}
	}
	s, _, a, _, api := landingUpdateReviewFixture(t)
	api.aliases.Aliases = []string{}
	r, err := s.Create(context.Background(), landingReviewRequest(a), "fixture-review-admin")
	if err != nil || r.BaselineAliases == nil || len(r.BaselineAliases) != 0 || r.BaselineAliasesObservedAt.IsZero() {
		t.Fatal("observed empty aliases were confused with unknown", r, err)
	}
}

func TestLandingUpdateReviewConnectionAndAdoptionChangesFenceSlowRead(t *testing.T) {
	for _, change := range []string{"selection", "connection disable", "adoption disable", "adoption version"} {
		t.Run(change, func(t *testing.T) {
			s, adoptions, a, c, api := landingUpdateReviewFixture(t)
			api.afterAliases = func() {
				var err error
				switch change {
				case "selection":
					selection := []string{"prj_two"}
					_, err = s.Service.Update(context.Background(), c.ID, UpdateRequest{Version: c.Version, ProjectIDs: &selection})
				case "connection disable":
					enabled := false
					_, err = s.Service.Update(context.Background(), c.ID, UpdateRequest{Version: c.Version, Enabled: &enabled})
				case "adoption disable":
					_, err = adoptions.Update(context.Background(), a.ID, LandingAdoptionUpdate{Version: a.Version, Enabled: false}, "fixture-adoption-admin")
				case "adoption version":
					_, err = adoptions.Update(context.Background(), a.ID, LandingAdoptionUpdate{Version: a.Version, ConnectionVersion: a.ConnectionVersion, Enabled: true, ConfirmManualUpdates: true}, "fixture-adoption-admin")
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.Create(context.Background(), landingReviewRequest(a), "fixture-review-admin"); !errors.Is(err, ErrConflict) {
				t.Fatal("changed authority admitted a review", err)
			}
			rows, err := s.List(context.Background())
			if err != nil || len(rows) != 0 {
				t.Fatal("stale authority review persisted", rows, err)
			}
		})
	}
}

func TestLandingUpdateReviewCanceledProviderReplyCannotPersist(t *testing.T) {
	s, _, a, _, api := landingUpdateReviewFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	api.afterAliases = cancel
	if _, err := s.Create(ctx, landingReviewRequest(a), "fixture-review-admin"); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled provider reply issued review", err)
	}
	rows, err := s.List(context.Background())
	if err != nil || len(rows) != 0 {
		t.Fatal("canceled review persisted", rows, err)
	}
}

func TestLandingUpdateReviewImmutableClonesAndBoundedHistory(t *testing.T) {
	s, _, a, _, _ := landingUpdateReviewFixture(t)
	r, err := s.Create(context.Background(), landingReviewRequest(a), "fixture-review-admin")
	if err != nil {
		t.Fatal(err)
	}
	store := s.Store.(*MemoryLandingUpdateReviews)
	r.BaselineAliases[0] = "https://changed.vercel.app"
	r.Assessment.Reasons = append(r.Assessment.Reasons, "changed")
	got, err := s.Get(context.Background(), r.ID)
	if err != nil || got.BaselineAliases[0] != "https://alpha.vercel.app" || len(got.Assessment.Reasons) != 0 {
		t.Fatal("caller changed immutable review", got, err)
	}
	if err = store.Create(context.Background(), got); !errors.Is(err, ErrConflict) {
		t.Fatal("immutable review was overwritten", err)
	}
	for index := 1; index <= LandingUpdateReviewHistoryLimit; index++ {
		next := cloneLandingUpdateReview(got)
		next.ID = fmt.Sprintf("vlur_history_%03d", index)
		offset := time.Duration(index) * time.Millisecond
		next.CreatedAt = next.CreatedAt.Add(-offset)
		next.ExpiresAt = next.CreatedAt.Add(LandingUpdateReviewTTL)
		next.Assessment.ObservedAt = next.Assessment.ObservedAt.Add(-offset)
		next.BaselineDeploymentObservedAt = next.BaselineDeploymentObservedAt.Add(-offset)
		next.BaselineAliasesObservedAt = next.BaselineAliasesObservedAt.Add(-offset)
		if err = store.Create(context.Background(), next); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.List(context.Background())
	if err != nil || len(rows) != LandingUpdateReviewHistoryLimit || rows[0].ID != got.ID {
		t.Fatal("recent history window unbounded/unordered", len(rows), err)
	}
	if _, err = s.Get(context.Background(), "vlur_history_100"); err != nil {
		t.Fatal("history window erased older receipt", err)
	}
}
