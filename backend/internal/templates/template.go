// Package templates provides the pipeline-template catalog. A
// Template carries a Pipeline-shaped JSON schema; the handler's
// "Create from template" path deep-copies that into a new Pipeline
// row (assigning fresh stage IDs so two pipelines created from the
// same template don't share node identifiers).
package templates

import (
	"encoding/json"
	"time"
)

// Template mirrors a row in pipeline_templates.
type Template struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Category    string          `json:"category,omitempty"`
	// Schema is the embedded Pipeline JSON. Channels that consume
	// the catalog (UI gallery) need only Name + Description +
	// Category; the Schema is fetched lazily on "Use template".
	Schema    json.RawMessage `json:"schema"`
	IconURL   string          `json:"iconUrl,omitempty"`
	Enabled   bool            `json:"enabled"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}
