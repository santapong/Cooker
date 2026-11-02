package model

import (
	"errors"
	"strings"
	"time"

	"github.com/santapong/cooker/internal/secrets"
)

var ErrInvalidRegistryAuthority = errors.New("registry: invalid accepted authority")

// RegistrySubject is an immutable App or authored Pipeline identity. Names and
// repository/registry host matches do not confer authority.
type RegistrySubject struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

func (s RegistrySubject) Validate() error {
	if (s.Kind != "app" && s.Kind != "pipeline") || !secrets.ValidSegment(s.ID) {
		return ErrInvalidRegistryAuthority
	}
	return nil
}

// RegistryCredentialPin contains metadata only. Database mode is a generation
// fence, not historical-value retention. External mode reads the exact version.
type RegistryCredentialPin struct {
	Mode                 string                    `json:"mode"`
	EnvironmentID        string                    `json:"environmentId,omitempty"`
	Key                  string                    `json:"key,omitempty"`
	EnvironmentVersion   int                       `json:"environmentVersion,omitempty"`
	EnvironmentCreatedAt time.Time                 `json:"environmentCreatedAt,omitempty"`
	Reference            *secrets.VersionReference `json:"reference,omitempty"`
}
type RegistryConnectionPin struct {
	ID         string                `json:"id"`
	Version    int64                 `json:"version"`
	Endpoint   string                `json:"endpoint"`
	Host       string                `json:"host"`
	Username   string                `json:"username,omitempty"`
	Credential RegistryCredentialPin `json:"credential"`
}

// RegistryAuthorityPlan is server-created admission evidence. An explicit empty
// Connections map authorizes anonymous access only; absent evidence is invalid.
// Stages binds authored Pipeline selections. App-generated stages reuse the App
// subject's complete selection, without acquiring a native Pipeline grant.
type RegistryAuthorityPlan struct {
	Version     int                              `json:"version"`
	Subject     RegistrySubject                  `json:"subject"`
	Connections map[string]RegistryConnectionPin `json:"connections"`
	Stages      map[string][]string              `json:"stages,omitempty"`
}

func ValidRegistryIDs(ids []string, max int) bool {
	if len(ids) > max {
		return false
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !secrets.ValidSegment(id) || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}
func registryMetadata(s string, max int) bool {
	if len(s) > max {
		return false
	}
	for _, r := range s {
		if r < 33 || r > 126 || strings.ContainsRune("\"\\<>`", r) {
			return false
		}
	}
	return true
}
func (p *RegistryAuthorityPlan) Validate() error {
	if p == nil || p.Version != 1 || p.Subject.Validate() != nil || p.Connections == nil || len(p.Connections) > 32 || len(p.Stages) > 256 {
		return ErrInvalidRegistryAuthority
	}
	for id, pin := range p.Connections {
		if id != pin.ID || !secrets.ValidSegment(id) || pin.Version < 1 || pin.Endpoint == "" || pin.Host == "" || !registryMetadata(pin.Endpoint, 512) || !registryMetadata(pin.Host, 256) || !registryMetadata(pin.Username, 256) {
			return ErrInvalidRegistryAuthority
		}
		c := pin.Credential
		switch c.Mode {
		case "none":
			if c.EnvironmentID != "" || c.Key != "" || c.EnvironmentVersion != 0 || !c.EnvironmentCreatedAt.IsZero() || c.Reference != nil {
				return ErrInvalidRegistryAuthority
			}
		case "database":
			if !secrets.ValidSegment(c.EnvironmentID) || !secrets.ValidSegment(c.Key) || c.EnvironmentVersion < 1 || c.EnvironmentCreatedAt.IsZero() || c.Reference != nil {
				return ErrInvalidRegistryAuthority
			}
		case "external":
			if !secrets.ValidSegment(c.EnvironmentID) || !secrets.ValidSegment(c.Key) || c.EnvironmentVersion != 0 || !c.EnvironmentCreatedAt.IsZero() || c.Reference == nil || c.Reference.Validate() != nil || c.Reference.Scope.EnvironmentID != c.EnvironmentID || c.Reference.Scope.Key != c.Key {
				return ErrInvalidRegistryAuthority
			}
		default:
			return ErrInvalidRegistryAuthority
		}
	}
	union := map[string]bool{}
	for stageID, ids := range p.Stages {
		if !secrets.ValidSegment(stageID) || !ValidRegistryIDs(ids, 32) {
			return ErrInvalidRegistryAuthority
		}
		for _, id := range ids {
			if _, ok := p.Connections[id]; !ok {
				return ErrInvalidRegistryAuthority
			}
			union[id] = true
		}
	}
	if p.Subject.Kind == "app" && len(p.Stages) != 0 {
		return ErrInvalidRegistryAuthority
	}
	if p.Subject.Kind == "pipeline" && len(union) != len(p.Connections) {
		return ErrInvalidRegistryAuthority
	}
	return nil
}
