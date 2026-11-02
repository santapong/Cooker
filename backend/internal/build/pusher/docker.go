package pusher

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/santapong/cooker/internal/build/registryauth"
	"io"
	"os/exec"
	"regexp"
	"strings"
)

// DockerSock pushes images by invoking the local docker CLI. Useful
// in development; production should use the go-containerregistry
// backend.
type DockerSock struct {
	Bin string
}

func NewDockerSock() *DockerSock { return &DockerSock{} }

// Push tags Source as Target (if they differ) and runs `docker push`.
// The returned Digest is parsed from docker's stdout (`digest:` line).
func (d *DockerSock) Push(ctx context.Context, req Request) (Result, error) {
	if req.Artifact != nil {
		return Result{}, fmt.Errorf("docker pusher cannot consume an OCI artifact; configure crane")
	}
	if req.Target == "" {
		return Result{}, errors.New("pusher: Target is required")
	}
	bin := d.Bin
	if bin == "" {
		bin = "docker"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return Result{}, fmt.Errorf("%w: docker CLI not found: %v", ErrUnavailable, err)
	}

	ref, err := name.ParseReference(req.Target)
	if err != nil {
		return Result{}, errors.New("pusher: invalid target")
	}
	resolver := req.RegistryAuth
	if resolver == nil && req.Auth != nil {
		resolver = func(_ context.Context, host string) (authn.Authenticator, error) {
			u, p, ok := req.Auth(host)
			if !ok {
				return authn.Anonymous, nil
			}
			return authn.FromConfig(authn.AuthConfig{Username: u, Password: p}), nil
		}
	}
	var result Result
	err = registryauth.WithDockerConfig(ctx, resolver, []string{ref.Context().RegistryStr()}, func(dir string) error {
		var pushErr error
		result, pushErr = d.pushScoped(ctx, req, bin, dir)
		return pushErr
	})
	return result, err
}

func (d *DockerSock) pushScoped(ctx context.Context, req Request, bin, configDir string) (Result, error) {
	if req.Source != "" && req.Source != req.Target {
		logf(req.LogWriter, "docker tag %s %s\n", req.Source, req.Target)
		tag := exec.CommandContext(ctx, bin, "tag", req.Source, req.Target)
		tag.Env = registryauth.DockerEnvironment(configDir)
		tag.Stdout, tag.Stderr = io.Discard, io.Discard
		if err := tag.Run(); err != nil {
			return Result{}, fmt.Errorf("docker tag %s -> %s: %w", req.Source, req.Target, err)
		}
	}

	var buf bytes.Buffer
	push := exec.CommandContext(ctx, bin, "push", req.Target)
	push.Env = registryauth.DockerEnvironment(configDir)
	// Tee docker's stdout/stderr to the caller's LogWriter when wired so
	// the run page sees live "Pushing repo..." / "digest: sha256:..."
	// progress lines as the daemon emits them, while still buffering the
	// full output for digest parsing.
	if req.LogWriter != nil {
		push.Stdout = io.MultiWriter(&buf, req.LogWriter)
		push.Stderr = io.MultiWriter(&buf, req.LogWriter)
	} else {
		push.Stdout = io.MultiWriter(&buf)
		push.Stderr = io.MultiWriter(&buf)
	}
	if err := push.Run(); err != nil {
		return Result{}, fmt.Errorf("docker push: %w", err)
	}
	digest := parseDigest(buf.String())
	if digest != "" {
		logf(req.LogWriter, "Pushed image to %s@%s\n", req.Target, digest)
	} else {
		logf(req.LogWriter, "Pushed image to %s\n", req.Target)
	}
	return Result{Digest: digest}, nil
}

// docker push emits lines like: "v1: digest: sha256:abcd... size: 1234"
var digestRE = regexp.MustCompile(`digest:\s*(sha256:[0-9a-f]{64})`)

func parseDigest(s string) string {
	m := digestRE.FindStringSubmatch(s)
	if len(m) != 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

var _ Pusher = (*DockerSock)(nil)
