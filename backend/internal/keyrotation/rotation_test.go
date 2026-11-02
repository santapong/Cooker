package keyrotation

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/crypto"
)

func testKeys(t *testing.T) (*crypto.Codec, *crypto.Codec) {
	t.Helper()
	a, _ := crypto.GenerateKey()
	b, _ := crypto.GenerateKey()
	old, e := crypto.NewCodec(a)
	if e != nil {
		t.Fatal(e)
	}
	next, e := crypto.NewCodec(b)
	if e != nil {
		t.Fatal(e)
	}
	return old, next
}

func TestBackupAuthenticatesBindingAndExcludesPlaintext(t *testing.T) {
	old, next := testKeys(t)
	canary := []byte("synthetic-rotation-secret-canary")
	sealed, _ := old.Seal(canary)
	env, _ := json.Marshal(map[string][]byte{"TOKEN": sealed})
	notif, _ := json.Marshal(struct {
		Enc  string `json:"enc"`
		Data []byte `json:"data"`
	}{"v1", sealed})
	rows := []row{{Family: "environments", ID: "internal-orphan", Guard: "g", Before: env, Kind: "encrypted"}, {Family: "apps", ID: "app", Guard: "g", Before: []byte(base64.StdEncoding.EncodeToString(sealed)), Kind: "encrypted"}, {Family: "source_connections", ID: "disabled-source", Guard: "g", Before: sealed, Kind: "encrypted"}, {Family: "vercel_connections", ID: "disabled-vercel", Guard: "g", Before: sealed, Kind: "encrypted"}, {Family: "notification_targets", ID: "disabled-target", Guard: "g", Before: notif, Kind: "encrypted"}, {Family: "notification_targets", ID: "legacy", Guard: "g", Kind: "plaintext_unchanged"}}
	rows = append(rows, row{Family: "neon_connections", ID: "disabled-neon", Guard: "g", Before: sealed, Kind: "encrypted"})
	prepared, counts, plain, err := prepare(rows, old, next)
	if err != nil || plain != 1 || len(counts) != 6 || counts["neon_connections"] != 1 {
		t.Fatal("full inventory failed")
	}
	p := plan{Version: 1, ID: uuid.NewString(), CreatedAt: time.Now().UTC(), Actor: "fixture-operator", OldKeyID: old.KeyID(), NewKeyID: next.KeyID(), Rows: prepared, Counts: counts, PlaintextNotifications: plain}
	path := filepath.Join(t.TempDir(), "ciphertext-backup.json")
	if err := writeBackup(path, p, old, next); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if bytes.Contains(data, canary) {
		t.Fatal("plaintext in backup")
	}
	if _, err := readBackup(path, old, next); err != nil {
		t.Fatal("authenticated backup unreadable")
	}
	if err := writeBackup(path, p, old, next); !errors.Is(err, ErrBackup) {
		t.Fatal("backup overwritten")
	}
	var doc backup
	json.Unmarshal(data, &doc)
	doc.Plan.Rows[0].ID = "different-binding"
	tampered, _ := json.Marshal(doc)
	os.WriteFile(path, tampered, 0600)
	if _, err := readBackup(path, old, next); !errors.Is(err, ErrBackup) {
		t.Fatal("altered record binding accepted")
	}
	os.WriteFile(path, data, 0644)
	os.Chmod(path, 0644)
	if _, err := readBackup(path, old, next); !errors.Is(err, ErrBackup) {
		t.Fatal("public backup accepted")
	}
}

func TestRecoveryClassificationRefusesMixedAndLogicalDrift(t *testing.T) {
	saved := []row{{Family: "apps", ID: "a", Guard: "version-1", Kind: "encrypted", Before: []byte("before-a"), After: []byte("after-a")}, {Family: "apps", ID: "b", Guard: "version-1", Kind: "encrypted", Before: []byte("before-b"), After: []byte("after-b")}}
	current := append([]row(nil), saved...)
	if classify(current, saved) != "before" {
		t.Fatal("before missing")
	}
	current[0].Before = current[0].After
	if classify(current, saved) != "mixed" {
		t.Fatal("mixed missing")
	}
	current[1].Before = current[1].After
	if classify(current, saved) != "after" {
		t.Fatal("after missing")
	}
	current[1].Guard = "version-2"
	if classify(current, saved) != "drift" {
		t.Fatal("generation drift accepted")
	}
	if classify(current[:1], saved) != "drift" {
		t.Fatal("deleted record accepted")
	}
}

func TestNotificationEnvelopeUnknownNeverBecomesPlaintext(t *testing.T) {
	for _, cfg := range []string{`null`, `[]`, `{"enc":"v2","data":"YQ=="}`, `{"enc":"v1"}`, `{"data":"YQ=="}`, `{"Enc":"v1","data":"YQ=="}`, `{"enc":"v1","data":null}`} {
		if _, _, err := notificationData([]byte(cfg)); !errors.Is(err, ErrCiphertext) {
			t.Fatalf("malformed envelope accepted: %s", cfg)
		}
	}
	if _, plain, err := notificationData([]byte(`{"url":"legacy-secret"}`)); err != nil || !plain {
		t.Fatal("legacy config misclassified")
	}
}

func TestCommandRejectsInputWithoutEchoingSecrets(t *testing.T) {
	for _, input := range []string{`{"databaseUrl":"postgres://PRIVATE","unexpected":"SECRET"}`, `{"databaseUrl":"PRIVATE"} {"oldKey":"SECRET"}`, `not-json-SECRET`} {
		var out bytes.Buffer
		if RunCommand(context.Background(), []string{"rotate"}, bytes.NewBufferString(input), &out) != 2 {
			t.Fatal("invalid input accepted")
		}
		if bytes.Contains(out.Bytes(), []byte("PRIVATE")) || bytes.Contains(out.Bytes(), []byte("SECRET")) {
			t.Fatal("input leaked")
		}
	}
}
