//go:build integration

package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	cookercrypto "github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/source/connection"
	storepg "github.com/santapong/cooker/internal/store/postgres"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Product native approval, dispatch, SQL atomic hook and receipt-first retries.
// Branch events omit ObjectSHA, matching authenticated adapters. Provider/checkout
// remains an owned local fixture. The actual native producer prepares scoped
// credential-free registry evidence and both SQL admission hooks; no runtime,
// credential-provider or hosted source claim.
func TestNativeApprovalBranchPostgresConcurrentAdmissionAndRestartRecovery(t *testing.T) {
	for _, policy := range []string{"automatic", "review"} {
		t.Run(policy, func(t *testing.T) { testNativeApprovalBranchPostgresAdmission(t, policy) })
	}
}

func testNativeApprovalBranchPostgresAdmission(t *testing.T, policy string) {
	dsn := os.Getenv("COOKER_NATIVE_APPROVAL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("owned disposable native-approval PostgreSQL required")
	}
	ownedURL, err := nativeApprovalOwnedFixtureURL(dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "native_automation_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec("DROP SCHEMA " + schema + " CASCADE")
	u, err := url.Parse(ownedURL.String())
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	st, err := storepg.NewStore(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(8)
	preparer, p, f := nativeSourceSetup(t)
	codec, _ := cookercrypto.NewCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	connections := &connection.Service{Store: &connection.Postgres{DB: db}, Codec: codec}
	c := &connection.Connection{Name: "native fixture", Provider: "github", Enabled: true, Repositories: []string{"owned/native"}}
	if err = connections.Create(ctx, c, strings.Repeat("fixture-", 8)); err != nil {
		t.Fatal(err)
	}
	p.ID = uuid.NewString()
	p.Source.ConnectionID = c.ID
	p.CreatedAt = time.Now().UTC()
	p.UpdatedAt = p.CreatedAt
	registry := &model.RegistryConfig{ID: uuid.NewString(), Name: "native-owned-registry", URL: "https://ghcr.io/owned", Username: "owned", AllowedPipelineIDs: []string{p.ID}}
	if err = st.Registries.Create(ctx, registry); err != nil {
		t.Fatal(err)
	}
	p.Stages[0].Config.RegistryConnectionIDs = []string{registry.ID}
	if err = st.Pipelines.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	p, err = st.Pipelines.Get(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	preparer.Store = st
	preparer.Connections = connections
	jobs := jobqueue.NewPostgres(db)
	jobs.OnEnqueue = func(ctx context.Context, tx *sql.Tx, job *jobqueue.Job) error {
		if err := storepg.SyncRegistryAuthorityAdmission(ctx, tx, job); err != nil {
			return err
		}
		return storepg.SyncTriggerAdmission(ctx, tx, job)
	}
	authority := NewRegistryAuthorityService(st, nil)
	// Metadata must be explicitly bound to the queue pool, as in server boot.
	authority.Registries, authority.Environments = storepg.NewRegistryConfigStore(db), storepg.NewEnvironmentStore(db)
	if !storepg.RegistryAdmissionStoresMatch(jobs, authority.Registries, authority.Environments) {
		t.Fatal("native producer authority is not bound to admission")
	}
	a := &PipelineAutomation{Preparer: preparer, Triggers: &TriggerService{Store: storepg.NewTriggerStore(db)}, Jobs: jobs, Authorize: func(context.Context, *model.TriggerRule, *model.Pipeline) error { return nil }, RequireRegistryPreflight: true, RegistryPreflight: func(ctx context.Context, p *model.Pipeline) error {
		return CheckPipelineRegistryPreflight(ctx, authority, p)
	}}
	r := &model.TriggerRule{ID: uuid.NewString(), Name: "native main", Enabled: true, TargetKind: "pipeline", TargetID: p.ID, RefKind: "branch", Match: "exact", Include: []string{"main"}, Approval: policy}
	if _, err = a.Preview(ctx, r, p.Source.FullRef); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	r.ReviewedAt = &now
	r.ReviewedBySubject = "fixture-admin"
	if err = a.Triggers.SaveRule(ctx, r); err != nil {
		t.Fatal(err)
	}
	event := model.VerifiedGitEvent{Source: r.Source, DeliveryID: "native-delivery", Event: "push", FullRef: p.Source.FullRef, CommitSHA: f.commit, PayloadHash: strings.Repeat("d", 64)}
	d, _, err := a.Triggers.AcceptVerified(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := a.Triggers.Store.GetIntent(ctx, d.Decisions[0].IntentID)
	if err != nil {
		t.Fatal(err)
	}
	if policy == "review" {
		if err = a.ApproveIntent(ctx, intent, "fixture-reviewer"); err != nil {
			t.Fatal(err)
		}
		intent, err = a.Triggers.Store.GetIntent(ctx, intent.ID)
		if err != nil || intent.Review == nil || intent.Review.ReviewerSubject != "fixture-reviewer" {
			t.Fatal("exact prospective PG review missing", err)
		}
	}
	var admissions, secretPreparations, registryPreparations atomic.Int32
	producer := NewJobQueueEnqueuer(jobs, st.Pipelines)
	producer.AdmissionGuard = storepg.NewTriggerStore(db)
	producer.RequireRegistryPins = true
	producer.PrepareRegistry = func(ctx context.Context, p *model.Pipeline) (*model.RegistryAuthorityPlan, error) {
		registryPreparations.Add(1)
		return PreparePipelineRegistryAuthority(ctx, authority, p)
	}
	producer.PrepareSecrets = func(ctx context.Context, p *model.Pipeline) (*PipelineSecretPlan, error) {
		secretPreparations.Add(1)
		return NewExecutor().PreparePipelineSecrets(ctx, p)
	}
	a.Enqueue = func(ctx context.Context, p *model.Pipeline, run *model.PipelineRun, source *model.PipelineSourcePlan, proof *model.PipelineTriggerProof) (*model.TriggerReceipt, error) {
		admissions.Add(1)
		return producer.EnqueueSourceRun(ctx, p, run, source, proof)
	}
	before := f.checkouts
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for n := 0; n < 12; n++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- a.Dispatch(ctx, intent) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal("concurrent native admission", err)
		}
	}
	if admissions.Load() != 1 || secretPreparations.Load() != 1 || registryPreparations.Load() != 1 || f.checkouts != before+1 {
		t.Fatalf("native initial duplicates repeated preparation: jobs=%d secret=%d registry=%d checkout=%d", admissions.Load(), secretPreparations.Load(), registryPreparations.Load(), f.checkouts-before)
	}
	accepted, err := storepg.NewTriggerStore(db).GetIntent(ctx, intent.ID)
	if err != nil || accepted.Receipt == nil || accepted.Status != model.TriggerQueued {
		t.Fatal(accepted, err)
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM jobs`).Scan(&count); err != nil || count != 1 {
		t.Fatal("native jobs duplicated", count, err)
	}
	acceptedJob, err := jobs.Get(ctx, accepted.Receipt.JobID)
	if err != nil {
		t.Fatal(err)
	}
	var safePlan runJobPayload
	if json.Unmarshal(acceptedJob.Payload, &safePlan) != nil || safePlan.Source == nil || safePlan.Trigger == nil || safePlan.Registry == nil || safePlan.Run == nil || safePlan.Run.Source == nil || safePlan.Run.Source.CheckedOutAt != nil || safePlan.Registry.Subject != (model.RegistrySubject{Kind: "pipeline", ID: p.ID}) || safePlan.Registry.Validate() != nil || safePlan.Registry.Connections[registry.ID].Credential.Mode != "none" || safePlan.Trigger.IntentID != intent.ID || safePlan.Source.CommitSHA != intent.Event.CommitSHA || strings.Contains(string(acceptedJob.Payload), registryCanary) {
		t.Fatal("actual native producer omitted safe source/registry/receipt evidence")
	}
	var persisted string
	if err = db.QueryRow(`SELECT payload::text FROM jobs WHERE id=$1`, accepted.Receipt.JobID).Scan(&persisted); err != nil || strings.Contains(persisted, registryCanary) {
		t.Fatal("unsafe job JSONB", err)
	}
	// Current grant revocation must not cause an accepted job to be repinned.
	if _, err = st.Registries.(interface {
		UpdateGrants(context.Context, string, int64, []string, []string) (*model.RegistryConfig, error)
	}).UpdateGrants(ctx, registry.ID, registry.Version, nil, nil); err != nil {
		t.Fatal(err)
	}
	// Fresh handles and unavailable provider model receiver restart after its
	// response was lost. The admitted receipt wins before all repin hooks.
	reopenedJobs := jobqueue.NewPostgres(db)
	reopenedJobs.OnEnqueue = jobs.OnEnqueue
	reopened := &PipelineAutomation{Jobs: reopenedJobs, Triggers: &TriggerService{Store: storepg.NewTriggerStore(db)}, RequireRegistryPreflight: true, Enqueue: func(context.Context, *model.Pipeline, *model.PipelineRun, *model.PipelineSourcePlan, *model.PipelineTriggerProof) (*model.TriggerReceipt, error) {
		admissions.Add(1)
		return nil, fmt.Errorf("must not repin accepted source")
	}}
	providerBefore := f.identities + f.revisions + f.checkouts
	for n := 0; n < 6; n++ {
		if err = reopened.Dispatch(ctx, intent); err != nil {
			t.Fatal("reopened native receipt recovery", err)
		}
	}
	if admissions.Load() != 1 || secretPreparations.Load() != 1 || registryPreparations.Load() != 1 || providerBefore != f.identities+f.revisions+f.checkouts {
		t.Fatal("restart native retry called a provider or repin hook")
	}
	got, err := storepg.NewTriggerStore(db).GetIntent(ctx, intent.ID)
	if err != nil || got.Receipt.PlanDigest != accepted.Receipt.PlanDigest || got.Receipt.JobID != accepted.Receipt.JobID {
		t.Fatal("restart changed native receipt", err)
	}
}

func nativeApprovalOwnedFixtureURL(dsn string) (*url.URL, error) {
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme != "postgres" || u.Hostname() != "cooker-native-approval-pg" || u.Port() != "5432" || u.Path != "/cooker_native_approval_test" || u.User == nil || u.User.Username() != "native_approval_fixture" || u.Query().Get("sslmode") != "disable" || len(u.Query()) != 1 || len(u.Query()["sslmode"]) != 1 || u.Fragment != "" {
		return nil, errors.New("refusing non-owned native-approval fixture")
	}
	if _, password := u.User.Password(); password {
		return nil, errors.New("fixture must not read a credential")
	}
	return u, nil
}
