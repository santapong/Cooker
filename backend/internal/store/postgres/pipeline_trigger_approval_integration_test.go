//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
)

func nativeApprovalFixtureURL(dsn string) (*url.URL, error) {
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme != "postgres" || u.Hostname() != "cooker-native-approval-pg" || u.Port() != "5432" || u.Path != "/cooker_native_approval_test" || u.User == nil || u.User.Username() != "native_approval_fixture" || u.Query().Get("sslmode") != "disable" || len(u.Query()) != 1 || len(u.Query()["sslmode"]) != 1 || u.Fragment != "" {
		return nil, errors.New("refusing non-owned native-approval fixture")
	}
	if _, password := u.User.Password(); password {
		return nil, errors.New("fixture must not read a credential")
	}
	return u, nil
}

func nativeApprovalTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("COOKER_NATIVE_APPROVAL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("owned disposable native-approval PostgreSQL required")
	}
	u, err := nativeApprovalFixtureURL(dsn)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	schema := "native_approval_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		_, _ = admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(); _, _ = admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); admin.Close() })
	if err := applyMigrations(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestNativeApprovalPostgresFixtureRefusesOwnerConnections(t *testing.T) {
	for _, dsn := range []string{"postgres://owner:synthetic-secret@localhost:5432/owner?sslmode=disable", "postgres://native_approval_fixture:synthetic-secret@cooker-native-approval-pg:5432/cooker_native_approval_test?sslmode=disable", "postgres://native_approval_fixture@cooker-native-approval-pg:5432/cooker_native_approval_test?sslmode=disable&search_path=public"} {
		if _, err := nativeApprovalFixtureURL(dsn); err == nil || strings.Contains(err.Error(), "synthetic-secret") {
			t.Fatal("unsafe fixture DSN accepted or echoed")
		}
	}
}

func TestNativeApprovalPostgresTypedRefAdmission(t *testing.T) {
	commit, object := strings.Repeat("a", 40), strings.Repeat("b", 40)
	for _, tc := range []struct {
		name, eventRef, eventObject, planRef, planCommit, planObject string
		accept                                                       bool
	}{
		{"branch-without-object", "refs/heads/main", "", "refs/heads/main", commit, commit, true},
		{"annotated-tag", "refs/tags/v1", object, "refs/tags/v1", commit, object, true},
		{"lightweight-tag", "refs/tags/v1", commit, "refs/tags/v1", commit, commit, true},
		{"branch-commit-changed", "refs/heads/main", "", "refs/heads/main", strings.Repeat("c", 40), strings.Repeat("c", 40), false},
		{"tag-object-missing", "refs/tags/v1", "", "refs/tags/v1", commit, object, false},
		{"tag-object-changed-same-commit", "refs/tags/v1", object, "refs/tags/v1", commit, strings.Repeat("c", 40), false},
		{"same-name-different-ref-kind", "refs/heads/main", "", "refs/tags/main", commit, object, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := nativeApprovalTestDB(t)
			ctx := context.Background()
			inbox, original, p, plan, _ := nativeAdmissionFixture(t, db)
			r, err := inbox.GetRule(ctx, original.Rule.ID)
			if err != nil {
				t.Fatal(err)
			}
			r.RefKind, r.Include = "branch", []string{strings.TrimPrefix(tc.eventRef, "refs/heads/")}
			if strings.HasPrefix(tc.eventRef, "refs/tags/") {
				r.RefKind, r.Include = "tag", []string{strings.TrimPrefix(tc.eventRef, "refs/tags/")}
			}
			if err := inbox.PutRule(ctx, r); err != nil {
				t.Fatal(err)
			}
			r, err = inbox.GetRule(ctx, r.ID)
			if err != nil {
				t.Fatal(err)
			}
			event := original.Event
			event.DeliveryID, event.FullRef, event.ObjectSHA = "typed-native-provider-delivery", tc.eventRef, tc.eventObject
			now := time.Now().UTC()
			i := &model.TriggerIntent{ID: "typed-native-i", Version: 1, RunID: "typed-native-run", LogicalKey: "typed-native-logical", DeliveryID: "typed-native-d", Rule: *r, Event: event, Status: model.TriggerPending, CreatedAt: now, UpdatedAt: now}
			d := &model.TriggerDelivery{ID: i.DeliveryID, Event: event, CreatedAt: now, Decisions: []model.TriggerDecision{{RuleID: r.ID, RuleVersion: r.Version, IntentID: i.ID, Status: "pending"}}}
			if _, _, err := inbox.RecordDelivery(ctx, d, []*model.TriggerIntent{i}); err != nil {
				t.Fatal(err)
			}
			plan.FullRef, plan.CommitSHA, plan.ObjectSHA = tc.planRef, tc.planCommit, tc.planObject
			digest, err := model.PipelineSourcePlanDigest(plan)
			if err != nil {
				t.Fatal(err)
			}
			run := &model.PipelineRun{ID: i.RunID, PipelineID: p.ID, PipelineVersion: p.Version, Status: model.RunStatusPending, CreatedAt: now, StartedByUserSub: r.ActorSubject, StartedByEmail: "automation:" + r.ID}
			proof := &model.PipelineTriggerProof{IntentID: i.ID, RuleID: r.ID, RuleVersion: r.Version, TargetKind: "pipeline", TargetID: p.ID, EnvelopeHash: plan.EnvelopeHash, SourcePlanDigest: digest}
			payload, err := json.Marshal(map[string]any{"pipelineId": p.ID, "runId": run.ID, "pipeline": p, "run": run, "source": plan, "trigger": proof})
			if err != nil {
				t.Fatal(err)
			}
			jobs := jobqueue.NewPostgres(db)
			jobs.OnEnqueue = SyncTriggerAdmission
			job, err := jobs.Enqueue(ctx, "pipeline-run", payload, jobqueue.EnqueueOptions{ID: "run:" + i.RunID, ConcurrencyKey: "pipeline:" + p.ID})
			if (err == nil) != tc.accept {
				t.Fatal("SQL native hook lost typed-ref proof", err)
			}
			current, err := NewTriggerStore(db).GetIntent(ctx, i.ID)
			if err != nil {
				t.Fatal(err)
			}
			if tc.accept {
				if current.Status != model.TriggerQueued || current.Receipt == nil || current.Receipt.JobID != job.ID {
					t.Fatal("accepted typed source has no atomic receipt")
				}
				persisted, err := jobqueue.NewPostgres(db).Get(ctx, job.ID)
				if err != nil {
					t.Fatal(err)
				}
				var accepted struct {
					Source *model.PipelineSourcePlan `json:"source"`
				}
				if err := json.Unmarshal(persisted.Payload, &accepted); err != nil || accepted.Source == nil || accepted.Source.ObjectSHA != tc.planObject || accepted.Source.CommitSHA != tc.planCommit || accepted.Source.FullRef != tc.planRef {
					t.Fatal("resolved source plan changed across JSONB", err)
				}
			} else {
				if current.Status != model.TriggerPending || current.Receipt != nil {
					t.Fatal("rejected typed source advanced intent")
				}
				if _, err := jobs.Get(ctx, "run:"+i.RunID); !errors.Is(err, jobqueue.ErrNotFound) {
					t.Fatal("rejected typed source left runnable job", err)
				}
			}
		})
	}
}
