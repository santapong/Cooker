// Package scheduler runs cron-triggered pipeline runs. A single
// elected replica (via pg_advisory_lock) scans the schedules table
// for due rows and enqueues pipeline-run jobs via the existing
// internal/jobqueue. Non-leader replicas wait for the lock and take
// over if the leader exits / crashes — leader election is
// continuous, not one-shot.
//
// Cron syntax: standard 5-field POSIX (minute hour dom month dow).
// See cron.go for what's supported. Operators who want descriptors
// like @hourly / @daily can either pin the equivalent canonical
// form ("0 * * * *" / "0 0 * * *") or wait for the follow-up that
// swaps in robfig/cron.
package scheduler

import "time"

// Schedule mirrors a row in the schedules table.
type Schedule struct {
	ID         string     `json:"id"`
	PipelineID string     `json:"pipelineId"`
	Name       string     `json:"name,omitempty"`
	CronExpr   string     `json:"cronExpr"`
	Timezone   string     `json:"timezone"` // IANA name; default "UTC"
	LastRunAt  *time.Time `json:"lastRunAt,omitempty"`
	LastRunID  string     `json:"lastRunId,omitempty"`
	NextRunAt  time.Time  `json:"nextRunAt"`
	Enabled    bool       `json:"enabled"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}

// LoadLocation returns the IANA location named on the schedule.
// Falls back to UTC on empty or unparseable. Defensive: an operator
// typo ("americas/new_york") shouldn't crash the scheduler tick.
func (s Schedule) LoadLocation() *time.Location {
	if s.Timezone == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}
