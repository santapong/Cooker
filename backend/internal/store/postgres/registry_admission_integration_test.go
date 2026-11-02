//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/secrets"
	"github.com/santapong/cooker/internal/secrets/database"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/store"
)

func registryAdmissionTestDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dsn := os.Getenv("COOKER_REGISTRY_ADMISSION_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("owned disposable registry-admission PostgreSQL required")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme != "postgres" || u.Hostname() != "cooker-registry-admission-pg" || u.Port() != "5432" || u.Path != "/cooker_registry_admission_test" || u.User == nil || u.User.Username() != "registry_admission_fixture" || len(u.Query()) != 1 || u.Query().Get("sslmode") != "disable" {
		t.Fatal("refusing non-owned registry-admission database")
	}
	if _, password := u.User.Password(); password {
		t.Fatal("fixture must not use a credential")
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "registry_admission_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec("CREATE SCHEMA " + schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		admin.Exec("DROP SCHEMA " + schema + " CASCADE")
		admin.Close()
		t.Fatal(err)
	}
	db.SetMaxOpenConns(12)
	t.Cleanup(func() { db.Close(); admin.Exec("DROP SCHEMA " + schema + " CASCADE"); admin.Close() })
	if err = applyMigrations(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db, u.String()
}

func registryAdmissionFixture(t *testing.T, db *sql.DB, kind, mode string) (*model.RegistryConfig, *model.RegistryAuthorityPlan, map[string]any, jobqueue.EnqueueOptions) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	subject := model.RegistrySubject{Kind: "app", ID: "app"}
	if kind == "pipeline-run" {
		subject = model.RegistrySubject{Kind: "pipeline", ID: "pipe"}
	}
	plan := &model.RegistryAuthorityPlan{Version: 1, Subject: subject, Connections: map[string]model.RegistryConnectionPin{}}
	var selected []string
	var registry *model.RegistryConfig
	if mode != "anonymous" {
		registry = &model.RegistryConfig{ID: "reg", Name: "owned registry", URL: "https://ghcr.io/team", Username: "owned-ci", AllowedAppIDs: []string{"app"}, AllowedPipelineIDs: []string{"pipe"}, CreatedAt: now, UpdatedAt: now}
		credential := model.RegistryCredentialPin{Mode: "none"}
		if mode == "database" || mode == "external" || mode == "legacy" {
			envID, key := "registry_6a2fc7f1e32640f5ab7d0b5f9400d663", "password"
			registry.PasswordRef = "registry:v2:6a2fc7f1-e326-40f5-ab7d-0b5f9400d663"
			if mode == "legacy" {
				envID, key = "_registries", "registry_password.reg"
				registry.PasswordRef = "registry:reg"
			}
			credential = model.RegistryCredentialPin{Mode: "external", EnvironmentID: envID, Key: key, Reference: &secrets.VersionReference{SchemaVersion: 1, Provider: "vault", Resource: "kv/data/" + envID + "/" + key, Version: "1", Incarnation: "2026-10-02T12:00:00Z", Scope: secrets.VersionScope{Authority: "https://owned-vault.invalid", Mount: "kv", EnvironmentID: envID, Key: key}}}
			if mode != "external" {
				env := &model.Environment{ID: envID, Name: envID, CreatedAt: now, Secrets: map[string][]byte{key: []byte("owned-ciphertext-canary")}}
				if err := NewEnvironmentStore(db).Create(ctx, env); err != nil {
					t.Fatal(err)
				}
				stored, err := NewEnvironmentStore(db).Get(ctx, envID)
				if err != nil {
					t.Fatal(err)
				}
				credential = model.RegistryCredentialPin{Mode: "database", EnvironmentID: envID, Key: key, EnvironmentVersion: stored.Version, EnvironmentCreatedAt: stored.CreatedAt}
			}
		}
		if err := NewRegistryConfigStore(db).Create(ctx, registry); err != nil {
			t.Fatal(err)
		}
		plan.Connections[registry.ID] = model.RegistryConnectionPin{ID: registry.ID, Version: registry.Version, Endpoint: "https://ghcr.io/team", Host: "ghcr.io", Username: registry.Username, Credential: credential}
		selected = []string{registry.ID}
	}
	var payload map[string]any
	opts := jobqueue.EnqueueOptions{MaxAttempts: 1}
	if kind == "app-deploy" {
		payload = map[string]any{"version": 1, "runId": "run", "createdAt": now, "app": &model.App{ID: "app", BuildPlan: &model.BuildPlan{RegistryConnectionIDs: selected}}, "registryAuthority": plan}
		opts.ID = "app-run:run"
		opts.ConcurrencyKey = "app:app"
	} else {
		plan.Stages = map[string][]string{"build": selected}
		p := &model.Pipeline{ID: "pipe", Version: 1, Stages: []model.Stage{{ID: "build", Type: model.StageTypeBuild, Config: model.StageConfig{RegistryConnectionIDs: selected}}}}
		payload = map[string]any{"pipelineId": p.ID, "runId": "run", "pipeline": p, "run": &model.PipelineRun{ID: "run", PipelineID: p.ID, PipelineVersion: 1, Status: model.RunStatusPending, CreatedAt: now}, "registryAuthority": plan}
		opts.ID = "run:run"
		opts.ConcurrencyKey = "pipeline:pipe"
	}
	if err := plan.Validate(); err != nil {
		t.Fatal(err)
	}
	return registry, plan, payload, opts
}

func TestRegistryAdmissionPostgresModesAndJSONBRestart(t *testing.T) {
	for _, kind := range []string{"app-deploy", "pipeline-run"} {
		for _, mode := range []string{"anonymous", "none", "database", "external", "legacy"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				db, dsn := registryAdmissionTestDB(t)
				_, plan, payload, opts := registryAdmissionFixture(t, db, kind, mode)
				data, _ := json.Marshal(payload)
				queue := jobqueue.NewPostgres(db)
				queue.OnEnqueue = SyncRegistryAuthorityAdmission
				if !RegistryAdmissionStoresMatch(queue, NewRegistryConfigStore(db), NewEnvironmentStore(db)) {
					t.Fatal("explicit queue binding rejected")
				}
				j, err := queue.Enqueue(context.Background(), kind, data, opts)
				if err != nil {
					t.Fatal(err)
				}
				fresh, err := sql.Open("postgres", dsn)
				if err != nil {
					t.Fatal(err)
				}
				defer fresh.Close()
				if RegistryAdmissionStoresMatch(queue, NewRegistryConfigStore(fresh), NewEnvironmentStore(fresh)) {
					t.Fatal("distinct pool treated as proven queue binding")
				}
				stored, err := jobqueue.NewPostgres(fresh).Get(context.Background(), j.ID)
				if err != nil {
					t.Fatal(err)
				}
				var decoded struct {
					Registry *model.RegistryAuthorityPlan `json:"registryAuthority"`
				}
				if json.Unmarshal(stored.Payload, &decoded) != nil || decoded.Registry == nil || decoded.Registry.Validate() != nil || decoded.Registry.Subject != plan.Subject {
					t.Fatal("JSONB/restart lost accepted authority")
				}
				before, _ := jobqueue.PayloadDigest(j.Payload)
				after, _ := jobqueue.PayloadDigest(stored.Payload)
				if before != after || strings.Contains(string(stored.Payload), "owned-ciphertext-canary") {
					t.Fatal("receipt changed or credential entered queue")
				}
			})
		}
	}
}

func TestRegistryAdmissionPostgresRejectsStaleOrSubstitutedPlans(t *testing.T) {
	for _, kind := range []string{"app-deploy", "pipeline-run"} {
		for _, mutation := range []string{"missing", "wrong-subject", "selection", "version", "revoked", "recreated", "endpoint", "username", "credential-scope", "environment-version", "environment-recreated", "key-removed", "malformed-reference", "job-scope"} {
			t.Run(kind+"/"+mutation, func(t *testing.T) {
				db, _ := registryAdmissionTestDB(t)
				registry, plan, payload, opts := registryAdmissionFixture(t, db, kind, "database")
				pin := plan.Connections["reg"]
				ctx := context.Background()
				switch mutation {
				case "missing":
					delete(payload, "registryAuthority")
				case "wrong-subject":
					plan.Subject.ID = "unrelated"
				case "selection":
					if kind == "app-deploy" {
						payload["app"].(*model.App).BuildPlan.RegistryConnectionIDs = nil
					} else {
						payload["pipeline"].(*model.Pipeline).Stages[0].Config.RegistryConnectionIDs = nil
					}
				case "version":
					pin.Version++
				case "revoked":
					if _, err := NewRegistryConfigStore(db).UpdateGrants(ctx, registry.ID, registry.Version, nil, nil); err != nil {
						t.Fatal(err)
					}
				case "recreated":
					st := NewRegistryConfigStore(db)
					if err := st.Delete(ctx, registry.ID); err != nil {
						t.Fatal(err)
					}
					if err := st.Create(ctx, registry.Clone()); err != nil {
						t.Fatal(err)
					}
				case "endpoint":
					pin.Endpoint = "https://unrelated.invalid"
				case "username":
					pin.Username = "unrelated-user"
				case "credential-scope":
					pin.Credential.EnvironmentID = "registry_unrelated"
				case "environment-version":
					if _, err := db.Exec(`UPDATE environments SET version=version+1 WHERE id=$1`, pin.Credential.EnvironmentID); err != nil {
						t.Fatal(err)
					}
				case "environment-recreated":
					st := NewEnvironmentStore(db)
					if err := st.Delete(ctx, pin.Credential.EnvironmentID); err != nil {
						t.Fatal(err)
					}
					env := &model.Environment{ID: pin.Credential.EnvironmentID, Name: pin.Credential.EnvironmentID, CreatedAt: time.Now().Add(time.Hour), Secrets: map[string][]byte{pin.Credential.Key: []byte("owned-recreated-canary")}}
					if err := st.Create(ctx, env); err != nil {
						t.Fatal(err)
					}
				case "key-removed":
					if _, err := db.Exec(`UPDATE environments SET secrets='{}'::jsonb WHERE id=$1`, pin.Credential.EnvironmentID); err != nil {
						t.Fatal(err)
					}
				case "malformed-reference":
					pin.Credential = model.RegistryCredentialPin{Mode: "external", EnvironmentID: pin.Credential.EnvironmentID, Key: pin.Credential.Key, Reference: &secrets.VersionReference{Provider: "owned-secret-canary"}}
				case "job-scope":
					opts.ConcurrencyKey = "unrelated:scope"
				}
				plan.Connections["reg"] = pin
				data, _ := json.Marshal(payload)
				queue := jobqueue.NewPostgres(db)
				later := 0
				queue.OnEnqueue = func(ctx context.Context, tx *sql.Tx, j *jobqueue.Job) error {
					if err := SyncRegistryAuthorityAdmission(ctx, tx, j); err != nil {
						return err
					}
					later++
					return nil
				}
				_, err := queue.Enqueue(ctx, kind, data, opts)
				if !errors.Is(err, store.ErrConflict) || strings.Contains(err.Error(), "canary") || later != 0 {
					t.Fatal("stale/substituted plan passed fence or exposed metadata")
				}
				if _, err = queue.Get(ctx, opts.ID); !errors.Is(err, jobqueue.ErrNotFound) {
					t.Fatal("rejected fence left admitted work")
				}
			})
		}
	}
}

func TestRegistryAdmissionPostgresLocksGrantAndCredentialThroughCommit(t *testing.T) {
	for _, target := range []string{"grant", "credential"} {
		t.Run(target, func(t *testing.T) {
			db, _ := registryAdmissionTestDB(t)
			r, plan, payload, opts := registryAdmissionFixture(t, db, "pipeline-run", "database")
			data, _ := json.Marshal(payload)
			queue := jobqueue.NewPostgres(db)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(release) })
			queue.OnEnqueue = func(ctx context.Context, tx *sql.Tx, j *jobqueue.Job) error {
				if err := SyncRegistryAuthorityAdmission(ctx, tx, j); err != nil {
					return err
				}
				close(entered)
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			queued := make(chan error, 1)
			go func() { _, err := queue.Enqueue(context.Background(), "pipeline-run", data, opts); queued <- err }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("fence did not enter")
			}
			writer, err := db.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Close()
			var pid int
			if err = writer.QueryRowContext(context.Background(), `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				t.Fatal(err)
			}
			updated := make(chan error, 1)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			go func() {
				var err error
				if target == "grant" {
					_, err = writer.ExecContext(ctx, `UPDATE registry_configs SET allowed_pipeline_ids='[]'::jsonb WHERE id=$1`, r.ID)
				} else {
					_, err = writer.ExecContext(ctx, `UPDATE environments SET version=version+1 WHERE id=$1`, plan.Connections[r.ID].Credential.EnvironmentID)
				}
				updated <- err
			}()
			deadline := time.Now().Add(2 * time.Second)
			observed := false
			for time.Now().Before(deadline) {
				var wait sql.NullString
				if err = db.QueryRow(`SELECT wait_event_type FROM pg_stat_activity WHERE pid=$1`, pid).Scan(&wait); err != nil {
					t.Fatal(err)
				}
				if wait.Valid && wait.String == "Lock" {
					observed = true
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !observed {
				t.Fatal("concurrent mutation was not fenced by row lock")
			}
			once.Do(func() { close(release) })
			if err = <-queued; err != nil {
				t.Fatal(err)
			}
			if err = <-updated; err != nil {
				t.Fatal(err)
			}
			if _, err = queue.Get(context.Background(), opts.ID); err != nil {
				t.Fatal("accepted work disappeared after later revocation")
			}
		})
	}
}

func TestRegistryAdmissionPostgresTriggerReceiptAndJobRollbackTogether(t *testing.T) {
	for _, failure := range []string{"revoked-before-fence", "after-receipt", "success"} {
		t.Run(failure, func(t *testing.T) {
			db, _ := registryAdmissionTestDB(t)
			r, _, payload, opts := registryAdmissionFixture(t, db, "app-deploy", "none")
			inbox := NewTriggerStore(db)
			delivery, intent := triggerRecordFixture(t, inbox)
			if _, _, err := inbox.RecordDelivery(context.Background(), delivery, []*model.TriggerIntent{intent}); err != nil {
				t.Fatal(err)
			}
			payload["commit"] = intent.Event.CommitSHA
			payload["actor"] = &model.PipelineRun{}
			payload["trigger"] = map[string]any{"intentId": intent.ID, "ruleId": intent.Rule.ID, "ruleVersion": intent.Rule.Version, "envelopeHash": intent.Rule.ApprovedEnvelopeHash}
			if failure == "revoked-before-fence" {
				if _, err := NewRegistryConfigStore(db).UpdateGrants(context.Background(), r.ID, r.Version, nil, nil); err != nil {
					t.Fatal(err)
				}
			}
			queue := jobqueue.NewPostgres(db)
			queue.OnEnqueue = func(ctx context.Context, tx *sql.Tx, j *jobqueue.Job) error {
				if err := SyncRegistryAuthorityAdmission(ctx, tx, j); err != nil {
					return err
				}
				if err := SyncTriggerAdmission(ctx, tx, j); err != nil {
					return err
				}
				if failure == "after-receipt" {
					return fmt.Errorf("owned failure after receipt")
				}
				return nil
			}
			data, _ := json.Marshal(payload)
			_, err := queue.Enqueue(context.Background(), "app-deploy", data, opts)
			if (err == nil) != (failure == "success") {
				t.Fatal("unexpected transaction outcome")
			}
			stored, err := NewTriggerStore(db).GetIntent(context.Background(), intent.ID)
			if err != nil {
				t.Fatal(err)
			}
			if failure == "success" {
				if stored.Status != model.TriggerQueued || stored.Receipt == nil {
					t.Fatal("accepted receipt absent")
				}
				j, err := jobqueue.NewPostgres(db).Get(context.Background(), opts.ID)
				if err != nil {
					t.Fatal(err)
				}
				digest, _ := jobqueue.PayloadDigest(j.Payload)
				if digest != stored.Receipt.PlanDigest {
					t.Fatal("JSONB receipt differs")
				}
			} else {
				if stored.Status != model.TriggerPending || stored.Receipt != nil {
					t.Fatal("rejected transaction advanced trigger")
				}
				if _, err := queue.Get(context.Background(), opts.ID); !errors.Is(err, jobqueue.ErrNotFound) {
					t.Fatal("rejected transaction retained job")
				}
			}
		})
	}
}

func TestRegistryAdmissionPostgresNativeReceiptFirstRestartDoesNotRepin(t *testing.T) {
	db, dsn := registryAdmissionTestDB(t)
	ctx := context.Background()
	st := &store.Store{Registries: NewRegistryConfigStore(db), Environments: NewEnvironmentStore(db), Pipelines: NewPipelineStore(db), Runs: NewRunStore(db)}
	codec, err := crypto.NewCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	manager := database.New(st.Environments, codec)
	r := &model.RegistryConfig{ID: "reg", Name: "owned registry", URL: "ghcr.io", Username: "owned-ci", AllowedPipelineIDs: []string{"pipe"}, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err = service.NewRegistryConfigService(st, manager).CreateRegistry(ctx, r, []byte("owned-native-registry-canary")); err != nil {
		t.Fatal(err)
	}
	p := &model.Pipeline{ID: "pipe", Version: 1, Stages: []model.Stage{{ID: "build", Type: model.StageTypeBuild, Config: model.StageConfig{RegistryConnectionIDs: []string{r.ID}}}}}
	p.SetDefaults()
	if err = st.Pipelines.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	jobs := jobqueue.NewPostgres(db)
	jobs.OnEnqueue = SyncRegistryAuthorityAdmission
	authority := service.NewRegistryAuthorityService(st, manager)
	e := service.NewJobQueueEnqueuer(jobs, st.Pipelines)
	e.RequireRegistryPins = true
	e.AdmissionGuard = NewTriggerStore(db)
	var pins atomic.Int32
	e.PrepareRegistry = func(ctx context.Context, p *model.Pipeline) (*model.RegistryAuthorityPlan, error) {
		pins.Add(1)
		return service.PreparePipelineRegistryAuthority(ctx, authority, p)
	}
	run := &model.PipelineRun{ID: "native-restart", PipelineID: p.ID, PipelineVersion: p.Version, Status: model.RunStatusPending, CreatedAt: time.Now().UTC(), StartedByUserSub: "owned-user"}
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- e.EnqueuePlannedRun(ctx, p, run.Clone()) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if pins.Load() != 1 {
		t.Fatal("concurrent admission repinned")
	}
	j, err := jobs.Get(ctx, "run:"+run.ID)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := jobqueue.PayloadDigest(j.Payload)
	if _, err = NewRegistryConfigStore(db).UpdateGrants(ctx, r.ID, r.Version, nil, nil); err != nil {
		t.Fatal(err)
	}
	freshDB, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer freshDB.Close()
	freshJobs := jobqueue.NewPostgres(freshDB)
	freshJobs.OnEnqueue = SyncRegistryAuthorityAdmission
	fresh := service.NewJobQueueEnqueuer(freshJobs, NewPipelineStore(freshDB))
	fresh.RequireRegistryPins = true
	fresh.AdmissionGuard = NewTriggerStore(freshDB)
	fresh.PrepareRegistry = func(context.Context, *model.Pipeline) (*model.RegistryAuthorityPlan, error) {
		t.Fatal("receipt recovery repinned after revocation")
		return nil, nil
	}
	if err = fresh.EnqueuePlannedRun(ctx, p, run.Clone()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := fresh.AcceptedRunSnapshot(ctx, run.ID)
	if err != nil || snapshot.StartedByUserSub != run.StartedByUserSub {
		t.Fatal("restart lost actor receipt")
	}
	stored, err := freshJobs.Get(ctx, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := jobqueue.PayloadDigest(stored.Payload)
	if before != after || strings.Contains(string(stored.Payload), "owned-native-registry-canary") {
		t.Fatal("accepted authority changed or leaked secret")
	}
}

func TestRegistryAdmissionPostgresDatabaseFailureAndCancelledLockRollback(t *testing.T) {
	for _, failure := range []string{"missing-registry-relation", "missing-environment-relation", "cancelled-lock"} {
		t.Run(failure, func(t *testing.T) {
			db, _ := registryAdmissionTestDB(t)
			r, _, payload, opts := registryAdmissionFixture(t, db, "pipeline-run", "database")
			queue := jobqueue.NewPostgres(db)
			queue.OnEnqueue = SyncRegistryAuthorityAdmission
			ctx := context.Background()
			switch failure {
			case "missing-registry-relation":
				if _, err := db.Exec(`ALTER TABLE registry_configs RENAME TO registry_configs_owned_unavailable`); err != nil {
					t.Fatal(err)
				}
			case "missing-environment-relation":
				if _, err := db.Exec(`ALTER TABLE environments RENAME TO environments_owned_unavailable`); err != nil {
					t.Fatal(err)
				}
			case "cancelled-lock":
				tx, err := db.BeginTx(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				if _, err = tx.Exec(`SELECT id FROM registry_configs WHERE id=$1 FOR UPDATE`, r.ID); err != nil {
					t.Fatal(err)
				}
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 150*time.Millisecond)
				defer cancel()
			}
			data, _ := json.Marshal(payload)
			if _, err := queue.Enqueue(ctx, "pipeline-run", data, opts); err == nil || strings.Contains(err.Error(), "owned-ciphertext-canary") {
				t.Fatal("database failure admitted work or exposed credential")
			}
			if _, err := queue.Get(context.Background(), opts.ID); !errors.Is(err, jobqueue.ErrNotFound) {
				t.Fatal("failed/cancelled transaction retained job")
			}
		})
	}
}
