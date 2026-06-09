package builder

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// DockerSock builds images by invoking the local Docker CLI, which
// talks to dockerd over its socket. Intended for development and
// single-host deployments where BuildKit-in-cluster would be overkill.
type DockerSock struct {
	// Bin overrides the docker binary path. Empty means "docker" on PATH.
	Bin string
	// ExtraArgs are appended after "build" and before the context path.
	// Useful to forward --pull, --no-cache, or --network host in tests.
	ExtraArgs []string
}

// NewDockerSock returns a DockerSock with default configuration.
func NewDockerSock() *DockerSock { return &DockerSock{} }

// Build shells out to `docker build`. Multi-arch requests are rejected
// here because classic `docker build` cannot produce an image index; a
// buildx-backed builder should handle those instead.
func (d *DockerSock) Build(ctx context.Context, req Request) (Result, error) {
	if err := validateRequest(req); err != nil {
		return Result{}, err
	}
	if len(req.Platforms) > 1 {
		return Result{}, fmt.Errorf("%w: docker-sock builder cannot produce multi-arch images; use buildkit", ErrUnavailable)
	}

	bin := d.Bin
	if bin == "" {
		bin = "docker"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return Result{}, fmt.Errorf("%w: docker CLI not found: %v", ErrUnavailable, err)
	}
	if req.Cache.enabled() && req.LogWriter != nil {
		// Classic docker build has no registry layer-cache transport;
		// the daemon's local cache applies implicitly instead.
		fmt.Fprintln(req.LogWriter, "cache: registry layer cache not supported on the docker builder; ignoring cache config")
	}

	args := []string{"build"}
	if req.Dockerfile != "" {
		args = append(args, "-f", req.Dockerfile)
	}
	for _, t := range req.Tags {
		args = append(args, "-t", t)
	}
	for k, v := range req.BuildArgs {
		args = append(args, "--build-arg", k+"="+v)
	}
	if len(req.Platforms) == 1 {
		args = append(args, "--platform", req.Platforms[0])
	}
	args = append(args, d.ExtraArgs...)
	args = append(args, req.ContextDir)

	cmd := exec.CommandContext(ctx, bin, args...)
	out := req.LogWriter
	if out == nil {
		out = io.Discard
	}
	cmd.Stdout = out
	cmd.Stderr = out

	if err := cmd.Run(); err != nil {
		return Result{}, fmt.Errorf("docker build failed: %w", err)
	}

	id, err := d.inspectID(ctx, bin, req.Tags[0])
	if err != nil {
		// Build succeeded but we couldn't resolve the ID; return partial
		// success rather than failing — the tags are still usable.
		return Result{Tags: req.Tags}, nil
	}
	return Result{ImageID: id, Tags: append([]string(nil), req.Tags...)}, nil
}

// inspectID resolves a tag to its content-addressable ID.
func (d *DockerSock) inspectID(ctx context.Context, bin, tag string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, "image", "inspect", "--format", "{{.Id}}", tag)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

func validateRequest(req Request) error {
	if req.ContextDir == "" {
		return errors.New("builder: ContextDir is required")
	}
	if len(req.Tags) == 0 {
		return errors.New("builder: at least one Tag is required")
	}
	return nil
}

var _ Builder = (*DockerSock)(nil)
