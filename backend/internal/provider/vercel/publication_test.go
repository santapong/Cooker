package vercel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/store"
	storepg "github.com/santapong/cooker/internal/store/postgres"
)

type staticFixture struct {
	projectCalls, deploymentCalls int
	projectError, deploymentError error
	ackProject, ackDeployment     bool
	result                        StaticDeploymentResult
	projectCancel                 context.CancelFunc
}

func (f *staticFixture) CreateStaticProject(_ context.Context, _ string, _ string, name string) (Project, error) {
	f.projectCalls++
	if f.projectCancel != nil {
		f.projectCancel()
	}
	if f.projectError != nil && !f.ackProject {
		return Project{}, f.projectError
	}
	return Project{ID: "prj_publication", Name: name}, f.projectError
}

func TestPublicationRetainsAcknowledgedProjectAfterCancellation(t *testing.T) {
	p, api, c := publisherFixture(t)
	op := reviewFixture(t, p, c)
	j, claimCtx := claimFixture(t, p, op)
	ctx, cancel := context.WithCancel(claimCtx)
	defer cancel()
	api.projectCancel = cancel
	api.projectError = ErrMutationUnknown
	api.ackProject = true
	if err := p.Handle(ctx, j); !errors.Is(err, jobqueue.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	got, err := p.Get(context.Background(), op.ID)
	if err != nil || got.ProjectID == "" {
		t.Fatal("acknowledged project lost when context ended", got, err)
	}
}
func (f *staticFixture) CreateStaticDeployment(_ context.Context, _ string, _ string, r StaticDeploymentRequest) (StaticDeploymentResult, error) {
	f.deploymentCalls++
	f.result = StaticDeploymentResult{ID: "dpl_publication", ProjectID: r.ProjectID, OperationID: r.OperationID, ArtifactSHA256: r.ArtifactSHA256, OwnerID: "user_fixture", State: "READY", Target: "production", URL: "https://publication.vercel.app"}
	if f.deploymentError != nil && !f.ackDeployment {
		return StaticDeploymentResult{}, f.deploymentError
	}
	return f.result, f.deploymentError
}
func (f *staticFixture) GetStaticDeployment(context.Context, string, string, string) (StaticDeploymentResult, error) {
	return f.result, nil
}

func publisherFixture(t *testing.T) (*Publisher, *staticFixture, *Connection) {
	t.Helper()
	dsn := os.Getenv("COOKER_INTEGRATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("COOKER_INTEGRATION_DATABASE_URL required")
	}
	ctx := context.Background()
	st, err := storepg.NewStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	svc, _, c := newFixture(t, &Postgres{DB: db})
	api := &staticFixture{}
	jobs := jobqueue.NewPostgres(db)
	jobs.OnEnqueue = PublicationAdmission
	jobs.OnTransition = PublicationTransition
	p := &Publisher{DB: db, Service: svc, Jobs: jobs, API: api, PollEvery: time.Millisecond, Deadline: time.Second}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM jobs WHERE id IN (SELECT id FROM vercel_publications WHERE connection_id=$1)`, c.ID)
		_, _ = db.ExecContext(ctx, `DELETE FROM vercel_publications WHERE connection_id=$1`, c.ID)
		_, _ = db.ExecContext(ctx, `DELETE FROM vercel_connections WHERE id=$1`, c.ID)
		_ = db.Close()
	})
	return p, api, c
}
func reviewFixture(t *testing.T, p *Publisher, c *Connection) *Publication {
	t.Helper()
	op, err := p.Review(context.Background(), c.ID, c.Version, "fixture-admin")
	if err != nil {
		t.Fatal(err)
	}
	return op
}
func claimFixture(t *testing.T, p *Publisher, op *Publication) (*jobqueue.Job, context.Context) {
	t.Helper()
	if _, err := p.Deploy(context.Background(), op.ID, "fixture-admin"); err != nil {
		t.Fatal(err)
	}
	j, err := p.Jobs.Dequeue(context.Background(), "publication-worker", []string{JobKindStatic})
	if err != nil || j == nil {
		t.Fatalf("claim: %v %v", j, err)
	}
	return j, store.WithRunClaim(context.Background(), j.ID, j.LockedBy, nil)
}

func TestLandingOnlyPublicAllowlist(t *testing.T) {
	a, files, err := Landing()
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"index.html":                          true,
		"styles.css":                          true,
		"main.js":                             true,
		"assets/tokens.css":                   true,
		"assets/cooker-theme.css":             true,
		"assets/favicon.svg":                  true,
		"assets/cooker-open-workbench-v1.png": true,
	}
	if a.FileCount != len(allowed) || a.Bytes <= 0 || len(a.SHA256) != 64 {
		t.Fatalf("bad artifact: %+v", a)
	}
	if err := validateStaticFiles(files); err != nil {
		t.Fatal("embedded artifact cannot publish", err)
	}
	for _, f := range files {
		if !allowed[f.Path] {
			t.Fatal("file outside the landing-only allowlist", f.Path)
		}
		delete(allowed, f.Path)
	}
	if len(allowed) != 0 {
		t.Fatal("required landing assets are missing", allowed)
	}
	b, _, _ := Landing()
	if a != b {
		t.Fatal("artifact is not stable")
	}
}

func TestLandingPreviewExcludesPrototypePagesAndPrivateMetadata(t *testing.T) {
	preview := LandingPreview()
	for _, path := range []string{
		"login.html", "login-ideas.html", "login.css", "login.js",
		"docs.html", "docs.css", "docs.js",
		"workspace.html", "workspace.css", "workspace.js", "workspace-dag.js",
		"workspace-dag.css", "workspace-examples.js", "workspace-n8n.js", "workspace-n8n.css",
		"assets/login-console.jpg", "assets/login-focused.jpg", "assets/login-paper.jpg",
		"assets/login-split.jpg", "assets/login-workshop.jpg", "assets/login-workshop-art.png",
		"examples/n8n-queue/README.md", "examples/n8n-queue/compose.yaml", "examples/n8n-queue/env.example",
		"SOURCE.json", "manifest.json", "build.py", "build_landing.py", ".git/config",
	} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			preview.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/landing-preview/"+path, nil))
			if response.Code != http.StatusNotFound {
				t.Fatalf("excluded path %s returned %d", path, response.Code)
			}
		})
	}
}

func TestPublicationDurableReviewConcurrentAdmissionAndReady(t *testing.T) {
	p, api, c := publisherFixture(t)
	op := reviewFixture(t, p, c)
	if op.Target != "production" || !strings.HasPrefix(op.ProjectName, "cooker-landing-") || !strings.Contains(op.Message, "only the Cooker landing page") || !strings.Contains(op.Message, "files are excluded") {
		t.Fatal("review missing effects", op)
	}
	if _, err := p.Deploy(context.Background(), op.ID, "another-admin"); !errors.Is(err, ErrConflict) {
		t.Fatal("review actor bypass")
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := p.Deploy(context.Background(), op.ID, "fixture-admin"); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	_ = p.DB.QueryRow(`SELECT count(*) FROM jobs WHERE id=$1`, op.ID).Scan(&count)
	if count != 1 {
		t.Fatal("duplicate job", count)
	}
	j, ctx := claimFixture(t, p, op)
	if err := p.Handle(ctx, j); err != nil {
		t.Fatal(err)
	}
	if err := p.Jobs.Complete(ctx, j.LockedBy, j.ID); err != nil {
		t.Fatal(err)
	}
	restarted := *p
	got, err := restarted.Get(context.Background(), op.ID)
	if err != nil || got.Status != "succeeded" || got.DeploymentID == "" || got.ProjectID == "" || got.URL == "" {
		t.Fatalf("receipt lost: %+v %v", got, err)
	}
	if _, err := p.Deploy(context.Background(), op.ID, "fixture-admin"); err != nil || api.projectCalls != 1 || api.deploymentCalls != 1 {
		t.Fatal("completed intent replayed")
	}
	data, _ := json.Marshal(got)
	if strings.Contains(string(data), testToken) || strings.Contains(string(data), "fixture-admin") {
		t.Fatal("credential/actor exposed")
	}
}

func TestPublicationAdmissionRejectsRotationAndExpiry(t *testing.T) {
	p, _, c := publisherFixture(t)
	op := reviewFixture(t, p, c)
	name := "Rotated label"
	if _, err := p.Service.Update(context.Background(), c.ID, UpdateRequest{Version: c.Version, Name: &name}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Deploy(context.Background(), op.ID, "fixture-admin"); !errors.Is(err, ErrConflict) {
		t.Fatal("stale version admitted", err)
	}
	var count int
	_ = p.DB.QueryRow(`SELECT count(*) FROM jobs WHERE id=$1`, op.ID).Scan(&count)
	if count != 0 {
		t.Fatal("failed admission left job")
	}
	r, err := p.Service.Store.Get(context.Background(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	op = reviewFixture(t, p, &r.Connection)
	op.ExpiresAt = time.Now().Add(-time.Minute)
	data, _ := json.Marshal(op)
	_, err = p.DB.Exec(`UPDATE vercel_publications SET receipt=$2 WHERE id=$1`, op.ID, data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Deploy(context.Background(), op.ID, "fixture-admin"); !errors.Is(err, ErrConflict) {
		t.Fatal("expired review admitted")
	}
}

func TestPublicationUnknownProjectDoesNotRetry(t *testing.T) {
	p, api, c := publisherFixture(t)
	api.projectError = ErrMutationUnknown
	op := reviewFixture(t, p, c)
	j, ctx := claimFixture(t, p, op)
	if err := p.Handle(ctx, j); !errors.Is(err, jobqueue.ErrOutcomeUnknown) {
		t.Fatal("unknown project became failure", err)
	}
	leases := p.Jobs.(jobqueue.LeaseStore)
	if err := leases.Suspend(ctx, j.LockedBy, j.ID, "unknown project outcome"); err != nil {
		t.Fatal(err)
	}
	got, err := p.Check(context.Background(), op.ID)
	if err != nil || got.Status != "needs_attention" || got.ProjectID != "" {
		t.Fatal(got, err)
	}
	if _, err := p.Deploy(context.Background(), op.ID, "fixture-admin"); err != nil {
		t.Fatal(err)
	}
	if api.projectCalls != 1 || api.deploymentCalls != 0 {
		t.Fatal("unknown project replayed")
	}
}

func TestPublicationAcknowledgementRecoveryFencingAndReadOnlyCheck(t *testing.T) {
	p, api, c := publisherFixture(t)
	api.deploymentError = ErrMutationUnknown
	api.ackDeployment = true
	op := reviewFixture(t, p, c)
	j, ctx := claimFixture(t, p, op)
	if err := p.Handle(ctx, j); !errors.Is(err, jobqueue.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	op, err := p.Get(context.Background(), j.ID)
	if err != nil || op.DeploymentID == "" {
		t.Fatal("ack ID lost", err)
	}
	leases := p.Jobs.(jobqueue.LeaseStore)
	recovered, err := leases.RecoverExpired(context.Background(), time.Now().Add(time.Minute))
	if err != nil || len(recovered) < 1 {
		t.Fatal("recovery failed", err)
	}
	if err = p.writeClaim(ctx, op); !errors.Is(err, ErrConflict) {
		t.Fatal("revoked worker wrote", err)
	}
	api.result.ArtifactSHA256 = "wrong-artifact"
	rotated := testToken + "-replacement"
	if _, err = p.Service.Update(context.Background(), c.ID, UpdateRequest{Version: c.Version, Token: &rotated}); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Check(context.Background(), op.ID); !errors.Is(err, ErrConflict) {
		t.Fatal("foreign artifact reconciled", err)
	}
	api.result.ArtifactSHA256 = op.ArtifactSHA256
	op, err = p.Check(context.Background(), op.ID)
	if err != nil || op.Status != "succeeded" {
		t.Fatal("provider read reconciliation failed", op, err)
	}
	job, err := p.Jobs.Get(context.Background(), j.ID)
	if err != nil || job.Status != jobqueue.StatusSucceeded {
		t.Fatal("receipt/job not atomic", err)
	}
	if api.projectCalls != 1 || api.deploymentCalls != 1 {
		t.Fatal("reconciliation wrote to provider")
	}
}

type blockedProjectInventory struct {
	API
	entered, release chan struct{}
}

func (b blockedProjectInventory) Projects(ctx context.Context, token, teamID string) ([]Project, error) {
	close(b.entered)
	select {
	case <-b.release:
		return b.API.Projects(ctx, token, teamID)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestPublicationRechecksConnectionAfterSlowInventory(t *testing.T) {
	p, api, c := publisherFixture(t)
	op := reviewFixture(t, p, c)
	j, ctx := claimFixture(t, p, op)
	b := blockedProjectInventory{API: p.Service.API, entered: make(chan struct{}), release: make(chan struct{})}
	p.Service.API = b
	done := make(chan error, 1)
	go func() { done <- p.Handle(ctx, j) }()
	<-b.entered
	enabled := false
	if _, err := p.Service.Update(context.Background(), c.ID, UpdateRequest{Version: c.Version, Enabled: &enabled}); err != nil {
		t.Fatal(err)
	}
	close(b.release)
	if err := <-done; err == nil {
		t.Fatal("disabled connection executed")
	}
	if api.projectCalls != 0 || api.deploymentCalls != 0 {
		t.Fatal("revoked inventory used to publish")
	}
}

type staticRecoveryInventory struct {
	API
	deploymentID string
	calls        int
}

func (s *staticRecoveryInventory) Deployments(context.Context, string, string, string) ([]Deployment, error) {
	s.calls++
	return []Deployment{{ID: s.deploymentID}}, nil
}

func TestPublicationReconciliationRetainsBuildingID(t *testing.T) {
	p, api, c := publisherFixture(t)
	api.deploymentError = ErrMutationUnknown
	op := reviewFixture(t, p, c)
	j, ctx := claimFixture(t, p, op)
	if err := p.Handle(ctx, j); !errors.Is(err, jobqueue.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	if err := p.Jobs.(jobqueue.LeaseStore).Suspend(ctx, j.LockedBy, j.ID, "deployment response was lost"); err != nil {
		t.Fatal(err)
	}
	history := &staticRecoveryInventory{API: p.Service.API, deploymentID: api.result.ID}
	p.Service.API = history
	api.result.State = "BUILDING"
	op, err := p.Check(context.Background(), op.ID)
	if err != nil || op.Status != "needs_attention" || op.DeploymentID == "" {
		t.Fatal("building ID not discovered", op, err)
	}
	reloaded, err := p.Get(context.Background(), op.ID)
	if err != nil || reloaded.DeploymentID != op.DeploymentID {
		t.Fatal("building ID not durable", err)
	}
	api.result.State = "READY"
	op, err = p.Check(context.Background(), op.ID)
	if err != nil || op.Status != "succeeded" || history.calls != 1 {
		t.Fatal("exact ID recovery failed", op, err, history.calls)
	}
	if api.deploymentCalls != 1 {
		t.Fatal("recovery submitted twice")
	}
}

func TestPublicationPartialProjectAckAndArtifactDrift(t *testing.T) {
	p, api, c := publisherFixture(t)
	api.projectError = ErrMutationUnknown
	api.ackProject = true
	op := reviewFixture(t, p, c)
	j, ctx := claimFixture(t, p, op)
	if err := p.Handle(ctx, j); !errors.Is(err, jobqueue.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	got, err := p.Get(context.Background(), op.ID)
	if err != nil || got.ProjectID == "" || api.deploymentCalls != 0 {
		t.Fatal("project ack lost", got, err)
	}
	if err = p.Jobs.(jobqueue.LeaseStore).Suspend(ctx, j.LockedBy, j.ID, "project creation uncertain"); err != nil {
		t.Fatal(err)
	}
	// A new intent has its own identity; changing its reviewed artifact fails
	// before creating any project even after a real JSONB round trip.
	op = reviewFixture(t, p, c)
	op.ArtifactSHA256 = strings.Repeat("a", 64)
	data, _ := json.Marshal(op)
	_, _ = p.DB.Exec(`UPDATE vercel_publications SET receipt=$2 WHERE id=$1`, op.ID, data)
	// Release the earlier resource lock only with explicit operator evidence.
	if err = p.Jobs.(jobqueue.LeaseStore).Resolve(context.Background(), j.ID, jobqueue.StatusCancelled, "fixture operator inspected the external project"); err != nil {
		t.Fatal(err)
	}
	j, ctx = claimFixture(t, p, op)
	if err = p.Handle(ctx, j); err == nil || errors.Is(err, jobqueue.ErrOutcomeUnknown) {
		t.Fatal("drift did not fail before mutation", err)
	}
	if api.projectCalls != 1 {
		t.Fatal("drift mutated provider")
	}
}
