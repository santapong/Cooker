// Package ecs adapts Cooker's deploytarget.Target to AWS ECS / Fargate.
//
// The adapter is intentionally minimal: it updates an existing
// service to point at a new task-definition revision. Operators
// supply the cluster + service via DeployTarget config; Cooker
// registers a new task definition per deploy and calls
// UpdateService to roll it out.
package ecs

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"

	"github.com/santapong/cooker/internal/deploytarget"
	"github.com/santapong/cooker/internal/model"
)

// Target is the ECS adapter.
type Target struct {
	Region  string
	Cluster string
	// TaskRole is the IAM role the running task assumes.
	TaskRole string
	// ExecutionRole is the IAM role ECS uses to pull from ECR / send
	// logs to CloudWatch.
	ExecutionRole string
	// Subnets / SecurityGroups configure the awsvpc network mode
	// required by Fargate.
	Subnets        []string
	SecurityGroups []string
}

// New constructs an ECS target.
func New(region, cluster string) *Target {
	return &Target{Region: region, Cluster: cluster}
}

func (*Target) Kind() model.DeployTargetKind { return model.DeployTargetECS }

func (t *Target) requireConfig() error {
	if t == nil || t.Region == "" || t.Cluster == "" {
		return fmt.Errorf("%w: ecs: Region + Cluster required", deploytarget.ErrUnavailable)
	}
	return nil
}

func (t *Target) client(ctx context.Context) (*ecs.Client, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(t.Region))
	if err != nil {
		return nil, err
	}
	return ecs.NewFromConfig(cfg), nil
}

// Deploy registers a new task definition and updates the service to
// run it. Service name is taken from spec.AppID.
func (t *Target) Deploy(ctx context.Context, spec deploytarget.Spec) error {
	if err := t.requireConfig(); err != nil {
		return err
	}
	c, err := t.client(ctx)
	if err != nil {
		return err
	}

	envVars := make([]ecstypes.KeyValuePair, 0, len(spec.Env))
	for k, v := range spec.Env {
		envVars = append(envVars, ecstypes.KeyValuePair{
			Name:  aws.String(k),
			Value: aws.String(v),
		})
	}
	portMappings := make([]ecstypes.PortMapping, 0, len(spec.Ports))
	for _, p := range spec.Ports {
		portMappings = append(portMappings, ecstypes.PortMapping{
			ContainerPort: aws.Int32(int32(p)),
			Protocol:      ecstypes.TransportProtocolTcp,
		})
	}

	td, err := c.RegisterTaskDefinition(ctx, &ecs.RegisterTaskDefinitionInput{
		Family:                  aws.String(spec.AppID),
		NetworkMode:             ecstypes.NetworkModeAwsvpc,
		RequiresCompatibilities: []ecstypes.Compatibility{ecstypes.CompatibilityFargate},
		Cpu:                     aws.String("256"),
		Memory:                  aws.String("512"),
		ExecutionRoleArn:        aws.String(t.ExecutionRole),
		TaskRoleArn:             aws.String(t.TaskRole),
		ContainerDefinitions: []ecstypes.ContainerDefinition{{
			Name:         aws.String(spec.AppID),
			Image:        aws.String(spec.Image),
			Essential:    aws.Bool(true),
			Environment:  envVars,
			PortMappings: portMappings,
		}},
	})
	if err != nil {
		return fmt.Errorf("ecs: register task definition: %w", err)
	}
	tdArn := aws.ToString(td.TaskDefinition.TaskDefinitionArn)

	desired := int32(spec.Replicas)
	if desired == 0 {
		desired = 1
	}
	// Try update; fall through to CreateService only on ServiceNotFoundException.
	// Any other error (IAM denied, throttling, wrong cluster ARN) is returned
	// immediately — silently falling through would create a ghost Fargate service.
	if _, uerr := c.UpdateService(ctx, &ecs.UpdateServiceInput{
		Cluster:        aws.String(t.Cluster),
		Service:        aws.String(spec.AppID),
		TaskDefinition: aws.String(tdArn),
		DesiredCount:   aws.Int32(desired),
	}); uerr != nil {
		var notFound *ecstypes.ServiceNotFoundException
		if !errors.As(uerr, &notFound) {
			return fmt.Errorf("ecs: update service: %w", uerr)
		}
		// Service does not exist yet — fall through to create.
	} else {
		return nil
	}
	_, err = c.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String(t.Cluster),
		ServiceName:    aws.String(spec.AppID),
		TaskDefinition: aws.String(tdArn),
		DesiredCount:   aws.Int32(desired),
		LaunchType:     ecstypes.LaunchTypeFargate,
		NetworkConfiguration: &ecstypes.NetworkConfiguration{
			AwsvpcConfiguration: &ecstypes.AwsVpcConfiguration{
				Subnets:        t.Subnets,
				SecurityGroups: t.SecurityGroups,
				AssignPublicIp: ecstypes.AssignPublicIpEnabled,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("ecs: create service: %w", err)
	}
	return nil
}

func (t *Target) Status(ctx context.Context, appID string) (deploytarget.Status, error) {
	if err := t.requireConfig(); err != nil {
		return deploytarget.Status{}, err
	}
	c, err := t.client(ctx)
	if err != nil {
		return deploytarget.Status{}, err
	}
	out, err := c.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String(t.Cluster),
		Services: []string{appID},
	})
	if err != nil || len(out.Services) == 0 {
		return deploytarget.Status{}, err
	}
	svc := out.Services[0]
	return deploytarget.Status{
		Healthy:  aws.ToInt32(&svc.RunningCount) >= aws.ToInt32(&svc.DesiredCount) && aws.ToInt32(&svc.DesiredCount) > 0,
		Replicas: int(svc.RunningCount),
	}, nil
}

func (t *Target) Logs(_ context.Context, _ string, _ io.Writer) error {
	return fmt.Errorf("%w: ecs logs: CloudWatch Logs streaming not yet wired", deploytarget.ErrUnavailable)
}

func (t *Target) Rollback(ctx context.Context, appID string) error {
	if err := t.requireConfig(); err != nil {
		return err
	}
	c, err := t.client(ctx)
	if err != nil {
		return err
	}
	out, err := c.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String(t.Cluster),
		Services: []string{appID},
	})
	if err != nil || len(out.Services) == 0 {
		return fmt.Errorf("ecs: service %q not found", appID)
	}
	svc := out.Services[0]
	cur := aws.ToString(svc.TaskDefinition)
	prev, err := previousTaskDefRevision(cur)
	if err != nil {
		return err
	}
	_, err = c.UpdateService(ctx, &ecs.UpdateServiceInput{
		Cluster:        aws.String(t.Cluster),
		Service:        aws.String(appID),
		TaskDefinition: aws.String(prev),
	})
	return err
}

// previousTaskDefRevision returns the ARN of revision n-1 from a
// task-definition ARN like ".../family:7" -> ".../family:6".
func previousTaskDefRevision(arn string) (string, error) {
	for i := len(arn) - 1; i >= 0; i-- {
		if arn[i] == ':' {
			rev := arn[i+1:]
			r, err := atoi(rev)
			if err != nil || r <= 1 {
				return "", fmt.Errorf("ecs: cannot derive previous revision from %q", arn)
			}
			return arn[:i+1] + itoa(r-1), nil
		}
	}
	return "", fmt.Errorf("ecs: malformed task-def arn %q", arn)
}

func atoi(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a number: %q", s)
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

var _ deploytarget.Target = (*Target)(nil)
