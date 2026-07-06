---
name: cooker-ci-debug
description: Triage a failing CI check on a Cooker PR. Trigger on "CI failed", "<check> failed", "the docker job is broken", a github-webhook-activity event with conclusion=failure, or any "why is the workflow red" question. Bias toward identifying the actual failed step before guessing.
---

# Cooker — CI failure triage

The four CI jobs (`backend`, `frontend`, `helm`, `docker`) have very different failure shapes. This skill walks the path from "I got a webhook saying X failed" to "I know exactly which step failed and what to do about it" without burning tokens on speculation.

> This is Cooker's answer key for its four CI jobs. Once the failing step is identified and it isn't in the catalogue below, switch to `/loop-debug` for the general reproduce → localize → root-cause method.

## First: confirm the run is still red

A webhook is a snapshot. By the time you investigate, a newer push may have re-run the same check. **Always reconfirm before debugging:**

```
mcp__github__pull_request_read
  method: get_check_runs
  owner: santapong
  repo: Cooker
  pullNumber: <PR>
```

Look at the **most recent run id** for the failed check name. If a newer run is `success`, **stop** — the failure was already fixed by a later push. (This burned 15 min on PR #23's docker bounce; don't repeat it.)

## Second: identify the actual failed step

```bash
.claude/skills/cooker-ci-debug/inspect.sh <PR-number>
```

Prints:
- The latest run for each check name + its conclusion
- For failed checks, the workflow name + job URL
- A reproduction-command suggestion based on the job

## Per-job runbook

### `backend` failed

The job runs four steps in order. They mostly fail in this order, so check from the top down:

| Step | Local repro | Common cause |
|---|---|---|
| `Build` | `cd backend && go build ./...` | Missing import; `go.mod` / `go.sum` drift after adding a dep |
| `gofmt` | `cd backend && gofmt -l .` | New file missing trailing newline; `gofmt -w` to fix |
| `Vet` | `cd backend && go vet ./...` | Unused import, shadowed variable, struct-tag typo |
| `golangci-lint` | (lint job runs with `continue-on-error`) | Doesn't fail the gate; safe to defer |
| `Test` | `cd backend && go test -race -timeout 90s ./...` | Race detector caught something the cached run hid; new test depends on Postgres service that isn't running locally |

**Postgres-dependent tests** read `DATABASE_URL=postgres://cooker:cooker@localhost:5432/cooker_test?sslmode=disable`. Run them locally with:

```bash
docker run -d --name cooker-test-pg -p 5432:5432 \
  -e POSTGRES_DB=cooker_test -e POSTGRES_USER=cooker -e POSTGRES_PASSWORD=cooker \
  postgres:16-alpine
DATABASE_URL=postgres://cooker:cooker@localhost:5432/cooker_test?sslmode=disable \
  go test -race -timeout 90s ./internal/store/postgres/...
docker rm -f cooker-test-pg
```

### `frontend` failed

Steps: `npm ci` → `npm run lint` → `npm run build` → `npm test`. 

| Step | Local repro |
|---|---|
| `npm ci` | `cd frontend && rm -rf node_modules && npm ci` |
| `npm run lint` | `cd frontend && npm run lint` |
| `npm run build` (`tsc -b && vite build`) | `cd frontend && npm run build` |
| `npm test` | `cd frontend && npm test -- --passWithNoTests` |

`tsc -b` honours `frontend/tsconfig.json` `"include": ["src"]` — so changes to `vite.config.ts` aren't type-checked. Vite loads the config via esbuild (lenient about `process.env`). If `npm run build` fails after a vite.config.ts edit, the failure is at vite-build time, not tsc.

### `helm` failed

Just runs `helm lint deploy/helm/cooker`. Failure means a template rendered to invalid YAML. Repro:

```bash
helm lint deploy/helm/cooker
helm template test deploy/helm/cooker | head -200      # see the rendered output
```

### `docker` failed (this is the one that bit us most often)

Runs `docker build -t cooker:ci -f deploy/docker/Dockerfile .` after `backend / frontend / helm` all pass. Failure modes ranked by likelihood:

| Symptom | Cause | Fix |
|---|---|---|
| `apk add … not found` | apk version pin doesn't exist on the alpine 3.19 mirror | Don't pin individual apk versions; pin alpine via `FROM alpine:3.19@sha256:…` instead |
| `npm ci` fails inside frontend stage but passes in `frontend` job | `--ignore-scripts` flag in the Dockerfile vs no flag in the CI job | Make sure no install-time native module is needed |
| `go build` fails inside backend stage but passes in `backend` job | `CGO_ENABLED=0` in the Dockerfile pins out CGO; some accidental cgo dep | Find the cgo import; replace with pure-Go alternative |
| `kubectl.sha256` mismatch | k8s.io changed the file format from "hash only" to "hash  filename" | Update the sha256sum -c construction in the RUN line |
| `pull access denied … docker.io/library/...` | Docker Hub rate-limit (anonymous pulls capped) | Pin to a digest or move to a logged-in registry; transient, can also retry |

The docker job depends on backend/frontend/helm. If those are red, docker is **skipped** (not failed). A "skipped" docker conclusion just means a prerequisite failed; fix that first.

## Third: reproduce locally before pushing the fix

The biggest CI time-waster is "fix → push → wait 5 min → still red." For each job:

```bash
# backend
.claude/skills/cooker-improve/check-pkg.sh internal/<changed-pkg>

# frontend
cd frontend && npm run lint && npm run build && npm test -- --passWithNoTests

# docker — needs docker daemon
docker build -t cooker:debug -f deploy/docker/Dockerfile .

# helm — needs helm CLI
helm lint deploy/helm/cooker
```

If you don't have docker locally, push a small commit with `--progress=plain` added to the workflow's `docker build` invocation to make the next failed build's log readable (then revert when fixed).

## When you can't reach the log

The MCP github tools don't include a "fetch action log" method, and the local GitHub mirror in this dev environment doesn't expose the logs over HTTP either. The fallback path:

1. Reconfirm the failure is current via `get_check_runs`.
2. Run the per-job repro commands above locally.
3. If still stuck, ask the user to paste the failed step's log — they can read it directly.

Don't guess at fixes from the Dockerfile when the actual failure could be on any of ~6 lines. Reproduce or ask.

## Anti-patterns

- Don't push a "blind" Dockerfile change without first confirming the run is still red (`get_check_runs`).
- Don't `cat` workflow files. Use Read.
- Don't add `set -x` to every RUN line — make the workflow itself emit `--progress=plain` for one cycle, then revert.
- Don't pin alpine apk packages without verifying versions (re-broke `docker` job at d4c2d64; same trap is reachable again).
