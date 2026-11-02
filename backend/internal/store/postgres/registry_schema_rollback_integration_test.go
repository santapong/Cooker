//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/santapong/cooker/internal/jobqueue"
)

// Schema reversal is separate from application rollback. A reversal must not
// erase live grants or accepted registry pins, or reuse a prior generation.
func TestRegistryAuthoritySchemaRollbackPreservesAuthority(t *testing.T) {
	for _, scenario := range []string{"explicit-grants", "accepted-pin-after-revocation", "accepted-case-variant", "accepted-simple-fold-variant"} {
		t.Run(scenario, func(t *testing.T) {
			db, _ := registryAdmissionTestDB(t)
			ctx := context.Background()
			registry, _, payload, opts := registryAdmissionFixture(t, db, "pipeline-run", "none")
			if scenario != "explicit-grants" {
				if scenario == "accepted-case-variant" || scenario == "accepted-simple-fold-variant" {
					authorityKey, connectionsKey := "RegistryAuthority", "Connections"
					if scenario == "accepted-simple-fold-variant" {
						authorityKey, connectionsKey = "regiſtryAuthority", "connectionſ"
					}
					encoded, err := json.Marshal(payload["registryAuthority"])
					if err != nil {
						t.Fatal(err)
					}
					var authority map[string]any
					if err := json.Unmarshal(encoded, &authority); err != nil {
						t.Fatal(err)
					}
					authority[connectionsKey] = authority["connections"]
					delete(authority, "connections")
					payload[authorityKey] = authority
					delete(payload, "registryAuthority")
				}
				data, err := json.Marshal(payload)
				if err != nil {
					t.Fatal(err)
				}
				queue := jobqueue.NewPostgres(db)
				queue.OnEnqueue = SyncRegistryAuthorityAdmission
				if _, err := queue.Enqueue(ctx, "pipeline-run", data, opts); err != nil {
					t.Fatal(err)
				}
				if _, err := NewRegistryConfigStore(db).UpdateGrants(ctx, registry.ID, registry.Version, nil, nil); err != nil {
					t.Fatal(err)
				}
			}
			var beforeRegistry, beforeJobs, beforeMigrations string
			read := func(registryJSON, jobsJSON, migrationsJSON *string) {
				t.Helper()
				if err := db.QueryRow(`SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY id),'[]'::jsonb)::text FROM registry_configs r`).Scan(registryJSON); err != nil {
					t.Fatal(err)
				}
				if err := db.QueryRow(`SELECT COALESCE(jsonb_agg(to_jsonb(j) ORDER BY id),'[]'::jsonb)::text FROM jobs j`).Scan(jobsJSON); err != nil {
					t.Fatal(err)
				}
				if err := db.QueryRow(`SELECT COALESCE(jsonb_agg(to_jsonb(m) ORDER BY version),'[]'::jsonb)::text FROM schema_migrations m`).Scan(migrationsJSON); err != nil {
					t.Fatal(err)
				}
			}
			read(&beforeRegistry, &beforeJobs, &beforeMigrations)
			err := rollbackMigration(ctx, db, "038_registry_authority")
			var afterRegistry, afterJobs, afterMigrations string
			read(&afterRegistry, &afterJobs, &afterMigrations)
			if err == nil {
				t.Error("UNSAFE_SCHEMA_ROLLBACK: authority-bearing schema reversal was accepted")
			}
			if beforeRegistry != afterRegistry || beforeJobs != afterJobs || beforeMigrations != afterMigrations {
				t.Error("UNSAFE_SCHEMA_ROLLBACK: registry authority, accepted jobs or migration records changed")
			}
		})
	}
}

func TestRegistryAuthoritySchemaRollbackLegacyGenerationNeverReused(t *testing.T) {
	db, _ := registryAdmissionTestDB(t)
	ctx := context.Background()
	if _, err := db.Exec(`INSERT INTO registry_configs(id,name,url,password_ref) VALUES ('legacy','legacy','ghcr.io','registry:legacy')`); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		if _, err := db.Exec(`UPDATE registry_configs SET name='legacy' WHERE id='legacy'`); err != nil {
			t.Fatal(err)
		}
	}
	var before int64
	if err := db.QueryRow(`SELECT version FROM registry_configs WHERE id='legacy'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := rollbackMigration(ctx, db, "038_registry_authority"); err != nil {
		t.Fatal("ungranted legacy-only reversal must remain supported", err)
	}
	var ref string
	if err := db.QueryRow(`SELECT password_ref FROM registry_configs WHERE id='legacy'`).Scan(&ref); err != nil || ref != "registry:legacy" {
		t.Fatal("legacy credential reference changed", err)
	}
	if err := applyMigrations(ctx, db); err != nil {
		t.Fatal(err)
	}
	var after int64
	if err := db.QueryRow(`SELECT version FROM registry_configs WHERE id='legacy'`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after <= before {
		t.Errorf("UNSAFE_SCHEMA_ROLLBACK: generation reused or regressed: before=%d after=%d", before, after)
	}
	r, err := NewRegistryConfigStore(db).Get(ctx, "legacy")
	if err != nil || r.PasswordRef != ref || len(r.AllowedAppIDs) != 0 || len(r.AllowedPipelineIDs) != 0 {
		t.Fatal("re-upgrade changed credential reference or invented grants", err)
	}
}
