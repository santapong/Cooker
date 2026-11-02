package server

import (
	"fmt"
	"os"
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
			if v := os.Getenv("COOKER_OUTPUTS_ENABLED"); v == "false" || v == "0" {
				return fmt.Errorf("source builds require stage outputs for the immutable image handoff; enable COOKER_OUTPUTS_ENABLED")
			}
			switch {
			case cfg.BuilderBackend == "docker" && cfg.PusherBackend == "docker":
				if cfg.Env.IsProduction() {
					return fmt.Errorf("docker socket builds are unavailable in production; configure BuildKit and Crane")
				}
			case cfg.BuilderBackend == "buildkit" && cfg.PusherBackend == "crane":
				if cfg.BuildKit.Address == "" {
					return fmt.Errorf("configure COOKER_BUILDKIT_ADDR for source builds")
				}
			default:
				return fmt.Errorf("source builds require the compatible BuildKit/Crane pair, or Docker/Docker in development")
			}
		}
		return nil
	}
}
