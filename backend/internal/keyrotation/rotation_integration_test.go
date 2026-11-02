//go:build integration

package keyrotation

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/secrets"
	secretsdb "github.com/santapong/cooker/internal/secrets/database"
	"github.com/santapong/cooker/internal/store/postgres"
)

func ownedDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("COOKER_KEYROTATION_TEST_DSN")
	if dsn == "" {
		t.Skip("owned PostgreSQL fixture required")
	}
	owner := os.Getenv("COOKER_KEYROTATION_TEST_OWNER")
	u, err := url.Parse(dsn)
	if uuid.Validate(owner) != nil || err != nil || u.Scheme != "postgres" || u.User == nil || u.User.Username() != "postgres" || u.Hostname() != "cooker-key-pg-"+owner || u.Port() != "5432" || u.Path != "/cooker_key_"+strings.ReplaceAll(owner, "-", "") || u.RawQuery != "sslmode=disable" || u.Fragment != "" {
		t.Fatal("only an exact UUID-owned fixture DSN is permitted")
	}
	if _, present := u.User.Password(); present {
		t.Fatal("fixture DSN must not contain credentials")
	}
	// Verify the task marker before NewStore can migrate or seed can truncate.
	probe, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal("owned marker read failed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	var database, actor, marker string
	err = probe.QueryRowContext(ctx, `SELECT current_database(),current_user,coalesce(shobj_description(oid,'pg_database'),'') FROM pg_database WHERE datname=current_database()`).Scan(&database, &actor, &marker)
	cancel()
	probe.Close()
	if err != nil || database != strings.TrimPrefix(u.Path, "/") || actor != "postgres" || marker != "cooker-owned-key-rotation:"+owner {
		t.Fatal("owned database marker mismatch")
	}
	st, err := postgres.NewStore(context.Background(), dsn)
	if err != nil {
		t.Fatal("owned schema initialization failed")
	}
	if st.Close() != nil {
		t.Fatal("schema pool close failed")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal("owned pool failed")
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)
	t.Cleanup(func() { db.Close() })
	return db
}

func execSQL(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatal("owned fixture SQL failed")
	}
}

func seedOwnedCiphertext(t *testing.T, db *sql.DB) (Input, *crypto.Codec, *crypto.Codec, []row) {
	t.Helper()
	ctx := context.Background()
	execSQL(t, db, `TRUNCATE environments, apps, source_connections, vercel_connections, neon_connections, notification_targets, jobs, pipelines, notification_events CASCADE`)
	oldKey, _ := crypto.GenerateKey()
	newKey, _ := crypto.GenerateKey()
	old, _ := crypto.NewCodec(oldKey)
	next, _ := crypto.NewCodec(newKey)
	legacy, _ := old.Seal([]byte("synthetic-secret-canary"))
	tagged, _ := old.SealVersioned([]byte("synthetic-secret-canary"))
	env, _ := json.Marshal(map[string][]byte{"TOKEN": legacy, "MIXED_TAG": tagged})
	for _, id := range []string{"ordinary", "_hosts", "registry_orphan"} {
		execSQL(t, db, `INSERT INTO environments(id,name,secrets,version)VALUES($1,$1,$2,7)`, id, string(env))
	}
	execSQL(t, db, `INSERT INTO apps(id,name,github_repo,webhook_secret,version)VALUES('app','app','fixture/repo',$1,8),('no-webhook','no-webhook','fixture/empty','',3),('null-webhook','null-webhook','fixture/null',NULL,4)`, base64.StdEncoding.EncodeToString(legacy))
	execSQL(t, db, `INSERT INTO source_connections(id,configuration,credential,version)VALUES('disabled-source','{"disabled":true}',$1,9)`, legacy)
	execSQL(t, db, `INSERT INTO vercel_connections(id,configuration,credential,version)VALUES('disabled-vercel','{"disabled":true}',$1,10)`, tagged)
	execSQL(t, db, `INSERT INTO neon_connections(id,configuration,credential,version,created_at,updated_at)VALUES('disabled-neon','{"enabled":false}',$1,11,now(),now())`, legacy)
	envelope, _ := json.Marshal(struct {
		Enc  string `json:"enc"`
		Data []byte `json:"data"`
	}{"v1", legacy})
	execSQL(t, db, `INSERT INTO notification_targets(id,name,kind,config,enabled)VALUES('target','target','webhook',$1,false),('legacy-target','legacy','webhook','{"url":"legacy-plaintext-canary"}',false)`, string(envelope))
	execSQL(t, db, `INSERT INTO pipelines(id,name)VALUES('pipeline','pipeline')`)
	execSQL(t, db, `INSERT INTO pipeline_runs(id,pipeline_id,status)VALUES('retained-run','pipeline','pending')`)
	execSQL(t, db, `INSERT INTO jobs(id,kind,payload,status,concurrency_key)VALUES('pending-job','fixture','{"environmentVersion":7,"sourceVersion":9,"artifact":"retained"}','pending','fixture')`)
	execSQL(t, db, `INSERT INTO notification_events(id,event_type,payload)VALUES('event','run.complete','{"runId":"retained-run"}')`)
	execSQL(t, db, `INSERT INTO notification_deliveries(id,event_id,target_id,target_version,kind)SELECT 'delivery','event',id,version,kind FROM notification_targets WHERE id='target'`)
	in := Input{OldKey: oldKey, NewKey: newKey, BackupPath: filepath.Join(t.TempDir(), "backup.json"), Actor: "fixture-operator", Offline: true}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal("fixture read failed")
	}
	rows, err := collect(ctx, tx)
	tx.Rollback()
	if err != nil {
		t.Fatal(err)
	}
	return in, old, next, rows
}

func collectOwned(t *testing.T, db *sql.DB) []row {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal("read transaction failed")
	}
	defer tx.Rollback()
	rows, err := collect(context.Background(), tx)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func protectedState(t *testing.T, db *sql.DB) []byte {
	t.Helper()
	var out bytes.Buffer
	for _, table := range []string{"jobs", "pipeline_runs", "notification_deliveries", "notification_events"} {
		var data []byte
		if db.QueryRow("SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb)::text FROM public."+table+" t").Scan(&data) != nil {
			t.Fatal("protected read failed")
		}
		out.Write(data)
	}
	var seq []byte
	if db.QueryRow(`SELECT jsonb_build_object('last_value',last_value,'is_called',is_called,'log_cnt',log_cnt)::text FROM notification_target_versions`).Scan(&seq) != nil {
		t.Fatal("sequence read failed")
	}
	out.Write(seq)
	var plain []byte
	if db.QueryRow(`SELECT to_jsonb(t)::text FROM notification_targets t WHERE id='legacy-target'`).Scan(&plain) != nil {
		t.Fatal("plaintext legacy read failed")
	}
	out.Write(plain)
	return out.Bytes()
}

func TestEnvironmentConditionalEditingActualPostgresCAS(t *testing.T) {
	db := ownedDB(t)
	_, codec, _, _ := seedOwnedCiphertext(t, db)
	envs := postgres.NewEnvironmentStore(db)
	manager := secretsdb.New(envs, codec)
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for _, key := range []string{"ONLY_WINNER_A", "ONLY_WINNER_B"} {
		workers.Add(1)
		go func(key string) {
			defer workers.Done()
			<-start
			_, err := manager.PutAtGeneration(context.Background(), "ordinary", key, []byte("synthetic-cas-value"), 7)
			results <- err
		}(key)
	}
	close(start)
	workers.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, secrets.ErrGenerationConflict) {
			conflict++
		} else {
			t.Fatal("unexpected CAS result")
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("same generation had multiple winners")
	}
	saved, err := envs.Get(context.Background(), "ordinary")
	if err != nil || saved.Version != 8 || len(saved.Secrets) != 3 {
		t.Fatal("CAS generation or inventory wrong")
	}
	if _, err := manager.DeleteAtGeneration(context.Background(), "ordinary", "TOKEN", 7); !errors.Is(err, secrets.ErrGenerationConflict) {
		t.Fatal("stale delete accepted")
	}
	if _, err := manager.Get(context.Background(), "ordinary", "TOKEN"); err != nil {
		t.Fatal("refused delete changed value")
	}
	if next, err := manager.DeleteAtGeneration(context.Background(), "ordinary", "TOKEN", 8); err != nil || next != 9 {
		t.Fatal("conditional delete failed")
	}
	// The stored representation, not an in-memory Manager snapshot, is observed
	// through a new pool. This does not claim official HTTP/owner acceptance.
	fresh, err := sql.Open("postgres", os.Getenv("COOKER_KEYROTATION_TEST_DSN"))
	if err != nil {
		t.Fatal("fresh CAS pool failed")
	}
	defer fresh.Close()
	fresh.SetMaxOpenConns(1)
	fresh.SetMaxIdleConns(0)
	persisted, err := postgres.NewEnvironmentStore(fresh).Get(context.Background(), "ordinary")
	if err != nil || persisted.Version != 9 {
		t.Fatal("CAS did not persist")
	}
	if _, present := persisted.Secrets["TOKEN"]; present {
		t.Fatal("delete did not persist")
	}
}

func assertValues(t *testing.T, rows []row, c *crypto.Codec) {
	t.Helper()
	for _, r := range rows {
		if r.Null || r.Kind == "plaintext_unchanged" || (r.Family == "apps" && len(r.Before) == 0) {
			continue
		}
		var blobs [][]byte
		switch r.Family {
		case "environments":
			var m map[string][]byte
			json.Unmarshal(r.Before, &m)
			for _, v := range m {
				blobs = append(blobs, v)
			}
		case "apps":
			v, err := base64.StdEncoding.DecodeString(string(r.Before))
			if err != nil {
				t.Fatal("base64 corrupted")
			}
			blobs = append(blobs, v)
		case "notification_targets":
			v, _, err := notificationData(r.Before)
			if err != nil {
				t.Fatal(err)
			}
			blobs = append(blobs, v)
		default:
			blobs = append(blobs, r.Before)
		}
		for _, b := range blobs {
			v, err := c.Open(b)
			if err != nil || string(v) != "synthetic-secret-canary" {
				t.Fatal("value identity not preserved")
			}
			clear(v)
		}
	}
}

func TestOfflineFiveFamiliesRotateFreshPoolRestore(t *testing.T) {
	db := ownedDB(t)
	in, old, next, before := seedOwnedCiphertext(t, db)
	protected := protectedState(t, db)
	report, err := Execute(context.Background(), db, "inventory", in)
	if err != nil || report.PlaintextNotifications != 1 {
		t.Fatal("inventory failed")
	}
	report, err = Execute(context.Background(), db, "rotate", in)
	if err != nil || report.State != "rotated" || report.Counts["environments"] != 6 || report.Counts["neon_connections"] != 1 || len(report.Counts) != 6 {
		t.Fatalf("rotation failed: %v", err)
	}
	after := collectOwned(t, db)
	assertValues(t, after, next)
	if bytes.Equal(before[0].Before, after[0].Before) {
		t.Fatal("ciphertext unchanged")
	}
	if !bytes.Equal(protected, protectedState(t, db)) {
		t.Fatal("authority, queued rows, sequence or plaintext target changed")
	}
	backupBytes, _ := os.ReadFile(in.BackupPath)
	reportBytes, _ := json.Marshal(report)
	for _, canary := range []string{"synthetic-secret-canary", "legacy-plaintext-canary", in.OldKey, in.NewKey} {
		if bytes.Contains(backupBytes, []byte(canary)) || bytes.Contains(reportBytes, []byte(canary)) {
			t.Fatal("secret in backup/report")
		}
	}
	// Closing and opening the pool proves no cached value is needed; process/PG
	// restart is deliberately a separate acceptance boundary.
	db.Close()
	fresh, err := sql.Open("postgres", os.Getenv("COOKER_KEYROTATION_TEST_DSN"))
	if err != nil {
		t.Fatal("fresh pool failed")
	}
	defer fresh.Close()
	fresh.SetMaxOpenConns(1)
	fresh.SetMaxIdleConns(0)
	report, err = Execute(context.Background(), fresh, "status", in)
	if err != nil || report.State != "after" {
		t.Fatal("committed classification failed")
	}
	report, err = Execute(context.Background(), fresh, "restore", in)
	if err != nil || report.State != "restored" {
		t.Fatal("restore failed")
	}
	restored := collectOwned(t, fresh)
	if !reflect.DeepEqual(before, restored) {
		t.Fatal("original ciphertext/metadata not restored")
	}
	assertValues(t, restored, old)
	if !bytes.Equal(protected, protectedState(t, fresh)) {
		t.Fatal("restored authority drift")
	}
	report, err = Execute(context.Background(), fresh, "restore", in)
	if err != nil || report.State != "already_restored" {
		t.Fatal("idempotent restore failed")
	}
}

func TestOfflinePartialWritesRollbackEveryFamily(t *testing.T) {
	db := ownedDB(t)
	for _, f := range families {
		t.Run(f.table, func(t *testing.T) {
			in, _, _, before := seedOwnedCiphertext(t, db)
			protected := protectedState(t, db)
			column := f.column + "::text"
			target := ""
			for _, r := range before {
				if r.Family == f.table && r.Kind == "encrypted" && !r.Null && len(r.Before) > 0 {
					target = r.ID
					break
				}
			}
			var original string
			if target == "" || db.QueryRow("SELECT "+column+" FROM public."+f.table+" WHERE id=$1", target).Scan(&original) != nil {
				t.Fatal("constraint binding failed")
			}
			quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
			execSQL(t, db, "ALTER TABLE public."+f.table+" ADD CONSTRAINT rotation_fixture_fault CHECK (id <> "+quote(target)+" OR "+column+" = "+quote(original)+")")
			_, err := Execute(context.Background(), db, "rotate", in)
			execSQL(t, db, "ALTER TABLE public."+f.table+" DROP CONSTRAINT rotation_fixture_fault")
			if !errors.Is(err, ErrDatabase) {
				t.Fatalf("fault did not refuse: %v", err)
			}
			if !reflect.DeepEqual(before, collectOwned(t, db)) || !bytes.Equal(protected, protectedState(t, db)) {
				t.Fatal("partial write leaked")
			}
			report, err := Execute(context.Background(), db, "status", in)
			if err != nil || report.State != "before" {
				t.Fatal("interrupted backup did not identify before state")
			}
			tx, _ := db.Begin()
			err = verifyTriggers(context.Background(), tx)
			tx.Rollback()
			if err != nil {
				t.Fatal("trigger not restored by rollback")
			}
		})
	}
}

func TestOfflineDriftAndMixedRestoreRefuse(t *testing.T) {
	db := ownedDB(t)
	for _, mode := range []string{"generation", "new-row", "deleted-row", "ciphertext", "mixed"} {
		t.Run(mode, func(t *testing.T) {
			in, _, next, _ := seedOwnedCiphertext(t, db)
			if _, err := Execute(context.Background(), db, "rotate", in); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "generation":
				execSQL(t, db, `UPDATE environments SET version=version+1 WHERE id='ordinary'`)
			case "new-row":
				execSQL(t, db, `INSERT INTO environments(id,name)VALUES('new-row','new-row')`)
			case "deleted-row":
				execSQL(t, db, `DELETE FROM source_connections WHERE id='disabled-source'`)
			case "ciphertext":
				v, _ := next.SealVersioned([]byte("changed"))
				execSQL(t, db, `UPDATE source_connections SET credential=$1 WHERE id='disabled-source'`, v)
			case "mixed":
				old, _ := crypto.NewCodec(in.OldKey)
				p, err := readBackup(in.BackupPath, old, next)
				if err != nil {
					t.Fatal(err)
				}
				for _, r := range p.Rows {
					if r.Family == "source_connections" {
						execSQL(t, db, `UPDATE source_connections SET credential=$1 WHERE id=$2`, r.Before, r.ID)
					}
				}
			}
			current := collectOwned(t, db)
			report, err := Execute(context.Background(), db, "status", in)
			expected := "drift"
			if mode == "mixed" {
				expected = "mixed"
			}
			if err != nil || report.State != expected {
				t.Fatal("drift classification failed")
			}
			if _, err := Execute(context.Background(), db, "restore", in); !errors.Is(err, ErrDrift) {
				t.Fatal("unsafe restore accepted")
			}
			if !reflect.DeepEqual(current, collectOwned(t, db)) {
				t.Fatal("refused restore changed rows")
			}
		})
	}
}

func TestOfflineWrongKeysCorruptionLiveClientsAndLeasesRefuse(t *testing.T) {
	db := ownedDB(t)
	for _, mode := range []string{"wrong-key", "corrupt-ciphertext", "live-client", "running-job", "sending-notification", "sync-lease", "neon-sync-lease", "rewritten-trigger", "function-settings"} {
		t.Run(mode, func(t *testing.T) {
			in, _, _, before := seedOwnedCiphertext(t, db)
			var cleanup func()
			switch mode {
			case "wrong-key":
				in.OldKey, _ = crypto.GenerateKey()
			case "corrupt-ciphertext":
				execSQL(t, db, `UPDATE source_connections SET credential='\x01'::bytea WHERE id='disabled-source'`)
			case "live-client":
				other, err := sql.Open("postgres", os.Getenv("COOKER_KEYROTATION_TEST_DSN"))
				if err != nil || other.Ping() != nil {
					t.Fatal("client setup failed")
				}
				cleanup = func() { other.Close() }
			case "running-job":
				execSQL(t, db, `UPDATE jobs SET status='running' WHERE id='pending-job'`)
			case "sending-notification":
				execSQL(t, db, `UPDATE notification_deliveries SET state='sending' WHERE id='delivery'`)
			case "sync-lease":
				execSQL(t, db, `UPDATE vercel_connections SET sync_lease='00000000-0000-4000-8000-000000000001' WHERE id='disabled-vercel'`)
			case "neon-sync-lease":
				execSQL(t, db, `UPDATE neon_connections SET sync_lease='nlease_fixture',sync_lease_until=now()+interval '100 seconds' WHERE id='disabled-neon'`)
			case "rewritten-trigger":
				execSQL(t, db, `ALTER TABLE notification_targets DISABLE TRIGGER notification_target_version`)
				cleanup = func() { execSQL(t, db, `ALTER TABLE notification_targets ENABLE TRIGGER notification_target_version`) }
			case "function-settings":
				execSQL(t, db, `ALTER FUNCTION cooker_notification_target_version() SET search_path = pg_catalog`)
				cleanup = func() { execSQL(t, db, `ALTER FUNCTION cooker_notification_target_version() RESET ALL`) }
			}
			before = collectOwned(t, db)
			_, err := Execute(context.Background(), db, "rotate", in)
			if cleanup != nil {
				cleanup()
			}
			if err == nil {
				t.Fatal("unsafe rotation accepted")
			}
			if !reflect.DeepEqual(before, collectOwned(t, db)) {
				t.Fatal("refused rotation changed ciphertext")
			}
			if _, err := os.Stat(in.BackupPath); !os.IsNotExist(err) {
				t.Fatal("backup written before preflight passed")
			}
		})
	}
}
