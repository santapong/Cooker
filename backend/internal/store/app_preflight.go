package store

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
)

// AppPreflightReason is a bounded business decision, never a provider error.
type AppPreflightReason string

const AppPreflightRegistryAuthority AppPreflightReason = "registry_authority"
const AppPreflightRegistrySummary = "accepted registry authority is unavailable or changed; review and start a new deployment"
const AppPreflightEnvironmentGeneration AppPreflightReason = "environment_generation"
const AppPreflightEnvironmentSummary = "environment changed after queueing; review and start a new deployment"

// AppPreflightSummary permits only fixed, credential-free business decisions.
// Unknown/provider error text cannot become determinate rejection evidence.
func AppPreflightSummary(reason AppPreflightReason) string {
	switch reason {
	case AppPreflightRegistryAuthority:
		return AppPreflightRegistrySummary
	case AppPreflightEnvironmentGeneration:
		return AppPreflightEnvironmentSummary
	default:
		return ""
	}
}

// AppPreflightReasonFromRun recovers the recorded outcome without changing the
// immutable acceptance envelope. A failed run alone is insufficient evidence;
// callers must still validate the exact typed history/no-effects marker.
func AppPreflightReasonFromRun(run *model.PipelineRun) (AppPreflightReason, bool) {
	if run == nil || run.Status != model.RunStatusFailed {
		return "", false
	}
	switch run.Error {
	case AppPreflightRegistrySummary:
		return AppPreflightRegistryAuthority, true
	case AppPreflightEnvironmentSummary:
		return AppPreflightEnvironmentGeneration, true
	default:
		return "", false
	}
}

// AppPreflightFailure contains only accepted identity and actor metadata. The
// immutable job remains the source/secret/registry receipt; this outcome stores
// no credentials, source contents, artifact claims, or checkout paths.
type AppPreflightFailure struct {
	AppID, RunID, PipelineID, CommitSHA string
	AcceptedAt                          time.Time
	Actor                               *model.PipelineRun
	EnvironmentID                       string
	EnvironmentVersion                  int
	EnvironmentVersionPresent           bool
	// AcceptedPayload is transient immutable input; it is never persisted here.
	AcceptedPayload []byte
	PayloadDigest   string
	Reason          AppPreflightReason
}
type AppPreflightFailureRecorder interface {
	RecordAppPreflightFailure(context.Context, AppPreflightFailure) error
}
type AppPreflightFailureReader interface {
	HasAppPreflightFailure(context.Context, AppPreflightFailure) (bool, error)
}

func AppPreflightHistoryID(runID string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("cooker-app-preflight:"+runID)).String()
}
func DecodeAppPreflightFailure(payload []byte) (AppPreflightFailure, error) {
	var p struct {
		Version int    `json:"version"`
		RunID   string `json:"runId"`
		App     *struct {
			ID            string `json:"id"`
			EnvironmentID string `json:"environmentId"`
		} `json:"app"`
		Commit             string             `json:"commit"`
		CreatedAt          time.Time          `json:"createdAt"`
		Actor              *model.PipelineRun `json:"actor"`
		EnvironmentVersion *int               `json:"environmentVersion"`
	}
	if json.Unmarshal(payload, &p) != nil || p.Version != 1 || p.App == nil {
		return AppPreflightFailure{}, ErrConflict
	}
	f := AppPreflightFailure{AppID: p.App.ID, RunID: p.RunID, PipelineID: "app-" + p.App.ID + "-" + p.RunID, CommitSHA: p.Commit, AcceptedAt: p.CreatedAt, Actor: p.Actor, EnvironmentID: p.App.EnvironmentID, Reason: AppPreflightRegistryAuthority}
	if p.EnvironmentVersion != nil {
		f.EnvironmentVersion, f.EnvironmentVersionPresent = *p.EnvironmentVersion, true
	}
	digest, err := jobqueue.PayloadDigest(payload)
	if err != nil {
		return AppPreflightFailure{}, ErrConflict
	}
	f.AcceptedPayload = append([]byte(nil), payload...)
	f.PayloadDigest = digest
	return f, f.Validate()
}
func (f AppPreflightFailure) Validate() error {
	if f.AppID == "" || f.RunID == "" || len(f.AppID) > 256 || len(f.RunID) > 256 || f.PipelineID != "app-"+f.AppID+"-"+f.RunID || f.AcceptedAt.IsZero() || AppPreflightSummary(f.Reason) == "" || len(f.CommitSHA) != 40 {
		return ErrConflict
	}
	if _, err := hex.DecodeString(f.CommitSHA); err != nil {
		return ErrConflict
	}
	if f.Reason == AppPreflightEnvironmentGeneration && (f.EnvironmentID == "" || len(f.EnvironmentID) > 256 || !f.EnvironmentVersionPresent || f.EnvironmentVersion < 0 || model.IsRegistryCredentialEnvironmentID(f.EnvironmentID)) {
		return ErrConflict
	}
	return nil
}
func SameAppPreflightAcceptance(a, b AppPreflightFailure) bool {
	return a.Validate() == nil && b.Validate() == nil && a.AppID == b.AppID && a.RunID == b.RunID && a.PipelineID == b.PipelineID && a.CommitSHA == b.CommitSHA && a.PayloadDigest == b.PayloadDigest && a.AcceptedAt.Equal(b.AcceptedAt) && a.EnvironmentID == b.EnvironmentID && a.EnvironmentVersion == b.EnvironmentVersion && a.EnvironmentVersionPresent == b.EnvironmentVersionPresent && sameAppPreflightActor(a.Actor, b.Actor)
}
func sameAppPreflightActor(a, b *model.PipelineRun) bool {
	if a == nil {
		a = &model.PipelineRun{}
	}
	if b == nil {
		b = &model.PipelineRun{}
	}
	return a.StartedByUserSub == b.StartedByUserSub && a.StartedByEmail == b.StartedByEmail && a.StartedByTokenHash == b.StartedByTokenHash && slices.Equal(a.StartedByGroups, b.StartedByGroups)
}
func appPreflightTimeEqual(a, b time.Time) bool {
	// PostgreSQL rounds timestamp input to microsecond precision. Exact half
	// cases may round either way in its parser; accept only that storage error.
	delta := a.Sub(b)
	return delta >= -500*time.Nanosecond && delta <= 500*time.Nanosecond
}

// ValidateAppPreflightRun is shared by atomic admission and lifecycle retention.
// Only an unstarted, effect-free App run can be a determinate rejection.
func ValidateAppPreflightLineage(f AppPreflightFailure, r *model.PipelineRun) error {
	if f.Validate() != nil || r == nil || r.ID != f.RunID || r.PipelineID != f.PipelineID || !appPreflightTimeEqual(r.CreatedAt, f.AcceptedAt) || !sameAppPreflightActor(f.Actor, r) || r.Source != nil {
		return ErrConflict
	}
	return nil
}
func ValidateAppPreflightRun(f AppPreflightFailure, r *model.PipelineRun) error {
	if ValidateAppPreflightLineage(f, r) != nil || r.StartedAt != nil || len(r.EnvironmentStatuses) != 0 {
		return ErrConflict
	}
	for _, s := range r.StageRuns {
		if s.Status != model.RunStatusPending || s.StartedAt != nil || s.FinishedAt != nil || s.Logs != "" || s.Error != "" || len(s.Outputs) > 0 || len(s.Artifacts) > 0 {
			return ErrConflict
		}
	}
	switch r.Status {
	case model.RunStatusPending:
		if r.Error != "" || r.FinishedAt != nil {
			return ErrConflict
		}
	case model.RunStatusFailed:
		if r.Error != AppPreflightSummary(f.Reason) || r.FinishedAt == nil || r.FinishedAt.IsZero() || r.FinishedAt.Before(r.CreatedAt) {
			return ErrConflict
		}
	default:
		return ErrConflict
	}
	return nil
}

// AppPreflightMarkerValid never treats an arbitrary FAILED/empty-image attempt
// as a known rejection. Every typed identity and the outcome time must match.
func AppPreflightMarkerValid(f AppPreflightFailure, r *model.PipelineRun, d *model.AppDeploy) bool {
	return ValidateAppPreflightRun(f, r) == nil && r.Status == model.RunStatusFailed && d != nil && d.ID == AppPreflightHistoryID(f.RunID) && d.AppID == f.AppID && d.RunID == f.RunID && d.PipelineID == f.PipelineID && d.Kind == model.AppDeployKindRejected && d.Status == model.RunStatusFailed && d.ImageRef == "" && d.Digest == "" && d.CreatedAt.Equal(*r.FinishedAt)
}

// Legacy App queue records predate a version field. Explicit null, zero,
// malformed, or unsupported versions never acquire legacy recovery semantics.
func IsLegacyAppDeploymentPayload(payload []byte) bool {
	var raw map[string]json.RawMessage
	if json.Unmarshal(payload, &raw) != nil || raw == nil {
		return false
	}
	for key := range raw {
		if strings.EqualFold(key, "version") {
			return false
		}
	}
	return true
}

type AppPreflightLegacyRecoveryGuard interface {
	CheckAppLegacyRecovery(context.Context, string) error
}
