# Game-changer ideas (post Phase 1 + Phase 2)

**Status:** suggestions, not blessed roadmap items. To be reviewed
by the PM, then either promoted into [`backlog.md`](../backlog.md)
or moved to a parking-lot doc.

The context: Phase 1 + Phase 2 (PR #89) made Cooker's **engine**
trustworthy — durable jobqueue, formal state machine, RBAC + MFA,
notifications, cron, multi-provider webhooks. The picks below are
about making the **product** indispensable. Each one leans on a
specific Cooker strength rather than chasing whatever Dokploy or
GitHub Actions ships next.

Cooker's actual moat is the **visual DAG editor**. Picks ranked
first when they leverage that moat.

---

## Decision framework

A Cooker feature is worth shipping if it answers at least one of:

1. **Does it leverage the visual canvas?** (Only Cooker can do it.)
2. **Does it close a top-3 CI/CD pain point?** (Debugging failures,
   re-running unchanged work, reviewing pipeline changes.)
3. **Does it stand on Phase 1 + Phase 2 primitives without
   reinventing?**

If the answer to all three is no, it's probably an "upcoming"
entry, not a game-changer.

---

## Picks

Each pick has: a one-line pitch, the why, what Cooker already has
that it builds on, an effort estimate, the first-week concrete
tasks, and the honest risk + mitigation.

### G1 — AI failure explainer that opens a draft PR

**Pitch.** When a stage fails, read the logs + the relevant files
in the repo + the stage definition, then open a **draft PR with a
proposed fix** on the user's branch. Not "here's what's wrong."
Here's the patch.

**Why game-changing.** The #1 time-sink in CI/CD is debugging
failures. Every tool shows logs. None ship a fix. This is the only
pick that directly addresses "I just want my pipeline to be green."

**Builds on.** Phase 2 F1 notifier (knows when a run failed); Phase
2 F3 git provider adapters (can post PR comments, open branches);
`internal/source/github` (already has clone helpers).

**Effort.** 6–8 weeks. Bulk of it is the heuristic catalog (regex
table of "error pattern → likely fix"). The LLM fallback is ~50 LOC.

**First-week tasks.**
1. New package `internal/explainer/` with `Explainer` interface.
2. `internal/explainer/heuristic/` — a YAML / Go-struct catalog of
   ~30 common build errors ("no such file or directory",
   "undefined symbol", "connection refused on port X", etc.) and
   their proposed remediations.
3. Wire `JobQueueRunner.dispatchOutcome` to call
   `Explainer.Explain(ctx, run, exec_err) (PRDraft, error)` on
   terminal failure.
4. New `internal/git/draft_pr.go` — opens a branch + commits the
   patch + creates a draft PR via the provider adapter.
5. Feature flag `COOKER_EXPLAINER_ENABLED` (default off).

**Risk.** False positives (wrong fix proposed). **Mitigation:** draft
PR, never auto-merge. Operators review.

**Tier.** Tier 1 (Q1 2027) — highest "wow factor," highest
implementation risk; ship after the platform stabilises.

---

### G2 — Content-addressable stage cache (Bazel-style)

**Pitch.** Hash a stage's inputs (source files matched by globs,
env vars, image tags). If the hash matches a previous successful
run, **skip the stage and reuse its artifacts**. Test stages, lint
stages, build stages — all eligible.

**Why game-changing.** In a monorepo, 80% of pipeline runs
re-execute unchanged work. This is the difference between "CI runs
in 12 minutes" and "CI runs in 90 seconds." Every monorepo team
feels this pain. Nx and Turborepo do it for JS; no equivalent in
general-purpose CI/CD.

**Builds on.** Cooker is already OCI-native end-to-end — the cache
store is an OCI blob, content-addressable by digest, served by an
existing registry. The jobqueue's `ConcurrencyKey` is a similar
opaque-string lookup pattern.

**Effort.** 4–6 weeks. The hash semantics are the hard part;
cache hit/miss plumbing is straightforward.

**First-week tasks.**
1. Add `cache_keys` JSONB column to `stage_runs` (migration
   `014_stage_cache_keys.up.sql`).
2. New package `internal/stagecache/` with `Key(inputs)` (SHA-256
   over normalised JSON) and `Lookup(ctx, key) (Artifacts, bool)`.
3. Per-stage-type `Inputs()` hook in `internal/builder/`,
   `internal/pusher/`, etc. (each strategy declares what
   constitutes its inputs).
4. Executor wraps each stage in `if hit := cache.Lookup(...); hit
   { ... } else { run + cache.Store(...) }`.
5. UI: "cache hit" badge on stages that hit.

**Risk.** Stale cache hits if hash inputs are incomplete (a stage
reads a file the hash didn't include). **Mitigation:** hash inputs
are **explicit per stage type**, not auto-detected. Operators opt in
per stage. `cache: false` opts out.

**Tier.** Tier 1 (Q4 2026) — highest perf impact for paying
customers (monorepo teams).

---

### G3 — Visual pipeline diff for PR review

**Pitch.** When a PR changes pipeline definitions, render a
**side-by-side canvas diff** in the PR comment: green for added
stages/edges, red for removed, yellow for modified. Reviewer
clicks "approve pipeline change" before the new version takes
effect.

**Why game-changing.** Reviewing YAML pipeline changes is brutal —
indented YAML diffs are unreadable. Cooker is the **only** CI/CD
tool that could ship this because it's the only one with a visual
DAG. The competitive moat is genuine, not synthetic.

**Builds on.** Pipeline JSONB is already version-able. Frontend
renders the canvas already. Phase 2 F3 git providers can post PR
comments.

**Effort.** 3–4 weeks. Mostly frontend.

**First-week tasks.**
1. `internal/pipelinediff/` package: `Diff(oldDef, newDef)` returns
   `{addedStages, removedStages, addedEdges, removedEdges,
   modifiedStages}`.
2. Server-side SVG renderer (D3-server or headless Chromium) that
   produces a side-by-side image from two `model.Pipeline` values.
3. Webhook handler for PR-opened events: detect pipeline-file
   changes, render diff, post as PR comment via the existing git
   provider adapter.
4. Optional: a `cooker review` CLI for the same operation locally.

**Risk.** Server-side rendering is fragile. **Mitigation:** fall
back to a Markdown table + link to a live diff view in Cooker.

**Tier.** Tier 1 (Q3 2026) — Cooker-only feature; immediate
competitive talking point. Smallest effort of the Tier 1 picks.

---

### G4 — Pipeline dry-run (`terraform plan` for pipelines)

**Pitch.** Run the DAG against **mocked builders and deploy
targets**: skip the actual `kaniko build`, but step through every
stage, emit logs, simulate timing. Output: "would have built 3
images, would have deployed to staging, would have taken ~8
minutes."

**Why game-changing.** Pipeline changes today are
deployed-then-tested-in-production. Dry-run shifts validation left
and unlocks CI-on-pipeline-changes: every PR touching a pipeline
runs `cooker dry-run` automatically.

**Builds on.** The `Noop` implementations of
`internal/builder/`, `internal/pusher/`, `internal/deployer/` already
exist for tests. The executor accepts these via `service.With*`
options — no executor changes needed.

**Effort.** 4–6 weeks. The hard part is making the simulated
output trustworthy.

**First-week tasks.**
1. New `internal/builder/simulation.go`,
   `internal/pusher/simulation.go`,
   `internal/deployer/simulation.go` — each wraps `Noop` and
   records what it would have done.
2. New endpoint `POST /api/v1/pipelines/:id/dry-run` that builds an
   executor with simulation strategies, runs it, returns the
   recorded trace.
3. CLI: `cooker dry-run pipeline.json`.
4. CI integration: a `dry-run` GitHub Action that posts results to
   the PR.

**Risk.** "It dry-runs fine but fails in production."
**Mitigation:** share the same DAG validation code
(`service.ValidatePipelineDAG`). Document explicitly that dry-run
catches structural errors, not runtime errors in the actual
builder.

**Tier.** Tier 2 (parking lot for now) — wait until pipeline
changes happen often enough to justify the investment.

---

### G5 — Interactive approvals in Slack / Discord / Teams

**Pitch.** When a pipeline hits an approval gate, post an
interactive message ("Deploy to prod?" + Approve / Reject buttons)
to a configured channel. Click → approval recorded → pipeline
continues.

**Why game-changing.** Pure UX win. Today approvers context-switch
to the Cooker UI. This eliminates that friction and stands on the
Phase 2 F1 notifier we just shipped.

**Builds on.** Phase 2 F1 notifier (outbound message); Phase 2 F3
git provider HMAC verification pattern (inbound webhook signing).
The `auth.CanApprovePromotion` check is already the authority on
who can approve.

**Effort.** 1–2 weeks per channel.

**First-week tasks.**
1. Extend `notifier.Event` with `Type = approval.requested` and a
   correlation token (signed HMAC of `runID + envID + nonce`).
2. Slack interactive payload (`blocks: [actions: [{button:
   approve, action_id: "cooker:approve:<token>"}]]`).
3. New endpoint `POST /webhooks/slack/interactive` (HMAC-verified
   per Slack's signing secret); decodes the token, calls
   `auth.CanApprovePromotion`, advances the run.
4. Discord, Teams equivalents on follow-up weeks.

**Risk.** Inbound webhook signature has to be airtight —
impersonation = silent prod deploy. **Mitigation:** reuse Phase 2 F3
patterns (`hmac.Equal`, `subtle.ConstantTimeCompare`). Tokens
encode `runID + envID` so a captured token can't be replayed for a
different run.

**Tier.** Tier 1 (Q3 2026) — cheapest pick. Land first to build
momentum.

---

## Anti-picks (explicit don't-do list)

These sound exciting in product calls. Each one would actively
harm Cooker's positioning or scope.

| Idea | Why skip |
|---|---|
| **Cooker Apps / PaaS mode** (run user apps, not just build/ship them) | Contradicts the product positioning. The whole point of Phase 1+2 was "stay a CI/CD tool, don't become Dokploy." Reversing now is whiplash. |
| **Multi-tenant SaaS / hosted Cooker** | Massive scope (auth, billing, isolation, abuse handling). Worth doing as a separate venture, not bolted on. |
| **Live pipeline editing during a running run** | Technically interesting, narrow user value. Most teams want pipelines to be predictable, not mutable. |
| **Cost dashboard / FinOps view** | Nice-to-have, not a differentiator. Operators have Grafana for this. |
| **Bring-your-own DSL** (YAML / HCL / Starlark for primary authoring) | The canvas IS the differentiator. Adding a DSL as the primary authoring path dilutes that. CKR-DSL stays scoped to import-only. |
| **Plugin marketplace with third-party plugins** | Distracting; commits Cooker to long-term API stability for plugins before the core API has stabilised. Revisit in 2 years. |
| **Self-hosted git server** | Out of scope; not a CI/CD problem. Operators use Gitea / GitLab / Forgejo. |
| **Container registry** | Same; GHCR / Harbor / Quay do this. Cooker is the *client*, not the registry. |

---

## Suggested sequence

If the team has 6 months and one engineer:

| Quarter | Pick | Why this slot |
|---|---|---|
| **Q3 2026** | G5 (Interactive approvals) | Cheapest. Builds on F1 notifier. Quick win that gets users excited. |
| **Q3 2026** | G3 (Visual pipeline diff) | Cooker-only feature. Highest-leverage marketing material per dev-week spent. |
| **Q4 2026** | G2 (Stage cache) | Biggest perf impact for monorepo teams. Hardest engineering of the cheap picks. |
| **Q1 2027** | G1 (AI failure explainer) | Highest-risk, highest-reward. Land after the platform has cooled for a quarter. |
| **Parking lot** | G4 (Dry-run) | Useful but not urgent. Promote when pipeline changes happen often enough to justify. |

---

## Narrative framing

After Phase 1+2 alone, Cooker can credibly claim:

> *"The only CI/CD tool with a visual DAG that's still trustworthy
> at production scale."*

The game-changer picks reinforce that claim:

- **G3 + G1** — *"The only CI/CD tool where you can see and fix
  pipeline changes visually."*
- **G2 + G5** — *"… and the engine is fast enough and friendly
  enough to use day-to-day."*

The Phase 1+2 work made the engine trustworthy. These picks make
the product indispensable. Don't confuse the two; both layers
matter.

---

## Open questions for the PM

Before promoting any of these into `backlog.md`:

1. **G1 AI explainer:** is the team comfortable with the latency /
   cost / vendor lock-in tradeoff of an optional hosted LLM call,
   or must this be fully offline?
2. **G2 stage cache:** which stage types should ship with cache
   support in v1? Test + Build are obvious; what about Deploy?
   (Cache-replaying a deploy is risky.)
3. **G3 visual diff:** which git providers must be supported on day
   one? GitHub-only would shrink scope.
4. **G5 interactive approvals:** which channel first? Slack is the
   safest bet; Teams has the most enterprise upside.
5. **Sequencing:** is the 4-quarter sequence above the right one, or
   does the team want to push G1 earlier to maximise novelty?

---

## Integration into the main backlog

When the PM blesses any of the above:

1. Copy the chosen item's section into [`backlog.md`](../backlog.md)
   under an appropriate area heading.
2. Drop the item from this file.
3. Update the `Suggested sequence` table here to reflect what's
   actually scheduled.

When all five picks are either promoted or rejected, this file can
be deleted.
