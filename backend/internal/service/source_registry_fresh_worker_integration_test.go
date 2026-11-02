//go:build integration

package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/build/builder"
	"github.com/santapong/cooker/internal/build/pusher"
	cookercrypto "github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/deploy/deployer"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/logstore"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/secrets"
	"github.com/santapong/cooker/internal/secrets/database"
	"github.com/santapong/cooker/internal/source/connection"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
	storepg "github.com/santapong/cooker/internal/store/postgres"
)

// TestSourceRegistryFreshWorkerRevocation proves a new OS worker's first claim,
// not a retained receipt retry or an in-process manual Handle invocation.
func TestSourceRegistryFreshWorkerRevocation(t *testing.T) {
	if os.Getenv("COOKER_FRESH_WORKER_ACCEPTANCE") != "owned-local-v1" {
		t.Skip("explicit owned PostgreSQL fresh-worker harness required")
	}
	nonce := os.Getenv("COOKER_FRESH_WORKER_NONCE")
	if u, err := uuid.Parse(nonce); err != nil || u == uuid.Nil || u.String() != nonce {
		t.Fatal("invalid fixture ownership")
	}
	network := "cooker-fresh-worker-" + nonce + "-net"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if os.Getenv("COOKER_FRESH_WORKER_NETWORK") != network || nativeRuntimeCommand(ctx, t, "docker", "network", "inspect", "--format", `{{index .Labels "cooker.fixture"}}`, network) != nonce || nativeRuntimeCommand(ctx, t, "docker", "network", "inspect", "--format", `{{.Internal}}`, network) != "true" {
		t.Fatal("owned internal fixture network required")
	}
	dsn, err := freshRevocationDSN(os.Getenv("COOKER_FRESH_WORKER_DATABASE_URL"), "")
	if err != nil {
		t.Fatal("owned database required")
	}
	var results []freshRevocationCaseEvidence
	kinds := []string{"native", "app"}
	if os.Getenv("COOKER_FRESH_WORKER_EXIT_AFTER_COMMIT") == "owned-local-v1" {
		kinds = []string{"app"}
	}
	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			result := freshRevocationCase(ctx, t, nonce, kind, dsn)
			results = append(results, result)
			if path := os.Getenv("COOKER_FRESH_WORKER_EVIDENCE"); path != "" {
				data, _ := json.MarshalIndent(result, "", "  ")
				if os.WriteFile(path+"."+kind+".json", append(data, '\n'), 0600) != nil {
					t.Fatal("safe per-case evidence persistence failed")
				}
			}
		})
	}
	if len(results) != len(kinds) {
		t.Fatal("both independent claims are required")
	}
	data, err := json.MarshalIndent(map[string]any{"scope": "owned-local-first-claim-registry-revocation-v1", "cases": results, "remainingFullGates": []string{"private base authentication", "in-flight revocation", "process-loss recovery", "hosted/UI acceptance", "other App preflight rejection reasons"}}, "", "  ")
	if err != nil {
		t.Fatal("safe evidence encoding failed")
	}
	if path := os.Getenv("COOKER_FRESH_WORKER_EVIDENCE"); path != "" {
		if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
			t.Fatal("safe evidence persistence failed")
		}
	}
}

type freshRevocationCaseEvidence struct {
	Kind                 string                  `json:"kind"`
	JobID                string                  `json:"jobId"`
	RunID                string                  `json:"runId"`
	PipelineID           string                  `json:"pipelineId"`
	Subject              model.RegistrySubject   `json:"subject"`
	AcceptedCommit       string                  `json:"acceptedCommit"`
	PayloadDigest        string                  `json:"payloadDigest"`
	RegistryVersion      int64                   `json:"registryVersion"`
	RevokedVersion       int64                   `json:"revokedVersion"`
	PendingAttempts      int                     `json:"pendingAttempts"`
	JobStatus            jobqueue.Status         `json:"jobStatus"`
	RunStatus            model.RunStatus         `json:"runStatus"`
	ReceiptUnchanged     bool                    `json:"receiptUnchanged"`
	PayloadUnchanged     bool                    `json:"payloadUnchanged"`
	SourceUnchecked      bool                    `json:"sourceUnchecked"`
	NoRunOrStageStart    bool                    `json:"noRunOrStageStart"`
	StageLogs            int                     `json:"stageLogEntries"`
	AppHistoryRows       int                     `json:"appHistoryRows"`
	RegistryRequests     int32                   `json:"postAdmissionRegistryRequests"`
	NoRuntime            bool                    `json:"noNewRuntimeOrImage"`
	NoPlaintextInRecords bool                    `json:"syntheticCanariesAbsent"`
	Worker               freshRevocationChildOut `json:"worker"`
}

// Keys and paths travel over private inherited pipe descriptors only. This
// structure is never logged, written to an artifact, or supplied as argv/env.
type freshRevocationChildIn struct {
	DSN, Nonce, Kind, Schema, JobID, RunID, RegistryID string
	Key                                                []byte
	ParentPID                                          int
	ExitAfterCommit                                    bool
}

type freshRevocationChildOut struct {
	PID                 int              `json:"pid"`
	ParentPID           int              `json:"parentPid"`
	ClaimJobID          string           `json:"claimJobId"`
	ClaimAttempts       int              `json:"claimAttempts"`
	ClaimOwner          string           `json:"claimOwner"`
	ClaimRunning        bool             `json:"claimRunning"`
	ClaimHasStartedAt   bool             `json:"claimHasStartedAt"`
	GrantRevoked        bool             `json:"grantRevokedBeforePoolStart"`
	Metadata            map[string]int32 `json:"metadataReads"`
	Effects             map[string]int32 `json:"effects"`
	JobStatus           jobqueue.Status  `json:"jobStatus"`
	ExitedAfterCommit   bool             `json:"exitedAfterAtomicDecision,omitempty"`
	CommittedHistoryID  string           `json:"committedHistoryId,omitempty"`
	CommittedFinishedAt *time.Time       `json:"committedFinishedAt,omitempty"`
}

func freshRevocationCase(ctx context.Context, t *testing.T, nonce, kind string, rootDSN *url.URL) freshRevocationCaseEvidence {
	t.Helper()
	schema := "fresh_revocation_" + kind + "_" + strings.ReplaceAll(nonce, "-", "")
	admin, err := sql.Open("postgres", rootDSN.String())
	if err != nil {
		t.Fatal("owned admin connection failed")
	}
	defer admin.Close()
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal("owned schema creation failed")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(cleanup, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error("owned schema cleanup failed")
		}
	}()
	u := *rootDSN
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	st, err := storepg.NewStore(ctx, u.String())
	if err != nil {
		t.Fatal("owned primary store failed")
	}
	defer st.Close()
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal("owned queue pool failed")
	}
	defer db.Close()
	db.SetMaxOpenConns(6)
	base := t.TempDir()
	repository := filepath.Join(base, "repository")
	if os.Mkdir(repository, 0700) != nil || os.WriteFile(filepath.Join(repository, "Dockerfile"), []byte("FROM scratch\nCOPY message.txt /message.txt\n"), 0600) != nil || os.WriteFile(filepath.Join(repository, "message.txt"), []byte("accepted-first-claim-"+kind), 0600) != nil || os.WriteFile(filepath.Join(repository, ".dockerignore"), []byte(".git\n"), 0600) != nil {
		t.Fatal("owned Git preparation failed")
	}
	nativeRuntimeCommand(ctx, t, "git", "-C", repository, "init", "--initial-branch=main")
	nativeRuntimeCommand(ctx, t, "git", "-C", repository, "add", "Dockerfile", "message.txt", ".dockerignore")
	nativeRuntimeCommand(ctx, t, "git", "-C", repository, "-c", "user.name=Owned fixture", "-c", "user.email=fixture@invalid.local", "commit", "-m", "owned fresh worker source")
	commit := nativeRuntimeCommand(ctx, t, "git", "-C", repository, "rev-parse", "HEAD")
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal("fixture key generation failed")
	}
	defer clear(key)
	codec, err := cookercrypto.NewCodec(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal("fixture codec failed")
	}
	sourceCanary, registryCanary, envCanary := "fixture-source-"+uuid.NewString(), "fixture-registry-"+uuid.NewString(), "fixture-env-"+uuid.NewString()
	repoName := "owned/first-claim"
	if kind == "app" {
		// Stable borrowed App fixture validates this exact public identity.
		repoName = "owned/app-runtime"
	}
	connections := &connection.Service{Store: &connection.Postgres{DB: db}, Codec: codec}
	c := &connection.Connection{Name: "Owned fresh worker " + kind, Provider: "github", Enabled: true, Repositories: []string{repoName}}
	if err := connections.Create(ctx, c, sourceCanary); err != nil {
		t.Fatal("fixture source connection failed")
	}
	manager := database.New(storepg.NewEnvironmentStore(db), codec)
	envID, incarnation := uuid.NewString(), uuid.NewString()
	registryEnvID := "registry_" + strings.ReplaceAll(incarnation, "-", "")
	for id, name := range map[string]string{envID: "Owned first-claim workload", registryEnvID: "Owned first-claim registry private"} {
		if err := st.Environments.Create(ctx, &model.Environment{ID: id, Name: name, PlainVars: map[string]string{}, CreatedAt: time.Now().UTC()}); err != nil {
			t.Fatal("fixture environment creation failed")
		}
	}
	if manager.Put(ctx, envID, "WORKLOAD_CANARY", []byte(envCanary)) != nil || manager.Put(ctx, registryEnvID, "password", []byte(registryCanary)) != nil {
		t.Fatal("fixture encrypted credential setup failed")
	}
	var requests atomic.Int32
	registryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("WWW-Authenticate", `Basic realm="owned-first-claim"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer registryServer.Close()
	registryHost := strings.TrimPrefix(registryServer.URL, "http://")
	subjectID := uuid.NewString()
	config := &model.RegistryConfig{ID: uuid.NewString(), Name: "Owned first-claim registry", URL: registryServer.URL, Username: "owned-fixture", PasswordRef: "registry:v2:" + incarnation}
	if kind == "native" {
		config.AllowedPipelineIDs = []string{subjectID}
	} else {
		config.AllowedAppIDs = []string{subjectID}
	}
	if st.Registries.Create(ctx, config) != nil {
		t.Fatal("fixture registry metadata failed")
	}
	authority := NewRegistryAuthorityService(st, manager)
	authority.Registries, authority.Environments = storepg.NewRegistryConfigStore(db), storepg.NewEnvironmentStore(db)
	jobs := jobqueue.NewPostgres(db)
	jobs.OnEnqueue = func(ctx context.Context, tx *sql.Tx, job *jobqueue.Job) error {
		if err := storepg.SyncRegistryAuthorityAdmission(ctx, tx, job); err != nil {
			return err
		}
		return storepg.SyncTriggerAdmission(ctx, tx, job)
	}
	jobs.OnTransition = storepg.SyncJobRun
	if !storepg.RegistryAdmissionStoresMatch(jobs, authority.Registries, authority.Environments) {
		t.Fatal("fixture registry admission pools mismatched")
	}
	var jobID, runID, pipelineID, intentID string
	var acceptedReceipt any
	var registryPlan *model.RegistryAuthorityPlan
	workload := "cooker-revoke-" + strings.ReplaceAll(nonce, "-", "")[:18] + "-" + kind
	image := registryHost + "/owned/first-claim-" + nonce + "-" + kind + ":accepted"
	if nativeRuntimeExists(ctx, "container", workload) || nativeRuntimeExists(ctx, "image", image) {
		t.Fatal("fixture runtime identity exists")
	}
	if kind == "native" {
		p := &model.Pipeline{ID: subjectID, Name: "Owned first-claim native", Source: &model.PipelineSourceConfig{ConnectionID: c.ID, Repository: "owned/first-claim", FullRef: "refs/heads/main"}, Stages: []model.Stage{
			{ID: "build", Name: "Build", Type: model.StageTypeBuild, Config: model.StageConfig{Context: ".", Dockerfile: "Dockerfile", Tags: []string{image}, RegistryConnectionIDs: []string{config.ID}}},
			{ID: "push", Name: "Push", Type: model.StageTypePush, Config: model.StageConfig{Image: "${stages.build.tag}", Repository: image, RegistryConnectionIDs: []string{config.ID}}},
			{ID: "deploy", Name: "Deploy", Type: model.StageTypeDeploy, EnvironmentID: envID, Config: model.StageConfig{Image: "${stages.push.image}", RuntimeName: workload, DeployRuntime: "docker", SecretRefs: []string{"WORKLOAD_CANARY"}, RegistryConnectionIDs: []string{config.ID}}},
		}, Edges: []model.Edge{{ID: "b-p", Source: "build", Target: "push"}, {ID: "p-d", Source: "push", Target: "deploy"}}}
		p.SetDefaults()
		if st.Pipelines.Create(ctx, p) != nil {
			t.Fatal("fixture native pipeline failed")
		}
		p, err = st.Pipelines.Get(ctx, p.ID)
		if err != nil {
			t.Fatal("fixture native pipeline reload failed")
		}
		preparer := &PipelineSourcePreparer{Store: st, Connections: connections, Source: &nativeRuntimeGit{root: repository, base: base, repositoryID: "owned-first-claim-" + nonce}}
		a := &PipelineAutomation{Preparer: preparer, Triggers: &TriggerService{Store: storepg.NewTriggerStore(db)}, Jobs: jobs, Authorize: func(context.Context, *model.TriggerRule, *model.Pipeline) error { return nil }, RequireRegistryPreflight: true, RegistryPreflight: func(ctx context.Context, p *model.Pipeline) error {
			return CheckPipelineRegistryPreflight(ctx, authority, p)
		}}
		rule := &model.TriggerRule{ID: uuid.NewString(), Name: "Owned first-claim main", Enabled: true, TargetKind: "pipeline", TargetID: p.ID, RefKind: "branch", Match: "exact", Include: []string{"main"}, Approval: "automatic"}
		if _, err := a.Preview(ctx, rule, p.Source.FullRef); err != nil {
			t.Fatal("fixture native preview failed")
		}
		now := time.Now().UTC()
		rule.ReviewedAt, rule.ReviewedBySubject = &now, "owned-fixture-admin"
		if a.Triggers.SaveRule(ctx, rule) != nil {
			t.Fatal("fixture native rule failed")
		}
		hash := sha256.Sum256([]byte("first-claim-delivery-" + nonce))
		delivery, _, err := a.Triggers.AcceptVerified(ctx, model.VerifiedGitEvent{Source: rule.Source, DeliveryID: "revoke-" + nonce, Event: "push", FullRef: p.Source.FullRef, CommitSHA: commit, PayloadHash: hex.EncodeToString(hash[:])})
		if err != nil || len(delivery.Decisions) != 1 {
			t.Fatal("fixture native event failed")
		}
		intent, err := a.Triggers.Store.GetIntent(ctx, delivery.Decisions[0].IntentID)
		if err != nil {
			t.Fatal("fixture native intent failed")
		}
		executor := NewExecutor(WithEnvironmentResolver(&AppEnvResolver{Environments: st.Environments, Secrets: manager}))
		producer := NewJobQueueEnqueuer(jobs, st.Pipelines)
		producer.RequireRegistryPins, producer.AdmissionGuard = true, storepg.NewTriggerStore(db)
		producer.PrepareRegistry = func(ctx context.Context, p *model.Pipeline) (*model.RegistryAuthorityPlan, error) {
			return PreparePipelineRegistryAuthority(ctx, authority, p)
		}
		producer.PrepareSecrets = executor.PreparePipelineSecrets
		a.Enqueue = producer.EnqueueSourceRun
		if a.Dispatch(ctx, intent) != nil {
			t.Fatal("actual native producer admission failed")
		}
		accepted, err := a.Triggers.Store.GetIntent(ctx, intent.ID)
		if err != nil || accepted.Receipt == nil {
			t.Fatal("native receipt absent")
		}
		intentID, acceptedReceipt = intent.ID, accepted.Receipt
		jobID, runID, pipelineID = accepted.Receipt.JobID, accepted.Receipt.RunID, p.ID
		job, err := jobs.Get(ctx, jobID)
		var plan runJobPayload
		if err != nil || json.Unmarshal(job.Payload, &plan) != nil || plan.Source == nil || plan.Trigger == nil || plan.Registry == nil || plan.Secrets == nil || plan.Run == nil || plan.Run.Source == nil || plan.Source.CommitSHA != commit || plan.Run.Source.CheckedOutAt != nil {
			t.Fatal("native immutable evidence absent")
		}
		registryPlan = plan.Registry
		// Real lifecycle SQL reconstruction creates the pending accepted view.
		if err := st.Runs.Create(ctx, plan.Run.Clone()); err != nil && !errors.Is(err, store.ErrConflict) {
			t.Fatal("native pending view failed")
		}
	} else {
		app := &model.App{ID: subjectID, Name: workload, GitHubRepo: repoName, Branch: "main", EnvironmentID: envID, RegistryRef: registryHost + "/owned", DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost, Prefix: workload}, BuildPlan: &model.BuildPlan{Kind: model.BuildPlanDockerfile, Path: "Dockerfile", SourceConnectionID: c.ID, SourceConnectionVersion: c.Version, SourceRepositoryID: "owned-first-claim-app-" + nonce, RegistryConnectionIDs: []string{config.ID}}}
		if st.Apps.Create(ctx, app) != nil {
			t.Fatal("fixture App creation failed")
		}
		app, err = st.Apps.Get(ctx, app.ID)
		if err != nil {
			t.Fatal("fixture App reload failed")
		}
		source := &appRuntimeGit{root: repository, base: base, connections: connections.Store, connectionID: c.ID, version: c.Version, repositoryID: app.BuildPlan.SourceRepositoryID, acceptedCommit: commit}
		d := NewAppDeployer(NewExecutor(WithRegistryAuthority(authority)), "")
		d.EnvResolver = &AppEnvResolver{Environments: st.Environments, Secrets: manager}
		d.SourceCheckout = source.Checkout
		queue := &AppDeployQueue{Jobs: jobs, Store: st, Deployer: d, RegistryAuthority: authority, RequireRegistryPins: true, RequireSecretPins: true, RequireImagePins: true, AdmissionGuard: storepg.NewTriggerStore(db), ResolveSource: source.Resolve}
		queue.PrepareImages = func(ctx context.Context, plan *AppDeployPlan) (*AppImageResolution, error) {
			return d.PrepareImages(ctx, plan, RegistryImageResolver(nil))
		}
		receipt, err := queue.EnqueueIntent(ctx, app, "", &model.PipelineRun{StartedByUserSub: "owned-fixture-admin", StartedByEmail: "fixture@example.invalid"}, uuid.NewString())
		if err != nil {
			t.Fatal("actual manual App producer admission failed")
		}
		acceptedReceipt = receipt
		jobID, runID, pipelineID = receipt.JobID, receipt.RunID, receipt.PipelineID
		job, err := jobs.Get(ctx, jobID)
		var plan AppDeployPlan
		if err != nil || json.Unmarshal(job.Payload, &plan) != nil || plan.Registry == nil || plan.Images == nil || plan.Secrets == nil || plan.Secrets.Mode != "database" || plan.Commit != commit || plan.Trigger != nil {
			t.Fatal("manual App immutable evidence absent")
		}
		registryPlan = plan.Registry
	}
	job, err := jobs.Get(ctx, jobID)
	pending, runErr := st.Runs.Get(ctx, runID)
	if err != nil || runErr != nil || job.Status != jobqueue.StatusPending || job.Attempts != 0 || job.StartedAt != nil || job.LockedAt != nil || job.LockedBy != "" || job.MaxAttempts != 1 || pending.Status != model.RunStatusPending || pending.StartedAt != nil || pending.FinishedAt != nil || registryPlan.Validate() != nil || len(registryPlan.Connections) != 1 || registryPlan.Connections[config.ID].Credential.Mode != "database" {
		t.Fatal("job is not a newly admitted pending attempt-zero job")
	}
	for _, stage := range pending.StageRuns {
		if stage.StartedAt != nil || stage.Logs != "" || stage.Status != model.RunStatusPending {
			t.Fatal("pre-admission stage effect detected")
		}
	}
	digest, err := jobqueue.PayloadDigest(job.Payload)
	if err != nil {
		t.Fatal("admitted plan digest unavailable")
	}
	registryBefore := requests.Load()
	grantStore := storepg.NewRegistryConfigStore(db)
	revoked, err := grantStore.UpdateGrants(ctx, config.ID, config.Version, []string{}, []string{})
	if err != nil || revoked.Version != config.Version+1 || len(revoked.AllowedAppIDs) != 0 || len(revoked.AllowedPipelineIDs) != 0 {
		t.Fatal("versioned post-admission grant revocation failed")
	}
	child, childLog := freshRevocationExec(ctx, t, freshRevocationChildIn{DSN: u.String(), Nonce: nonce, Kind: kind, Schema: schema, JobID: jobID, RunID: runID, RegistryID: config.ID, Key: key, ParentPID: os.Getpid(), ExitAfterCommit: os.Getenv("COOKER_FRESH_WORKER_EXIT_AFTER_COMMIT") == "owned-local-v1"}, base)
	if child.PID == os.Getpid() || child.ParentPID != os.Getpid() || child.ClaimJobID != jobID || child.ClaimAttempts != 1 || !child.ClaimRunning || child.ClaimOwner == "" || !child.ClaimHasStartedAt || !child.GrantRevoked || len(child.Effects) != 14 || len(child.Metadata) != 2 || child.Metadata["registry"] < 2 {
		t.Fatal("new OS process did not make the first actual PostgreSQL claim")
	}
	for _, n := range child.Effects {
		if n != 0 {
			t.Fatal("fresh revoked worker reached an execution effect")
		}
	}
	// Reconstruct results through independently reopened SQL pools/stores.
	view, err := storepg.NewStore(ctx, u.String())
	if err != nil {
		t.Fatal("fresh result store failed")
	}
	defer view.Close()
	readDB, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal("fresh result queue pool failed")
	}
	defer readDB.Close()
	readJobs := jobqueue.NewPostgres(readDB)
	if child.ExitedAfterCommit {
		if kind != "app" || child.JobStatus != jobqueue.StatusRunning || child.CommittedHistoryID != store.AppPreflightHistoryID(runID) || child.CommittedFinishedAt == nil {
			t.Fatal("child did not exit at committed preflight boundary")
		}
		beforeRun, e := view.Runs.Get(ctx, runID)
		beforeHistory, h := view.AppDeploys.ListByApp(ctx, subjectID, 100)
		if e != nil || h != nil || len(beforeHistory) != 1 || !beforeRun.FinishedAt.Equal(*child.CommittedFinishedAt) {
			t.Fatal("committed decision absent before expiry")
		}
		readJobs.OnTransition = storepg.SyncJobRun
		recovered, e := readJobs.RecoverExpired(ctx, time.Now().Add(time.Second))
		if e != nil || len(recovered) != 1 {
			t.Fatal("fresh-pool postcommit expiry failed")
		}
		afterRun, e := view.Runs.Get(ctx, runID)
		afterHistory, h := view.AppDeploys.ListByApp(ctx, subjectID, 100)
		if e != nil || h != nil || !reflect.DeepEqual(beforeRun, afterRun) || !reflect.DeepEqual(beforeHistory, afterHistory) {
			t.Fatal("postcommit process loss overwrote business decision")
		}
		raw, e := readJobs.Get(ctx, jobID)
		if e != nil {
			t.Fatal("recovered job unavailable")
		}
		f, e := store.DecodeAppPreflightFailure(raw.Payload)
		if e != nil {
			t.Fatal("retained preflight acceptance invalid")
		}
		stale := store.WithRunClaim(ctx, jobID, child.ClaimOwner, nil)
		if e := view.AppDeploys.(store.AppPreflightFailureRecorder).RecordAppPreflightFailure(stale, f); !errors.Is(e, store.ErrConflict) {
			t.Fatal("lost child claim regained outcome authority")
		}
	}
	finalJob, err := readJobs.Get(ctx, jobID)
	run, runErr := view.Runs.Get(ctx, runID)
	if err != nil || runErr != nil || finalJob.Attempts != 1 || finalJob.MaxAttempts != 1 || finalJob.StartedAt == nil || (!child.ExitedAfterCommit && finalJob.FinishedAt == nil) || finalJob.LockedBy != "" || run.Status != model.RunStatusFailed || run.StartedAt != nil || run.FinishedAt == nil {
		t.Fatal("fresh-claim durable terminal evidence differs")
	}
	if kind == "native" && (finalJob.Status != jobqueue.StatusFailed || run.Source == nil || run.Source.CheckedOutAt != nil || run.Source.CommitSHA != commit) || kind == "app" && (((!child.ExitedAfterCommit && finalJob.Status != jobqueue.StatusSucceeded) || (child.ExitedAfterCommit && finalJob.Status != jobqueue.StatusNeedsAttention)) || run.Source != nil || run.Error != "accepted registry authority is unavailable or changed; review and start a new deployment") {
		t.Fatal("queue handler completion must remain distinct from business failure")
	}
	for _, stage := range run.StageRuns {
		if stage.StartedAt != nil || stage.Logs != "" || stage.Outputs != nil && len(stage.Outputs) != 0 {
			t.Fatal("revoked worker published stage effects")
		}
	}
	finalDigest, err := jobqueue.PayloadDigest(finalJob.Payload)
	if err != nil || finalDigest != digest {
		t.Fatal("worker replaced accepted pins or plan")
	}
	var receipt any
	if kind == "native" {
		intent, err := storepg.NewTriggerStore(readDB).GetIntent(ctx, intentID)
		if err != nil || intent.Receipt == nil {
			t.Fatal("retained native receipt unavailable")
		}
		receipt = intent.Receipt
	} else {
		r, err := receiptForPlan(finalJob, subjectID, commit)
		if err != nil {
			t.Fatal("retained manual App receipt unavailable")
		}
		receipt = r
	}
	if !reflect.DeepEqual(receipt, acceptedReceipt) {
		t.Fatal("first-claim failure changed the original receipt")
	}
	var stages, histories int
	if readDB.QueryRowContext(ctx, "SELECT count(*) FROM stage_log_entries WHERE run_id=$1", runID).Scan(&stages) != nil || stages != 0 {
		t.Fatal("revoked worker emitted durable stage logs")
	}
	if kind == "app" {
		rows, err := view.AppDeploys.ListByApp(ctx, subjectID, 10)
		if err != nil {
			t.Fatal("App history lookup failed")
		}
		histories = len(rows)
		if histories != 1 {
			t.Fatal("App preflight rejection must have exactly one failed history row")
		}
		for _, row := range rows {
			if row.RunID != runID || row.Status != model.RunStatusFailed || row.Kind != model.AppDeployKindRejected || row.ImageRef != "" || row.Digest != "" || row.PipelineID != pipelineID || row.ID != store.AppPreflightHistoryID(runID) || !row.CreatedAt.Equal(*run.FinishedAt) {
				t.Fatal("App preflight history invents successful deployment")
			}
		}
	}
	graph, err := view.Pipelines.Get(ctx, pipelineID)
	if err != nil {
		t.Fatal("durable graph absent")
	}
	public, _ := json.Marshal([]any{run, graph, receipt})
	for _, secret := range []string{sourceCanary, registryCanary, envCanary, base64.StdEncoding.EncodeToString([]byte("owned-fixture:" + registryCanary)), base64.StdEncoding.EncodeToString([]byte(registryCanary)), base64.StdEncoding.EncodeToString(key), hex.EncodeToString(key), base} {
		if bytes.Contains(finalJob.Payload, []byte(secret)) || bytes.Contains(public, []byte(secret)) || bytes.Contains(childLog, []byte(secret)) {
			t.Fatal("synthetic credential or private path entered retained records")
		}
	}
	noRuntime := !nativeRuntimeExists(ctx, "container", workload) && !nativeRuntimeExists(ctx, "image", image)
	if requests.Load() != registryBefore || !noRuntime {
		t.Fatal("revoked worker caused registry or runtime effects")
	}
	return freshRevocationCaseEvidence{Kind: kind, JobID: jobID, RunID: runID, PipelineID: pipelineID, Subject: registryPlan.Subject, AcceptedCommit: commit, PayloadDigest: digest, RegistryVersion: config.Version, RevokedVersion: revoked.Version, PendingAttempts: 0, JobStatus: finalJob.Status, RunStatus: run.Status, ReceiptUnchanged: true, PayloadUnchanged: true, SourceUnchecked: kind == "app" || run.Source.CheckedOutAt == nil, NoRunOrStageStart: true, StageLogs: stages, AppHistoryRows: histories, RegistryRequests: requests.Load() - registryBefore, NoRuntime: noRuntime, NoPlaintextInRecords: true, Worker: child}
}

func freshRevocationExec(ctx context.Context, t *testing.T, input freshRevocationChildIn, base string) (freshRevocationChildOut, []byte) {
	t.Helper()
	inRead, inWrite, err := os.Pipe()
	if err != nil {
		t.Fatal("private worker IPC unavailable")
	}
	defer inRead.Close()
	defer inWrite.Close()
	outRead, outWrite, err := os.Pipe()
	if err != nil {
		t.Fatal("private result IPC unavailable")
	}
	defer outRead.Close()
	defer outWrite.Close()
	home, config := filepath.Join(base, "child-home"), filepath.Join(base, "child-docker")
	if os.Mkdir(home, 0700) != nil || os.Mkdir(config, 0700) != nil {
		t.Fatal("isolated worker profile failed")
	}
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSourceRegistryFreshWorkerChild$", "-test.v", "-test.timeout=40s")
	cmd.Dir = base
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=" + home, "DOCKER_CONFIG=" + config, "TMPDIR=" + base, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "COOKER_FRESH_WORKER_CHILD=private-ipc-v1", "COOKER_OUTPUTS_ENABLED=true"}
	cmd.ExtraFiles = []*os.File{inRead, outWrite}
	var log bytes.Buffer
	cmd.Stdout, cmd.Stderr = &log, &log
	if cmd.Start() != nil {
		t.Fatal("new OS worker could not start")
	}
	_ = inRead.Close()
	_ = outWrite.Close()
	encoded := make(chan error, 1)
	go func() { encoded <- json.NewEncoder(inWrite).Encode(input); _ = inWrite.Close() }()
	var result freshRevocationChildOut
	readResult := make(chan error, 1)
	go func() { readResult <- json.NewDecoder(io.LimitReader(outRead, 16384)).Decode(&result) }()
	waitErr := cmd.Wait()
	encodeErr, resultErr := <-encoded, <-readResult
	expectedExit := input.ExitAfterCommit && cmd.ProcessState.ExitCode() == 23 && result.ExitedAfterCommit
	if (waitErr != nil && !expectedExit) || encodeErr != nil || resultErr != nil {
		t.Fatal("fresh OS worker failed; no unfiltered child output published")
	}
	if result.PID != cmd.ProcessState.Pid() || result.PID == os.Getpid() {
		t.Fatal("worker PID evidence mismatch")
	}
	return result, log.Bytes()
}

func freshRevocationDSN(raw, schema string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "postgres" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.Path != "/cooker_fresh_worker_test" || u.User == nil || u.User.Username() != "fresh_worker_fixture" || u.Query().Get("sslmode") != "disable" || u.Fragment != "" {
		return nil, errors.New("owned fresh-worker database required")
	}
	if _, ok := u.User.Password(); ok {
		return nil, errors.New("database credential forbidden")
	}
	if schema == "" && len(u.Query()) != 1 || schema != "" && (len(u.Query()) != 2 || u.Query().Get("search_path") != schema) {
		return nil, errors.New("owned schema required")
	}
	return u, nil
}

// This entry is launched only by the parent fixture with private IPC. It builds
// independent real SQL pools and claims through jobqueue.Pool, never Handle-only.
func TestSourceRegistryFreshWorkerChild(t *testing.T) {
	if os.Getenv("COOKER_FRESH_WORKER_CHILD") != "private-ipc-v1" {
		t.Skip("fresh OS child with private IPC required")
	}
	in, out := os.NewFile(3, "private-fixture-input"), os.NewFile(4, "private-fixture-result")
	defer in.Close()
	defer out.Close()
	var input freshRevocationChildIn
	if json.NewDecoder(io.LimitReader(in, 16384)).Decode(&input) != nil || len(input.Key) != 32 || input.ParentPID != os.Getppid() || input.Kind != "native" && input.Kind != "app" {
		t.Fatal("invalid private worker control")
	}
	defer clear(input.Key)
	if u, err := uuid.Parse(input.Nonce); err != nil || u == uuid.Nil || u.String() != input.Nonce || input.Schema != "fresh_revocation_"+input.Kind+"_"+strings.ReplaceAll(input.Nonce, "-", "") {
		t.Fatal("invalid worker ownership")
	}
	u, err := freshRevocationDSN(input.DSN, input.Schema)
	if err != nil {
		t.Fatal("unowned child database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	st, err := storepg.NewStore(ctx, u.String())
	if err != nil {
		t.Fatal("fresh child primary pool failed")
	}
	defer st.Close()
	db, err := sql.Open("postgres", u.String())
	if err != nil || db.PingContext(ctx) != nil {
		t.Fatal("fresh child queue pool failed")
	}
	defer db.Close()
	db.SetMaxOpenConns(6)
	codec, err := cookercrypto.NewCodec(base64.StdEncoding.EncodeToString(input.Key))
	if err != nil {
		t.Fatal("fresh child codec failed")
	}
	counters := &freshRevocationCounters{}
	envs := &freshRevocationEnvironments{EnvironmentStore: storepg.NewEnvironmentStore(db), counters: counters}
	manager := &freshRevocationManager{Manager: database.New(envs, codec), counters: counters}
	registries := &freshRevocationRegistries{RegistryConfigStore: storepg.NewRegistryConfigStore(db), counters: counters}
	authority := &RegistryAuthorityService{Registries: registries, Environments: envs, Secrets: manager}
	st.Environments = envs
	source := &freshRevocationSource{counters: counters}
	connections := &connection.Service{Store: &freshRevocationConnections{Store: &connection.Postgres{DB: db}, counters: counters}, Codec: codec}
	preparer := &PipelineSourcePreparer{Store: st, Connections: connections, Source: source}
	jobs := jobqueue.NewPostgres(db)
	jobs.OnEnqueue = func(ctx context.Context, tx *sql.Tx, job *jobqueue.Job) error {
		counters.repin.Add(1)
		if err := storepg.SyncRegistryAuthorityAdmission(ctx, tx, job); err != nil {
			return err
		}
		return storepg.SyncTriggerAdmission(ctx, tx, job)
	}
	jobs.OnTransition = storepg.SyncJobRun
	current, err := registries.Get(ctx, input.RegistryID)
	if err != nil || len(current.AllowedAppIDs) != 0 || len(current.AllowedPipelineIDs) != 0 {
		t.Fatal("grant must already be revoked before constructing the pool")
	}
	// Permit only metadata reads; all execution dependencies fail safely and are
	// counted if reached. No substitute runtime success can satisfy this test.
	executor := NewExecutor(WithRegistryAuthority(authority), WithEnvironmentResolver(&AppEnvResolver{Environments: envs, Secrets: manager}), WithBuilder(&freshRevocationBuild{counters}), WithPusher(&freshRevocationPush{counters}), WithDockerDeployer(&freshRevocationDeploy{counters}), WithLogStore(logstore.NewPostgres(db, logstore.Limits{})), WithRunUpdater(func(ctx context.Context, run *model.PipelineRun) error {
		counters.stageProgress.Add(1)
		return st.Runs.UpdateProgress(ctx, run.ID, run.StageRuns)
	}))
	result := freshRevocationChildOut{PID: os.Getpid(), ParentPID: os.Getppid(), GrantRevoked: true}
	dispatch := jobqueue.NewRegistry()
	if input.Kind == "native" {
		a := &PipelineAutomation{Preparer: preparer, Triggers: &TriggerService{Store: storepg.NewTriggerStore(db)}, Jobs: jobs, RequireRegistryPreflight: true, RegistryPreflight: func(ctx context.Context, p *model.Pipeline) error {
			return CheckPipelineRegistryPreflight(ctx, authority, p)
		}, Authorize: func(context.Context, *model.TriggerRule, *model.Pipeline) error { return nil }}
		a.Enqueue = func(context.Context, *model.Pipeline, *model.PipelineRun, *model.PipelineSourcePlan, *model.PipelineTriggerProof) (*model.TriggerReceipt, error) {
			counters.repin.Add(1)
			return nil, errors.New("worker cannot admit replacement work")
		}
		runner := NewJobQueueRunner(st, executor, nil)
		runner.SourcePreparer, runner.NativeAutomation, runner.RegistryAuthority = preparer, a, authority
		dispatch.Register(JobKindPipelineRun, runner.Handle)
	} else {
		d := NewAppDeployer(executor, "")
		d.Apps, d.Deploys, d.SaveRun = st.Apps, st.AppDeploys, st.Runs.Update
		d.EnvResolver = &AppEnvResolver{Environments: envs, Secrets: manager}
		d.SourceCheckout = func(context.Context, *model.App, string, io.Writer) (string, error) {
			counters.checkout.Add(1)
			return "", errors.New("revoked worker cannot checkout")
		}
		d.SavePipeline = func(context.Context, *model.Pipeline) error {
			counters.stageProgress.Add(1)
			return errors.New("revoked worker cannot publish stages")
		}
		queue := &AppDeployQueue{Jobs: jobs, Store: st, Deployer: d, RegistryAuthority: authority, RequireRegistryPins: true, RequireSecretPins: true, RequireImagePins: true, Deadline: 15 * time.Second}
		queue.ResolveSource = func(context.Context, *model.App, string) (string, error) {
			counters.sourceRevision.Add(1)
			return "", errors.New("worker cannot rediscover source")
		}
		queue.PrepareImages = func(context.Context, *AppDeployPlan) (*AppImageResolution, error) {
			counters.repin.Add(1)
			return nil, errors.New("worker cannot repin images")
		}
		dispatch.Register(JobKindAppDeploy, func(c context.Context, j *jobqueue.Job) error {
			err := queue.Handle(c, j)
			if err != nil || !input.ExitAfterCommit {
				return err
			}
			r, e := st.Runs.Get(c, input.RunID)
			rows, h := st.AppDeploys.ListByApp(c, jobsAppID(j), 100)
			f, x := store.DecodeAppPreflightFailure(j.Payload)
			observed, g := jobs.Get(c, j.ID)
			if e != nil || h != nil || x != nil || g != nil || len(rows) != 1 || !store.AppPreflightMarkerValid(f, r, rows[0]) || observed.Status != jobqueue.StatusRunning {
				return errors.New("postcommit fixture decision unavailable")
			}
			result.JobStatus = observed.Status
			result.ExitedAfterCommit = true
			result.CommittedHistoryID = rows[0].ID
			result.CommittedFinishedAt = r.FinishedAt
			freshRevocationCapture(&result, counters)
			if json.NewEncoder(out).Encode(result) != nil {
				return errors.New("postcommit fixture IPC failed")
			}
			_ = out.Close()
			os.Exit(23) // Intentional owned process loss before real Pool.Complete.
			return nil
		})
	}
	pool, err := jobqueue.NewPool(jobqueue.PoolOptions{Store: jobs, Registry: dispatch, Size: 1, PollEvery: 25 * time.Millisecond, WorkerIDPrefix: "owned-fresh-worker-" + input.Kind, LeaseTimeout: 15 * time.Second, HeartbeatEvery: time.Second, OnHeartbeat: HeartbeatJobRun(st), OnRecovery: RecoverJobRun(st), ClaimContext: func(ctx context.Context, job *jobqueue.Job) context.Context {
		result.ClaimJobID, result.ClaimAttempts, result.ClaimOwner = job.ID, job.Attempts, job.LockedBy
		result.ClaimRunning, result.ClaimHasStartedAt = job.Status == jobqueue.StatusRunning, job.StartedAt != nil
		return store.WithRunClaim(ctx, job.ID, job.LockedBy, nil)
	}})
	if err != nil {
		t.Fatal("fresh PostgreSQL pool unavailable")
	}
	workerCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- pool.Run(workerCtx) }()
	stopped := false
	defer func() {
		if !stopped {
			stop()
			<-done
		}
	}()
	for {
		job, err := jobs.Get(ctx, input.JobID)
		if err != nil {
			t.Fatal("fresh claim outcome unavailable")
		}
		if job.Status.IsTerminal() {
			result.JobStatus = job.Status
			break
		}
		if job.Status == jobqueue.StatusNeedsAttention {
			t.Fatal("fresh before-effects rejection became indeterminate")
		}
		select {
		case <-ctx.Done():
			t.Fatal("fresh claim deadline")
		case <-time.After(25 * time.Millisecond):
		}
	}
	stop()
	<-done
	stopped = true
	freshRevocationCapture(&result, counters)
	if json.NewEncoder(out).Encode(result) != nil {
		t.Fatal("safe worker result IPC failed")
	}
}

func freshRevocationCapture(result *freshRevocationChildOut, counters *freshRevocationCounters) {
	result.Metadata = map[string]int32{"registry": counters.registryMetadata.Load(), "environment": counters.envMetadata.Load()}
	result.Effects = map[string]int32{"sourceConnectionReads": counters.sourceConnection.Load(), "sourceIdentity": counters.sourceIdentity.Load(), "sourceRevision": counters.sourceRevision.Load(), "checkout": counters.checkout.Load(), "repinning": counters.repin.Load(), "secretGet": counters.secretGet.Load(), "secretList": counters.secretList.Load(), "secretPin": counters.secretPin.Load(), "secretGetPinned": counters.secretPinned.Load(), "environmentResolution": counters.envResolved.Load(), "build": counters.build.Load(), "push": counters.push.Load(), "deploy": counters.deploy.Load(), "stageProgress": counters.stageProgress.Load()}
}
func jobsAppID(j *jobqueue.Job) string {
	f, err := store.DecodeAppPreflightFailure(j.Payload)
	if err != nil {
		return ""
	}
	return f.AppID
}
func TestAppPreflightChildExitAfterAtomicDecision(t *testing.T) {
	if os.Getenv("COOKER_FRESH_WORKER_EXIT_AFTER_COMMIT") != "owned-local-v1" {
		t.Skip("explicit owned child postcommit exit fixture required")
	}
	TestSourceRegistryFreshWorkerRevocation(t)
}

type freshRevocationCounters struct {
	registryMetadata, envMetadata, sourceConnection, sourceIdentity, sourceRevision, checkout, repin atomic.Int32
	secretGet, secretList, secretPin, secretPinned, envResolved, build, push, deploy, stageProgress  atomic.Int32
}

type freshRevocationManager struct {
	secrets.Manager
	counters *freshRevocationCounters
}

func (*freshRevocationManager) EnvironmentGenerationBound() bool { return true }
func (m *freshRevocationManager) Get(ctx context.Context, id, key string) ([]byte, error) {
	m.counters.secretGet.Add(1)
	return m.Manager.Get(ctx, id, key)
}
func (m *freshRevocationManager) List(ctx context.Context, id string) ([]string, error) {
	m.counters.secretList.Add(1)
	return m.Manager.List(ctx, id)
}
func (m *freshRevocationManager) Pin(context.Context, string, string) (secrets.VersionReference, error) {
	m.counters.secretPin.Add(1)
	return secrets.VersionReference{}, secrets.ErrVersionUnsupported
}
func (m *freshRevocationManager) GetPinned(context.Context, string, string, secrets.VersionReference) ([]byte, error) {
	m.counters.secretPinned.Add(1)
	return nil, secrets.ErrVersionUnsupported
}

type freshRevocationEnvironments struct {
	store.EnvironmentStore
	counters *freshRevocationCounters
}

func (s *freshRevocationEnvironments) Get(ctx context.Context, id string) (*model.Environment, error) {
	s.counters.envMetadata.Add(1)
	// ResolveDetailed is concrete in product wiring. Its first metadata read
	// provides an entry spy without changing the resolver or decrypting values.
	pc := make([]uintptr, 24)
	frames := runtime.CallersFrames(pc[:runtime.Callers(2, pc)])
	for {
		f, more := frames.Next()
		if strings.HasSuffix(f.Function, ".(*AppEnvResolver).ResolveDetailed") {
			s.counters.envResolved.Add(1)
			break
		}
		if !more {
			break
		}
	}
	return s.EnvironmentStore.Get(ctx, id)
}

type freshRevocationRegistries struct {
	store.RegistryConfigStore
	counters *freshRevocationCounters
}

func (s *freshRevocationRegistries) Get(ctx context.Context, id string) (*model.RegistryConfig, error) {
	s.counters.registryMetadata.Add(1)
	return s.RegistryConfigStore.Get(ctx, id)
}

type freshRevocationConnections struct {
	connection.Store
	counters *freshRevocationCounters
}

func (s *freshRevocationConnections) Get(ctx context.Context, id string) (*connection.Connection, error) {
	s.counters.sourceConnection.Add(1)
	return s.Store.Get(ctx, id)
}

type freshRevocationSource struct{ counters *freshRevocationCounters }

func (s *freshRevocationSource) Identity(context.Context, *connection.Connection, string) (model.GitSourceIdentity, error) {
	s.counters.sourceIdentity.Add(1)
	return model.GitSourceIdentity{}, errors.New("revoked worker cannot inspect source")
}
func (s *freshRevocationSource) Revision(context.Context, *connection.Connection, string, string) (SourceRevision, error) {
	s.counters.sourceRevision.Add(1)
	return SourceRevision{}, errors.New("revoked worker cannot inspect refs")
}
func (s *freshRevocationSource) CheckoutSource(context.Context, *model.PipelineSourcePlan, io.Writer) (string, error) {
	s.counters.checkout.Add(1)
	return "", errors.New("revoked worker cannot checkout")
}

type freshRevocationBuild struct{ counters *freshRevocationCounters }

func (s *freshRevocationBuild) Build(context.Context, builder.Request) (builder.Result, error) {
	s.counters.build.Add(1)
	return builder.Result{}, errors.New("revoked worker cannot build")
}

type freshRevocationPush struct{ counters *freshRevocationCounters }

func (s *freshRevocationPush) Push(context.Context, pusher.Request) (pusher.Result, error) {
	s.counters.push.Add(1)
	return pusher.Result{}, errors.New("revoked worker cannot push")
}

type freshRevocationDeploy struct{ counters *freshRevocationCounters }

func (s *freshRevocationDeploy) Deploy(context.Context, deployer.Request) (deployer.Result, error) {
	s.counters.deploy.Add(1)
	return deployer.Result{}, errors.New("revoked worker cannot deploy")
}

// A positive counter control prevents a falsely quiet entry spy from proving
// the zero-effect campaign. It is separate memory-only instrumentation proof.
func TestFreshRevocationCounterBoundaries(t *testing.T) {
	ctx := context.Background()
	st := memory.New()
	id := uuid.NewString()
	if st.Environments.Create(ctx, &model.Environment{ID: id, Name: "Counter control", PlainVars: map[string]string{}, CreatedAt: time.Now().UTC()}) != nil {
		t.Fatal("counter control setup failed")
	}
	codec, err := cookercrypto.NewCodec(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)))
	if err != nil {
		t.Fatal("counter control codec failed")
	}
	delegate := database.New(st.Environments, codec)
	if delegate.Put(ctx, id, "VALUE", []byte("synthetic-counter-control")) != nil {
		t.Fatal("counter control encrypted setup failed")
	}
	counters := &freshRevocationCounters{}
	envs := &freshRevocationEnvironments{EnvironmentStore: st.Environments, counters: counters}
	manager := &freshRevocationManager{Manager: database.New(envs, codec), counters: counters}
	if _, err := envs.Get(ctx, id); err != nil || counters.envMetadata.Load() != 1 || counters.envResolved.Load() != 0 {
		t.Fatal("metadata is not value resolution")
	}
	values, err := (&AppEnvResolver{Environments: envs, Secrets: manager}).ResolveDetailed(ctx, &model.App{EnvironmentID: id})
	if err != nil || values.Values["VALUE"] != "synthetic-counter-control" || counters.envResolved.Load() == 0 || counters.secretGet.Load() != 1 || counters.secretList.Load() != 1 {
		t.Fatal("real value resolution was not counted")
	}
	_, _ = manager.Pin(ctx, id, "VALUE")
	_, _ = manager.GetPinned(ctx, id, "VALUE", secrets.VersionReference{})
	source := &freshRevocationSource{counters}
	_, _ = source.Identity(ctx, nil, "")
	_, _ = source.Revision(ctx, nil, "", "")
	_, _ = source.CheckoutSource(ctx, nil, io.Discard)
	_, _ = (&freshRevocationBuild{counters}).Build(ctx, builder.Request{})
	_, _ = (&freshRevocationPush{counters}).Push(ctx, pusher.Request{})
	_, _ = (&freshRevocationDeploy{counters}).Deploy(ctx, deployer.Request{})
	for _, value := range []int32{counters.secretPin.Load(), counters.secretPinned.Load(), counters.sourceIdentity.Load(), counters.sourceRevision.Load(), counters.checkout.Load(), counters.build.Load(), counters.push.Load(), counters.deploy.Load()} {
		if value != 1 {
			t.Fatal("execution counter did not observe an attempted effect")
		}
	}
}
