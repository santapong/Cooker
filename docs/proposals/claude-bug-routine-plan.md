# Claude bug-fixing routine — plan

**Status:** plan only. Nothing in this doc is enabled until a
maintainer creates the skill / agent / workflow files described
below.

**Goal.** A coordinated Claude routine for Cooker that:

1. **Reactive** — takes a GitHub issue, plans a fix, opens a draft PR
2. **Proactive** — on a schedule, hunts for bugs across multiple
   risk categories and opens draft PRs for the cheap ones
3. **CI-triggered** — when CI fails, diagnoses and proposes a fix
4. **PR review** — when a human opens a PR, runs an automated review

The routine is layered: skills (manual triggers) compose with
subagents (focused workers) compose with GitHub Actions workflows
(automated triggers). Each layer is independently useful.

---

## What's already in place

Cooker's `.claude/` skill registry already covers six of the seven
modes:

| Mode | Skill | Notes |
|---|---|---|
| Fix one known bug | `/cooker-fix-bug` | Routine for fixing a bug; biased toward existing audit docs |
| Find bugs (broad) | `/cooker-audit` | Find bugs, SPOFs, vulnerabilities, chain errors |
| Refactor / known-issue fix | `/cooker-improve` | Quality / refactor / known-issue workflow |
| Triage failing CI | `/cooker-ci-debug` | Per-CI-failure routine |
| Weekly bug hunt | `/cooker-weekly` | Scheduled: read issues + commits, pick one bug, draft a PR |
| Find code | `/cooker-find` | Fast file navigation |
| New feature | `/cooker-new-feature` | New user-facing feature routine |

What's missing: **(1)** an issue-driven workflow that takes a GitHub
issue number as input, **(2)** a coordinated cross-scenario audit
that runs concurrency + security + perf + error-handling in one
pass.

---

## Architecture

```
                       ┌──────────────────────────────────┐
                       │   GITHUB EVENTS                  │
                       │   - issue labelled "claude-fix"  │
                       │   - cron @ Monday 1pm UTC        │
                       │   - PR opened by human           │
                       │   - workflow_dispatch (manual)   │
                       └───────────┬────────────────────┘
                                   │
          ┌───────────────────────────────────────────┐
          ▼                              ▼                       ▼
  ┌──────────────────┐    ┌──────────────────┐    ┌──────────────────┐
  │  GH ACTIONS     │    │  GH ACTIONS    │    │  GH ACTIONS    │
  │ issue-autofix   │    │ cooker-weekly  │    │  pr-review     │
  │ .yml            │    │ .yml (extend)  │    │  .yml          │
  └──────┬─────────┘    └───────┬────────┘    └───────┬────────┘
         │                     │                     │
         ▼                     ▼                     ▼
  ┌──────────────────┐    ┌──────────────────┐    ┌──────────────────┐
  │  /issue-fix     │    │ /cooker-weekly │    │ /pr-review     │
  │  (new SKILL)    │    │ + /xscenario   │    │ (new SKILL)    │
  └──────┬─────────┘    └───────┬────────┘    └───────┬────────┘
         │                     │                     │
         ▼                     ▼                     ▼
  ┌──────────────────┐    ┌──────────────────┐    ┌──────────────────┐
  │ cooker-issue-   │    │ cooker-bug-    │    │ cooker-review  │
  │ fixer           │    │ hunter         │    │ (existing or  │
  │ (new SUBAGENT)  │    │ (new SUBAGENT) │    │  new SUBAGENT) │
  └──────────────────┘    └──────────────────┘    └──────────────────┘
```

### Why three layers?

- **Workflows** trigger automatically on events you don't control
  (issue labels, cron, PR open).
- **Skills** trigger manually with a slash command. They wrap a
  subagent invocation so a future change to the workflow / agent
  only needs to be made in one place.
- **Subagents** are focused worker definitions. They have a single
  responsibility (e.g., "take an issue → produce a patch") and a
  narrow toolset (just `Read`, `Bash`, `Grep` — not `Edit` for the
  hunter, since the hunter only reports).

---

## What to add

### Layer 1 — Skills (manual triggers)

#### `/issue-fix <number>`

Fetch a GitHub issue by number, plan a fix, implement on a fresh
branch, open a draft PR linked back to the issue.

**File:** `.claude/skills/issue-fix.md`

```markdown
---
name: issue-fix
description: Take a GitHub issue number, fetch it (title + body + 
  comments + labels + author), plan a fix, implement it on a fresh 
  branch named claude/fix-issue-<number>, open a draft PR linked back 
  to the issue. Use when given an issue number or issue URL.
---

Spawn the cooker-issue-fixer subagent with the issue context loaded 
from `mcp__github__issue_read`. The agent:

1. Reads the issue body + all comments to understand the problem.
2. Uses `/cooker-find` to locate the affected code.
3. Reproduces the bug locally if a stack trace or repro steps are 
   provided.
4. Plans the fix as a 5-bullet summary BEFORE writing code.
5. Implements the fix on branch claude/fix-issue-<number>.
6. Runs `go test ./...` to confirm no regression.
7. Opens a DRAFT PR titled "fix: <issue title> (#<number>)" with body 
   that links to the issue and explains the fix.

Never auto-merge. Always draft.
```

#### `/cross-scenario-audit` (or `/xscenario`)

Coordinated multi-pass audit across five risk categories. Reports
findings; does not fix without explicit follow-up.

**File:** `.claude/skills/cross-scenario-audit.md`

```markdown
---
name: cross-scenario-audit
description: Run a coordinated audit across five risk categories 
  (concurrency, security, error handling, performance, edge cases). 
  Reports findings as P0 / P1 / P2 with file:line citations.
---

Spawn the cooker-bug-hunter subagent five times in parallel, once 
per risk category:

1. concurrency — race conditions, deadlocks, goroutine leaks, 
   missing context cancellation
2. security — HMAC verification, SQL injection, secret handling, 
   timing leaks, auth bypass
3. error-handling — silent error swallows, missing %w wrap, 
   unchecked sql.Rows.Err, unwrapped panics
4. performance — n+1 queries, unbounded reads, missing partial 
   indexes, inefficient JSON marshalling in hot paths
5. edge-cases — nil pointer derefs on optional fields, empty slice 
   misuse, integer overflow on multi-arch builds

After all five return, consolidate findings into a single P0 / P1 / 
P2 priority list. Ask the user which findings to file as issues or 
fix immediately.
```

### Layer 2 — Subagents (focused workers)

#### `cooker-issue-fixer`

**File:** `.claude/agents/cooker-issue-fixer.md`

```markdown
---
name: cooker-issue-fixer
description: Takes a single GitHub issue, plans a fix, implements 
  on a fresh branch, opens a draft PR. Reads the issue body + 
  comments. Uses cooker-find to locate code. Reports each step.
tools: Bash, Read, Edit, Write, Glob, Grep
---

You are a focused bug-fixer for the Cooker project. You take exactly 
one GitHub issue at a time and produce exactly one draft PR.

Process:

1. **Understand.** Read the issue body + every comment. Identify:
   - The user's reported behaviour
   - The expected behaviour
   - Any repro steps, stack traces, or affected version
   - Any prior triage from maintainers in comments

2. **Locate.** Use the cooker-find skill (or grep) to find the 
   relevant files. If the issue references a file or symbol, jump 
   there directly.

3. **Reproduce.** If repro steps are provided, run them. Capture 
   the failure exactly so the fix can be validated. If no repro is 
   possible from the issue alone, write a failing test that 
   demonstrates the bug before fixing.

4. **Plan.** Before any Edit / Write, output a 5-bullet plan:
   - Root cause
   - Files that change
   - Tests that change
   - Risks of the fix
   - How you'll verify it works

5. **Implement.** On branch claude/fix-issue-<number>. Keep the diff 
   small. If the fix requires more than 200 lines or touches more 
   than 3 files, STOP and escalate to the user.

6. **Verify.** Run `go test ./...` (and `go test -race` for 
   concurrency fixes). Run `make test` if it exists.

7. **Open PR.** Draft only. Title: "fix: <one-line summary> (#<num>)".
   Body must include:
   - "Closes #<number>"
   - The root cause from your plan
   - The verification steps you ran
   - Anything the human reviewer should double-check

Never push to main. Never force-push. Never auto-merge. Always draft.

Guardrails:
- If the issue is a feature request, not a bug, abort and tell the 
  user to use /cooker-new-feature instead.
- If the issue is a question, abort and tell the user to answer in 
  the issue comments.
- If the fix requires changing public API, abort and ask for sign-off.
```

#### `cooker-bug-hunter`

**File:** `.claude/agents/cooker-bug-hunter.md`

```markdown
---
name: cooker-bug-hunter
description: Scans the codebase for bugs in a single risk category 
  (concurrency / security / error-handling / performance / 
  edge-cases) per invocation. REPORTS findings; does not fix.
tools: Bash, Read, Glob, Grep
---

You audit Cooker's Go code for bugs in ONE risk category, named in 
the invocation prompt. You only have read tools — you cannot edit 
files. Your output is a report.

Category-specific guidance:

  concurrency
    - sync.Mutex held across function boundaries?
    - context.Context propagated to every goroutine?
    - time.After in select without Stop() (timer leak)?
    - goroutines spawned without WaitGroup tracking?
    - shared map / slice without sync.Mutex / sync.RWMutex?
    - channel send on closed channel risk?
    - race-detector output: `go test -race ./...`

  security
    - HMAC compared with `==` instead of hmac.Equal / 
      subtle.ConstantTimeCompare?
    - SQL built with fmt.Sprintf instead of $1 placeholders?
    - Secret values in error messages or logs?
    - Path traversal: filepath.Join used to resolve user input?
    - Auth bypass: missing claims check before action?
    - JWT signing key in plaintext anywhere?

  error-handling
    - `err` ignored with `_` where it shouldn't be?
    - Missing %w wrap in fmt.Errorf?
    - sql.Rows: missing rows.Err() check after iteration?
    - panic without recover in goroutines?
    - error returned but caller discards / logs and continues?

  performance
    - n+1 queries (loop calling store.Get)?
    - sql query without LIMIT on user-facing path?
    - Missing index on a column that's filtered in WHERE?
    - json.Marshal / Unmarshal in a hot path that could be 
      preallocated or streamed?
    - allocation inside a `for` loop that hoists outside?

  edge-cases
    - Nil pointer deref on optional model fields?
    - Empty slice handled differently from nil slice?
    - Integer overflow on 32-bit builds (int vs int64)?
    - Time zone assumed UTC when it isn't?
    - JSON unmarshal of missing fields silently zero-valuing?

Report format:

```
## Category: <name>

### P0 — corrupts data or breaks build
- File: backend/internal/foo/bar.go:42
- Issue: <one paragraph>
- Suggested fix: <one paragraph or 5-line snippet>

### P1 — race condition, leak, perf regression
...

### P2 — cosmetic, missing test, doc drift
...

### Coverage notes
What I checked. What I didn't. Why the boundaries are where they are.
```

Never modify files. Report only.
```

### Layer 3 — GitHub Actions (automated triggers)

#### Issue autofix

**File:** `.github/workflows/cooker-issue-autofix.yml`

```yaml
name: cooker-issue-autofix
on:
  issues:
    types: [labeled]
  workflow_dispatch:
    inputs:
      issue_number:
        description: 'GitHub issue number to fix'
        required: true

permissions:
  contents: write
  pull-requests: write
  issues: read

jobs:
  fix:
    # Only fire when the issue gets the "claude-fix" label or when
    # manually dispatched.
    if: |
      github.event_name == 'workflow_dispatch' ||
      (github.event_name == 'issues' && github.event.label.name == 'claude-fix')
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: anthropics/claude-code-action@v1
        with:
          prompt: |
            /issue-fix ${{ github.event.issue.number || inputs.issue_number }}
          # Allow the action to commit + push a fix branch and open
          # a draft PR.
          allowed-tools: Bash,Read,Edit,Write,mcp__github__create_pull_request,mcp__github__push_files
```

Note: the exact action name / input shape may differ — check
[`anthropics/claude-code-action`](https://github.com/anthropics/claude-code-action)
for the current spec. Cooker already has
`.github/workflows/cooker-weekly.yml` using this pattern, so copy
from there.

#### Extend the existing weekly workflow

The existing `.github/workflows/cooker-weekly.yml` already runs
`/cooker-weekly`. After Phase 1 + Phase 2 merged, extend its prompt
to include the new packages:

```yaml
# Inside the existing job's prompt input, add:
When scanning for bugs, prioritise the Phase 1 + Phase 2 packages 
added in PR #89 since they're newest and least battle-tested:
  backend/internal/jobqueue/
  backend/internal/runstate/
  backend/internal/notifier/
  backend/internal/scheduler/
  backend/internal/templates/
  backend/internal/source/{gitlab,bitbucket,gitea}/
  backend/internal/auth/permission.go

See docs/architecture-phase1-phase2.md for design intent.
```

#### PR review

**File:** `.github/workflows/cooker-pr-review.yml`

```yaml
name: cooker-pr-review
on:
  pull_request:
    types: [opened, ready_for_review]

permissions:
  contents: read
  pull-requests: write

jobs:
  review:
    # Skip Claude's own PRs to avoid review loops.
    if: github.event.pull_request.user.login != 'github-actions[bot]'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          ref: ${{ github.event.pull_request.head.sha }}
      - uses: anthropics/claude-code-action@v1
        with:
          prompt: /review
          allowed-tools: Read,Grep,Glob,mcp__github__pull_request_review_write,mcp__github__add_comment_to_pending_review
```

Reuses the existing `/review` skill in your registry. Posts a single
review pass with comments inline.

---

## Decision framework

Use the routine in this priority order. When in doubt, **escalate to
the human**.

| Trigger | What fires | Output |
|---|---|---|
| Issue gets `claude-fix` label | `cooker-issue-autofix` workflow → `/issue-fix` → `cooker-issue-fixer` | Draft PR linked to the issue |
| CI fails on `main` | (manual today) `/cooker-ci-debug` | Diagnosis comment on the failing run |
| Cron @ Monday 1pm UTC | `cooker-weekly.yml` workflow → `/cooker-weekly` | Draft PR for one new finding |
| Human opens a PR | `cooker-pr-review.yml` → `/review` | Review comments on the PR |
| Before each release | (manual) `/cross-scenario-audit` | Multi-category audit report |
| Operator asks "is this safe?" | (manual) `/cooker-audit` | Targeted audit of the named area |

---

## Risks and guardrails

### Never auto-merge

Every Claude-generated PR is **draft** by default. The human reviews
before merge. This rule is enforced in the subagent definitions
above (`cooker-issue-fixer` opens drafts only). Do not relax it.

### Cap scope per run

- One issue per `/issue-fix` invocation.
- One risk category per `cooker-bug-hunter` invocation.
- If a fix grows past 200 LOC or 3 files, the subagent escalates to
  the human instead of charging on.

### Don't fight Claude with Claude

The `cooker-pr-review.yml` workflow has `if:` clause to skip its
own PRs. Without it, every Claude-generated fix would trigger a
self-review that triggers another change that triggers another
review — ad infinitum.

### Cost budget

Each issue-trigger is at least one Claude Code Action invocation
(~5–10 minutes of compute). Set a monthly budget in your
Anthropic console. Consider:

- Limiting `cooker-issue-autofix` to issues with a specific label
  (already done: only `claude-fix`-labelled issues fire)
- Rate-limiting the workflow with `concurrency: { group: cooker-claude, cancel-in-progress: false }`
- Pausing it during incidents (manually disable the workflow)

### Don't let it fix infrastructure

The subagent definitions don't grant access to
`.github/workflows/`, `deploy/helm/`, or `deploy/kubernetes/`. A
broken workflow yaml can disable Claude's own ability to fix
workflows, so changes there should always be human-authored.

If you must allow it, gate behind a separate skill
(`/edit-workflows`) with a stricter prompt that requires a human
approver label before merge.

---

## Rollout plan

Don't ship everything at once. Order:

| Week | What |
|---|---|
| **1** | Extend the existing `cooker-weekly.yml` prompt to include the Phase 1+2 packages. Smallest change; uses what's already there. |
| **2** | Add `cooker-bug-hunter` subagent + `/cross-scenario-audit` skill. Manual trigger only; no workflow yet. |
| **3** | Run `/cross-scenario-audit` manually a few times. Tune the category lists in `cooker-bug-hunter.md`. |
| **4** | Add `cooker-issue-fixer` subagent + `/issue-fix` skill. Manual trigger only. |
| **5** | Run `/issue-fix` against 3–5 real issues. Tune the escalation thresholds. |
| **6** | Add `cooker-issue-autofix.yml` workflow. Watch what happens for a week. Adjust the label / dispatch criteria. |
| **7** | Add `cooker-pr-review.yml` workflow. |
| **8** | Review monthly cost. Tune. |

This order minimises the risk of breaking your dev loop with a
misbehaving automation that opens dozens of bad PRs at once.

---

## Verification

After rollout, sanity check:

```bash
# How many Claude-generated PRs in the last 30 days?
gh pr list --author github-actions[bot] --state all --limit 100 \
  --json createdAt,title | jq '[.[] | select(.createdAt > "'(date -d '30 days ago' -Iso)'")] | length'

# What was their merge rate?
# (If <30%, the prompts need tuning. If >80%, you're shipping too fast.)

# Time-to-first-review on Claude PRs?
# (Should be < 4 hours during business hours.)
```

A healthy routine produces 1–3 draft PRs per week from `cooker-weekly`,
0–2 from `issue-autofix`, and a comment from `pr-review` on every
incoming human PR. The maintainer's time per Claude PR should drop
to <15 minutes (read the plan section, scan the diff, approve or
request changes).

---

## File checklist

Files to create (in order):

- [ ] `.claude/skills/cross-scenario-audit.md`
- [ ] `.claude/skills/issue-fix.md`
- [ ] `.claude/agents/cooker-bug-hunter.md`
- [ ] `.claude/agents/cooker-issue-fixer.md`
- [ ] `.github/workflows/cooker-issue-autofix.yml`
- [ ] `.github/workflows/cooker-pr-review.yml`
- [ ] Edit existing `.github/workflows/cooker-weekly.yml` prompt to include Phase 1+2 packages

## Useful prior art

The Phase 1 + Phase 2 PR demonstrated the same `.claude/`-driven
workflow shape — 21 commits landed on a branch, self-reviewed,
documented, merged via the routine. The pattern works; the routine
below makes it repeatable for bug-fixing rather than feature work.

See:

- [`docs/adapted-from-dokploy.md`](./adapted-from-dokploy.md)
- [`docs/architecture-phase1-phase2.md`](../reference/architecture-phase1-phase2.md)
- [`docs/game-changer-ideas.md`](./game-changer-ideas.md)
- [Claude Code Action docs](https://github.com/anthropics/claude-code-action) (external)
