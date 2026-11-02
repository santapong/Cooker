// Package neon prepares bounded read-only control-plane metadata access.
// It neither obtains credentials nor establishes Vercel bindings or SQL health.
// A GET-only client does not reduce a Neon API key's inherent permissions.
package neon

import (
	"context"
	"time"
)

type Project struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	OrganizationID string    `json:"organizationId"`
	RegionID       string    `json:"regionId"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type Branch struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"projectId"`
	ParentID  string    `json:"parentId,omitempty"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Complete covers the provider-reported accessible inventory within this one
// explicit organization/project scope, not all accounts or hidden resources.
// Methods return no partial inventory on a failed or incomplete page campaign.
type ProjectInventory struct {
	OrganizationID string    `json:"organizationId"`
	Projects       []Project `json:"projects"`
	ObservedAt     time.Time `json:"observedAt"`
	Complete       bool      `json:"complete"`
}

type BranchInventory struct {
	ProjectID  string    `json:"projectId"`
	Branches   []Branch  `json:"branches"`
	ObservedAt time.Time `json:"observedAt"`
	Complete   bool      `json:"complete"`
}

// MetadataAPI accepts a separately authorized Neon API key. No Vercel token,
// cookie, SQL password or account-mode inference is supported by this contract.
type MetadataAPI interface {
	Projects(context.Context, string, string) (*ProjectInventory, error)
	Branches(context.Context, string, string) (*BranchInventory, error)
}

// Database identity is the exact project + branch + provider ID tuple. ID is a
// decimal int64 string so a JSON consumer cannot round the provider identity.
// Name is intentionally readable configuration metadata, not a credential.
type Database struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	BranchID  string `json:"branchId"`
	Name      string `json:"name"`
}

// Endpoint contains only control-plane metadata. CurrentState is a reported
// lifecycle state (init/active/idle/unknown), never SQL or application health.
// ComputeReleaseVersion is not the project's PostgreSQL version.
type Endpoint struct {
	ID                    string `json:"id"`
	ProjectID             string `json:"projectId"`
	BranchID              string `json:"branchId"`
	Type                  string `json:"type"`
	CurrentState          string `json:"currentState"`
	ComputeReleaseVersion string `json:"computeReleaseVersion,omitempty"`
}

// Complete covers the reported full list in this exact project/branch scope,
// not inaccessible resources or databases in another branch/account.
type DatabaseInventory struct {
	ProjectID  string     `json:"projectId"`
	BranchID   string     `json:"branchId"`
	Databases  []Database `json:"databases"`
	ObservedAt time.Time  `json:"observedAt"`
	Complete   bool       `json:"complete"`
}

// Complete covers the reported full list in this project scope only.
type EndpointInventory struct {
	ProjectID  string     `json:"projectId"`
	Endpoints  []Endpoint `json:"endpoints"`
	ObservedAt time.Time  `json:"observedAt"`
	Complete   bool       `json:"complete"`
}

// ResourceMetadataAPI is optional so existing project/branch adapters remain
// compatible. These reads neither obtain SQL credentials nor establish a
// resource's environment, billing owner, Vercel binding or actual connectivity.
type ResourceMetadataAPI interface {
	Databases(context.Context, string, string, string) (*DatabaseInventory, error)
	Endpoints(context.Context, string, string) (*EndpointInventory, error)
}

// Error carries fixed Cooker-authored classification only. Neither provider
// response bodies, transport messages, input strings nor key values are wrapped.
type Error struct {
	Code       string        `json:"code"`
	StatusCode int           `json:"statusCode,omitempty"`
	RetryAfter time.Duration `json:"-"`
}

func (e *Error) Error() string {
	switch e.Code {
	case "invalid_input":
		return "Neon metadata input is invalid"
	case "unauthorized":
		return "Neon API authentication is unavailable"
	case "forbidden":
		return "Neon API access is denied"
	case "not_found":
		return "Neon metadata scope was not found"
	case "rate_limited":
		return "Neon API rate limit reached"
	case "canceled":
		return "Neon metadata request was canceled"
	case "transport":
		return "Neon metadata response was not received"
	case "response_limit":
		return "Neon metadata response exceeds the supported size"
	case "inventory_limit":
		return "Neon metadata inventory exceeds the supported limit"
	case "incomplete_coverage":
		return "Neon metadata inventory completeness could not be established"
	case "scope_mismatch":
		return "Neon metadata does not match the requested scope"
	case "invalid_response":
		return "Neon metadata response is invalid"
	default:
		return "Neon metadata provider is unavailable"
	}
}
