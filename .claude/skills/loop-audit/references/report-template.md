# Change-Audit Report Template

Fill this structure top to bottom. Keep it a **report**: state what changed, what it touches, how risky it is, and whether tests cover it — do not propose code edits inline (those go under Recommendations as pointers). Match length to the change set: a two-file PR is a short memo, a release is the full form. Delete any section that genuinely does not apply and say why rather than leaving it blank. The classification vocabulary (feature / fix / refactor / breaking / docs / chore) and the risk scale (Low / Medium / High / Critical) are fixed by the parent skill (§2, §4) so every audit is comparable.

---

## TEMPLATE

# Change Audit: <subject>

**Scope:** `<git range or PR>` — <N> files, +<ins>/-<del> · base `<base-sha>` -> head `<head-sha>`
**Audited:** <date> · **Auditor:** <who/agent>

## Summary

<2-3 sentences: what the change set does overall, and the single headline risk a release manager needs to know. Lead with the verdict, not the narrative.>

**Overall risk: <Low | Medium | High | Critical>** — <one-line driver of the rollup rating.>

## Changes by area

One row per logical change (not per file); split mixed commits so each row carries one class.

| Area / file group | Class | What it does |
|---|---|---|
| <area or path glob> | <feature/fix/refactor/breaking/docs/chore> | <one line> |
| … | … | … |

## Impact & blast radius

What depends on the changed code — direct callers, transitive dependents, public API/exports, schema/config/wire contracts. Impact is about reach, not line count (a one-line change to a hot function outranks a 500-line leaf).

- **<change / area>** -> <who and what depends on it; how far the reach goes.>
- **Breaking changes:** <list each public-contract break — API signature, schema/wire format, config key, CLI flag, default — or "none.">
- **Data / schema / migration surface:** <migrations, backfills, one-way operations, or "none.">

## Risk rating

Per-change rating on likelihood × impact, then the rollup. Ratings come from blast radius and reversibility, not diff size.

| Change / area | Rating | Driving factors | Rollback |
|---|---|---|---|
| <area> | <Low/Med/High/Critical> | <complexity, coverage, blast radius, state/IO/concurrency> | <revertable? how — or "irreversible: …"> |

## Test coverage

Judged against the changed lines and changed behavior, not the repo's headline coverage %.

- **Covered:** <changed behaviors with tests that actually assert them.>
- **Gaps:** <changed behavior with no covering test; refactors claimed behavior-preserving with no pinning test.> Authoring these tests is a follow-up (delegate to `loop-test`), not part of this report.

## Security note

<Summary of confirmed findings folded in from the `loop-review` skill run on this diff — severity, category, location — with a pointer to the full review. Fold in only findings that clear that skill's own reporting bar (HIGH/MEDIUM at confidence ≥ 0.8, per its §5); a Low or lower-confidence lead is not a `loop-review` finding and does not belong here. If no review was run: **"No security review run."** and why (e.g. user asked for what/impact/risk/coverage only).>

## Recommendations / follow-ups

- <Actionable next step — add tests, gate a breaking change, stage a migration, run `loop-review`, etc. Pointers only; this report does not make the edits.>

## Open questions

- <Ambiguity or unverifiable assumption that affects the verdict — a base SHA to confirm, an intended-behavior question for the author, a dependent you could not fully trace.>

---

## EXAMPLE (filled in)

# Change Audit: PR #482 — rate-limit the export endpoint

**Scope:** `main...pr-482` — 6 files, +214/-37 · base `a1b9f0c` -> head `7d4e221`
**Audited:** 2026-07-03 · **Auditor:** loop-audit agent

## Summary

Adds a token-bucket rate limiter to `POST /v1/exports` and changes the endpoint's error contract to return `429` with a `Retry-After` header. The limiter is sound, but the new `429` response is a **breaking change** for the two internal clients that currently treat any non-`200` as a fatal error, and no test pins the retry path.

**Overall risk: Medium** — limited blast radius (one endpoint) but a public-contract change with an untested failure mode.

## Changes by area

| Area / file group | Class | What it does |
|---|---|---|
| `api/exports/handler.go` | feature | Wraps the export handler in a token-bucket limiter (60/min/tenant). |
| `api/exports/handler.go` (error path) | breaking | Returns `429` + `Retry-After` instead of the previous `503`. |
| `internal/ratelimit/*` | feature | New reusable token-bucket package. |
| `config/limits.yaml` | chore | Adds `exports.rate_per_min` key (default 60). |

## Impact & blast radius

- **Export handler** -> reached by the dashboard export button and the nightly `reporting-worker` batch job; both hit this endpoint directly.
- **Breaking changes:** error response for over-limit callers changed `503` -> `429`. `reporting-worker` and `sdk-js@<3.2` treat non-`200` as fatal and will not honor `Retry-After` — they will fail the batch instead of backing off.
- **Data / schema / migration surface:** none. New config key is additive with a default.

## Risk rating

| Change / area | Rating | Driving factors | Rollback |
|---|---|---|---|
| Token-bucket limiter | Low | Isolated new package, pure in-memory, covered by unit tests. | Revertable — feature-flagged via config; set `rate_per_min: 0` to disable. |
| `429` error contract | Medium | Changes a public response code; two known clients mishandle it. | Revertable by config, but deployed clients already assume old behavior. |

## Test coverage

- **Covered:** token-bucket refill/consume logic (`ratelimit_test.go`); the happy-path `200` on the export handler.
- **Gaps:** no test asserts the `429` + `Retry-After` response, and no test covers concurrent tenants sharing a bucket. Recommend adding these (follow-up for `loop-test`).

## Security note

`loop-review` run on this diff — 1 confirmed Medium finding (broken access control): the limiter keys on a client-supplied `X-Tenant-Id` header without verifying it against the auth context (`handler.go:88`), letting a caller bypass the rate limit by spoofing another tenant's id. See full review for the source->sink trace. No High findings.

## Recommendations / follow-ups

- Gate the `429` rollout behind a client-compat check, or ship the `503`->`429` change only after `reporting-worker` and `sdk-js` handle `Retry-After`.
- Add tests for the `429` path and concurrent-tenant buckets (`loop-test`).
- Address the Medium security finding by deriving the tenant from the auth context, not the header.

## Open questions

- Is `reporting-worker` pinned to `sdk-js@3.1`? If it is already on `>=3.2`, the breaking-change risk drops to Low.
- Confirmed base is the PR's merge base and not a stale local `main`.
