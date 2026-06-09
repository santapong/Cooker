package model

import "time"

// AuditEvent is one queryable row of the audit trail (roadmap M5).
// Mirrors audit.Event with latency normalised to milliseconds for
// sorting/aggregation. Bodies are never captured by design.
type AuditEvent struct {
	ID        int64     `json:"id" db:"id"`
	Time      time.Time `json:"time" db:"time"`
	UserSub   string    `json:"userSub,omitempty" db:"user_sub"`
	UserEmail string    `json:"userEmail,omitempty" db:"user_email"`
	Method    string    `json:"method" db:"method"`
	Path      string    `json:"path" db:"path"`
	Status    int       `json:"status" db:"status"`
	LatencyMS int64     `json:"latencyMs" db:"latency_ms"`
	ClientIP  string    `json:"clientIp,omitempty" db:"client_ip"`
}
