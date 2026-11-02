//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/secrets/database"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/store"
)

// Exercise the actual App producer, not a hand-constructed JSON payload. The
// source/image callbacks supply only fixture metadata; this is not OCI/runtime
// acceptance. Registry grants, encrypted credentials and receipt persistence use
// the owned PostgreSQL profile shared with the atomic admission suite.
func TestAppRegistryProducerPostgresConcurrentReceiptAndFreshPool(t *testing.T) {
	for _, selected := range []bool{false, true} {
		name := "anonymous"
		if selected {
			name = "database-credential"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			db, dsn := registryAdmissionTestDB(t)
			primary, err := NewStore(ctx, dsn)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { primary.Close() })
			codec, err := crypto.NewCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)))
			if err != nil {
				t.Fatal(err)
			}
			manager := database.New(primary.Environments, codec)
			authority := service.NewRegistryAuthorityService(primary, manager)
			// Mirror production's explicit binding of metadata to the queue pool;
			// the primary CRUD/secret store has its own pool over this same schema.
			authority.Registries, authority.Environments = NewRegistryConfigStore(db), NewEnvironmentStore(db)
			app := &model.App{ID: uuid.NewString(), Name: "owned-producer", GitHubRepo: "owned/fixture", Branch: "main", BuildPlan: &model.BuildPlan{Kind: model.BuildPlanDockerfile, Path: "Dockerfile"}, DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost}}
			var config *model.RegistryConfig
			var credentialEnv string
			if selected {
				incarnation := uuid.NewString()
				credentialEnv = "registry_" + strings.ReplaceAll(incarnation, "-", "")
				if err = primary.Environments.Create(ctx, &model.Environment{ID: credentialEnv, Name: credentialEnv, CreatedAt: time.Now().UTC()}); err != nil {
					t.Fatal(err)
				}
				if err = manager.Put(ctx, credentialEnv, "password", []byte("owned-original-password-canary")); err != nil {
					t.Fatal(err)
				}
				config = &model.RegistryConfig{ID: uuid.NewString(), Name: "owned-producer-registry", URL: "https://ghcr.io/owned", Username: "owned", PasswordRef: "registry:v2:" + incarnation, AllowedAppIDs: []string{app.ID}}
				if err = primary.Registries.Create(ctx, config); err != nil {
					t.Fatal(err)
				}
				app.BuildPlan.RegistryConnectionIDs = []string{config.ID}
			}
			if err = primary.Apps.Create(ctx, app); err != nil {
				t.Fatal(err)
			}
			jobs := jobqueue.NewPostgres(db)
			jobs.OnEnqueue = SyncRegistryAuthorityAdmission
			if !RegistryAdmissionStoresMatch(jobs, authority.Registries, authority.Environments) {
				t.Fatal("producer authority is not bound to admission")
			}
			var sourceCalls, imageCalls atomic.Int32
			q := &service.AppDeployQueue{Jobs: jobs, Store: primary, Deployer: service.NewAppDeployer(service.NewExecutor(service.WithRegistryAuthority(authority)), "ghcr.io/owned"), RegistryAuthority: authority, RequireRegistryPins: true, AdmissionGuard: NewTriggerStore(db), RequireImagePins: true,
				ResolveSource: func(context.Context, *model.App, string) (string, error) {
					sourceCalls.Add(1)
					return strings.Repeat("a", 40), nil
				},
				PrepareImages: func(context.Context, *service.AppDeployPlan) (*service.AppImageResolution, error) {
					imageCalls.Add(1)
					return &service.AppImageResolution{Version: 1, Kind: model.BuildPlanDockerfile, Services: map[string]service.AppImagePin{}}, nil
				},
			}
			actor := &model.PipelineRun{StartedByUserSub: "owned-original-actor"}
			var wg sync.WaitGroup
			receipts := make(chan *service.AppDeployReceipt, 12)
			errs := make(chan error, 12)
			for range 12 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					receipt, e := q.EnqueueIntent(ctx, app, "", actor, "owned-producer-intent")
					receipts <- receipt
					errs <- e
				}()
			}
			wg.Wait()
			close(receipts)
			close(errs)
			for e := range errs {
				if e != nil {
					t.Fatal(e)
				}
			}
			var original *service.AppDeployReceipt
			for receipt := range receipts {
				if original == nil {
					original = receipt
				} else if *receipt != *original {
					t.Fatal("concurrent App producer changed receipt")
				}
			}
			if original == nil || sourceCalls.Load() != 1 || imageCalls.Load() != 1 {
				t.Fatal("concurrent App producer repeated admission preparation")
			}
			acceptedJob, err := jobs.Get(ctx, original.JobID)
			if err != nil {
				t.Fatal(err)
			}
			var accepted service.AppDeployPlan
			if json.Unmarshal(acceptedJob.Payload, &accepted) != nil || accepted.Registry == nil || accepted.Registry.Subject != (model.RegistrySubject{Kind: "app", ID: app.ID}) || accepted.Registry.Validate() != nil || strings.Contains(string(acceptedJob.Payload), "password-canary") {
				t.Fatal("producer did not persist safe App authority")
			}
			if selected {
				pin := accepted.Registry.Connections[config.ID].Credential
				if pin.Mode != "database" || pin.EnvironmentVersion < 1 || pin.EnvironmentID != credentialEnv {
					t.Fatal("producer omitted database generation")
				}
				if err = manager.Put(ctx, credentialEnv, "password", []byte("owned-rotated-password-canary")); err != nil {
					t.Fatal(err)
				}
				if _, err = primary.Registries.(store.RegistryGrantStore).UpdateGrants(ctx, config.ID, config.Version, nil, nil); err != nil {
					t.Fatal(err)
				}
			} else if len(accepted.Registry.Connections) != 0 {
				t.Fatal("anonymous producer acquired a credential")
			}
			// Reconstruct a fresh primary pool/queue. The receipt is returned before
			// any authority/source/image dependencies are used, even after revocation.
			freshPrimary, err := NewStore(ctx, dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer freshPrimary.Close()
			freshDB, err := sql.Open("postgres", dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer freshDB.Close()
			freshJobs := jobqueue.NewPostgres(freshDB)
			reopened := &service.AppDeployQueue{Jobs: freshJobs, RequireRegistryPins: true}
			actor.StartedByUserSub = "replacement-actor"
			app.BuildPlan.RegistryConnectionIDs = nil
			for range 6 {
				receipt, e := reopened.EnqueueIntent(ctx, app, original.Commit, actor, "owned-producer-intent")
				if e != nil || *receipt != *original {
					t.Fatal("fresh App caller repinned accepted work", e)
				}
			}
			stored, err := freshJobs.Get(ctx, original.JobID)
			if err != nil {
				t.Fatal(err)
			}
			before, _ := jobqueue.PayloadDigest(acceptedJob.Payload)
			after, _ := jobqueue.PayloadDigest(stored.Payload)
			run, err := freshPrimary.Runs.Get(ctx, original.RunID)
			if err != nil || run.StartedByUserSub != "owned-original-actor" || before != after || sourceCalls.Load() != 1 || imageCalls.Load() != 1 {
				t.Fatal("fresh receipt lost original actor/digest")
			}
			if _, err = reopened.EnqueueIntent(ctx, app, strings.Repeat("b", 40), actor, "owned-producer-intent"); err == nil {
				t.Fatal("App intent accepted another commit")
			}
		})
	}
}

type appRegistryFenceQueue struct {
	jobqueue.Store
	before func(context.Context) error
}

func (q appRegistryFenceQueue) Enqueue(ctx context.Context, kind string, payload json.RawMessage, opts jobqueue.EnqueueOptions) (*jobqueue.Job, error) {
	if err := q.before(ctx); err != nil {
		return nil, err
	}
	return q.Store.Enqueue(ctx, kind, payload, opts)
}

func TestAppRegistryProducerPostgresFenceRejectsMutationAfterPreparation(t *testing.T) {
	for _, mutation := range []string{"grant", "credential-generation"} {
		t.Run(mutation, func(t *testing.T) {
			ctx := context.Background()
			db, dsn := registryAdmissionTestDB(t)
			primary, err := NewStore(ctx, dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer primary.Close()
			codec, _ := crypto.NewCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)))
			manager := database.New(primary.Environments, codec)
			_, _, fixturePayload, _ := registryAdmissionFixture(t, db, "app-deploy", "database")
			app := fixturePayload["app"].(*model.App)
			app.Name, app.GitHubRepo, app.Branch = "owned-fence", "owned/fixture", "main"
			app.BuildPlan.Kind, app.BuildPlan.Path = model.BuildPlanDockerfile, "Dockerfile"
			app.DeployTarget.Kind = model.DeployTargetDockerHost
			config, err := primary.Registries.Get(ctx, "reg")
			if err != nil {
				t.Fatal(err)
			}
			envID, key, _, err := service.RegistryCredentialScope(config.PasswordRef, config.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err = manager.Put(ctx, envID, key, []byte("owned-fence-password-canary")); err != nil {
				t.Fatal(err)
			}
			authority := service.NewRegistryAuthorityService(primary, manager)
			authority.Registries, authority.Environments = NewRegistryConfigStore(db), NewEnvironmentStore(db)
			jobs := jobqueue.NewPostgres(db)
			jobs.OnEnqueue = SyncRegistryAuthorityAdmission
			fenced := appRegistryFenceQueue{Store: jobs, before: func(ctx context.Context) error {
				if mutation == "grant" {
					_, e := primary.Registries.(store.RegistryGrantStore).UpdateGrants(ctx, config.ID, config.Version, nil, nil)
					return e
				}
				return manager.Put(ctx, envID, key, []byte("owned-replacement-password-canary"))
			}}
			q := &service.AppDeployQueue{Jobs: fenced, Store: primary, Deployer: service.NewAppDeployer(service.NewExecutor(service.WithRegistryAuthority(authority)), "ghcr.io/team"), RegistryAuthority: authority, RequireRegistryPins: true, AdmissionGuard: NewTriggerStore(db), RequireImagePins: true,
				PrepareImages: func(context.Context, *service.AppDeployPlan) (*service.AppImageResolution, error) {
					return &service.AppImageResolution{Version: 1, Kind: model.BuildPlanDockerfile, Services: map[string]service.AppImagePin{}}, nil
				},
			}
			if _, err = q.EnqueueIntent(ctx, app, strings.Repeat("a", 40), nil, "owned-fenced-intent"); !errors.Is(err, store.ErrConflict) || strings.Contains(err.Error(), "canary") {
				t.Fatal("post-preparation mutation passed atomic admission", err)
			}
			stats, err := jobs.Stats(ctx)
			if err != nil || stats[jobqueue.StatusPending] != 0 {
				t.Fatal("rejected App producer left runnable work")
			}
		})
	}
}
