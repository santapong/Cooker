# CI critical-path baseline — W4 (2026-05-13)

**Scope:** Measure CI wall-clock duration for the last 5 PRs post-W3,
compare against the W1 PR #35 warm-cache target of ~3 min, identify the
next quick win.

**Limitation:** The `gh` CLI is not installed in this environment. Live
GitHub Actions run-duration data could not be fetched. Section 2 provides
a manual measurement protocol. All estimates below are derived from static
analysis of `.github/workflows/ci.yml` and the PR #35 diff.

---

## 1. What PR #35 changed (W1 baseline)

PR #35 (`ci: critical path → ~3min`, merged 2026-05-12, commit `19044ce`)
landed four structural fixes. Source: closed findings in
`docs/audits/2026-05-perf-and-optimization.md`.

| Finding | Change | Predicted save |
|---|---|---|
| P26-05-34 | `for pkg; go test` → single `go test -race ./...` | ~70 s |
| P26-05-35 | `actions/cache` for `~/.cache/go-build` keyed on `go.sum` | ~90 s warm |
| P26-05-38 | Dropped `needs:[backend,frontend,helm]` from docker job — all four jobs parallel | ~3–5 min off critical path |
| P26-05-39 | `docker/build-push-action@v6` with `cache-from/to: type=gha,mode=max` | ~3 min warm → ~1 min |

No further `ci.yml` changes landed between PR #35 and HEAD (confirmed via
`git log --all -- .github/workflows/ci.yml`; only a `docs:` commit at #54
touched `ci.yml` after #35, with no YAML delta).

---

## 2. Theoretical job durations (post-PR #35)

| Job | Cold | Warm | Notes |
|---|---|---|---|
| backend | ~3 min | ~1.5 min | Go build cache + parallel test invocation |
| frontend | ~2.5 min | ~1.5 min | `npm` cache via `setup-node` |
| helm | ~2 min | ~1.5 min | 11 sequential `helm template` renders; no dep to cache |
| docker | ~5 min | ~1–2 min | buildx GHA layer cache |
| **critical path** | **~5 min** | **~2–2.5 min** | `max(backend, frontend, helm, docker)` |

The W1 target of ~3 min on warm cache is structurally achievable. Docker
is the warm-cache critical path at ~1–2 min; backend is the cold-cache
critical path at ~3 min.

---

## 3. Duration table — last 5 PRs (to be filled)

All five post-W3 PRs merged on 2026-05-13 against the same `ci.yml`.

| PR | Title | backend | frontend | helm | docker | critical path |
|---|---|---|---|---|---|---|
| #57 | perf(useWebSocket): stable onMessage ref | — | — | — | — | — |
| #60 | release: v0.1.0 publish | — | — | — | — | — |
| #62 | feat(AppDetailPage): W11 quickwins | — | — | — | — | — |
| #64 | fix(executor,handler): T1+T3+dedup | — | — | — | — | — |
| #65 | docs: refresh README post-W3 | — | — | — | — | — |

**Manual measurement protocol:**

```bash
# Per-run summary (replace RUN_ID with output of the list command)
gh run list --workflow ci.yml --branch main --limit 20 \
  --json databaseId,createdAt,updatedAt,conclusion

# Per-job breakdown
gh run view RUN_ID --json jobs \
  | jq '.jobs[] | {name:.name, conclusion:.conclusion,
      duration: ((.completedAt | fromdateiso8601) - (.startedAt | fromdateiso8601))}'
```

---

## 4. Comparison against W1 target

**Target:** ~3 min warm cache (PR #35 commit message).
**Verdict:** Structurally plausible but not confirmed with live data.

Three real-world factors may push past 3 min on some runs:

1. **GHA runner queue time** (0–60 s) is not counted in job duration.
2. **Cache eviction.** With go-build (~400 MB), npm (~200 MB), and docker
   buildx (`mode=max`, potentially 2–4 GB) all competing for the 10 GB
   per-repo GHA cache quota, the buildx cache is the most likely LRU
   victim. When evicted, docker reverts to ~4–5 min cold, pushing the
   critical path above 5 min.
3. **Dependency bump cold builds.** Go 1.25 bump during W2/W3 invalidated
   the `go.sum` key and reverted backend to full cold build on the first
   post-bump PR.

---

## 5. Chronic CI failures observed in W2+W3

| Cause | Status | Recommendation |
|---|---|---|
| `golangci-lint continue-on-error: true` | Intentional. Step fails visibly but does not block merges. | If confusion persists, fix the lint issues and remove the flag rather than accepting chronic red noise. |
| OCI conformance `pull_request:` trigger | Closed P0.6 — removed in `claude/review-production-rollout-MT3YO`. Now schedule-only and non-blocking. | No action. |
| CloudRun deploy test triggering ADC discovery | Fixed in commit `1bfddfb` (`fix(ci): drop CloudRun deploy test...`). | No action. |
| Docker buildx cache eviction | Suspected based on cache quota math. Not confirmed. | See section 6 (next quick win). |

**Recommendation on further investigation:** Wait until the measurement
protocol in section 3 is run. If docker warm-cache duration is consistently
>2 min, the eviction hypothesis is confirmed and the fix in section 6
should land in W5. If warm cache is <2 min across all five PRs, no action
needed.

---

## 6. Next quick win — docker buildx `mode=min`

**Finding:** `cache-to: type=gha,mode=max` stores all intermediate build
layers (~2–4 GB for a multi-stage Go+Node Dockerfile). This crowds out the
go-build and npm caches in the 10 GB quota.

**Fix (1 line in `.github/workflows/ci.yml`):**

```yaml
cache-to: type=gha,mode=min   # was: mode=max
```

`mode=min` stores only the final stage (~50 MB), preserving quota for the
go-build and npm caches that benefit more sequential runs.

**Trade-off:** Intermediate Go and Node layers are not cached; source
changes require a full intermediate rebuild (~3 min). Final layer (the
stripped binary + static files) is still cached, so pushes that touch only
docs or config files get a fast final-stage build.

**Effort:** S (1 line). Can land in W5 as part of any CI-touching PR.

**Predicted effect:** Docker warm build remains ~1–2 min; go-build and npm
caches stop being evicted, keeping backend warm at ~1.5 min. Critical-path
warm stabilises at ~2–2.5 min consistently rather than spiking to ~5 min
after cache eviction.

### Alternative: frontend lint+build parallelism (P26-05-36)

Split `npm run lint` + `npm test` into a parallel job alongside `npm run
build`. Saves ~30 s on the frontend job. Not on the warm-cache critical
path today (docker > frontend). Worthwhile after the docker cache fix lands.
Effort: ~20 lines of YAML. See P26-05-36 in
`docs/audits/2026-05-perf-and-optimization.md`.

---

## 7. Summary

| Metric | Value |
|---|---|
| W1 PR #35 warm-cache target | ~3 min |
| Theoretical warm-cache critical path (post-PR #35) | ~2–2.5 min |
| Live data available | No — `gh` not installed; use protocol in §3 |
| Next quick win | `docker buildx cache-to: mode=min` (1 line, W5) |
| Most likely failure mode | GHA cache eviction forcing docker cold build |

---

## References

- W1 CI overhaul: PR #35, commit `19044ce`.
- P26-05-34, -35, -36, -38, -39: `docs/audits/2026-05-perf-and-optimization.md`.
- OCI conformance P0.6: `backlog.md` "Closed (recent)".
- CloudRun ADC fix: commit `1bfddfb`.
