package deployer

import (
	"context"
	"fmt"
)

// ClientGo applies manifests via the Kubernetes dynamic client. A
// production deployment uses in-cluster credentials; an operator
// workstation can point it at a kubeconfig.
//
// The client-go wiring is intentionally deferred (roadmap Phase 1.3):
// it pulls a large dep tree and every meaningful test needs a live
// API server. This type is defined now so callers can program
// against the production Deployer.
type ClientGo struct {
	Kubeconfig string // empty means in-cluster
}

func NewClientGo(kubeconfig string) *ClientGo { return &ClientGo{Kubeconfig: kubeconfig} }

// Deploy is not yet wired; returns ErrUnavailable.
func (c *ClientGo) Deploy(_ context.Context, _ Request) (Result, error) {
	return Result{}, fmt.Errorf("%w: client-go dynamic client not yet wired (roadmap Phase 1.3)", ErrUnavailable)
}

var _ Deployer = (*ClientGo)(nil)
