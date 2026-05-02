// Package cloudrun adapts Cooker's deploytarget.Target to Google
// Cloud Run via cloud.google.com/go/run/apiv2.
package cloudrun

import (
	"context"
	"fmt"
	"io"

	run "cloud.google.com/go/run/apiv2"
	"cloud.google.com/go/run/apiv2/runpb"

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

// Deploy creates or updates a Cloud Run Service for spec.AppID and
// waits for the resulting long-running operation to complete.
func (t *Target) Deploy(ctx context.Context, spec deploytarget.Spec) error {
	if err := t.requireConfig(); err != nil {
		return err
	}
	c, err := run.NewServicesClient(ctx)
	if err != nil {
		return fmt.Errorf("cloud-run: services client: %w", err)
	}
	defer c.Close()
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
	c, err := run.NewServicesClient(ctx)
	if err != nil {
		return deploytarget.Status{}, err
	}
	defer c.Close()
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
	sc, err := run.NewServicesClient(ctx)
	if err != nil {
		return err
	}
	defer sc.Close()
	rc, err := run.NewRevisionsClient(ctx)
	if err != nil {
		return err
	}
	defer rc.Close()
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
