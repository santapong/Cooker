package observability

import (
	"strings"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/client_golang/prometheus"
)

// counterValue reads a counter without pulling in
// prometheus/client_golang/prometheus/testutil — that subpackage
// transitively requires Go 1.26 via gvisor/tailscale, which would
// break the Docker build on golang:1.25-alpine.
func counterValue(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	var m dto.Metric
	if err := c.Write(&m); err != nil {
		t.Fatalf("write metric: %v", err)
	}
	if m.Counter == nil || m.Counter.Value == nil {
		return 0
	}
	return *m.Counter.Value
}

func TestResilienceCounters_Increment(t *testing.T) {
	IncDBConnectionError()
	IncRedisConnectionError()
	IncJWKSFetchFailure()
	AddPipelineRunsOrphaned(3)

	if got := counterValue(t, dbConnectionErrors); got < 1 {
		t.Errorf("dbConnectionErrors = %v, want >= 1", got)
	}
	if got := counterValue(t, redisConnectionErrors); got < 1 {
		t.Errorf("redisConnectionErrors = %v, want >= 1", got)
	}
	if got := counterValue(t, jwksFetchFailures); got < 1 {
		t.Errorf("jwksFetchFailures = %v, want >= 1", got)
	}
	if got := counterValue(t, pipelineRunsOrphaned); got < 3 {
		t.Errorf("pipelineRunsOrphaned = %v, want >= 3", got)
	}
}

func TestAddPipelineRunsOrphaned_IgnoresZeroAndNegative(t *testing.T) {
	before := counterValue(t, pipelineRunsOrphaned)
	AddPipelineRunsOrphaned(0)
	AddPipelineRunsOrphaned(-5)
	after := counterValue(t, pipelineRunsOrphaned)
	if after != before {
		t.Errorf("counter changed on n<=0: before=%v after=%v", before, after)
	}
}

// TestCounterNamesStable guards against accidental rename — alerts are
// keyed on these string names.
func TestCounterNamesStable(t *testing.T) {
	for _, want := range []string{
		"cooker_db_connection_errors_total",
		"cooker_redis_connection_errors_total",
		"cooker_jwks_fetch_failures_total",
		"cooker_pipeline_runs_orphaned_total",
	} {
		desc := ""
		switch want {
		case "cooker_db_connection_errors_total":
			desc = dbConnectionErrors.Desc().String()
		case "cooker_redis_connection_errors_total":
			desc = redisConnectionErrors.Desc().String()
		case "cooker_jwks_fetch_failures_total":
			desc = jwksFetchFailures.Desc().String()
		case "cooker_pipeline_runs_orphaned_total":
			desc = pipelineRunsOrphaned.Desc().String()
		}
		if !strings.Contains(desc, want) {
			t.Errorf("counter %s renamed: desc=%q", want, desc)
		}
	}
}
