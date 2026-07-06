package model

import "time"

// DeployStrategy selects how an App ships a new version. "rolling" is
// the default in-place replace path (Clone→Build→Push→Deploy, the
// existing behaviour); "canary" routes a configurable slice of traffic
// to the new version first and only shifts the rest after a health
// window passes (or rolls back on failure).
type DeployStrategy string

const (
	DeployStrategyRolling DeployStrategy = "rolling"
	DeployStrategyCanary  DeployStrategy = "canary"
)

// CanaryConfig is the opt-in canary deployment policy carried on an
// App. It is user-visible configuration (lands via app create/update)
// and is persisted as the apps.canary_config JSONB column. The live
// progress of an in-flight canary is tracked separately on AppCanary —
// this struct only describes intent.
type CanaryConfig struct {
	// Strategy is "rolling" (default) or "canary". An empty value is
	// normalised to "rolling" by Normalize so a zero-value App keeps the
	// pre-canary behaviour.
	Strategy DeployStrategy `json:"strategy,omitempty"`
	// Weight is the percent of traffic (1–99) sent to the new version
	// while the canary is being evaluated. Ignored for rolling.
	Weight int `json:"weight,omitempty"`
	// AutoPromote, when true, shifts the canary to 100% automatically
	// once HealthWindowSeconds elapse with no failing health checks.
	// When false the canary holds at Weight until an operator promotes
	// or aborts it from the UI / API.
	AutoPromote bool `json:"autoPromote,omitempty"`
	// HealthWindowSeconds is how long the new version must stay healthy
	// at Weight before an auto-promote fires. Ignored when AutoPromote
	// is false (a manual canary has no window). Normalised to a sane
	// default when canary+auto-promote is set with a non-positive value.
	HealthWindowSeconds int `json:"healthWindowSeconds,omitempty"`
}

// Defaults applied by Normalize when a canary config is under-specified.
const (
	// DefaultCanaryWeight is used when strategy=canary but no weight was
	// supplied. A 10% slice is the conventional first canary step.
	DefaultCanaryWeight = 10
	// DefaultCanaryHealthWindowSeconds is the auto-promote soak time when
	// strategy=canary, AutoPromote=true, and no window was supplied.
	DefaultCanaryHealthWindowSeconds = 300
	// minCanaryWeight / maxCanaryWeight bound the traffic split. A canary
	// at 0% ships nothing; a canary at 100% is just a rolling deploy.
	minCanaryWeight = 1
	maxCanaryWeight = 99
)

// IsCanary reports whether this config opts into the canary path.
func (c CanaryConfig) IsCanary() bool {
	return c.Strategy == DeployStrategyCanary
}

// Normalize fills in defaults and returns the config in a canonical
// form. It does NOT reject out-of-range values — that is Validate's job
// (the handler validates before persisting); Normalize only ensures a
// stored-but-empty config reads back as an explicit rolling default and
// that a canary with omitted optional fields gets workable values.
func (c CanaryConfig) Normalize() CanaryConfig {
	if c.Strategy == "" {
		c.Strategy = DeployStrategyRolling
	}
	if c.Strategy != DeployStrategyCanary {
		// Rolling carries no canary knobs; zero them so a flip-flop in the
		// UI can't leave stale weight/window values on a rolling app.
		return CanaryConfig{Strategy: c.Strategy}
	}
	if c.Weight == 0 {
		c.Weight = DefaultCanaryWeight
	}
	if c.AutoPromote && c.HealthWindowSeconds <= 0 {
		c.HealthWindowSeconds = DefaultCanaryHealthWindowSeconds
	}
	return c
}

// Validate checks a canary config for API-acceptable values. Returns a
// non-nil error with a client-safe message when the config is invalid.
// Call after Normalize so defaulted fields are in range.
func (c CanaryConfig) Validate() error {
	switch c.Strategy {
	case "", DeployStrategyRolling:
		return nil
	case DeployStrategyCanary:
		if c.Weight < minCanaryWeight || c.Weight > maxCanaryWeight {
			return &CanaryConfigError{Field: "canaryWeight", Msg: "must be between 1 and 99"}
		}
		if c.AutoPromote && c.HealthWindowSeconds < 0 {
			return &CanaryConfigError{Field: "canaryHealthWindowSeconds", Msg: "must be >= 0"}
		}
		return nil
	default:
		return &CanaryConfigError{Field: "deployStrategy", Msg: "must be \"rolling\" or \"canary\""}
	}
}

// CanaryConfigError is a validation error carrying the offending field
// so the handler can surface a precise 400. Kept in model (not handler)
// because Validate is a model method and services may call it too.
type CanaryConfigError struct {
	Field string
	Msg   string
}

func (e *CanaryConfigError) Error() string { return e.Field + ": " + e.Msg }

// CanaryStatus is the lifecycle state of an in-flight canary.
type CanaryStatus string

const (
	// CanaryProgressing: the new version is live at the configured weight
	// and being evaluated. This is the state during the health window and
	// while a manual canary waits for an operator decision.
	CanaryProgressing CanaryStatus = "progressing"
	// CanaryPromoted: traffic shifted to 100% on the new version; terminal
	// success.
	CanaryPromoted CanaryStatus = "promoted"
	// CanaryAborted: rolled back to the stable version (operator abort or
	// failed health check); terminal failure.
	CanaryAborted CanaryStatus = "aborted"
	// CanaryFailed: the canary could not be established at all (deploy of
	// the weighted version errored); terminal failure.
	CanaryFailed CanaryStatus = "failed"
)

// IsTerminal reports whether s is an end state (no further transitions).
func (s CanaryStatus) IsTerminal() bool {
	return s == CanaryPromoted || s == CanaryAborted || s == CanaryFailed
}

// AppCanary is the live state of one canary rollout for an App. At most
// one non-terminal row exists per app at a time (enforced by a partial
// unique index in Postgres and a guard in the service). It is written
// when a canary deploy starts, updated as the rollout progresses, and
// stamped terminal on promote / abort / failure. Unlike CanaryConfig
// (intent), this is observed progress and is never set by the user
// directly — it is driven by the CanaryService.
type AppCanary struct {
	ID    string `json:"id" db:"id"`
	AppID string `json:"appId" db:"app_id"`
	// RunID ties this canary to the deploy run that created it so the UI
	// can stream the same app-run:<id> channel and the run page can show
	// canary context.
	RunID string `json:"runId" db:"run_id"`
	// StableImage is the image serving the remaining (100-Weight)% of
	// traffic — the version we roll back to on abort.
	StableImage string `json:"stableImage,omitempty" db:"stable_image"`
	// CanaryImage is the new image under evaluation at Weight%.
	CanaryImage string `json:"canaryImage" db:"canary_image"`
	// Weight is the live traffic percent on CanaryImage. Starts at the
	// config's weight; set to 100 on promote.
	Weight int          `json:"weight" db:"weight"`
	Status CanaryStatus `json:"status" db:"status"`
	// AutoPromote / HealthWindowSeconds are snapshotted from the App's
	// CanaryConfig at start so a mid-rollout config edit can't change the
	// in-flight contract.
	AutoPromote         bool `json:"autoPromote" db:"auto_promote"`
	HealthWindowSeconds int  `json:"healthWindowSeconds" db:"health_window_seconds"`
	// Healthy mirrors the most recent health verdict observed during the
	// window (true until a probe fails). Surfaced in the status panel.
	Healthy bool `json:"healthy" db:"healthy"`
	// Message carries the latest human-readable note (why it aborted, or
	// the current progress line).
	Message string `json:"message,omitempty" db:"message"`
	// PromoteAfter is when an auto-promote becomes eligible (StartedAt +
	// HealthWindowSeconds). Nil for manual canaries.
	PromoteAfter *time.Time `json:"promoteAfter,omitempty" db:"promote_after"`
	StartedAt    time.Time  `json:"startedAt" db:"started_at"`
	UpdatedAt    time.Time  `json:"updatedAt" db:"updated_at"`
	// ResolvedAt is stamped when Status becomes terminal.
	ResolvedAt *time.Time `json:"resolvedAt,omitempty" db:"resolved_at"`
}
