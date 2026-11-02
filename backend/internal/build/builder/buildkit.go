package builder

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/session"
	"github.com/santapong/cooker/internal/build/artifact"
	"github.com/santapong/cooker/internal/build/registryauth"
	"github.com/tonistiigi/fsutil"
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
	// TLS parameters secure the control channel to a remote isolated builder.
	TLSCA, TLSCert, TLSKey, TLSServerName string
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
	var opts []client.ClientOpt
	if b.TLSCA != "" {
		opts = append(opts, client.WithServerConfig(b.TLSServerName, b.TLSCA))
	}
	if b.TLSCert != "" {
		opts = append(opts, client.WithCredentials(b.TLSCert, b.TLSKey))
	}
	c, err := client.New(ctx, b.Addr, opts...)
	if err != nil {
		return Result{}, fmt.Errorf("%w: dial %s: %v", ErrUnavailable, b.Addr, err)
	}
	defer c.Close()

	output, err := artifact.NewImage()
	if err != nil {
		return Result{}, fmt.Errorf("buildkit: create artifact: %w", err)
	}
	retained := false
	defer func() {
		if !retained {
			_ = output.Close()
		}
	}()
	solveOpt, err := solveOptions(req)
	if err != nil {
		return Result{}, err
	}
	solveOpt.Exports[0].OutputDir = output.OutputDir()
	solveOpt.Session = []session.Attachable{&registrySession{resolve: func(_ context.Context, host string) (authn.Authenticator, error) {
		return registryauth.Resolve(ctx, req.RegistryAuth, host)
	}}}
	statusCh := make(chan *client.SolveStatus, 8)
	logsDone := make(chan struct{})
	go func() {
		defer close(logsDone)
		// Drain status until BuildKit closes the channel, regardless
		// of whether req.LogWriter is set — leaving messages
		// undrained pins the Solve goroutine and leaks this one.
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
	// Solve always closes statusCh on return (success or error); wait
	// for our drainer so the goroutine can't leak past Build.
	<-logsDone
	if err != nil {
		return Result{}, fmt.Errorf("buildkit: solve: %w", err)
	}
	digest := ""
	if resp != nil && resp.ExporterResponse != nil {
		digest = resp.ExporterResponse["containerimage.digest"]
	}
	_, desc, err := output.Contents()
	if err != nil {
		return Result{}, fmt.Errorf("buildkit: invalid exported artifact: %w", err)
	}
	if digest == "" || digest != desc.Digest.String() {
		return Result{}, fmt.Errorf("buildkit: exported artifact digest does not match build result")
	}
	retained = true
	return Result{
		Artifact: output,
		ImageID:  digest,
		Tags:     req.Tags,
	}, nil
}

// solveOptions assembles the SolveOpt without talking to buildkitd so
// tests can assert the cache wiring shape directly.
func solveOptions(req Request) (client.SolveOpt, error) {
	dockerfile := req.Dockerfile
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	tagAttr := strings.Join(req.Tags, ",")
	if tagAttr == "" {
		tagAttr = "cooker:local"
	}
	frontendAttrs := map[string]string{"filename": dockerfile}
	dockerfileDir := req.ContextDir
	if filepath.Dir(dockerfile) != "." {
		path := dockerfile
		if !filepath.IsAbs(path) {
			path = filepath.Join(req.ContextDir, path)
		}
		dockerfileDir = filepath.Dir(path)
		frontendAttrs["filename"] = filepath.Base(path)
	}
	if req.Target != "" {
		frontendAttrs["target"] = req.Target
	}
	for k, v := range req.BuildArgs {
		frontendAttrs["build-arg:"+k] = v
	}
	if len(req.Platforms) > 0 {
		frontendAttrs["platform"] = strings.Join(req.Platforms, ",")
	}
	contextFS, err := fsutil.NewFS(req.ContextDir)
	if err != nil {
		return client.SolveOpt{}, fmt.Errorf("buildkit: context filesystem: %w", err)
	}
	dockerfileFS, err := fsutil.NewFS(dockerfileDir)
	if err != nil {
		return client.SolveOpt{}, fmt.Errorf("buildkit: dockerfile filesystem: %w", err)
	}
	opt := client.SolveOpt{
		Frontend:      "dockerfile.v0",
		FrontendAttrs: frontendAttrs,
		LocalMounts: map[string]fsutil.FS{
			"context":    contextFS,
			"dockerfile": dockerfileFS,
		},
		Exports: []client.ExportEntry{{
			Type:  client.ExporterOCI,
			Attrs: map[string]string{"name": tagAttr, "tar": "false"},
		}},
	}
	if req.Cache.enabled() {
		// Import and export use separate attr maps — SolveOpt mutates
		// neither, but sharing one map across entries invites aliasing
		// bugs if a future change does.
		importAttrs := map[string]string{"ref": req.Cache.Ref}
		exportAttrs := map[string]string{"ref": req.Cache.Ref}
		if req.Cache.Mode == "oci" {
			importAttrs["oci-mediatypes"] = "true"
			exportAttrs["oci-mediatypes"] = "true"
		}
		if req.Cache.Inline {
			exportAttrs["mode"] = "max"
		}
		opt.CacheImports = []client.CacheOptionsEntry{{Type: "registry", Attrs: importAttrs}}
		opt.CacheExports = []client.CacheOptionsEntry{{Type: "registry", Attrs: exportAttrs}}
	}
	return opt, nil
}

var _ Builder = (*BuildKit)(nil)
