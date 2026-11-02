package service

import (
	"context"
	"fmt"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/source/gittransport"
	"io"
	"os"
	"reflect"
	"time"
)

type PreparedPipelineSource struct {
	Root         string
	Plan         *model.PipelineSourcePlan
	CheckedOutAt time.Time
}

type pipelineSourceCheckoutKey struct{}

// WithPipelineSourceCheckout carries an ephemeral private export, not a host
// directory from a request or queue payload. Never serialize this context value.
func WithPipelineSourceCheckout(ctx context.Context, source *PreparedPipelineSource) context.Context {
	return context.WithValue(ctx, pipelineSourceCheckoutKey{}, source)
}

// RequirePipelineSourceContext is the executor's pre-stage fail-closed guard.
// Until this is wired, Source!=nil pipelines must not be executable anywhere.
func RequirePipelineSourceContext(ctx context.Context, p *model.Pipeline) error {
	if p == nil {
		return fmt.Errorf("pipeline is required")
	}
	source, _ := ctx.Value(pipelineSourceCheckoutKey{}).(*PreparedPipelineSource)
	if p.Source == nil {
		if source != nil {
			return fmt.Errorf("source checkout does not belong to this pipeline")
		}
		return nil
	}
	if source == nil || source.Root == "" || source.Plan == nil || source.CheckedOutAt.IsZero() {
		return fmt.Errorf("source-bound execution requires a verified exact checkout")
	}
	return ValidatePipelineSourcePlan(p, source.Plan)
}

// PipelineStageBuildPaths preserves source-unbound behavior and rebases only a
// verified ephemeral source context. The stored stage is never rewritten.
func PipelineStageBuildPaths(ctx context.Context, stage *model.Stage) (string, string, error) {
	if stage == nil {
		return "", "", fmt.Errorf("Build stage is required")
	}
	source, _ := ctx.Value(pipelineSourceCheckoutKey{}).(*PreparedPipelineSource)
	if source == nil {
		return stage.Config.Context, stage.Config.Dockerfile, nil
	}
	if source.Root == "" || source.CheckedOutAt.IsZero() {
		return "", "", fmt.Errorf("verified native source is unavailable")
	}
	return PipelineBuildPaths(source.Root, stage)
}

func (s *PreparedPipelineSource) Close() error {
	if s == nil || s.Root == "" {
		return nil
	}
	root := s.Root
	s.Root = ""
	return os.RemoveAll(root)
}

// Open validates authority, exports exact source, and checks reviewed recipes.
// The caller must defer Close before any native stage can start.
func (s *PipelineSourcePreparer) Open(ctx context.Context, p *model.Pipeline, plan *model.PipelineSourcePlan, freshness bool) (*PreparedPipelineSource, error) {
	if err := s.Validate(ctx, p, plan, freshness); err != nil {
		return nil, err
	}
	root, err := s.Source.CheckoutSource(ctx, plan, io.Discard)
	if err != nil {
		return nil, fmt.Errorf("accepted source checkout is unavailable")
	}
	defer os.RemoveAll(root)
	head := s.Head
	if head == nil {
		head = gittransport.HeadCommit
	}
	commit, err := head(ctx, root)
	if err != nil || commit != plan.CommitSHA {
		return nil, fmt.Errorf("checkout does not match accepted source commit")
	}
	export, err := ExportPipelineSource(ctx, root)
	if err != nil {
		return nil, err
	}
	inputs, hash, err := InspectPipelineSourceRecipe(p, export)
	if err != nil || hash != plan.EnvelopeHash || !reflect.DeepEqual(inputs, plan.InputFiles) {
		os.RemoveAll(export)
		return nil, fmt.Errorf("source recipe changed after review")
	}
	return &PreparedPipelineSource{Root: export, Plan: plan, CheckedOutAt: time.Now().UTC()}, nil
}
