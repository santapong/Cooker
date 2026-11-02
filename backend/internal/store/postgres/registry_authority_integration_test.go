//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/secrets/database"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/store"
)

func registryTestDatabase(t *testing.T, migrate bool) (*sql.DB, string) {
	t.Helper()
	dsn := os.Getenv("COOKER_REGISTRY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("owned disposable registry PostgreSQL required")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Hostname() != "cooker-registry-sec04-pg" || u.Path != "/cooker_registry_test" {
		t.Fatal("refusing non-owned registry fixture")
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "registry_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	scoped := u.String()
	db, err := sql.Open("postgres", scoped)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(); _, _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE"); admin.Close() })
	if migrate {
		if err = applyMigrations(context.Background(), db); err != nil {
			t.Fatal(err)
		}
	}
	return db, scoped
}

func TestRegistryPostgresGrantsRestartAndCredentialFences(t *testing.T) {
	db, dsn := registryTestDatabase(t, true)
	ctx := context.Background()
	st := &store.Store{Registries: NewRegistryConfigStore(db), Environments: NewEnvironmentStore(db)}
	codec, err := crypto.NewCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	manager := database.New(st.Environments, codec)
	r := &model.RegistryConfig{ID: "connection", Name: "fixture", URL: "ghcr.io", Username: "ci", AllowedAppIDs: []string{"app"}, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err = service.NewRegistryConfigService(st, manager).CreateRegistry(ctx, r, []byte("owned-registry-secret-canary")); err != nil {
		t.Fatal(err)
	}
	subject := model.RegistrySubject{Kind: "app", ID: "app"}
	authority := service.NewRegistryAuthorityService(st, manager)
	plan, err := authority.Prepare(ctx, subject, []string{r.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(plan)
	if strings.Contains(string(data), "secret-canary") {
		t.Fatal("plan leaked value")
	}
	if _, err = db.Exec(`CREATE TABLE accepted_registry_fixture (payload jsonb NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO accepted_registry_fixture VALUES ($1)`, data); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	freshDB, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer freshDB.Close()
	fresh := &store.Store{Registries: NewRegistryConfigStore(freshDB), Environments: NewEnvironmentStore(freshDB)}
	freshManager := database.New(fresh.Environments, codec)
	if err = freshDB.QueryRow(`SELECT payload FROM accepted_registry_fixture`).Scan(&data); err != nil {
		t.Fatal(err)
	}
	var restored model.RegistryAuthorityPlan
	if err = json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	freshAuthority := service.NewRegistryAuthorityService(fresh, freshManager)
	resolve, err := freshAuthority.Resolver(ctx, subject, &restored, "")
	if err != nil {
		t.Fatal(err)
	}
	a, err := resolve(ctx, "ghcr.io")
	if err != nil {
		t.Fatal(err)
	}
	auth, _ := a.Authorization()
	if auth.Password != "owned-registry-secret-canary" {
		t.Fatal("fresh resolver lost accepted credentials")
	}
	if _, err = freshDB.Exec(`UPDATE registry_configs SET name='renamed' WHERE id=$1`, r.ID); err != nil {
		t.Fatal(err)
	}
	newer, err := fresh.Registries.Get(ctx, r.ID)
	if err != nil || newer.Version <= r.Version {
		t.Fatal("direct config edit reused generation", err)
	}
	if _, err = resolve(ctx, "ghcr.io"); !errors.Is(err, service.ErrRegistryAuthorityChanged) {
		t.Fatal("rename did not revoke accepted config")
	}
	plan, err = freshAuthority.Prepare(ctx, subject, []string{r.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	resolve, err = freshAuthority.Resolver(ctx, subject, plan, "")
	if err != nil {
		t.Fatal(err)
	}
	env, key, _, err := service.RegistryCredentialScope(newer.PasswordRef, newer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = freshManager.Put(ctx, env, key, []byte("rotation")); err != nil {
		t.Fatal(err)
	}
	if _, err = resolve(ctx, "ghcr.io"); !errors.Is(err, service.ErrRegistryAuthorityChanged) {
		t.Fatal("database rotation reused accepted credential")
	}
	if err = fresh.Registries.Delete(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	newer.Name = "recreated"
	if err = fresh.Registries.Create(ctx, newer); err != nil {
		t.Fatal(err)
	}
	if newer.Version <= plan.Connections[r.ID].Version {
		t.Fatal("delete/recreate reused registry version")
	}
	if err = freshAuthority.ValidateCurrent(ctx, subject, plan); !errors.Is(err, service.ErrRegistryAuthorityChanged) {
		t.Fatal("recreated registry retained accepted authority")
	}
}

func TestRegistryPostgresConcurrentGrantCASAndDBError(t *testing.T) {
	db, _ := registryTestDatabase(t, true)
	ctx := context.Background()
	s := NewRegistryConfigStore(db)
	r := &model.RegistryConfig{ID: "connection", Name: "fixture", URL: "ghcr.io", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.Create(ctx, r); err != nil {
		t.Fatal(err)
	}
	version := r.Version
	var won atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.UpdateGrants(ctx, r.ID, version, []string{"app"}, nil)
			if err == nil {
				won.Add(1)
			} else if !errors.Is(err, store.ErrConflict) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if won.Load() != 1 {
		t.Fatal("concurrent grants bypassed CAS")
	}
	if err := s.Create(ctx, r); !errors.Is(err, store.ErrConflict) {
		t.Fatal("duplicate ID overwrote registry")
	}
	if _, err := s.UpdateGrants(ctx, r.ID, version, []string{"app"}, nil); !errors.Is(err, store.ErrConflict) {
		t.Fatal("stale edit accepted")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	authority := service.NewRegistryAuthorityService(&store.Store{Registries: s}, nil)
	if _, err := authority.Prepare(ctx, model.RegistrySubject{Kind: "app", ID: "app"}, []string{r.ID}, nil); !errors.Is(err, service.ErrRegistryAuthorityDenied) {
		t.Fatal("DB failure did not fail closed")
	}
}

func TestRegistryPostgresMigrationRetainsLegacyWithoutGrants(t *testing.T) {
	db, _ := registryTestDatabase(t, false)
	setup, err := migrationsFS.ReadFile("migrations/021_settings_configs.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(setup)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO registry_configs(id,name,url,password_ref) VALUES ('legacy','legacy','ghcr.io','registry:legacy')`); err != nil {
		t.Fatal(err)
	}
	up, _ := migrationsFS.ReadFile("migrations/038_registry_authority.up.sql")
	if _, err = db.Exec(string(up)); err != nil {
		t.Fatal(err)
	}
	r, err := NewRegistryConfigStore(db).Get(context.Background(), "legacy")
	if err != nil || r.PasswordRef != "registry:legacy" || r.Version < 1 || len(r.AllowedAppIDs) != 0 || len(r.AllowedPipelineIDs) != 0 {
		t.Fatal("migration changed credentials or invented grants", err)
	}
	down, _ := migrationsFS.ReadFile("migrations/038_registry_authority.down.sql")
	if _, err = db.Exec(string(down)); err != nil {
		t.Fatal(err)
	}
	var ref string
	if err = db.QueryRow(`SELECT password_ref FROM registry_configs WHERE id='legacy'`).Scan(&ref); err != nil || ref != "registry:legacy" {
		t.Fatal("legacy projection cannot resume", err)
	}
}
