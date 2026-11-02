package vercel

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const LandingUpdateReviewTTL = 10 * time.Minute
const LandingUpdateReviewHistoryLimit = 100
const landingUpdateReviewIDPrefix = "vlur_"

// LandingUpdateReview is an immutable server-issued baseline, not an operation
// or permission to submit a deployment. It never enters vercel_publications.
type LandingUpdateReview struct {
	ID                           string                   `json:"id"`
	Status                       string                   `json:"status"`
	AdoptionID                   string                   `json:"adoptionId"`
	AdoptionVersion              int                      `json:"adoptionVersion"`
	ConnectionID                 string                   `json:"connectionId"`
	ConnectionVersion            int                      `json:"connectionVersion"`
	AccountScope                 string                   `json:"accountScope"`
	AccountID                    string                   `json:"accountId"`
	TeamID                       string                   `json:"teamId"`
	ProjectID                    string                   `json:"projectId"`
	ProjectName                  string                   `json:"projectName"`
	Environment                  string                   `json:"environment"`
	DeploymentOwner              string                   `json:"deploymentOwner"`
	SourceKind                   string                   `json:"sourceKind"`
	Action                       string                   `json:"action"`
	ArtifactSHA256               string                   `json:"artifactSha256"`
	ArtifactFileCount            int                      `json:"artifactFileCount"`
	ArtifactBytes                int                      `json:"artifactBytes"`
	BaselineDeploymentID         string                   `json:"baselineDeploymentId"`
	BaselineDeploymentObservedAt time.Time                `json:"baselineDeploymentObservedAt"`
	BaselineAliases              []string                 `json:"baselineAliases"`
	BaselineAliasesObservedAt    time.Time                `json:"baselineAliasesObservedAt"`
	Assessment                   *StaticProjectAssessment `json:"assessment"`
	ProvenancePublicationID      string                   `json:"provenancePublicationId"`
	ProvenanceDeploymentID       string                   `json:"provenanceDeploymentId"`
	ProvenanceArtifactSHA256     string                   `json:"provenanceArtifactSha256"`
	CreatedAt                    time.Time                `json:"createdAt"`
	ExpiresAt                    time.Time                `json:"expiresAt"`
	Actor                        string                   `json:"-"`
}

// The derived flag is always false, including when reading an older receipt.
// No caller-controlled or persisted flag can activate deployment submission.
func (r *LandingUpdateReview) MarshalJSON() ([]byte, error) {
	type receipt LandingUpdateReview
	return json.Marshal(struct {
		*receipt
		SubmissionAvailable bool `json:"submissionAvailable"`
	}{receipt: (*receipt)(r), SubmissionAvailable: false})
}

type LandingUpdateReviewRequest struct {
	AdoptionID        string `json:"adoptionId"`
	AdoptionVersion   int    `json:"adoptionVersion"`
	ConnectionVersion int    `json:"connectionVersion"`
}

// LandingDeploymentReadAPI deliberately excludes all StaticAPI mutations.
type LandingDeploymentReadAPI interface {
	GetStaticDeployment(context.Context, string, string, string) (StaticDeploymentResult, error)
}

type LandingUpdateReviewService struct {
	Service      *Service
	Adoptions    LandingAdoptionStore
	Store        LandingUpdateReviewStore
	Projects     StaticProjectReadAPI
	Deployments  LandingDeploymentReadAPI
	Aliases      AliasesAPI
	Publications PublicationProvenanceReader
}

func (s *LandingUpdateReviewService) available() bool {
	return s != nil && s.Service.available() && s.Adoptions != nil && s.Store != nil && s.Projects != nil && s.Deployments != nil && s.Aliases != nil && s.Publications != nil
}

func (s *LandingUpdateReviewService) Get(ctx context.Context, id string) (*LandingUpdateReview, error) {
	if s == nil || s.Store == nil {
		return nil, ErrUnavailable
	}
	if !validID(id) {
		return nil, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	r, err := s.Store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return cloneLandingUpdateReview(r), nil
}

func (s *LandingUpdateReviewService) List(ctx context.Context) ([]*LandingUpdateReview, error) {
	if s == nil || s.Store == nil {
		return nil, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	rows, err := s.Store.List(ctx)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if len(rows) > LandingUpdateReviewHistoryLimit {
		return nil, ErrConflict
	}
	out := make([]*LandingUpdateReview, 0, len(rows))
	for _, row := range rows {
		out = append(out, cloneLandingUpdateReview(row))
	}
	return out, nil
}

func (s *LandingUpdateReviewService) Create(ctx context.Context, req LandingUpdateReviewRequest, actor string) (*LandingUpdateReview, error) {
	if !s.available() {
		return nil, ErrUnavailable
	}
	if !validID(req.AdoptionID) || req.AdoptionVersion < 1 || req.ConnectionVersion < 1 || !validAdoptionActor(actor) {
		return nil, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, SyncTimeout)
	defer cancel()
	started := time.Now().UTC()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	a, err := s.Adoptions.Get(ctx, req.AdoptionID)
	if err != nil {
		return nil, err
	}
	if validateLandingAdoption(a) != nil || !a.Enabled || a.Version != req.AdoptionVersion || a.ConnectionVersion != req.ConnectionVersion {
		return nil, ErrConflict
	}
	r, err := s.Service.Store.Get(ctx, a.ConnectionID)
	if err != nil {
		return nil, err
	}
	if !adoptionConnectionMatches(r, a) {
		return nil, ErrConflict
	}
	token, err := s.Service.token(r)
	if err != nil {
		return nil, err
	}
	if err = s.Service.verifyIdentity(ctx, r, token); err != nil {
		return nil, err
	}
	provenance, err := s.Publications.Get(ctx, a.ProvenancePublicationID)
	if err != nil {
		return nil, err
	}
	if provenance == nil || provenance.ID != a.ProvenancePublicationID || provenance.Status != "succeeded" || provenance.AccountID != a.AccountID || provenance.AccountScope != a.AccountScope || provenance.TeamID != a.TeamID || provenance.ProjectID != a.ProjectID || provenance.DeploymentID != a.ProvenanceDeploymentID || provenance.Target != LandingAdoptionEnvironment || !staticSHA256(provenance.ArtifactSHA256) {
		return nil, ErrConflict
	}
	assessment, err := s.Projects.StaticProject(ctx, token, a.TeamID, a.ProjectID)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if !reviewAssessmentMatches(assessment, a, started, time.Now().UTC()) {
		return nil, ErrConflict
	}
	baselineID := assessment.ProductionDeploymentID
	deployment, err := s.Deployments.GetStaticDeployment(ctx, token, a.TeamID, baselineID)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if deployment.ID != baselineID || deployment.ProjectID != a.ProjectID || deployment.OwnerID != a.AccountID || deployment.Target != LandingAdoptionEnvironment || deployment.State != "READY" {
		return nil, ErrConflict
	}
	deploymentObservedAt := time.Now().UTC()
	aliases, err := s.Aliases.DeploymentAliases(ctx, token, a.TeamID, baselineID)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if aliases == nil || aliases.DeploymentID != baselineID || aliases.Error != "" || !reviewObservationFresh(aliases.ObservedAt, started, time.Now().UTC()) {
		return nil, ErrConflict
	}
	canonical, err := canonicalReviewAliases(aliases.Aliases)
	if err != nil {
		return nil, err
	}
	// Re-read after deployment/alias reads, without holding database locks. This
	// detects a changed production target; it is not provider-side atomic CAS.
	assessment, err = s.Projects.StaticProject(ctx, token, a.TeamID, a.ProjectID)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if !reviewAssessmentMatches(assessment, a, started, time.Now().UTC()) || assessment.ProductionDeploymentID != baselineID {
		return nil, ErrConflict
	}
	artifact, _, err := Landing()
	if err != nil || !staticSHA256(artifact.SHA256) || artifact.FileCount < 1 || artifact.Bytes < 1 {
		return nil, ErrUnavailable
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	now := time.Now().UTC()
	review := &LandingUpdateReview{ID: landingUpdateReviewIDPrefix + uuid.NewString(), Status: "reviewed", AdoptionID: a.ID, AdoptionVersion: a.Version, ConnectionID: a.ConnectionID, ConnectionVersion: a.ConnectionVersion, AccountScope: a.AccountScope, AccountID: a.AccountID, TeamID: a.TeamID, ProjectID: a.ProjectID, ProjectName: assessment.Name, Environment: a.Environment, DeploymentOwner: a.DeploymentOwner, SourceKind: a.SourceKind, Action: LandingAdoptionAction, ArtifactSHA256: artifact.SHA256, ArtifactFileCount: artifact.FileCount, ArtifactBytes: artifact.Bytes, BaselineDeploymentID: baselineID, BaselineDeploymentObservedAt: deploymentObservedAt, BaselineAliases: canonical, BaselineAliasesObservedAt: aliases.ObservedAt, Assessment: cloneStaticAssessment(assessment), ProvenancePublicationID: provenance.ID, ProvenanceDeploymentID: provenance.DeploymentID, ProvenanceArtifactSHA256: provenance.ArtifactSHA256, CreatedAt: now, ExpiresAt: now.Add(LandingUpdateReviewTTL), Actor: actor}
	if err = s.Store.Create(ctx, review); err != nil {
		return nil, err
	}
	return cloneLandingUpdateReview(review), nil
}

func reviewObservationFresh(observed, started, now time.Time) bool {
	return !observed.IsZero() && !observed.Before(started) && !observed.After(now)
}

func reviewAssessmentMatches(assessment *StaticProjectAssessment, a *LandingAdoption, started, now time.Time) bool {
	return validStaticAssessment(assessment) && assessment.ProjectID == a.ProjectID && assessment.OwnerID == a.AccountID && reviewObservationFresh(assessment.ObservedAt, started, now)
}

func canonicalReviewAliases(values []string) ([]string, error) {
	if values == nil || len(values) > 200 {
		return nil, ErrConflict
	}
	out, seen := []string{}, map[string]bool{}
	for _, raw := range values {
		safe := deploymentURL(raw)
		u, err := url.Parse(safe)
		if safe == "" || err != nil || (u.Path != "" && u.Path != "/") {
			return nil, ErrConflict
		}
		u.Path = ""
		value := u.String()
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out, nil
}

func validateLandingUpdateReview(r *LandingUpdateReview) error {
	if r == nil || !validID(r.ID) || !strings.HasPrefix(r.ID, landingUpdateReviewIDPrefix) || r.Status != "reviewed" || !validID(r.AdoptionID) || r.AdoptionVersion < 1 || !validID(r.ConnectionID) || r.ConnectionVersion < 1 || !validAdoptionAccount(r.AccountScope, r.AccountID, r.TeamID) || staticID(r.ProjectID, "prj_") == "" || !validName(r.ProjectName) || len(r.ProjectName) > 100 || r.Environment != LandingAdoptionEnvironment || r.DeploymentOwner != LandingAdoptionOwner || r.SourceKind != LandingAdoptionSource || r.Action != LandingAdoptionAction || !staticSHA256(r.ArtifactSHA256) || r.ArtifactFileCount < 1 || r.ArtifactFileCount > MaxStaticFiles || r.ArtifactBytes < 1 || r.ArtifactBytes > MaxStaticTotalBytes || staticID(r.BaselineDeploymentID, "dpl_") == "" || !validID(r.ProvenancePublicationID) || staticID(r.ProvenanceDeploymentID, "dpl_") == "" || !staticSHA256(r.ProvenanceArtifactSHA256) || !validAdoptionActor(r.Actor) || r.CreatedAt.IsZero() || !r.ExpiresAt.Equal(r.CreatedAt.Add(LandingUpdateReviewTTL)) || !validStaticAssessment(r.Assessment) || r.Assessment.ProjectID != r.ProjectID || r.Assessment.OwnerID != r.AccountID || r.Assessment.Name != r.ProjectName || r.Assessment.ProductionDeploymentID != r.BaselineDeploymentID {
		return ErrInvalid
	}
	for _, observed := range []time.Time{r.Assessment.ObservedAt, r.BaselineDeploymentObservedAt, r.BaselineAliasesObservedAt} {
		// PostgreSQL timestamps have microsecond precision. Preserve the original
		// observation while allowing only that precision gap in the safe receipt.
		if observed.IsZero() || observed.Before(r.CreatedAt.Add(-SyncTimeout)) || observed.After(r.CreatedAt.Add(time.Microsecond)) {
			return ErrInvalid
		}
	}
	canonical, err := canonicalReviewAliases(r.BaselineAliases)
	if err != nil || len(canonical) != len(r.BaselineAliases) {
		return ErrInvalid
	}
	for i, value := range canonical {
		if value != r.BaselineAliases[i] {
			return ErrInvalid
		}
	}
	return nil
}

func reviewAdoptionMatches(a *LandingAdoption, r *LandingUpdateReview) bool {
	return validateLandingAdoption(a) == nil && a.Enabled && a.ID == r.AdoptionID && a.Version == r.AdoptionVersion && a.ConnectionID == r.ConnectionID && a.ConnectionVersion == r.ConnectionVersion && a.AccountScope == r.AccountScope && a.AccountID == r.AccountID && a.TeamID == r.TeamID && a.ProjectID == r.ProjectID && a.Environment == r.Environment && a.DeploymentOwner == r.DeploymentOwner && a.SourceKind == r.SourceKind && a.ProvenancePublicationID == r.ProvenancePublicationID && a.ProvenanceDeploymentID == r.ProvenanceDeploymentID
}

func cloneLandingUpdateReview(r *LandingUpdateReview) *LandingUpdateReview {
	if r == nil {
		return nil
	}
	out := *r
	out.BaselineAliases = append([]string{}, r.BaselineAliases...)
	out.Assessment = cloneStaticAssessment(r.Assessment)
	return &out
}
