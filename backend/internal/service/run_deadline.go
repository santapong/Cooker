package service

import (
	"time"

	"github.com/santapong/cooker/internal/model"
)

// Run-deadline clamp bounds. The API validator rejects values outside
// this window at save time; the clamp here is defence-in-depth for
// rows written before validation existed (or by direct DB edits).
const (
	MinRunDeadline = 10 * time.Second
	MaxRunDeadline = 24 * time.Hour
)

// PipelineRunDeadline parses p.RunDeadline and clamps it to
// [MinRunDeadline, MaxRunDeadline]. Returns 0 when the pipeline has
// no usable override — callers fall back to the cluster default
// (COOKER_RUN_DEADLINE, owned by the run coordinator).
func PipelineRunDeadline(p *model.Pipeline) time.Duration {
	if p == nil || p.RunDeadline == "" {
		return 0
	}
	d, err := time.ParseDuration(p.RunDeadline)
	if err != nil || d <= 0 {
		return 0
	}
	if d < MinRunDeadline {
		return MinRunDeadline
	}
	if d > MaxRunDeadline {
		return MaxRunDeadline
	}
	return d
}
