#!/usr/bin/env bash
# scripts/e2e/run.sh — deterministic end-to-end smoke against a running
# Cooker UAT stack.
#
# Flow:
#   1. Wait for /health/ready to return 200 (orchestrators have brought
#      the stack up)
#   2. POST a single-stage "custom" pipeline to /api/v1/pipelines
#   3. POST run on the new pipeline → capture runId
#   4. Poll /api/v1/pipelines/<id>/runs/<runId> with backoff until the
#      run is no longer pending|running
#   5. Assert final status == "success"
#
# Why "custom": executeCustom in internal/service/executor.go is a
# deterministic no-op (no Docker, no registry, no k8s). Lets the test
# verify pipeline orchestration end-to-end without depending on the
# build/push/deploy adapters working in the UAT environment.
#
# Auth: relies on COOKER_OIDC_ENABLED=false (UAT default). The backend
# injects a dev admin user, so unauthenticated requests are accepted.
# If you need to run this against an OIDC-enabled stack, set
# COOKER_TEST_BEARER to a valid token before invoking.
#
# Invoked via `make test-e2e`. The Makefile target boots `make uat-up`
# first and tears down on exit; this script assumes the stack is
# already up.

set -euo pipefail

API="${COOKER_API:-http://localhost:8080}"
READY_TIMEOUT_S="${COOKER_E2E_READY_TIMEOUT:-120}"
RUN_TIMEOUT_S="${COOKER_E2E_RUN_TIMEOUT:-60}"

log() { printf '[e2e %s] %s\n' "$(date -u +%H:%M:%S)" "$*" >&2; }

require() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required tool: $1" >&2
    exit 2
  }
}

require curl
require jq

auth_header=()
if [[ -n "${COOKER_TEST_BEARER:-}" ]]; then
  auth_header=(-H "Authorization: Bearer ${COOKER_TEST_BEARER}")
fi

# 1. Wait for /health/ready
log "waiting for ${API}/health/ready (timeout ${READY_TIMEOUT_S}s)"
deadline=$(( $(date +%s) + READY_TIMEOUT_S ))
while :; do
  if curl -fsS "${API}/health/ready" >/dev/null 2>&1; then
    log "ready"
    break
  fi
  if (( $(date +%s) >= deadline )); then
    echo "FAIL: ${API}/health/ready did not return 200 within ${READY_TIMEOUT_S}s" >&2
    curl -s -o /dev/stderr -w "last status: %{http_code}\n" "${API}/health/ready" >&2 || true
    exit 1
  fi
  sleep 2
done

# 2. Create a single-stage "custom" pipeline
log "creating pipeline"
pipeline_payload=$(cat <<'JSON'
{
  "name": "e2e-smoke",
  "description": "deterministic e2e smoke test (custom no-op stage)",
  "stages": [
    {
      "id": "noop",
      "name": "Noop",
      "type": "custom",
      "config": {
        "script": "echo hello-from-e2e",
        "timeout": "30s",
        "retries": 0
      },
      "position": { "x": 0, "y": 0 }
    }
  ],
  "edges": [],
  "variables": {}
}
JSON
)
pipeline=$(curl -fsS -X POST "${API}/api/v1/pipelines" \
  -H "Content-Type: application/json" \
  "${auth_header[@]}" \
  -d "${pipeline_payload}")
pipeline_id=$(echo "${pipeline}" | jq -re '.id')
log "pipeline_id=${pipeline_id}"

# 3. Trigger a run
log "triggering run"
run=$(curl -fsS -X POST "${API}/api/v1/pipelines/${pipeline_id}/run" \
  "${auth_header[@]}")
run_id=$(echo "${run}" | jq -re '.id')
log "run_id=${run_id}"

# 4. Poll until terminal
log "polling run status (timeout ${RUN_TIMEOUT_S}s)"
deadline=$(( $(date +%s) + RUN_TIMEOUT_S ))
status="pending"
while :; do
  body=$(curl -fsS "${API}/api/v1/pipelines/${pipeline_id}/runs/${run_id}" \
    "${auth_header[@]}")
  status=$(echo "${body}" | jq -re '.status')
  case "${status}" in
    success|failed|cancelled)
      log "terminal status: ${status}"
      break
      ;;
  esac
  if (( $(date +%s) >= deadline )); then
    echo "FAIL: run ${run_id} did not reach terminal status within ${RUN_TIMEOUT_S}s (last status: ${status})" >&2
    echo "${body}" | jq . >&2 || echo "${body}" >&2
    exit 1
  fi
  sleep 1
done

# 5. Assert
if [[ "${status}" != "success" ]]; then
  echo "FAIL: run terminated with status=${status}, want success" >&2
  echo "${body}" | jq . >&2 || echo "${body}" >&2
  exit 1
fi

log "PASS: pipeline ${pipeline_id} run ${run_id} succeeded"
