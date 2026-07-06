---
name: loop-debug
description: Diagnose and fix bugs with a reproduce to localize to root-cause to fix method. Use when the user reports a bug, a failing test, an exception or stack trace, unexpected behavior, a crash, or asks why something is broken or how to fix it. Drives hypothesis-driven debugging and adds a regression test once fixed.
---

# Diagnosing Bugs

You are about to debug by the scientific method: reproduce the failure, read the evidence, localize the fault, form falsifiable hypotheses, and test each until one survives. The engine is disciplined elimination, not pattern-matching a fix onto a symptom. **A bug you cannot reproduce you cannot verify fixed** — so a green run against a case you never saw fail is not a fix, and a change that "should" help without a confirmed root cause is a guess. Every fix here is anchored to a reproduction that failed before it and passes after.

## 1. Reproduce reliably first

Before touching code, make the failure happen on demand. Capture the **exact trigger, inputs, environment, and expected-vs-actual** — the precise command or call, the input values, versions and config, and what should have happened versus what did. **A bug you cannot reproduce you cannot verify fixed**, so invest here first: shrink the trigger to the smallest reliable repro, and note flakiness (timing, order, concurrency, external state) as itself a clue to the fault class. If you genuinely cannot reproduce it, say so and gather more evidence rather than guessing at a fix.

## 2. Read the evidence

Read the error message, stack trace, and logs literally, and **start where the failure surfaces, then work backward** to where it originated. The top frame is where it blew up, not always where it went wrong — walk the stack toward the first frame you own. Read the message as written (the wrong assumption is usually that it says something other than it does), check the log lines immediately before the failure for the last known-good state, and distinguish the primary error from downstream noise it triggered.

## 3. Localize: narrow the fault region

Shrink the search space before theorizing about cause. **Bisect, do not scan** — halve the suspect region each step instead of reading top to bottom. `git bisect` pins a regression to the commit that introduced it; binary-searching the code path (does the value hold at the midpoint?) pins where correct becomes wrong; targeted instrumentation makes an invisible intermediate state observable. Full techniques — bisect workflow, binary search, instrumentation, and delta-debugging the input — are in **`references/methodology.md`**; open it before a non-trivial localization.

## 4. Hypothesize and test to eliminate

From the localized region, generate **concrete, falsifiable hypotheses** for the root cause — each a specific claim you can prove wrong with a cheap check. **Test to ELIMINATE, not to confirm**: a check that would pass whether or not the hypothesis is true tells you nothing; design each test so one outcome kills the hypothesis. Rank by likelihood × cheapness and run the cheapest discriminating test first. Enumerating hypotheses, designing eliminating tests, and reading a surprising result (it means a hidden assumption is wrong — chase it) are in **`references/hypothesis-testing.md`**.

## 5. Root cause, not symptom

Fix the cause, not the surface. **State the causal chain out loud** — trigger → the flawed logic → the observed failure — and confirm each link before you accept it; if you cannot narrate why the bug happens, you have not found the root cause and a fix now is a patch over a symptom. Guard the value at the point it goes wrong, not at the point it finally crashes. Beware fixes that merely move or silence the symptom (a swallowed exception, a null check at the blast site) while the cause still fires.

## 6. Minimal fix plus regression test

Make the **smallest correct change** that addresses the root cause — resist refactoring adjacent code in the same edit; a tight diff is easier to verify and to revert. Then lock it in with a regression test that **FAILS without the fix and passes with it**: write or run the test against the unfixed code first to watch it fail for the right reason, then apply the fix and watch it pass. Delegate authoring the test to the **`loop-test`** skill so it matches the project's framework and conventions — a test that never failed proves nothing.

## 7. Verify end to end

Confirm the fix in the real flow, not just the unit test. Invoke the built-in **`verify`** skill to drive the affected path end to end and observe the corrected behavior on the original reproduction from §1. Then **check the neighbors**: re-run the surrounding test suite and exercise adjacent code paths that share the changed logic, so the fix did not trade one bug for another. A fix that passes its own test but breaks a sibling is not done.

## 8. Orchestration: many candidate causes

A bug with an obvious cause you debug inline in this session. For a **hard bug with many plausible, independent causes** — where hypotheses do not share a cheap discriminating test and testing them serially is slow — run them in parallel with **`templates/bug-diagnosis.workflow.js`**:

1. **Hypothesis fan-out** — one agent per candidate root cause (parallel), each returning a structured verdict: does the evidence confirm or ELIMINATE this cause, with the test it ran.
2. **Converge** — merge the verdicts in plain script logic; the surviving hypotheses are the real suspects, the eliminated ones are logged (no silent drops).
3. **Fix + verify** — apply the minimal fix for the surviving cause, then the §6–§7 regression-test and end-to-end verify.

This is the parallel fan-out → converge pattern from the **`loop-engine`** skill (see its `templates/parallel.workflow.js` and harness policy H2 earned barrier, H4 adversarial/diverse-lens verify, H5 null handling). Each hypothesis agent is prompted to *eliminate* its assigned cause and default to "not the cause" when the evidence is inconclusive — the debugging analogue of the adversarial verifier. Invoke the `loop-engine` skill to author and execute the run. For a bug with one or two candidate causes, skip the workflow and debug directly — do not fan out agents for a one-line off-by-one.

## Reference files

- `references/methodology.md` — localization in full: `git bisect`, binary-searching the code path, instrumentation, and delta-debugging the input
- `references/hypothesis-testing.md` — enumerating falsifiable hypotheses, designing eliminating tests, and reading surprising results
- `references/standards.md` — the authoritative standards this skill applies — named, version-pinned, and mapped to its workflow
- `templates/bug-diagnosis.workflow.js` — parallel hypotheses → eliminate/converge → fix workflow script
