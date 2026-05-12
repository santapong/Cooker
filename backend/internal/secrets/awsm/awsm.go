// Package awsm implements secrets.Manager backed by AWS Secrets
// Manager. Each Cooker secret maps 1:1 to an AWS secret named
// "<prefix>/<envID>/<key>" with the SecretString set to the raw value.
//
// Auth: standard AWS chain — IRSA / EC2 instance profile / env vars
// / shared credentials. Operators don't need to inject anything
// special; the SDK picks up whatever the runtime provides.
package awsm

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"

	"github.com/santapong/cooker/internal/secrets"
)

// Manager satisfies secrets.Manager against AWS Secrets Manager.
type Manager struct {
	client *secretsmanager.Client
	prefix string
}

// Config holds the SDK-construction inputs. Region is required if it
// isn't discoverable from the AWS chain (env vars, instance metadata).
type Config struct {
	Region string
	Prefix string // e.g. "cooker"; AWS secret IDs become "cooker/<envID>/<key>"
}

// New constructs a Manager. ctx is used for the SDK config load
// only; per-call contexts are passed through Get/Put/Delete/List.
func New(ctx context.Context, cfg Config) (*Manager, error) {
	if cfg.Prefix == "" {
		cfg.Prefix = "cooker"
	}
	loadOpts := []func(*awsconfig.LoadOptions) error{}
	if cfg.Region != "" {
		loadOpts = append(loadOpts, awsconfig.WithRegion(cfg.Region))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, err
	}
	return &Manager{
		client: secretsmanager.NewFromConfig(awsCfg),
		prefix: cfg.Prefix,
	}, nil
}

func (m *Manager) name(envID, key string) string {
	return m.prefix + "/" + envID + "/" + key
}

func (m *Manager) Get(ctx context.Context, envID, key string) ([]byte, error) {
	out, err := m.client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(m.name(envID, key)),
	})
	if err != nil {
		var nfe *smtypes.ResourceNotFoundException
		if errors.As(err, &nfe) {
			return nil, secrets.ErrNotFound
		}
		return nil, err
	}
	if out.SecretString != nil {
		return []byte(*out.SecretString), nil
	}
	return out.SecretBinary, nil
}

func (m *Manager) Put(ctx context.Context, envID, key string, value []byte) error {
	id := m.name(envID, key)
	_, err := m.client.PutSecretValue(ctx, &secretsmanager.PutSecretValueInput{
		SecretId:     aws.String(id),
		SecretString: aws.String(string(value)),
	})
	if err == nil {
		return nil
	}
	var nfe *smtypes.ResourceNotFoundException
	if !errors.As(err, &nfe) {
		return err
	}
	// First-time put: secret doesn't exist yet.
	_, cerr := m.client.CreateSecret(ctx, &secretsmanager.CreateSecretInput{
		Name:         aws.String(id),
		SecretString: aws.String(string(value)),
	})
	return cerr
}

func (m *Manager) Delete(ctx context.Context, envID, key string) error {
	_, err := m.client.DeleteSecret(ctx, &secretsmanager.DeleteSecretInput{
		SecretId:                   aws.String(m.name(envID, key)),
		ForceDeleteWithoutRecovery: aws.Bool(true),
	})
	if err != nil {
		var nfe *smtypes.ResourceNotFoundException
		if errors.As(err, &nfe) {
			return nil
		}
		return err
	}
	return nil
}

func (m *Manager) List(ctx context.Context, envID string) ([]string, error) {
	prefix := m.prefix + "/" + envID + "/"
	var keys []string
	var next *string
	for {
		out, err := m.client.ListSecrets(ctx, &secretsmanager.ListSecretsInput{
			NextToken: next,
			Filters: []smtypes.Filter{{
				Key:    smtypes.FilterNameStringTypeName,
				Values: []string{prefix},
			}},
		})
		if err != nil {
			return nil, err
		}
		for _, s := range out.SecretList {
			if s.Name == nil {
				continue
			}
			if !strings.HasPrefix(*s.Name, prefix) {
				continue
			}
			keys = append(keys, strings.TrimPrefix(*s.Name, prefix))
		}
		if out.NextToken == nil {
			break
		}
		next = out.NextToken
	}
	return keys, nil
}
