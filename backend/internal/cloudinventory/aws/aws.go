// Package aws implements cloudinventory.Provider against AWS using the
// AWS SDK for Go v2. It is strictly read-only: it calls
// EC2:DescribeInstances, EKS:ListClusters/DescribeCluster,
// ECR:DescribeRepositories, and CostExplorer:GetCostAndUsage — never a
// mutating API.
//
// Auth mirrors the awsm secrets backend and the ecs deploy target:
// config.LoadDefaultConfig picks up the standard AWS credential chain
// (IRSA / instance profile / env vars / shared credentials). The
// caller may also pass explicit static credentials via Config when the
// runtime can't supply them through the chain.
package aws

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/eks"

	"github.com/santapong/cooker/internal/model"
)

// ec2API / eksAPI / ecrAPI / costAPI are the narrow slices of each SDK
// client the provider uses. Defining them as interfaces lets tests
// inject fakes (or endpoint-overridden real clients) without real cloud
// access; production wires the concrete *ec2.Client etc.
type ec2API interface {
	DescribeInstances(ctx context.Context, in *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
}

type eksAPI interface {
	ListClusters(ctx context.Context, in *eks.ListClustersInput, optFns ...func(*eks.Options)) (*eks.ListClustersOutput, error)
	DescribeCluster(ctx context.Context, in *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error)
}

type ecrAPI interface {
	DescribeRepositories(ctx context.Context, in *ecr.DescribeRepositoriesInput, optFns ...func(*ecr.Options)) (*ecr.DescribeRepositoriesOutput, error)
}

type costAPI interface {
	GetCostAndUsage(ctx context.Context, in *costexplorer.GetCostAndUsageInput, optFns ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error)
}

// Provider is the AWS cloudinventory.Provider.
type Provider struct {
	region string
	ec2    ec2API
	eks    eksAPI
	ecr    ecrAPI
	cost   costAPI
	// now is injected for tests; defaults to time.Now.
	now func() time.Time
}

// Config holds the SDK-construction inputs. Region is required (Cost
// Explorer and the regional services need it). AccessKeyID/SecretAccessKey
// are optional explicit static credentials; when empty the standard AWS
// chain is used (IRSA / instance profile / env / shared config), exactly
// like the awsm backend.
type Config struct {
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

// New constructs an AWS provider, building the SDK clients from the
// standard credential chain (plus any explicit static credentials in
// cfg). ctx is used only for the config load; per-call contexts flow
// through the Provider methods.
func New(ctx context.Context, cfg Config) (*Provider, error) {
	if cfg.Region == "" {
		return nil, fmt.Errorf("aws: Region is required")
	}
	loadOpts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.Region),
	}
	if cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" {
		loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken),
		))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("aws: load config: %w", err)
	}
	return &Provider{
		region: cfg.Region,
		ec2:    ec2.NewFromConfig(awsCfg),
		eks:    eks.NewFromConfig(awsCfg),
		ecr:    ecr.NewFromConfig(awsCfg),
		cost:   costexplorer.NewFromConfig(awsCfg),
		now:    time.Now,
	}, nil
}

// newWithClients builds a Provider over already-constructed (possibly
// fake or endpoint-overridden) SDK clients. Used by tests to avoid real
// cloud access; production goes through New.
func newWithClients(region string, e2 ec2API, ek eksAPI, ec ecrAPI, co costAPI, now func() time.Time) *Provider {
	if now == nil {
		now = time.Now
	}
	return &Provider{region: region, ec2: e2, eks: ek, ecr: ec, cost: co, now: now}
}

// Name reports the AWS provider identity.
func (p *Provider) Name() model.CloudProvider { return model.CloudProviderAWS }

// ListResources gathers EC2 instances, EKS clusters, and ECR
// repositories. A failure in any one category fails the whole call
// (the service layer isolates it to this provider).
func (p *Provider) ListResources(ctx context.Context) ([]model.CloudResource, error) {
	var out []model.CloudResource

	instances, err := p.listInstances(ctx)
	if err != nil {
		return nil, err
	}
	out = append(out, instances...)

	clusters, err := p.listClusters(ctx)
	if err != nil {
		return nil, err
	}
	out = append(out, clusters...)

	repos, err := p.listRepositories(ctx)
	if err != nil {
		return nil, err
	}
	out = append(out, repos...)

	return out, nil
}

func (p *Provider) listInstances(ctx context.Context) ([]model.CloudResource, error) {
	var out []model.CloudResource
	var next *string
	for {
		page, err := p.ec2.DescribeInstances(ctx, &ec2.DescribeInstancesInput{NextToken: next})
		if err != nil {
			return nil, fmt.Errorf("aws: ec2 describe instances: %w", err)
		}
		for _, res := range page.Reservations {
			for _, inst := range res.Instances {
				id := aws.ToString(inst.InstanceId)
				tags := map[string]string{}
				name := id
				for _, tg := range inst.Tags {
					k, v := aws.ToString(tg.Key), aws.ToString(tg.Value)
					if k == "" {
						continue
					}
					tags[k] = v
					if k == "Name" && v != "" {
						name = v
					}
				}
				status := ""
				if inst.State != nil {
					status = string(inst.State.Name)
				}
				region := p.region
				if inst.Placement != nil && aws.ToString(inst.Placement.AvailabilityZone) != "" {
					region = aws.ToString(inst.Placement.AvailabilityZone)
				}
				if len(tags) == 0 {
					tags = nil
				}
				out = append(out, model.CloudResource{
					Provider: model.CloudProviderAWS,
					Type:     model.CloudResourceCompute,
					ID:       id,
					Name:     name,
					Region:   region,
					Status:   status,
					Tags:     tags,
				})
			}
		}
		if page.NextToken == nil {
			break
		}
		next = page.NextToken
	}
	return out, nil
}

func (p *Provider) listClusters(ctx context.Context) ([]model.CloudResource, error) {
	var out []model.CloudResource
	var next *string
	for {
		page, err := p.eks.ListClusters(ctx, &eks.ListClustersInput{NextToken: next})
		if err != nil {
			return nil, fmt.Errorf("aws: eks list clusters: %w", err)
		}
		for _, name := range page.Clusters {
			n := name
			desc, err := p.eks.DescribeCluster(ctx, &eks.DescribeClusterInput{Name: &n})
			if err != nil {
				return nil, fmt.Errorf("aws: eks describe cluster %q: %w", n, err)
			}
			status, version := "", ""
			tags := map[string]string{}
			if desc.Cluster != nil {
				status = string(desc.Cluster.Status)
				version = aws.ToString(desc.Cluster.Version)
				for k, v := range desc.Cluster.Tags {
					tags[k] = v
				}
			}
			if version != "" {
				tags["version"] = version
			}
			if len(tags) == 0 {
				tags = nil
			}
			out = append(out, model.CloudResource{
				Provider: model.CloudProviderAWS,
				Type:     model.CloudResourceCluster,
				ID:       n,
				Name:     n,
				Region:   p.region,
				Status:   status,
				Tags:     tags,
			})
		}
		if page.NextToken == nil {
			break
		}
		next = page.NextToken
	}
	return out, nil
}

func (p *Provider) listRepositories(ctx context.Context) ([]model.CloudResource, error) {
	var out []model.CloudResource
	var next *string
	for {
		page, err := p.ecr.DescribeRepositories(ctx, &ecr.DescribeRepositoriesInput{NextToken: next})
		if err != nil {
			return nil, fmt.Errorf("aws: ecr describe repositories: %w", err)
		}
		for _, repo := range page.Repositories {
			out = append(out, model.CloudResource{
				Provider: model.CloudProviderAWS,
				Type:     model.CloudResourceRegistry,
				ID:       aws.ToString(repo.RepositoryUri),
				Name:     aws.ToString(repo.RepositoryName),
				Region:   p.region,
				// ECR repositories have no lifecycle state; "available"
				// is the honest constant rather than an empty cell.
				Status: "available",
			})
		}
		if page.NextToken == nil {
			break
		}
		next = page.NextToken
	}
	return out, nil
}

// CostSummary returns month-to-date spend grouped by AWS service via
// Cost Explorer. The window is [first-of-month, today-exclusive]; on the
// first of the month that window is empty, so we widen End to tomorrow
// to always return a valid (possibly near-zero) figure.
func (p *Provider) CostSummary(ctx context.Context) (model.CostSummary, error) {
	now := p.now().UTC()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := now
	if !end.After(start) {
		end = start.Add(24 * time.Hour)
	}
	startStr := start.Format("2006-01-02")
	endStr := end.Format("2006-01-02")
	if startStr == endStr {
		// Same calendar day (first of month): Cost Explorer requires
		// start < end, so push end one day forward.
		endStr = end.Add(24 * time.Hour).Format("2006-01-02")
	}

	in := &costexplorer.GetCostAndUsageInput{
		TimePeriod:  &cetypes.DateInterval{Start: &startStr, End: &endStr},
		Granularity: cetypes.GranularityMonthly,
		Metrics:     []string{"UnblendedCost"},
		GroupBy: []cetypes.GroupDefinition{{
			Type: cetypes.GroupDefinitionTypeDimension,
			Key:  aws.String("SERVICE"),
		}},
	}
	res, err := p.cost.GetCostAndUsage(ctx, in)
	if err != nil {
		return model.CostSummary{}, fmt.Errorf("aws: cost explorer get cost and usage: %w", err)
	}

	summary := model.CostSummary{
		Provider: model.CloudProviderAWS,
		Currency: "USD",
		Start:    startStr,
		End:      endStr,
	}
	var totalCents int64
	haveTotal := false
	for _, rbt := range res.ResultsByTime {
		for _, g := range rbt.Groups {
			service := "unknown"
			if len(g.Keys) > 0 {
				service = g.Keys[0]
			}
			mv, ok := g.Metrics["UnblendedCost"]
			if !ok {
				continue
			}
			amount := aws.ToString(mv.Amount)
			unit := aws.ToString(mv.Unit)
			if unit != "" {
				summary.Currency = unit
			}
			summary.Services = append(summary.Services, model.CostServiceLine{
				Service:  service,
				Amount:   amount,
				Currency: summary.Currency,
			})
			if c, ok := parseAmountCents(amount); ok {
				totalCents += c
				haveTotal = true
			}
		}
	}
	// Stable, descending-by-amount ordering so the panel is deterministic.
	sort.SliceStable(summary.Services, func(i, j int) bool {
		ai, _ := parseAmountCents(summary.Services[i].Amount)
		aj, _ := parseAmountCents(summary.Services[j].Amount)
		return ai > aj
	})
	if haveTotal {
		summary.Total = formatCents(totalCents)
	} else {
		summary.Total = "0.00"
	}
	return summary, nil
}
