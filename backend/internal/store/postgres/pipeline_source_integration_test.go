//go:build integration

package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/source/connection"
	"github.com/santapong/cooker/internal/store"
	"reflect"
	"strings"
	"testing"
	"time"
)

func nativeAdmissionFixture(t *testing.T, db *sql.DB) (*TriggerStore, *model.TriggerIntent, *model.Pipeline, *model.PipelineSourcePlan, json.RawMessage) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	cs := &connection.Postgres{DB: db}
	c := &connection.Connection{ID: "native-c", Provider: "github", Origin: "https://github.com", Enabled: true, Version: 1, Repositories: []string{"owned/native"}, Credential: []byte("synthetic-encrypted-canary"), CreatedAt: now, UpdatedAt: now}
	if err := cs.Create(ctx, c); err != nil {
		t.Fatal(err)
	}
	envs := NewEnvironmentStore(db)
	if err := envs.Create(ctx, &model.Environment{ID: "native-env", Name: "native-env"}); err != nil {
		t.Fatal(err)
	}
	env, err := envs.Get(ctx, "native-env")
	if err != nil {
		t.Fatal(err)
	}
	p := &model.Pipeline{ID: "native-p", Name: "native-p", CreatedAt: now, UpdatedAt: now, Source: &model.PipelineSourceConfig{ConnectionID: c.ID, Repository: "owned/native", FullRef: "refs/heads/main"}, Stages: []model.Stage{{ID: "build", Type: model.StageTypeBuild, EnvironmentID: env.ID, Config: model.StageConfig{Context: ".", Dockerfile: "Dockerfile"}}}}
	p.SetDefaults()
	ps := NewPipelineStore(db)
	if err = ps.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	p, err = ps.Get(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	source := model.GitSourceIdentity{Provider: c.Provider, ConnectionID: c.ID, Host: "github.com", RepositoryID: "native-provider-id", Repository: "owned/native"}
	plan := &model.PipelineSourcePlan{Version: 1, Source: source, ConnectionVersion: 1, FullRef: p.Source.FullRef, CommitSHA: strings.Repeat("a", 40), ObjectSHA: strings.Repeat("a", 40), PipelineID: p.ID, PipelineVersion: p.Version, EnvironmentVersions: map[string]int{env.ID: env.Version}, ReviewPolicy: "automatic", InputFiles: []model.SourceInputHash{{Path: "Dockerfile", Present: true, SHA256: strings.Repeat("d", 64)}}}
	plan.PlanHash = model.PipelineSourceConfigDigest(p, plan)
	recipeBytes, _ := json.Marshal(plan.InputFiles)
	recipeHash := sha256.Sum256(recipeBytes)
	plan.EnvelopeHash = hex.EncodeToString(recipeHash[:])
	r := &model.TriggerRule{ID: "native-r", Name: "native-r", Enabled: true, TargetKind: "pipeline", TargetID: p.ID, PipelineVersion: p.Version, Source: source, SourceVersion: 1, EnvironmentVersions: plan.EnvironmentVersions, RefKind: "branch", Match: "exact", Include: []string{"main"}, Approval: "automatic", ActorSubject: "cooker:trigger:native-r", PlanHash: plan.PlanHash, ApprovedEnvelopeHash: plan.EnvelopeHash, ReviewedBySubject: "owner", ReviewedAt: &now}
	inbox := NewTriggerStore(db)
	if err = inbox.PutRule(ctx, r); err != nil {
		t.Fatal(err)
	}
	event := model.VerifiedGitEvent{Source: source, DeliveryID: "native-provider-delivery", Event: "push", FullRef: plan.FullRef, CommitSHA: plan.CommitSHA, ObjectSHA: plan.ObjectSHA, PayloadHash: strings.Repeat("c", 64)}
	i := &model.TriggerIntent{ID: "native-i", Version: 1, RunID: "native-run", LogicalKey: "native-logical", DeliveryID: "native-d", Rule: *r, Event: event, Status: model.TriggerPending, Reason: "awaiting source, actor and deployment-envelope validation", CreatedAt: now, UpdatedAt: now}
	d := &model.TriggerDelivery{ID: i.DeliveryID, Event: event, CreatedAt: now, Decisions: []model.TriggerDecision{{RuleID: r.ID, RuleVersion: r.Version, IntentID: i.ID, TargetKind: "app", TargetID: "spoofed-app", AppID: "spoofed-app", Status: "pending"}}}
	if _, _, err = inbox.RecordDelivery(ctx, d, []*model.TriggerIntent{i}); err != nil {
		t.Fatal(err)
	}
	run := &model.PipelineRun{ID: i.RunID, PipelineID: p.ID, PipelineVersion: p.Version, Status: model.RunStatusPending, CreatedAt: now, StartedByUserSub: r.ActorSubject, StartedByEmail: "automation:" + r.ID}
	digest, _ := model.PipelineSourcePlanDigest(plan)
	proof := model.PipelineTriggerProof{IntentID: i.ID, RuleID: r.ID, RuleVersion: r.Version, TargetKind: "pipeline", TargetID: p.ID, EnvelopeHash: plan.EnvelopeHash, SourcePlanDigest: digest}
	payload, _ := json.Marshal(map[string]any{"pipelineId": p.ID, "runId": run.ID, "pipeline": p, "run": run, "source": plan, "trigger": proof})
	return inbox, i, p, plan, payload
}

func TestNativePipelineSourcePostgresRoundTripHistoryAndImmutableTargets(t *testing.T) {
	db := claimTestDB(t)
	ctx := context.Background()
	inbox, i, p, _, _ := nativeAdmissionFixture(t, db)
	got, err := NewPipelineStore(db).Get(ctx, p.ID)
	if err != nil || got.Source == nil || *got.Source != *p.Source {
		t.Fatal("source did not survive PG persistence", err)
	}
	got.Source.FullRef = "refs/tags/release"
	if err = NewPipelineStore(db).Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewPipelineStore(db).Get(ctx, p.ID)
	if err != nil || reopened.Source.FullRef != "refs/tags/release" || reopened.Version != got.Version {
		t.Fatal(reopened, err)
	}
	r, _ := inbox.GetRule(ctx, i.Rule.ID)
	for n := 0; n < 30; n++ {
		d := &model.TriggerDelivery{ID: fmt.Sprintf("native-h-%02d", n), Event: i.Event, CreatedAt: time.Now().UTC().Add(time.Duration(n) * time.Second), Decisions: []model.TriggerDecision{{RuleID: r.ID, RuleVersion: r.Version, Status: "ignored", Reason: "ref does not match", AppID: "secret-spoof", TargetKind: "app", TargetID: "secret-spoof"}}}
		d.Event.DeliveryID = d.ID
		if _, _, err = inbox.RecordDelivery(ctx, d, nil); err != nil {
			t.Fatal(err)
		}
	}
	r.TargetID = "renamed-native-target"
	if err = inbox.PutRule(ctx, r); err != nil {
		t.Fatal(err)
	}
	page, err := NewTriggerStore(db).ListTriggerHistory(ctx, store.TriggerHistoryQuery{TargetKind: "pipeline", TargetID: p.ID, Status: "ignored", Limit: 7, Offset: 21})
	if err != nil || page.Total != 30 || len(page.Items) != 7 || page.Items[0].DeliveryID != "native-h-08" {
		t.Fatal(page, err)
	}
	for _, row := range page.Items {
		if row.TargetKind != "pipeline" || row.TargetID != p.ID || row.AppID != "" {
			t.Fatal("target followed edit or caller claim", row)
		}
	}
	b, _ := json.Marshal(page)
	if strings.Contains(string(b), "synthetic-encrypted-canary") || strings.Contains(string(b), "secret-spoof") {
		t.Fatal("private data escaped native history")
	}
	if _, err = inbox.PruneTriggers(ctx, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	page, err = inbox.ListTriggerHistory(ctx, store.TriggerHistoryQuery{TargetKind: "pipeline", TargetID: p.ID, Status: "ignored", Limit: 1})
	if err != nil || page.Total != 30 {
		t.Fatal("native decision lost during compaction", page, err)
	}
}

func TestNativePipelineTriggerPostgresAtomicQueueAndReceipt(t *testing.T) {
	db := claimTestDB(t)
	ctx := context.Background()
	inbox, i, p, _, payload := nativeAdmissionFixture(t, db)
	jobs := jobqueue.NewPostgres(db)
	jobs.OnEnqueue = func(ctx context.Context, tx *sql.Tx, j *jobqueue.Job) error {
		if err := SyncTriggerAdmission(ctx, tx, j); err != nil {
			return err
		}
		return fmt.Errorf("injected precommit interruption")
	}
	opts := jobqueue.EnqueueOptions{ID: "run:" + i.RunID, ConcurrencyKey: "pipeline:" + p.ID, MaxAttempts: 1}
	if _, err := jobs.Enqueue(ctx, "pipeline-run", payload, opts); err == nil {
		t.Fatal("interrupted native admission succeeded")
	}
	if _, err := jobs.Get(ctx, opts.ID); !errors.Is(err, jobqueue.ErrNotFound) {
		t.Fatal("rollback retained job", err)
	}
	current, _ := inbox.GetIntent(ctx, i.ID)
	if current.Status != model.TriggerPending || current.Receipt != nil {
		t.Fatal("rollback advanced intent", current)
	}
	jobs.OnEnqueue = SyncTriggerAdmission
	job, err := jobs.Enqueue(ctx, "pipeline-run", payload, opts)
	if err != nil {
		t.Fatal(err)
	}
	current, err = NewTriggerStore(db).GetIntent(ctx, i.ID)
	if err != nil || current.Status != model.TriggerQueued || current.Receipt == nil || current.Receipt.PipelineID != p.ID || current.Receipt.JobID != job.ID {
		t.Fatal(current, err)
	}
	persisted, err := jobqueue.NewPostgres(db).Get(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := jobqueue.PayloadDigest(persisted.Payload)
	if err != nil || digest != current.Receipt.PlanDigest {
		t.Fatal("native receipt changed under JSONB/reopened handles", err)
	}
	if _, err = jobs.Enqueue(ctx, "pipeline-run", payload, opts); err == nil {
		t.Fatal("raw queue insertion silently duplicated an accepted job")
	}
	recovered, err := jobs.Get(ctx, opts.ID)
	if err != nil || recovered.ID != job.ID {
		t.Fatal("existing accepted job could not be recovered", err)
	}
	recoveredIntent, err := inbox.GetIntent(ctx, i.ID)
	if err != nil || !reflect.DeepEqual(recoveredIntent.Receipt, current.Receipt) {
		t.Fatal("duplicate insertion changed atomic receipt", err)
	}
}

func TestNativePipelineTriggerPostgresRejectsChangedPinnedInputs(t *testing.T) {
	for _, kind := range []string{"pipeline", "connection", "disabled-connection", "environment", "registry-environment-alias", "rule", "ref-policy", "actor", "target", "digest", "extra-environment", "review"} {
		t.Run(kind, func(t *testing.T) {
			db := claimTestDB(t)
			ctx := context.Background()
			inbox, i, p, _, payload := nativeAdmissionFixture(t, db)
			var data map[string]any
			json.Unmarshal(payload, &data)
			switch kind {
			case "pipeline":
				db.Exec(`UPDATE pipelines SET version=version+1 WHERE id=$1`, p.ID)
			case "connection":
				db.Exec(`UPDATE source_connections SET version=version+1 WHERE id='native-c'`)
			case "disabled-connection":
				db.Exec(`UPDATE source_connections SET configuration=jsonb_set(configuration,'{enabled}','false'::jsonb) WHERE id='native-c'`)
			case "environment":
				db.Exec(`UPDATE environments SET version=version+1 WHERE id='native-env'`)
			case "registry-environment-alias":
				// Friendly-name poisoning cannot use a normal ID/version as authority.
				db.Exec(`UPDATE environments SET name='_registries (internal)' WHERE id='native-env'`)
			case "rule":
				r, _ := inbox.GetRule(ctx, i.Rule.ID)
				r.Enabled = false
				inbox.PutRule(ctx, r)
			case "ref-policy":
				// Recheck the locked policy, independently of the rule version.
				db.Exec(`UPDATE trigger_rules SET body=jsonb_set(body,'{include}','["release"]'::jsonb) WHERE id=$1`, i.Rule.ID)
			case "actor":
				data["run"].(map[string]any)["startedByUserSub"] = "human-forgery"
			case "target":
				data["trigger"].(map[string]any)["targetId"] = "vflow_imported"
			case "digest":
				data["trigger"].(map[string]any)["sourcePlanDigest"] = strings.Repeat("f", 64)
			case "extra-environment":
				data["source"].(map[string]any)["environmentVersions"].(map[string]any)["OTHER"] = 1
			case "review":
				db.Exec(`UPDATE trigger_rules SET body=body-'reviewedBySubject' WHERE id=$1`, i.Rule.ID)
			}
			payload, _ = json.Marshal(data)
			jobs := jobqueue.NewPostgres(db)
			jobs.OnEnqueue = SyncTriggerAdmission
			if _, err := jobs.Enqueue(ctx, "pipeline-run", payload, jobqueue.EnqueueOptions{ID: "run:" + i.RunID, ConcurrencyKey: "pipeline:" + p.ID}); err == nil {
				t.Fatal("changed native input admitted")
			}
			if _, err := jobs.Get(ctx, "run:"+i.RunID); !errors.Is(err, jobqueue.ErrNotFound) {
				t.Fatal("rejected native job visible", err)
			}
			got, _ := inbox.GetIntent(ctx, i.ID)
			if got.Status != model.TriggerPending || got.Receipt != nil {
				t.Fatal("rejected native input advanced receipt")
			}
		})
	}
}

func TestNativeTriggerAdmissionPostgresCanceledWaitAndReleasedLock(t *testing.T) {
	db := claimTestDB(t)
	db.SetMaxOpenConns(2)
	inbox := NewTriggerStore(db)
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- inbox.WithNativeTriggerAdmission(context.Background(), "bounded-native", func(context.Context) error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Millisecond)
	defer cancel()
	err := inbox.WithNativeTriggerAdmission(ctx, "bounded-native", func(context.Context) error {
		t.Error("canceled waiter obtained work")
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Error("wait did not honor deadline", err)
	}
	// A waiting caller must not keep the second pool connection occupied.
	if err := db.PingContext(context.Background()); err != nil {
		t.Error("wait starved connection pool", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal("owner failed", err)
	}
	called := false
	if err := NewTriggerStore(db).WithNativeTriggerAdmission(context.Background(), "bounded-native", func(context.Context) error {
		called = true
		return nil
	}); err != nil || !called {
		t.Fatal("reopened handle could not acquire released lock", err)
	}
}
