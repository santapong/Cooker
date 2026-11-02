//go:build integration

package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
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

// Real owned PG and the product PipelineAutomation/PG hook, provider SDK/checkout
// fixture only. No native executor, secret/registry SDK or hosted provider claim.
func TestNativeAutomationPostgresConcurrentAdmissionAndRestartRecovery(t *testing.T) {
	for _, policy := range []string{"automatic", "review"} {
		t.Run(policy, func(t *testing.T) { testNativeAutomationPostgresAdmission(t, policy) })
	}
}

func testNativeAutomationPostgresAdmission(t *testing.T, policy string) {
	dsn := os.Getenv("COOKER_INTEGRATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable PostgreSQL required")
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
	u, err := url.Parse(dsn)
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
	jobs.OnEnqueue = storepg.SyncTriggerAdmission
	a := &PipelineAutomation{Preparer: preparer, Triggers: &TriggerService{Store: storepg.NewTriggerStore(db)}, Jobs: jobs, Authorize: func(context.Context, *model.TriggerRule, *model.Pipeline) error { return nil }}
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
	event := model.VerifiedGitEvent{Source: r.Source, DeliveryID: "native-delivery", Event: "push", FullRef: p.Source.FullRef, CommitSHA: f.commit, ObjectSHA: f.object, PayloadHash: strings.Repeat("d", 64)}
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
	var admissions, secretPins, registryPins atomic.Int32
	a.Enqueue = func(ctx context.Context, p *model.Pipeline, run *model.PipelineRun, source *model.PipelineSourcePlan, proof *model.PipelineTriggerProof) (*model.TriggerReceipt, error) {
		admissions.Add(1)
		secretPins.Add(1)
		registryPins.Add(1)
		payload, err := json.Marshal(map[string]any{"pipelineId": p.ID, "runId": run.ID, "pipeline": p, "run": run, "source": source, "trigger": proof})
		if err != nil {
			return nil, err
		}
		job, err := jobs.Enqueue(ctx, "pipeline-run", payload, jobqueue.EnqueueOptions{ID: "run:" + run.ID, ConcurrencyKey: "pipeline:" + p.ID, MaxAttempts: 1})
		if err != nil {
			return nil, err
		}
		digest, err := jobqueue.PayloadDigest(job.Payload)
		return &model.TriggerReceipt{RunID: run.ID, JobID: job.ID, PipelineID: p.ID, PlanDigest: digest}, err
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
	if admissions.Load() != 1 || secretPins.Load() != 1 || registryPins.Load() != 1 || f.checkouts != before+1 {
		t.Fatalf("native initial duplicates repinned: jobs=%d secret=%d registry=%d checkout=%d", admissions.Load(), secretPins.Load(), registryPins.Load(), f.checkouts-before)
	}
	accepted, err := storepg.NewTriggerStore(db).GetIntent(ctx, intent.ID)
	if err != nil || accepted.Receipt == nil || accepted.Status != model.TriggerQueued {
		t.Fatal(accepted, err)
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM jobs`).Scan(&count); err != nil || count != 1 {
		t.Fatal("native jobs duplicated", count, err)
	}
	// Fresh handles and unavailable provider model receiver restart after its
	// response was lost. The admitted receipt wins before all repin hooks.
	reopenedJobs := jobqueue.NewPostgres(db)
	reopenedJobs.OnEnqueue = storepg.SyncTriggerAdmission
	reopened := &PipelineAutomation{Jobs: reopenedJobs, Triggers: &TriggerService{Store: storepg.NewTriggerStore(db)}, Enqueue: func(context.Context, *model.Pipeline, *model.PipelineRun, *model.PipelineSourcePlan, *model.PipelineTriggerProof) (*model.TriggerReceipt, error) {
		admissions.Add(1)
		return nil, fmt.Errorf("must not repin accepted source")
	}}
	providerBefore := f.identities + f.revisions + f.checkouts
	for n := 0; n < 6; n++ {
		if err = reopened.Dispatch(ctx, intent); err != nil {
			t.Fatal("reopened native receipt recovery", err)
		}
	}
	if admissions.Load() != 1 || secretPins.Load() != 1 || registryPins.Load() != 1 || providerBefore != f.identities+f.revisions+f.checkouts {
		t.Fatal("restart native retry called a provider or repin hook")
	}
	got, err := storepg.NewTriggerStore(db).GetIntent(ctx, intent.ID)
	if err != nil || got.Receipt.PlanDigest != accepted.Receipt.PlanDigest || got.Receipt.JobID != accepted.Receipt.JobID {
		t.Fatal("restart changed native receipt", err)
	}
}
