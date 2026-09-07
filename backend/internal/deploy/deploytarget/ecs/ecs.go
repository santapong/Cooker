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
	"math"
	"strconv"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"

	"github.com/santapong/cooker/internal/deploy/deploytarget"
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

	// AD-H1: cache the ECS client so we don't re-resolve credentials /
	// hit IMDS on every Deploy/Status/Rollback call. sync.Once
	// serialises the one-shot init; initErr captures any failure.
	clientOnce sync.Once
	cachedCli  *ecs.Client
	clientErr  error
}

// New constructs an ECS target.
func New(region, cluster string) *Target {
	return &Target{Region: region, Cluster: cluster}
}

func (*Target) Kind() model.DeployTargetKind { return model.DeployTargetECS }

func (t *Target) Validate() error {
	if err := t.requireConfig(); err != nil {
		return err
	}
	if len(t.Subnets) == 0 || len(t.SecurityGroups) == 0 || t.ExecutionRole == "" {
		return fmt.Errorf("ECS needs configured subnets, security groups and an execution role")
	}
	return nil
}

// TaskSize selects the smallest Linux Fargate task fitting requested limits.
// Both preview and execution call this function so sizing is reviewable.
func TaskSize(r *model.ResourceLimits) (int32, int32, error) {
	cpu, memory := int32(256), int32(512)
	if r != nil {
		if r.NanoCPUs < 0 || r.MemoryBytes < 0 {
			return 0, 0, fmt.Errorf("resource limits must be positive")
		}
		if r.NanoCPUs > 16e9 || r.MemoryBytes > 120*1024*1024*1024 {
			return 0, 0, fmt.Errorf("requested resources exceed supported Fargate task sizes")
		}
		if r.NanoCPUs > 0 {
			cpu = int32(math.Ceil(float64(r.NanoCPUs) / 1e9 * 1024))
		}
		if r.MemoryBytes > 0 {
			memory = int32(math.Ceil(float64(r.MemoryBytes) / (1024 * 1024)))
		}
	}
	for _, size := range []struct{ cpu, min, max, step int32 }{{256, 512, 2048, 512}, {512, 1024, 4096, 1024}, {1024, 2048, 8192, 1024}, {2048, 4096, 16384, 1024}, {4096, 8192, 30720, 1024}, {8192, 16384, 61440, 4096}, {16384, 32768, 122880, 8192}} {
		if size.cpu < cpu || size.max < memory {
			continue
		}
		m := size.min
		for m < memory {
			m += size.step
		}
		if size.cpu == 256 && m == 1536 {
			m = 2048
		}
		return size.cpu, m, nil
	}
	return 0, 0, fmt.Errorf("requested resources exceed supported Fargate task sizes")
}

func (t *Target) requireConfig() error {
	if t == nil || t.Region == "" || t.Cluster == "" {
		return fmt.Errorf("%w: ecs: Region + Cluster required", deploytarget.ErrUnavailable)
	}
	return nil
}

// client returns the cached ECS client, constructing it on the first
// call. Subsequent calls return the same client without hitting IMDS or
// re-resolving credentials (AD-H1). The background context is used for
// the one-shot SDK init; per-call operations still use their own ctx.
func (t *Target) client(ctx context.Context) (*ecs.Client, error) {
	t.clientOnce.Do(func() {
		cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(t.Region))
		if err != nil {
			t.clientErr = err
			return
		}
		t.cachedCli = ecs.NewFromConfig(cfg)
	})
	return t.cachedCli, t.clientErr
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

	cpu, memory, err := TaskSize(spec.Resources)
	if err != nil {
		return err
	}
	var health *ecstypes.HealthCheck
	if h := spec.HealthCheck; h != nil {
		if h.Interval < 5 || h.Interval > 300 || h.Timeout < 2 || h.Timeout > 60 || h.Retries < 1 || h.Retries > 10 || h.StartPeriod < 0 || h.StartPeriod > 300 {
			return fmt.Errorf("healthcheck values exceed ECS limits")
		}
		health = &ecstypes.HealthCheck{Command: h.Command, Interval: aws.Int32(h.Interval), Timeout: aws.Int32(h.Timeout), Retries: aws.Int32(h.Retries), StartPeriod: aws.Int32(h.StartPeriod)}
	}
	td, err := c.RegisterTaskDefinition(ctx, &ecs.RegisterTaskDefinitionInput{
		Family:                  aws.String(spec.AppID),
		NetworkMode:             ecstypes.NetworkModeAwsvpc,
		RequiresCompatibilities: []ecstypes.Compatibility{ecstypes.CompatibilityFargate},
		Cpu:                     aws.String(strconv.Itoa(int(cpu))),
		Memory:                  aws.String(strconv.Itoa(int(memory))),
		ExecutionRoleArn:        aws.String(t.ExecutionRole),
		TaskRoleArn:             aws.String(t.TaskRole),
		ContainerDefinitions: []ecstypes.ContainerDefinition{{
			Name:         aws.String(spec.AppID),
			Image:        aws.String(spec.Image),
			Essential:    aws.Bool(true),
			Environment:  envVars,
			Command:      spec.Command,
			HealthCheck:  health,
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
		Cluster:         aws.String(t.Cluster),
		ServiceName:     aws.String(spec.AppID),
		TaskDefinition:  aws.String(tdArn),
		DesiredCount:    aws.Int32(desired),
		LaunchType:      ecstypes.LaunchTypeFargate,
		PlatformVersion: aws.String("LATEST"),
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
	if err != nil {
		return deploytarget.Status{}, err
	}
	if len(out.Services) == 0 || len(out.Failures) > 0 {
		return deploytarget.Status{}, fmt.Errorf("ECS service is unavailable")
	}
	svc := out.Services[0]
	stable := svc.PendingCount == 0 && len(svc.Deployments) <= 1
	for _, d := range svc.Deployments {
		if d.RolloutState == ecstypes.DeploymentRolloutStateFailed {
			return deploytarget.Status{}, fmt.Errorf("ECS rollout failed")
		}
		if d.RolloutState == ecstypes.DeploymentRolloutStateInProgress {
			stable = false
		}
	}
	return deploytarget.Status{
		Healthy:  stable && svc.RunningCount >= svc.DesiredCount && svc.DesiredCount > 0,
		Replicas: int(svc.RunningCount),
	}, nil
}

func (t *Target) VerifyImage(ctx context.Context, name, image string) error {
	c, err := t.client(ctx)
	if err != nil {
		return err
	}
	res, err := c.DescribeServices(ctx, &ecs.DescribeServicesInput{Cluster: aws.String(t.Cluster), Services: []string{name}})
	if err != nil {
		return err
	}
	if len(res.Services) != 1 {
		return fmt.Errorf("ECS service missing during image verification")
	}
	td, err := c.DescribeTaskDefinition(ctx, &ecs.DescribeTaskDefinitionInput{TaskDefinition: res.Services[0].TaskDefinition})
	if err != nil {
		return err
	}
	if td.TaskDefinition == nil || len(td.TaskDefinition.ContainerDefinitions) != 1 || aws.ToString(td.TaskDefinition.ContainerDefinitions[0].Image) != image {
		return fmt.Errorf("ECS is not running the requested image revision")
	}
	return nil
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
