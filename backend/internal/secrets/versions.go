package secrets

import (
	"context"
	"errors"
	"net/url"
	"strings"
)

// VersionedManager is optional. Accepted plans must fail closed when the
// configured backend cannot honor their immutable reference; Get is never a
// fallback for either method. Pin retrieves metadata, not a plaintext digest.
type VersionedManager interface {
	Pin(ctx context.Context, envID, key string) (VersionReference, error)
	GetPinned(ctx context.Context, envID, key string, ref VersionReference) ([]byte, error)
}

var (
	ErrVersionUnsupported      = errors.New("secrets: immutable versions unsupported")
	ErrVersionUnavailable      = errors.New("secrets: pinned version unavailable")
	ErrVersionMismatch         = errors.New("secrets: pinned version does not match")
	ErrInvalidVersionReference = errors.New("secrets: invalid version reference")
)

// VersionScope binds a reference to the configuration and logical key that
// created it. These are bounded nonsecret identifiers, never authentication
// material. Authority is the public endpoint (or fixed service authority).
type VersionScope struct {
	Authority     string `json:"authority"`
	Region        string `json:"region,omitempty"`
	Project       string `json:"project,omitempty"`
	Mount         string `json:"mount,omitempty"`
	Prefix        string `json:"prefix,omitempty"`
	EnvironmentID string `json:"environmentId"`
	Key           string `json:"key"`
}

// VersionReference contains metadata only. Incarnation binds creation metadata
// where a resource name and numeric version can be reused after deletion.
type VersionReference struct {
	SchemaVersion int          `json:"schemaVersion"`
	Provider      string       `json:"provider"`
	Resource      string       `json:"resource"`
	Version       string       `json:"version"`
	Incarnation   string       `json:"incarnation,omitempty"`
	Scope         VersionScope `json:"scope"`
}

func bounded(s string, max int, required bool) bool {
	if required && s == "" || len(s) > max {
		return false
	}
	for _, r := range s {
		if r < 33 || r > 126 || strings.ContainsRune("\"\\<>`", r) {
			return false
		}
	}
	return true
}

// Validate verifies shape and bounds. Each adapter additionally validates its
// exact resource/version protocol; arbitrary provider fields are not copied.
func (r VersionReference) Validate() error {
	if r.SchemaVersion != 1 || !bounded(r.Resource, 1024, true) || !bounded(r.Version, 128, true) || !bounded(r.Incarnation, 256, false) {
		return ErrInvalidVersionReference
	}
	switch r.Provider {
	case "awsm", "gcpsm", "vault", "keepsave":
	default:
		return ErrInvalidVersionReference
	}
	return ValidateVersionScope(r.Scope)
}

func ValidateVersionScope(s VersionScope) error {
	if !bounded(s.Authority, 512, true) || !bounded(s.Region, 64, false) || !bounded(s.Project, 128, false) || !bounded(s.Mount, 256, false) || !bounded(s.Prefix, 256, false) || !bounded(s.EnvironmentID, 128, true) || !bounded(s.Key, 128, true) {
		return ErrInvalidVersionReference
	}
	if strings.Contains(s.Authority, "://") {
		if _, err := CanonicalAuthority(s.Authority); err != nil {
			return err
		}
	}
	return nil
}

// ValidSegment rejects path traversal and separators in logical identifiers.
func ValidSegment(s string) bool {
	if !bounded(s, 128, true) || s == "." || s == ".." {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}

func ValidPath(s string, empty bool) bool {
	if s == "" {
		return empty
	}
	if len(s) > 256 {
		return false
	}
	for _, segment := range strings.Split(s, "/") {
		if !ValidSegment(segment) {
			return false
		}
	}
	return true
}

// ValidateScope must run before a provider read. Exact comparison rejects
// backend/endpoint/project/prefix/environment/key substitution.
func (r VersionReference) ValidateScope(provider string, expected VersionScope) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if r.Provider != provider || r.Scope != expected {
		return ErrVersionMismatch
	}
	return nil
}

// CanonicalAuthority rejects embedded credentials and query/fragment data.
func CanonicalAuthority(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || (strings.ToLower(u.Scheme) != "http" && strings.ToLower(u.Scheme) != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !bounded(raw, 512, true) {
		return "", ErrInvalidVersionReference
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u.String(), nil
}

// PinVersion and ReadPinned keep unsupported adapters fail closed.
func PinVersion(ctx context.Context, manager Manager, envID, key string) (VersionReference, error) {
	v, ok := manager.(VersionedManager)
	if !ok {
		return VersionReference{}, ErrVersionUnsupported
	}
	return v.Pin(ctx, envID, key)
}

func ReadPinned(ctx context.Context, manager Manager, envID, key string, ref VersionReference) ([]byte, error) {
	v, ok := manager.(VersionedManager)
	if !ok {
		return nil, ErrVersionUnsupported
	}
	return v.GetPinned(ctx, envID, key, ref)
}
