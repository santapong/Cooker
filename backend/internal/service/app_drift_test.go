package service

import (
	"context"
	"errors"
	"testing"

	"github.com/santapong/cooker/internal/kube"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store/memory"
)

type fakeWorkloads struct {
	image string
	err   error
}

func (f *fakeWorkloads) GetWorkload(_ context.Context, _, _, _ string) (model.KubeWorkload, error) {
	if f.err != nil {
		return model.KubeWorkload{}, f.err
	}
	return model.KubeWorkload{Image: f.image}, nil
}

func k8sDriftApp() *model.App {
	return &model.App{
		ID: "a1", Name: "shop",
		DeployTarget: model.DeployTarget{Kind: model.DeployTargetKubernetes, Namespace: "prod"},
	}
}

func seedDeploy(t *testing.T, st interface {
	Create(ctx context.Context, d *model.AppDeploy) error
}, status model.RunStatus, ref string) {
	t.Helper()
	if err := st.Create(context.Background(), &model.AppDeploy{
		ID: "d-" + ref + string(status), AppID: "a1", RunID: "r",
		ImageRef: ref, Status: status, Kind: model.AppDeployKindDeploy,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCheckDrift(t *testing.T) {
	t.Run("unsupported target", func(t *testing.T) {
		app := &model.App{ID: "a1", DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost}}
		got := CheckDrift(context.Background(), &fakeWorkloads{}, memory.New().AppDeploys, app)
		if got.Status != DriftUnsupported {
			t.Errorf("status = %s, want unsupported", got.Status)
		}
	})

	t.Run("no history → unknown", func(t *testing.T) {
		got := CheckDrift(context.Background(), &fakeWorkloads{image: "x"}, memory.New().AppDeploys, k8sDriftApp())
		if got.Status != DriftUnknown {
			t.Errorf("status = %s, want unknown", got.Status)
		}
	})

	t.Run("in sync", func(t *testing.T) {
		st := memory.New().AppDeploys
		seedDeploy(t, st, model.RunStatusSuccess, "reg/shop:42")
		got := CheckDrift(context.Background(), &fakeWorkloads{image: "reg/shop:42"}, st, k8sDriftApp())
		if got.Status != DriftInSync {
			t.Errorf("status = %s (%s), want in_sync", got.Status, got.Message)
		}
	})

	t.Run("drift", func(t *testing.T) {
		st := memory.New().AppDeploys
		seedDeploy(t, st, model.RunStatusSuccess, "reg/shop:42")
		got := CheckDrift(context.Background(), &fakeWorkloads{image: "reg/shop:41"}, st, k8sDriftApp())
		if got.Status != DriftDetected {
			t.Errorf("status = %s, want drift", got.Status)
		}
		if got.ExpectedImage != "reg/shop:42" || got.LiveImage != "reg/shop:41" {
			t.Errorf("images = %s / %s", got.ExpectedImage, got.LiveImage)
		}
	})

	t.Run("failed deploys don't count as expected", func(t *testing.T) {
		st := memory.New().AppDeploys
		seedDeploy(t, st, model.RunStatusFailed, "reg/shop:43")
		got := CheckDrift(context.Background(), &fakeWorkloads{image: "reg/shop:42"}, st, k8sDriftApp())
		if got.Status != DriftUnknown {
			t.Errorf("status = %s, want unknown (only successful deploys count)", got.Status)
		}
	})

	t.Run("cluster unreachable → unknown", func(t *testing.T) {
		st := memory.New().AppDeploys
		seedDeploy(t, st, model.RunStatusSuccess, "reg/shop:42")
		got := CheckDrift(context.Background(), &fakeWorkloads{err: kube.ErrUnavailable}, st, k8sDriftApp())
		if got.Status != DriftUnknown || got.Message != "cluster unreachable" {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("workload missing → unknown", func(t *testing.T) {
		st := memory.New().AppDeploys
		seedDeploy(t, st, model.RunStatusSuccess, "reg/shop:42")
		got := CheckDrift(context.Background(), &fakeWorkloads{err: errors.New("not found")}, st, k8sDriftApp())
		if got.Status != DriftUnknown {
			t.Errorf("status = %s, want unknown", got.Status)
		}
	})
}
