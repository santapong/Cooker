//go:build integration

package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	cookercrypto "github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/source/connection"
	"github.com/santapong/cooker/internal/store"
	storepg "github.com/santapong/cooker/internal/store/postgres"
)

func executableCompatConfigureRunner(r *JobQueueRunner, e *Executor, st *store.Store, db *sql.DB) {
	a := NewRegistryAuthorityService(st, nil)
	a.Registries, a.Environments = storepg.NewRegistryConfigStore(db), storepg.NewEnvironmentStore(db)
	r.RegistryAuthority, e.registryAuthority = a, a
}

// Emits real current producer/admission JSON for a separate older executable.
// This fixture never executes an application adapter or contacts a provider.
func TestNativeExecutableCompatibilitySeed(t *testing.T) {
	dsn := os.Getenv("COOKER_NATIVE_EXECUTABLE_TEST_DATABASE_URL")
	path := os.Getenv("COOKER_NATIVE_EXECUTABLE_SEED_FILE")
	if dsn == "" || path == "" {
		t.Skip("owned executable compatibility fixture required")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme != "postgres" || u.Hostname() != "cooker-native-executable-pg" || u.Port() != "5432" || u.Path != "/cooker_native_executable_test" || u.User == nil || u.User.Username() != "native_executable_fixture" || u.Query().Get("sslmode") != "disable" || len(u.Query()) != 1 || u.Fragment != "" {
		t.Fatal("refusing non-owned executable fixture")
	}
	if _, password := u.User.Password(); password {
		t.Fatal("fixture must not use credentials")
	}
	ctx := context.Background()
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	manifest := map[string]any{"schemaVersion": 1, "groups": map[string]any{}}
	groups := manifest["groups"].(map[string]any)
	for _, consumer := range []string{"old727", "current843"} {
		schema := "native_executable_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		if _, err = admin.Exec("CREATE SCHEMA " + schema); err != nil {
			t.Fatal(err)
		}
		// The harness owns/drop-checks schemas after both binaries have read them.
		fixtureURL := *u
		q := fixtureURL.Query()
		q.Set("search_path", schema)
		fixtureURL.RawQuery = q.Encode()
		st, err := storepg.NewStore(ctx, fixtureURL.String())
		if err != nil {
			t.Fatal(err)
		}
		db, err := sql.Open("postgres", fixtureURL.String())
		if err != nil {
			st.Close()
			t.Fatal(err)
		}
		cases := []map[string]any{}
		for _, policy := range []string{"automatic", "review", "regular-build", "regular-push", "regular-deploy"} {
			preparer, p, f := nativeSourceSetup(t)
			codec, err := cookercrypto.NewCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)))
			if err != nil {
				t.Fatal(err)
			}
			connections := &connection.Service{Store: &connection.Postgres{DB: db}, Codec: codec}
			c := &connection.Connection{Name: "owned compatibility source", Provider: "github", Enabled: true, Repositories: []string{"owned/native"}}
			if err = connections.Create(ctx, c, strings.Repeat("fixture-", 8)); err != nil {
				t.Fatal(err)
			}
			p.ID, p.Name = uuid.NewString(), "Owned "+policy+" compatibility"
			p.Source.ConnectionID = c.ID
			p.CreatedAt = time.Now().UTC()
			p.UpdatedAt = p.CreatedAt
			image := "ghcr.io/owned/compat@sha256:" + strings.Repeat("c", 64)
			p.Stages = append(p.Stages,
				model.Stage{ID: "push", Name: "Independent publication", Type: model.StageTypePush, Config: model.StageConfig{Image: image, Repository: "ghcr.io/owned/compat:fixture"}},
				model.Stage{ID: "deploy", Name: "Independent Docker deployment", Type: model.StageTypeDeploy, Config: model.StageConfig{Image: image, RuntimeName: "owned-compat", DeployRuntime: "docker"}},
			)
			p.Stages[0].Config.Tags = []string{"ghcr.io/owned/compat:fixture"}
			registry := &model.RegistryConfig{ID: uuid.NewString(), Name: "credential-free owned authority " + policy, URL: "https://ghcr.io/owned", Username: "owned", AllowedPipelineIDs: []string{p.ID}}
			if err = st.Registries.Create(ctx, registry); err != nil {
				t.Fatal(err)
			}
			for i := range p.Stages {
				if !strings.HasPrefix(policy, "regular-") {
					p.Stages[i].Config.RegistryConnectionIDs = []string{registry.ID}
				}
			}
			if strings.HasPrefix(policy, "regular-") {
				p.Source = nil
				switch policy {
				case "regular-build":
					p.Stages = p.Stages[:1]
				case "regular-push":
					p.Stages = p.Stages[1:2]
				case "regular-deploy":
					p.Stages = p.Stages[2:3]
				}
			}
			if err = st.Pipelines.Create(ctx, p); err != nil {
				t.Fatal(err)
			}
			p, err = st.Pipelines.Get(ctx, p.ID)
			if err != nil {
				t.Fatal(err)
			}
			preparer.Store, preparer.Connections = st, connections
			jobs := jobqueue.NewPostgres(db)
			jobs.OnEnqueue = func(ctx context.Context, tx *sql.Tx, job *jobqueue.Job) error {
				if err := storepg.SyncRegistryAuthorityAdmission(ctx, tx, job); err != nil {
					return err
				}
				return storepg.SyncTriggerAdmission(ctx, tx, job)
			}
			producer := NewJobQueueEnqueuer(jobs, st.Pipelines)
			producer.AdmissionGuard = storepg.NewTriggerStore(db)
			authority := NewRegistryAuthorityService(st, nil)
			authority.Registries, authority.Environments = storepg.NewRegistryConfigStore(db), storepg.NewEnvironmentStore(db)
			producer.RequireRegistryPins = true
			producer.PrepareRegistry = func(ctx context.Context, p *model.Pipeline) (*model.RegistryAuthorityPlan, error) {
				return PreparePipelineRegistryAuthority(ctx, authority, p)
			}
			producer.PrepareSecrets = func(ctx context.Context, p *model.Pipeline) (*PipelineSecretPlan, error) {
				return NewExecutor().PreparePipelineSecrets(ctx, p)
			}
			var jobID, intentID string
			var receipt *model.TriggerReceipt
			if strings.HasPrefix(policy, "regular-") {
				run := pendingPipelineRun(p, uuid.NewString())
				run.StartedByUserSub, run.StartedByEmail = "owned-admin", "fixture@invalid.local"
				if err = producer.EnqueuePlannedRun(ctx, p, run); err != nil {
					t.Fatal(err)
				}
				jobID = "run:" + run.ID
			} else {
				authority := NewRegistryAuthorityService(st, nil)
				authority.Registries, authority.Environments = storepg.NewRegistryConfigStore(db), storepg.NewEnvironmentStore(db)
				if !storepg.RegistryAdmissionStoresMatch(jobs, authority.Registries, authority.Environments) {
					t.Fatal("fixture authority not queue-pool-bound")
				}
				producer.RequireRegistryPins = true
				producer.PrepareRegistry = func(ctx context.Context, p *model.Pipeline) (*model.RegistryAuthorityPlan, error) {
					return PreparePipelineRegistryAuthority(ctx, authority, p)
				}
				a := &PipelineAutomation{Preparer: preparer, Triggers: &TriggerService{Store: storepg.NewTriggerStore(db)}, Jobs: jobs, Authorize: func(context.Context, *model.TriggerRule, *model.Pipeline) error { return nil }, RequireRegistryPreflight: true, RegistryPreflight: func(ctx context.Context, p *model.Pipeline) error {
					return CheckPipelineRegistryPreflight(ctx, authority, p)
				}, Enqueue: producer.EnqueueSourceRun}
				rule := &model.TriggerRule{ID: uuid.NewString(), Name: "owned main", Enabled: true, TargetKind: "pipeline", TargetID: p.ID, RefKind: "branch", Match: "exact", Include: []string{"main"}, Approval: policy}
				if _, err = a.Preview(ctx, rule, p.Source.FullRef); err != nil {
					t.Fatal(err)
				}
				now := time.Now().UTC()
				rule.ReviewedAt, rule.ReviewedBySubject = &now, "owned-admin"
				if err = a.Triggers.SaveRule(ctx, rule); err != nil {
					t.Fatal(err)
				}
				event := model.VerifiedGitEvent{Source: rule.Source, DeliveryID: "owned-" + policy, Event: "push", FullRef: p.Source.FullRef, CommitSHA: f.commit, PayloadHash: strings.Repeat("d", 64)}
				delivery, _, err := a.Triggers.AcceptVerified(ctx, event)
				if err != nil || len(delivery.Decisions) != 1 {
					t.Fatal("fixture delivery not admitted", err)
				}
				intent, err := a.Triggers.Store.GetIntent(ctx, delivery.Decisions[0].IntentID)
				if err != nil {
					t.Fatal(err)
				}
				if policy == "review" {
					if err = a.ApproveIntent(ctx, intent, "owned-reviewer"); err != nil {
						t.Fatal(err)
					}
					intent, err = a.Triggers.Store.GetIntent(ctx, intent.ID)
					if err != nil || intent.Review == nil {
						t.Fatal("exact review not retained", err)
					}
				}
				if err = a.Dispatch(ctx, intent); err != nil {
					t.Fatal(err)
				}
				accepted, err := a.Triggers.Store.GetIntent(ctx, intent.ID)
				if err != nil || accepted.Receipt == nil {
					t.Fatal("atomic receipt absent", err)
				}
				receipt, jobID, intentID = accepted.Receipt, accepted.Receipt.JobID, intent.ID
			}
			job, err := jobs.Get(ctx, jobID)
			if err != nil || job.Status != jobqueue.StatusPending || job.Attempts != 0 {
				t.Fatal("seed job was not untouched pending admission", err)
			}
			var accepted runJobPayload
			if json.Unmarshal(job.Payload, &accepted) != nil || accepted.Run == nil || accepted.Pipeline == nil || accepted.Run.Status != model.RunStatusPending || (!strings.HasPrefix(policy, "regular-") && (accepted.Source == nil || accepted.Trigger == nil || accepted.Registry == nil || accepted.Run.Source == nil || accepted.Run.Source.CheckedOutAt != nil)) {
				t.Fatal("current emitted source/authority snapshot incomplete")
			}
			// Preserve the admitted public view, as a successfully returned API does.
			if err = st.Runs.Create(ctx, accepted.Run.Clone()); err != nil {
				t.Fatal(err)
			}
			digest, err := jobqueue.PayloadDigest(job.Payload)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(job.Payload), strings.Repeat("fixture-", 8)) || strings.Contains(string(job.Payload), f.base) {
				t.Fatal("private fixture data entered accepted JSON")
			}
			cases = append(cases, map[string]any{"name": policy, "jobId": job.ID, "runId": accepted.Run.ID, "pipelineId": p.ID, "pipelineVersion": p.Version, "intentId": intentID, "receipt": receipt, "payloadDigest": digest, "payload": json.RawMessage(job.Payload), "sourceBound": !strings.HasPrefix(policy, "regular-")})
		}
		groups[consumer] = map[string]any{"schema": schema, "cases": cases}
		db.Close()
		st.Close()
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil || os.WriteFile(path, append(encoded, '\n'), 0600) != nil {
		t.Fatal(fmt.Errorf("safe seed manifest unavailable"))
	}
}
