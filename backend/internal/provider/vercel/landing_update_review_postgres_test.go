package vercel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/jobqueue"
)

func landingUpdateReviewPostgresFixture(t *testing.T) (*LandingUpdateReviewService, *LandingAdoption, *Connection, *landingReviewReadFixture, *sql.DB) {
	t.Helper()
	adoptions, c, req, db := adoptionPostgresFixture(t)
	a, err := adoptions.Create(context.Background(), req, "fixture-adoption-admin")
	if err != nil {
		t.Fatal(err)
	}
	read := newLandingReviewReadFixture(a)
	s := &LandingUpdateReviewService{Service: adoptions.Service, Adoptions: adoptions.Store, Store: &PostgresLandingUpdateReviews{DB: db}, Projects: read, Deployments: read, Aliases: read, Publications: adoptions.Publications}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM vercel_landing_update_reviews WHERE adoption_id=$1`, a.ID)
	})
	return s, a, c, read, db
}

func TestPostgresLandingUpdateReviewFreshPoolAndLegacyPublicationIsolation(t *testing.T) {
	s, a, _, _, db := landingUpdateReviewPostgresFixture(t)
	r, err := s.Create(context.Background(), landingReviewRequest(a), "fixture-review-admin")
	if err != nil {
		t.Fatal(err)
	}
	var data []byte
	if err = db.QueryRowContext(context.Background(), `SELECT receipt FROM vercel_landing_update_reviews WHERE id=$1`, r.ID).Scan(&data); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), testToken) || strings.Contains(string(data), "synthetic-private") || strings.Contains(string(data), "fixture-review-admin") {
		t.Fatal("review receipt leaked excluded data", string(data))
	}
	pool, err := sql.Open("postgres", os.Getenv("COOKER_INTEGRATION_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	defer func() { _ = pool.Close() }()
	restarted := &PostgresLandingUpdateReviews{DB: pool}
	got, err := restarted.Get(context.Background(), r.ID)
	if err != nil || got.Actor != "fixture-review-admin" || got.ArtifactSHA256 != r.ArtifactSHA256 || got.AdoptionVersion != a.Version || got.BaselineDeploymentID != r.BaselineDeploymentID {
		t.Fatal("immutable review not durable", got, err)
	}
	legacy := &Publisher{DB: db, Service: s.Service, Jobs: jobqueue.NewPostgres(db), API: &staticFixture{}}
	if _, err = legacy.Get(context.Background(), r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("legacy publisher read separate review", err)
	}
	if _, err = legacy.Deploy(context.Background(), r.ID, "fixture-review-admin"); !errors.Is(err, ErrNotFound) {
		t.Fatal("legacy deployment admitted review ID", err)
	}
	var jobs, publications int
	if err = db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM jobs WHERE id=$1`, r.ID).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM vercel_publications WHERE id=$1`, r.ID).Scan(&publications); err != nil || jobs != 0 || publications != 0 {
		t.Fatal("review preparation created executable operation", jobs, publications, err)
	}
	// The derived JSON flag never becomes authority after a persisted edit.
	var decoded map[string]any
	if json.Unmarshal(data, &decoded) != nil {
		t.Fatal("bad receipt JSON")
	}
	decoded["submissionAvailable"] = true
	data, err = json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(context.Background(), `UPDATE vercel_landing_update_reviews SET receipt=$2 WHERE id=$1`, r.ID, data); err != nil {
		t.Fatal(err)
	}
	got, err = restarted.Get(context.Background(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(got)
	if err != nil || !strings.Contains(string(data), `"submissionAvailable":false`) {
		t.Fatal("stored flag advertised submission", string(data), err)
	}
}

func TestPostgresLandingUpdateReviewAtomicConnectionAndAdoptionFences(t *testing.T) {
	for _, change := range []string{"connection", "adoption"} {
		t.Run(change, func(t *testing.T) {
			s, a, c, read, db := landingUpdateReviewPostgresFixture(t)
			read.afterAliases = func() {
				var err error
				if change == "connection" {
					enabled := false
					_, err = s.Service.Update(context.Background(), c.ID, UpdateRequest{Version: c.Version, Enabled: &enabled})
				} else {
					stored, readErr := s.Adoptions.Get(context.Background(), a.ID)
					if readErr != nil {
						t.Fatal(readErr)
					}
					stored.Enabled = false
					err = s.Adoptions.Update(context.Background(), stored, stored.Version)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.Create(context.Background(), landingReviewRequest(a), "fixture-review-admin"); !errors.Is(err, ErrConflict) {
				t.Fatal("changed authority committed review", err)
			}
			var count int
			if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM vercel_landing_update_reviews WHERE adoption_id=$1`, a.ID).Scan(&count); err != nil || count != 0 {
				t.Fatal("stale review persisted", count, err)
			}
		})
	}
}
