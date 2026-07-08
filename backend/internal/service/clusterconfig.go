package service

import (
	"context"
	"fmt"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/secrets"
	"github.com/santapong/cooker/internal/store"
)

// ClusterConfigService is the cluster-config analogue of
// RegistryConfigService. The secret it manages is the kubeconfig body.
type ClusterConfigService struct {
	Store   *store.Store
	Secrets secrets.Manager
}

// NewClusterConfigService constructs a ClusterConfigService.
func NewClusterConfigService(s *store.Store, secs secrets.Manager) *ClusterConfigService {
	return &ClusterConfigService{Store: s, Secrets: secs}
}

// clusterKeyRef returns the canonical KubeconfigRef for a cluster id.
func clusterKeyRef(id string) string { return "cluster:" + id }

// CreateCluster persists a new ClusterConfig, optionally storing the
// supplied kubeconfig into secrets.Manager and stamping the ref onto
// the record. The kubeconfig bytes never live on the struct after this
// call.
func (s *ClusterConfigService) CreateCluster(ctx context.Context, c *model.ClusterConfig, kubeconfig []byte) error {
	if len(kubeconfig) > 0 {
		if s.Secrets == nil {
			return ErrConfigSecretsUnavailable
		}
		if c.ID == "" {
			return fmt.Errorf("settingsconfig: cluster ID required before storing kubeconfig")
		}
		if err := ensureScopeEnv(ctx, s.Store, clusterSecretsEnvID, "_clusters (internal)"); err != nil {
			return err
		}
		if err := s.Secrets.Put(ctx, clusterSecretsEnvID, clusterKubeconfigKey+"."+c.ID, kubeconfig); err != nil {
			return fmt.Errorf("settingsconfig: store kubeconfig: %w", err)
		}
		c.KubeconfigRef = clusterKeyRef(c.ID)
	}
	return s.Store.Clusters.Create(ctx, c)
}

// DeleteCluster removes the cluster and (best-effort) its stored
// kubeconfig.
func (s *ClusterConfigService) DeleteCluster(ctx context.Context, id string) error {
	if err := s.Store.Clusters.Delete(ctx, id); err != nil {
		return err
	}
	if s.Secrets != nil {
		_ = s.Secrets.Delete(ctx, clusterSecretsEnvID, clusterKubeconfigKey+"."+id)
	}
	return nil
}
