// Package deployer defines the deployment abstraction used by the
// pipeline executor. Implementations back this with client-go, Helm,
// kubectl, or a no-op.
package deployer

import (
	"context"
	"errors"
	"io"
)

// ErrUnavailable is returned when the deployment backend is not
// reachable or not configured.
var ErrUnavailable = errors.New("deployer: unavailable")

// ErrCanaryUnsupported is returned by DeployWeighted when the backend
// cannot split traffic for the requested target (e.g. a docker-host or
// cloud-runtime deploy, or a Noop deployer). The service layer maps it
// to HTTP 422 so the UI can explain that canary needs a Kubernetes
// target. Callers should check with errors.Is.
var ErrCanaryUnsupported = errors.New("deployer: canary not supported for this target")

// Kind identifies the flavour of the deployment request.
type Kind string

const (
	KindManifest  Kind = "manifest"   // raw Kubernetes YAML / JSON
	KindHelm      Kind = "helm"       // Helm chart + values
	KindCompose   Kind = "compose"    // docker compose up (whole stack)
	KindDockerRun Kind = "docker-run" // single-service `docker run` on a Docker host
)

// ResourceLimits mirrors model.ResourceLimits in a deployer-local form
// so the package stays free of a model import. The docker deployer maps
// these to --memory / --cpus flags.
type ResourceLimits struct {
	Memory string // compose-native string, e.g. "512m"
	CPUs   string // e.g. "1.5"
}

// Request describes a single deployment. Exactly one of Manifest or
// HelmChart should be set (Kind selects which).
type Request struct {
	Kind      Kind
	Namespace string // Kubernetes namespace; ignored by docker/compose
	// Manifest is raw YAML (possibly multi-doc) when Kind==KindManifest.
	Manifest []byte
	// HelmChart is the chart path or OCI ref when Kind==KindHelm.
	HelmChart string
	// HelmValues, when non-nil, is merged onto the chart's default values.
	HelmValues map[string]interface{}
	// ReleaseName is the Helm release name (required for KindHelm).
	ReleaseName string
	// Image, when non-empty, is substituted into the manifest/values as
	// the container image ref. Convention: ${IMAGE} placeholder in
	// manifests, or .image.repository/tag keys in Helm values. For
	// KindDockerRun it is the image to run.
	Image string
	// Name is the container/service name for KindDockerRun / KindCompose.
	Name string
	// Ports are container ports to publish for KindDockerRun ("p:p").
	Ports []string
	// Env are environment variables to inject for KindDockerRun.
	Env map[string]string
	// Resources, when non-nil, applies CPU/memory limits (KindDockerRun
	// → --memory/--cpus; other kinds may ignore it).
	Resources *ResourceLimits
	// ComposeFile is the path to the compose file for KindCompose.
	ComposeFile string
	// LogWriter, when non-nil, receives a stream of human-readable
	// progress lines from the adapter. At minimum each implementation
	// writes one "Applied <kind>/<name>" line per applied resource so
	// users watching a run on RunPage see what landed. The executor
	// wires this to a capped buffer (persisted to StageRun.Logs) and,
	// when a broadcaster is configured, to the per-stage WebSocket
	// channel for live tailing. Implementations must tolerate a nil
	// LogWriter — callers in tests omit it.
	LogWriter io.Writer
}

// Result reports the outcome of a successful apply.
type Result struct {
	// AppliedResources is a short list of "kind/namespace/name" strings
	// describing what landed in the cluster. Empty for Helm (the chart
	// decides) and compose.
	AppliedResources []string
	// Outputs are optional adapter-emitted outputs, merged over the
	// executor's derived baseline (the executor sets resources from
	// AppliedResources; an adapter may add or override keys here).
	Outputs map[string]string
}

// Deployer applies workloads to a target (Kubernetes cluster, Docker
// host, cloud runtime). Implementations must be safe for concurrent
// use by multiple goroutines.
type Deployer interface {
	Deploy(ctx context.Context, req Request) (Result, error)
}

// WeightedRequest describes a canary traffic split (OR-1). The backend
// runs CanaryImage alongside the stable workload so that approximately
// Weight percent of traffic reaches the new version. On vanilla
// Kubernetes (no service mesh) this is achieved by replica weighting: a
// "<name>-canary" Deployment is sized proportionally to Weight while the
// stable Deployment keeps the remainder, both selected by the same
// Service. A Weight of 100 promotes (stable scaled to zero / canary to
// full); a Weight of 0 aborts (canary removed).
type WeightedRequest struct {
	// Namespace is the Kubernetes namespace to apply into.
	Namespace string
	// Name is the base workload name; the canary Deployment is
	// "<Name>-canary".
	Name string
	// StableImage is the image currently serving production traffic.
	StableImage string
	// CanaryImage is the new image under evaluation.
	CanaryImage string
	// Weight is the target traffic percent (0–100) for CanaryImage.
	Weight int
	// Replicas is the total desired replica count across stable+canary
	// (the pool the weight is split over). Zero means a sane default.
	Replicas int
	// LogWriter, when non-nil, receives human-readable progress lines.
	// Implementations must tolerate a nil LogWriter.
	LogWriter io.Writer
}

// WeightedResult reports the realised split after a DeployWeighted call.
type WeightedResult struct {
	// CanaryReplicas / StableReplicas are the replica counts the backend
	// actually set (after rounding the weight onto whole pods).
	CanaryReplicas int
	StableReplicas int
	// AppliedResources lists "kind/namespace/name" strings for what landed.
	AppliedResources []string
}

// WeightedDeployer is the optional capability a Deployer advertises when
// it can split traffic between a stable and a canary workload. Only the
// Kubernetes-backed deployers (Kubectl, ClientGo) implement it; the
// service layer type-asserts for it and returns ErrCanaryUnsupported
// when the configured deployer does not. Kept separate from Deployer so
// non-K8s backends don't have to stub a method they can't honour.
type WeightedDeployer interface {
	Deployer
	// DeployWeighted establishes (or re-balances) the canary split. It is
	// idempotent: calling it again with the same Weight is a no-op apply.
	DeployWeighted(ctx context.Context, req WeightedRequest) (WeightedResult, error)
}

// CanaryProber is the optional capability a WeightedDeployer advertises
// when it can report the readiness of the "<name>-canary" Deployment
// directly (PM26-07-04). The canary service prefers this over the
// app-level health prober during the soak window: the app prober hits
// the shared Service and is dominated by the stable pods, so a
// crash-looping canary would read healthy. The K8s deployers implement
// it (client-go Get / kubectl); non-K8s backends do not.
type CanaryProber interface {
	// CanaryReady reports whether the <name>-canary Deployment in
	// namespace has all its desired replicas ready (and at least one
	// pod). detail is a human-readable summary (e.g. "2/2 ready"). A
	// non-nil error means readiness could not be determined — the caller
	// should fall back to the app prober rather than treat it as
	// unhealthy.
	CanaryReady(ctx context.Context, namespace, name string) (ready bool, detail string, err error)
}
