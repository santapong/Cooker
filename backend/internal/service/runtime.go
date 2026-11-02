package service

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/santapong/cooker/internal/deploy/deploytarget"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// RuntimeStatus is the live state of one deployed service's
// container/pod, surfaced in the deployment-view runtime panel.
type RuntimeStatus struct {
	// Runtime is "docker" or "kubernetes".
	Runtime string `json:"runtime"`
	// Ref is the container name (docker) or pod/deployment name (k8s).
	Ref string `json:"ref"`
	// State is a human string: "running", "exited", "pending", etc.
	State string `json:"state"`
	// Healthy is a coarse rollup for the status dot.
	Healthy bool `json:"healthy"`
	// Image is the running image, when resolvable.
	Image string `json:"image,omitempty"`
	// Message carries any diagnostic (e.g. "not found").
	Message string `json:"message,omitempty"`
}

// RuntimeService inspects and tails the actual container/pod backing a
// deployed compose service. It is intentionally CLI-backed (docker /
// kubectl) to match the existing builder/deployer adapters and avoid a
// heavyweight typed-client dependency in this layer.
//
// Mapping: a compose service "web" deploys as a docker container named
// "web" (DockerRun uses the service name) or a K8s Deployment/pod with
// label app=<sanitized name> (composeServiceManifest). RuntimeService
// resolves the right runtime from the App's deploy target.
type RuntimeService struct {
	// DockerBin / KubectlBin override the binaries (tests). Empty →
	// "docker" / "kubectl" on PATH.
	DockerBin  string
	KubectlBin string
	// Namespace is the default K8s namespace when the App target omits
	// one.
	Namespace  string
	Kubeconfig string
	// Store binds runtime reads to the recorded deployment rather than a
	// caller-supplied service name. The server always configures this.
	Store *store.Store
	// SecretGenerationBound is set only when secret writes advance the same
	// Environment.Version checked by the recorded deployment's resolver.
	SecretGenerationBound bool
	logSessionsMu         sync.Mutex
	logSessions           *runtimeLogSessionCache
}

// NewRuntimeService constructs a runtime service.
func NewRuntimeService(namespace string) *RuntimeService {
	return &RuntimeService{Namespace: namespace}
}

func (r *RuntimeService) dockerBin() string {
	if r.DockerBin != "" {
		return r.DockerBin
	}
	return "docker"
}

func (r *RuntimeService) kubectlBin() string {
	if r.KubectlBin != "" {
		return r.KubectlBin
	}
	return "kubectl"
}

// isDocker reports whether the App deploys to a Docker host.
func isDocker(app *model.App) bool {
	return app.DeployTarget.Kind == model.DeployTargetDockerHost
}

// Status returns the live state of the named service for the app.
func (r *RuntimeService) Status(ctx context.Context, app *model.App, serviceName string) (RuntimeStatus, error) {
	if r.Store != nil {
		observed, err := r.observe(ctx, app)
		if err != nil {
			return RuntimeStatus{}, err
		}
		return r.observedStatus(ctx, app, serviceName, observed)
	}
	name := runtimeWorkloadName(app, serviceName)
	for _, b := range app.DeployTarget.ExternalServices {
		if b.Service == serviceName {
			return RuntimeStatus{Runtime: "external", Ref: b.Resource, State: "external", Message: "Database lifecycle and health are managed by its provider"}, nil
		}
	}
	return r.workloadStatus(ctx, app, name, "")
}

func (r *RuntimeService) observedStatus(ctx context.Context, app *model.App, service string, observed *runtimeObservation) (RuntimeStatus, error) {
	bound, name, err := observed.workload(app, service)
	if err != nil {
		return RuntimeStatus{}, err
	}
	return r.workloadStatus(ctx, bound, name, observed.deployment.RunID)
}

func (r *RuntimeService) workloadStatus(ctx context.Context, app *model.App, name, runID string) (RuntimeStatus, error) {
	if app.DeployTarget.Kind == model.DeployTargetECS || app.DeployTarget.Kind == model.DeployTargetCloudRun {
		t, err := deploytarget.Lookup(app.DeployTarget.Kind)
		if err != nil {
			return RuntimeStatus{}, err
		}
		st, err := t.Status(ctx, name)
		if err != nil {
			return RuntimeStatus{}, err
		}
		state := "pending"
		if st.Healthy {
			state = "running"
		}
		return RuntimeStatus{Runtime: string(t.Kind()), Ref: name, State: state, Healthy: st.Healthy}, nil
	}
	if app.DeployTarget.Kind != model.DeployTargetDockerHost && app.DeployTarget.Kind != model.DeployTargetKubernetes {
		return RuntimeStatus{}, fmt.Errorf("runtime inspection is unsupported for target %s", app.DeployTarget.Kind)
	}
	if isDocker(app) {
		return r.dockerStatus(ctx, name, runID)
	}
	return r.kubeStatus(ctx, app, name, runID)
}

// Logs streams the named service's container/pod logs to out until the
// context is cancelled or the stream ends.
func (r *RuntimeService) Logs(ctx context.Context, app *model.App, serviceName string, out io.Writer) error {
	name := runtimeWorkloadName(app, serviceName)
	for _, b := range app.DeployTarget.ExternalServices {
		if b.Service == serviceName {
			return fmt.Errorf("external database logs are available from its provider")
		}
	}
	if r.Store != nil {
		return fmt.Errorf("recorded service logs require an immutable deployment log session")
	}
	if app.DeployTarget.Kind == model.DeployTargetECS || app.DeployTarget.Kind == model.DeployTargetCloudRun {
		t, err := deploytarget.Lookup(app.DeployTarget.Kind)
		if err != nil {
			return err
		}
		return t.Logs(ctx, name, out)
	}
	if app.DeployTarget.Kind != model.DeployTargetDockerHost && app.DeployTarget.Kind != model.DeployTargetKubernetes {
		return fmt.Errorf("runtime logs are unsupported for target %s", app.DeployTarget.Kind)
	}
	if isDocker(app) {
		return runStream(ctx, out, r.dockerBin(), "logs", "--follow", "--tail", "200", name)
	}
	ns := app.DeployTarget.Namespace
	if ns == "" {
		ns = r.Namespace
	}
	// Stream logs for all pods of the Deployment via a label selector.
	args := []string{"logs", "--follow", "--tail", "200", "-l", "app=" + name}
	if ns != "" {
		args = append(args, "-n", ns)
	}
	if r.Kubeconfig != "" {
		args = append(args, "--kubeconfig", r.Kubeconfig)
	}
	return runStream(ctx, out, r.kubectlBin(), args...)
}

func runtimeWorkloadName(app *model.App, service string) string {
	if app.BuildPlan != nil && app.BuildPlan.Kind != model.BuildPlanCompose {
		return AppPrefix(app)
	}
	if app.DeployTarget.Kind == model.DeployTargetDockerHost && app.BuildPlan != nil && app.BuildPlan.Kind == model.BuildPlanCompose {
		return AppPrefix(app) + "-" + service + "-1"
	}
	if app.DeployTarget.Prefix != "" {
		return RuntimeServiceName(app, service)
	}
	return sanitize(service) // Compatibility for deployments made before prefixes.
}

func (r *RuntimeService) dockerStatus(ctx context.Context, name string, runIDs ...string) (RuntimeStatus, error) {
	bin := r.dockerBin()
	if _, err := exec.LookPath(bin); err != nil {
		return RuntimeStatus{Runtime: "docker", Ref: name, Message: "docker CLI not found"}, nil
	}
	format := "{{.State.Status}}|{{.Config.Image}}|{{if .State.Health}}{{.State.Health.Status}}{{end}}"
	if len(runIDs) > 0 && runIDs[0] != "" {
		format += "|{{index .Config.Labels \"" + RuntimeRunLabel + "\"}}"
	}
	out, err := exec.CommandContext(ctx, bin, "inspect", "-f", format, name).Output()
	if err != nil {
		return RuntimeStatus{Runtime: "docker", Ref: name, State: "unknown", Message: "not found"}, nil
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "|", 4)
	st := RuntimeStatus{Runtime: "docker", Ref: name, State: parts[0]}
	if len(runIDs) > 0 && runIDs[0] != "" && (len(parts) != 4 || parts[3] != runIDs[0]) {
		return RuntimeStatus{Runtime: "docker", Ref: name, State: "unknown", Message: "container does not match recorded deployment"}, nil
	}
	if len(parts) > 1 {
		st.Image = parts[1]
	}
	if st.State == "running" {
		if len(parts) >= 3 && parts[2] != "" {
			st.Healthy = parts[2] == "healthy"
			st.Message = "container health check: " + parts[2]
			if !st.Healthy {
				st.State = "degraded"
			}
		} else {
			st.Message = "running; no container health check is configured"
		}
	}
	return st, nil
}

func (r *RuntimeService) kubeStatus(ctx context.Context, app *model.App, name string, runIDs ...string) (RuntimeStatus, error) {
	bin := r.kubectlBin()
	if _, err := exec.LookPath(bin); err != nil {
		return RuntimeStatus{Runtime: "kubernetes", Ref: name, Message: "kubectl not found"}, nil
	}
	ns := app.DeployTarget.Namespace
	if ns == "" {
		ns = r.Namespace
	}
	args := []string{"get", "deployment", name, "-o", "jsonpath={.status.readyReplicas}/{.spec.replicas}/{.status.updatedReplicas}/{.metadata.generation}/{.status.observedGeneration} {.spec.template.spec.containers[0].image}"}
	if len(runIDs) > 0 && runIDs[0] != "" {
		args[4] += " {.spec.template.metadata.labels.io\\.cooker\\.run-id}"
	}
	if ns != "" {
		args = append(args, "-n", ns)
	}
	if r.Kubeconfig != "" {
		args = append(args, "--kubeconfig", r.Kubeconfig)
	}
	out, err := exec.CommandContext(ctx, bin, args...).Output()
	if err != nil {
		return RuntimeStatus{Runtime: "kubernetes", Ref: name, State: "unknown", Message: "not found"}, nil
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	st := RuntimeStatus{Runtime: "kubernetes", Ref: name, State: "unknown"}
	if len(runIDs) > 0 && runIDs[0] != "" && (len(fields) != 3 || fields[2] != runIDs[0]) {
		st.Message = "workload does not match recorded deployment"
		return st, nil
	}
	if len(fields) > 0 {
		// fields[0] is "<readyReplicas>/<replicas>" from the jsonpath
		// template. Only treat it as a replica ratio when it actually
		// contains the "/" the template emits — otherwise leave State as
		// "unknown" rather than surfacing a stray token (e.g. the image)
		// as the state.
		if rt := strings.Split(fields[0], "/"); len(rt) == 5 {
			readyCount, readyErr := strconv.Atoi(rt[0])
			totalCount, totalErr := strconv.Atoi(rt[1])
			updated, updatedErr := strconv.Atoi(rt[2])
			generation, genErr := strconv.Atoi(rt[3])
			observed, observedErr := strconv.Atoi(rt[4])
			st.Healthy = readyErr == nil && totalErr == nil && updatedErr == nil && genErr == nil && observedErr == nil && totalCount > 0 && readyCount == totalCount && updated == totalCount && observed >= generation
			ready, total := rt[0], rt[1]
			if ready == "" {
				ready = "0"
			}
			if total == "" {
				total = "0"
			}
			if st.Healthy {
				st.State = "running"
			} else {
				st.State = "degraded"
			}
			st.Message = "ready " + ready + "/" + total
		}
	}
	if len(fields) > 1 {
		st.Image = fields[1]
	}
	return st, nil
}

// runStream runs cmd and copies combined stdout+stderr to out line by
// line until the context is cancelled or the process exits.
func runStream(ctx context.Context, out io.Writer, bin string, args ...string) error {
	if _, err := exec.LookPath(bin); err != nil {
		return fmt.Errorf("runtime log adapter is unavailable: %s not found", bin)
	}
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(streamCtx, bin, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("runtime stream start: %w", err)
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var writeErr error
	for sc.Scan() {
		if _, werr := fmt.Fprintln(out, sc.Text()); werr != nil {
			writeErr = werr
			cancel()
			break
		}
	}
	if sc.Err() != nil {
		cancel()
	}
	waitErr := cmd.Wait()
	return errors.Join(writeErr, sc.Err(), waitErr)
}
