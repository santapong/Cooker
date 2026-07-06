---
name: cooker-fix-bug
description: Routine for fixing a bug in Cooker. Trigger on "fix this bug", "this is broken", a stack trace paste, a failing-test paste, an issue URL, or a one-line description like "/runs/:id returns 500 when …". Bias toward identifying the root cause via existing audit docs before reading source — most bugs in this project are already catalogued.
---

# Cooker — fix-a-bug routine

> **Methodology lives in `/loop-debug`** (reproduce → localize → root-cause → fix → regression test). This skill is the Cooker overlay on it: triage against the catalogued audit corpus in `docs/audits/` before reading source, and maintain the audit ledger when you close a chain. For a bug in another repo, use `/loop-debug` directly.

## Inputs the user provides

One of:
- A bug description in prose ("X returns 500 when Y").
- A stack trace or log line.
- A failing-test paste.
- A GitHub issue URL or number.

## Steps

### 1. Pin down the bug

- Get a one-sentence statement of *what's wrong* and *what should happen*.
- Find the smallest reproduction. If the user gave a stack trace, the top frame is your starting point. If they gave a log line, grep the codebase for the exact format string.
- If you can't pin it down from the input, ask **one** clarifying question and stop. Don't guess.

### 2. Check whether it's already known

Read first, code second. Run, in order:

```bash
# Is this already in chain-recheck?
rg -n -i "<keyword from the symptom>" docs/audits/chain-recheck.md docs/audits/launch-readiness.md

# Is it on the post-launch roadmap?
rg -n -i "<keyword>" docs/audits/launch-readiness.md
```

If the bug matches a chain ID like `B.X.Y` or a finding ID like `[A<n>-<m>]`, **point at the existing reference** and ask the user whether to promote it out of the roadmap into this fix. Don't re-discover.

### 3. Locate the code

Use the **cooker-find** skill or its `where-is.sh` script. Don't compose ad-hoc grep first — the curated map is cheaper.

```bash
.claude/skills/cooker-find/where-is.sh <noun-from-symptom>
```

If the symptom is unknown-target (audit-style), use **cooker-audit**'s `audit-greps.sh` for candidate patterns instead.

### 4. Form a hypothesis with file:line evidence

State it out loud, in this shape:

> "The bug is at `<file>:<line>` because `<code construct>` does `<X>`, but `<expected>`. Confirmed by `<test or log line or trace frame>`."

If you can't fill that template, you don't know enough yet. Read more.

### 5. Write a failing test if possible

- For a logic bug: a unit test that fails on `main`.
- For a race: `go test -race -count=10 ./internal/<pkg>/...`.
- For a handler bug: a test in `internal/handler/*_test.go` using `newTestHandler` (see `idor_test.go` for the pattern).

A failing test is the receipt for the diagnosis. Skip only if the bug is in deploy / docs / config.

### 6. Fix using the cooker-improve workflow

- One focused commit. No opportunistic cleanup.
- Wrap errors with the package prefix (`fmt.Errorf("<pkg>: <op>: %w", err)`).
- If the fix touches a layer (handler / service / store), respect the layering rules in `cooker-improve`'s SKILL.md.
- Run the scoped check before committing:
  ```bash
  .claude/skills/cooker-improve/check-pkg.sh internal/<changed-pkg>
  ```

### 7. Cross-reference the audit docs

If the bug **was** in an audit doc:
- Update the doc's row from "Open" → "Closed by <commit-SHA>" (e.g. in `chain-recheck.md`).
- Reference the audit ID in the commit message.

If the bug was **not** in any audit doc:
- Add it to `chain-recheck.md` under the "New chains introduced by remediation" section if it's chain-shaped.
- Otherwise, just cite "Closes #<issue>" if there's an issue, or describe the root cause in the commit body.

### 8. Branch + draft PR

- Branch: `fix/<area>-<topic>` (e.g. `fix/handler-idor-runid`).
- Commit message format:
  ```
  fix(<pkg>): <short title>

  <root-cause sentence + how the fix addresses it; ~5-15 lines>

  Closes <issue or audit-id>.
  ```
- Open as a **draft PR**. CI gates merge.

## Anti-patterns to refuse

- **"Quick fix" without root cause.** A change that makes the symptom go away but doesn't explain why is a regression magnet. Always state the root cause in the commit message.
- **Scope creep.** Don't fix three nearby smells in the same commit. File issues for them.
- **Adding a feature flag to "make it switchable."** If the old behaviour was wrong, just fix it.
- **Skipping the existing-audit check.** Half of the bugs you'll see are in `chain-recheck.md`.
- **Editing the test until it passes.** Write the test, then write the fix.
- **Reverting your own audit-doc closure when CI fails.** If you closed a chain in `chain-recheck.md` and CI breaks, fix CI; don't reopen the chain.

## When to stop and ask the user

Stop when:
- You can't pin down the symptom from the input (ask one clarifying question).
- The fix would change a public API (handler signature, env var name, schema column).
- The bug is in `chain-recheck.md` listed as roadmap (R<n>) — the user may not want to absorb that scope here.
- The fix would touch authentication, secrets, or the Dockerfile (`SECURITY.md` says these need a separate review).

## Output expected at the end

- One focused commit (or 2-3 if naturally separable: test, fix, doc).
- A draft PR with the diagnosis in the body.
- Updated `chain-recheck.md` row if applicable.
- Backend tests green: `go test -race ./internal/<pkg>/...`.

## Checklist before declaring done

- [ ] Root cause stated in the commit message
- [ ] Failing test written first; passes after fix
- [ ] `check-pkg.sh internal/<changed-pkg>` clean (gofmt + vet + race)
- [ ] Audit-doc row updated if applicable
- [ ] No scope-creep changes in the diff
- [ ] PR is draft; CI green before requesting review
