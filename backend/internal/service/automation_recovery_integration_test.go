//go:build integration

package service_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	cookercrypto "github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/provider/vercel"
	"github.com/santapong/cooker/internal/scheduler"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/source/connection"
	storepg "github.com/santapong/cooker/internal/store/postgres"
)

type missedRefSource struct {
	revision service.SourceRevision
	reads    atomic.Int32
}

func (s *missedRefSource) Identity(_ context.Context, c *connection.Connection, repo string) (model.GitSourceIdentity, error) {
	s.reads.Add(1)
	return model.GitSourceIdentity{Provider: "github", ConnectionID: c.ID, Host: "github.com", RepositoryID: "123", Repository: repo}, nil
}
func (s *missedRefSource) Revision(context.Context, *connection.Connection, string, string) (service.SourceRevision, error) {
	s.reads.Add(1)
	return s.revision, nil
}

// This is real PostgreSQL inbox/producer persistence with a source contract
// fixture. No worker, hosted source, checkout, build or deployment is executed.
func TestAppMissedRefPostgresRecoveryAndSharedInbox(t *testing.T) {
	dsn, nonce := os.Getenv("COOKER_MISSED_REF_DATABASE_URL"), os.Getenv("COOKER_MISSED_REF_FIXTURE_ID")
	if dsn == "" && nonce == "" {
		t.Skip("explicit owned missed-ref PostgreSQL required")
	}
	parsed, err := uuid.Parse(nonce)
	if err != nil || parsed == uuid.Nil || parsed.String() != nonce {
		t.Fatal("owned fixture identity required")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme != "postgres" || u.User == nil || u.User.Username() != "postgres" || u.Path != "/cmiss_"+strings.ReplaceAll(nonce, "-", "") || u.Query().Get("sslmode") != "disable" || len(u.Query()) != 1 || u.Fragment != "" {
		t.Fatal("owned database scope required")
	}
	if _, present := u.User.Password(); present {
		t.Fatal("owner credentials cannot be inherited")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal("owned database unavailable")
	}
	defer admin.Close()
	var marker string
	if err = admin.QueryRowContext(ctx, `SELECT shobj_description(oid,'pg_database') FROM pg_database WHERE datname=current_database()`).Scan(&marker); err != nil || marker != "missed-ref-owned:"+nonce {
		t.Fatal("owned database marker required before writes")
	}

	for _, mode := range []string{"branch", "tag", "shared matching rules"} {
		t.Run(mode, func(t *testing.T) {
			schema := "cmiss_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
				t.Fatal("owned schema unavailable")
			}
			defer admin.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
			local := *u
			query := local.Query()
			query.Set("search_path", schema)
			local.RawQuery = query.Encode()
			st, err := storepg.NewStore(ctx, local.String())
			if err != nil {
				t.Fatal("owned store unavailable")
			}
			defer st.Close()
			db, err := sql.Open("postgres", local.String())
			if err != nil {
				t.Fatal("owned inbox unavailable")
			}
			defer db.Close()
			codec, err := cookercrypto.NewCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)))
			if err != nil {
				t.Fatal("fixture codec unavailable")
			}
			canary := "fixture-only-" + uuid.NewString()
			connections := &connection.Service{Store: &connection.Postgres{DB: db}, Codec: codec}
			conn := &connection.Connection{Name: "owned recovery", Provider: "github", Enabled: true, Repositories: []string{"owned/recovery"}}
			if err := connections.Create(ctx, conn, canary); err != nil {
				t.Fatal("fixture connection unavailable")
			}
			kind, ref := "branch", "refs/heads/main"
			if mode == "tag" {
				kind, ref = "tag", "refs/tags/main"
			}
			source := &missedRefSource{revision: service.SourceRevision{FullRef: ref, Commit: strings.Repeat("a", 40), Object: strings.Repeat("b", 40)}}
			triggers := &service.TriggerService{Store: storepg.NewTriggerStore(db)}
			jobs := jobqueue.NewPostgres(db)
			hookCalls := map[string]int{}
			jobs.OnEnqueue = func(c context.Context, tx *sql.Tx, j *jobqueue.Job) error {
				for _, hook := range []struct {
					name string
					run  func(context.Context, *sql.Tx, *jobqueue.Job) error
				}{
					{"registry", storepg.SyncRegistryAuthorityAdmission}, {"schedule", scheduler.SyncOccurrenceAdmission},
					{"trigger", storepg.SyncTriggerAdmission}, {"publication", vercel.PublicationAdmission},
				} {
					hookCalls[hook.name]++
					if err := hook.run(c, tx, j); err != nil {
						return err
					}
				}
				return nil
			}
			jobs.OnTransition = func(c context.Context, tx *sql.Tx, j *jobqueue.Job) error {
				if err := storepg.SyncJobRun(c, tx, j); err != nil {
					return err
				}
				return vercel.PublicationTransition(c, tx, j)
			}
			authority := service.NewRegistryAuthorityService(st, nil)
			authority.Registries, authority.Environments = storepg.NewRegistryConfigStore(db), storepg.NewEnvironmentStore(db)
			q := &service.AppDeployQueue{Jobs: jobs, Store: st, Deployer: &service.AppDeployer{}, AdmissionGuard: storepg.NewTriggerStore(db), RegistryAuthority: authority, RequireRegistryPins: true}
			a := &service.Automation{Connections: connections, Store: st, Queue: q, Triggers: triggers, Source: source,
				Authorize: func(context.Context, *model.TriggerRule, *model.App) error { return nil },
				Inspect: func(_ context.Context, app *model.App, _ map[string]string) (*service.AutomationEnvelope, error) {
					return &service.AutomationEnvelope{Hash: strings.Repeat("c", 64), Commit: app.BuildPlan.Commit}, nil
				},
			}
			q.ValidateTrigger = a.ValidateQueued
			count := 1
			if mode == "shared matching rules" {
				count = 2
			}
			var rules []*model.TriggerRule
			for index := 0; index < count; index++ {
				now := time.Now().UTC()
				app := &model.App{ID: uuid.NewString(), Name: fmt.Sprint("owned-", index), GitHubRepo: "owned/recovery", Branch: "main", BuildPlan: &model.BuildPlan{Kind: model.BuildPlanCompose, Files: []string{"compose.yaml"}, EnvironmentScope: "service"}, DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost}, CreatedAt: now, UpdatedAt: now}
				if err := st.Apps.Create(ctx, app); err != nil {
					t.Fatal("fixture App unavailable")
				}
				r := &model.TriggerRule{ID: uuid.NewString(), Name: app.Name, AppID: app.ID, Enabled: true, Source: model.GitSourceIdentity{ConnectionID: conn.ID}, RefKind: kind, Match: "exact", Include: []string{"main"}, Approval: "automatic"}
				if _, err := a.Preview(ctx, r, ref); err != nil {
					t.Fatal("fixture review unavailable")
				}
				if err := triggers.SaveRule(ctx, r); err != nil {
					t.Fatal("reviewed rule unavailable")
				}
				r, err = triggers.Store.GetRule(ctx, r.ID)
				if err != nil {
					t.Fatal("persisted review unavailable")
				}
				rules = append(rules, r)
			}
			delivery, duplicate, err := a.CheckRef(ctx, rules[0], ref)
			if err != nil || duplicate || delivery == nil || len(delivery.Decisions) != count {
				t.Fatal("recovery did not evaluate the shared matching rules")
			}
			repeated, duplicate, err := a.CheckRef(ctx, rules[0], ref)
			if err != nil || !duplicate || repeated.ID != delivery.ID {
				t.Fatal("recovery redelivery was not deduplicated")
			}
			webhook := delivery.Event
			webhook.DeliveryID = "hosted-contract-fixture"
			webhook.PayloadHash = strings.Repeat("d", 64)
			fromWebhook, duplicate, err := triggers.AcceptVerified(ctx, webhook)
			if err != nil || duplicate || len(fromWebhook.Decisions) != count {
				t.Fatal("webhook logical dedupe failed")
			}
			for index, decision := range delivery.Decisions {
				if decision.Status != "pending" && decision.Status != "accepted" {
					t.Fatal("recovery decision was not accepted")
				}
				var linked bool
				for _, other := range fromWebhook.Decisions {
					if other.RuleID == decision.RuleID && other.IntentID == decision.IntentID && other.Status == "duplicate" {
						linked = true
					}
				}
				if !linked {
					t.Fatal("webhook created another logical intent")
				}
				intent, err := triggers.Store.GetIntent(ctx, decision.IntentID)
				if err != nil || intent.Status != model.TriggerPending {
					t.Fatal("persisted recovered intent unavailable")
				}
				if err := a.Dispatch(ctx, intent); err != nil {
					t.Fatal("real producer admission failed")
				}
				if err := a.Dispatch(ctx, intent); err != nil {
					t.Fatal("producer receipt recovery failed")
				}
				accepted, err := triggers.Store.GetIntent(ctx, intent.ID)
				if err != nil || accepted.Status != model.TriggerQueued || accepted.Receipt == nil {
					t.Fatal("durable queue receipt unavailable")
				}
				job, err := jobs.Get(ctx, accepted.Receipt.JobID)
				if err != nil || job.Status != jobqueue.StatusPending || job.Attempts != 0 {
					t.Fatal("unexpected worker execution")
				}
				var plan service.AppDeployPlan
				if json.Unmarshal(job.Payload, &plan) != nil || plan.Commit != source.revision.Commit || plan.Trigger == nil || plan.Trigger.IntentID != intent.ID || plan.Actor == nil || plan.Actor.StartedByUserSub != intent.Rule.ActorSubject || bytes.Contains(job.Payload, []byte(canary)) {
					t.Fatal("accepted source authority differs")
				}
				if err := a.ValidateQueued(ctx, &plan); err != nil {
					t.Fatal("current queued authority refused")
				}
				if index == 0 {
					fresh, err := sql.Open("postgres", local.String())
					if err != nil {
						t.Fatal("fresh pool unavailable")
					}
					stored, err := storepg.NewTriggerStore(fresh).GetIntent(ctx, intent.ID)
					fresh.Close()
					if err != nil || stored.Receipt == nil || stored.Receipt.JobID != accepted.Receipt.JobID {
						t.Fatal("fresh pool lost the accepted receipt")
					}
				}
			}
			for _, name := range []string{"registry", "schedule", "trigger", "publication"} {
				if hookCalls[name] != count {
					t.Fatal("full producer hook was omitted or repeated")
				}
			}
			var jobCount int
			if db.QueryRowContext(ctx, `SELECT count(*) FROM jobs`).Scan(&jobCount) != nil || jobCount != count {
				t.Fatal("duplicate jobs persisted")
			}
			// A new observed ref can be recovered, but becomes obsolete before
			// dispatch if the branch or annotated tag changes again.
			source.revision.Commit = strings.Repeat("e", 40)
			source.revision.Object = strings.Repeat("e", 40)
			newDelivery, _, err := a.CheckRef(ctx, rules[0], ref)
			if err != nil || newDelivery == nil {
				t.Fatal("new ref recovery failed")
			}
			source.revision.Commit = strings.Repeat("f", 40)
			source.revision.Object = strings.Repeat("f", 40)
			for _, decision := range newDelivery.Decisions {
				intent, err := triggers.Store.GetIntent(ctx, decision.IntentID)
				if err != nil || a.Dispatch(ctx, intent) != nil {
					t.Fatal("obsolete ref classification failed")
				}
				intent, err = triggers.Store.GetIntent(ctx, intent.ID)
				wantStatus := model.TriggerSuperseded
				if kind == "tag" {
					wantStatus = model.TriggerAwaitingReview
				}
				if err != nil || intent.Status != wantStatus {
					t.Fatal("obsolete recovery could deploy")
				}
			}
			if db.QueryRowContext(ctx, `SELECT count(*) FROM jobs`).Scan(&jobCount) != nil || jobCount != count {
				t.Fatal("obsolete ref queued another job")
			}
			// A disabled saved rule fails before any source call or inbox write.
			rules[0].Enabled = false
			if triggers.SaveRule(ctx, rules[0]) != nil {
				t.Fatal("fixture revocation unavailable")
			}
			beforeReads := source.reads.Load()
			var beforeDeliveries, afterDeliveries int
			if db.QueryRowContext(ctx, `SELECT count(*) FROM webhook_deliveries`).Scan(&beforeDeliveries) != nil {
				t.Fatal("inbox baseline unavailable")
			}
			if d, _, err := a.CheckRef(ctx, rules[0], ref); err == nil || d != nil || source.reads.Load() != beforeReads {
				t.Fatal("disabled recovery touched source")
			}
			if db.QueryRowContext(ctx, `SELECT count(*) FROM webhook_deliveries`).Scan(&afterDeliveries) != nil || beforeDeliveries != afterDeliveries {
				t.Fatal("disabled recovery wrote the inbox")
			}
		})
	}
}
