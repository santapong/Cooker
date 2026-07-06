# Credit-Horizon Hook

What the loop is missing that has nothing to do with verification or scheduling: it
never learns from its own track record. Every round runs the same fixed triage logic,
whether the last ten `tech-debt` proposals were merged clean or rejected as noise. This
is the gap the agentic-optimization framing names directly — of systems built with the
tooling to close this loop, most never do. This hook closes it here.

> **Status:** the **write side** (the ledger + the reconcile pass in
> `templates/credit-ledger.workflow.js`) ships now. The **read side** (biasing triage
> with `trustWeight`) is a deliberate deferred follow-up — see "Two hooks" below. Wiring
> a read against an empty, all-`0.6` ledger teaches the triage agent nothing; let the
> reconcile pass accumulate at least one `BATCH_SIZE` of real outcomes first.

## Mapping the three knobs onto this loop

The thing being iteratively improved here isn't a single prompt or artifact — it's the
**triage policy**: the rule that turns `{kind, detail}` into `{worthDoing, priority}`.

- **Starting artifact** — before any outcome data exists, triage treats every `kind`
  identically (today's actual behavior). That uniform prior is knob 1's starting point,
  not a placeholder to feel bad about.
- **Credit horizon** — how far back an outcome gets attributed. This loop's causal chain
  is short and clean: one triage decision → one PR → one terminal outcome, no
  multi-step diffusion of blame. That's a best-case credit horizon (effectively 1 step),
  which is exactly why this hook is low-risk to add here even though it's hard in
  general — the credit-assignment problem mostly evaporates when the chain is this short.
- **Experience batching** — how many outcomes accumulate before the policy updates.
  Recomputing a trust weight after every single PR is pure noise (one rejected
  `tech-debt` PR doesn't mean the category is bad). Batch it.

## The ledger

Persisted as the body of a single dedicated, labeled GitHub issue — `🤖 Credit Ledger
(automated, do not edit)`, label `credit-ledger` — so it reuses the exact
read/write primitives (`issue_read`, `update_issue`) already in use everywhere else in
this plugin. No new storage primitive. Create it once by hand (see `deployment.md`).

```json
{
  "kinds": {
    "ci-failure":     { "proposed": 0, "merged": 0, "mergedWithChanges": 0, "rejected": 0, "stale": 0, "trustWeight": 0.6 },
    "issue":          { "proposed": 0, "merged": 0, "mergedWithChanges": 0, "rejected": 0, "stale": 0, "trustWeight": 0.6 },
    "pr-comment":     { "proposed": 0, "merged": 0, "mergedWithChanges": 0, "rejected": 0, "stale": 0, "trustWeight": 0.6 },
    "tech-debt":      { "proposed": 0, "merged": 0, "mergedWithChanges": 0, "rejected": 0, "stale": 0, "trustWeight": 0.6 },
    "research-idea":  { "proposed": 0, "merged": 0, "mergedWithChanges": 0, "rejected": 0, "stale": 0, "trustWeight": 0.6 }
  },
  "pendingSinceRecalc": 0,
  "lastRecalc": null
}
```

`trustWeight` starts at 0.6 (mild optimism — untested categories get a fair shot, not
punished for having no history) and only recomputes once `pendingSinceRecalc` crosses
`BATCH_SIZE` (default 10 — knob 3):

```
trustWeight[kind] = (merged + 0.5 * mergedWithChanges) / proposed
```

## Two hooks into the existing loop

**Before triage (read) — DEFERRED:** load the ledger once per round and pass each item's
kind's `trustWeight` into the triage prompt as context, **not** as a hard filter — a low
weight should make the triage agent demand a clearer case, not auto-reject the kind
entirely (a bad week for `tech-debt` shouldn't permanently blacklist it). Wire this only
after the ledger holds real data (see Status above).

**After outcomes resolve (write) — SHIPPED:** outcomes aren't known when a PR is
proposed — they arrive later, on the reviewer's schedule, not the loop's. This needs a
**separate reconcile pass**, not a hook inside the main loop: periodically (daily is
reasonable), query every PR the loop has ever opened (label `automated`) that's closed
since the last reconcile, classify its fate, increment the matching ledger counters, and
recompute `trustWeight` for any kind that crossed `BATCH_SIZE`. See
`templates/credit-ledger.workflow.js`.

## Runtime note (H10)

Workflow scripts can't read the clock (`Date.now()` / argless `new Date()` throw —
harness policy H10), so the reconcile template takes the current time as `args`
(`nowMs`, `nowIso`) supplied by the deploying Routine, and uses `Date.parse()` for the
fixed timestamps it reads off PRs. Treat the template as a draft to run once against a
scratch repo before trusting its ledger math — the same way you'd treat any new template
in this plugin.
