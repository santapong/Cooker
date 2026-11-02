package vercel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	storepg "github.com/santapong/cooker/internal/store/postgres"
)

type postgresAliasesFixture struct {
	*detailFixture
	aliasAt    time.Time
	aliasError error
}

func (f *postgresAliasesFixture) DeploymentAliases(_ context.Context, _, _, id string) (*DeploymentAliasDetails, error) {
	if f.aliasError != nil {
		return nil, f.aliasError
	}
	return &DeploymentAliasDetails{DeploymentID: id, Aliases: []string{"https://production.example.com"}, ObservedAt: f.aliasAt}, nil
}

func TestPostgresAliasSnapshotSurvivesRestartAndIndependentFailure(t *testing.T) {
	dsn := os.Getenv("COOKER_INTEGRATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("COOKER_INTEGRATION_DATABASE_URL required")
	}
	ctx := context.Background()
	st, err := storepg.NewStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc, details, c := detailService(t, &Postgres{DB: db}, 1)
	defer func() { _, _ = db.ExecContext(ctx, `DELETE FROM vercel_connections WHERE id=$1`, c.ID) }()
	at := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	api := &postgresAliasesFixture{detailFixture: details, aliasAt: at}
	svc.API = api
	if _, err = svc.Sync(ctx, c.ID, true); err != nil {
		t.Fatal(err)
	}
	restarted := &Service{Store: &Postgres{DB: db}, Codec: svc.Codec, API: api}
	current, err := restarted.Store.Get(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	production := current.Snapshot.Projects[0].Details.ProductionDeployment
	if production.Details.AliasDetails == nil || production.Details.AliasDetails.DeploymentID != production.ID || !production.Details.AliasDetails.ObservedAt.Equal(at) || len(production.Details.AliasDetails.Aliases) != 1 {
		t.Fatal("alias observation not persisted on exact production deployment")
	}
	api.aliasError = errors.New("synthetic-private-provider-response-canary")
	if _, err = restarted.Sync(ctx, c.ID, true); err != nil {
		t.Fatal(err)
	}
	current, err = restarted.Store.Get(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	production = current.Snapshot.Projects[0].Details.ProductionDeployment
	alias := production.Details.AliasDetails
	if alias.Error == "" || !alias.ObservedAt.Equal(at) || len(alias.Aliases) != 1 || !production.Details.ObservedAt.After(at) || production.Details.Error != "" || current.LastSuccessAt == nil {
		t.Fatal("optional alias failure erased its own clock or fresh core/deployment data")
	}
	stored, err := json.Marshal(current.Snapshot)
	if err != nil || strings.Contains(string(stored), "synthetic-private-provider-response-canary") {
		t.Fatal("raw provider failure reached snapshot")
	}
	// Absence remains backward readable, without inventing a new observation.
	production.Details.AliasDetails = nil
	encoded, err := json.Marshal(current.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE vercel_connections SET snapshot=$2::jsonb WHERE id=$1`, c.ID, string(encoded)); err != nil {
		t.Fatal(err)
	}
	old, err := restarted.Store.Get(ctx, c.ID)
	if err != nil || old.Snapshot.Projects[0].Details.ProductionDeployment.Details.AliasDetails != nil {
		t.Fatal("old snapshot compatibility lost")
	}
}
