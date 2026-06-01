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
}

// Deployer applies workloads to a target (Kubernetes cluster, Docker
// host, cloud runtime). Implementations must be safe for concurrent
// use by multiple goroutines.
type Deployer interface {
	Deploy(ctx context.Context, req Request) (Result, error)
}
