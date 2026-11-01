package server

import (
	"fmt"
	"os/exec"

	"github.com/santapong/cooker/internal/config"
	"github.com/santapong/cooker/internal/model"
)

// A no-op backend is useful for pipeline demos but cannot complete App Deploy.
func appExecutionCheck(cfg *config.Config) func(*model.App, bool) error {
	return func(app *model.App, builds bool) error {
		if app.DeployTarget.Kind == model.DeployTargetDockerHost {
			if _, err := exec.LookPath("docker"); err != nil {
				return fmt.Errorf("install Docker CLI and Compose v2 on the Cooker host")
			}
		}
		if app.DeployTarget.Kind == model.DeployTargetKubernetes && cfg.DeployerBackend != "kubectl" && cfg.DeployerBackend != "clientgo" {
			return fmt.Errorf("configure a Kubernetes deployer; the current backend cannot apply workloads")
		}
		if builds {
			if cfg.BuilderBackend != "docker" {
				return fmt.Errorf("GitHub app builds currently require COOKER_BUILDER=docker so the inspected checkout and built image share the same host")
			}
			if cfg.PusherBackend != "docker" {
				return fmt.Errorf("GitHub app builds currently require COOKER_PUSHER=docker to publish the locally built image")
			}
		}
		return nil
	}
}
