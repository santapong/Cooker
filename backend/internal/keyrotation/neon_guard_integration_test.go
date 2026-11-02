//go:build integration

package keyrotation

import (
	"context"
	"testing"
)

func TestNeonSchemaRejectsLegacyFiveFamilyRotationAndUnguardedRewrap(t *testing.T) {
	db := ownedDB(t)
	_, old, _, _ := seedOwnedCiphertext(t, db)
	var count int
	// This is the pre-Neon executable's exact five-table trigger check. It
	// refuses before collect/backup even if the Vercel table has no rows.
	query := `SELECT count(*) FROM pg_trigger WHERE NOT tgisinternal AND tgrelid IN ('public.environments'::regclass,'public.apps'::regclass,'public.source_connections'::regclass,'public.vercel_connections'::regclass,'public.notification_targets'::regclass)`
	if db.QueryRow(query).Scan(&count) != nil || count != 2 {
		t.Fatal("old five-family schema check would not refuse")
	}
	changed, err := old.Seal([]byte("synthetic-new-ciphertext"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE vercel_connections SET credential=$1 WHERE id='disabled-vercel'`, changed); err == nil {
		t.Fatal("absent sixth-family marker accepted rewrap")
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`SET LOCAL cooker.key_rotation_families='5'`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`UPDATE vercel_connections SET credential=$1 WHERE id='disabled-vercel'`, changed); err == nil {
		tx.Rollback()
		t.Fatal("wrong family marker accepted rewrap")
	}
	tx.Rollback()
	// Logical key replacement is a new generation and stays available.
	if _, err = db.Exec(`UPDATE vercel_connections SET credential=$1,version=version+1 WHERE id='disabled-vercel'`, changed); err != nil {
		t.Fatal("logical key replacement was blocked")
	}
	tx, err = db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = verifyTriggers(context.Background(), tx); err != nil {
		tx.Rollback()
		t.Fatal("current exact guard verification failed")
	}
	tx.Rollback()
	execSQL(t, db, `DELETE FROM vercel_connections`)
	if db.QueryRow(query).Scan(&count) != nil || count != 2 {
		t.Fatal("legacy schema refusal depends on Vercel row presence")
	}
}
