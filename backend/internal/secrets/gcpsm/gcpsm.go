// Package gcpsm implements secrets.Manager backed by Google Cloud
// Secret Manager. Each Cooker secret is one secret in the GCP project
// named "<prefix>__<envID>__<key>" (double-underscore separator —
// Secret Manager only allows [A-Za-z0-9_-]).
//
// Auth: Application Default Credentials. On GKE/Workload Identity
// no extra config is needed; otherwise GOOGLE_APPLICATION_CREDENTIALS
// must point at a service-account key file.
package gcpsm

import (
	"context"
	"fmt"
	"strings"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/santapong/cooker/internal/secrets"
)

// Manager satisfies secrets.Manager against GCP Secret Manager.
type Manager struct {
	client    *secretmanager.Client
	projectID string
	prefix    string
}

// Config holds the SDK construction inputs.
type Config struct {
	ProjectID string // GCP project ID
	Prefix    string // optional, default "cooker"
}

// New constructs a Manager. ctx is used for the SDK init only; per-
// call contexts are passed through.
func New(ctx context.Context, cfg Config) (*Manager, error) {
	if cfg.ProjectID == "" {
		return nil, fmt.Errorf("gcpsm: ProjectID is required")
	}
	if cfg.Prefix == "" {
		cfg.Prefix = "cooker"
	}
	c, err := secretmanager.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	return &Manager{client: c, projectID: cfg.ProjectID, prefix: cfg.Prefix}, nil
}

func (m *Manager) secretID(envID, key string) string {
	return m.prefix + "__" + envID + "__" + key
}

func (m *Manager) parent() string {
	return fmt.Sprintf("projects/%s", m.projectID)
}

func (m *Manager) versionResource(envID, key string) string {
	return fmt.Sprintf("projects/%s/secrets/%s/versions/latest", m.projectID, m.secretID(envID, key))
}

func (m *Manager) Get(ctx context.Context, envID, key string) ([]byte, error) {
	resp, err := m.client.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{
		Name: m.versionResource(envID, key),
	})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, secrets.ErrNotFound
		}
		return nil, err
	}
	return resp.Payload.Data, nil
}

func (m *Manager) Put(ctx context.Context, envID, key string, value []byte) error {
	id := m.secretID(envID, key)
	secretName := fmt.Sprintf("projects/%s/secrets/%s", m.projectID, id)
	// Try to add a new version first; create the secret on NotFound.
	_, err := m.client.AddSecretVersion(ctx, &secretmanagerpb.AddSecretVersionRequest{
		Parent:  secretName,
		Payload: &secretmanagerpb.SecretPayload{Data: value},
	})
	if err == nil {
		return nil
	}
	if status.Code(err) != codes.NotFound {
		return err
	}
	if _, cerr := m.client.CreateSecret(ctx, &secretmanagerpb.CreateSecretRequest{
		Parent:   m.parent(),
		SecretId: id,
		Secret: &secretmanagerpb.Secret{
			Replication: &secretmanagerpb.Replication{
				Replication: &secretmanagerpb.Replication_Automatic_{
					Automatic: &secretmanagerpb.Replication_Automatic{},
				},
			},
		},
	}); cerr != nil {
		return cerr
	}
	_, err = m.client.AddSecretVersion(ctx, &secretmanagerpb.AddSecretVersionRequest{
		Parent:  secretName,
		Payload: &secretmanagerpb.SecretPayload{Data: value},
	})
	return err
}

func (m *Manager) Delete(ctx context.Context, envID, key string) error {
	err := m.client.DeleteSecret(ctx, &secretmanagerpb.DeleteSecretRequest{
		Name: fmt.Sprintf("projects/%s/secrets/%s", m.projectID, m.secretID(envID, key)),
	})
	if err == nil {
		return nil
	}
	if status.Code(err) == codes.NotFound {
		return nil
	}
	return err
}

func (m *Manager) List(ctx context.Context, envID string) ([]string, error) {
	prefix := m.prefix + "__" + envID + "__"
	it := m.client.ListSecrets(ctx, &secretmanagerpb.ListSecretsRequest{
		Parent: m.parent(),
	})
	var keys []string
	for {
		s, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		// s.Name is "projects/<id>/secrets/<secretID>" — extract secretID.
		idx := strings.LastIndex(s.Name, "/")
		if idx < 0 {
			continue
		}
		id := s.Name[idx+1:]
		if !strings.HasPrefix(id, prefix) {
			continue
		}
		keys = append(keys, strings.TrimPrefix(id, prefix))
	}
	return keys, nil
}
