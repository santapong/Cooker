// Package cloudrun adapts Cooker's Target interface to Google
// Cloud Run. The real adapter would call cloud.google.com/go/run
// to create or update a Service, wait for the revision to become
// Ready, and stream logs via the Cloud Logging API.
//
// Staying intentionally unwired: adding the google-cloud-go
// modules would pull several hundred MB into every Cooker build
// and the meaningful tests all need a GCP project. Defined now so
// the rest of the codebase can program against a stable Kind.
package cloudrun

import (
	"context"
	"fmt"
	"io"

	"github.com/cooker-ci/cooker/internal/deploytarget"
	"github.com/cooker-ci/cooker/internal/model"
)

// Target is the Cloud Run adapter. Project + Region are required.
type Target struct {
	Project string
	Region  string
}

// New returns a Cloud Run target bound to a GCP project and region.
func New(project, region string) *Target {
	return &Target{Project: project, Region: region}
}

func (*Target) Kind() model.DeployTargetKind { return model.DeployTargetCloudRun }

func (t *Target) Deploy(_ context.Context, _ deploytarget.Spec) error {
	return t.unavailable("Deploy")
}

func (t *Target) Status(_ context.Context, _ string) (deploytarget.Status, error) {
	return deploytarget.Status{}, t.unavailable("Status")
}

func (t *Target) Logs(_ context.Context, _ string, _ io.Writer) error {
	return t.unavailable("Logs")
}

func (t *Target) Rollback(_ context.Context, _ string) error {
	return t.unavailable("Rollback")
}

func (t *Target) unavailable(op string) error {
	if t == nil || t.Project == "" || t.Region == "" {
		return fmt.Errorf("%w: cloud-run %s: Project + Region required", deploytarget.ErrUnavailable, op)
	}
	return fmt.Errorf("%w: cloud-run %s: google-cloud-go run client not yet wired (roadmap Phase 7)", deploytarget.ErrUnavailable, op)
}

var _ deploytarget.Target = (*Target)(nil)
