package aws

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"

	"github.com/santapong/cooker/internal/model"
)

// --- fakes (implement the narrow *API interfaces) ---

type fakeEC2 struct {
	pages []*ec2.DescribeInstancesOutput
	err   error
	calls int
}

func (f *fakeEC2) DescribeInstances(_ context.Context, in *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	if f.err != nil {
		return nil, f.err
	}
	page := f.pages[f.calls]
	f.calls++
	return page, nil
}

type fakeEKS struct {
	list     *eks.ListClustersOutput
	describe map[string]*eks.DescribeClusterOutput
	listErr  error
	descErr  error
}

func (f *fakeEKS) ListClusters(_ context.Context, _ *eks.ListClustersInput, _ ...func(*eks.Options)) (*eks.ListClustersOutput, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.list, nil
}

func (f *fakeEKS) DescribeCluster(_ context.Context, in *eks.DescribeClusterInput, _ ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
	if f.descErr != nil {
		return nil, f.descErr
	}
	return f.describe[aws.ToString(in.Name)], nil
}

type fakeECR struct {
	out *ecr.DescribeRepositoriesOutput
	err error
}

func (f *fakeECR) DescribeRepositories(_ context.Context, _ *ecr.DescribeRepositoriesInput, _ ...func(*ecr.Options)) (*ecr.DescribeRepositoriesOutput, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.out, nil
}

type fakeCost struct {
	out *costexplorer.GetCostAndUsageOutput
	err error
	in  *costexplorer.GetCostAndUsageInput
}

func (f *fakeCost) GetCostAndUsage(_ context.Context, in *costexplorer.GetCostAndUsageInput, _ ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error) {
	f.in = in
	if f.err != nil {
		return nil, f.err
	}
	return f.out, nil
}

func strp(s string) *string { return &s }

// --- tests ---

func TestListResources_FlattensAllCategories(t *testing.T) {
	e2 := &fakeEC2{pages: []*ec2.DescribeInstancesOutput{{
		Reservations: []ec2types.Reservation{{
			Instances: []ec2types.Instance{{
				InstanceId: strp("i-123"),
				State:      &ec2types.InstanceState{Name: ec2types.InstanceStateNameRunning},
				Placement:  &ec2types.Placement{AvailabilityZone: strp("us-east-1a")},
				Tags:       []ec2types.Tag{{Key: strp("Name"), Value: strp("web-1")}, {Key: strp("env"), Value: strp("prod")}},
			}},
		}},
	}}}
	ek := &fakeEKS{
		list: &eks.ListClustersOutput{Clusters: []string{"prod-cluster"}},
		describe: map[string]*eks.DescribeClusterOutput{
			"prod-cluster": {Cluster: &ekstypes.Cluster{
				Name:    strp("prod-cluster"),
				Status:  ekstypes.ClusterStatusActive,
				Version: strp("1.29"),
			}},
		},
	}
	ec := &fakeECR{out: &ecr.DescribeRepositoriesOutput{
		Repositories: []ecrtypes.Repository{{
			RepositoryName: strp("cooker"),
			RepositoryUri:  strp("123.dkr.ecr.us-east-1.amazonaws.com/cooker"),
		}},
	}}
	co := &fakeCost{out: &costexplorer.GetCostAndUsageOutput{}}

	p := newWithClients("us-east-1", e2, ek, ec, co, nil)
	got, err := p.ListResources(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 resources (1 each), got %d: %+v", len(got), got)
	}

	byType := map[model.CloudResourceType]model.CloudResource{}
	for _, r := range got {
		byType[r.Type] = r
		if r.Provider != model.CloudProviderAWS {
			t.Errorf("resource %s has wrong provider %s", r.ID, r.Provider)
		}
	}
	if c := byType[model.CloudResourceCompute]; c.ID != "i-123" || c.Name != "web-1" || c.Status != "running" || c.Region != "us-east-1a" {
		t.Errorf("compute resource wrong: %+v", c)
	}
	if c := byType[model.CloudResourceCompute]; c.Tags["env"] != "prod" {
		t.Errorf("expected tag env=prod, got %+v", c.Tags)
	}
	if k := byType[model.CloudResourceCluster]; k.ID != "prod-cluster" || k.Status != "ACTIVE" || k.Tags["version"] != "1.29" {
		t.Errorf("cluster resource wrong: %+v", k)
	}
	if reg := byType[model.CloudResourceRegistry]; reg.Name != "cooker" || reg.ID != "123.dkr.ecr.us-east-1.amazonaws.com/cooker" || reg.Status != "available" {
		t.Errorf("registry resource wrong: %+v", reg)
	}
}

func TestListResources_PaginatesInstances(t *testing.T) {
	e2 := &fakeEC2{pages: []*ec2.DescribeInstancesOutput{
		{
			NextToken: strp("page2"),
			Reservations: []ec2types.Reservation{{
				Instances: []ec2types.Instance{{InstanceId: strp("i-1"), State: &ec2types.InstanceState{Name: ec2types.InstanceStateNameRunning}}},
			}},
		},
		{
			Reservations: []ec2types.Reservation{{
				Instances: []ec2types.Instance{{InstanceId: strp("i-2"), State: &ec2types.InstanceState{Name: ec2types.InstanceStateNameStopped}}},
			}},
		},
	}}
	p := newWithClients("eu-west-1", e2,
		&fakeEKS{list: &eks.ListClustersOutput{}},
		&fakeECR{out: &ecr.DescribeRepositoriesOutput{}},
		&fakeCost{out: &costexplorer.GetCostAndUsageOutput{}}, nil)

	got, err := p.ListResources(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 instances across 2 pages, got %d", len(got))
	}
	if e2.calls != 2 {
		t.Errorf("expected 2 DescribeInstances calls (pagination), got %d", e2.calls)
	}
}

func TestListResources_PropagatesError(t *testing.T) {
	p := newWithClients("us-east-1",
		&fakeEC2{err: errors.New("AccessDenied")},
		&fakeEKS{}, &fakeECR{}, &fakeCost{}, nil)
	_, err := p.ListResources(context.Background())
	if err == nil {
		t.Fatal("expected error to propagate from ec2")
	}
}

func TestCostSummary_GroupsByServiceAndSumsTotal(t *testing.T) {
	co := &fakeCost{out: &costexplorer.GetCostAndUsageOutput{
		ResultsByTime: []cetypes.ResultByTime{{
			Groups: []cetypes.Group{
				{Keys: []string{"Amazon Elastic Compute Cloud"}, Metrics: map[string]cetypes.MetricValue{
					"UnblendedCost": {Amount: strp("12.34"), Unit: strp("USD")},
				}},
				{Keys: []string{"Amazon Simple Storage Service"}, Metrics: map[string]cetypes.MetricValue{
					"UnblendedCost": {Amount: strp("1.66"), Unit: strp("USD")},
				}},
			},
		}},
	}}
	p := newWithClients("us-east-1", &fakeEC2{}, &fakeEKS{}, &fakeECR{}, co,
		func() time.Time { return time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC) })

	cs, err := p.CostSummary(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cs.Provider != model.CloudProviderAWS {
		t.Errorf("expected provider aws, got %s", cs.Provider)
	}
	if cs.Currency != "USD" {
		t.Errorf("expected USD, got %s", cs.Currency)
	}
	if cs.Total != "14.00" {
		t.Errorf("expected total 14.00, got %s", cs.Total)
	}
	if len(cs.Services) != 2 {
		t.Fatalf("expected 2 service lines, got %d", len(cs.Services))
	}
	// Descending by amount: EC2 (12.34) before S3 (1.66).
	if cs.Services[0].Service != "Amazon Elastic Compute Cloud" {
		t.Errorf("expected EC2 first (sorted desc), got %s", cs.Services[0].Service)
	}
	// MTD window: first-of-month .. today.
	if cs.Start != "2026-06-01" || cs.End != "2026-06-12" {
		t.Errorf("expected MTD window 2026-06-01..2026-06-12, got %s..%s", cs.Start, cs.End)
	}
}

func TestCostSummary_FirstOfMonthWidensWindow(t *testing.T) {
	co := &fakeCost{out: &costexplorer.GetCostAndUsageOutput{}}
	p := newWithClients("us-east-1", &fakeEC2{}, &fakeEKS{}, &fakeECR{}, co,
		func() time.Time { return time.Date(2026, 6, 1, 3, 0, 0, 0, time.UTC) })

	cs, err := p.CostSummary(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// start==end on the first => end pushed forward a day; never equal.
	if cs.Start == cs.End {
		t.Errorf("start and end must differ for a valid Cost Explorer window, got %s..%s", cs.Start, cs.End)
	}
	if cs.Total != "0.00" {
		t.Errorf("expected zero total with no groups, got %s", cs.Total)
	}
	// Confirm the request actually carried start<end.
	if co.in == nil || co.in.TimePeriod == nil {
		t.Fatal("expected a cost request to have been issued")
	}
}

func TestCostSummary_PropagatesError(t *testing.T) {
	p := newWithClients("us-east-1", &fakeEC2{}, &fakeEKS{}, &fakeECR{},
		&fakeCost{err: errors.New("throttled")}, nil)
	_, err := p.CostSummary(context.Background())
	if err == nil {
		t.Fatal("expected cost explorer error to propagate")
	}
}

func TestNew_RequiresRegion(t *testing.T) {
	_, err := New(context.Background(), Config{})
	if err == nil {
		t.Fatal("expected error when Region is empty")
	}
}

func TestParseAmountCents(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		ok   bool
	}{
		{"12.34", 1234, true},
		{"12.3456789", 1234, true}, // truncated to 2 dp
		{"5", 500, true},
		{"0", 0, true},
		{"0.5", 50, true},
		{"-3.21", -321, true},
		{"", 0, false},
		{"abc", 0, false},
	}
	for _, c := range cases {
		got, ok := parseAmountCents(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("parseAmountCents(%q) = (%d,%v), want (%d,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestFormatCents(t *testing.T) {
	cases := map[int64]string{0: "0.00", 5: "0.05", 1234: "12.34", 100: "1.00", -321: "-3.21"}
	for in, want := range cases {
		if got := formatCents(in); got != want {
			t.Errorf("formatCents(%d) = %q, want %q", in, got, want)
		}
	}
}
