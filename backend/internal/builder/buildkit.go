package builder

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/moby/buildkit/client"
)

// BuildKit drives image builds via the buildkitd gRPC API.
//
// Scope: connect to the configured endpoint, solve the dockerfile
// context, and return the image digest. Advanced features (cache
// import/export, secret/ssh mounts, multi-platform manifests) are
// reachable through SolveOpt and are deferred until a concrete
// pipeline shape calls for them.
type BuildKit struct {
	// Addr is the buildkitd endpoint, e.g. "tcp://buildkit:1234" or
	// "kube-pod://ns/pod".
	Addr string
	// OCIRegistryAuth, when non-nil, returns {username, password} for
	// pushing built images to the given registry host. Reserved for
	// when an exporter "push=true" path lands.
	OCIRegistryAuth func(registry string) (string, string, bool)
}

func NewBuildKit(addr string) *BuildKit { return &BuildKit{Addr: addr} }

func (b *BuildKit) Build(ctx context.Context, req Request) (Result, error) {
	if b == nil || b.Addr == "" {
		return Result{}, fmt.Errorf("%w: BuildKit endpoint not configured", ErrUnavailable)
	}
	if req.ContextDir == "" {
		return Result{}, errors.New("buildkit: ContextDir required")
	}
	if _, err := os.Stat(req.ContextDir); err != nil {
		return Result{}, fmt.Errorf("buildkit: context %q: %w", req.ContextDir, err)
	}
	c, err := client.New(ctx, b.Addr)
	if err != nil {
		return Result{}, fmt.Errorf("%w: dial %s: %v", ErrUnavailable, b.Addr, err)
	}
	defer c.Close()

	dockerfile := req.Dockerfile
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	tagAttr := strings.Join(req.Tags, ",")
	if tagAttr == "" {
		tagAttr = "cooker:local"
	}
	frontendAttrs := map[string]string{"filename": dockerfile}
	for k, v := range req.BuildArgs {
		frontendAttrs["build-arg:"+k] = v
	}
	if len(req.Platforms) > 0 {
		frontendAttrs["platform"] = strings.Join(req.Platforms, ",")
	}
	solveOpt := client.SolveOpt{
		Frontend:      "dockerfile.v0",
		FrontendAttrs: frontendAttrs,
		LocalDirs: map[string]string{
			"context":    req.ContextDir,
			"dockerfile": req.ContextDir,
		},
		Exports: []client.ExportEntry{{
			Type:  client.ExporterImage,
			Attrs: map[string]string{"name": tagAttr, "push": "false"},
		}},
	}
	statusCh := make(chan *client.SolveStatus, 8)
	go func() {
		for st := range statusCh {
			if req.LogWriter == nil {
				continue
			}
			for _, v := range st.Logs {
				_, _ = req.LogWriter.Write(v.Data)
			}
		}
	}()
	resp, err := c.Solve(ctx, nil, solveOpt, statusCh)
	if err != nil {
		return Result{}, fmt.Errorf("buildkit: solve: %w", err)
	}
	digest := ""
	if resp != nil && resp.ExporterResponse != nil {
		digest = resp.ExporterResponse["containerimage.digest"]
	}
	return Result{
		ImageID: digest,
		Tags:    req.Tags,
	}, nil
}

var _ Builder = (*BuildKit)(nil)
