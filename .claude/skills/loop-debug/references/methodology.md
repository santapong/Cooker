# Debugging Methodology

The full procedure behind the `loop-debug` skill: turn a symptom into a confirmed root cause through a disciplined loop — reproduce, observe, localize, scan the bug classes, and confirm by toggling. The engine is not a hunch; it is a chain of evidence where each step narrows the search space and the last step *proves* causation. **A fix shipped without a confirmed root cause is a guess, and this skill exists to replace guesses with a cause you can turn on and off at will.** When the loop runs at scale (many hypotheses, a wide bisect, parallel instrumentation), its orchestration is governed by the `loop-engine` skill's harness and loop policies.

## Step 1 — Reproduce

You cannot debug what you cannot trigger. Before touching the code, get the bug to happen on demand.

- **Minimize the repro.** Strip the failing scenario to the smallest input, shortest path, and fewest steps that still fail. Every element you remove without the bug disappearing is a variable eliminated. A one-line repro localizes faster than a full end-to-end scenario.
- **Make it deterministic.** An intermittent bug is an uncontrolled variable, not a mystery. Pin the seed, freeze the clock, serialize the concurrency, fix the input ordering — whatever makes failure happen every run, not one in ten. If it only fails 1-in-N, that flakiness *is* a clue (points at async/race/ordering — see Step 4).
- **Pin the environment.** Record and lock the versions, config, env vars, feature flags, data fixtures, and OS/runtime that produce the failure. "Works on my machine" is almost always env drift; capture the env now so you can compare against a working one later.

**The repro is the ground truth for the whole loop — if it isn't deterministic, every later step is measuring noise.**

## Step 2 — Observe

Read what the failure is already telling you before you form a theory.

- **Read stack traces bottom-up to the first frame in your code.** The top frame is often deep in a library where the error surfaced, not where it originated. Walk down (or up, depending on your runtime's ordering) to the first frame you own — that is where your code handed bad data or made a bad call. Note the exact exception type and message; "undefined is not a function" and "connection refused" send you to completely different bug classes.
- **Diff the logs of a good run vs a bad run.** Capture output from a passing case and the failing case, then diff them. The point where they diverge is the point where behavior went wrong — that line number is your first localization anchor. If logging is too sparse to show the divergence, that gap is what Step 3 instrumentation fills.

**Let the evidence pick the hypothesis; don't pick a hypothesis and hunt for evidence — confirmation bias is the most expensive bug in debugging.**

## Step 3 — Localize

Narrow "somewhere in the system" down to a specific line or state transition. Pick the technique that fits what you know.

### git bisect — for regressions ("it used to work")

When the bug is new and you have a known-good and known-bad commit, binary-search the history:

```
git bisect start
git bisect bad                 # current commit is broken
git bisect good <old-sha>      # this commit worked
# git checks out the midpoint; test it, then mark:
git bisect good                # or: git bisect bad
# repeat until git prints "<sha> is the first bad commit"
git bisect reset               # restore your original HEAD
```

Automate it when the repro is scriptable — `git bisect run ./repro-test.sh` (exit 0 = good, non-zero = bad) drives the whole search unattended and lands on the exact culprit commit in `log₂(n)` steps. The diff of that commit is your suspect.

### Binary search by disabling code paths

No clean commit boundary? Bisect the *code* instead of the history. Short-circuit half the suspect region — early-return, comment out, feature-flag off, stub the dependency — and re-run the repro. Bug gone → it lived in the half you disabled; bug remains → it's in the other half. Halve again. Each round doubles your resolution.

### Instrumentation — logging & breakpoints

Make the invisible state visible at the boundaries Step 2 flagged. Log or breakpoint the inputs and outputs of the suspect function: assert what you *believe* is true at that point and watch where reality diverges from belief. Log values, not just "reached here" — a wrong value is the finding; a reached line is only a location. Prefer a conditional breakpoint (`when x == null`) over stepping through thousands of iterations.

### Rubber-duck the data flow

Trace the bad value backward by explaining, out loud or in writing, exactly how the data reaches the failure point: where it's created, every transform it passes through, and what each step assumes about it. The step whose assumption the data violates is the bug. This is often faster than any tool because it forces you to state assumptions you'd otherwise skip past.

**Every localization technique is binary search in disguise — each move should roughly halve the space you still have to search, or you're poking, not localizing.**

## Step 4 — Scan the common bug classes

With the region localized, pattern-match against the classes that produce most bugs. Scan the suspect code against this checklist rather than working from memory — the class points you at the specific line and the confirm test:

- **Off-by-one** — `<` vs `<=`, loop bounds, slice/substring indices, fencepost counts, inclusive vs exclusive ranges.
- **Null / undefined** — unguarded access on a value that can be absent; missing return producing `undefined`; optional field assumed present.
- **Type coercion** — `"1" + 1`, truthy/falsy surprises (`0`, `""`, `[]`), loose equality, implicit string↔number conversion, JSON round-trip changing types.
- **Async / race / ordering** — unawaited promise, callback fired out of order, two writers to one resource, assuming sequential execution of concurrent work. (Suspect this first if Step 1 couldn't make the repro deterministic.)
- **State mutation / shared state** — a shared object mutated in place, aliasing, a cached reference changed under a reader, a default argument reused across calls.
- **Boundary / empty input** — empty list/string/map, zero, negative, single-element, max size, the first or last iteration, unicode/whitespace.
- **Error swallowing** — an empty `catch`, a discarded error return, a `finally` that overrides the throw — the real failure hidden upstream of the symptom you see.
- **Config / env drift** — a value that differs between the working env and the broken one: a flag, a path, a locale, a timezone, a credential, a default. (Compare against the env you pinned in Step 1.)
- **Dependency version** — a transitive upgrade, a lockfile change, a breaking minor, an API that changed behavior between versions.
- **Caching / staleness** — a stale read, an unindexed invalidation, a memoized value that outlived its inputs, a CDN/build artifact serving the old version.

**Match the symptom to a class to get a testable hypothesis — the checklist's job is to convert "something's wrong here" into "I bet it's X, and here's how I'll prove it."**

## Step 5 — Confirm the root cause

A localized line is a *suspect*, not a cause. You have the root cause only when **you can turn the bug on and off by toggling the suspected cause and nothing else.**

- Apply the minimal change that would fix the suspected cause → the repro passes. Revert it → the repro fails again. Re-apply → passes. That on/off/on control is proof of causation; anything less is correlation.
- If toggling the suspect does *not* cleanly switch the bug, you have the wrong cause (or only one of several) — return to Step 3 with what you learned. Do not "fix" a correlated symptom.
- Beware the fix that masks: silencing the error or special-casing the failing input can make the repro pass without addressing the cause. The test is whether the *mechanism* you identified explains every observation from Step 2, not just whether the symptom disappeared.
- Once confirmed, write a regression test that fails on the old code and passes on the fix — it locks the toggle in place so the bug can't silently return.

**"The symptom went away" is not confirmation; "I can make it come back and go away at will" is.**

## Worked example

**Symptom.** A report-export endpoint that worked last week now returns an empty CSV for some users, intermittently. No error in the response; a 200 with a header row and no data rows.

**Step 1 — Reproduce.** The intermittency is the first clue. Trying user IDs, it fails deterministically for users whose account has *zero* orders in the selected range and passes for users with orders — the "intermittent" was just which user happened to be tested. Minimal repro: `GET /export?user=<no-orders-user>&from=2026-06-01`. Env pinned to current `main`.

**Step 2 — Observe.** No stack trace (it's a silent-empty, not a crash). Diffing logs of a good user vs the empty one: both log `fetched N orders`, but the empty user logs `fetched 0 orders` then `wrote 0 rows` — expected — yet last week the same zero-order user got a valid (header-only) file that downstream tooling accepted. So the behavior *changed*; this is a regression.

**Step 3 — Localize (bisect).** Known-good = last week's release tag, known-bad = `main`. `git bisect start; git bisect bad; git bisect good release-2026-06-24`, then `git bisect run ./repro-export.sh`. It lands on a single commit: a refactor of the CSV writer that "cleaned up" the header logic.

**Step 4 — Bug class.** The culprit diff moved the header-write inside the `for row in rows:` loop. Bug class: **boundary / empty input** — with zero rows the loop body never executes, so the header is never written, so the tooling that expected at least a header row rejects the file.

**Step 5 — Confirm.** Hoist the header-write back above the loop → repro returns a header-only CSV, tooling accepts it. Revert the hoist → empty file again. Re-apply → fixed. The toggle switches the bug cleanly: root cause confirmed. Lock it with a regression test asserting a zero-order export still contains the header row.

## Depth control

- **Shallow bug** — obvious symptom, small blast radius, one plausible cause: run the loop inline, often collapsing Steps 3–5 into a single instrument-and-toggle. Don't spin up a workflow for a typo.
- **Deep / wide bug** — many plausible causes, a large history to bisect, or reproduction that itself takes real effort: run hypotheses as a `loop-engine`. Fan out one investigator per candidate cause (parallel, harness policy H2's earned barrier before you converge), have each *try to disprove* its own hypothesis (adversarial verify, harness policy H4 — a suspect no investigator can toggle survives as the cause), and treat "no hypothesis confirmed" as a real, reportable outcome rather than forcing a fix (H5 nulls). If new evidence keeps reshaping the hypothesis set, loop the observe→localize cycle until two consecutive rounds add no new suspect (loop-until-dry, loop policy L1) rather than stopping at a fixed number of tries. See the sibling `loop-engine` skill and its `templates/parallel.workflow.js`.
