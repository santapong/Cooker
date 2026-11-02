package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// PipelineSourceConfig contains author selections, never clone URLs or secrets.
type PipelineSourceConfig struct {
	ConnectionID string `json:"connectionId"`
	Repository   string `json:"repository"`
	FullRef      string `json:"fullRef"`
}

// PipelineSourcePlan is immutable accepted input shared by queue and admission.
// Temporary paths, credential values and source file contents never belong here.
type PipelineSourcePlan struct {
	Version             int               `json:"version"`
	Source              GitSourceIdentity `json:"source"`
	ConnectionVersion   int               `json:"connectionVersion"`
	FullRef             string            `json:"fullRef"`
	CommitSHA           string            `json:"commitSha"`
	ObjectSHA           string            `json:"objectSha,omitempty"`
	PipelineID          string            `json:"pipelineId"`
	PipelineVersion     int               `json:"pipelineVersion"`
	EnvironmentVersions map[string]int    `json:"environmentVersions"`
	PlanHash            string            `json:"planHash"`
	EnvelopeHash        string            `json:"envelopeHash"`
	ReviewPolicy        string            `json:"reviewPolicy"`
	InputFiles          []SourceInputHash `json:"inputFiles"`
}

// Present binds absent-to-present changes as well as changes to existing bytes.
type SourceInputHash struct {
	Path    string `json:"path"`
	Present bool   `json:"present"`
	SHA256  string `json:"sha256,omitempty"`
}

type PipelineTriggerProof struct {
	IntentID         string `json:"intentId"`
	RuleID           string `json:"ruleId"`
	RuleVersion      int    `json:"ruleVersion"`
	TargetKind       string `json:"targetKind"`
	TargetID         string `json:"targetId"`
	EnvelopeHash     string `json:"envelopeHash"`
	SourcePlanDigest string `json:"sourcePlanDigest"`
}

// PipelineRunSource is a safe presentation receipt. Root's run persistence
// integration must derive it from an accepted job, never current definitions.
type PipelineRunSource struct {
	Source           GitSourceIdentity `json:"source"`
	FullRef          string            `json:"fullRef"`
	CommitSHA        string            `json:"commitSha"`
	ObjectSHA        string            `json:"objectSha,omitempty"`
	SourcePlanDigest string            `json:"sourcePlanDigest"`
	CheckedOutAt     *time.Time        `json:"checkedOutAt,omitempty"`
}

type TriggerReview struct {
	ReviewerSubject string    `json:"reviewerSubject"`
	ReviewedAt      time.Time `json:"reviewedAt"`
	IntentVersion   int       `json:"intentVersion"`
	RuleVersion     int       `json:"ruleVersion"`
	PipelineVersion int       `json:"pipelineVersion,omitempty"`
	CommitSHA       string    `json:"commitSha"`
	ObjectSHA       string    `json:"objectSha,omitempty"`
	PlanHash        string    `json:"planHash"`
	EnvelopeHash    string    `json:"envelopeHash"`
}

func PipelineSourcePlanDigest(plan *PipelineSourcePlan) (string, error) {
	b, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

// PipelineSourceConfigDigest binds every executable definition field. Positions
// and persisted times remain included; edits advance the pipeline version.
func PipelineSourceConfigDigest(p *Pipeline, plan *PipelineSourcePlan) string {
	copy := *p
	copy.CreatedAt = copy.CreatedAt.UTC()
	copy.UpdatedAt = copy.UpdatedAt.UTC()
	b, _ := json.Marshal(struct {
		Pipeline          *Pipeline
		Source            GitSourceIdentity
		ConnectionVersion int
		Environments      map[string]int
		ReviewPolicy      string
	}{&copy, plan.Source, plan.ConnectionVersion, plan.EnvironmentVersions, plan.ReviewPolicy})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
