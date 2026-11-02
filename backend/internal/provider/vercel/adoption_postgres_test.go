package vercel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	storepg "github.com/santapong/cooker/internal/store/postgres"
)

func adoptionPostgresFixture(t *testing.T) (*LandingAdoptionService, *Connection, LandingAdoptionRequest, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("COOKER_INTEGRATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("COOKER_INTEGRATION_DATABASE_URL required")
	}
	ctx := context.Background()
	migrated, err := storepg.NewStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err = migrated.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(1)
	svc, _, c := newFixture(t, &Postgres{DB: db})
	c = selectProjects(t, svc, c, "prj_one")
	assessment, err := staticProjectAssessment([]byte(staticAssessmentWire()))
	if err != nil {
		t.Fatal(err)
	}
	publication := &Publication{ID: "publication_prior", AccountScope: "personal", AccountID: "user_fixture", ProjectID: "prj_one", DeploymentID: "dpl_prior", Target: "production", Status: "succeeded", ArtifactSHA256: strings.Repeat("a", 64)}
	s := &LandingAdoptionService{Service: svc, Store: &PostgresLandingAdoptions{DB: db}, Projects: adoptionProjectFixture{assessment: assessment}, Publications: adoptionPublicationFixture{publication: publication}}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM vercel_landing_adoptions WHERE connection_id=$1`, c.ID)
		_, _ = db.ExecContext(ctx, `DELETE FROM vercel_connections WHERE id=$1`, c.ID)
		_ = db.Close()
	})
	return s, c, LandingAdoptionRequest{ConnectionID: c.ID, ConnectionVersion: c.Version, ProjectID: "prj_one", ProvenancePublicationID: publication.ID, ConfirmManualUpdates: true}, db
}

func TestPostgresLandingAdoptionFreshPoolPersistenceAndSafeProjection(t *testing.T) {
	s, c, req, db := adoptionPostgresFixture(t)
	a, err := s.Create(context.Background(), req, "fixture-admin")
	if err != nil {
		t.Fatal(err)
	}
	var data []byte
	if err = db.QueryRowContext(context.Background(), `SELECT binding FROM vercel_landing_adoptions WHERE id=$1`, a.ID).Scan(&data); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), testToken) || strings.Contains(string(data), "synthetic-private") || strings.Contains(string(data), `"buildCommand":`) || strings.Contains(string(data), `"rootDirectory":`) {
		t.Fatal("raw credential or provider settings persisted", string(data))
	}
	fresh, err := sql.Open("postgres", os.Getenv("COOKER_INTEGRATION_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	fresh.SetMaxOpenConns(1)
	defer func() { _ = fresh.Close() }()
	restarted := &PostgresLandingAdoptions{DB: fresh}
	got, err := restarted.Get(context.Background(), a.ID)
	if err != nil || got.ConnectionVersion != c.Version || got.ProjectID != req.ProjectID || got.ProvenanceDeploymentID != "dpl_prior" || got.Assessment == nil || !got.Assessment.Eligible {
		t.Fatal("adoption not durable across pool boundary", got, err)
	}
	got.PermittedActions[0] = "delete_project"
	unchanged, err := restarted.Get(context.Background(), a.ID)
	if err != nil || unchanged.PermittedActions[0] != LandingAdoptionAction {
		t.Fatal("mutated fetched authority changed persistence", unchanged, err)
	}
	// Revocation remains possible without a connection and retains provenance.
	if err = s.Service.Delete(context.Background(), c.ID, c.Version); err != nil {
		t.Fatal(err)
	}
	disabled, err := s.Update(context.Background(), a.ID, LandingAdoptionUpdate{Version: a.Version, Enabled: false}, "fixture-admin")
	if err != nil || disabled.Enabled || disabled.Version != 2 {
		t.Fatal("disconnect prevented durable revocation", disabled, err)
	}
	got, err = restarted.Get(context.Background(), a.ID)
	if err != nil || got.Enabled || got.ProvenancePublicationID != req.ProvenancePublicationID {
		t.Fatal("revocation lost historical evidence", got, err)
	}
}

func TestPostgresLandingAdoptionSlowAssessmentCannotCommitChangedConnection(t *testing.T) {
	s, c, req, _ := adoptionPostgresFixture(t)
	f := s.Projects.(adoptionProjectFixture)
	f.entered, f.release = make(chan struct{}), make(chan struct{})
	s.Projects = f
	done := make(chan error, 1)
	go func() { _, err := s.Create(context.Background(), req, "fixture-admin"); done <- err }()
	<-f.entered
	selection := []string{"prj_two"}
	if _, err := s.Service.Update(context.Background(), c.ID, UpdateRequest{Version: c.Version, ProjectIDs: &selection}); err != nil {
		t.Fatal(err)
	}
	close(f.release)
	if err := <-done; !errors.Is(err, ErrConflict) {
		t.Fatal("stale assessment committed after selection change", err)
	}
}

func TestPostgresLandingAdoptionConcurrentConnectionsShareOneDestination(t *testing.T) {
	s, _, req, db := adoptionPostgresFixture(t)
	other, err := s.Service.Create(context.Background(), CreateRequest{Name: "overlapping PostgreSQL fixture", Token: testToken})
	if err != nil {
		t.Fatal(err)
	}
	other = selectProjects(t, s.Service, other, "prj_one")
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM vercel_landing_adoptions WHERE connection_id=$1`, other.ID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM vercel_connections WHERE id=$1`, other.ID)
	})
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
		t.Fatal("unique destination admitted concurrent writers", success, conflicts)
	}
	var count int
	if err = db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM vercel_landing_adoptions WHERE connection_id IN ($1,$2) AND enabled`, req.ConnectionID, other.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("independent SQL uniqueness check failed", count, err)
	}
}

func TestPostgresLandingAdoptionRejectsTamperedIndexedIdentity(t *testing.T) {
	s, _, req, db := adoptionPostgresFixture(t)
	a, err := s.Create(context.Background(), req, "fixture-admin")
	if err != nil {
		t.Fatal(err)
	}
	var data []byte
	if err = db.QueryRowContext(context.Background(), `SELECT binding FROM vercel_landing_adoptions WHERE id=$1`, a.ID).Scan(&data); err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if json.Unmarshal(data, &value) != nil {
		t.Fatal("bad fixture JSON")
	}
	value["connectionVersion"] = a.ConnectionVersion + 1
	data, err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(context.Background(), `UPDATE vercel_landing_adoptions SET binding=$2 WHERE id=$1`, a.ID, data); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(context.Background(), a.ID); !errors.Is(err, ErrInvalid) {
		t.Fatal("indexed/binding identity disagreement accepted", err)
	}
}
