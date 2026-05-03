package observability

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestResilienceCounters_Increment(t *testing.T) {
	IncDBConnectionError()
	IncRedisConnectionError()
	IncJWKSFetchFailure()
	AddPipelineRunsOrphaned(3)

	if got := testutil.ToFloat64(dbConnectionErrors); got < 1 {
		t.Errorf("dbConnectionErrors = %v, want >= 1", got)
	}
	if got := testutil.ToFloat64(redisConnectionErrors); got < 1 {
		t.Errorf("redisConnectionErrors = %v, want >= 1", got)
	}
	if got := testutil.ToFloat64(jwksFetchFailures); got < 1 {
		t.Errorf("jwksFetchFailures = %v, want >= 1", got)
	}
	if got := testutil.ToFloat64(pipelineRunsOrphaned); got < 3 {
		t.Errorf("pipelineRunsOrphaned = %v, want >= 3", got)
	}
}

func TestAddPipelineRunsOrphaned_IgnoresZeroAndNegative(t *testing.T) {
	before := testutil.ToFloat64(pipelineRunsOrphaned)
	AddPipelineRunsOrphaned(0)
	AddPipelineRunsOrphaned(-5)
	after := testutil.ToFloat64(pipelineRunsOrphaned)
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
