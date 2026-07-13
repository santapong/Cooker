#!/usr/bin/env bash
# Locate code in Cooker by intent.
#
# Usage:
#   .claude/skills/cooker-find/where-is.sh <noun-or-phrase>
#
# Resolves a domain word ("pipeline", "auth", "buildah", "migration"…)
# into the canonical file paths via the table baked into
# cooker-find/SKILL.md. Output is parseable: one path per line.
#
# Falls back to ripgrep across the backend if the noun isn't in the
# table, so the script always returns something useful.

set -euo pipefail

if [ $# -eq 0 ]; then
  cat <<'USAGE'
usage: where-is.sh <noun>

Examples:
  where-is.sh pipeline       # the pipeline handler + service + store
  where-is.sh kaniko         # the Kaniko builder adapter
  where-is.sh oidc           # OIDC middleware
  where-is.sh migration      # the migrations directory
  where-is.sh "rate limiter" # passes through to ripgrep
USAGE
  exit 2
fi

noun="$(echo "$1" | tr '[:upper:]' '[:lower:]' | tr -d ' ')"
root="$(git rev-parse --show-toplevel)"

# Curated map. Keep aligned with the table in
# .claude/skills/cooker-find/SKILL.md so ripgrepping for "Who-owns-what"
# always agrees with what this script prints.
case "$noun" in
  pipeline|pipelines)
    paths=(
      "backend/internal/handler/pipeline.go"
      "backend/internal/service/executor.go"
      "backend/internal/service/pipeline.go"
      "backend/internal/store/postgres/pipeline.go"
      "backend/internal/model/pipeline.go"
    )
    ;;
  run|runs|pipelinerun)
    paths=(
      "backend/internal/handler/pipeline.go"
      "backend/internal/server/runs.go"
      "backend/internal/store/postgres/run.go"
      "backend/internal/model/run.go"
      "backend/pkg/dagrunner/runner.go"
    )
    ;;
  app|apps|deploy|deployer)
    paths=(
      "backend/internal/handler/app.go"
      "backend/internal/service/app_deployer.go"
      "backend/internal/store/postgres/app.go"
      "backend/internal/deploy/deployer/clientgo.go"
    )
    ;;
  environment|environments|env|secret|secrets)
    paths=(
      "backend/internal/handler/environment.go"
      "backend/internal/store/postgres/environment.go"
      "backend/internal/secrets"
      "backend/internal/crypto/codec.go"
    )
    ;;
  host|hosts)
    paths=(
      "backend/internal/handler/host.go"
      "backend/internal/store/postgres/host.go"
      "backend/internal/transport/tsnet/real.go"
    )
    ;;
  user|users|auth|oidc|rbac)
    paths=(
      "backend/internal/auth/oidc.go"
      "backend/internal/auth/rbac.go"
      "backend/internal/auth/local"
      "backend/internal/handler/auth_local.go"
    )
    ;;
  builder|build|kaniko|buildah|buildkit)
    paths=(
      "backend/internal/build/builder/builder.go"
      "backend/internal/build/builder/kaniko.go"
      "backend/internal/build/builder/buildah.go"
      "backend/internal/build/builder/buildkit.go"
    )
    ;;
  pusher|push|registry)
    paths=(
      "backend/internal/build/pusher/pusher.go"
      "backend/internal/build/pusher/crane.go"
      "backend/internal/build/pusher/docker.go"
    )
    ;;
  websocket|ws|hub)
    paths=(
      "backend/internal/server/websocket.go"
      "backend/internal/server/wshub_backend.go"
      "backend/internal/server/wsticket.go"
      "backend/internal/server/wsticket_redis.go"
    )
    ;;
  ratelimit|ratelimiter)
    paths=(
      "backend/internal/server/ratelimit.go"
      "backend/internal/server/ratelimit_redis.go"
    )
    ;;
  audit|auditlog)
    paths=(
      "backend/internal/audit/audit.go"
      "backend/internal/server/middleware_audit.go"
    )
    ;;
  observability|metrics|tracing)
    paths=("backend/internal/observability/observability.go")
    ;;
  config|configuration)
    paths=(
      "backend/internal/config/config.go"
      ".env.uat.example"
    )
    ;;
  migration|migrations|schema)
    paths=(
      "backend/internal/store/postgres/store.go"
      "backend/internal/store/postgres/migrations"
    )
    ;;
  retry)            paths=("backend/internal/retry/retry.go") ;;
  validate|validation) paths=("backend/internal/validate/validate.go") ;;
  idempotency|idempotent) paths=(
        "backend/internal/idempotency/idempotency.go"
        "backend/internal/server/middleware_idempotency.go"
      ) ;;
  github|webhook|clone)
    paths=(
      "backend/internal/handler/app.go"
      "backend/internal/source/github/webhook.go"
      "backend/internal/source/github/clone.go"
    )
    ;;
  gitops)
    paths=(
      "backend/internal/gitops/gogit.go"
      "backend/internal/gitops/writer.go"
    )
    ;;
  helm|chart)       paths=("deploy/helm/cooker") ;;
  kubernetes|k8s|rbac.yaml) paths=("deploy/kubernetes") ;;
  dockerfile|docker)        paths=("deploy/docker/Dockerfile") ;;
  ci|workflow)              paths=(".github/workflows") ;;
  audit-doc|audits)
    paths=(
      "docs/audits/dag-performance.md"
      "docs/audits/spof-and-database.md"
      "docs/audits/crash-and-service-quality.md"
      "docs/audits/vulnerabilities-and-chains.md"
      "docs/audits/chain-recheck.md"
      "docs/audits/remediation-plan.md"
    )
    ;;
  *)
    # Fallback: ripgrep across the backend for the term as a symbol.
    echo "# no curated entry for '$noun'; falling back to ripgrep -l" >&2
    cd "$root"
    rg -l --hidden --glob '!**/node_modules/**' --glob '!**/.git/**' \
       --case-sensitive=false -- "$1" || true
    exit 0
    ;;
esac

cd "$root"
for p in "${paths[@]}"; do
  [ -e "$p" ] && echo "$p"
done
