package model

import "time"

// User represents a local-auth account stored in Cooker's database.
// The OIDC path does not produce User rows — its identities live at
// the IdP. Local users coexist with OIDC users in the auth middleware
// via a JWT-prefix probe.
type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	PasswordHash string    `json:"-"` // never serialised
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}
