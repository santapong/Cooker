package vercel

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	LandingAdoptionEnvironment = "production"
	LandingAdoptionOwner       = "cooker_manual"
	LandingAdoptionSource      = "embedded_cooker_landing"
	LandingAdoptionAction      = "update_landing"
)

// LandingAdoption is local, versioned authority for a supported manual action.
// It creates no provider deployment, job, native App or editable imported DAG.
type LandingAdoption struct {
	ID                      string                   `json:"id"`
	Version                 int                      `json:"version"`
	Enabled                 bool                     `json:"enabled"`
	ConnectionID            string                   `json:"connectionId"`
	ConnectionVersion       int                      `json:"connectionVersion"`
	AccountScope            string                   `json:"accountScope"`
	AccountID               string                   `json:"accountId"`
	TeamID                  string                   `json:"teamId"`
	ProjectID               string                   `json:"projectId"`
	ProjectName             string                   `json:"projectName"`
	Environment             string                   `json:"environment"`
	DeploymentOwner         string                   `json:"deploymentOwner"`
	SourceKind              string                   `json:"sourceKind"`
	PermittedActions        []string                 `json:"permittedActions"`
	ProvenancePublicationID string                   `json:"provenancePublicationId"`
	ProvenanceDeploymentID  string                   `json:"provenanceDeploymentId"`
	Assessment              *StaticProjectAssessment `json:"assessment"`
	CreatedBy               string                   `json:"createdBy"`
	UpdatedBy               string                   `json:"updatedBy"`
	CreatedAt               time.Time                `json:"createdAt"`
	UpdatedAt               time.Time                `json:"updatedAt"`
}

type LandingAdoptionRequest struct {
	ConnectionID            string `json:"connectionId"`
	ConnectionVersion       int    `json:"connectionVersion"`
	ProjectID               string `json:"projectId"`
	ProvenancePublicationID string `json:"provenancePublicationId"`
	ConfirmManualUpdates    bool   `json:"confirmManualUpdates"`
}

type LandingAdoptionUpdate struct {
	Version              int  `json:"version"`
	ConnectionVersion    int  `json:"connectionVersion"`
	Enabled              bool `json:"enabled"`
	ConfirmManualUpdates bool `json:"confirmManualUpdates"`
}

type PublicationProvenanceReader interface {
	Get(context.Context, string) (*Publication, error)
}

type LandingAdoptionService struct {
	Service      *Service
	Store        LandingAdoptionStore
	Projects     StaticProjectReadAPI
	Publications PublicationProvenanceReader
}

func (s *LandingAdoptionService) available() bool {
	return s != nil && s.Store != nil && s.Service.available() && s.Projects != nil && s.Publications != nil
}

func (s *LandingAdoptionService) Get(ctx context.Context, id string) (*LandingAdoption, error) {
	if s == nil || s.Store == nil {
		return nil, ErrUnavailable
	}
	if !validID(id) {
		return nil, ErrInvalid
	}
	return s.Store.Get(ctx, id)
}

func (s *LandingAdoptionService) List(ctx context.Context) ([]*LandingAdoption, error) {
	if s == nil || s.Store == nil {
		return nil, ErrUnavailable
	}
	return s.Store.List(ctx)
}

func (s *LandingAdoptionService) Create(ctx context.Context, req LandingAdoptionRequest, actor string) (*LandingAdoption, error) {
	if !s.available() {
		return nil, ErrUnavailable
	}
	if !validAdoptionActor(actor) || !req.ConfirmManualUpdates || !validID(req.ConnectionID) || req.ConnectionVersion < 1 || staticID(req.ProjectID, "prj_") == "" || !validID(req.ProvenancePublicationID) {
		return nil, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, SyncTimeout)
	defer cancel()
	r, assessment, provenance, err := s.assess(ctx, req.ConnectionID, req.ConnectionVersion, req.ProjectID, req.ProvenancePublicationID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	a := &LandingAdoption{ID: uuid.NewString(), Version: 1, Enabled: true, ConnectionID: r.ID, ConnectionVersion: r.Version, AccountScope: r.Identity.Scope, AccountID: r.Identity.ID, TeamID: r.TeamID, ProjectID: req.ProjectID, ProjectName: assessment.Name, Environment: LandingAdoptionEnvironment, DeploymentOwner: LandingAdoptionOwner, SourceKind: LandingAdoptionSource, PermittedActions: []string{LandingAdoptionAction}, ProvenancePublicationID: provenance.ID, ProvenanceDeploymentID: provenance.DeploymentID, Assessment: cloneStaticAssessment(assessment), CreatedBy: actor, UpdatedBy: actor, CreatedAt: now, UpdatedAt: now}
	if err = s.Store.Create(ctx, a); err != nil {
		return nil, err
	}
	return cloneLandingAdoption(a), nil
}

func (s *LandingAdoptionService) Update(ctx context.Context, id string, req LandingAdoptionUpdate, actor string) (*LandingAdoption, error) {
	if s == nil || s.Store == nil {
		return nil, ErrUnavailable
	}
	if !validID(id) || req.Version < 1 || !validAdoptionActor(actor) {
		return nil, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, SyncTimeout)
	defer cancel()
	current, err := s.Store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.Version != req.Version {
		return nil, ErrConflict
	}
	if req.Enabled {
		if !s.available() {
			return nil, ErrUnavailable
		}
		if !req.ConfirmManualUpdates || req.ConnectionVersion < 1 {
			return nil, ErrInvalid
		}
		r, assessment, _, err := s.assess(ctx, current.ConnectionID, req.ConnectionVersion, current.ProjectID, current.ProvenancePublicationID)
		if err != nil {
			return nil, err
		}
		if r.Identity.Scope != current.AccountScope || r.Identity.ID != current.AccountID || r.TeamID != current.TeamID {
			return nil, ErrConflict
		}
		current.ConnectionVersion, current.ProjectName, current.Assessment = r.Version, assessment.Name, cloneStaticAssessment(assessment)
	}
	// Disable remains possible after connection deletion. It revokes only local
	// authority and preserves the historical identity and assessment receipt.
	current.Enabled, current.UpdatedBy, current.UpdatedAt = req.Enabled, actor, time.Now().UTC()
	if err = s.Store.Update(ctx, current, req.Version); err != nil {
		return nil, err
	}
	return cloneLandingAdoption(current), nil
}

func (s *LandingAdoptionService) assess(ctx context.Context, connectionID string, version int, projectID, publicationID string) (*Record, *StaticProjectAssessment, *Publication, error) {
	r, err := s.Service.Store.Get(ctx, connectionID)
	if err != nil {
		return nil, nil, nil, err
	}
	if r.Version != version || !r.Enabled || !selectedAdoptionProject(r, projectID) || !validAdoptionAccount(r.Identity.Scope, r.Identity.ID, r.TeamID) {
		return nil, nil, nil, ErrConflict
	}
	token, err := s.Service.token(r)
	if err != nil {
		return nil, nil, nil, err
	}
	if err = s.Service.verifyIdentity(ctx, r, token); err != nil {
		return nil, nil, nil, err
	}
	provenance, err := s.Publications.Get(ctx, publicationID)
	if err != nil {
		return nil, nil, nil, err
	}
	if provenance == nil || provenance.ID != publicationID || provenance.Status != "succeeded" || provenance.ProjectID != projectID || provenance.AccountScope != r.Identity.Scope || provenance.AccountID != r.Identity.ID || provenance.TeamID != r.TeamID || provenance.Target != LandingAdoptionEnvironment || staticID(provenance.DeploymentID, "dpl_") == "" || !staticSHA256(provenance.ArtifactSHA256) {
		return nil, nil, nil, ErrConflict
	}
	assessment, err := s.Projects.StaticProject(ctx, token, r.TeamID, projectID)
	if err != nil {
		return nil, nil, nil, err
	}
	if !validStaticAssessment(assessment) || assessment.ProjectID != projectID || assessment.OwnerID != r.Identity.ID {
		return nil, nil, nil, ErrConflict
	}
	return r, cloneStaticAssessment(assessment), provenance, nil
}

func validStaticAssessment(a *StaticProjectAssessment) bool {
	return a != nil && staticID(a.ProjectID, "prj_") != "" && validProviderID(a.OwnerID) && validName(a.Name) && len(a.Name) <= 100 && staticID(a.ProductionDeploymentID, "dpl_") != "" && !a.ObservedAt.IsZero() && a.Eligible && len(a.Reasons) == 0 && len(staticEligibilityReasons(a)) == 0
}

func selectedAdoptionProject(r *Record, id string) bool {
	for _, selected := range r.ProjectIDs {
		if selected == id {
			return true
		}
	}
	return false
}

func validAdoptionAccount(scope, accountID, teamID string) bool {
	if !validProviderID(accountID) {
		return false
	}
	return (scope == "personal" && teamID == "") || (scope == "team" && validProviderID(teamID) && teamID == accountID)
}

func validAdoptionActor(actor string) bool {
	return strings.TrimSpace(actor) != "" && len(actor) <= 512 && !strings.ContainsAny(actor, "\r\n\x00")
}

func validateLandingAdoption(a *LandingAdoption) error {
	if a == nil || !validID(a.ID) || a.Version < 1 || !validID(a.ConnectionID) || a.ConnectionVersion < 1 || !validAdoptionAccount(a.AccountScope, a.AccountID, a.TeamID) || staticID(a.ProjectID, "prj_") == "" || !validName(a.ProjectName) || len(a.ProjectName) > 100 || a.Environment != LandingAdoptionEnvironment || a.DeploymentOwner != LandingAdoptionOwner || a.SourceKind != LandingAdoptionSource || len(a.PermittedActions) != 1 || a.PermittedActions[0] != LandingAdoptionAction || !validID(a.ProvenancePublicationID) || staticID(a.ProvenanceDeploymentID, "dpl_") == "" || !validStaticAssessment(a.Assessment) || a.Assessment.ProjectID != a.ProjectID || a.Assessment.OwnerID != a.AccountID || a.Assessment.Name != a.ProjectName || !validAdoptionActor(a.CreatedBy) || !validAdoptionActor(a.UpdatedBy) || a.CreatedAt.IsZero() || a.UpdatedAt.IsZero() {
		return ErrInvalid
	}
	return nil
}

func adoptionConnectionMatches(r *Record, a *LandingAdoption) bool {
	return r != nil && r.ID == a.ConnectionID && r.Version == a.ConnectionVersion && r.Enabled && r.Identity.Scope == a.AccountScope && r.Identity.ID == a.AccountID && r.TeamID == a.TeamID && selectedAdoptionProject(r, a.ProjectID)
}

func sameAdoptionIdentity(a, b *LandingAdoption) bool {
	return a.ID == b.ID && a.ConnectionID == b.ConnectionID && a.AccountScope == b.AccountScope && a.AccountID == b.AccountID && a.TeamID == b.TeamID && a.ProjectID == b.ProjectID && a.Environment == b.Environment && a.DeploymentOwner == b.DeploymentOwner && a.SourceKind == b.SourceKind && a.ProvenancePublicationID == b.ProvenancePublicationID && a.ProvenanceDeploymentID == b.ProvenanceDeploymentID && a.CreatedBy == b.CreatedBy && a.CreatedAt.Equal(b.CreatedAt)
}

func sameAdoptionDestination(a, b *LandingAdoption) bool {
	return a.AccountScope == b.AccountScope && a.AccountID == b.AccountID && a.TeamID == b.TeamID && a.ProjectID == b.ProjectID && a.Environment == b.Environment
}

func cloneLandingAdoption(a *LandingAdoption) *LandingAdoption {
	if a == nil {
		return nil
	}
	out := *a
	out.PermittedActions = append([]string{}, a.PermittedActions...)
	out.Assessment = cloneStaticAssessment(a.Assessment)
	return &out
}
