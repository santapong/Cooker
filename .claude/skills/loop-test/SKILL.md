---
name: loop-test
description: "Write and repair automated tests: design cases, match the project test framework, and verify tests run and fail for the right reason. Use when the user asks to write, add, generate, or fix tests, improve coverage, add a regression test for a bug, or set up testing for a module. Matches the repo existing test stack and conventions."
---

# Writing Tests

You are about to author or repair automated tests. The engine is you understanding what the code is *supposed to do* and pinning that behavior down — not padding a coverage number. A test that passes without exercising real behavior, or that breaks the moment an implementation detail changes, is worse than no test: it manufactures false confidence. **A test that never actually ran, or that passes for the wrong reason, is the failure mode this skill exists to prevent** — every test you add is executed and shown to pass for real, and every regression test is shown to fail on the unfixed code first.

## 1. Understand the unit under test

**You cannot test behavior you have not characterized — read the code and its contract before writing a line of test.** Pin down, for the unit in scope:

- **Behavior**: what it computes or decides, in plain terms.
- **Inputs**: parameters, their types, valid ranges, and what "invalid" means.
- **Outputs**: return values and their shape across the input space.
- **Side effects**: I/O, mutation, network, DB, logging, events — what crosses a boundary.
- **Contracts**: pre/postconditions, invariants, documented errors, and edge behavior the callers rely on.

Test **behavior, not implementation**. Assert on observable outputs and effects, not on private helpers, call order, or internal state. If a refactor that preserves behavior would break your test, the test is coupled to the implementation — rewrite it against the contract.

## 2. Detect the project test stack and conventions

**Match what exists — never introduce a new test framework, runner, or assertion library into a repo that already has one.** Before writing, detect the stack and mirror it: framework, runner and invocation command, test-file naming and location, fixture/factory patterns, assertion style, and setup/teardown idioms. The detection procedure and per-ecosystem signals are in **`references/framework-conventions.md`** — read it to identify the stack and copy its conventions exactly. When a repo has no test setup at all and the user is asking you to establish one, that reference also covers picking an ecosystem-default runner.

## 3. Design the cases

**Enumerate cases across categories before writing — an untested edge or error path is a gap, not a detail.** For the unit in scope, design cases spanning:

- **Happy path** — representative valid inputs producing expected outputs.
- **Edge / boundary** — empty, zero, one, max, off-by-one, null/None, unicode, ordering, duplicates.
- **Error / exception** — invalid inputs and failure modes; assert the *specific* error type and message, not just "it throws".
- **Property-based** (where valuable) — invariants that hold across generated inputs (round-trips, idempotence, ordering, conservation). Use the repo's property library if one exists.

Case-design heuristics, the boundary checklist, and when property-based earns its keep are in **`references/test-design.md`**.

## 4. Write the tests to repo conventions

**One behavior per test, arranged so a failure names its own cause.** Follow:

- **AAA** — Arrange (set up inputs and world), Act (invoke the unit once), Assert (check outputs and effects). Keep the three visually distinct.
- **Deterministic** — no reliance on wall-clock, random seeds, network, or ordering of other tests. Inject or freeze time and randomness.
- **Fast & isolated** — no shared mutable state between tests; each sets up and tears down its own world.
- **Minimal mocking** — mock only at **trust boundaries** (network, clock, filesystem, third-party services), never the unit under test or your own pure logic. Over-mocking tests the mocks, not the code.
- **Named to describe behavior** — the test name states the expected behavior, so a failure in CI reads as a sentence.

## 5. Verify — run them, and prove they fail for the right reason

**A test you did not run is not done; a regression test that never went red does not guard anything.** For every test you add or repair:

1. **Run it** using the repo's own command (from §2). A new test must actually execute — a skipped, filtered-out, or uncollected test is a silent no-op.
2. **Pass for real** — confirm green, then sanity-check it isn't vacuous (an assertion that can't fail, e.g. `assert True`, is a bug).
3. **For a regression test: show it goes red on the unfixed code first.** Add the failing test before (or with the fix reverted), watch it fail with the expected error, then apply/restore the fix and watch it pass. A regression test that was never red proves nothing.

Pair this with the Claude Code built-in **verify** skill to exercise the change end-to-end, not just at the unit boundary.

## 6. Coverage: target behaviors, not a percentage

**Chase uncovered behaviors and risk, not a coverage number.** A coverage report is a map of *untested behavior* — read it to find branches, error paths, and edge cases nobody exercises, and write cases for the ones that carry real risk. Do not add tests solely to move a percentage, and do not assert on a coverage threshold as if it were a behavior. 100% line coverage with no boundary or error cases is a weaker suite than 70% that pins the contract.

## 7. Orchestration: generate tests for many modules at once

A handful of modules you can test inline in this session. For **a whole package or a coverage sweep across many modules, run it as a multi-agent workflow** using the template at **`templates/test-generation.workflow.js`**:

1. **Design fan-out** — one agent per module characterizes the unit and designs its cases (§1, §3), returning a structured case list.
2. **Write** — per module, one agent writes the tests to repo conventions (§2, §4) from that case list.
3. **Run (optional)** — per module, execute the new tests and report pass/fail, so a broken or vacuous file surfaces instead of shipping green-by-accident.

This is the **pipeline** pattern (design → write → run per module, no barrier between modules) from the **`loop-engine`** skill (see its `templates/pipeline.workflow.js` and harness policy H1 pipeline-default, H4 adversarial verify). Invoke the `loop-engine` skill to author and execute the run; the test-generation template pre-wires the case schema, the convention-matching step, and the run-and-report gate. For a module or two you can hold in context, skip the workflow and write the tests directly.

## Reference files

| File | Read it when |
|---|---|
| `references/framework-conventions.md` | Detecting the repo's stack, runner, naming, fixtures, and assertion style — or choosing a default when none exists |
| `references/test-design.md` | Designing cases: happy/edge/error/property categories, the boundary checklist, minimal-mocking rules |
| `references/standards.md` | Authoritative standards this skill applies — xUnit Patterns, Pyramid/Trophy, FIRST, mutation testing, Pact, MC/DC — named, pinned, and mapped |
| `templates/test-generation.workflow.js` | Generating tests across many modules: design → write → run pipeline workflow script |
