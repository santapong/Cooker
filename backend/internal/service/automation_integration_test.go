//go:build integration

package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
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
	storepg "github.com/santapong/cooker/internal/store/postgres"
)

type durableAutomationSourceFixture struct{ sha string }

func (f *durableAutomationSourceFixture) Identity(_ context.Context, c *connection.Connection, repo string) (model.GitSourceIdentity, error) {
	return model.GitSourceIdentity{Provider: "github", ConnectionID: c.ID, Host: "github.com", RepositoryID: "123", Repository: repo}, nil
}
func (f *durableAutomationSourceFixture) Revision(_ context.Context, _ *connection.Connection, _, ref string) (SourceRevision, error) {
	return SourceRevision{FullRef: ref, Commit: f.sha, Object: f.sha}, nil
}

// Real PostgreSQL, but a provider contract fixture: this proves the durable
// boundary and revocation rules, not a hosted GitHub webhook delivery.
func TestAutomationDurableAdmissionAndRevocation(t *testing.T) {
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
	schema := "cooker_automation_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec("DROP SCHEMA " + schema + " CASCADE")
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
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
	codec, _ := cookercrypto.NewCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	connections := &connection.Service{Store: &connection.Postgres{DB: db}, Codec: codec}
	conn := &connection.Connection{Name: "fixture", Provider: "github", Enabled: true, Repositories: []string{"acme/shop"}}
	if err := connections.Create(ctx, conn, strings.Repeat("fixture-", 8)); err != nil {
		t.Fatal(err)
	}
	app := &model.App{ID: uuid.NewString(), Name: "fixture", GitHubRepo: "acme/shop", Branch: "main", BuildPlan: &model.BuildPlan{Kind: model.BuildPlanCompose, Files: []string{"compose.yaml"}, EnvironmentScope: "service"}, DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost}, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := st.Apps.Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	jobs := jobqueue.NewPostgres(db)
	jobs.OnEnqueue = storepg.SyncTriggerAdmission
	q := &AppDeployQueue{Jobs: jobs, Store: st, Deployer: &AppDeployer{}}
	source := &durableAutomationSourceFixture{sha: strings.Repeat("a", 40)}
	envelope := strings.Repeat("c", 64)
	a := &Automation{Connections: connections, Store: st, Queue: q, Source: source, Triggers: &TriggerService{Store: storepg.NewTriggerStore(db)}, Authorize: func(context.Context, *model.TriggerRule, *model.App) error { return nil }, Inspect: func(_ context.Context, a *model.App, _ map[string]string) (*AutomationEnvelope, error) {
		return &AutomationEnvelope{Hash: envelope, Commit: a.BuildPlan.Commit}, nil
	}}
	q.ValidateTrigger = a.ValidateQueued
	r := &model.TriggerRule{ID: uuid.NewString(), Name: "main", AppID: app.ID, Enabled: true, Source: model.GitSourceIdentity{ConnectionID: conn.ID}, RefKind: "branch", Match: "exact", Include: []string{"main"}, Approval: "automatic", ActorSubject: "client-supplied-admin"}
	preview, err := a.Preview(ctx, r, "refs/heads/main")
	if err != nil {
		t.Fatal(err)
	}
	if preview.Rule.ActorSubject != "cooker:trigger:"+r.ID {
		t.Fatal("client actor trusted")
	}
	if err := a.Triggers.SaveRule(ctx, r); err != nil {
		t.Fatal(err)
	}
	event := model.VerifiedGitEvent{Source: r.Source, DeliveryID: "first", Event: "push", FullRef: "refs/heads/main", CommitSHA: source.sha, PayloadHash: strings.Repeat("d", 64)}
	d, duplicate, err := a.Triggers.AcceptVerified(ctx, event)
	if err != nil || duplicate {
		t.Fatal(d, duplicate, err)
	}
	i, err := a.Triggers.Store.GetIntent(ctx, d.Decisions[0].IntentID)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Dispatch(ctx, i); err != nil {
		t.Fatal(err)
	}
	accepted, err := storepg.NewTriggerStore(db).GetIntent(ctx, i.ID)
	if err != nil || accepted.Status != model.TriggerQueued || accepted.Receipt == nil {
		t.Fatal("no durable receipt", accepted, err)
	}
	job, err := jobqueue.NewPostgres(db).Get(ctx, accepted.Receipt.JobID)
	if err != nil {
		t.Fatal(err)
	}
	var plan AppDeployPlan
	if err := json.Unmarshal(job.Payload, &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Commit != event.CommitSHA || plan.Actor.StartedByUserSub != r.ActorSubject || plan.Trigger == nil {
		t.Fatal("queued identity lost")
	}
	if err := a.ValidateQueued(ctx, &plan); err != nil {
		t.Fatal(err)
	}
	// A receiver crash after enqueue can retry from its old in-memory intent.
	if err := a.Dispatch(ctx, i); err != nil {
		t.Fatal("receipt not recovered", err)
	}
	var count int
	db.QueryRow(`SELECT count(*) FROM jobs`).Scan(&count)
	if count != 1 {
		t.Fatalf("duplicate jobs: %d", count)
	}
	// Queue-time mutable source changes cannot silently roll the App backward.
	source.sha = strings.Repeat("b", 40)
	if err := a.ValidateQueued(ctx, &plan); err == nil {
		t.Fatal("stale branch passed execution validation")
	}
	old := event
	old.DeliveryID = "late-old"
	old.PayloadHash = strings.Repeat("e", 64)
	// Different logical config version permits a fresh test intent, but an
	// out-of-order SHA must still be marked superseded before queueing.
	r.Name = "reviewed-v2"
	if err := a.Triggers.SaveRule(ctx, r); err != nil {
		t.Fatal(err)
	}
	d, _, err = a.Triggers.AcceptVerified(ctx, old)
	if err != nil {
		t.Fatal(err)
	}
	late, _ := a.Triggers.Store.GetIntent(ctx, d.Decisions[0].IntentID)
	if err := a.Dispatch(ctx, late); err != nil {
		t.Fatal(err)
	}
	late, _ = a.Triggers.Store.GetIntent(ctx, late.ID)
	if late.Status != model.TriggerSuperseded {
		t.Fatalf("late branch status %s (%s)", late.Status, late.Reason)
	}
	// The same deployment configuration is required for the next source commit.
	event.DeliveryID = "changed-config"
	event.CommitSHA = source.sha
	event.PayloadHash = strings.Repeat("f", 64)
	envelope = strings.Repeat("9", 64)
	d, _, err = a.Triggers.AcceptVerified(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	changed, _ := a.Triggers.Store.GetIntent(ctx, d.Decisions[0].IntentID)
	if err := a.Dispatch(ctx, changed); err != nil {
		t.Fatal(err)
	}
	changed, _ = a.Triggers.Store.GetIntent(ctx, changed.ID)
	if changed.Status != model.TriggerAwaitingReview {
		t.Fatal("changed envelope did not pause", changed.Status)
	}
	db.QueryRow(`SELECT count(*) FROM jobs`).Scan(&count)
	if count != 1 {
		t.Fatal("unapproved work queued")
	}
	// Current administrative revocation is checked again before execution.
	r.Enabled = false
	if err := a.Triggers.SaveRule(ctx, r); err != nil {
		t.Fatal(err)
	}
	if err := a.ValidateQueued(ctx, &plan); err == nil {
		t.Fatal("revoked rule allowed execution")
	}
}
