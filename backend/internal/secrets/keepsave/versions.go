package keepsave

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/secrets"
)

// Only the versioned-vault revision protocol is supported. Legacy version
// counters are derived from retained history and can be reused after pruning.
type revisionMetadata struct {
	ID            string    `json:"id"`
	ProjectID     string    `json:"project_id"`
	EnvironmentID string    `json:"environment_id"`
	Environment   string    `json:"environment"`
	Key           string    `json:"key"`
	Revision      int64     `json:"revision"`
	CreatedAt     time.Time `json:"created_at"`
	Deleted       bool      `json:"deleted"`
}
type revisionValue struct {
	revisionMetadata
	Value *string `json:"value"`
}

func canonicalUUID(s string) bool {
	u, err := uuid.Parse(s)
	return err == nil && u != uuid.Nil && u.String() == s
}
func (m *Manager) versionScope(ctx context.Context, envID, key string) (secrets.VersionScope, error) {
	s := secrets.VersionScope{Project: m.projectID, EnvironmentID: envID, Key: key}
	if m.client == nil || m.envs == nil || !canonicalUUID(m.projectID) || !secrets.ValidSegment(envID) || !secrets.ValidSegment(key) {
		return s, secrets.ErrInvalidVersionReference
	}
	var err error
	s.Authority, err = secrets.CanonicalAuthority(m.client.BaseURL)
	if err != nil {
		return s, err
	}
	name, err := m.envName(ctx, envID)
	if err != nil {
		return s, secrets.ErrVersionUnavailable
	}
	if !secrets.ValidSegment(name) {
		return s, secrets.ErrInvalidVersionReference
	}
	s.Prefix = name
	return s, secrets.ValidateVersionScope(s)
}

// versionRead never retains error bodies or arbitrary provider diagnostics and
// is bounded independently of the existing editing client's response path.
func (c *Client) versionRead(ctx context.Context, path string, out any) error {
	authority, err := secrets.CanonicalAuthority(c.BaseURL)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, authority+path, nil)
	if err != nil {
		return secrets.ErrVersionUnavailable
	}
	req.Header.Set("X-API-Key", c.APIKey)
	req.Header.Set("Accept", "application/json")
	client := c.HTTP
	if client == nil {
		return secrets.ErrVersionUnavailable
	}
	// Redirects could forward credentials or read a different backend. A shallow
	// copy preserves the caller's transport/timeouts while disabling redirects.
	owned := *client
	owned.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := owned.Do(req)
	if err != nil {
		return secrets.ErrVersionUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return secrets.ErrVersionUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		return secrets.ErrVersionUnavailable
	}
	if err := json.Unmarshal(body, out); err != nil {
		return secrets.ErrVersionUnavailable
	}
	return nil
}
func (m *Manager) revisionValid(r revisionMetadata, key string) bool {
	return canonicalUUID(r.ID) && r.ProjectID == m.projectID && canonicalUUID(r.EnvironmentID) && r.Key == key && r.Revision > 0 && !r.CreatedAt.IsZero() && !r.Deleted
}

func (m *Manager) Pin(ctx context.Context, envID, key string) (secrets.VersionReference, error) {
	s, err := m.versionScope(ctx, envID, key)
	if err != nil {
		return secrets.VersionReference{}, err
	}
	var list struct {
		Secrets []revisionMetadata `json:"secrets"`
	}
	path := "/api/v1/projects/" + m.projectID + "/secrets?environment=" + url.QueryEscape(s.Prefix)
	if err := m.client.versionRead(ctx, path, &list); err != nil {
		return secrets.VersionReference{}, err
	}
	var found *revisionMetadata
	for i := range list.Secrets {
		if list.Secrets[i].Key == key {
			if found != nil {
				return secrets.VersionReference{}, secrets.ErrVersionMismatch
			}
			found = &list.Secrets[i]
		}
	}
	if found == nil {
		return secrets.VersionReference{}, secrets.ErrVersionUnavailable
	}
	if found.Revision == 0 {
		return secrets.VersionReference{}, secrets.ErrVersionUnsupported
	}
	if !m.revisionValid(*found, key) || (found.Environment != "" && found.Environment != s.Prefix) {
		return secrets.VersionReference{}, secrets.ErrVersionMismatch
	}
	r := secrets.VersionReference{SchemaVersion: 1, Provider: "keepsave", Resource: found.ID, Version: strconv.FormatInt(found.Revision, 10), Incarnation: found.EnvironmentID + "|" + found.CreatedAt.UTC().Format(time.RFC3339Nano), Scope: s}
	if err := r.Validate(); err != nil {
		return secrets.VersionReference{}, err
	}
	return r, nil
}

func (m *Manager) GetPinned(ctx context.Context, envID, key string, r secrets.VersionReference) ([]byte, error) {
	s, err := m.versionScope(ctx, envID, key)
	if err != nil {
		return nil, err
	}
	if err := r.ValidateScope("keepsave", s); err != nil {
		return nil, err
	}
	v, err := strconv.ParseInt(r.Version, 10, 64)
	i := strings.Split(r.Incarnation, "|")
	if err != nil || v < 1 || strconv.FormatInt(v, 10) != r.Version || !canonicalUUID(r.Resource) || len(i) != 2 || !canonicalUUID(i[0]) {
		return nil, secrets.ErrInvalidVersionReference
	}
	t, err := time.Parse(time.RFC3339Nano, i[1])
	if err != nil || t.IsZero() || t.UTC().Format(time.RFC3339Nano) != i[1] {
		return nil, secrets.ErrInvalidVersionReference
	}
	var out revisionValue
	path := "/api/v1/projects/" + m.projectID + "/secrets/" + r.Resource + "/versions/" + r.Version
	if err := m.client.versionRead(ctx, path, &out); err != nil {
		return nil, err
	}
	if out.Revision == 0 {
		return nil, secrets.ErrVersionUnsupported
	}
	if !m.revisionValid(out.revisionMetadata, key) || out.ID != r.Resource || out.Revision != v || out.Environment != s.Prefix || out.EnvironmentID+"|"+out.CreatedAt.UTC().Format(time.RFC3339Nano) != r.Incarnation {
		return nil, secrets.ErrVersionMismatch
	}
	// KeepSave omits an empty value via omitempty. The successful exact revision
	// read denotes an existing value; absent value therefore means empty string.
	if out.Value == nil {
		return []byte{}, nil
	}
	return []byte(*out.Value), nil
}

var _ secrets.VersionedManager = (*Manager)(nil)
