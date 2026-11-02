package service

import (
	"context"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/santapong/cooker/internal/deploy/deployer"
	"github.com/santapong/cooker/internal/model"
)

func assignRuntimeReceipt(ctx context.Context, runID, stageID string, req deployer.Request, res deployer.Result, sr *model.StageRun) {
	sr.RuntimeReceipt = nil
	sr.RuntimeBindingWarning = model.RuntimeBindingUnavailable
	if req.Kind != deployer.KindDockerRun {
		return
	}
	r := res.RuntimeReceipt
	effectiveName := req.Name
	if effectiveName == "" {
		effectiveName = "cooker-service"
	}
	if r == nil || res.RuntimeBindingWarning != "" || r.RunID != runID || r.StageID != stageID ||
		req.Labels[RuntimeRunLabel] != runID || req.Labels[RuntimeStageLabel] != stageID ||
		r.Name != effectiveName || r.ImageReference != req.Image {
		return
	}
	if _, err := name.ParseReference(r.ImageReference); err != nil {
		return
	}
	receipt := &model.RuntimeReceipt{Version: r.Version, Runtime: r.Runtime,
		RunID: r.RunID, StageID: r.StageID, ContainerID: r.ContainerID, Name: r.Name,
		ImageReference: r.ImageReference, ImageID: r.ImageID, CapturedAt: r.CapturedAt.UTC()}
	if !receipt.Valid() || !runtimeReceiptMetadataSafe(ctx, receipt) {
		return
	}
	sr.RuntimeReceipt, sr.RuntimeBindingWarning = receipt, ""
}

// Withhold the whole receipt if a known secret entered metadata. Redacting an
// ID/name/reference would create a misleading identity. This optional warning
// never changes the existing artifact/output rejection or operation outcome.
func runtimeReceiptMetadataSafe(ctx context.Context, r *model.RuntimeReceipt) bool {
	for _, value := range []string{r.Runtime, r.RunID, r.StageID, r.ContainerID, r.Name,
		r.ImageReference, r.ImageID, r.CapturedAt.Format(time.RFC3339Nano)} {
		if RedactLogText(ctx, value) != value {
			return false
		}
	}
	return true
}
