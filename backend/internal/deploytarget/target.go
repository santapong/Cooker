// Package deploytarget defines Cooker's pluggable deploy-backend
// interface. A Target adapts one runtime (Kubernetes cluster,
// Docker host, Cloud Run, ECS/Fargate, ...) behind a small verb set
// so the App "Deploy" button can speak one shape regardless of
// where the App lands.
//
// Adapters live in sibling packages (deploytarget/kubernetes,
// deploytarget/dockerhost, deploytarget/cloudrun) and register
// themselves with Register so the server can look them up by Kind.
package deploytarget

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/cooker-ci/cooker/internal/model"
)

// ErrUnavailable is returned when a target backend has no
// credentials, no network, or is not implemented in this build.
var ErrUnavailable = errors.New("deploytarget: unavailable")

// Spec describes one deployment request in target-agnostic terms.
// Adapters translate this to K8s manifests, a cloud-run service
// YAML, or a Docker container spec as appropriate.
type Spec struct {
	// AppID identifies the App this deployment belongs to. Adapters
	// use it to tag resources for later Status/Logs/Rollback calls.
	AppID string
	// Image is the fully-qualified image reference (includes tag or
	// digest).
	Image string
	// Env is the merged environment (PlainVars + resolved secrets)
	// for the container.
	Env map[string]string
	// Ports is the set of container ports the service exposes.
	Ports []int
	// Replicas is the desired replica count. Zero means "backend
	// default" (Cloud Run autoscales; K8s picks 1).
	Replicas int
}

// Status summarises the live state of a deployment.
type Status struct {
	Healthy  bool   `json:"healthy"`
	Replicas int    `json:"replicas"`
	URL      string `json:"url,omitempty"`
	Message  string `json:"message,omitempty"`
}

// Target is the plugin interface. Adapters must be safe for
// concurrent use.
type Target interface {
	// Kind returns the stable identifier used when routing an App's
	// DeployTarget.Kind to a specific adapter.
	Kind() model.DeployTargetKind
	// Deploy applies spec to the target runtime.
	Deploy(ctx context.Context, spec Spec) error
	// Status reports the current health of the deployment.
	Status(ctx context.Context, appID string) (Status, error)
	// Logs streams container logs for appID into out until ctx is
	// cancelled or the stream ends.
	Logs(ctx context.Context, appID string, out io.Writer) error
	// Rollback restores the previous revision of appID.
	Rollback(ctx context.Context, appID string) error
}

// registry is the global adapter registry. Adapters register in
// their own init() so importing the package is enough to make the
// Kind available to the server.
var (
	registry   = map[model.DeployTargetKind]Target{}
	registryMu sync.RWMutex
)

// Register adds t to the registry. Panics on duplicate Kind so
// misconfigured binaries fail loudly at startup rather than in the
// middle of a deploy.
func Register(t Target) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := registry[t.Kind()]; dup {
		panic(fmt.Sprintf("deploytarget: duplicate Kind %q", t.Kind()))
	}
	registry[t.Kind()] = t
}

// Lookup returns the registered adapter for kind. Callers should
// handle ErrUnavailable as "no adapter for this kind in this build".
func Lookup(kind model.DeployTargetKind) (Target, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	t, ok := registry[kind]
	if !ok {
		return nil, fmt.Errorf("%w: no adapter registered for kind %q", ErrUnavailable, kind)
	}
	return t, nil
}

// ResetForTest clears the registry. Tests in adjacent packages use
// this to avoid duplicate-kind panics across test runs.
func ResetForTest() {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = map[model.DeployTargetKind]Target{}
}
