package service

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/santapong/cooker/internal/model"
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
	Namespace string
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
	name := sanitize(serviceName)
	if isDocker(app) {
		return r.dockerStatus(ctx, name)
	}
	return r.kubeStatus(ctx, app, name)
}

// Logs streams the named service's container/pod logs to out until the
// context is cancelled or the stream ends.
func (r *RuntimeService) Logs(ctx context.Context, app *model.App, serviceName string, out io.Writer) error {
	name := sanitize(serviceName)
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
	return runStream(ctx, out, r.kubectlBin(), args...)
}

func (r *RuntimeService) dockerStatus(ctx context.Context, name string) (RuntimeStatus, error) {
	bin := r.dockerBin()
	if _, err := exec.LookPath(bin); err != nil {
		return RuntimeStatus{Runtime: "docker", Ref: name, Message: "docker CLI not found"}, nil
	}
	// {{.State.Status}}|{{.Config.Image}}
	out, err := exec.CommandContext(ctx, bin, "inspect", "-f", "{{.State.Status}}|{{.Config.Image}}", name).Output()
	if err != nil {
		return RuntimeStatus{Runtime: "docker", Ref: name, State: "unknown", Message: "not found"}, nil
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "|", 2)
	st := RuntimeStatus{Runtime: "docker", Ref: name, State: parts[0], Healthy: parts[0] == "running"}
	if len(parts) > 1 {
		st.Image = parts[1]
	}
	return st, nil
}

func (r *RuntimeService) kubeStatus(ctx context.Context, app *model.App, name string) (RuntimeStatus, error) {
	bin := r.kubectlBin()
	if _, err := exec.LookPath(bin); err != nil {
		return RuntimeStatus{Runtime: "kubernetes", Ref: name, Message: "kubectl not found"}, nil
	}
	ns := app.DeployTarget.Namespace
	if ns == "" {
		ns = r.Namespace
	}
	args := []string{"get", "deployment", name, "-o", "jsonpath={.status.readyReplicas}/{.status.replicas} {.spec.template.spec.containers[0].image}"}
	if ns != "" {
		args = append(args, "-n", ns)
	}
	out, err := exec.CommandContext(ctx, bin, args...).Output()
	if err != nil {
		return RuntimeStatus{Runtime: "kubernetes", Ref: name, State: "unknown", Message: "not found"}, nil
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	st := RuntimeStatus{Runtime: "kubernetes", Ref: name, State: "unknown"}
	if len(fields) > 0 {
		// fields[0] is "<readyReplicas>/<replicas>" from the jsonpath
		// template. Only treat it as a replica ratio when it actually
		// contains the "/" the template emits — otherwise leave State as
		// "unknown" rather than surfacing a stray token (e.g. the image)
		// as the state.
		if rt := strings.SplitN(fields[0], "/", 2); len(rt) == 2 {
			st.Healthy = rt[0] == rt[1] && rt[0] != "0" && rt[0] != ""
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
		fmt.Fprintf(out, "[runtime] %s not found\n", bin)
		return nil
	}
	cmd := exec.CommandContext(ctx, bin, args...)
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
	for sc.Scan() {
		if _, werr := fmt.Fprintln(out, sc.Text()); werr != nil {
			break
		}
	}
	_ = cmd.Wait()
	return nil
}
