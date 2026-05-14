package notifier

import (
	"encoding/json"
	"time"
)

// Target represents a single configured notification destination.
// Mirrors a row in the notification_targets table; channel-
// specific config lives inside Config (channel adapters decode
// their fields from it).
type Target struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Kind       string          `json:"kind"`
	Config     json.RawMessage `json:"config"`
	// EventTypes filters which events this target receives. Empty
	// list means "all events" (least surprise: a freshly configured
	// target with no filter set fires on everything).
	EventTypes []EventType `json:"eventTypes,omitempty"`
	Enabled    bool        `json:"enabled"`
	CreatedAt  time.Time   `json:"createdAt"`
	UpdatedAt  time.Time   `json:"updatedAt"`
}

// Matches reports whether this Target should receive an event of
// the given type. Empty EventTypes is the wildcard match.
func (t Target) Matches(et EventType) bool {
	if !t.Enabled {
		return false
	}
	if len(t.EventTypes) == 0 {
		return true
	}
	for _, allowed := range t.EventTypes {
		if allowed == et {
			return true
		}
	}
	return false
}

// DecodeConfig unmarshals the channel-specific Config blob into
// the supplied destination. Wraps json.Unmarshal so callers don't
// need to remember to dereference RawMessage.
func (t Target) DecodeConfig(dst any) error {
	if len(t.Config) == 0 {
		return nil
	}
	return json.Unmarshal(t.Config, dst)
}
