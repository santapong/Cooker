package deployer

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Kubectl applies manifests by shelling out to the kubectl CLI.
// Intended for development; production deployments should use the
// ClientGo backend, which talks to the API server directly.
type Kubectl struct {
	Bin        string
	Kubeconfig string
}

func NewKubectl() *Kubectl { return &Kubectl{} }

// Deploy runs `kubectl apply -f -` with the manifest piped on stdin.
// Only KindManifest is supported by this backend; Helm is deferred to
// a dedicated Helm deployer.
func (k *Kubectl) Deploy(ctx context.Context, req Request) (Result, error) {
	if req.Kind != KindManifest {
		return Result{}, fmt.Errorf("%w: kubectl deployer only supports manifest kind; got %q", ErrUnavailable, req.Kind)
	}
	if len(req.Manifest) == 0 {
		return Result{}, errors.New("deployer: Manifest is required")
	}

	bin := k.Bin
	if bin == "" {
		bin = "kubectl"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return Result{}, fmt.Errorf("%w: kubectl not found: %v", ErrUnavailable, err)
	}

	manifest := req.Manifest
	if req.Image != "" {
		manifest = bytes.ReplaceAll(manifest, []byte("${IMAGE}"), []byte(req.Image))
	}

	args := []string{"apply", "-f", "-"}
	if req.Namespace != "" {
		args = append([]string{"--namespace", req.Namespace}, args...)
	}
	if k.Kubeconfig != "" {
		args = append([]string{"--kubeconfig", k.Kubeconfig}, args...)
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = bytes.NewReader(manifest)
	var out bytes.Buffer
	cmd.Stdout = io.MultiWriter(&out)
	cmd.Stderr = io.MultiWriter(&out)

	if err := cmd.Run(); err != nil {
		return Result{}, fmt.Errorf("kubectl apply: %w (output: %s)", err, out.String())
	}

	return Result{AppliedResources: parseAppliedResources(out.String())}, nil
}

// kubectl apply emits lines like "deployment.apps/myapp created" or
// "service/myapp unchanged". Capture the resource identifier.
func parseAppliedResources(output string) []string {
	scanner := bufio.NewScanner(strings.NewReader(output))
	var out []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			out = append(out, fields[0])
		}
	}
	return out
}

var _ Deployer = (*Kubectl)(nil)
