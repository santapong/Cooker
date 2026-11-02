//go:build integration

package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	cookercrypto "github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/logstore"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/secrets/database"
	"github.com/santapong/cooker/internal/source/connection"
	storepg "github.com/santapong/cooker/internal/store/postgres"
)

// This opt-in drill owns a fresh PostgreSQL container and two new databases.
// It never accepts an existing database URL or reads a configured secret key.
// Random encryption keys exist only in memory; all credential values are fake.
func TestPostgresBackupRestoreLive(t *testing.T) {
	if os.Getenv("COOKER_RESTORE_ACCEPTANCE") != "true" {
		t.Skip("set COOKER_RESTORE_ACCEPTANCE=true for a disposable Docker restore drill")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	started := time.Now()
	name := "cooker-restore-drill-" + uuid.NewString()[:8]
	report := map[string]any{"fixture": name, "scope": "disposable PostgreSQL logical backup/restore; no online key rotation", "startedAt": started.UTC().Format(time.RFC3339)}
	cmd := exec.CommandContext(ctx, "docker", "run", "-d", "--name", name, "-e", "POSTGRES_DB=cooker_restore_source", "-e", "POSTGRES_PASSWORD=disposable-fixture-only", "-p", "127.0.0.1::5432", "postgres:16-alpine")
	created, err := cmd.Output()
	if err != nil {
		t.Fatalf("create isolated PostgreSQL: %v", err)
	}
	id := strings.TrimSpace(string(created))
	t.Logf("owned fixture=%s container=%s", name, id)
	var volumes []string
	t.Cleanup(func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		// Use the exact ID returned by our create, never a shared service name.
		if err := exec.CommandContext(cleanupCtx, "docker", "rm", "-f", "-v", id).Run(); err != nil {
			t.Errorf("owned PostgreSQL cleanup: %v", err)
		}
		containers, err := exec.CommandContext(cleanupCtx, "docker", "ps", "-a", "--filter", "id="+id, "-q").Output()
		clean := err == nil && len(bytes.TrimSpace(containers)) == 0
		for _, volume := range volumes {
			remaining, err := exec.CommandContext(cleanupCtx, "docker", "volume", "ls", "--filter", "name=^"+volume+"$", "-q").Output()
			clean = clean && err == nil && len(bytes.TrimSpace(remaining)) == 0
		}
		if !clean {
			t.Error("owned restore fixture resources remain")
		}
		report["cleanupVerified"] = clean
		report["passed"] = !t.Failed()
		report["durationSeconds"] = time.Since(started).Seconds()
		if path := os.Getenv("COOKER_RESTORE_EVIDENCE_FILE"); path != "" {
			data, err := json.MarshalIndent(report, "", "  ")
			if err != nil {
				t.Error(err)
				return
			}
			if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
				t.Error(err)
			}
		}
		t.Logf("restore cleanup verified=%t", clean)
	})
	port := strings.TrimSpace(restoreDocker(t, ctx, "port", id, "5432/tcp"))
	if !strings.HasPrefix(port, "127.0.0.1:") || strings.Contains(port, "\n") {
		t.Fatal("fixture is not bound only to loopback")
	}
	mounts := strings.TrimSpace(restoreDocker(t, ctx, "inspect", "--format", "{{range .Mounts}}{{if eq .Type \"volume\"}}{{.Name}}{{println}}{{end}}{{end}}", id))
	if mounts != "" {
		volumes = strings.Split(mounts, "\n")
	}
	if len(volumes) != 1 {
		t.Fatal("unexpected owned fixture volumes")
	}
	dsn := func(database string) string {
		return "postgres://postgres:disposable-fixture-only@" + port + "/" + database + "?sslmode=disable"
	}
	sourceDB, err := sql.Open("postgres", dsn("cooker_restore_source"))
	if err != nil {
		t.Fatal(err)
	}
	defer sourceDB.Close()
	ready := false
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); {
		if sourceDB.PingContext(ctx) == nil {
			ready = true
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !ready {
		t.Fatal("isolated PostgreSQL did not become ready")
	}
	st, err := storepg.NewStore(ctx, dsn("cooker_restore_source"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	versions := restoreMigrationVersions(t, ctx, sourceDB)
	if len(versions) < 31 {
		t.Fatalf("current migrations incomplete: %d", len(versions))
	}
	report["migrationVersions"] = versions
	report["postgresVersion"] = strings.TrimSpace(restoreDocker(t, ctx, "exec", id, "postgres", "--version"))
	report["dumpToolVersion"] = strings.TrimSpace(restoreDocker(t, ctx, "exec", id, "pg_dump", "--version"))
	key, err := cookercrypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	codec, err := cookercrypto.NewCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	const probe = "not-a-real-secret-restore-probe"
	const webhook = "not-a-real-webhook-secret-for-restore-probe"
	const token = "not-a-real-provider-token-restore-probe"
	env := &model.Environment{ID: uuid.NewString(), Name: "Restore fixture", PlainVars: map[string]string{"DB_NAME": "restore_fixture"}}
	if err := st.Environments.Create(ctx, env); err != nil {
		t.Fatal(err)
	}
	if err := database.New(st.Environments, codec).Put(ctx, env.ID, "RESTORE_PROBE", []byte(probe)); err != nil {
		t.Fatal(err)
	}
	env, err = st.Environments.Get(ctx, env.ID)
	if err != nil {
		t.Fatal(err)
	}
	source := &connection.Connection{Name: "Restore fixture", Provider: "github", AuthMode: "token", Repositories: []string{"fixture/restore-probe"}, Enabled: true}
	connections := &connection.Service{Store: &connection.Postgres{DB: sourceDB}, Codec: codec}
	if err := connections.CreateWithCredentials(ctx, source, connection.Credentials{WebhookSecret: webhook, Token: token}); err != nil {
		t.Fatal(err)
	}
	sealedEnvironment := append([]byte(nil), env.Secrets["RESTORE_PROBE"]...)
	sealedConnection := append([]byte(nil), source.Credential...)
	if bytes.Contains(sealedEnvironment, []byte(probe)) || bytes.Contains(sealedConnection, []byte(token)) {
		t.Fatal("plaintext persisted as ciphertext")
	}
	app := &model.App{ID: uuid.NewString(), Name: "Restore fixture", EnvironmentID: env.ID, DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost, Prefix: "restore-fixture"}, BuildPlan: &model.BuildPlan{Kind: model.BuildPlanCompose, EnvironmentScope: ServiceEnvironmentScope, ServiceEnvironment: map[string][]string{"web": {"RESTORE_PROBE", "DB_NAME"}}}}
	if err := st.Apps.Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	pipeline := &model.Pipeline{ID: uuid.NewString(), Name: "Restore fixture", Stages: []model.Stage{{ID: "deploy-web", Type: model.StageTypeDeploy, Config: model.StageConfig{RuntimeName: "restore-fixture-web-1", ComposeServiceName: "web", DeployRuntime: "compose", ReviewOnly: true}}}}
	if err := st.Pipelines.Create(ctx, pipeline); err != nil {
		t.Fatal(err)
	}
	run := &model.PipelineRun{ID: uuid.NewString(), PipelineID: pipeline.ID, Status: model.RunStatusNeedsAttention, StageRuns: []model.StageRun{{StageID: "deploy-web", Status: model.RunStatusNeedsAttention}}, StartedByUserSub: "restore-fixture-actor"}
	if err := st.Runs.Create(ctx, run); err != nil {
		t.Fatal(err)
	}
	deploy := &model.AppDeploy{ID: uuid.NewString(), AppID: app.ID, RunID: run.ID, PipelineID: pipeline.ID, Status: model.RunStatusNeedsAttention, Kind: model.AppDeployKindDeploy}
	if err := st.AppDeploys.Create(ctx, deploy); err != nil {
		t.Fatal(err)
	}
	jobs := jobqueue.NewPostgres(sourceDB)
	payload, err := json.Marshal(AppDeployPlan{Version: 1, RunID: run.ID, App: app, Commit: strings.Repeat("a", 40), EnvironmentVersion: env.Version, Actor: run})
	if err != nil {
		t.Fatal(err)
	}
	job, err := jobs.Enqueue(ctx, JobKindAppDeploy, payload, jobqueue.EnqueueOptions{ID: "app-run:" + run.ID, ConcurrencyKey: "app:" + app.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jobs.Dequeue(ctx, "fixture-owner", []string{JobKindAppDeploy}); err != nil {
		t.Fatal(err)
	}
	if err := jobs.Suspend(ctx, "fixture-owner", job.ID, "restore fixture requires reconciliation"); err != nil {
		t.Fatal(err)
	}
	job, err = jobs.Get(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := jobqueue.PayloadDigest(job.Payload)
	if err != nil {
		t.Fatal(err)
	}
	logs := logstore.NewPostgres(sourceDB, logstore.Limits{})
	var masked bytes.Buffer
	writer := NewSecretLogWriter(WithLogSecrets(ctx, []string{probe}), &masked)
	_, _ = writer.Write([]byte("probe=" + probe[:10]))
	_, _ = writer.Write([]byte(probe[10:]))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if masked.String() != "probe=[REDACTED]" {
		t.Fatal("restore fixture log not redacted before persistence")
	}
	for _, line := range []string{masked.String(), "deployment requires reconciliation"} {
		if _, err := logs.AppendNext(ctx, run.ID, "deploy-web", logstore.Entry{Line: line, TS: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	beforeEntries, beforeCursor, err := logs.History(ctx, run.ID, "deploy-web", 0)
	if err != nil {
		t.Fatal(err)
	}
	// All writers are quiescent. Take an actual custom-format logical backup.
	dumpPath := filepath.Join(t.TempDir(), "fixture.dump")
	dump, err := os.OpenFile(dumpPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	dumpCmd := exec.CommandContext(ctx, "docker", "exec", id, "pg_dump", "-U", "postgres", "-d", "cooker_restore_source", "--format=custom", "--no-owner", "--no-acl")
	dumpCmd.Stdout = dump
	if err := dumpCmd.Run(); err != nil {
		dump.Close()
		t.Fatalf("dump: %v", err)
	}
	if err := dump.Close(); err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(archive)
	report["archiveSHA256"] = hex.EncodeToString(sum[:])
	report["archiveBytes"] = len(archive)
	restoreDocker(t, ctx, "exec", id, "createdb", "-U", "postgres", "--template=template0", "cooker_restore_target")
	restoredDB, err := sql.Open("postgres", dsn("cooker_restore_target"))
	if err != nil {
		t.Fatal(err)
	}
	defer restoredDB.Close()
	var empty bool
	if err := restoredDB.QueryRowContext(ctx, "SELECT to_regclass('public.schema_migrations') IS NULL").Scan(&empty); err != nil || !empty {
		t.Fatal("restore target was not empty")
	}
	restoreCmd := exec.CommandContext(ctx, "docker", "exec", "-i", id, "pg_restore", "-U", "postgres", "-d", "cooker_restore_target", "--exit-on-error", "--single-transaction", "--no-owner", "--no-acl")
	restoreCmd.Stdin = bytes.NewReader(archive)
	if err := restoreCmd.Run(); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !reflect.DeepEqual(versions, restoreMigrationVersions(t, ctx, restoredDB)) {
		t.Fatal("restored migration ledger differs before application startup")
	}
	restored, err := storepg.NewStore(ctx, dsn("cooker_restore_target"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if !reflect.DeepEqual(versions, restoreMigrationVersions(t, ctx, restoredDB)) {
		t.Fatal("restore startup unexpectedly changed migration ledger")
	}
	readEnv, err := restored.Environments.Get(ctx, env.ID)
	if err != nil || readEnv.Version != env.Version || readEnv.PlainVars["DB_NAME"] != "restore_fixture" || !bytes.Equal(readEnv.Secrets["RESTORE_PROBE"], sealedEnvironment) {
		t.Fatal("restored environment/version/ciphertext differs")
	}
	readConn, err := (&connection.Postgres{DB: restoredDB}).Get(ctx, source.ID)
	if err != nil || readConn.Version != source.Version || !readConn.Allows("fixture/restore-probe") || !bytes.Equal(readConn.Credential, sealedConnection) {
		t.Fatal("restored connection/configuration/ciphertext differs")
	}
	correct := &connection.Service{Store: &connection.Postgres{DB: restoredDB}, Codec: codec}
	readCredentials, err := correct.OpenCredentials(readConn)
	if err != nil || readCredentials.WebhookSecret != webhook || readCredentials.Token != token {
		t.Fatal("same-key source credential read failed")
	}
	readProbe, err := database.New(restored.Environments, codec).Get(ctx, env.ID, "RESTORE_PROBE")
	if err != nil || string(readProbe) != probe {
		t.Fatal("same-key environment read failed")
	}
	wrongKey, err := cookercrypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	wrongCodec, err := cookercrypto.NewCodec(wrongKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.New(restored.Environments, wrongCodec).Get(ctx, env.ID, "RESTORE_PROBE"); err == nil {
		t.Fatal("wrong key decrypted environment secret")
	}
	wrong := &connection.Service{Store: &connection.Postgres{DB: restoredDB}, Codec: wrongCodec}
	if _, err := wrong.OpenCredentials(readConn); err == nil {
		t.Fatal("wrong key decrypted source credentials")
	}
	if err := wrong.UpdateWithCredentials(ctx, readConn, connection.CredentialsPatch{}); err == nil {
		t.Fatal("wrong key overwrote preserved source ciphertext")
	}
	afterEnv, err := restored.Environments.Get(ctx, env.ID)
	if err != nil || afterEnv.Version != env.Version || !bytes.Equal(afterEnv.Secrets["RESTORE_PROBE"], sealedEnvironment) {
		t.Fatal("failed decryption modified environment ciphertext")
	}
	afterConn, err := correct.Store.Get(ctx, source.ID)
	if err != nil || afterConn.Version != source.Version || !bytes.Equal(afterConn.Credential, sealedConnection) {
		t.Fatal("failed decryption modified source ciphertext")
	}
	if _, err := correct.OpenCredentials(afterConn); err != nil {
		t.Fatal("original key no longer works after wrong-key checks")
	}
	readJob, err := jobqueue.NewPostgres(restoredDB).Get(ctx, job.ID)
	if err != nil || readJob.Status != jobqueue.StatusNeedsAttention || readJob.ConcurrencyKey != job.ConcurrencyKey || readJob.LastError != job.LastError {
		t.Fatal("durable job state/lock evidence lost")
	}
	readDigest, err := jobqueue.PayloadDigest(readJob.Payload)
	if err != nil || digest != readDigest {
		t.Fatal("durable job payload identity changed")
	}
	readRun, err := restored.Runs.Get(ctx, run.ID)
	if err != nil || readRun.Status != run.Status || readRun.StartedByUserSub != run.StartedByUserSub {
		t.Fatal("durable run outcome/actor lost")
	}
	readDeploy, err := restored.AppDeploys.Get(ctx, deploy.ID)
	if err != nil || readDeploy.RunID != run.ID || readDeploy.Status != deploy.Status {
		t.Fatal("deployment history lost")
	}
	restoredLogs := logstore.NewPostgres(restoredDB, logstore.Limits{})
	readEntries, readCursor, err := restoredLogs.History(ctx, run.ID, "deploy-web", 0)
	if err != nil || readCursor != beforeCursor || !reflect.DeepEqual(readEntries, beforeEntries) {
		t.Fatal("restored redacted log contents/cursors differ")
	}
	next, err := restoredLogs.AppendNext(ctx, run.ID, "deploy-web", logstore.Entry{Line: "restored replay verified", TS: time.Now()})
	if err != nil || next.Seq != beforeCursor+1 {
		t.Fatal("restored log cursor failed to continue")
	}
	report["sameKeyEnvironmentAndSourceRead"] = true
	report["wrongKeyReadsRejected"] = true
	report["wrongKeySourceUpdateRejectedWithoutCiphertextChange"] = true
	report["environmentCiphertextUnchangedAfterWrongKeyRead"] = true
	report["migrationLedgerPreserved"] = true
	report["durableJobRunActorAndHistoryPreserved"] = true
	report["redactedLogsAndMonotonicCursorPreserved"] = true
	report["backupArtifactRetained"] = false
	report["encryptionKeyPersistedOrPrinted"] = false
	t.Log("verified same-key reads, wrong-key refusal with unchanged ciphertext, migration ledger, durable job/run/history, redacted log replay and resumed cursor")
}

func restoreDocker(t *testing.T, ctx context.Context, args ...string) string {
	t.Helper()
	out, err := exec.CommandContext(ctx, "docker", args...).Output()
	if err != nil {
		t.Fatalf("restore fixture Docker operation %s failed: %v", args[0], err)
	}
	return string(out)
}

func restoreMigrationVersions(t *testing.T, ctx context.Context, db *sql.DB) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, "SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var versions []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		versions = append(versions, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return versions
}
