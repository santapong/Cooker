---
name: loop-audit
description: Audit a set of code changes and produce an impact and risk report. Use when the user asks to audit a diff, PR, release, or commit range, summarize what changed, assess the impact, blast radius, or risk of a change, check whether changes are covered by tests, or produce a change report or release risk memo. For finding security or quality defects to fix, use loop-review instead.
---

# Auditing Changes

You are about to audit a change set and produce a **report** — what changed, why, what it touches, how risky it is, and whether tests cover it. The engine is you reading the diff and tracing its reach, then writing a memo a release manager can act on. **This skill reports; it does not hunt for defects to fix.** Finding exploitable bugs is a different job with a different bar — when the audit needs a security or quality verdict, it delegates to the sibling `loop-review` skill and folds in that skill's confirmed findings rather than re-deriving them here. Keep the boundary sharp: an audit that quietly turns into a bug hunt produces a noisy, half-done review and a missing report.

## 1. Scope the change

**You cannot audit a change until you have pinned its exact boundaries — establish the change set first, from git, not from the description.**

- **Working tree**: `git diff` (unstaged) / `git diff --cached` (staged) / `git diff HEAD` for everything uncommitted.
- **Commit range / PR**: `git diff <base>...<head>` and `git log --oneline <base>..<head>` for the narrative. For a PR, resolve its base and head SHAs first.
- **Release**: a release is `git diff <prev-tag>..<new-tag>` with `git log <prev-tag>..<new-tag>`. Confirm the two tags before diffing; a wrong base silently inflates or hides the change set.
- Capture `--stat` for the shape (files, insertions, deletions) and use it to decide inline vs. workflow (§8). State the resolved scope back to the user before analyzing.

## 2. Classify each change

**Every changed unit gets exactly one primary class — the class drives the risk rating and what the report must say.**

Bucket each logical change (not each file) as one of:

- **feature** — new capability or surface.
- **fix** — corrects defective behavior; note the bug it closes.
- **refactor** — behavior-preserving restructure; the claim "no behavior change" is itself a thing to verify (§5).
- **breaking** — changes or removes a public contract (API signature, wire/schema format, config key, CLI flag, default). Under semver this forces a major bump; flag it loudly.
- **docs** — documentation/comments only.
- **chore** — build, deps, CI, formatting, tooling.

Mixed commits are common: split a commit that both fixes and refactors into two entries so each carries its own class and risk.

## 3. Impact & blast radius

**Impact is measured by what depends on the changed code, not by how many lines moved — a one-line change to a widely-called function outranks a 500-line change to a leaf.**

For each change, trace outward and record the reach: direct callers, transitive dependents, public API/exports, data and schema touch points, config and feature flags, and cross-service or wire contracts. The full tracing procedure — how to enumerate callers, follow exports, and spot schema/config/data-migration surface — is in **`references/methodology.md`**; open it before a non-trivial audit rather than eyeballing the diff.

## 4. Risk rating

**Rate each change on likelihood × impact, and never let a small diff mask a high-risk change — the rating comes from the blast radius (§3) and reversibility, not the line count.**

- **likelihood** — how plausibly this breaks: complexity, test coverage (§5), how well-understood the code is, whether it touches concurrency/state/IO.
- **impact** — from §3 blast radius: how much breaks and how visibly if it goes wrong.
- Escalate and call out explicitly: **breaking changes** (§2), **data or schema migrations**, and **irreversible operations** (destructive migrations, deletes, one-way backfills, external side effects). These carry a rollback note in the report — can this be reverted, and how.
- Give each change a rating (e.g. low / medium / high / critical) with a one-line justification, and roll up an overall risk verdict for the change set.

## 5. Test coverage of the change

**Coverage is judged against the changed lines and the changed behavior, not the repo's headline coverage percentage.**

- Identify tests that exercise the changed code: map changed functions/paths to tests that reach them. Are the new or modified behaviors actually asserted, or merely executed?
- Flag changed behavior with **no** covering test, and flag refactors claimed as behavior-preserving that have no test pinning the old behavior.
- Report the coverage gap as a finding in the audit. **Do not author the missing tests here** — writing tests is out of scope for a report; delegate that to the **`loop-test`** skill and reference it as the recommended follow-up.

## 6. Security dimension

**Do not re-implement review here. Invoke `loop-review` on the same diff and fold its confirmed findings into the report.**

When the audit needs a security/quality verdict, call the **`loop-review`** skill scoped to this change set. Take its findings that clear its own severity/confidence bar and summarize them in the report's risk section (severity, category, location) with a pointer to the full review — the audit report cites the review, it does not duplicate the analysis or lower the review's bar. If the user only wants the "what/impact/risk/coverage" memo and no defect hunt, skip this section and say so in the report.

## 7. Produce the report

**Write the audit using the fixed structure so every audit is comparable and nothing load-bearing is omitted.**

Assemble the report from **`references/report-template.md`**: summary and overall risk verdict up top, then per-change entries (class, impact/blast radius, risk rating + justification, coverage, rollback note), the folded-in `loop-review` findings, and an explicit open-questions/gaps list. Match length to the change: a two-file PR is a short memo; a release is a full risk memo. This is a report — do not open PRs, edit code, or write tests as part of it.

## 8. Orchestration: scale past a small change set

**A change small enough to hold in context, audit inline. For a large PR, a multi-package change, or a release spanning many areas, run it as a multi-agent workflow** using the template at **`templates/change-audit.workflow.js`**:

1. **Per-area analyze** — partition the change set by area/package and fan out one analyzer per area (parallel), each classifying its changes (§2) and returning structured entries.
2. **Impact assess in parallel** — trace blast radius and rate risk (§3–§4) per area, then a barrier merges the per-area results.
3. **Synthesize report** — one agent assembles the §7 report from the merged entries and the folded-in `loop-review` findings.

This is the parallel-analyze → barrier → synthesis pattern from the **`loop-engine`** skill (see its `templates/parallel.workflow.js` and harness policy H2's earned barrier and H4 adversarial verify). Invoke the `loop-engine` skill to author and execute the run; the change-audit template pre-wires the classification, blast-radius, and risk schema. For a small diff, skip the workflow and audit directly — do not spin up agents for a two-file change.

## Reference files

| File | What it holds |
|---|---|
| `references/methodology.md` | Blast-radius tracing: callers, transitive dependents, public API, schema/config/data surface |
| `references/report-template.md` | The fixed audit report structure — summary, per-change entries, risk verdict, gaps |
| `references/standards.md` | Authoritative standards this skill applies — ISO 31000, DORA, SemVer impact, ITIL — named, pinned, and mapped |
| `templates/change-audit.workflow.js` | Per-area analyze → parallel impact assess → synthesize-report workflow script |
