// Package gcp implements cloudinventory.Provider against Google Cloud
// using the google.golang.org/api REST client libraries. It is strictly
// read-only: it calls compute.instances.aggregatedList,
// container.projects.locations.clusters.list, and
// artifactregistry.projects.locations.repositories.list — never a
// mutating API.
//
// Auth mirrors the gcpsm secrets backend and the cloudrun deploy target:
// Application Default Credentials by default (Workload Identity on GKE,
// or GOOGLE_APPLICATION_CREDENTIALS pointing at a service-account key).
// The caller may also pass an explicit credentials-JSON blob via Config
// for runtimes that can't supply ADC.
//
// Cost note: GCP exposes month-to-date spend only through the BigQuery
// billing export (or the Budgets API), NOT through the Cloud Billing v1
// API used for account/SKU metadata. Rather than fabricate a figure,
// CostSummary returns a zero, clearly-labelled summary. Wiring the
// BigQuery export is tracked as follow-up (see docs/reference/design.md).
package gcp

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	artifactregistry "google.golang.org/api/artifactregistry/v1"
	compute "google.golang.org/api/compute/v1"
	container "google.golang.org/api/container/v1"
	"google.golang.org/api/option"

	"github.com/santapong/cooker/internal/model"
)

// listFunc is the read seam for one resource category. Real
// constructors fill these with SDK-backed closures; tests inject fakes.
type listFunc func(ctx context.Context) ([]model.CloudResource, error)

// Provider is the GCP cloudinventory.Provider.
type Provider struct {
	projectID string

	listInstances listFunc
	listClusters  listFunc
	listRepos     listFunc

	now func() time.Time
}

// Config holds the SDK-construction inputs. ProjectID is required.
// CredentialsJSON is an optional service-account key (JSON bytes); when
// empty, Application Default Credentials are used (matching gcpsm).
type Config struct {
	ProjectID       string
	CredentialsJSON string
}

// New constructs a GCP provider, building the compute, container, and
// artifact-registry REST services from ADC (plus any explicit
// credentials in cfg). ctx is used only for service construction;
// per-call contexts flow through the Provider methods.
func New(ctx context.Context, cfg Config) (*Provider, error) {
	if cfg.ProjectID == "" {
		return nil, fmt.Errorf("gcp: ProjectID is required")
	}
	opts := clientOptions(cfg.CredentialsJSON)

	computeSvc, err := compute.NewService(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("gcp: compute service: %w", err)
	}
	containerSvc, err := container.NewService(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("gcp: container service: %w", err)
	}
	arSvc, err := artifactregistry.NewService(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("gcp: artifact registry service: %w", err)
	}

	p := &Provider{projectID: cfg.ProjectID, now: time.Now}
	p.listInstances = computeInstancesLister(computeSvc, cfg.ProjectID)
	p.listClusters = gkeClustersLister(containerSvc, cfg.ProjectID)
	p.listRepos = artifactReposLister(arSvc, cfg.ProjectID)
	return p, nil
}

// clientOptions builds the option slice shared by all three services.
// An explicit credentials JSON blob wins; otherwise the libraries fall
// back to ADC.
func clientOptions(credentialsJSON string) []option.ClientOption {
	if strings.TrimSpace(credentialsJSON) != "" {
		return []option.ClientOption{option.WithCredentialsJSON([]byte(credentialsJSON))}
	}
	return nil
}

// newWithListers builds a Provider over already-constructed list seams.
// Used by tests to exercise the flattening/aggregation logic without
// real cloud access; production goes through New.
func newWithListers(projectID string, instances, clusters, repos listFunc, now func() time.Time) *Provider {
	if now == nil {
		now = time.Now
	}
	return &Provider{
		projectID:     projectID,
		listInstances: instances,
		listClusters:  clusters,
		listRepos:     repos,
		now:           now,
	}
}

// Name reports the GCP provider identity.
func (p *Provider) Name() model.CloudProvider { return model.CloudProviderGCP }

// ListResources gathers Compute instances, GKE clusters, and Artifact
// Registry repositories. A failure in any category fails the whole call
// (the service layer isolates it to this provider).
func (p *Provider) ListResources(ctx context.Context) ([]model.CloudResource, error) {
	var out []model.CloudResource
	for _, fn := range []listFunc{p.listInstances, p.listClusters, p.listRepos} {
		if fn == nil {
			continue
		}
		rs, err := fn(ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, rs...)
	}
	sortByName(out)
	return out, nil
}

// CostSummary returns a zero, clearly-labelled month-to-date summary.
// GCP's spend data lives in the BigQuery billing export, which is not
// wired here (see the package doc). Returning a clean zero rather than
// an error keeps the GCP provider "healthy" in the UI — the panel shows
// resources and a "cost export not configured" note instead of a red
// per-provider banner.
func (p *Provider) CostSummary(_ context.Context) (model.CostSummary, error) {
	now := p.now().UTC()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return model.CostSummary{
		Provider: model.CloudProviderGCP,
		Currency: "USD",
		Total:    "0.00",
		Start:    start.Format("2006-01-02"),
		End:      now.Format("2006-01-02"),
		Services: nil,
	}, nil
}

// --- SDK-backed listers ---

func computeInstancesLister(svc *compute.Service, projectID string) listFunc {
	return func(ctx context.Context) ([]model.CloudResource, error) {
		var out []model.CloudResource
		call := svc.Instances.AggregatedList(projectID)
		err := call.Pages(ctx, func(page *compute.InstanceAggregatedList) error {
			for _, scoped := range page.Items {
				for _, inst := range scoped.Instances {
					out = append(out, model.CloudResource{
						Provider: model.CloudProviderGCP,
						Type:     model.CloudResourceCompute,
						ID:       fmt.Sprintf("%d", inst.Id),
						Name:     inst.Name,
						Region:   lastPathSegment(inst.Zone),
						Status:   inst.Status,
						Tags:     nonEmptyLabels(inst.Labels),
					})
				}
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("gcp: compute aggregated list: %w", err)
		}
		return out, nil
	}
}

func gkeClustersLister(svc *container.Service, projectID string) listFunc {
	return func(ctx context.Context) ([]model.CloudResource, error) {
		// "-" as the location wildcard lists clusters across all
		// regions/zones in one call.
		parent := fmt.Sprintf("projects/%s/locations/-", projectID)
		resp, err := svc.Projects.Locations.Clusters.List(parent).Context(ctx).Do()
		if err != nil {
			return nil, fmt.Errorf("gcp: gke list clusters: %w", err)
		}
		var out []model.CloudResource
		for _, cl := range resp.Clusters {
			tags := nonEmptyLabels(cl.ResourceLabels)
			if cl.CurrentMasterVersion != "" {
				if tags == nil {
					tags = map[string]string{}
				}
				tags["version"] = cl.CurrentMasterVersion
			}
			out = append(out, model.CloudResource{
				Provider: model.CloudProviderGCP,
				Type:     model.CloudResourceCluster,
				ID:       cl.Name,
				Name:     cl.Name,
				Region:   cl.Location,
				Status:   cl.Status,
				Tags:     tags,
			})
		}
		return out, nil
	}
}

func artifactReposLister(svc *artifactregistry.Service, projectID string) listFunc {
	return func(ctx context.Context) ([]model.CloudResource, error) {
		// "-" lists repositories across all locations.
		parent := fmt.Sprintf("projects/%s/locations/-", projectID)
		var out []model.CloudResource
		err := svc.Projects.Locations.Repositories.List(parent).Pages(ctx, func(page *artifactregistry.ListRepositoriesResponse) error {
			for _, repo := range page.Repositories {
				out = append(out, model.CloudResource{
					Provider: model.CloudProviderGCP,
					Type:     model.CloudResourceRegistry,
					ID:       repo.Name,
					Name:     lastPathSegment(repo.Name),
					Region:   artifactRepoLocation(repo.Name),
					// Format (DOCKER/MAVEN/...) is the closest analog to a
					// status for a registry; pass it through.
					Status: repo.Format,
					Tags:   nonEmptyLabels(repo.Labels),
				})
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("gcp: artifact registry list repositories: %w", err)
		}
		return out, nil
	}
}

// sortByName gives the GCP resource list a deterministic order
// (providers may return zones/locations in arbitrary order).
func sortByName(rs []model.CloudResource) {
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].Name < rs[j].Name })
}

// lastPathSegment returns the final segment of a slash- (or
// fully-qualified-URL-) delimited GCP resource path. GCP returns zones
// and resource names as URLs/paths; the UI wants the bare name.
func lastPathSegment(s string) string {
	if s == "" {
		return ""
	}
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// artifactRepoLocation extracts the location from an Artifact Registry
// resource name of the form
// "projects/<p>/locations/<loc>/repositories/<name>".
func artifactRepoLocation(name string) string {
	parts := strings.Split(name, "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "locations" {
			return parts[i+1]
		}
	}
	return ""
}

// nonEmptyLabels returns m, or nil when m is empty, so the JSON output
// omits an empty tags object.
func nonEmptyLabels(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	return m
}
