package service

import (
	"context"
	"errors"
	"time"

	"github.com/santapong/cooker/internal/kube"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// Drift verdicts. "unknown" means we couldn't determine one side
// (no history, cluster unreachable, workload missing); "unsupported"
// means the deploy target has no drift story (non-kubernetes).
const (
	DriftInSync      = "in_sync"
	DriftDetected    = "drift"
	DriftUnknown     = "unknown"
	DriftUnsupported = "unsupported"
)

// AppDrift is the on-demand drift verdict for one app (roadmap M3).
type AppDrift struct {
	Status        string    `json:"status"`
	ExpectedImage string    `json:"expectedImage,omitempty"`
	LiveImage     string    `json:"liveImage,omitempty"`
	Message       string    `json:"message,omitempty"`
	CheckedAt     time.Time `json:"checkedAt"`
}

// KubeWorkloadGetter is the slice of kube.Client drift needs; the
// interface keeps the service testable with a fake.
type KubeWorkloadGetter interface {
	GetWorkload(ctx context.Context, namespace, kind, name string) (model.KubeWorkload, error)
}

// CheckDrift compares the app's last successfully shipped image (from
// deploy history) against the live Deployment's image in the cluster.
// Exact string compare — a re-tagged but identical image counts as
// drift, which is the honest answer for "does the cluster run what
// Cooker shipped".
func CheckDrift(ctx context.Context, kc KubeWorkloadGetter, deploys store.AppDeployStore, app *model.App) AppDrift {
	out := AppDrift{Status: DriftUnknown, CheckedAt: time.Now()}
	if app == nil || app.DeployTarget.Kind != model.DeployTargetKubernetes {
		out.Status = DriftUnsupported
		out.Message = "drift detection supports kubernetes deploy targets only"
		return out
	}
	if kc == nil || deploys == nil {
		out.Message = "drift detection not configured"
		return out
	}

	history, err := deploys.ListByApp(ctx, app.ID, 20)
	if err != nil {
		out.Message = "history unavailable: " + err.Error()
		return out
	}
	for _, d := range history {
		if d.Status == model.RunStatusSuccess && d.ImageRef != "" {
			out.ExpectedImage = d.ImageRef
			break
		}
	}
	if out.ExpectedImage == "" {
		out.Message = "no successful deploy with a recorded image yet"
		return out
	}

	wl, err := kc.GetWorkload(ctx, app.DeployTarget.Namespace, "deployment", sanitize(app.Name))
	if err != nil {
		if errors.Is(err, kube.ErrUnavailable) {
			out.Message = "cluster unreachable"
		} else {
			out.Message = "workload not found: " + err.Error()
		}
		return out
	}
	out.LiveImage = wl.Image
	if out.LiveImage == out.ExpectedImage {
		out.Status = DriftInSync
	} else {
		out.Status = DriftDetected
		out.Message = "live image differs from the last successful deploy"
	}
	return out
}
