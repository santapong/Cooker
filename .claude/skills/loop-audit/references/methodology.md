# Change-Audit Methodology

The full procedure behind the `loop-audit` skill: turn a set of changes — a working tree, a commit range, a PR, or a release span — into a risk-ranked audit through precise scoping, per-change classification, blast-radius tracing, a risk-factor pass, and a coverage assessment. The orchestration shape (parallel tracing, adversarial risk verification) is governed by the `loop-engine` skill's harness and loop policies. Read this before any non-trivial audit.

An audit is not a code review. A review asks "is this code correct?"; an audit asks "what could this change break, how far does it reach, and is that reach tested?" You inventory and rank risk across the whole change set — you do not need to prove a bug to flag a change as high-risk, only to show it touches something dangerous or far-reaching.

## Step 1 — Establish the change set precisely

**You cannot audit what you have not bounded — pin the exact set of changes before reading a single hunk.** An audit against the wrong base is worse than none: it silently omits changes or invents them.

Pick the boundary that matches the ask, then materialize it:

| Ask | Boundary | Command |
|---|---|---|
| "audit my changes" | working tree vs base | `git diff --stat` then `git diff <base>` |
| "audit this branch / PR" | merge base → head | `git merge-base main HEAD`, then `git diff <mergeBase>..HEAD` |
| "audit this release" | tag → tag | `git diff <prevTag>..<newTag> --stat` |
| "audit commit X" | single commit | `git show <sha>` |

Start with `--stat` for the shape — how many files, how big, where the churn concentrates — then the full diff for content. For a PR, reconcile the diff against the PR's file list so a force-push or a stale base doesn't leave you auditing the wrong revision. Record the base and head SHAs in the report; a reader must be able to reproduce the exact set you judged. Note deleted and renamed files explicitly — `git diff -M` surfaces renames — because a rename that changes a public path is a breaking change hiding as a move.

## Step 2 — Classify each change and flag what breaks

**Every changed hunk gets one classification, and any semver-breaking change is flagged loudly regardless of how small the diff is.** A one-line default change can break every caller; diff size is not risk size.

Classify each logical change into exactly one bucket: `feature`, `fix`, `refactor`, `breaking`, `docs`, `chore`. Refactor means behavior-preserving by intent — if you cannot convince yourself behavior is preserved, it is not a refactor, it is an unlabeled feature or fix and should be traced as one.

Then run the semver-breaking filter over the change set. Flag as **breaking** any of:

- **Removed or renamed public API** — an exported function, class, method, endpoint, CLI flag, or env var that external callers depend on, deleted or renamed.
- **Changed signature** — added required parameter, reordered or removed parameters, narrowed accepted types, changed return type or shape.
- **Changed default** — a config default, feature-flag default, timeout, retry count, or behavioral default flipped. Callers who relied on the old default get new behavior silently.
- **Data or schema migration** — a DB migration, serialization-format change, wire-protocol change, or on-disk format change. These are breaking *and* often irreversible (see Step 4).

"Public" is relative to the module's contract: for a library it is the exported surface; for a service it is its API and events; for an internal module it is whatever other modules import. Judge against the actual dependents (Step 3), not a guess.

## Step 3 — Blast-radius tracing

**For every changed symbol, trace who depends on it; the audit's headline is local-vs-far-reaching, and you establish that by finding callers, not by eyeballing the diff.** A three-line change to a leaf utility called in two hundred places is a bigger event than a hundred-line change to a script nobody imports.

For each changed symbol (function, type, constant, config key, table, endpoint), trace outward:

- **Callers / dependents** — grep the repo for every reference to the symbol. Count and locate them. For an exported symbol, note that dependents may live outside this repo (downstream consumers) and say so.
- **Public surface** — is this symbol reachable from an API, CLI, event, or exported module? If yes, its blast radius crosses the repo boundary.
- **Config touchpoints** — does the change read or write a config key, env var, or feature flag? Trace every read site; a changed default reaches all of them.
- **Data touchpoints** — does it touch a schema, migration, cache key, serialized format, or persisted file? Data changes reach every producer and consumer, past and future, including data already written.

Then classify each change as **local** (blast radius contained within the changed file or module, few internal callers, no public/data surface) or **far-reaching** (many dependents, crosses the public/API/data boundary, or touches shared state). Far-reaching changes are the ones that carry the report. State the radius concretely — "17 call sites across 6 modules, 2 of them public endpoints" — not "widely used".

## Step 4 — Risk-factor checklist

**Run this checklist over every change; a change matching any factor is elevated even if the code looks correct, because the audit ranks exposure, not just defects.** Correctness is Step 5's and the reviewer's job. The audit's job is to say where a mistake would hurt most.

Score each change against these factors and tally which it hits:

- **Breaking API** — anything flagged in Step 2. Downstream fallout.
- **DB / schema migration** — new columns, dropped columns, type changes, index changes, backfills. Ordering and rollback matter.
- **Irreversible / destructive ops** — deletes, drops, truncates, overwrites, one-way migrations, sends (emails, payments, webhooks). Cannot be undone by revert.
- **Concurrency** — new threads, async, locks, shared mutable state, transactions, ordering assumptions. Bugs here are non-deterministic and escape tests.
- **Security-sensitive code** — auth, authz, crypto, secrets, input parsing, deserialization, file paths, SQL. Cross-reference the `loop-review` skill for a deep security pass; the audit only flags that this change lives in that zone.
- **Wide blast radius** — the far-reaching verdict from Step 3.
- **Low test coverage** — the changed behavior is thinly tested or untested (Step 5).
- **Large diff** — big enough that review fidelity drops and a subtle change can hide in the noise.

A change hitting several factors — a large diff that migrates a schema irreversibly and is thinly tested — is a top-of-report item. A `docs` change hitting none is a footnote. The output is a risk-ranked list, most-exposed first, each item naming the factors it hit and its blast radius. When an audit runs as a workflow, verify the high-risk flags adversarially: a skeptic per flagged change, prompted to argue the change is actually safe, defaulting to "risk stands" if it cannot (harness policy H4). This kills the "looks scary, is inert" false positives that erode trust in the ranking.

## Step 5 — Coverage assessment

**Map each changed behavior to the tests that exercise it; an untested change to a far-reaching or destructive symbol is the audit's highest-value finding.** Risk you cannot catch in CI is risk that ships.

For each changed behavior, ask which tests reach it:

- Find tests that import or drive the changed symbol — grep test files for the symbol and its callers.
- Distinguish "a test file was touched in this diff" from "the changed behavior is asserted". A diff that edits code and its tests together still needs the *new* behavior asserted, not just the old test kept passing.
- Flag changed behaviors with **no** exercising test, and weight that flag by the Step 3 radius and Step 4 factors — an untested one-line default flip that fans out to 200 call sites outranks an untested typo fix.
- Note coverage you cannot determine from reading (e.g. integration or manual coverage) as an explicit gap rather than assuming either way.

Coverage feeds back into the Step 4 ranking: "low test coverage" is only meaningful once you have actually looked for the tests.

## Worked mini-example

A one-file diff in a payments library:

```diff
--- a/billing/refund.py
+++ b/billing/refund.py
@@ def issue_refund(charge_id, amount, reason):
-def issue_refund(charge_id, amount, reason):
-    return gateway.refund(charge_id, amount)
+def issue_refund(charge_id, amount=None, reason=None):
+    if amount is None:
+        amount = gateway.get_charge(charge_id).total   # full refund by default
+    return gateway.refund(charge_id, amount)
```

- **Step 1 — change set**: one file, `billing/refund.py`, `git diff --stat` shows +4/-1. Small diff.
- **Step 2 — classify + breaking**: labelled by the author as a `feature` (optional-amount convenience). But the signature changed — `amount` went from required to optional with a new default behavior — so it is **breaking**. A caller that previously passed a partial `amount` is unaffected, but a caller relying on the old "amount required" contract, or any wrapper that introspects arity, is now inconsistent. Flag it.
- **Step 3 — blast radius**: grep for `issue_refund` → 11 call sites across 4 modules, plus one exposed as a `POST /refunds` handler. The default path (`amount is None`) issues a **full** refund. So this crosses the public/API boundary → **far-reaching**.
- **Step 4 — risk factors**: hits **breaking API** (signature + default), **irreversible/destructive op** (a refund sends money and cannot be undone by a code revert), **security-sensitive** (money movement), and **wide blast radius**. Four factors, one of them irreversible money movement → **top of the report**.
- **Step 5 — coverage**: grep tests for `issue_refund` → the existing test only asserts the two-arg partial-refund path. The new `amount is None` full-refund branch has **no test**. Untested + irreversible + public → the single highest-value finding: "a mistyped call now silently issues a full refund, exercised by no test."

The diff is four lines. The audit finding is a shippable, untested, irreversible full-refund path reachable from a public endpoint. Diff size told you nothing; the trace and the risk pass told you everything.

## Depth control

- **Small change set (a few files, no public/data surface)** — run Steps 1–5 inline in this session; do not spin up agents to audit a two-file diff.
- **Large or release-scale audit (big PR, many files, tag-to-tag span)** — run it as a workflow: fan out blast-radius tracing per changed symbol in parallel (barrier only where a later step truly needs all traces — harness policy H2), then an adversarial risk-verification pass (H4), then a ranked synthesis. Handle the empty change set and the untraceable symbol as explicit nulls, not crashes (H5). If new dependents keep surfacing as you trace, loop the trace until two consecutive rounds add no new touchpoints (loop-until-dry, loop policy L1) rather than stopping at a fixed count. Invoke the `loop-engine` skill to author and run it, following the skill's own `templates/change-audit.workflow.js` (a specialization of the `loop-engine` skill's `templates/parallel.workflow.js`).
