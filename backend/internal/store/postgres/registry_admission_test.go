package postgres

import (
	"context"
	"database/sql"
	"testing"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store/memory"
)

func TestRegistryAdmissionExplicitPoolBinding(t *testing.T) {
	queueDB, primaryDB := new(sql.DB), new(sql.DB)
	queue := jobqueue.NewPostgres(queueDB)
	queue.OnEnqueue = SyncRegistryAuthorityAdmission
	for _, tc := range []struct {
		name        string
		registry    *sql.DB
		environment *sql.DB
		want        bool
	}{
		{"queue-bound", queueDB, queueDB, true},
		{"distinct-primary-pool", primaryDB, primaryDB, false},
		{"split-metadata-stores", queueDB, primaryDB, false},
		{"nil-registry-pool", nil, queueDB, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := RegistryAdmissionStoresMatch(queue, NewRegistryConfigStore(tc.registry), NewEnvironmentStore(tc.environment)); got != tc.want {
				t.Fatal("unproven pool binding accepted")
			}
		})
	}
	st := memory.New()
	if RegistryAdmissionStoresMatch(queue, st.Registries, st.Environments) {
		t.Fatal("memory metadata treated as transaction-bound")
	}
	if RegistryAdmissionStoresMatch(jobqueue.NewMemory(), NewRegistryConfigStore(queueDB), NewEnvironmentStore(queueDB)) {
		t.Fatal("memory queue treated as atomic PostgreSQL fence")
	}
	queue.OnEnqueue = nil
	if RegistryAdmissionStoresMatch(queue, NewRegistryConfigStore(queueDB), NewEnvironmentStore(queueDB)) {
		t.Fatal("missing admission hook accepted")
	}
}

func TestRegistryAdmissionCredentialScopeProtocol(t *testing.T) {
	for _, tc := range []struct {
		name, ref, mode, env, key string
		want                      bool
	}{
		{"v2-external", "registry:v2:6a2fc7f1-e326-40f5-ab7d-0b5f9400d663", "external", "registry_6a2fc7f1e32640f5ab7d0b5f9400d663", "password", true},
		{"v2-database", "registry:v2:6a2fc7f1-e326-40f5-ab7d-0b5f9400d663", "database", "registry_6a2fc7f1e32640f5ab7d0b5f9400d663", "password", true},
		{"substituted-scope", "registry:v2:6a2fc7f1-e326-40f5-ab7d-0b5f9400d663", "external", "other", "password", false},
		{"malformed-id", "registry:v2:bad", "external", "other", "password", false},
		{"zero-id", "registry:v2:00000000-0000-0000-0000-000000000000", "external", "registry_00000000000000000000000000000000", "password", false},
		{"legacy-database", "registry:reg", "database", "_registries", "registry_password.reg", true},
		{"legacy-external", "registry:reg", "external", "_registries", "registry_password.reg", false},
		{"legacy-wrong-key", "registry:reg", "database", "_registries", "password.reg", false},
		{"no-credential", "", "none", "", "", true},
		{"missing-pin", "registry:reg", "none", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := registryAdmissionCredentialScope(&model.RegistryConfig{ID: "reg", PasswordRef: tc.ref}, model.RegistryCredentialPin{Mode: tc.mode, EnvironmentID: tc.env, Key: tc.key}); got != tc.want {
				t.Fatal("credential protocol mismatch")
			}
		})
	}
	if err := SyncRegistryAuthorityAdmission(context.Background(), nil, &jobqueue.Job{Kind: "unrelated"}); err != nil {
		t.Fatal("unrelated queue kind changed")
	}
}
