package gcpsm

import (
	"context"
	"hash/crc32"
	"regexp"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"github.com/googleapis/gax-go/v2"
	"github.com/santapong/cooker/internal/secrets"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type versionClient interface {
	GetSecret(context.Context, *secretmanagerpb.GetSecretRequest, ...gax.CallOption) (*secretmanagerpb.Secret, error)
	GetSecretVersion(context.Context, *secretmanagerpb.GetSecretVersionRequest, ...gax.CallOption) (*secretmanagerpb.SecretVersion, error)
	AccessSecretVersion(context.Context, *secretmanagerpb.AccessSecretVersionRequest, ...gax.CallOption) (*secretmanagerpb.AccessSecretVersionResponse, error)
}

var gcpProjectNumber = regexp.MustCompile(`^[0-9]+$`)
var gcpSecretID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,255}$`)

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
		authority = "secretmanager.googleapis.com"
	}
	s := secrets.VersionScope{Authority: authority, Project: m.projectID, Prefix: m.prefix, EnvironmentID: envID, Key: key}
	if !secrets.ValidSegment(envID) || !secrets.ValidSegment(key) || !secrets.ValidSegment(m.projectID) || !gcpSecretID.MatchString(m.secretID(envID, key)) {
		return s, secrets.ErrInvalidVersionReference
	}
	return s, secrets.ValidateVersionScope(s)
}

func versionAuthority(target string) string {
	for _, prefix := range []string{"dns:///", "passthrough:///", "google-c2p:///"} {
		target = strings.TrimPrefix(target, prefix)
	}
	if strings.ContainsAny(target, "/@?#") || target == "" {
		return "unsupported://configured-endpoint"
	}
	authority, err := secrets.CanonicalAuthority("https://" + target)
	if err != nil {
		return "unsupported://configured-endpoint"
	}
	return authority
}
func stamp(t *timestamppb.Timestamp) string {
	if t == nil || t.CheckValid() != nil || t.AsTime().IsZero() {
		return ""
	}
	return t.AsTime().UTC().Format(time.RFC3339Nano)
}
func positiveVersion(v string) bool {
	n, err := strconv.ParseInt(v, 10, 64)
	return err == nil && n > 0 && strconv.FormatInt(n, 10) == v
}

// Accept a configured project ID or canonical project number returned by the
// scoped GetSecret request. The returned exact resource is pinned thereafter.
func (m *Manager) canonicalSecret(ctx context.Context, c versionClient, envID, key string) (*secretmanagerpb.Secret, error) {
	out, err := c.GetSecret(ctx, &secretmanagerpb.GetSecretRequest{Name: m.parent() + "/secrets/" + m.secretID(envID, key)})
	if err != nil {
		return nil, secrets.ErrVersionUnavailable
	}
	if out == nil || stamp(out.CreateTime) == "" {
		return nil, secrets.ErrVersionMismatch
	}
	p := strings.Split(out.Name, "/")
	if len(p) != 4 || p[0] != "projects" || p[2] != "secrets" || p[3] != m.secretID(envID, key) || (p[1] != m.projectID && !gcpProjectNumber.MatchString(p[1])) {
		return nil, secrets.ErrVersionMismatch
	}
	return out, nil
}
func versionMetadata(v *secretmanagerpb.SecretVersion, parent, version string) (string, error) {
	if v == nil || v.State != secretmanagerpb.SecretVersion_ENABLED || stamp(v.CreateTime) == "" {
		return "", secrets.ErrVersionUnavailable
	}
	prefix := parent + "/versions/"
	if !strings.HasPrefix(v.Name, prefix) {
		return "", secrets.ErrVersionMismatch
	}
	n := strings.TrimPrefix(v.Name, prefix)
	if !positiveVersion(n) || (version != "" && n != version) {
		return "", secrets.ErrVersionMismatch
	}
	return n, nil
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
	secret, err := m.canonicalSecret(ctx, c, envID, key)
	if err != nil {
		return secrets.VersionReference{}, err
	}
	v, err := c.GetSecretVersion(ctx, &secretmanagerpb.GetSecretVersionRequest{Name: secret.Name + "/versions/latest"})
	if err != nil {
		return secrets.VersionReference{}, secrets.ErrVersionUnavailable
	}
	n, err := versionMetadata(v, secret.Name, "")
	if err != nil {
		return secrets.VersionReference{}, err
	}
	r := secrets.VersionReference{SchemaVersion: 1, Provider: "gcpsm", Resource: v.Name, Version: n, Incarnation: stamp(secret.CreateTime) + "|" + stamp(v.CreateTime), Scope: s}
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
	if err := r.ValidateScope("gcpsm", s); err != nil {
		return nil, err
	}
	parts := strings.Split(r.Resource, "/")
	incarnation := strings.Split(r.Incarnation, "|")
	validStamp := func(s string) bool {
		t, err := time.Parse(time.RFC3339Nano, s)
		return err == nil && !t.IsZero() && t.UTC().Format(time.RFC3339Nano) == s
	}
	if !positiveVersion(r.Version) || len(parts) != 6 || parts[0] != "projects" || (parts[1] != m.projectID && !gcpProjectNumber.MatchString(parts[1])) || parts[2] != "secrets" || parts[3] != m.secretID(envID, key) || parts[4] != "versions" || parts[5] != r.Version || len(incarnation) != 2 || !validStamp(incarnation[0]) || !validStamp(incarnation[1]) {
		return nil, secrets.ErrInvalidVersionReference
	}
	c := m.versionClient()
	if c == nil {
		return nil, secrets.ErrVersionUnavailable
	}
	secret, err := m.canonicalSecret(ctx, c, envID, key)
	if err != nil {
		return nil, err
	}
	if r.Resource != secret.Name+"/versions/"+r.Version {
		return nil, secrets.ErrVersionMismatch
	}
	check := func() error {
		v, err := c.GetSecretVersion(ctx, &secretmanagerpb.GetSecretVersionRequest{Name: r.Resource})
		if err != nil {
			return secrets.ErrVersionUnavailable
		}
		if _, err := versionMetadata(v, secret.Name, r.Version); err != nil {
			return err
		}
		if r.Incarnation != stamp(secret.CreateTime)+"|"+stamp(v.CreateTime) {
			return secrets.ErrVersionMismatch
		}
		return nil
	}
	if err := check(); err != nil {
		return nil, err
	}
	out, err := c.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{Name: r.Resource})
	if err != nil {
		return nil, secrets.ErrVersionUnavailable
	}
	if out == nil || out.Name != r.Resource || out.Payload == nil {
		return nil, secrets.ErrVersionMismatch
	}
	// The second metadata read fences deletion/recreation between metadata and
	// payload access. No bytes escape until the exact incarnation is confirmed.
	if err := check(); err != nil {
		return nil, err
	}
	if out.Payload.DataCrc32C != nil && uint64(*out.Payload.DataCrc32C) != uint64(crc32.Checksum(out.Payload.Data, crc32.MakeTable(crc32.Castagnoli))) {
		return nil, secrets.ErrVersionMismatch
	}
	return append([]byte{}, out.Payload.Data...), nil
}

var _ secrets.VersionedManager = (*Manager)(nil)
