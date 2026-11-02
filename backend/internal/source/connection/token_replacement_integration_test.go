//go:build integration

package connection_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/source/connection"
	storepg "github.com/santapong/cooker/internal/store/postgres"
)

// The exact disposable DB/ownership guard precedes migrations and writes.
// This verifies real persistence/CAS, not hosted provider permissions or rollout.
func TestSourceTokenReplacementPostgresPersistenceAndConcurrentCAS(t *testing.T) {
	dsn, marker := os.Getenv("COOKER_SOURCE_TOKEN_DATABASE_URL"), os.Getenv("COOKER_SOURCE_TOKEN_FIXTURE_ID")
	if dsn == "" {
		t.Skip("owned source-token PostgreSQL fixture required")
	}
	u, err := url.Parse(dsn)
	if _, parseErr := uuid.Parse(marker); err != nil || parseErr != nil || u.Host != "cooker-token-pg-"+marker+":5432" || u.Path != "/ctoken_"+strings.ReplaceAll(marker, "-", "") || u.User.Username() != "postgres" || u.RawQuery != "sslmode=disable" {
		t.Fatal("refusing non-owned source-token database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal("fixture SQL setup failed")
	}
	defer db.Close()
	var observed string
	if db.QueryRowContext(ctx, `SELECT shobj_description(oid,'pg_database') FROM pg_database WHERE datname=current_database()`).Scan(&observed) != nil || observed != "source-token-owned:"+marker {
		t.Fatal("fixture database ownership is unverified")
	}
	st, err := storepg.NewStore(ctx, dsn)
	if err != nil {
		t.Fatal("fixture migration failed")
	}
	st.Close()
	codec, err := crypto.NewCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal("fixture codec failed")
	}
	cs := &connection.Service{Store: &connection.Postgres{DB: db}, Codec: codec}
	create := func(name string) *connection.Connection {
		c := &connection.Connection{Name: name, Provider: "github", AuthMode: "token", Repositories: []string{"owned/repo"}, Enabled: true}
		if cs.CreateWithCredentials(ctx, c, connection.Credentials{Token: "synthetic-old-token", WebhookSecret: strings.Repeat("h", 32)}) != nil {
			t.Fatal("fixture source creation failed")
		}
		before, err := cs.Store.Get(ctx, c.ID)
		if err != nil {
			t.Fatal("fixture source read failed")
		}
		return before
	}

	t.Run("saved-token-survives-fresh-pool", func(t *testing.T) {
		before := create("Owned persisted source")
		cs.TokenProbe = func(context.Context, *connection.Connection) error { return nil }
		after, err := cs.ReplaceToken(ctx, before.ID, before.Version, "synthetic-new-token", nil)
		if err != nil || after.Version != before.Version+1 {
			t.Fatal("replacement failed")
		}
		freshDB, err := sql.Open("postgres", dsn)
		if err != nil {
			t.Fatal("fresh pool failed")
		}
		defer freshDB.Close()
		fresh := &connection.Service{Store: &connection.Postgres{DB: freshDB}, Codec: codec}
		saved, err := fresh.Store.Get(ctx, before.ID)
		if err != nil {
			t.Fatal("fresh stored source unavailable")
		}
		value, err := fresh.OpenCredentials(saved)
		if err != nil || value.Token != "synthetic-new-token" || value.WebhookSecret != strings.Repeat("h", 32) || saved.Version != after.Version || !saved.CreatedAt.Equal(before.CreatedAt) || !saved.Enabled {
			t.Fatal("replacement persistence lost identity or credentials")
		}
		var public string
		if freshDB.QueryRowContext(ctx, `SELECT configuration::text FROM source_connections WHERE id=$1`, before.ID).Scan(&public) != nil {
			t.Fatal("public metadata read failed")
		}
		for _, private := range []string{"synthetic-old-token", "synthetic-new-token", strings.Repeat("h", 32), "credential"} {
			if strings.Contains(public, private) {
				t.Fatal("private material in stored public metadata")
			}
		}
	})

	t.Run("failed-probe-leaves-exact-row", func(t *testing.T) {
		before := create("Owned refused source")
		cs.TokenProbe = func(context.Context, *connection.Connection) error { return errors.New("synthetic-private-error") }
		if _, err := cs.ReplaceToken(ctx, before.ID, before.Version, "synthetic-new-token", nil); !errors.Is(err, connection.ErrCredentialCheck) {
			t.Fatal("failed check was accepted")
		}
		after, err := cs.Store.Get(ctx, before.ID)
		if err != nil || after.Version != before.Version || !after.UpdatedAt.Equal(before.UpdatedAt) || string(after.Credential) != string(before.Credential) {
			t.Fatal("failed check changed persisted row")
		}
	})

	t.Run("two-successful-checks-produce-one-versioned-write", func(t *testing.T) {
		before := create("Owned concurrent source")
		otherDB, err := sql.Open("postgres", dsn)
		if err != nil {
			t.Fatal("second writer failed")
		}
		defer otherDB.Close()
		other := &connection.Service{Store: &connection.Postgres{DB: otherDB}, Codec: codec}
		arrived := make(chan struct{}, 2)
		release := make(chan struct{})
		probe := func(ctx context.Context, _ *connection.Connection) error {
			arrived <- struct{}{}
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		cs.TokenProbe, other.TokenProbe = probe, probe
		errs := make([]error, 2)
		var wg sync.WaitGroup
		for i, s := range []*connection.Service{cs, other} {
			wg.Add(1)
			go func(i int, s *connection.Service) {
				defer wg.Done()
				_, errs[i] = s.ReplaceToken(ctx, before.ID, before.Version, "synthetic-new-token", nil)
			}(i, s)
		}
		for i := 0; i < 2; i++ {
			select {
			case <-arrived:
			case <-ctx.Done():
				close(release)
				wg.Wait()
				t.Fatal("concurrent writers did not reach checks")
			}
		}
		close(release)
		wg.Wait()
		ok, conflict := 0, 0
		for _, err := range errs {
			if err == nil {
				ok++
			} else if errors.Is(err, connection.ErrConflict) {
				conflict++
			}
		}
		after, err := cs.Store.Get(ctx, before.ID)
		if ok != 1 || conflict != 1 || err != nil || after.Version != before.Version+1 {
			t.Fatal("concurrent PostgreSQL writers lost version fencing")
		}
	})
}
