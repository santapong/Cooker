package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/source/connection"
	"github.com/santapong/cooker/internal/source/gittransport"
	"github.com/santapong/cooker/internal/store"
	"maps"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
)

// SyncPipelineTriggerAdmission runs in the same transaction as native job INSERT.
// Lock order: rule -> intent -> pipeline -> connection -> sorted environments.
// Only bounded metadata is decoded; no provider request/secret read occurs here.
func SyncPipelineTriggerAdmission(ctx context.Context, tx *sql.Tx, j *jobqueue.Job) error {
	var p struct {
		PipelineID string                      `json:"pipelineId"`
		RunID      string                      `json:"runId"`
		Pipeline   *model.Pipeline             `json:"pipeline"`
		Run        *model.PipelineRun          `json:"run"`
		Source     *model.PipelineSourcePlan   `json:"source"`
		Trigger    *model.PipelineTriggerProof `json:"trigger"`
	}
	if err := json.Unmarshal(j.Payload, &p); err != nil {
		return err
	}
	if p.Trigger == nil {
		return nil
	}
	if p.Source == nil || gittransport.ValidateCommit(p.Source.CommitSHA) != nil || p.Source.ObjectSHA != "" && gittransport.ValidateCommit(p.Source.ObjectSHA) != nil || len(p.Source.PlanHash) != 64 || len(p.Source.EnvelopeHash) != 64 || len(p.Source.InputFiles) == 0 || len(p.Source.InputFiles) > 384 || p.Source.EnvironmentVersions == nil || len(p.Source.EnvironmentVersions) > 100 || !strings.HasPrefix(p.Source.FullRef, "refs/heads/") && !strings.HasPrefix(p.Source.FullRef, "refs/tags/") {
		return fmt.Errorf("invalid native source plan: %w", store.ErrConflict)
	}
	for index, input := range p.Source.InputFiles {
		if input.Path == "" || len(input.Path) > 1024 || filepath.IsAbs(input.Path) || strings.ContainsAny(input.Path, "\x00\r\n\\:") || (filepath.Clean(input.Path) == ".." || strings.HasPrefix(filepath.ToSlash(filepath.Clean(input.Path)), "../")) || input.Path != filepath.ToSlash(filepath.Clean(input.Path)) || index > 0 && p.Source.InputFiles[index-1].Path >= input.Path || input.Present && len(input.SHA256) != 64 || !input.Present && input.SHA256 != "" {
			return fmt.Errorf("invalid native recipe inputs: %w", store.ErrConflict)
		}
		if input.Present {
			if _, err := hex.DecodeString(input.SHA256); err != nil {
				return err
			}
		}
	}
	recipeBytes, _ := json.Marshal(p.Source.InputFiles)
	recipeHash := sha256.Sum256(recipeBytes)
	if hex.EncodeToString(recipeHash[:]) != p.Source.EnvelopeHash {
		return fmt.Errorf("native recipe digest changed: %w", store.ErrConflict)
	}
	conflict := func() error {
		return fmt.Errorf("native trigger changed before queue admission: %w", store.ErrConflict)
	}
	if j.Kind != "pipeline-run" || p.Pipeline == nil || p.Run == nil || p.Source == nil || p.Source.Version != 1 || j.ID != "run:"+p.RunID || j.ConcurrencyKey != "pipeline:"+p.PipelineID || p.Pipeline.ID != p.PipelineID || p.Run.ID != p.RunID || p.Run.PipelineID != p.PipelineID || p.Run.PipelineVersion != p.Pipeline.Version || p.Run.Status != model.RunStatusPending || p.Run.StartedAt != nil || p.Run.FinishedAt != nil || p.Run.CreatedAt.IsZero() {
		return conflict()
	}
	r, err := scanTriggerRule(tx.QueryRowContext(ctx, `SELECT body,version,created_at,updated_at FROM trigger_rules WHERE id=$1 FOR SHARE`, p.Trigger.RuleID))
	if err != nil {
		return err
	}
	i, err := scanTriggerIntent(tx.QueryRowContext(ctx, `SELECT `+triggerIntentColumns+` FROM trigger_intents WHERE id=$1 FOR UPDATE`, p.Trigger.IntentID))
	if err != nil {
		return err
	}
	if matches, _ := r.Matches(i.Event); !matches {
		return conflict()
	}
	kind, id := r.Target()
	ikind, iid := i.Rule.Target()
	sp := p.Source
	// Branch events bind their exact typed ref and commit; adapters need not
	// repeat that commit as an ObjectSHA. Tags additionally bind the original
	// object, preventing a different annotated tag from reusing its peeled commit.
	tagObjectChanged := strings.HasPrefix(i.Event.FullRef, "refs/tags/") && (i.Event.ObjectSHA == "" || i.Event.ObjectSHA != sp.ObjectSHA)
	if !r.Enabled || kind != "pipeline" || id != p.PipelineID || ikind != kind || iid != id || i.Status != model.TriggerPending || i.Rule.ID != r.ID || i.Rule.Version != r.Version || p.Trigger.RuleVersion != r.Version || p.Trigger.TargetKind != kind || p.Trigger.TargetID != id || i.RunID != p.RunID || i.Event.CommitSHA != sp.CommitSHA || tagObjectChanged || i.Event.FullRef != sp.FullRef || !i.Event.Source.SameRepository(sp.Source) || !r.Source.SameRepository(sp.Source) || r.Source.Repository != sp.Source.Repository || r.PipelineVersion != p.Pipeline.Version || sp.PipelineID != p.PipelineID || sp.PipelineVersion != p.Pipeline.Version || r.SourceVersion != sp.ConnectionVersion || r.PlanHash != sp.PlanHash || i.Rule.PlanHash != r.PlanHash || r.ApprovedEnvelopeHash != sp.EnvelopeHash || i.Rule.ApprovedEnvelopeHash != r.ApprovedEnvelopeHash || p.Trigger.EnvelopeHash != sp.EnvelopeHash || !maps.Equal(r.EnvironmentVersions, sp.EnvironmentVersions) || sp.ReviewPolicy != r.Approval || r.ReviewedBySubject == "" || r.ReviewedAt == nil || r.ReviewedAt.IsZero() {
		return conflict()
	}
	if r.ActorSubject != "cooker:trigger:"+r.ID || p.Run.StartedByUserSub != r.ActorSubject || p.Run.StartedByEmail != "automation:"+r.ID || len(r.ActorGroups) != 0 || len(p.Run.StartedByGroups) != 0 || p.Run.StartedByTokenHash != "" {
		return conflict()
	}
	if r.Approval != "automatic" {
		review := i.Review
		if review == nil || review.ReviewerSubject == "" || review.ReviewedAt.IsZero() || review.IntentVersion != i.Version-1 || review.RuleVersion != r.Version || review.PipelineVersion != r.PipelineVersion || review.CommitSHA != sp.CommitSHA || review.ObjectSHA != sp.ObjectSHA || review.PlanHash != sp.PlanHash || review.EnvelopeHash != sp.EnvelopeHash {
			return conflict()
		}
	}
	digest, err := model.PipelineSourcePlanDigest(sp)
	if err != nil || digest != p.Trigger.SourcePlanDigest {
		return conflict()
	}
	current, err := scanPipeline(tx.QueryRowContext(ctx, `SELECT id,name,description,stages,edges,variables,source,created_at,updated_at,version,run_deadline FROM pipelines WHERE id=$1 FOR SHARE`, p.PipelineID))
	if err != nil {
		return err
	}
	if current.Source == nil || current.Version != r.PipelineVersion || model.PipelineSourceConfigDigest(current, sp) != sp.PlanHash || model.PipelineSourceConfigDigest(p.Pipeline, sp) != sp.PlanHash || current.Source.ConnectionID != sp.Source.ConnectionID || current.Source.Repository != sp.Source.Repository {
		return conflict()
	}
	var config []byte
	var version int
	if err = tx.QueryRowContext(ctx, `SELECT configuration,version FROM source_connections WHERE id=$1 FOR SHARE`, sp.Source.ConnectionID).Scan(&config, &version); err != nil {
		return err
	}
	var c connection.Connection
	if err = json.Unmarshal(config, &c); err != nil {
		return err
	}
	origin, err := url.Parse(c.Origin)
	if err != nil || version != sp.ConnectionVersion || !c.Allows(sp.Source.Repository) || c.Provider != sp.Source.Provider || origin.Host != sp.Source.Host {
		return conflict()
	}
	ids := map[string]bool{}
	for _, stage := range current.Stages {
		if stage.EnvironmentID != "" {
			ids[stage.EnvironmentID] = true
		}
	}
	if len(ids) != len(sp.EnvironmentVersions) {
		return conflict()
	}
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	for _, id := range ordered {
		if model.IsRegistryCredentialEnvironmentID(id) {
			return conflict()
		}
		expected, ok := sp.EnvironmentVersions[id]
		if !ok || expected < 0 {
			return conflict()
		}
		var env model.Environment
		if err = tx.QueryRowContext(ctx, `SELECT id,name,version FROM environments WHERE id=$1 FOR SHARE`, id).Scan(&env.ID, &env.Name, &env.Version); err != nil {
			return err
		}
		if env.Version != expected || model.IsRegistryCredentialEnvironment(&env) {
			return conflict()
		}
	}
	fullDigest, err := jobqueue.PayloadDigest(j.Payload)
	if err != nil {
		return err
	}
	receipt := model.TriggerReceipt{RunID: p.RunID, JobID: j.ID, PipelineID: p.PipelineID, PlanDigest: fullDigest}
	b, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE trigger_intents SET status='queued',reason='deployment durably queued',receipt=$2,version=version+1,updated_at=NOW() WHERE id=$1`, i.ID, b)
	return err
}
