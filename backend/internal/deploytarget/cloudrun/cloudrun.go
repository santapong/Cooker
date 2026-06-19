// Package cloudrun adapts Cooker's deploytarget.Target to Google
// Cloud Run via cloud.google.com/go/run/apiv2.
package cloudrun

import (
	"context"
	"fmt"
	"io"
	"sync"

	run "cloud.google.com/go/run/apiv2"
	"cloud.google.com/go/run/apiv2/runpb"

	"github.com/santapong/cooker/internal/deploytarget"
	"github.com/santapong/cooker/internal/model"
)

// Target is the Cloud Run adapter. Project + Region are required.
type Target struct {
	Project string
	Region  string

	// AD-H2: cache gRPC clients via sync.Once so we don't open a new
	// gRPC connection on every Deploy/Status/Rollback call. Up to 4
	// connections were being opened per Rollback before this fix.
	svcOnce sync.Once
	svcCli  *run.ServicesClient
	svcErr  error
	revOnce sync.Once
	revCli  *run.RevisionsClient
	revErr  error
}

// New returns a Cloud Run target bound to a GCP project and region.
func New(project, region string) *Target {
	return &Target{Project: project, Region: region}
}

func (*Target) Kind() model.DeployTargetKind { return model.DeployTargetCloudRun }

func (t *Target) parent() string {
	return fmt.Sprintf("projects/%s/locations/%s", t.Project, t.Region)
}

func (t *Target) serviceName(appID string) string {
	return fmt.Sprintf("%s/services/%s", t.parent(), appID)
}

func (t *Target) requireConfig() error {
	if t == nil || t.Project == "" || t.Region == "" {
		return fmt.Errorf("%w: cloud-run: Project + Region required", deploytarget.ErrUnavailable)
	}
	return nil
}

// servicesClient returns the cached Cloud Run ServicesClient, creating
// it on the first call (AD-H2). The background context is used for the
// one-shot SDK init; per-call operations use their own ctx.
func (t *Target) servicesClient(ctx context.Context) (*run.ServicesClient, error) {
	t.svcOnce.Do(func() {
		c, err := run.NewServicesClient(ctx)
		if err != nil {
			t.svcErr = fmt.Errorf("cloud-run: services client: %w", err)
			return
		}
		t.svcCli = c
	})
	return t.svcCli, t.svcErr
}

// revisionsClient returns the cached Cloud Run RevisionsClient, creating
// it on the first call (AD-H2).
func (t *Target) revisionsClient(ctx context.Context) (*run.RevisionsClient, error) {
	t.revOnce.Do(func() {
		c, err := run.NewRevisionsClient(ctx)
		if err != nil {
			t.revErr = fmt.Errorf("cloud-run: revisions client: %w", err)
			return
		}
		t.revCli = c
	})
	return t.revCli, t.revErr
}

// Deploy creates or updates a Cloud Run Service for spec.AppID and
// waits for the resulting long-running operation to complete.
func (t *Target) Deploy(ctx context.Context, spec deploytarget.Spec) error {
	if err := t.requireConfig(); err != nil {
		return err
	}
	c, err := t.servicesClient(ctx)
	if err != nil {
		return err
	}
	envVars := make([]*runpb.EnvVar, 0, len(spec.Env))
	for k, v := range spec.Env {
		envVars = append(envVars, &runpb.EnvVar{Name: k, Values: &runpb.EnvVar_Value{Value: v}})
	}
	template := &runpb.RevisionTemplate{
		Containers: []*runpb.Container{{
			Image: spec.Image,
			Env:   envVars,
		}},
	}
	if spec.Replicas > 0 {
		template.Scaling = &runpb.RevisionScaling{MinInstanceCount: int32(spec.Replicas)}
	}

	if _, gerr := c.GetService(ctx, &runpb.GetServiceRequest{Name: t.serviceName(spec.AppID)}); gerr == nil {
		op, uerr := c.UpdateService(ctx, &runpb.UpdateServiceRequest{Service: &runpb.Service{
			Name:     t.serviceName(spec.AppID),
			Template: template,
		}})
		if uerr != nil {
			return fmt.Errorf("cloud-run: update %s: %w", spec.AppID, uerr)
		}
		if _, err := op.Wait(ctx); err != nil {
			return fmt.Errorf("cloud-run: wait update %s: %w", spec.AppID, err)
		}
		return nil
	}
	op, cerr := c.CreateService(ctx, &runpb.CreateServiceRequest{
		Parent:    t.parent(),
		ServiceId: spec.AppID,
		Service:   &runpb.Service{Template: template},
	})
	if cerr != nil {
		return fmt.Errorf("cloud-run: create %s: %w", spec.AppID, cerr)
	}
	if _, err := op.Wait(ctx); err != nil {
		return fmt.Errorf("cloud-run: wait create %s: %w", spec.AppID, err)
	}
	return nil
}

func (t *Target) Status(ctx context.Context, appID string) (deploytarget.Status, error) {
	if err := t.requireConfig(); err != nil {
		return deploytarget.Status{}, err
	}
	c, err := t.servicesClient(ctx)
	if err != nil {
		return deploytarget.Status{}, err
	}
	svc, err := c.GetService(ctx, &runpb.GetServiceRequest{Name: t.serviceName(appID)})
	if err != nil {
		return deploytarget.Status{}, err
	}
	healthy := svc.GetTerminalCondition().GetState() == runpb.Condition_CONDITION_SUCCEEDED
	return deploytarget.Status{
		Healthy: healthy,
		URL:     svc.GetUri(),
	}, nil
}

func (t *Target) Logs(_ context.Context, _ string, _ io.Writer) error {
	// Cloud Logging streaming is a separate SDK; deferred.
	return fmt.Errorf("%w: cloud-run logs: streaming via Cloud Logging not yet wired", deploytarget.ErrUnavailable)
}

// Rollback retargets 100% of traffic to the previous ready revision.
func (t *Target) Rollback(ctx context.Context, appID string) error {
	if err := t.requireConfig(); err != nil {
		return err
	}
	sc, err := t.servicesClient(ctx)
	if err != nil {
		return err
	}
	rc, err := t.revisionsClient(ctx)
	if err != nil {
		return err
	}
	it := rc.ListRevisions(ctx, &runpb.ListRevisionsRequest{Parent: t.serviceName(appID)})
	var prev string
	for i := 0; i < 2; i++ {
		r, err := it.Next()
		if err != nil {
			break
		}
		prev = r.GetName()
	}
	if prev == "" {
		return fmt.Errorf("cloud-run: no previous revision to roll back to")
	}
	_, err = sc.UpdateService(ctx, &runpb.UpdateServiceRequest{Service: &runpb.Service{
		Name: t.serviceName(appID),
		Traffic: []*runpb.TrafficTarget{{
			Type:     runpb.TrafficTargetAllocationType_TRAFFIC_TARGET_ALLOCATION_TYPE_REVISION,
			Revision: prev,
			Percent:  100,
		}},
	}})
	return err
}

var _ deploytarget.Target = (*Target)(nil)
