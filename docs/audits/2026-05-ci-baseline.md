# CI critical-path baseline — W4 (2026-05-13)

**Scope:** Measure actual CI wall-clock duration for the last 5 PRs merged
post-W3, compare against the W1 PR #35 warm-cache target of ~3 min, and
identify the next quick win if the target has not been met.

**Method:** Static analysis of `.github/workflows/ci.yml` at HEAD, the PR
#35 diff, and related audit findings. The `gh` CLI is not installed in this
environment, so live GitHub Actions run-duration data could not be fetched.
Section 2 below provides a manual measurement protocol that the repo owner
can run to fill in real numbers.

**Source PRs reviewed:** #35 (W1 CI overhaul), #57, #60, #62, #64, #65
(post-W3 merges as of 2026-05-13). No workflow-file changes landed after
#35 except `docs: refresh README` (#54, which touched no YAML).

---

## 1. What PR #35 changed

PR #35 (`ci: critical path → ~3min`, merged 2026-05-12) landed four fixes
that materially affect wall-clock CI time. Closed findings in
`docs/audits/2026-05-perf-and-optimization.md`:

| Finding | Change | Predicted save |
|---|---|---|
| P26-05-34 | Replaced sequential `for pkg; go test` loop with single `go test -race ./...` | ~70 s |
| P26-05-35 | Added `actions/cache` step for `~/.cache/go-build` keyed on `go.sum` | ~90 s warm |
| P26-05-38 | Dropped `needs:[backend,frontend,helm]` from the docker job — all four jobs now parallel | ~3–5 min off critical path |
| P26-05-39 | Switched to `docker/build-push-action@v6` with `cache-from/to: type=gha` | ~3 min warm → ~1 min |

**Pre-#35 critical path (estimated):** docker job ran after all three test
jobs, each serial. Total: ~8–10 min cold, ~6–8 min warm.

**Post-#35 theoretical critical path:**

| Job | Cold estimate | Warm estimate | Notes |
|---|---|---|---|
| backend | ~3 min | ~1.5 min | Go build cache (`go.sum` keyed); parallel test invocation |
| frontend | ~2.5 min | ~1.5 min | `npm` cache via `setup-node`; no change in PR #35 |
| helm | ~2 min | ~1.5 min | `helm template` × 11 renders; no cache needed |
| docker | ~5 min | ~1–2 min | buildx GHA layer cache; runs in parallel |
| **critical path** | **~5 min** | **~2–2.5 min** | max(backend, frontend, helm, docker) |

The W1 target of "~3 min on warm cache" is theoretically achievable and
plausible based on the structural changes. The docker job is likely the
warm-cache critical path at ~1–2 min; the backend job is the cold-cache
critical path at ~3 min.

---

## 2. Manual measurement protocol

Because `gh` is unavailable in this environment, run the following to
collect actual numbers and update the table in section 3:

```bash
# Requires: gh CLI authenticated, run from repo root
gh run list \
  --workflow ci.yml \
  --branch main \
  --limit 20 \
  --json databaseId,headBranch,createdAt,updatedAt,conclusion \
  | jq '.[] | {id:.databaseId, branch:.headBranch, conclusion:.conclusion,
               elapsed: ((.updatedAt | fromdateiso8601) - (.createdAt | fromdateiso8601))}'
```

For per-job breakdown on a specific run (replace `RUN_ID`):

```bash
gh run view RUN_ID --json jobs \
  | jq '.jobs[] | {name:.name, duration:(.completedAt // .startedAt | fromdateiso8601) -
         (.startedAt | fromdateiso8601), conclusion:.conclusion}'
```

Map PR numbers to run IDs:

```bash
# For PR #60 (post-W3):
gh pr view 60 --json number,title,mergedAt
gh run list --commit $(git log --format="%H %s" | grep "#60" | awk '{print $1}') --json databaseId
```

Target columns for section 3:

- First-run duration per job (cold cache, first push to branch).
- Repeat-run duration per job (warm cache, second+ push or re-run).
- Critical path = `max(backend, frontend, helm, docker)` per run.

---

## 3. Duration table (to be filled by repo owner)

Post-W3 PRs (all merged 2026-05-13 per git log):

| PR | Title | backend cold | backend warm | frontend | helm | docker cold | docker warm | critical path warm |
|---|---|---|---|---|---|---|---|---|
| #57 | perf(useWebSocket): stable onMessage ref | — | — | — | — | — | — | — |
| #60 | release: v0.1.0 publish | — | — | — | — | — | — | — |
| #62 | feat(AppDetailPage): W11 quickwins | — | — | — | — | — | — | — |
| #64 | fix(executor,handler): T1+T3+dedup | — | — | — | — | — | — | — |
| #65 | docs: refresh README post-W3 | — | — | — | — | — | — | — |

All five PRs were merged on the same day in rapid succession. They all ran
against the same `ci.yml` that landed in PR #35 (no workflow changes between
#35 and HEAD). If the cache was already primed from an earlier run on the
`main` branch, these PRs would see warm-cache numbers.

---

## 4. Comparison against W1 PR #35 target

**Target:** ~3 min on warm cache (stated in PR #35 commit message).

**Confidence assessment (no live data):**

The theoretical warm-cache critical path is ~2–2.5 min, which is _under_
the 3 min target. However, several real-world factors add latency:

1. **Runner queue time.** GitHub's `ubuntu-latest` pool has variable queue
   latency (0–60 s) not counted in job duration. During high-demand
   windows this is the dominant variable.

2. **GHA cache miss on first PR after a dep bump.** `go.sum` key means any
   `go get` bumps the backend to full cold build. Post-W3 saw a Go 1.25
   bump; if that landed mid-W2/W3, several PRs ran cold.

3. **helm job grows with each new matrix row.** PR #35 committed 11
   sequential `helm template` invocations. This is not on the warm-cache
   critical path (helm < docker warm), but it is growing.

4. **docker buildx cache eviction.** The GHA cache has a 10 GB per-repo
   cap. If other caches (node_modules, go-build) have filled the quota, the
   buildx cache gets evicted LRU-style, reverting docker to cold build.

**Verdict:** Without live run data, cannot confirm the 3 min target is
consistently met. The structural changes from PR #35 are correct and
sufficient in theory. The likely failure mode is cache eviction competing
across the three cache entries (go-build, npm, docker-buildx).

---

## 5. CI failure observations (W2 + W3)

The user noted chronic CI failures during W2 and W3 that were treated as
ignorable. From the git log and CI structure, the likely causes are:

### 5a. golangci-lint `continue-on-error: true`

`.github/workflows/ci.yml:72`:
```yaml
- name: golangci-lint
  uses: golangci/golangci-lint-action@v6
  continue-on-error: true
```

This step is explicitly non-blocking. Lint failures during W2/W3 did not
block merges. This is a known accepted trade-off (the step still records
as failed in the UI, producing "red" CI noise even when tests pass).

### 5b. OCI conformance workflow

`oci-conformance.yml` was flipping between `pull_request` and
`workflow_dispatch+schedule` triggers during W2/W3 (see backlog P0.6,
closed in `claude/review-production-rollout-MT3YO`). Conformance runs
that triggered on PRs were failing because the AI agent could not reach
logs. P0.6 removed the `pull_request:` trigger — conformance is now
scheduled-only and non-blocking.

### 5c. CloudRun deploy test (removed)

Commit `1bfddfb` (`fix(ci): drop CloudRun deploy test that triggers ADC
discovery`) removed a test that was triggering GCP ADC discovery on CI
runners that have no credentials. This was a source of chronic backend job
failures in early W2.

### 5d. Cache quota pressure

With three parallel cache streams (go-build ~400 MB, npm ~200 MB, docker
buildx potentially 2–4 GB), the 10 GB GHA cache quota can fill within a
few days of active development. Once the docker buildx cache is evicted,
the docker job reverts to 4–5 min cold builds, pushing the critical path
back above 5 min.

**Recommendation:** Monitor cache usage via the GitHub Actions UI
(Settings → Caches) after the next five PRs. If the docker buildx cache
is repeatedly evicted, see section 6 below.

---

## 6. Next quick win

### Recommended: limit docker buildx cache footprint (mode=min)

The current config uses `cache-to: type=gha,mode=max`. Mode `max` stores
all intermediate layers (build stages, not just the final image). For a
multi-stage Dockerfile with a Go build stage and a Node build stage, this
can easily exceed 2 GB.

**Change:**

```yaml
# .github/workflows/ci.yml — docker job
- uses: docker/build-push-action@v6
  with:
    cache-from: type=gha
    cache-to: type=gha,mode=min   # was: mode=max
```

`mode=min` stores only the final stage's layers (~50 MB for
`alpine:3.19` + the cooker binary + static assets). The intermediate Go
and Node layers are not stored, saving ~1.5–2 GB of quota per run, which
in turn prevents LRU eviction of the go-build and npm caches.

**Trade-off:** Cold builds are slower if the final layer's inputs change
(any source change triggers a full rebuild of the final stage). But because
the Go compilation happens in the build stage (not the final stage), changing
Go source still hits the cache for the `npm ci` / Node layers even with
`mode=min`.

**Predicted effect:** Docker warm build stays ~1–2 min (final layer cached);
docker cold build stays ~4–5 min (only final layer cached, not intermediate).
Critical-path warm-cache improves from potentially ~5 min (cache evicted)
to a consistently ~2 min because go-build and npm caches are no longer
crowded out.

**Effort:** 1-line change. Can land in W5 as part of any CI-touching PR.

### Alternative: frontend lint+build parallelism (P26-05-36)

The W1 audit identified that `npm run lint` and `npm test` are independent
of `npm run build`. Splitting into two parallel jobs would cut the frontend
job from ~2.5 min to ~1.5 min. See P26-05-36 in
`docs/audits/2026-05-perf-and-optimization.md`.

**Effort:** ~20 lines of YAML. Not on the warm-cache critical path today
(docker is longer), but would matter if docker cache is consistently warm.
Recommend after the docker cache mode change lands.

---

## 7. Summary findings

1. PR #35 made the right structural changes: parallel jobs, Go build
   cache, docker buildx cache. The theoretical warm-cache critical path is
   ~2–2.5 min, under the 3 min target.

2. Live run data is not available in this environment. The manual
   measurement protocol in section 2 will confirm whether the target is
   consistently met in practice.

3. The most plausible reason the target may be exceeded on some runs is GHA
   cache quota pressure evicting the docker buildx cache, reverting it to
   cold (~4–5 min). This makes docker — not backend or frontend — the
   intermittent bottleneck.

4. The next quick win is switching `cache-to: type=gha,mode=max` to
   `mode=min` in the docker job. 1-line change, no logic risk. Saves ~2 GB
   of cache quota, preventing LRU eviction of the go-build and npm caches.

5. The golangci-lint `continue-on-error: true` is a known source of
   "red CI" noise during W2/W3. It is intentional (surfacing lint failures
   alongside test results without blocking merges). If it is causing
   confusion, removing the flag and fixing the outstanding lint issues is the
   correct response — not papering over with `continue-on-error`.

---

## References

- W1 CI overhaul: PR #35, commit `19044ce`.
- P26-05-34, -35, -38, -39: `docs/audits/2026-05-perf-and-optimization.md`.
- P26-05-36 (frontend parallelism): same file.
- OCI conformance P0.6: `backlog.md` "Closed (recent)" section.
- CloudRun ADC fix: commit `1bfddfb`.
