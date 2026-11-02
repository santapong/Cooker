package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
)

// RuntimeRunLabel is stamped by the executor, after repository configuration.
// A service name can be reused during rollout; this label binds one container
// to the same recorded run that supplies its secret redaction generation.
const RuntimeRunLabel = "io.cooker.run-id"

var runtimeContainerID = regexp.MustCompile(`^[0-9a-f]{64}$`)

// StreamDeploymentLogs captures history once and never follows a reused name.
// Docker logs follow the immutable container ID even if a deployment replaces
// its name between inspection and opening the stream. Missing legacy labels,
// missing retained job input, and changed secret generations fail closed.
func (r *RuntimeService) StreamDeploymentLogs(ctx context.Context, app *model.App, service string, jobs jobqueue.Store, resolver *AppEnvResolver, out io.Writer) error {
	observed, err := r.observe(ctx, app)
	if err != nil {
		return err
	}
	bound, name, err := observed.workload(app, service)
	if err != nil {
		return err
	}
	if bound.DeployTarget.Kind != model.DeployTargetDockerHost {
		return fmt.Errorf("service logs unavailable: immutable log sessions are currently supported for Docker deployments only")
	}
	if observed.deployment.RunID == "" || observed.deployment.Status == model.RunStatusNeedsAttention {
		return fmt.Errorf("service logs unavailable: recorded deployment requires reconciliation")
	}
	format := "{{.Id}}|{{index .Config.Labels \"" + RuntimeRunLabel + "\"}}"
	data, err := exec.CommandContext(ctx, r.dockerBin(), "inspect", "-f", format, name).Output()
	if err != nil {
		return fmt.Errorf("service logs unavailable: recorded container could not be inspected")
	}
	identity := strings.Split(strings.TrimSpace(string(data)), "|")
	if len(identity) != 2 || !runtimeContainerID.MatchString(identity[0]) || identity[1] != observed.deployment.RunID {
		return fmt.Errorf("service logs unavailable: container does not match recorded deployment; wait for deployment to finish or redeploy legacy workloads")
	}
	resolved, err := deploymentLogEnvironment(ctx, app.ID, &observed.deployment, jobs, resolver, !r.SecretGenerationBound)
	if err != nil {
		return fmt.Errorf("service logs unavailable: recorded secret configuration changed, is missing, or lacks an immutable backend generation; review configuration and deploy again")
	}
	ctx = WithLogSecrets(ctx, resolved.SecretValues)
	writer := NewSecretLogWriter(ctx, out)
	err = runStream(ctx, writer, r.dockerBin(), "logs", "--follow", "--tail", "200", identity[0])
	flushErr := writer.Close()
	if combined := errors.Join(err, flushErr); combined != nil {
		return errors.New(RedactLogText(ctx, combined.Error()))
	}
	return nil
}
