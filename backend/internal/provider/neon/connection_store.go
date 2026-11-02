package neon

import (
	"context"
	"encoding/json"
	"sort"
	"time"
)

const MaxSelectedConnectionProjects = 5
const MaxNeonConnections = 100
const ConnectionSyncTimeout = 90 * time.Second
const ConnectionLeaseDuration = 100 * time.Second
const ConnectionCredentialPurpose = "cooker.neon.management.v1"

type CredentialCodec interface {
	Active() bool
	Seal([]byte) ([]byte, error)
	Open([]byte) ([]byte, error)
}

// AccountMode is an operator declaration, not proof of billing ownership or of
// which account modes the presented key can access. A GET client is not a
// read-only credential: a Neon management key may inherently permit writes.
type Connection struct {
	ID                 string              `json:"id"`
	Name               string              `json:"name"`
	OrganizationID     string              `json:"organizationId"`
	AccountMode        string              `json:"accountMode"`
	AccountModeSource  string              `json:"accountModeSource"`
	CredentialMayWrite bool                `json:"credentialMayWrite"`
	Enabled            bool                `json:"enabled"`
	HasCredential      bool                `json:"hasCredential"`
	Version            int                 `json:"version"`
	ProjectIDs         []string            `json:"projectIds"`
	Snapshot           *ConnectionSnapshot `json:"snapshot,omitempty"`
	LastAttemptAt      *time.Time          `json:"lastAttemptAt,omitempty"`
	LastSuccessAt      *time.Time          `json:"lastSuccessAt,omitempty"`
	ErrorCode          string              `json:"errorCode,omitempty"`
	State              string              `json:"state"`
	Syncing            bool                `json:"syncing"`
	CreatedAt          time.Time           `json:"createdAt"`
	UpdatedAt          time.Time           `json:"updatedAt"`
}

type ConnectionSnapshot struct {
	OrganizationID string    `json:"organizationId"`
	Projects       []Project `json:"projects"`
	Branches       []Branch  `json:"branches"`
	ObservedAt     time.Time `json:"observedAt"`
	Complete       bool      `json:"complete"`
}

type ConnectionRecord struct {
	Connection
	Credential []byte     `json:"-"`
	Lease      string     `json:"-"`
	LeaseUntil *time.Time `json:"-"`
}

// All conditional methods must use the authoritative row under one short lock
// or transaction. No provider HTTP occurs under that lock. Update preserves the
// latest observation unless key/selection changes invalidate it, bumps version,
// and clears a lease. Finish requires the same version, lease and unexpired
// deadline; nil snapshot retains last success. Only fixed safe error codes enter
// ErrorCode. No provider body/message or ciphertext is public JSON.
type ConnectionStore interface {
	List(context.Context) ([]*ConnectionRecord, error)
	Get(context.Context, string) (*ConnectionRecord, error)
	Create(context.Context, *ConnectionRecord) error
	Update(context.Context, *ConnectionRecord, int) error
	Delete(context.Context, string, int) error
	Claim(context.Context, string, int, string, time.Time, time.Time) error
	Finish(context.Context, string, int, string, *ConnectionSnapshot, string, time.Time) error
}

type RequestError struct {
	Code string `json:"code"`
}

func (e *RequestError) Error() string {
	switch e.Code {
	case "invalid_input":
		return "Neon connection input is invalid"
	case "not_found":
		return "Neon connection was not found"
	case "conflict":
		return "Neon connection changed; reload before retrying"
	case "paused":
		return "Neon connection is paused or has no selected projects"
	case "busy":
		return "Neon connection refresh is already running"
	case "connection_limit":
		return "Neon connection limit reached"
	case "canceled":
		return "Neon connection request was canceled"
	case "provider_unauthorized":
		return "Neon API key is unavailable or expired"
	case "provider_denied":
		return "Neon metadata access is denied"
	case "rate_limited":
		return "Neon metadata rate limit reached"
	case "scope_mismatch":
		return "Neon metadata does not match the saved scope"
	case "invalid_response":
		return "Neon metadata coverage is unavailable"
	case "provider_unavailable":
		return "Neon metadata provider is unavailable"
	default:
		return "Neon connections are temporarily unavailable"
	}
}

func connectionError(code string) error { return &RequestError{Code: code} }

func safeConnectionCode(code string) bool {
	switch code {
	case "canceled", "provider_unauthorized", "provider_denied", "rate_limited", "scope_mismatch", "invalid_response", "provider_unavailable":
		return true
	default:
		return false
	}
}

func cloneConnectionSnapshot(in *ConnectionSnapshot) *ConnectionSnapshot {
	if in == nil {
		return nil
	}
	out := *in
	out.Projects = append([]Project{}, in.Projects...)
	out.Branches = append([]Branch{}, in.Branches...)
	return &out
}

func cloneConnectionRecord(in *ConnectionRecord) *ConnectionRecord {
	if in == nil {
		return nil
	}
	out := *in
	out.ProjectIDs = append([]string{}, in.ProjectIDs...)
	out.Snapshot = cloneConnectionSnapshot(in.Snapshot)
	out.Credential = append([]byte{}, in.Credential...)
	if in.LastAttemptAt != nil {
		value := *in.LastAttemptAt
		out.LastAttemptAt = &value
	}
	if in.LastSuccessAt != nil {
		value := *in.LastSuccessAt
		out.LastSuccessAt = &value
	}
	if in.LeaseUntil != nil {
		value := *in.LeaseUntil
		out.LeaseUntil = &value
	}
	return &out
}

func publicConnection(in *ConnectionRecord, now time.Time) *Connection {
	r := cloneConnectionRecord(in)
	r.HasCredential = len(r.Credential) > 0
	r.AccountModeSource, r.CredentialMayWrite = "operator_declared", true
	r.Syncing = r.Lease != "" && r.LeaseUntil != nil && r.LeaseUntil.After(now)
	badClock := (r.LastSuccessAt != nil && (r.LastSuccessAt.IsZero() || r.LastSuccessAt.After(now))) || (r.LastAttemptAt != nil && (r.LastAttemptAt.IsZero() || r.LastAttemptAt.After(now)))
	if r.Snapshot != nil && (validateConnectionSnapshot(r.Snapshot, r.OrganizationID, r.ProjectIDs) != nil || r.LastSuccessAt == nil || badClock || r.Snapshot.ObservedAt.After(now)) {
		r.Snapshot = nil
		r.LastSuccessAt = nil
		r.ErrorCode = "invalid_response"
	}
	if badClock {
		r.Snapshot = nil
		r.LastSuccessAt = nil
		r.LastAttemptAt = nil
		r.Syncing = false
		r.ErrorCode = "invalid_response"
	}
	if r.Snapshot == nil {
		r.LastSuccessAt = nil
	}
	r.State = "observed"
	if r.Snapshot == nil {
		r.State = "not_observed"
		if r.LastAttemptAt != nil || r.ErrorCode != "" {
			r.State = "unavailable"
		}
	}
	if r.Snapshot != nil && r.ErrorCode != "" {
		r.State = "stale"
	}
	if r.Snapshot != nil && !r.Syncing && r.Lease != "" {
		r.State = "stale"
	}
	if r.Snapshot != nil && !r.Syncing && r.LastAttemptAt != nil && r.LastSuccessAt != nil && r.LastAttemptAt.After(*r.LastSuccessAt) {
		r.State = "stale"
	}
	if !r.Enabled {
		r.State = "paused"
		r.Syncing = false
	}
	if !safeConnectionCode(r.ErrorCode) {
		r.ErrorCode = ""
	}
	return &r.Connection
}

func sameProjectSelection(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func applyConnectionConfig(current, proposed *ConnectionRecord) *ConnectionRecord {
	out := cloneConnectionRecord(current)
	out.Name, out.AccountMode, out.Enabled = proposed.Name, proposed.AccountMode, proposed.Enabled
	out.Credential = append([]byte{}, proposed.Credential...)
	out.ProjectIDs = append([]string{}, proposed.ProjectIDs...)
	out.Version++
	out.UpdatedAt = proposed.UpdatedAt
	out.Lease, out.LeaseUntil, out.Syncing = "", nil, false
	if !sameProjectSelection(current.ProjectIDs, proposed.ProjectIDs) || string(current.Credential) != string(proposed.Credential) {
		out.Snapshot, out.LastSuccessAt, out.LastAttemptAt, out.ErrorCode = nil, nil, nil, ""
	}
	return out
}

func validConnectionRecord(r *ConnectionRecord) bool {
	if r == nil || !validConnectionID(r.ID) || !validID(r.OrganizationID) || r.Version < 1 || !validConnectionName(r.Name) || !validAccountMode(r.AccountMode) || len(r.Credential) == 0 || len(r.Credential) > 16384 || r.CreatedAt.IsZero() || r.UpdatedAt.IsZero() {
		return false
	}
	selection, err := normalizedProjectSelection(r.ProjectIDs)
	return err == nil && sameProjectSelection(selection, r.ProjectIDs)
}

func normalizedProjectSelection(ids []string) ([]string, error) {
	if ids == nil || len(ids) > MaxSelectedConnectionProjects {
		return nil, connectionError("invalid_input")
	}
	out, seen := append([]string{}, ids...), map[string]bool{}
	for _, id := range out {
		if !validID(id) || seen[id] {
			return nil, connectionError("invalid_input")
		}
		seen[id] = true
	}
	sort.Strings(out)
	return out, nil
}

// Stable plaintext binding prevents ciphertext copied to another connection or
// organization from silently selecting a different credential authority.
type connectionCredentialEnvelope struct {
	Purpose        string `json:"purpose"`
	ConnectionID   string `json:"connectionId"`
	OrganizationID string `json:"organizationId"`
	Key            string `json:"key"`
}

func connectionEnvelopeBytes(id, organization, key string) ([]byte, error) {
	return json.Marshal(connectionCredentialEnvelope{Purpose: ConnectionCredentialPurpose, ConnectionID: id, OrganizationID: organization, Key: key})
}
