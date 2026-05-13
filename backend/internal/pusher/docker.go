package pusher

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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

	if req.Source != "" && req.Source != req.Target {
		logf(req.LogWriter, "docker tag %s %s\n", req.Source, req.Target)
		tag := exec.CommandContext(ctx, bin, "tag", req.Source, req.Target)
		tag.Stdout, tag.Stderr = io.Discard, io.Discard
		if err := tag.Run(); err != nil {
			return Result{}, fmt.Errorf("docker tag %s -> %s: %w", req.Source, req.Target, err)
		}
	}

	var buf bytes.Buffer
	push := exec.CommandContext(ctx, bin, "push", req.Target)
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
