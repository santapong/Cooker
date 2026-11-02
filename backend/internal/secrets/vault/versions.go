package vault

import (
	"context"
	"encoding/json"
	"math"
	"net/url"
	"strconv"
	"time"

	"github.com/hashicorp/vault-client-go"
	"github.com/hashicorp/vault-client-go/schema"
	"github.com/santapong/cooker/internal/secrets"
)

type versionClient interface {
	KvV2Read(context.Context, string, ...vault.RequestOption) (*vault.Response[schema.KvV2ReadResponse], error)
	KvV2ReadMetadata(context.Context, string, ...vault.RequestOption) (*vault.Response[schema.KvV2ReadMetadataResponse], error)
}

func (m *Manager) versionClient() versionClient {
	if m.versions != nil {
		return m.versions
	}
	if m.client == nil {
		return nil
	}
	return &m.client.Secrets
}
func (m *Manager) versionScope(envID, key string) (secrets.VersionScope, error) {
	s := secrets.VersionScope{Authority: m.authority, Mount: m.mount, Prefix: m.prefix, EnvironmentID: envID, Key: key}
	if !secrets.ValidSegment(envID) || !secrets.ValidSegment(key) || !secrets.ValidPath(m.mount, false) || !secrets.ValidPath(m.prefix, true) {
		return s, secrets.ErrInvalidVersionReference
	}
	return s, secrets.ValidateVersionScope(s)
}
func creation(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil || t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
func liveMetadata(v map[string]any) string {
	if v == nil {
		return ""
	}
	destroyed, ok := v["destroyed"].(bool)
	if !ok || destroyed {
		return ""
	}
	deleted, ok := v["deletion_time"].(string)
	if !ok || deleted != "" {
		return ""
	}
	return creation(v["created_time"])
}
func number(v any) int64 {
	switch n := v.(type) {
	case float64:
		if n > 0 && n <= 1<<53 && math.Trunc(n) == n {
			return int64(n)
		}
	case int64:
		return n
	case int:
		return int64(n)
	case json.Number:
		i, err := n.Int64()
		if err == nil {
			return i
		}
	}
	return 0
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
	out, err := c.KvV2ReadMetadata(ctx, m.keyPath(envID, key), vault.WithMountPath(m.mount), vault.WithNamespace(""))
	if err != nil {
		return secrets.VersionReference{}, secrets.ErrVersionUnavailable
	}
	if out == nil || out.Data.CurrentVersion < 1 {
		return secrets.VersionReference{}, secrets.ErrVersionUnavailable
	}
	v := strconv.FormatInt(out.Data.CurrentVersion, 10)
	metadata, ok := out.Data.Versions[v].(map[string]any)
	if !ok {
		return secrets.VersionReference{}, secrets.ErrVersionUnavailable
	}
	created := liveMetadata(metadata)
	if created == "" {
		return secrets.VersionReference{}, secrets.ErrVersionUnavailable
	}
	r := secrets.VersionReference{SchemaVersion: 1, Provider: "vault", Resource: m.mount + "/" + m.keyPath(envID, key), Version: v, Incarnation: created, Scope: s}
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
	if err := r.ValidateScope("vault", s); err != nil {
		return nil, err
	}
	v, err := strconv.ParseInt(r.Version, 10, 64)
	if err != nil || v < 1 || strconv.FormatInt(v, 10) != r.Version || creation(r.Incarnation) != r.Incarnation || r.Resource != m.mount+"/"+m.keyPath(envID, key) {
		return nil, secrets.ErrInvalidVersionReference
	}
	c := m.versionClient()
	if c == nil {
		return nil, secrets.ErrVersionUnavailable
	}
	out, err := c.KvV2Read(ctx, m.keyPath(envID, key), vault.WithMountPath(m.mount), vault.WithNamespace(""), vault.WithQueryParameters(url.Values{"version": {r.Version}}))
	if err != nil {
		return nil, secrets.ErrVersionUnavailable
	}
	if out == nil || out.Data.Data == nil {
		return nil, secrets.ErrVersionUnavailable
	}
	if number(out.Data.Metadata["version"]) != v || liveMetadata(out.Data.Metadata) != r.Incarnation {
		return nil, secrets.ErrVersionMismatch
	}
	value, ok := out.Data.Data[valueField].(string)
	if !ok {
		return nil, secrets.ErrVersionUnavailable
	}
	return []byte(value), nil
}

var _ secrets.VersionedManager = (*Manager)(nil)
