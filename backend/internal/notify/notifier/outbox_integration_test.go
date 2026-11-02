//go:build integration

package notifier

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/jobqueue"
)

// The fixture refuses every owner/runtime database URL. Its container has no
// published port and each test owns one new schema in this disposable DB.
func notificationDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("COOKER_NOTIFICATION_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("owned disposable notification PostgreSQL fixture not configured")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Hostname() != "cooker-roadmap-notifications-pg-20261002" || u.Path != "/cooker_notifications_test" {
		t.Fatal("refusing non-owned notification database")
	}
	root, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "notify_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := root.Exec(`CREATE SCHEMA ` + schema); err != nil {
		root.Close()
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(12)
	t.Cleanup(func() { db.Close(); _, _ = root.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); root.Close() })
	for _, name := range []string{"010_jobs.up.sql", "011_notification_targets.up.sql", "035_notification_outbox.up.sql"} {
		body, err := os.ReadFile(filepath.Join("..", "..", "store", "postgres", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatalf("migration %s failed", name)
		}
	}
	if _, err := db.Exec(`CREATE TABLE pipeline_runs(id TEXT PRIMARY KEY,status TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	return db
}

func notificationFixture(t *testing.T, db *sql.DB, receiver string) (*Outbox, *PostgresTargetStore) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	codec, err := crypto.NewCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	ts := NewPostgresTargetStore(db).WithCodec(codec)
	cfg, _ := json.Marshal(WebhookConfig{URL: receiver + "/fixture-endpoint-secret", BearerToken: "fixture-access-secret"})
	if err := ts.Create(context.Background(), Target{ID: "target-one", Kind: "webhook", Config: cfg, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	r := NewRegistry()
	r.Register(NewWebhookNotifier())
	return NewOutbox(db, ts, r), ts
}

func fixtureEvent(run string) Event {
	return Event{Type: EventRunFailed, PipelineID: "pipe-one", RunID: run, PipelineName: "fixture-execution-secret", Error: "fixture-execution-secret", URL: "https://fixture-execution-secret"}
}

func onlyDelivery(t *testing.T, o *Outbox) Delivery {
	t.Helper()
	page, err := o.ListDeliveries(context.Background(), DeliveryQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("want one delivery, got %d", len(page.Items))
	}
	return page.Items[0]
}

func process(t *testing.T, o *Outbox, now time.Time) bool {
	t.Helper()
	worked, err := o.ProcessOne(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	return worked
}

func assertNoNotificationSecrets(t *testing.T, db *sql.DB, logs []byte) {
	t.Helper()
	var events, deliveries, receipts string
	for _, q := range []struct {
		query string
		out   *string
	}{
		{`SELECT COALESCE(json_agg(e)::text,'[]') FROM notification_events e`, &events},
		{`SELECT COALESCE(json_agg(d)::text,'[]') FROM notification_deliveries d`, &deliveries},
		{`SELECT COALESCE(json_agg(r)::text,'[]') FROM notification_job_receipts r`, &receipts},
	} {
		if err := db.QueryRow(q.query).Scan(q.out); err != nil {
			t.Fatal(err)
		}
	}
	var cfg string
	if err := db.QueryRow(`SELECT config::text FROM notification_targets WHERE id='target-one'`).Scan(&cfg); err != nil {
		t.Fatal(err)
	}
	joined := events + deliveries + receipts + cfg + string(logs)
	for _, canary := range []string{"fixture-execution-secret", "fixture-access-secret", "fixture-endpoint-secret", "fixture-response-secret", "fixture-db-error-secret"} {
		if strings.Contains(joined, canary) {
			t.Fatal("notification persistence/logs leaked a synthetic secret")
		}
	}
}

func TestOutboxConcurrentAcceptanceRestartAndDuplicateCallbacks(t *testing.T) {
	db := notificationDB(t)
	var calls atomic.Int32
	var bodyMu sync.Mutex
	var bodies [][]byte
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		b, _ := io.ReadAll(r.Body)
		bodyMu.Lock()
		bodies = append(bodies, b)
		bodyMu.Unlock()
		if r.Header.Get("X-Cooker-Event-ID") == "" || r.Header.Get("Idempotency-Key") == "" {
			t.Error("missing stable receiver identity")
		}
		w.WriteHeader(204)
	}))
	defer receiver.Close()
	o, ts := notificationFixture(t, db, receiver.URL)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := o.Accept(context.Background(), fixtureEvent("run-one")); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 0 {
		t.Fatal("acceptance performed an external send")
	}
	restarted := NewOutbox(db, ts, o.registry)
	if !process(t, restarted, time.Now().Add(time.Second)) || onlyDelivery(t, restarted).State != "delivered" {
		t.Fatal("restart did not deliver durable pending event")
	}
	if err := restarted.Accept(context.Background(), fixtureEvent("run-one")); err != nil {
		t.Fatal(err)
	}
	if process(t, restarted, time.Now().Add(time.Hour)) || calls.Load() != 1 {
		t.Fatal("completed callback was blindly repeated")
	}
	assertNoNotificationSecrets(t, db, nil)
	bodyMu.Lock()
	defer bodyMu.Unlock()
	if bytes.Contains(bodies[0], []byte("fixture-execution-secret")) {
		t.Fatal("receiver got raw execution data")
	}
}

func TestOutboxTargetReplacementDisableDeleteAndLateSelection(t *testing.T) {
	for _, change := range []string{"replace", "disable", "delete", "filter", "delete-and-recreate"} {
		t.Run(change, func(t *testing.T) {
			db := notificationDB(t)
			var calls atomic.Int32
			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(204) }))
			defer receiver.Close()
			o, ts := notificationFixture(t, db, receiver.URL)
			if err := o.Accept(context.Background(), fixtureEvent("run-one")); err != nil {
				t.Fatal(err)
			}
			target, _ := ts.Get(context.Background(), "target-one")
			switch change {
			case "delete-and-recreate":
				if err := ts.Delete(context.Background(), target.ID); err != nil {
					t.Fatal(err)
				}
				if err := ts.Create(context.Background(), target); err != nil {
					t.Fatal(err)
				}
			case "delete":
				if err := ts.Delete(context.Background(), target.ID); err != nil {
					t.Fatal(err)
				}
			case "disable":
				target.Enabled = false
				if err := ts.Update(context.Background(), target); err != nil {
					t.Fatal(err)
				}
			case "filter":
				target.EventTypes = []EventType{EventRunSucceeded}
				if err := ts.Update(context.Background(), target); err != nil {
					t.Fatal(err)
				}
			case "replace":
				target.Config = json.RawMessage(`{"url":"http://127.0.0.1:1/replacement"}`)
				if err := ts.Update(context.Background(), target); err != nil {
					t.Fatal(err)
				}
			}
			process(t, o, time.Now().Add(time.Second))
			if onlyDelivery(t, o).State != "cancelled" || calls.Load() != 0 {
				t.Fatal("changed target was contacted")
			}
			if err := o.Accept(context.Background(), fixtureEvent("run-one")); err != nil {
				t.Fatal(err)
			}
			if process(t, o, time.Now().Add(time.Hour)) {
				t.Fatal("old accepted event adopted replacement")
			}
		})
	}
	t.Run("no-matching-target-at-acceptance", func(t *testing.T) {
		db := notificationDB(t)
		o, ts := notificationFixture(t, db, "http://127.0.0.1:1")
		target, _ := ts.Get(context.Background(), "target-one")
		target.Enabled = false
		_ = ts.Update(context.Background(), target)
		if err := o.Accept(context.Background(), fixtureEvent("run-one")); err != nil {
			t.Fatal(err)
		}
		target.Enabled = true
		_ = ts.Update(context.Background(), target)
		_ = o.Accept(context.Background(), fixtureEvent("run-one"))
		page, err := o.ListDeliveries(context.Background(), DeliveryQuery{})
		if err != nil || page.Total != 0 {
			t.Fatal("later target was retroactively selected")
		}
	})
}

func TestOutboxBoundedBackoffAndSafeReceiverErrors(t *testing.T) {
	for _, status := range []int{503, 429, 400} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			db := notificationDB(t)
			var calls atomic.Int32
			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
				_, _ = w.Write([]byte("fixture-response-secret"))
			}))
			defer receiver.Close()
			o, _ := notificationFixture(t, db, receiver.URL)
			_ = o.Accept(context.Background(), fixtureEvent("run-one"))
			now := time.Now().UTC().Truncate(time.Microsecond).Add(time.Second)
			process(t, o, now)
			d := onlyDelivery(t, o)
			if status == 400 {
				if d.State != "failed" || d.Attempts != 1 {
					t.Fatal("permanent rejection was retried")
				}
			} else {
				if d.State != "retry_wait" || d.NextAttemptAt.Sub(now) != 30*time.Second {
					t.Fatalf("first backoff incorrect: state=%s delta=%s", d.State, d.NextAttemptAt.Sub(now))
				}
				if process(t, o, now.Add(29*time.Second)) {
					t.Fatal("retry ran early")
				}
				process(t, o, now.Add(30*time.Second))
				d = onlyDelivery(t, o)
				if d.Attempts != 2 || d.NextAttemptAt.Sub(now.Add(30*time.Second)) != 2*time.Minute {
					t.Fatal("second backoff incorrect")
				}
				process(t, o, now.Add(150*time.Second))
				d = onlyDelivery(t, o)
				if d.State != "failed" || d.LastCode != "retry_exhausted" || !d.DuplicatePossible {
					t.Fatal("retry cap/at-least-once limit missing")
				}
			}
			if process(t, o, now.Add(24*time.Hour)) {
				t.Fatal("terminal failed send repeated")
			}
			assertNoNotificationSecrets(t, db, nil)
		})
	}
}

func TestOutboxTransportUncertaintyIsNotAutomaticallyRepeated(t *testing.T) {
	db := notificationDB(t)
	var calls atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		hijacker := w.(http.Hijacker)
		conn, _, err := hijacker.Hijack()
		if err == nil {
			conn.Close()
		}
	}))
	defer receiver.Close()
	o, _ := notificationFixture(t, db, receiver.URL)
	_ = o.Accept(context.Background(), fixtureEvent("run-one"))
	process(t, o, time.Now().Add(time.Second))
	d := onlyDelivery(t, o)
	if d.State != "needs_attention" || !d.DuplicatePossible || d.Attempts != 1 {
		t.Fatal("lost response was treated as a safe retry")
	}
	if process(t, o, time.Now().Add(time.Hour)) || calls.Load() != 1 {
		t.Fatal("uncertain send repeated")
	}
}

func TestOutboxWorkerLossAndResultPersistenceFailure(t *testing.T) {
	for _, afterSend := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-send", true: "after-receiver-acceptance"}[afterSend], func(t *testing.T) {
			db := notificationDB(t)
			var calls atomic.Int32
			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(204) }))
			defer receiver.Close()
			o, ts := notificationFixture(t, db, receiver.URL)
			_ = o.Accept(context.Background(), fixtureEvent("run-one"))
			now := time.Now().Add(time.Second)
			if afterSend {
				_, err := db.Exec(`CREATE FUNCTION fixture_finish_failure() RETURNS trigger AS $$ BEGIN IF NEW.state='delivered' THEN RAISE EXCEPTION 'fixture-db-error-secret'; END IF; RETURN NEW; END; $$ LANGUAGE plpgsql;
			CREATE TRIGGER fixture_finish_failure BEFORE UPDATE ON notification_deliveries FOR EACH ROW EXECUTE FUNCTION fixture_finish_failure()`)
				if err != nil {
					t.Fatal(err)
				}
				_, err = o.ProcessOne(context.Background(), now)
				if err == nil || strings.Contains(err.Error(), "fixture-db-error-secret") {
					t.Fatal("persistence error absent/unsafe")
				}
			} else {
				if _, err := o.claim(context.Background(), now); err != nil {
					t.Fatal(err)
				}
			}
			restarted := NewOutbox(db, ts, o.registry)
			if _, err := restarted.RecoverExpired(context.Background(), now.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			d := onlyDelivery(t, restarted)
			if d.State != "needs_attention" || d.LastCode != "worker_lost" {
				t.Fatal("lost claim was silently resent")
			}
			if process(t, restarted, now.Add(time.Hour)) {
				t.Fatal("lost worker delivery automatically repeated")
			}
			if calls.Load() != map[bool]int32{false: 0, true: 1}[afterSend] {
				t.Fatal("unexpected receiver count")
			}
		})
	}
}

func seedTerminalJob(t *testing.T, db *sql.DB, id string, status string, finished time.Time) *jobqueue.Postgres {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO pipeline_runs(id,status) VALUES($1,$2)`, id, status); err != nil {
		t.Fatal(err)
	}
	queueStatus := map[string]string{"success": "succeeded", "failed": "failed", "cancelled": "cancelled"}[status]
	payload, _ := json.Marshal(map[string]any{"runId": id, "pipelineId": "pipe-one", "unexpectedCredential": "fixture-execution-secret"})
	_, err := db.Exec(`INSERT INTO jobs(id,kind,payload,status,finished_at,updated_at,last_error) VALUES($1,'pipeline-run',$2,$3,$4,$4,'fixture-execution-secret')`, "job-"+id, payload, queueStatus, finished)
	if err != nil {
		t.Fatal(err)
	}
	return jobqueue.NewPostgres(db)
}

func TestOutboxCommitCallbackCrashRestartAndRetentionBarrier(t *testing.T) {
	for _, status := range []string{"success", "failed", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			db := notificationDB(t)
			var calls atomic.Int32
			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(204) }))
			defer receiver.Close()
			o, ts := notificationFixture(t, db, receiver.URL)
			now := time.Now().Add(time.Second)
			queue := seedTerminalJob(t, db, "run-one", status, now)
			// No OnComplete callback runs. A fresh outbox recovers the committed job.
			restarted := NewOutbox(db, ts, o.registry)
			queue.BeforePrune = restarted.CaptureTerminalJob
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				if err := restarted.ReconcileJobs(context.Background(), 100); err != nil {
					t.Error(err)
				}
			}()
			go func() {
				defer wg.Done()
				if _, err := queue.DeleteOlderThan(context.Background(), now.Add(time.Hour)); err != nil {
					t.Error(err)
				}
			}()
			wg.Wait()
			process(t, restarted, now.Add(2*time.Second))
			if onlyDelivery(t, restarted).State != "delivered" || calls.Load() != 1 {
				t.Fatal("terminal event not recovered exactly once locally")
			}
			if err := restarted.ReconcileJobs(context.Background(), 100); err != nil {
				t.Fatal(err)
			}
			if process(t, restarted, now.Add(time.Hour)) {
				t.Fatal("retention/restart recreated delivery")
			}
			var actual string
			if err := db.QueryRow(`SELECT status FROM pipeline_runs WHERE id='run-one'`).Scan(&actual); err != nil || actual != status {
				t.Fatal("notification changed deployment outcome")
			}
			assertNoNotificationSecrets(t, db, nil)
		})
	}
}

func TestOutboxAcceptanceDBErrorRetainsSourceAndRepairs(t *testing.T) {
	db := notificationDB(t)
	var calls atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(400) }))
	defer receiver.Close()
	o, _ := notificationFixture(t, db, receiver.URL)
	now := time.Now().Add(time.Second)
	queue := seedTerminalJob(t, db, "run-one", "success", now)
	queue.BeforePrune = o.CaptureTerminalJob
	_, err := db.Exec(`CREATE FUNCTION fixture_accept_failure() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'fixture-db-error-secret'; END; $$ LANGUAGE plpgsql;
	CREATE TRIGGER fixture_accept_failure BEFORE INSERT ON notification_deliveries FOR EACH ROW EXECUTE FUNCTION fixture_accept_failure()`)
	if err != nil {
		t.Fatal(err)
	}
	if err := o.ReconcileJobs(context.Background(), 100); err == nil || strings.Contains(err.Error(), "fixture-db-error-secret") {
		t.Fatal("DB rejection must be visible and safe")
	}
	if n, err := queue.DeleteOlderThan(context.Background(), now.Add(time.Hour)); n != 0 || err == nil {
		t.Fatal("retention removed unrepaired notification source")
	}
	var state string
	if err := db.QueryRow(`SELECT status FROM jobs WHERE id='job-run-one'`).Scan(&state); err != nil || state != "succeeded" {
		t.Fatal("notification failure changed job success")
	}
	var eventCount, receiptCount int
	_ = db.QueryRow(`SELECT COUNT(*) FROM notification_events`).Scan(&eventCount)
	_ = db.QueryRow(`SELECT COUNT(*) FROM notification_job_receipts`).Scan(&receiptCount)
	if eventCount != 0 || receiptCount != 0 {
		t.Fatal("failed capture committed partial metadata")
	}
	page, err := o.ListDeliveries(context.Background(), DeliveryQuery{})
	if err != nil || page.PendingSourceJobs != 1 {
		t.Fatal("pending source was hidden")
	}
	if _, err := db.Exec(`DROP TRIGGER fixture_accept_failure ON notification_deliveries`); err != nil {
		t.Fatal(err)
	}
	// Persistence recovery respects source backoff, including after restart.
	if _, err := db.Exec(`UPDATE notification_source_repairs SET next_capture_at=NOW() WHERE job_id='job-run-one'`); err != nil {
		t.Fatal(err)
	}
	if err := o.ReconcileJobs(context.Background(), 100); err != nil {
		t.Fatal(err)
	}
	process(t, o, now.Add(time.Second))
	if onlyDelivery(t, o).State != "failed" || calls.Load() != 1 {
		t.Fatal("repair did not preserve delivery failure independently")
	}
	if err := db.QueryRow(`SELECT status FROM jobs WHERE id='job-run-one'`).Scan(&state); err != nil || state != "succeeded" {
		t.Fatal("receiver rejection changed deployment success")
	}
	assertNoNotificationSecrets(t, db, nil)
}

func TestOutboxHistoricalCutoverAndPartialSourceRepair(t *testing.T) {
	db := notificationDB(t)
	o, _ := notificationFixture(t, db, "http://127.0.0.1:1")
	seedTerminalJob(t, db, "historic-run", "success", time.Now().Add(-24*time.Hour))
	seedTerminalJob(t, db, "new-run", "success", time.Now().Add(time.Second))
	_, err := db.Exec(`INSERT INTO jobs(id,kind,payload,status,finished_at,updated_at) VALUES('invalid-job','pipeline-run','{}','failed',NOW()+interval '2 seconds',NOW()+interval '2 seconds')`)
	if err != nil {
		t.Fatal(err)
	}
	if err := o.ReconcileJobs(context.Background(), 100); err == nil {
		t.Fatal("partial source failure hidden")
	}
	page, err := o.ListDeliveries(context.Background(), DeliveryQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.PendingSourceJobs != 1 {
		t.Fatal("one malformed source blocked unrelated delivery or replayed history")
	}
	var disposition string
	_ = db.QueryRow(`SELECT disposition FROM notification_job_receipts WHERE job_id='job-historic-run'`).Scan(&disposition)
	if disposition != "before_policy_cutover" {
		t.Fatal("pre-upgrade history was not suppressed")
	}
}

func TestOutboxMoreThanOnePoisonBatchDoesNotStarveNewerJobs(t *testing.T) {
	db := notificationDB(t)
	var calls atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(204) }))
	defer receiver.Close()
	o, _ := notificationFixture(t, db, receiver.URL)
	for i := 0; i < 205; i++ {
		if _, err := db.Exec(`INSERT INTO jobs(id,kind,payload,status,finished_at,updated_at)
		VALUES($1,'pipeline-run','{}','failed',NOW()+interval '1 second',NOW()+interval '1 second')`, fmt.Sprintf("poison-%03d", i)); err != nil {
			t.Fatal(err)
		}
	}
	seedTerminalJob(t, db, "healthy-run", "success", time.Now().Add(3*time.Second))
	for i := 0; i < 3; i++ {
		if err := o.ReconcileJobs(context.Background(), 100); err == nil {
			t.Fatal("poison-source warning hidden")
		}
	}
	page, err := o.ListDeliveries(context.Background(), DeliveryQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.PendingSourceJobs != 205 || page.PendingSourceRepairJobs != 205 {
		t.Fatalf("source backlog visibility incorrect: %+v", page)
	}
	process(t, o, time.Now().Add(4*time.Second))
	if calls.Load() != 1 || onlyDelivery(t, o).State != "delivered" {
		t.Fatal("newer healthy source was starved")
	}
	var jobs, receipts int
	_ = db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE id LIKE 'poison-%'`).Scan(&jobs)
	_ = db.QueryRow(`SELECT COUNT(*) FROM notification_job_receipts WHERE job_id LIKE 'poison-%'`).Scan(&receipts)
	if jobs != 205 || receipts != 0 {
		t.Fatal("poison sources were deleted or marked accepted")
	}
	// Force only one repair due in the owned fixture; verify the bounded backoff
	// advances rather than spinning, and that unaccepted sources remain visible.
	if _, err := db.Exec(`UPDATE notification_source_repairs SET next_capture_at=NOW() WHERE job_id='poison-000'`); err != nil {
		t.Fatal(err)
	}
	if err := o.ReconcileJobs(context.Background(), 100); err == nil {
		t.Fatal("repair failure hidden")
	}
	var attempts int
	var delay float64
	if err := db.QueryRow(`SELECT attempts,EXTRACT(EPOCH FROM next_capture_at-NOW()) FROM notification_source_repairs WHERE job_id='poison-000'`).Scan(&attempts, &delay); err != nil || attempts != 2 || delay < 110 || delay > 121 {
		t.Fatal("source repair backoff incorrect")
	}
}

func TestOutboxFilteringPaginationAndOldTargetWriterCompatibility(t *testing.T) {
	db := notificationDB(t)
	o, ts := notificationFixture(t, db, "http://127.0.0.1:1")
	for _, id := range []string{"run-one", "run-two", "run-three"} {
		if err := o.Accept(context.Background(), fixtureEvent(id)); err != nil {
			t.Fatal(err)
		}
	}
	page, err := o.ListDeliveries(context.Background(), DeliveryQuery{RunID: "run-two", Limit: 1})
	if err != nil || page.Total != 1 || len(page.Items) != 1 {
		t.Fatal("filter was applied after pagination")
	}
	page, err = o.ListDeliveries(context.Background(), DeliveryQuery{State: "pending", Limit: 1, Offset: 1})
	if err != nil || page.Total != 3 || len(page.Items) != 1 {
		t.Fatal("pagination lost count")
	}
	if _, err := o.GetDelivery(context.Background(), "absent"); !errors.Is(err, ErrDeliveryNotFound) {
		t.Fatal("absent delivery not distinguished")
	}
	// Migration trigger versions writers that do not know the version column.
	if _, err := db.Exec(`UPDATE notification_targets SET name='renamed',updated_at=NOW() WHERE id='target-one'`); err != nil {
		t.Fatal(err)
	}
	target, err := ts.Get(context.Background(), "target-one")
	if err != nil || target.Version != 2 {
		t.Fatal("legacy update failed to fence pending deliveries")
	}
	var oldName string
	if err := db.QueryRow(`SELECT name FROM notification_targets WHERE id='target-one'`).Scan(&oldName); err != nil || oldName != "renamed" {
		t.Fatal("old reader compatibility broken")
	}
}

func TestOutboxDispatcherAcceptsWithoutSendingAndSafeLogs(t *testing.T) {
	db := notificationDB(t)
	o, ts := notificationFixture(t, db, "http://127.0.0.1:1")
	d := NewDispatcher(ts, o.registry)
	d.Outbox = o
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(old)
	if err := d.Dispatch(context.Background(), fixtureEvent("run-one")); err != nil {
		t.Fatal(err)
	}
	if onlyDelivery(t, o).State != "pending" {
		t.Fatal("dispatch did not commit pending delivery")
	}
	process(t, o, time.Now().Add(time.Second))
	if onlyDelivery(t, o).State != "needs_attention" {
		t.Fatal("unreachable endpoint must remain conservatively uncertain")
	}
	assertNoNotificationSecrets(t, db, logs.Bytes())
}
