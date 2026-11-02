package model

import (
	"encoding/hex"
	"errors"
	"net/url"
	"reflect"
	"strings"

	"github.com/santapong/cooker/internal/source/gittransport"
)

var ErrInvalidRunSource = errors.New("invalid run source receipt")

// Validate is a bounded metadata boundary. It cannot establish admission or
// checkout authority; the queue/worker independently verifies those proofs.
func (s *PipelineRunSource) Validate() error {
	if s == nil {
		return nil
	}
	metadata := func(value string, max int) bool {
		if value == "" || len(value) > max {
			return false
		}
		for _, r := range value {
			if r < 33 || r > 126 || strings.ContainsRune("\"\\<>`", r) {
				return false
			}
		}
		return true
	}
	id := s.Source
	if (id.Provider != "github" && id.Provider != "gitlab" && id.Provider != "gitea") || !metadata(id.ConnectionID, 128) || !metadata(id.RepositoryID, 512) || strings.ContainsAny(id.ConnectionID+id.RepositoryID, "/:@?#") || gittransport.ValidateRepositoryPath(id.Repository, true) != nil {
		return ErrInvalidRunSource
	}
	if !metadata(id.Host, 512) {
		return ErrInvalidRunSource
	}
	origin, err := url.Parse("https://" + id.Host)
	if err != nil || origin.User != nil || origin.Host != id.Host || origin.Hostname() == "" || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return ErrInvalidRunSource
	}
	ref := gittransport.Ref{}
	switch {
	case strings.HasPrefix(s.FullRef, "refs/heads/"):
		ref.Kind, ref.Name = "branch", strings.TrimPrefix(s.FullRef, "refs/heads/")
	case strings.HasPrefix(s.FullRef, "refs/tags/"):
		ref.Kind, ref.Name = "tag", strings.TrimPrefix(s.FullRef, "refs/tags/")
	default:
		return ErrInvalidRunSource
	}
	if full, err := ref.FullName(); err != nil || full != s.FullRef || gittransport.ValidateCommit(s.CommitSHA) != nil || s.ObjectSHA != "" && gittransport.ValidateCommit(s.ObjectSHA) != nil || len(s.SourcePlanDigest) != 64 {
		return ErrInvalidRunSource
	}
	if _, err = hex.DecodeString(s.SourcePlanDigest); err != nil {
		return ErrInvalidRunSource
	}
	if s.CheckedOutAt != nil && s.CheckedOutAt.IsZero() {
		return ErrInvalidRunSource
	}
	return nil
}

func (s *PipelineRunSource) Clone() *PipelineRunSource {
	if s == nil {
		return nil
	}
	cp := *s
	cp.CheckedOutAt = cloneTimePtr(s.CheckedOutAt)
	return &cp
}

// RunSourceFromAcceptedPlan projects only public metadata and the exact typed
// plan digest. It never reads the current pipeline or claims checkout occurred.
func RunSourceFromAcceptedPlan(plan *PipelineSourcePlan) (*PipelineRunSource, error) {
	if plan == nil {
		return nil, nil
	}
	digest, err := PipelineSourcePlanDigest(plan)
	if err != nil {
		return nil, ErrInvalidRunSource
	}
	s := &PipelineRunSource{Source: plan.Source, FullRef: plan.FullRef, CommitSHA: plan.CommitSHA, ObjectSHA: plan.ObjectSHA, SourcePlanDigest: digest}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return s, nil
}

// MergeRunSource preserves older callers that omit Source. Admitted lineage
// cannot change; a checkout observation may be added once by a trusted worker.
// The stores never generate an observation time themselves.
func MergeRunSource(previous, next *PipelineRunSource) (*PipelineRunSource, error) {
	if previous.Validate() != nil || next.Validate() != nil {
		return nil, ErrInvalidRunSource
	}
	if next == nil {
		return previous.Clone(), nil
	}
	if previous == nil {
		return next.Clone(), nil
	}
	old, incoming := *previous, *next
	old.CheckedOutAt, incoming.CheckedOutAt = nil, nil
	if !reflect.DeepEqual(old, incoming) || previous.CheckedOutAt != nil && next.CheckedOutAt != nil && !previous.CheckedOutAt.Equal(*next.CheckedOutAt) {
		return nil, ErrInvalidRunSource
	}
	out := next.Clone()
	if out.CheckedOutAt == nil {
		out.CheckedOutAt = cloneTimePtr(previous.CheckedOutAt)
	}
	return out, nil
}
