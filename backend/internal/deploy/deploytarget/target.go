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

	"github.com/santapong/cooker/internal/model"
)

// ErrUnavailable is returned when a target backend has no
// credentials, no network, or is not implemented in this build.
var ErrUnavailable = errors.New("deploytarget: unavailable")

// ErrDuplicateKind is returned by Register when an adapter for the
// same Kind has already been registered. Callers can either treat
// this as fatal (use MustRegister) or surface it through their own
// startup-validation path.
var ErrDuplicateKind = errors.New("deploytarget: duplicate kind")

// Spec describes one deployment request in target-agnostic terms.
// Adapters translate this to K8s manifests, a cloud-run service
// YAML, or a Docker container spec as appropriate.
type Spec struct {
	AppID    string
	Image    string
	Env      map[string]string
	Ports    []int
	Replicas int
	// LogWriter, when non-nil, receives per-call streaming output
	// from the adapter (docker pull progress for SSH, kubectl apply
	// output for K8s, etc.). Adapters that don't stream may ignore
	// it. Non-streaming adapters MUST NOT panic on a nil writer.
	LogWriter io.Writer
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
	Kind() model.DeployTargetKind
	Deploy(ctx context.Context, spec Spec) error
	Status(ctx context.Context, appID string) (Status, error)
	Logs(ctx context.Context, appID string, out io.Writer) error
	Rollback(ctx context.Context, appID string) error
}

var (
	registry   = map[model.DeployTargetKind]Target{}
	registryMu sync.RWMutex
)

// Register adds t to the registry. Returns ErrDuplicateKind when an
// adapter for the same Kind is already registered. Callers wiring
// adapters at boot should either log.Fatal on the error or use
// MustRegister for the same effect with less ceremony.
func Register(t Target) error {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := registry[t.Kind()]; dup {
		return fmt.Errorf("%w: %q", ErrDuplicateKind, t.Kind())
	}
	registry[t.Kind()] = t
	return nil
}

// MustRegister is the panic-on-error convenience wrapper around
// Register. Use from init() blocks where a duplicate registration
// indicates a programming error and should fail loudly at startup.
func MustRegister(t Target) {
	if err := Register(t); err != nil {
		panic(err)
	}
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
// this to avoid duplicate-kind state across test runs.
func ResetForTest() {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = map[model.DeployTargetKind]Target{}
}
