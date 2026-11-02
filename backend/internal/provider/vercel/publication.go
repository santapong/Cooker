package vercel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/store"
)

const JobKindStatic = "vercel-static-deploy"

type Publication struct {
	ID                string    `json:"id"`
	ConnectionID      string    `json:"connectionId"`
	ConnectionVersion int       `json:"connectionVersion"`
	AccountID         string    `json:"accountId"`
	AccountScope      string    `json:"accountScope"`
	TeamID            string    `json:"teamId"`
	AccountName       string    `json:"accountName"`
	ProjectName       string    `json:"projectName"`
	ProjectID         string    `json:"projectId,omitempty"`
	ArtifactSHA256    string    `json:"artifactSha256"`
	Target            string    `json:"target"`
	Status            string    `json:"status"`
	DeploymentID      string    `json:"deploymentId,omitempty"`
	URL               string    `json:"url,omitempty"`
	Message           string    `json:"message,omitempty"`
	ExpiresAt         time.Time `json:"expiresAt"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
	Actor             string    `json:"-"`
}

// Publisher is deliberately bounded to the embedded Cooker landing and a new
// dedicated static project. It cannot publish arbitrary paths or replace apps.
type Publisher struct {
	DB        *sql.DB
	Service   *Service
	Jobs      jobqueue.Store
	API       StaticAPI
	PollEvery time.Duration
	Deadline  time.Duration
}

func (p *Publisher) available() bool {
	return p != nil && p.DB != nil && p.Service.available() && p.Jobs != nil && p.API != nil
}

func readPublication(row interface{ Scan(...any) error }) (*Publication, error) {
	var data []byte
	var actor, status string
	var created, updated time.Time
	if err := row.Scan(&data, &actor, &status, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var op Publication
	if err := json.Unmarshal(data, &op); err != nil {
		return nil, err
	}
	op.Actor, op.Status, op.CreatedAt, op.UpdatedAt = actor, status, created, updated
	return &op, nil
}

func (p *Publisher) Get(ctx context.Context, id string) (*Publication, error) {
	if !p.available() {
		return nil, ErrUnavailable
	}
	return readPublication(p.DB.QueryRowContext(ctx, `SELECT receipt,actor,status,created_at,updated_at FROM vercel_publications WHERE id=$1`, id))
}

func (p *Publisher) List(ctx context.Context) ([]*Publication, error) {
	if !p.available() {
		return nil, ErrUnavailable
	}
	rows, err := p.DB.QueryContext(ctx, `SELECT receipt,actor,status,created_at,updated_at FROM vercel_publications ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ops := []*Publication{}
	for rows.Next() {
		op, err := readPublication(rows)
		if err != nil {
			return nil, err
		}
		ops = append(ops, op)
	}
	return ops, rows.Err()
}

func (p *Publisher) credentials(ctx context.Context, op *Publication) (*Record, string, error) {
	r, err := p.Service.Store.Get(ctx, op.ConnectionID)
	if err != nil {
		return nil, "", err
	}
	if r.Version != op.ConnectionVersion || !r.Enabled || r.Identity.ID != op.AccountID {
		return nil, "", ErrConflict
	}
	token, err := p.Service.token(r)
	if err != nil {
		return nil, "", err
	}
	if err = p.Service.verifyIdentity(ctx, r, token); err != nil {
		return nil, "", err
	}
	return r, token, nil
}

func (p *Publisher) Review(ctx context.Context, connectionID string, version int, actor string) (*Publication, error) {
	if !p.available() {
		return nil, ErrUnavailable
	}
	if actor == "" || !validID(connectionID) || version < 1 {
		return nil, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, SyncTimeout)
	defer cancel()
	r, err := p.Service.Store.Get(ctx, connectionID)
	if err != nil {
		return nil, err
	}
	if r.Version != version || !r.Enabled {
		return nil, ErrConflict
	}
	artifact, _, err := Landing()
	if err != nil {
		return nil, ErrUnavailable
	}
	now := time.Now().UTC()
	id := uuid.NewString()
	op := &Publication{ID: id, ConnectionID: connectionID, ConnectionVersion: version, AccountID: r.Identity.ID, AccountScope: r.Identity.Scope, TeamID: r.TeamID, AccountName: r.Identity.Name, ProjectName: "cooker-landing-" + id[:8], ArtifactSHA256: artifact.SHA256, Target: "production", Status: "reviewed", Actor: actor, CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(10 * time.Minute), Message: "Create a new dedicated static project and publish only the Cooker landing page and its assets as the first production release. Login, documentation, workspace and example files are excluded."}
	r, token, err := p.credentials(ctx, op)
	if err != nil {
		return nil, err
	}
	projects, err := p.Service.API.Projects(ctx, token, r.TeamID)
	if err != nil {
		return nil, err
	}
	for _, project := range projects {
		if project.Name == op.ProjectName {
			return nil, ErrConflict
		}
	}
	data, err := json.Marshal(op)
	if err != nil {
		return nil, err
	}
	res, err := p.DB.ExecContext(ctx, `INSERT INTO vercel_publications(id,connection_id,connection_version,actor,status,receipt,created_at,updated_at) SELECT $1,$2,$3,$4,'reviewed',$5,$6,$6 FROM vercel_connections WHERE id=$2 AND version=$3 AND (configuration->>'enabled')::boolean`, id, connectionID, version, actor, data, now)
	if err != nil {
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n != 1 {
		return nil, ErrConflict
	}
	return op, nil
}

func (p *Publisher) Deploy(ctx context.Context, id, actor string) (*Publication, error) {
	op, err := p.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if op.Actor != actor {
		return nil, ErrConflict
	}
	// The reviewed intent is the durable idempotency key, even after a lost reply.
	if op.Status != "reviewed" {
		return op, nil
	}
	if time.Now().After(op.ExpiresAt) {
		return nil, ErrConflict
	}
	payload, _ := json.Marshal(map[string]string{"operationId": id, "artifactSha256": op.ArtifactSHA256})
	_, err = p.Jobs.Enqueue(ctx, JobKindStatic, payload, jobqueue.EnqueueOptions{ID: id, MaxAttempts: 1, ConcurrencyKey: "vercel-publish:" + op.AccountID})
	if err != nil {
		// An INSERT or response may have raced an identical submission.
		latest, readErr := p.Get(ctx, id)
		if readErr == nil && latest.Status != "reviewed" {
			return latest, nil
		}
		return nil, ErrConflict
	}
	return p.Get(ctx, id)
}

// Atomic admission and recovery are installed before the queue starts. Receipt
// and job transitions commit together; worker loss never schedules another POST.
func PublicationAdmission(ctx context.Context, tx *sql.Tx, j *jobqueue.Job) error {
	if j.Kind != JobKindStatic {
		return nil
	}
	var version int
	err := tx.QueryRowContext(ctx, `SELECT c.version FROM vercel_connections c JOIN vercel_publications p ON p.connection_id=c.id WHERE p.id=$1 AND (c.configuration->>'enabled')::boolean FOR SHARE OF c`, j.ID).Scan(&version)
	if err != nil {
		return ErrConflict
	}
	op, err := readPublication(tx.QueryRowContext(ctx, `SELECT receipt,actor,status,created_at,updated_at FROM vercel_publications WHERE id=$1 FOR UPDATE`, j.ID))
	if err != nil {
		return err
	}
	var payload struct {
		OperationID string `json:"operationId"`
		SHA256      string `json:"artifactSha256"`
	}
	if json.Unmarshal(j.Payload, &payload) != nil || payload.OperationID != op.ID || payload.SHA256 != op.ArtifactSHA256 || op.ConnectionVersion != version || op.Status != "reviewed" || time.Now().After(op.ExpiresAt) {
		return ErrConflict
	}
	op.Status, op.Message = "queued", "Waiting for a Cooker deployment worker"
	return savePublication(ctx, tx, op)
}

func PublicationTransition(ctx context.Context, tx *sql.Tx, j *jobqueue.Job) error {
	if j.Kind != JobKindStatic {
		return nil
	}
	op, err := readPublication(tx.QueryRowContext(ctx, `SELECT receipt,actor,status,created_at,updated_at FROM vercel_publications WHERE id=$1 FOR UPDATE`, j.ID))
	if err != nil {
		return err
	}
	switch j.Status {
	case jobqueue.StatusNeedsAttention:
		op.Status, op.Message = "needs_attention", "The provider outcome needs checking. Cooker will not submit this deployment again."
	case jobqueue.StatusFailed:
		op.Status, op.Message = "failed", "Deployment stopped. Review the retained provider IDs before creating a new request."
	case jobqueue.StatusCancelled:
		op.Status, op.Message = "failed", "Publication was cancelled; no automatic retry."
	case jobqueue.StatusSucceeded:
		op.Status = "succeeded"
	default:
		return nil
	}
	return savePublication(ctx, tx, op)
}

func savePublication(ctx context.Context, tx *sql.Tx, op *Publication) error {
	op.UpdatedAt = time.Now().UTC()
	data, err := json.Marshal(op)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE vercel_publications SET receipt=$2,status=$3,updated_at=$4 WHERE id=$1`, op.ID, data, op.Status, op.UpdatedAt)
	return err
}

// Lock the authoritative job before any receipt write. A revoked worker cannot
// overwrite reconciliation or erase an acknowledged provider ID.
func (p *Publisher) writeClaim(ctx context.Context, op *Publication) error {
	return p.writeIntent(ctx, op, false)
}

func (p *Publisher) writeIntent(ctx context.Context, op *Publication, recheckConnection bool) error {
	claim, ok := store.RunClaimFromContext(ctx)
	if !ok || claim.JobID != op.ID || claim.Owner == "" {
		return ErrConflict
	}
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var owned int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM jobs WHERE id=$1 AND kind=$2 AND locked_by=$3 AND status='running' FOR SHARE`, op.ID, JobKindStatic, claim.Owner).Scan(&owned)
	if err != nil {
		return ErrConflict
	}
	if recheckConnection {
		var version int
		var identity, scope, teamID string
		var enabled bool
		err = tx.QueryRowContext(ctx, `SELECT version,configuration->'identity'->>'id',configuration->'identity'->>'scope',configuration->>'teamId',(configuration->>'enabled')::boolean FROM vercel_connections WHERE id=$1 FOR SHARE`, op.ConnectionID).Scan(&version, &identity, &scope, &teamID, &enabled)
		if err != nil || version != op.ConnectionVersion || !enabled || identity != op.AccountID || scope != op.AccountScope || teamID != op.TeamID {
			return ErrConflict
		}
	}
	if err = savePublication(ctx, tx, op); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *Publisher) retainAcknowledgement(ctx context.Context, op *Publication) error {
	// A provider response may acknowledge an ID just as its deadline expires.
	// Retaining that evidence still requires the same live job ownership fence.
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	return p.writeClaim(recordCtx, op)
}

func (p *Publisher) Handle(ctx context.Context, j *jobqueue.Job) error {
	deadline := p.Deadline
	if deadline <= 0 {
		deadline = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	op, err := p.Get(ctx, j.ID)
	if err != nil {
		return errors.New("cannot read publication")
	}
	if op.Status != "queued" {
		return jobqueue.ErrOutcomeUnknown
	}
	artifact, files, err := Landing()
	if err != nil || artifact.SHA256 != op.ArtifactSHA256 {
		return errors.New("reviewed landing artifact changed; create a new review")
	}
	r, token, err := p.credentials(ctx, op)
	if err != nil {
		return errors.New("connection changed or is unavailable; create a new review")
	}
	projects, err := p.Service.API.Projects(ctx, token, r.TeamID)
	if err != nil {
		return errors.New("cannot verify new project destination")
	}
	for _, project := range projects {
		if project.Name == op.ProjectName {
			return errors.New("dedicated project name is already in use")
		}
	}
	op.Status, op.Message = "creating_project", "Creating the reviewed static project"
	if p.writeIntent(ctx, op, true) != nil {
		return jobqueue.ErrOutcomeUnknown
	}
	project, err := p.API.CreateStaticProject(ctx, token, r.TeamID, op.ProjectName)
	// Preserve an acknowledged ID even if the response cannot be fully checked.
	if project.ID != "" {
		op.ProjectID = project.ID
		if p.retainAcknowledgement(ctx, op) != nil {
			return jobqueue.ErrOutcomeUnknown
		}
	}
	if err != nil {
		if errors.Is(err, ErrMutationUnknown) || op.ProjectID != "" {
			return jobqueue.ErrOutcomeUnknown
		}
		return errors.New("vercel rejected creation of the static project")
	}
	if !validID(op.ProjectID) || project.Name != op.ProjectName {
		return jobqueue.ErrOutcomeUnknown
	}
	r, token, err = p.credentials(ctx, op)
	if err != nil {
		return jobqueue.ErrOutcomeUnknown
	}
	op.Status, op.Message = "submitting", "Submitting the reviewed landing files to Vercel"
	if p.writeIntent(ctx, op, true) != nil {
		return jobqueue.ErrOutcomeUnknown
	}
	result, err := p.API.CreateStaticDeployment(ctx, token, r.TeamID, StaticDeploymentRequest{ProjectID: op.ProjectID, Name: op.ProjectName, OperationID: op.ID, ArtifactSHA256: op.ArtifactSHA256, Files: files})
	if result.ID != "" {
		op.DeploymentID = result.ID
		op.Status, op.Message = "checking", "Vercel acknowledged the deployment; waiting for readiness"
		if p.retainAcknowledgement(ctx, op) != nil {
			return jobqueue.ErrOutcomeUnknown
		}
	}
	if err != nil {
		if errors.Is(err, ErrMutationUnknown) || op.DeploymentID != "" {
			return jobqueue.ErrOutcomeUnknown
		}
		return errors.New("vercel rejected the landing deployment; the created project ID is retained")
	}
	if !validID(op.DeploymentID) {
		return jobqueue.ErrOutcomeUnknown
	}
	poll := p.PollEvery
	if poll <= 0 {
		poll = 5 * time.Second
	}
	for {
		result, err = p.API.GetStaticDeployment(ctx, token, r.TeamID, op.DeploymentID)
		if err == nil && matchesPublication(op, result) {
			if result.State == "READY" && result.URL != "" {
				op.Status, op.URL, op.Message = "succeeded", result.URL, "Vercel reports the landing deployment ready"
				if p.writeClaim(ctx, op) != nil {
					return jobqueue.ErrOutcomeUnknown
				}
				return nil
			}
			if result.State == "ERROR" || result.State == "CANCELED" {
				return errors.New("vercel reported a failed or cancelled deployment")
			}
		}
		select {
		case <-ctx.Done():
			return jobqueue.ErrOutcomeUnknown
		case <-time.After(poll):
		}
	}
}

func matchesPublication(op *Publication, d StaticDeploymentResult) bool {
	return d.ID == op.DeploymentID && d.ProjectID == op.ProjectID && d.OperationID == op.ID && d.ArtifactSHA256 == op.ArtifactSHA256 && (d.OwnerID == "" || d.OwnerID == op.AccountID) && d.Target == "production"
}

func (p *Publisher) reconciliationCredentials(ctx context.Context, op *Publication) (*Record, string, error) {
	// Read reconciliation may use a replacement token for the SAME account.
	// Original version pins still apply to every provider mutation.
	r, err := p.Service.Store.Get(ctx, op.ConnectionID)
	if err != nil {
		return nil, "", err
	}
	if r.Identity.ID != op.AccountID || r.Identity.Scope != op.AccountScope || r.TeamID != op.TeamID {
		return nil, "", ErrConflict
	}
	token, err := p.Service.token(r)
	if err != nil {
		return nil, "", err
	}
	if err = p.Service.verifyIdentity(ctx, r, token); err != nil {
		return nil, "", err
	}
	return r, token, nil
}

// Check performs only provider reads. Missing history never authorizes replay.
func (p *Publisher) Check(ctx context.Context, id string) (*Publication, error) {
	op, err := p.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if op.Status != "needs_attention" {
		return op, nil
	}
	if op.ProjectID == "" {
		return op, nil
	} // Unknown project creation requires operator investigation.
	ctx, cancel := context.WithTimeout(ctx, SyncTimeout)
	defer cancel()
	r, token, err := p.reconciliationCredentials(ctx, op)
	if err != nil {
		return nil, err
	}
	var result StaticDeploymentResult
	if op.DeploymentID != "" {
		result, err = p.API.GetStaticDeployment(ctx, token, r.TeamID, op.DeploymentID)
	} else {
		var recent []Deployment
		recent, err = p.Service.API.Deployments(ctx, token, r.TeamID, op.ProjectID)
		if err != nil {
			return nil, err
		}
		var matched []StaticDeploymentResult
		for _, d := range recent {
			candidate, e := p.API.GetStaticDeployment(ctx, token, r.TeamID, d.ID)
			if e != nil {
				return nil, e
			}
			probe := *op
			probe.DeploymentID = d.ID
			if matchesPublication(&probe, candidate) {
				matched = append(matched, candidate)
			}
		}
		if len(matched) != 1 {
			return op, nil
		}
		result = matched[0]
		op.DeploymentID = result.ID
	}
	if err != nil {
		return nil, err
	}
	if !matchesPublication(op, result) {
		return nil, ErrConflict
	}
	if result.State == "READY" && result.URL != "" {
		op.Status, op.URL, op.Message = "succeeded", result.URL, "Provider identity and artifact verified; Vercel reports READY"
	} else if result.State == "ERROR" || result.State == "CANCELED" {
		op.Status, op.Message = "failed", "Provider identity verified; Vercel reports a failed or cancelled deployment"
	} else {
		op.Message = "Vercel acknowledged the deployment; readiness is still pending. Checking will not submit another deployment."
	}
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id=$1 FOR UPDATE`, id).Scan(&state); err != nil {
		return nil, err
	}
	if state != "needs_attention" {
		return nil, ErrConflict
	}
	if err = savePublication(ctx, tx, op); err != nil {
		return nil, err
	}
	if op.Status == "succeeded" || op.Status == "failed" {
		_, err = tx.ExecContext(ctx, `UPDATE jobs SET status=$2,last_error=$3,finished_at=NOW(),updated_at=NOW() WHERE id=$1`, id, op.Status, fmt.Sprintf("Provider read reconciliation by Cooker: %s", op.Message))
		if err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return p.Get(ctx, id)
}
