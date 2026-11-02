//go:build integration

package neon

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/store/postgres"
)

// No SQL or migration may run before both the exact disposable DSN and its
// database comment are checked. This never falls back to an operator DSN.
func ownedNeonPostgres(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dsn := os.Getenv("COOKER_NEON_TEST_DSN")
	if dsn == "" {
		t.Skip("exact UUID-owned Neon PostgreSQL fixture required")
	}
	owner := os.Getenv("COOKER_NEON_TEST_OWNER")
	id, err := uuid.Parse(owner)
	if err != nil || id == uuid.Nil || id.String() != owner {
		t.Fatal("canonical nonzero fixture UUID required")
	}
	expectedDatabase := "cooker_neon_" + strings.ReplaceAll(owner, "-", "")
	expected := "postgres://postgres@cooker-neon-pg-" + owner + ":5432/" + expectedDatabase + "?sslmode=disable"
	u, err := url.Parse(dsn)
	if err != nil || dsn != expected || u.Scheme != "postgres" || u.User == nil || u.User.Username() != "postgres" || u.RawQuery != "sslmode=disable" || u.RawPath != "" || u.Fragment != "" {
		t.Fatal("only the exact UUID-owned Neon fixture DSN is permitted")
	}
	if _, hasPassword := u.User.Password(); hasPassword {
		t.Fatal("fixture DSN must not contain a password")
	}
	// The input guard requires exactly sslmode=disable. Bound every subsequent
	// connection by deriving driver options only after that guard succeeds.
	query := u.Query()
	query.Set("connect_timeout", "5")
	query.Set("options", "-c statement_timeout=5000 -c lock_timeout=1000 -c idle_in_transaction_session_timeout=10000")
	u.RawQuery = query.Encode()
	boundedDSN := u.String()
	t.Setenv("PGPASSWORD", "")
	t.Setenv("PGPASSFILE", os.DevNull)
	t.Setenv("COOKER_DB_MAX_OPEN_CONNS", "2")
	t.Setenv("COOKER_DB_MAX_IDLE_CONNS", "1")
	probe, err := sql.Open("postgres", boundedDSN)
	if err != nil {
		t.Fatal("fixture marker pool could not be opened")
	}
	probe.SetMaxOpenConns(1)
	probe.SetMaxIdleConns(0)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	var database, actor, marker string
	err = probe.QueryRowContext(ctx, `SELECT current_database(),current_user,coalesce(shobj_description(oid,'pg_database'),'') FROM pg_database WHERE datname=current_database()`).Scan(&database, &actor, &marker)
	cancel()
	closeErr := probe.Close()
	if err != nil || closeErr != nil || database != expectedDatabase || actor != "postgres" || marker != "cooker-owned-neon:"+owner {
		t.Fatal("owned Neon database marker mismatch")
	}
	// NewStore can apply all embedded migrations. It is intentionally below the
	// ownership read and receives only the validated, query-bounded DSN.
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	store, err := postgres.NewStore(ctx, boundedDSN)
	cancel()
	if err != nil {
		t.Fatal("owned Neon schema initialization failed")
	}
	if err = store.Close(); err != nil {
		t.Fatal("owned schema pool close failed")
	}
	return freshNeonPostgresPool(t, boundedDSN), boundedDSN
}

func freshNeonPostgresPool(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal("owned Neon pool could not be opened")
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(0)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

type neonPGStored struct {
	credential, configuration, snapshot []byte
	version                             int
	lastSuccess                         sql.NullTime
}

func readNeonPGStored(t *testing.T, db *sql.DB, id string) neonPGStored {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var row neonPGStored
	if db.QueryRowContext(ctx, `SELECT credential,configuration::text,snapshot::text,version,last_success_at FROM neon_connections WHERE id=$1`, id).Scan(&row.credential, &row.configuration, &row.snapshot, &row.version, &row.lastSuccess) != nil {
		t.Fatal("owned Neon raw row read failed")
	}
	return row
}

func requireNeonPGCode(t *testing.T, err error, code string) {
	t.Helper()
	var safe *RequestError
	if !errors.As(err, &safe) || safe.Code != code {
		t.Fatal("unexpected Neon conditional-operation outcome")
	}
}

func createNeonPGConnection(t *testing.T, db *sql.DB, selection []string) (*ConnectionService, *crypto.Codec, *Connection) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal("synthetic encryption-key generation failed")
	}
	codec, err := crypto.NewCodec(key)
	if err != nil {
		t.Fatal("real encryption codec initialization failed")
	}
	service := &ConnectionService{Store: &PostgresConnections{DB: db}, Codec: codec, API: &connectionAPIFixture{}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	created, err := service.Create(ctx, CreateConnectionRequest{Name: "Owned Neon fixture", OrganizationID: "org-fixture", AccountMode: "unknown", APIKey: connectionFixtureKey, ProjectIDs: selection, AcknowledgeKeyPermissions: true})
	if err != nil || created == nil {
		t.Fatal("real Neon onboarding did not persist")
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := db.ExecContext(cleanupCtx, `DELETE FROM neon_connections WHERE id=$1`, created.ID); err != nil && !errors.Is(err, sql.ErrConnDone) {
			// A test may close its original pool to force a new one. Its exact row
			// cleanup is then registered on the fresh pool before that close.
			if err.Error() != "sql: database is closed" {
				t.Error("owned Neon row cleanup failed")
			}
		}
	})
	return service, codec, created
}

func assertNeonPGSafeJSON(t *testing.T, value any, forbidden ...string) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal("public Neon JSON could not be encoded")
	}
	for _, secret := range forbidden {
		if secret != "" && bytes.Contains(data, []byte(secret)) {
			t.Fatal("private credential material entered Neon JSON")
		}
	}
	for _, field := range []string{`"credential"`, `"lease"`, `"apiKey"`, `"key"`} {
		if bytes.Contains(data, []byte(field)) {
			t.Fatal("private field entered Neon JSON")
		}
	}
}

func TestNeonPostgresRealCryptoSyncAndFreshPoolPersistence(t *testing.T) {
	db, dsn := ownedNeonPostgres(t)
	service, codec, created := createNeonPGConnection(t, db, []string{"project-one"})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	synced, err := service.Sync(ctx, created.ID)
	if err != nil || synced.Snapshot == nil || synced.State != "observed" || synced.LastSuccessAt == nil || synced.Syncing {
		t.Fatal("real Neon sync did not retain a completed observation")
	}
	before := readNeonPGStored(t, db, created.ID)
	if len(before.credential) == 0 || bytes.Contains(before.credential, []byte(connectionFixtureKey)) || bytes.Contains(before.configuration, []byte(connectionFixtureKey)) || bytes.Contains(before.snapshot, []byte(connectionFixtureKey)) {
		t.Fatal("Neon key was stored outside encrypted credential bytes")
	}
	plain, err := codec.Open(before.credential)
	if err != nil {
		t.Fatal("stored Neon ciphertext could not be decrypted by real codec")
	}
	defer clear(plain)
	var envelope connectionCredentialEnvelope
	if json.Unmarshal(plain, &envelope) != nil || envelope.Purpose != ConnectionCredentialPurpose || envelope.ConnectionID != created.ID || envelope.OrganizationID != created.OrganizationID || envelope.Key != connectionFixtureKey {
		t.Fatal("persisted credential lost exact connection/organization binding")
	}
	assertNeonPGSafeJSON(t, synced, connectionFixtureKey, string(plain))
	fresh := freshNeonPostgresPool(t, dsn)
	t.Cleanup(func() {
		cleanupCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		if _, err := fresh.ExecContext(cleanupCtx, `DELETE FROM neon_connections WHERE id=$1`, created.ID); err != nil {
			t.Error("fresh-pool exact row cleanup failed")
		}
	})
	if err = db.Close(); err != nil {
		t.Fatal("original Neon pool close failed")
	}
	reopened := &ConnectionService{Store: &PostgresConnections{DB: fresh}, Codec: codec, API: &connectionAPIFixture{}}
	got, err := reopened.Get(ctx, created.ID)
	if err != nil || got.Snapshot == nil || got.State != "observed" || got.Version != 1 || len(got.Snapshot.Projects) != 1 || got.Snapshot.Projects[0].ID != "project-one" || len(got.Snapshot.Branches) != 1 || got.Snapshot.Branches[0].ProjectID != "project-one" {
		t.Fatal("fresh pool lost selected metadata or generation")
	}
	after := readNeonPGStored(t, fresh, created.ID)
	if !bytes.Equal(before.credential, after.credential) || !bytes.Equal(before.snapshot, after.snapshot) || before.version != after.version || !before.lastSuccess.Valid || !after.lastSuccess.Valid || !before.lastSuccess.Time.Equal(after.lastSuccess.Time) {
		t.Fatal("fresh pool changed persisted Neon state")
	}
	if _, err = reopened.Sync(ctx, created.ID); err != nil {
		t.Fatal("fresh service could not use the persisted real encrypted credential")
	}
	assertNeonPGSafeJSON(t, got, connectionFixtureKey, string(plain))
	raw, err := reopened.Store.Get(ctx, created.ID)
	if err != nil {
		t.Fatal("fresh private record read failed")
	}
	assertNeonPGSafeJSON(t, raw, connectionFixtureKey, string(plain))
}

func TestNeonPostgresConfigSelectionCredentialCASAndLeaseFences(t *testing.T) {
	db, _ := ownedNeonPostgres(t)
	service, codec, created := createNeonPGConnection(t, db, []string{"project-one"})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := service.Sync(ctx, created.ID); err != nil {
		t.Fatal("initial observed state unavailable")
	}
	store := service.Store.(*PostgresConnections)
	before := readNeonPGStored(t, db, created.ID)
	now := time.Now().UTC()
	lease := "nlease_config_fixture"
	if err := store.Claim(ctx, created.ID, 1, lease, now, now.Add(ConnectionLeaseDuration)); err != nil {
		t.Fatal("owned lease could not be claimed")
	}
	name := "Renamed config"
	renamed, err := service.Update(ctx, created.ID, UpdateConnectionRequest{Version: 1, Name: &name})
	if err != nil || renamed.Version != 2 || renamed.Snapshot == nil {
		t.Fatal("configuration edit lost observation or version")
	}
	requireNeonPGCode(t, store.Finish(ctx, created.ID, 1, lease, nil, "provider_unavailable", time.Now().UTC()), "conflict")
	afterRename := readNeonPGStored(t, db, created.ID)
	if !bytes.Equal(before.credential, afterRename.credential) || !bytes.Equal(before.snapshot, afterRename.snapshot) {
		t.Fatal("configuration edit rewrote credential or metadata")
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, candidate := range []string{"CAS winner A", "CAS winner B"} {
		go func(value string) {
			<-start
			_, err := service.Update(ctx, created.ID, UpdateConnectionRequest{Version: 2, Name: &value})
			results <- err
		}(candidate)
	}
	close(start)
	wins, conflicts := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			wins++
		} else {
			requireNeonPGCode(t, err, "conflict")
			conflicts++
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatal("same generation produced multiple configuration winners")
	}
	selected := []string{"project-two"}
	_, err = service.Update(ctx, created.ID, UpdateConnectionRequest{Version: 2, ProjectIDs: &selected})
	requireNeonPGCode(t, err, "conflict")
	changed, err := service.Update(ctx, created.ID, UpdateConnectionRequest{Version: 3, ProjectIDs: &selected})
	if err != nil || changed.Version != 4 || changed.Snapshot != nil || changed.LastSuccessAt != nil || changed.LastAttemptAt != nil || len(changed.ProjectIDs) != 1 || changed.ProjectIDs[0] != "project-two" {
		t.Fatal("selection CAS retained old authority/observation")
	}
	afterSelection := readNeonPGStored(t, db, created.ID)
	if !bytes.Equal(before.credential, afterSelection.credential) {
		t.Fatal("selection-only edit replaced credential bytes")
	}
	now = time.Now().UTC()
	lease = "nlease_key_fixture"
	if err = store.Claim(ctx, created.ID, 4, lease, now, now.Add(ConnectionLeaseDuration)); err != nil {
		t.Fatal("key replacement lease setup failed")
	}
	replacement := "synthetic-replaced-neon-management-key-not-real"
	changed, err = service.Update(ctx, created.ID, UpdateConnectionRequest{Version: 4, APIKey: &replacement, AcknowledgeKeyPermissions: true})
	if err != nil || changed.Version != 5 || changed.Snapshot != nil {
		t.Fatal("credential CAS did not invalidate old snapshot")
	}
	_, err = service.Update(ctx, created.ID, UpdateConnectionRequest{Version: 4, APIKey: &replacement, AcknowledgeKeyPermissions: true})
	requireNeonPGCode(t, err, "conflict")
	requireNeonPGCode(t, store.Finish(ctx, created.ID, 4, lease, nil, "provider_unavailable", time.Now().UTC()), "conflict")
	afterKey := readNeonPGStored(t, db, created.ID)
	if bytes.Equal(afterKey.credential, before.credential) || bytes.Contains(afterKey.credential, []byte(replacement)) {
		t.Fatal("credential replacement was unchanged or plaintext")
	}
	plain, err := codec.Open(afterKey.credential)
	if err != nil {
		t.Fatal("replacement ciphertext could not be opened")
	}
	defer clear(plain)
	var envelope connectionCredentialEnvelope
	if json.Unmarshal(plain, &envelope) != nil || envelope.Key != replacement || envelope.ConnectionID != created.ID || envelope.OrganizationID != created.OrganizationID {
		t.Fatal("replacement credential scope changed")
	}
	assertNeonPGSafeJSON(t, changed, connectionFixtureKey, replacement, string(plain))
	if _, err = service.Sync(ctx, created.ID); err != nil {
		t.Fatal("selected replacement credential could not sync")
	}
	retained := readNeonPGStored(t, db, created.ID)
	now = time.Now().UTC()
	until := now.Add(ConnectionLeaseDuration)
	lease = "nlease_finish_fixture"
	if err = store.Claim(ctx, created.ID, 5, lease, now, until); err != nil {
		t.Fatal("finish fence setup failed")
	}
	requireNeonPGCode(t, store.Finish(ctx, created.ID, 5, "nlease_wrong", nil, "provider_unavailable", time.Now().UTC()), "conflict")
	// PostgreSQL stores microseconds; step beyond that rounding boundary rather
	// than asserting expiry against a pre-rounding nanosecond timestamp.
	requireNeonPGCode(t, store.Finish(ctx, created.ID, 5, lease, nil, "provider_unavailable", until.Add(time.Second)), "conflict")
	afterRefusals := readNeonPGStored(t, db, created.ID)
	if !bytes.Equal(retained.snapshot, afterRefusals.snapshot) || !retained.lastSuccess.Time.Equal(afterRefusals.lastSuccess.Time) || retained.version != afterRefusals.version {
		t.Fatal("stale lease changed retained observation")
	}
	if err = store.Finish(ctx, created.ID, 5, lease, nil, "provider_unavailable", time.Now().UTC()); err != nil {
		t.Fatal("current failed read could not release lease")
	}
	failed, err := service.Get(ctx, created.ID)
	if err != nil || failed.State != "stale" || failed.Snapshot == nil || failed.Syncing {
		t.Fatal("failed refresh lost last success or retained lease")
	}
	requireNeonPGCode(t, service.Delete(ctx, created.ID, 4), "conflict")
	if err = service.Delete(ctx, created.ID, 5); err != nil {
		t.Fatal("current disconnect failed")
	}
	_, err = service.Get(ctx, created.ID)
	requireNeonPGCode(t, err, "not_found")
	requireNeonPGCode(t, store.Finish(ctx, created.ID, 5, lease, nil, "provider_unavailable", time.Now().UTC()), "not_found")
}

func TestNeonPostgresMigration042RefusesAnyCredentialRow(t *testing.T) {
	db, _ := ownedNeonPostgres(t)
	service, _, created := createNeonPGConnection(t, db, []string{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	disabled := false
	paused, err := service.Update(ctx, created.ID, UpdateConnectionRequest{Version: 1, Enabled: &disabled})
	if err != nil || paused.Enabled || len(paused.ProjectIDs) != 0 {
		t.Fatal("disabled empty-selection fixture unavailable")
	}
	before := readNeonPGStored(t, db, created.ID)
	down, err := os.ReadFile(filepath.Join("..", "..", "store", "postgres", "migrations", "042_neon_connections.down.sql"))
	if err != nil {
		t.Fatal("exact down042 source unavailable")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal("down042 transaction setup failed")
	}
	_, downErr := tx.ExecContext(ctx, string(down))
	rollbackErr := tx.Rollback()
	var rejection *pq.Error
	if !errors.As(downErr, &rejection) || rejection.Code != "P0001" || rejection.Message != "cannot remove Neon credential recovery support while connections remain" || rollbackErr != nil {
		t.Fatal("down042 did not refuse the existing credential row")
	}
	after := readNeonPGStored(t, db, created.ID)
	if !bytes.Equal(before.credential, after.credential) || !bytes.Equal(before.configuration, after.configuration) || !bytes.Equal(before.snapshot, after.snapshot) || before.version != after.version {
		t.Fatal("refused down042 changed retained credential state")
	}
	var migrations, guards int
	if db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE version='042_neon_connections'`).Scan(&migrations) != nil || migrations != 1 {
		t.Fatal("refused down042 lost migration tracking")
	}
	if db.QueryRowContext(ctx, `SELECT count(*) FROM pg_trigger WHERE tgrelid='vercel_connections'::regclass AND tgname='vercel_credential_rewrap_guard' AND NOT tgisinternal`).Scan(&guards) != nil || guards != 1 {
		t.Fatal("refused down042 removed sixth-family credential guard")
	}
}
