// Package keyrotation performs explicitly offline, atomic rewrapping of all
// Cooker-managed database ciphertext. It never changes configuration versions.
package keyrotation

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/crypto"
)

const maxBackupBytes = 64 << 20
const maxRows = 100000

var (
	ErrInput         = errors.New("rotation: invalid input")
	ErrOffline       = errors.New("rotation: stop every Cooker process and resolve active work before rotation")
	ErrDatabase      = errors.New("rotation: database unavailable or schema unsupported")
	ErrCiphertext    = errors.New("rotation: ciphertext authentication or format unavailable")
	ErrBackup        = errors.New("rotation: backup unavailable, unsafe, invalid or unauthenticated")
	ErrDrift         = errors.New("rotation: database differs from authenticated backup; no records changed")
	ErrCommitUnknown = errors.New("rotation: commit result uncertain; use status with this backup before any retry")
)

// Input is read only from private stdin by the CLI. Keys and database URLs must
// never be command-line arguments or included in result/error text.
type Input struct {
	DatabaseURL string `json:"databaseUrl"`
	OldKey      string `json:"oldKey"`
	NewKey      string `json:"newKey"`
	BackupPath  string `json:"backupPath"`
	Actor       string `json:"actor"`
	Offline     bool   `json:"offline"`
}

type Report struct {
	Operation              string         `json:"operation"`
	State                  string         `json:"state"`
	RotationID             string         `json:"rotationId,omitempty"`
	Actor                  string         `json:"actor,omitempty"`
	OldKeyID               string         `json:"oldKeyId,omitempty"`
	NewKeyID               string         `json:"newKeyId,omitempty"`
	Counts                 map[string]int `json:"encryptedValues"`
	PlaintextNotifications int            `json:"plaintextNotificationsUnchanged"`
	ExternalSecrets        string         `json:"externalSecrets"`
}

// Only ciphertext and non-secret identity/generation digests enter the backup.
// Legacy plaintext notification config is excluded altogether.
type row struct {
	Family string `json:"family"`
	ID     string `json:"id"`
	Guard  string `json:"guard"`
	Kind   string `json:"kind"`
	Null   bool   `json:"null,omitempty"`
	Before []byte `json:"before,omitempty"`
	After  []byte `json:"after,omitempty"`
}
type plan struct {
	Version                int            `json:"version"`
	ID                     string         `json:"id"`
	CreatedAt              time.Time      `json:"createdAt"`
	Actor                  string         `json:"actor"`
	OldKeyID               string         `json:"oldKeyId"`
	NewKeyID               string         `json:"newKeyId"`
	Rows                   []row          `json:"rows"`
	Counts                 map[string]int `json:"encryptedValues"`
	PlaintextNotifications int            `json:"plaintextNotificationsUnchanged"`
}
type backup struct {
	Plan   plan   `json:"plan"`
	OldMAC []byte `json:"oldMac"`
	NewMAC []byte `json:"newMac"`
}
type family struct {
	table, column string
	json          bool
}

var families = []family{
	{"environments", "secrets", true},
	{"apps", "webhook_secret", false},
	{"source_connections", "credential", false},
	{"vercel_connections", "credential", false},
	{"neon_connections", "credential", false},
	{"notification_targets", "config", true},
}

// Execute uses one database connection. Callers must have stopped all Cooker
// processes; checking other connections cannot stop a new process being started.
// No schema migrations, provider requests, or automatic retry are performed.
func Execute(ctx context.Context, db *sql.DB, operation string, in Input) (Report, error) {
	r := Report{Operation: operation, Counts: map[string]int{}, ExternalSecrets: "not_rotated"}
	if db == nil {
		return r, ErrInput
	}
	if operation != "inventory" && operation != "rotate" && operation != "status" && operation != "restore" {
		return r, ErrInput
	}
	old, err := crypto.NewCodec(in.OldKey)
	if err != nil || !old.Active() {
		return r, ErrInput
	}
	var next *crypto.Codec
	if operation != "inventory" {
		next, err = crypto.NewCodec(in.NewKey)
		if err != nil || !next.Active() || old.KeyID() == next.KeyID() || !filepath.IsAbs(in.BackupPath) {
			return r, ErrInput
		}
	}
	if operation == "rotate" || operation == "restore" {
		if !in.Offline || !safeActor(in.Actor) {
			return r, ErrOffline
		}
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: operation == "inventory" || operation == "status"})
	if err != nil {
		return r, ErrDatabase
	}
	defer tx.Rollback()
	if operation == "rotate" || operation == "restore" {
		if err := lockOffline(ctx, tx); err != nil {
			return r, err
		}
		if _, err := tx.ExecContext(ctx, `SET LOCAL cooker.key_rotation_families='6'`); err != nil {
			return r, ErrDatabase
		}
	} else {
		if err := verifyTriggers(ctx, tx); err != nil {
			return r, err
		}
	}
	current, err := collect(ctx, tx)
	if err != nil {
		return r, err
	}
	if operation == "inventory" {
		_, counts, plain, err := prepare(current, old, old)
		if err != nil {
			return r, err
		}
		r.Counts, r.PlaintextNotifications, r.OldKeyID, r.State = counts, plain, old.KeyID(), "verified_inventory"
		return r, nil
	}
	var p plan
	if operation == "rotate" {
		prepared, counts, plain, err := prepare(current, old, next)
		if err != nil {
			return r, err
		}
		total := 0
		for _, count := range counts {
			total += count
		}
		if total == 0 {
			r.State, r.Counts, r.PlaintextNotifications = "no_encrypted_values", counts, plain
			return r, nil
		}
		p = plan{Version: 1, ID: uuid.NewString(), CreatedAt: time.Now().UTC(), Actor: in.Actor, OldKeyID: old.KeyID(), NewKeyID: next.KeyID(), Rows: prepared, Counts: counts, PlaintextNotifications: plain}
		if err := writeBackup(in.BackupPath, p, old, next); err != nil {
			return r, err
		}
	} else {
		p, err = readBackup(in.BackupPath, old, next)
		if err != nil {
			return r, err
		}
	}
	r.RotationID, r.Actor, r.OldKeyID, r.NewKeyID = p.ID, p.Actor, p.OldKeyID, p.NewKeyID
	r.Counts, r.PlaintextNotifications = p.Counts, p.PlaintextNotifications
	state := classify(current, p.Rows)
	r.State = state
	if operation == "status" {
		return r, nil
	}
	if operation == "restore" && state == "before" {
		r.State = "already_restored"
		return r, nil
	}
	if (operation == "rotate" && state != "before") || (operation == "restore" && state != "after") {
		return r, ErrDrift
	}
	if err := apply(ctx, tx, p.Rows, operation == "restore"); err != nil {
		return r, err
	}
	observed, err := collect(ctx, tx)
	if err != nil {
		return r, err
	}
	expected := "after"
	if operation == "restore" {
		expected = "before"
	}
	if classify(observed, p.Rows) != expected {
		return r, ErrDrift
	}
	if err := tx.Commit(); err != nil {
		r.State = "commit_uncertain"
		return r, ErrCommitUnknown
	}
	r.State = "rotated"
	if operation == "restore" {
		r.State = "restored"
		r.Actor = in.Actor
	}
	return r, nil
}

func safeActor(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if c < 32 || c > 126 {
			return false
		}
	}
	return true
}

func lockOffline(ctx context.Context, tx *sql.Tx) error {
	// Lock timeouts bound refusal; table locks cover every encrypted family and
	// worker/sync/outbox claims for the duration of the atomic rewrap.
	if _, err := tx.ExecContext(ctx, `SET LOCAL lock_timeout = '3s'`); err != nil {
		return ErrDatabase
	}
	var other int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND backend_type='client backend'`).Scan(&other); err != nil {
		return ErrDatabase
	}
	if other != 0 {
		return ErrOffline
	}
	if _, err := tx.ExecContext(ctx, `LOCK TABLE public.jobs, public.pipeline_runs, public.notification_deliveries, public.environments, public.apps, public.source_connections, public.vercel_connections, public.neon_connections, public.notification_targets IN ACCESS EXCLUSIVE MODE`); err != nil {
		return ErrDatabase
	}
	var active bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE status='running') OR EXISTS(SELECT 1 FROM pipeline_runs WHERE status='running') OR EXISTS(SELECT 1 FROM notification_deliveries WHERE state='sending') OR EXISTS(SELECT 1 FROM vercel_connections WHERE sync_lease IS NOT NULL) OR EXISTS(SELECT 1 FROM neon_connections WHERE sync_lease IS NOT NULL)`).Scan(&active); err != nil {
		return ErrDatabase
	}
	if active {
		return ErrOffline
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND backend_type='client backend'`).Scan(&other); err != nil {
		return ErrDatabase
	}
	if other != 0 {
		return ErrOffline
	}
	return verifyTriggers(ctx, tx)
}

func verifyTriggers(ctx context.Context, tx *sql.Tx) error {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM pg_trigger WHERE NOT tgisinternal AND tgrelid IN ('public.environments'::regclass,'public.apps'::regclass,'public.source_connections'::regclass,'public.vercel_connections'::regclass,'public.neon_connections'::regclass,'public.notification_targets'::regclass)`).Scan(&count); err != nil || count != 2 {
		return ErrDatabase
	}
	var valid bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_trigger t JOIN pg_proc p ON p.oid=t.tgfoid WHERE t.tgrelid='public.notification_targets'::regclass AND t.tgname='notification_target_version' AND t.tgenabled='O' AND t.tgtype=19 AND t.tgnargs=0 AND NOT t.tgisinternal AND t.tgqual IS NULL AND t.tgattr=''::int2vector AND t.tgconstraint=0 AND NOT t.tgdeferrable AND NOT t.tginitdeferred AND p.oid='public.cooker_notification_target_version()'::regprocedure AND p.proconfig IS NULL AND NOT p.prosecdef AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql') AND p.prorettype='trigger'::regtype AND p.prokind='f' AND p.pronargs=0 AND p.provolatile='v' AND NOT p.proisstrict AND NOT p.proleakproof AND p.proparallel='u' AND regexp_replace(p.prosrc,'\s','','g')=$1)`, "BEGINNEW.version:=nextval('notification_target_versions');RETURNNEW;END;").Scan(&valid); err != nil || !valid {
		return ErrDatabase
	}
	const guardBody = "BEGINIFNEW.credentialISDISTINCTFROMOLD.credentialANDNEW.version=OLD.versionANDcurrent_setting('cooker.key_rotation_families',true)ISDISTINCTFROM'6'THENRAISEEXCEPTION'six-familyofflinecredentialrewraprequired';ENDIF;RETURNNEW;END;"
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_trigger t JOIN pg_proc p ON p.oid=t.tgfoid WHERE t.tgrelid='public.vercel_connections'::regclass AND t.tgname='vercel_credential_rewrap_guard' AND t.tgenabled='O' AND t.tgtype=19 AND t.tgnargs=0 AND NOT t.tgisinternal AND t.tgqual IS NULL AND t.tgattr=''::int2vector AND t.tgconstraint=0 AND NOT t.tgdeferrable AND NOT t.tginitdeferred AND p.oid='public.cooker_vercel_credential_rewrap_guard()'::regprocedure AND p.proconfig IS NULL AND NOT p.prosecdef AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql') AND p.prorettype='trigger'::regtype AND p.prokind='f' AND p.pronargs=0 AND p.provolatile='v' AND NOT p.proisstrict AND NOT p.proleakproof AND p.proparallel='u' AND regexp_replace(p.prosrc,'\s','','g')=$1)`, guardBody).Scan(&valid); err != nil || !valid {
		return ErrDatabase
	}
	return nil
}

func collect(ctx context.Context, tx *sql.Tx) ([]row, error) {
	var out []row
	total := 0
	for _, f := range families {
		column := f.column
		if f.json {
			column += "::text"
		}
		q := "SELECT t.id, (to_jsonb(t)-'" + f.column + "')::text, " + column + " FROM public." + f.table + " t ORDER BY t.id"
		rs, err := tx.QueryContext(ctx, q)
		if err != nil {
			return nil, ErrDatabase
		}
		for rs.Next() {
			var id string
			var metadata, cell []byte
			if err := rs.Scan(&id, &metadata, &cell); err != nil {
				rs.Close()
				return nil, ErrDatabase
			}
			total += len(metadata) + len(cell) + len(id)
			if total > maxBackupBytes/4 || len(out) >= maxRows {
				rs.Close()
				return nil, ErrDatabase
			}
			digest := sha256.Sum256(metadata)
			r := row{Family: f.table, ID: id, Guard: hex.EncodeToString(digest[:]), Before: cell, Null: cell == nil, Kind: "encrypted"}
			if f.table == "notification_targets" {
				_, plain, err := notificationData(cell)
				if err != nil {
					rs.Close()
					return nil, err
				}
				if plain {
					r.Kind = "plaintext_unchanged"
					r.Before = nil
				}
			}
			out = append(out, r)
		}
		err = rs.Err()
		rs.Close()
		if err != nil {
			return nil, ErrDatabase
		}
	}
	return out, nil
}

// notificationData rejects unknown or malformed envelope-like config. Ordinary
// legacy objects remain unchanged and never enter the ciphertext backup.
func notificationData(cell []byte) ([]byte, bool, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(cell, &fields); err != nil || fields == nil {
		return nil, false, ErrCiphertext
	}
	_, enc := fields["enc"]
	_, data := fields["data"]
	for k := range fields {
		if (strings.EqualFold(k, "enc") && k != "enc") || (strings.EqualFold(k, "data") && k != "data") {
			return nil, false, ErrCiphertext
		}
	}
	if !enc && !data {
		return nil, true, nil
	}
	if !enc || !data || len(fields) != 2 {
		return nil, false, ErrCiphertext
	}
	var envelope struct {
		Enc  string `json:"enc"`
		Data []byte `json:"data"`
	}
	if err := json.Unmarshal(cell, &envelope); err != nil || envelope.Enc != "v1" || len(envelope.Data) == 0 {
		return nil, false, ErrCiphertext
	}
	return envelope.Data, false, nil
}

func prepare(rows []row, old, next *crypto.Codec) ([]row, map[string]int, int, error) {
	out := make([]row, len(rows))
	copy(out, rows)
	counts := map[string]int{}
	for _, f := range families {
		counts[f.table] = 0
	}
	plain := 0
	rewrap := func(b []byte) ([]byte, error) {
		pt, err := old.Open(b)
		if err != nil {
			return nil, ErrCiphertext
		}
		defer clear(pt)
		ct, err := next.SealVersioned(pt)
		if err != nil {
			return nil, ErrCiphertext
		}
		return ct, nil
	}
	for i := range out {
		r := &out[i]
		if r.Kind == "plaintext_unchanged" {
			plain++
			continue
		}
		if r.Null {
			if r.Family != "apps" {
				return nil, nil, 0, ErrCiphertext
			}
			continue
		}
		switch r.Family {
		case "apps":
			if len(r.Before) == 0 {
				continue
			}
			sealed, err := base64.StdEncoding.DecodeString(string(r.Before))
			if err != nil {
				return nil, nil, 0, ErrCiphertext
			}
			ct, err := rewrap(sealed)
			if err != nil {
				return nil, nil, 0, err
			}
			r.After = []byte(base64.StdEncoding.EncodeToString(ct))
			counts[r.Family]++
		case "environments":
			var cells map[string][]byte
			if err := json.Unmarshal(r.Before, &cells); err != nil || cells == nil {
				return nil, nil, 0, ErrCiphertext
			}
			for key, value := range cells {
				ct, err := rewrap(value)
				if err != nil {
					return nil, nil, 0, err
				}
				cells[key] = ct
				counts[r.Family]++
			}
			var err error
			r.After, err = json.Marshal(cells)
			if err != nil {
				return nil, nil, 0, ErrCiphertext
			}
		case "notification_targets":
			data, _, err := notificationData(r.Before)
			if err != nil {
				return nil, nil, 0, err
			}
			ct, err := rewrap(data)
			if err != nil {
				return nil, nil, 0, err
			}
			r.After, _ = json.Marshal(struct {
				Enc  string `json:"enc"`
				Data []byte `json:"data"`
			}{"v1", ct})
			counts[r.Family]++
		default:
			ct, err := rewrap(r.Before)
			if err != nil {
				return nil, nil, 0, err
			}
			r.After = ct
			counts[r.Family]++
		}
	}
	return out, counts, plain, nil
}

func classify(current, saved []row) string {
	if len(current) != len(saved) {
		return "drift"
	}
	before, after := true, true
	for i, c := range current {
		s := saved[i]
		if c.Family != s.Family || c.ID != s.ID || c.Guard != s.Guard || c.Kind != s.Kind || c.Null != s.Null {
			return "drift"
		}
		if c.Kind == "plaintext_unchanged" || c.Null {
			continue
		}
		equal := bytes.Equal
		if c.Family == "environments" || c.Family == "notification_targets" {
			equal = jsonEqual
		}
		b, a := equal(c.Before, s.Before), equal(c.Before, s.After)
		if !b && !a {
			return "drift"
		}
		before = before && b
		after = after && a
	}
	if before {
		return "before"
	}
	if after {
		return "after"
	}
	return "mixed"
}

func jsonEqual(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	ax, _ := json.Marshal(x)
	by, _ := json.Marshal(y)
	return bytes.Equal(ax, by)
}

func apply(ctx context.Context, tx *sql.Tx, rows []row, restore bool) error {
	// This trigger changes credential authority on every UPDATE. The verified
	// offline transaction disables only this trigger and restores it before commit.
	if _, err := tx.ExecContext(ctx, `ALTER TABLE public.notification_targets DISABLE TRIGGER notification_target_version`); err != nil {
		return ErrDatabase
	}
	for _, r := range rows {
		if r.Kind == "plaintext_unchanged" || r.Null || (r.Family == "apps" && len(r.Before) == 0) {
			continue
		}
		var f *family
		for i := range families {
			if families[i].table == r.Family {
				f = &families[i]
				break
			}
		}
		if f == nil {
			return ErrBackup
		}
		cell := r.After
		if restore {
			cell = r.Before
		}
		q := "UPDATE public." + f.table + " SET " + f.column + "=$1 WHERE id=$2"
		var param any = cell
		if f.json || f.table == "apps" {
			param = string(cell)
		}
		result, err := tx.ExecContext(ctx, q, param, r.ID)
		if err != nil {
			return ErrDatabase
		}
		count, err := result.RowsAffected()
		if err != nil || count != 1 {
			return ErrDrift
		}
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE public.notification_targets ENABLE TRIGGER notification_target_version`); err != nil {
		return ErrDatabase
	}
	return verifyTriggers(ctx, tx)
}

func writeBackup(path string, p plan, old, next *crypto.Codec) error {
	data, err := json.Marshal(p)
	if err != nil {
		return ErrBackup
	}
	doc, err := json.Marshal(backup{p, old.RotationManifestMAC(data), next.RotationManifestMAC(data)})
	if err != nil || len(doc) > maxBackupBytes {
		return ErrBackup
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return ErrBackup
	}
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return ErrBackup
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		f.Close()
		return ErrBackup
	}
	// A failed write is deliberately retained and never treated as a valid plan.
	_, writeErr := f.Write(doc)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return ErrBackup
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return ErrBackup
	}
	err = dir.Sync()
	closeErr = dir.Close()
	if err != nil || closeErr != nil {
		return ErrBackup
	}
	return nil
}

func readBackup(path string, old, next *crypto.Codec) (plan, error) {
	var zero plan
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > maxBackupBytes {
		return zero, ErrBackup
	}
	f, err := os.Open(path)
	if err != nil {
		return zero, ErrBackup
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) || opened.Mode().Perm() != 0600 || opened.Size() > maxBackupBytes {
		return zero, ErrBackup
	}
	d := json.NewDecoder(io.LimitReader(f, maxBackupBytes+1))
	d.DisallowUnknownFields()
	var b backup
	if d.Decode(&b) != nil {
		return zero, ErrBackup
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return zero, ErrBackup
	}
	p := b.Plan
	if p.Version != 1 || uuid.Validate(p.ID) != nil || !safeActor(p.Actor) || p.OldKeyID != old.KeyID() || p.NewKeyID != next.KeyID() || len(p.Rows) > maxRows {
		return zero, ErrBackup
	}
	data, err := json.Marshal(p)
	if err != nil || !hmac.Equal(b.OldMAC, old.RotationManifestMAC(data)) || !hmac.Equal(b.NewMAC, next.RotationManifestMAC(data)) {
		return zero, ErrBackup
	}
	return p, nil
}
