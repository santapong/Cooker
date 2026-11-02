package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/build/registryauth"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/secrets"
	"github.com/santapong/cooker/internal/store"
)

// RegistryAdmissionStoresMatch checks explicit pool binding, not DSN/network
// identity. bootJobQueue opens a separate pool from the primary stores; bind the
// authority's registry/environment stores to that queue pool before enabling it.
// Custom/memory stores and distinct pools fail closed until explicitly adapted.
func RegistryAdmissionStoresMatch(jobs jobqueue.Store, registries store.RegistryConfigStore, environments store.EnvironmentStore) bool {
	queue, ok := jobs.(*jobqueue.Postgres)
	registry, registryOK := registries.(*RegistryConfigStore)
	environment, environmentOK := environments.(*EnvironmentStore)
	return ok && registryOK && environmentOK && queue != nil && queue.OnEnqueue != nil && registry != nil && environment != nil && registry.db != nil && registry.db == environment.db && queue.RegistryAdmissionDatabaseMatches(registry.db)
}

func registryAdmissionConflict() error {
	return fmt.Errorf("registry authority changed before durable admission: %w", store.ErrConflict)
}

// SyncRegistryAuthorityAdmission runs first in the job INSERT transaction, before
// schedule/trigger/publication receipts. It performs metadata-only reads and
// locks selected registry rows, then sorted database credential environments.
// Grants, generations and credential incarnation cannot change before commit.
// Worker/current checks still enforce revocation after admission. No provider
// request, decryption, repinning or ciphertext read occurs under these locks.
func SyncRegistryAuthorityAdmission(ctx context.Context, tx *sql.Tx, j *jobqueue.Job) error {
	if j == nil {
		return registryAdmissionConflict()
	}
	if j.Kind != "app-deploy" && j.Kind != "pipeline-run" {
		return nil
	}
	if tx == nil || j.MaxAttempts != 1 {
		return registryAdmissionConflict()
	}
	var payload struct {
		Version    int                          `json:"version"`
		RunID      string                       `json:"runId"`
		PipelineID string                       `json:"pipelineId"`
		App        *model.App                   `json:"app"`
		Pipeline   *model.Pipeline              `json:"pipeline"`
		Run        *model.PipelineRun           `json:"run"`
		CreatedAt  time.Time                    `json:"createdAt"`
		Registry   *model.RegistryAuthorityPlan `json:"registryAuthority"`
	}
	if json.Unmarshal(j.Payload, &payload) != nil || !secrets.ValidSegment(payload.RunID) || payload.Registry == nil || payload.Registry.Validate() != nil {
		return registryAdmissionConflict()
	}
	plan := payload.Registry
	if j.Kind == "app-deploy" {
		if payload.Version != 1 || payload.CreatedAt.IsZero() || payload.App == nil || payload.Pipeline != nil || payload.Run != nil || plan.Subject != (model.RegistrySubject{Kind: "app", ID: payload.App.ID}) || j.ID != "app-run:"+payload.RunID || j.ConcurrencyKey != "app:"+payload.App.ID || strings.HasPrefix(payload.App.ID, "vapp_") || strings.HasPrefix(payload.App.ID, "vflow_") {
			return registryAdmissionConflict()
		}
		var selected []string
		if payload.App.BuildPlan != nil {
			selected = payload.App.BuildPlan.RegistryConnectionIDs
		}
		if !registryAdmissionSelection(plan, selected) {
			return registryAdmissionConflict()
		}
	} else {
		p, run := payload.Pipeline, payload.Run
		if p == nil || payload.App != nil || run == nil || p.ID != payload.PipelineID || plan.Subject != (model.RegistrySubject{Kind: "pipeline", ID: p.ID}) || j.ID != "run:"+payload.RunID || j.ConcurrencyKey != "pipeline:"+p.ID || run.ID != payload.RunID || run.PipelineID != p.ID || run.PipelineVersion != p.Version || p.Version < 1 || run.Status != model.RunStatusPending || run.CreatedAt.IsZero() || run.StartedAt != nil || run.FinishedAt != nil || strings.HasPrefix(p.ID, "vapp_") || strings.HasPrefix(p.ID, "vflow_") || len(p.Stages) > 256 || len(plan.Stages) != len(p.Stages) {
			return registryAdmissionConflict()
		}
		seen := map[string]bool{}
		for _, stage := range p.Stages {
			if !secrets.ValidSegment(stage.ID) || seen[stage.ID] || stage.Config.ReviewOnly || !model.ValidRegistryIDs(stage.Config.RegistryConnectionIDs, 32) {
				return registryAdmissionConflict()
			}
			seen[stage.ID] = true
			ids, exists := plan.Stages[stage.ID]
			want, got := slices.Clone(stage.Config.RegistryConnectionIDs), slices.Clone(ids)
			sort.Strings(want)
			sort.Strings(got)
			if !exists || !slices.Equal(want, got) || !registryAdmissionHosts(plan, ids) {
				return registryAdmissionConflict()
			}
		}
	}
	ids := make([]string, 0, len(plan.Connections))
	for id := range plan.Connections {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	// Registry locks precede credential environment locks. Settings grant/delete
	// writes take row locks; FOR SHARE deliberately conflicts with those writes.
	for _, id := range ids {
		pin := plan.Connections[id]
		r, err := scanRegistryConfig(tx.QueryRowContext(ctx, `SELECT `+registryConfigColumns+` FROM registry_configs WHERE id=$1 FOR SHARE`, id))
		if err != nil || r == nil || r.Version != pin.Version {
			return registryAdmissionConflict()
		}
		grants := r.AllowedAppIDs
		if plan.Subject.Kind == "pipeline" {
			grants = r.AllowedPipelineIDs
		}
		endpoint, host, err := registryauth.Endpoint(r.URL)
		if err != nil || !slices.Contains(grants, plan.Subject.ID) || endpoint != pin.Endpoint || host != pin.Host || r.Username != pin.Username || !registryAdmissionCredentialScope(r, pin.Credential) {
			return registryAdmissionConflict()
		}
	}
	type envPin struct {
		environment string
		connection  string
	}
	var environmentPins []envPin
	for _, id := range ids {
		pin := plan.Connections[id].Credential
		if pin.Mode == "database" {
			environmentPins = append(environmentPins, envPin{environment: pin.EnvironmentID, connection: id})
		}
	}
	sort.Slice(environmentPins, func(i, k int) bool {
		if environmentPins[i].environment == environmentPins[k].environment {
			return environmentPins[i].connection < environmentPins[k].connection
		}
		return environmentPins[i].environment < environmentPins[k].environment
	})
	for _, item := range environmentPins {
		pin := plan.Connections[item.connection].Credential
		var version int
		var created time.Time
		var present bool
		if err := tx.QueryRowContext(ctx, `SELECT version,created_at,secrets ? $2 FROM environments WHERE id=$1 FOR SHARE`, item.environment, pin.Key).Scan(&version, &created, &present); err != nil || version != pin.EnvironmentVersion || !created.Equal(pin.EnvironmentCreatedAt) || !present {
			return registryAdmissionConflict()
		}
	}
	return nil
}

func registryAdmissionSelection(plan *model.RegistryAuthorityPlan, selected []string) bool {
	if !model.ValidRegistryIDs(selected, 32) || len(selected) != len(plan.Connections) || !registryAdmissionHosts(plan, selected) {
		return false
	}
	for _, id := range selected {
		if _, exists := plan.Connections[id]; !exists {
			return false
		}
	}
	return true
}

func registryAdmissionHosts(plan *model.RegistryAuthorityPlan, ids []string) bool {
	seen := map[string]bool{}
	for _, id := range ids {
		pin, exists := plan.Connections[id]
		if !exists || seen[pin.Host] {
			return false
		}
		seen[pin.Host] = true
	}
	return true
}

// Match only the bounded protocol, never a provider lookup. Legacy references
// require database generation proof; external legacy references are rejected.
func registryAdmissionCredentialScope(r *model.RegistryConfig, pin model.RegistryCredentialPin) bool {
	if r.PasswordRef == "" {
		return pin.Mode == "none"
	}
	if pin.Mode == "none" {
		return false
	}
	if strings.HasPrefix(r.PasswordRef, "registry:v2:") {
		raw := strings.TrimPrefix(r.PasswordRef, "registry:v2:")
		id, err := uuid.Parse(raw)
		return err == nil && id != uuid.Nil && id.String() == raw && pin.EnvironmentID == "registry_"+strings.ReplaceAll(raw, "-", "") && pin.Key == "password"
	}
	return r.PasswordRef == "registry:"+r.ID && pin.Mode == "database" && pin.EnvironmentID == "_registries" && pin.Key == "registry_password."+r.ID
}
