package deployer

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
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
	// Tee kubectl's combined output to the caller's LogWriter when wired
	// so users tailing the run see "deployment.apps/web created" as it
	// happens; the full output is also retained in `out` for the
	// parseAppliedResources step below.
	if req.LogWriter != nil {
		cmd.Stdout = io.MultiWriter(&out, req.LogWriter)
		cmd.Stderr = io.MultiWriter(&out, req.LogWriter)
	} else {
		cmd.Stdout = io.MultiWriter(&out)
		cmd.Stderr = io.MultiWriter(&out)
	}

	if err := cmd.Run(); err != nil {
		return Result{}, fmt.Errorf("kubectl apply: %w (output: %s)", err, out.String())
	}

	applied := parseAppliedResources(out.String())
	// Emit a normalised "Applied <ref>" line per resource as well, so the
	// log shape matches the client-go adapter — downstream UI / parsers
	// see the same vocabulary regardless of backend.
	for _, ref := range applied {
		logf(req.LogWriter, "Applied %s\n", ref)
	}
	return Result{AppliedResources: applied}, nil
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

// DeployWeighted establishes (or re-balances) a replica-weighted canary
// (OR-1) by piping the synthesised stable+canary Deployment pair through
// `kubectl apply`. Idempotent: a re-apply with a new weight scales the
// two Deployments to the new split.
func (k *Kubectl) DeployWeighted(ctx context.Context, req WeightedRequest) (WeightedResult, error) {
	manifest, canaryReplicas, stableReplicas := weightedManifestFor(req)
	logf(req.LogWriter, "[canary] weight=%d%% canary=%d stable=%d replicas\n", req.Weight, canaryReplicas, stableReplicas)
	res, err := k.Deploy(ctx, Request{
		Kind:      KindManifest,
		Namespace: req.Namespace,
		Manifest:  []byte(manifest),
		LogWriter: req.LogWriter,
	})
	if err != nil {
		return WeightedResult{}, err
	}
	return WeightedResult{
		CanaryReplicas:   canaryReplicas,
		StableReplicas:   stableReplicas,
		AppliedResources: res.AppliedResources,
	}, nil
}

// CanaryReady reports whether the <name>-canary Deployment has all its
// desired replicas ready (PM26-07-04), read via `kubectl get deployment
// -o jsonpath`.
func (k *Kubectl) CanaryReady(ctx context.Context, namespace, name string) (bool, string, error) {
	bin := k.Bin
	if bin == "" {
		bin = "kubectl"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return false, "", fmt.Errorf("%w: kubectl not found: %v", ErrUnavailable, err)
	}
	args := []string{"get", "deployment", sanitizeName(name) + "-canary",
		"-o", "jsonpath={.status.readyReplicas}/{.spec.replicas}"}
	if namespace != "" {
		args = append([]string{"--namespace", namespace}, args...)
	}
	if k.Kubeconfig != "" {
		args = append([]string{"--kubeconfig", k.Kubeconfig}, args...)
	}
	var out, errBuf bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return false, "", fmt.Errorf("%w: kubectl get canary: %v (%s)", ErrUnavailable, err, errBuf.String())
	}
	// jsonpath yields "<ready>/<desired>"; an absent readyReplicas prints
	// as empty (the leading field is ""), so split on "/" and parse each
	// side independently — a single Sscanf("%d/%d") would abort on the
	// leading "/" and lose the desired count.
	ready, desired := 0, 0
	if parts := strings.SplitN(out.String(), "/", 2); len(parts) == 2 {
		ready, _ = strconv.Atoi(strings.TrimSpace(parts[0]))
		desired, _ = strconv.Atoi(strings.TrimSpace(parts[1]))
	}
	detail := fmt.Sprintf("%d/%d ready", ready, desired)
	return ready > 0 && ready >= desired, detail, nil
}

var (
	_ Deployer         = (*Kubectl)(nil)
	_ WeightedDeployer = (*Kubectl)(nil)
	_ CanaryProber     = (*Kubectl)(nil)
)
