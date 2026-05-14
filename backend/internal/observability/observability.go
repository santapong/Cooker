// Package observability wires Prometheus metrics and OpenTelemetry
// traces into the Gin HTTP layer. Both are opt-in via config; the
// zero-value configuration is a no-op so dev workflows aren't forced
// to run a collector.
package observability

import (
	"context"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Config bundles the env-var-driven knobs that observability.Setup
// reads. Mirrors fields on config.Config; passed through unchanged so
// the observability package doesn't import the config package.
type Config struct {
	MetricsEnabled bool
	TracingEnabled bool
	OTLPEndpoint   string // host:port
	OTLPInsecure   bool
	ServiceName    string
	ServiceVersion string
}

var (
	httpRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cooker_http_requests_total",
		Help: "Total HTTP requests handled by the Cooker API, labelled by method, route template, and status class.",
	}, []string{"method", "route", "status"})

	httpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "cooker_http_request_duration_seconds",
		Help:    "Latency of HTTP requests handled by the Cooker API.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "route"})

	// Resilience counters introduced for the SPOF closeout. All four
	// stay at zero on a healthy deployment; rate > 0 / 5m is page-worthy.
	dbConnectionErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "cooker_db_connection_errors_total",
		Help: "Number of times the Postgres ping failed during the boot-time backoff loop or readiness probe.",
	})

	redisConnectionErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "cooker_redis_connection_errors_total",
		Help: "Number of times a Redis call (rate limiter, ws-ticket, ws-hub publish) returned an error.",
	})

	jwksFetchFailures = promauto.NewCounter(prometheus.CounterOpts{
		Name: "cooker_jwks_fetch_failures_total",
		Help: "Number of times OIDC provider discovery or JWKS fetch failed.",
	})

	pipelineRunsOrphaned = promauto.NewCounter(prometheus.CounterOpts{
		Name: "cooker_pipeline_runs_orphaned_total",
		Help: "Number of pipeline_runs rows the boot-time orphan sweep marked failed.",
	})

	// Pipeline-stage histograms — one per stage type so dashboards
	// can split out build vs push vs deploy without a separate
	// label. Labelled by status to count succ/fail at the same time.
	stageDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "cooker_pipeline_stage_duration_seconds",
		Help:    "Wall-clock duration of pipeline stages, labelled by stage type and final status.",
		Buckets: []float64{0.5, 1, 5, 15, 30, 60, 180, 300, 600, 1800, 3600},
	}, []string{"type", "status"})

	auditDropped = promauto.NewCounter(prometheus.CounterOpts{
		Name: "cooker_audit_events_dropped_total",
		Help: "Number of audit events dropped because the file sink's queue was full (T16 fail-open).",
	})

	heartbeatErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "cooker_run_heartbeat_errors_total",
		Help: "Number of run-coordinator heartbeat write failures.",
	})

	// Phase 1 / A1 jobqueue series. Depth gauge is set on demand by
	// callers that have a fresh Stats snapshot; attempts counter
	// increments per Dispatch; duration histogram observes per Dispatch
	// labelled by status (succ/fail).
	jobqueueDepth = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "cooker_jobqueue_depth",
		Help: "Current number of jobs in the queue, by status (pending, running, succeeded, failed, cancelled).",
	}, []string{"status"})

	jobqueueAttempts = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cooker_jobqueue_attempts_total",
		Help: "Number of job-handler dispatches the worker pool has performed, labelled by kind.",
	}, []string{"kind"})

	jobqueueDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "cooker_jobqueue_run_duration_seconds",
		Help:    "Wall-clock duration of a single job-handler dispatch, labelled by kind and outcome.",
		Buckets: []float64{0.05, 0.1, 0.5, 1, 5, 15, 30, 60, 180, 300, 600, 1800, 3600},
	}, []string{"kind", "outcome"})

	notifierSent = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cooker_notifier_sent_total",
		Help: "Number of notification sends attempted by the notifier package, labelled by channel and outcome.",
	}, []string{"channel", "outcome"})
)

// ObserveStageDuration records the wall-clock time a pipeline stage
// took, labelled by type and final status. Called from the
// executor's per-stage callback.
func ObserveStageDuration(stageType, status string, d time.Duration) {
	stageDuration.WithLabelValues(stageType, status).Observe(d.Seconds())
}

// IncAuditDropped records an audit-sink overflow event.
func IncAuditDropped() { auditDropped.Inc() }

// IncHeartbeatError records a heartbeat write failure (any cause).
func IncHeartbeatError() { heartbeatErrors.Inc() }

// IncDBConnectionError records a database connection / ping error.
// Called from postgres.NewStore's backoff loop and the readiness
// probe's DB check.
func IncDBConnectionError() { dbConnectionErrors.Inc() }

// IncRedisConnectionError records a failed Redis call from any of the
// Redis-backed adapters (rate limiter, ws-ticket, ws-hub).
func IncRedisConnectionError() { redisConnectionErrors.Inc() }

// IncJWKSFetchFailure records a failed OIDC provider discovery or
// JWKS fetch. The lazy-init middleware calls this on every miss.
func IncJWKSFetchFailure() { jwksFetchFailures.Inc() }

// AddPipelineRunsOrphaned records the size of a boot-time orphan
// sweep. Sums to the total number of orphans across all boots.
func AddPipelineRunsOrphaned(n int) {
	if n > 0 {
		pipelineRunsOrphaned.Add(float64(n))
	}
}

// SetJobqueueDepth sets the gauge for a single status. Callers
// (typically a periodic scraper goroutine) pass each status they
// care about; statuses they omit retain their previous value.
func SetJobqueueDepth(status string, n int) {
	jobqueueDepth.WithLabelValues(status).Set(float64(n))
}

// IncJobqueueAttempts records a single dispatch attempt for the
// given kind. Called by the worker pool unconditionally; the outcome
// is recorded separately via ObserveJobqueueDuration.
func IncJobqueueAttempts(kind string) {
	jobqueueAttempts.WithLabelValues(kind).Inc()
}

// ObserveJobqueueDuration records the wall-clock time a single job
// dispatch took. ok=true labels it "success"; ok=false labels it
// "error". Workers call this immediately after the handler returns,
// regardless of subsequent Reschedule/Fail outcome.
func ObserveJobqueueDuration(kind string, d time.Duration, ok bool) {
	outcome := "success"
	if !ok {
		outcome = "error"
	}
	jobqueueDuration.WithLabelValues(kind, outcome).Observe(d.Seconds())
}

// IncNotifierSent records a notifier send attempt by channel and
// outcome. ok=true labels it "success"; ok=false labels it "error".
// Channels: slack, discord, email, webhook.
func IncNotifierSent(channel string, ok bool) {
	outcome := "success"
	if !ok {
		outcome = "error"
	}
	notifierSent.WithLabelValues(channel, outcome).Inc()
}

// MetricsHandler returns the Prometheus /metrics handler.
func MetricsHandler() gin.HandlerFunc {
	h := promhttp.Handler()
	return func(c *gin.Context) { h.ServeHTTP(c.Writer, c.Request) }
}

// MetricsMiddleware records request count + duration. Routes are
// labelled by Gin's matched template (e.g. "/api/v1/pipelines/:id"),
// not the concrete URL — keeps cardinality bounded.
func MetricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		status := strconv.Itoa(c.Writer.Status())
		httpRequests.WithLabelValues(c.Request.Method, route, status).Inc()
		httpDuration.WithLabelValues(c.Request.Method, route).Observe(time.Since(start).Seconds())
	}
}

// TracingMiddleware returns the otelgin middleware bound to the
// global TracerProvider. Setup must have been called for spans to
// propagate to the configured collector; otherwise this still runs
// as a passthrough that records into the no-op tracer.
func TracingMiddleware(serviceName string) gin.HandlerFunc {
	return otelgin.Middleware(serviceName)
}

// Setup configures the global OTel TracerProvider when tracing is
// enabled. Returns a shutdown function callers must invoke at process
// exit (no-op when tracing is disabled). Errors initializing the
// exporter are returned so the server can fail fast in production.
func Setup(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	if !cfg.TracingEnabled {
		return func(context.Context) error { return nil }, nil
	}
	dial := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint)}
	if cfg.OTLPInsecure {
		dial = append(dial, otlptracegrpc.WithInsecure())
	}
	exporter, err := otlptrace.New(ctx, otlptracegrpc.NewClient(dial...))
	if err != nil {
		return nil, err
	}
	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(cfg.ServiceVersion),
	))
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))
	return tp.Shutdown, nil
}
