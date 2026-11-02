package awsm

import (
	"context"
	"regexp"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/santapong/cooker/internal/secrets"
)

type versionClient interface {
	DescribeSecret(context.Context, *secretsmanager.DescribeSecretInput, ...func(*secretsmanager.Options)) (*secretsmanager.DescribeSecretOutput, error)
	GetSecretValue(context.Context, *secretsmanager.GetSecretValueInput, ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
}

var awsVersion = regexp.MustCompile(`^[A-Za-z0-9_-]{32,64}$`)
var awsARN = regexp.MustCompile(`^arn:(aws|aws-cn|aws-us-gov):secretsmanager:([a-z0-9-]+):[0-9]{12}:secret:(.+)-[A-Za-z0-9]{6}$`)

func (m *Manager) versionClient() versionClient {
	if m.versions != nil {
		return m.versions
	}
	if m.client == nil {
		return nil
	}
	return m.client
}
func (m *Manager) versionScope(envID, key string) (secrets.VersionScope, error) {
	authority := m.authority
	if authority == "" {
		authority = "aws-secrets-manager"
	}
	s := secrets.VersionScope{Authority: authority, Region: m.region, Prefix: m.prefix, EnvironmentID: envID, Key: key}
	if !secrets.ValidSegment(envID) || !secrets.ValidSegment(key) || !secrets.ValidPath(m.prefix, false) || !secrets.ValidSegment(m.region) {
		return s, secrets.ErrInvalidVersionReference
	}
	return s, secrets.ValidateVersionScope(s)
}

func versionAuthority(endpoint *string) string {
	if endpoint == nil {
		return "aws-secrets-manager"
	}
	authority, err := secrets.CanonicalAuthority(*endpoint)
	if err != nil {
		return "unsupported://configured-endpoint"
	}
	return authority
}
func (m *Manager) validARN(arn, envID, key string) bool {
	p := awsARN.FindStringSubmatch(arn)
	return len(p) == 4 && p[2] == m.region && p[3] == m.name(envID, key)
}

func (m *Manager) Pin(ctx context.Context, envID, key string) (secrets.VersionReference, error) {
	s, err := m.versionScope(envID, key)
	if err != nil {
		return secrets.VersionReference{}, err
	}
	c := m.versionClient()
	if c == nil {
		return secrets.VersionReference{}, secrets.ErrVersionUnavailable
	}
	out, err := c.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{SecretId: aws.String(m.name(envID, key))})
	if err != nil {
		return secrets.VersionReference{}, secrets.ErrVersionUnavailable
	}
	if out == nil || out.DeletedDate != nil || aws.ToString(out.Name) != m.name(envID, key) || !m.validARN(aws.ToString(out.ARN), envID, key) {
		return secrets.VersionReference{}, secrets.ErrVersionMismatch
	}
	var version string
	for id, stages := range out.VersionIdsToStages {
		for _, stage := range stages {
			if stage == "AWSCURRENT" {
				if version != "" {
					return secrets.VersionReference{}, secrets.ErrVersionMismatch
				}
				version = id
			}
		}
	}
	if !awsVersion.MatchString(version) {
		return secrets.VersionReference{}, secrets.ErrVersionUnavailable
	}
	r := secrets.VersionReference{SchemaVersion: 1, Provider: "awsm", Resource: aws.ToString(out.ARN), Version: version, Scope: s}
	if err := r.Validate(); err != nil {
		return secrets.VersionReference{}, err
	}
	return r, nil
}

func (m *Manager) GetPinned(ctx context.Context, envID, key string, r secrets.VersionReference) ([]byte, error) {
	s, err := m.versionScope(envID, key)
	if err != nil {
		return nil, err
	}
	if err := r.ValidateScope("awsm", s); err != nil {
		return nil, err
	}
	if !m.validARN(r.Resource, envID, key) || !awsVersion.MatchString(r.Version) || r.Incarnation != "" {
		return nil, secrets.ErrInvalidVersionReference
	}
	c := m.versionClient()
	if c == nil {
		return nil, secrets.ErrVersionUnavailable
	}
	out, err := c.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: aws.String(r.Resource), VersionId: aws.String(r.Version)})
	if err != nil {
		return nil, secrets.ErrVersionUnavailable
	}
	if out == nil || aws.ToString(out.ARN) != r.Resource || aws.ToString(out.VersionId) != r.Version || aws.ToString(out.Name) != m.name(envID, key) {
		return nil, secrets.ErrVersionMismatch
	}
	if out.SecretString != nil && out.SecretBinary == nil {
		return []byte(*out.SecretString), nil
	}
	if out.SecretString == nil && out.SecretBinary != nil {
		return append([]byte{}, out.SecretBinary...), nil
	}
	return nil, secrets.ErrVersionUnavailable
}

var _ secrets.VersionedManager = (*Manager)(nil)
