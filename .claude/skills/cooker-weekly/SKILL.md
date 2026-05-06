---
name: cooker-weekly
description: Cooker's weekly bug-hunt + remediation routine. Reads open GitHub issues + recent commits, picks one open chain or new bug, lands a focused fix, and opens a draft PR. Trigger manually each Monday, or via the scheduled workflow at .github/workflows/cooker-weekly.yml. Bias toward landing one solid fix per week, not many shallow ones.
---

# Cooker — weekly bug hunt

Goal: each week, advance the open-finding count downward by exactly one PR's worth of work.

## Inputs

- **GitHub issues** opened or updated since the last weekly run. Look back **8 days** to overlap with the previous window so nothing falls through the cracks.
- **`docs/audits/chain-recheck.md`** — current Open / Closed / Mitigated state for each of the 54 chain failures.
- **`docs/audits/remediation-plan.md`** — themed fixes T1–T24, plus any new theme letters added since.
- **Recent commits** since the last `T-weekly-*` tag (or the last 7 days if no tag exists).

## Steps

### 1. Read recent activity

```
mcp__github__list_issues  state=open  perPage=50
mcp__github__list_pull_requests  state=open  perPage=20
```

```bash
# Last weekly tag, if any
last_tag=$(git tag --list 'T-weekly-*' --sort=-creatordate | head -1)
since="${last_tag:-7 days ago}"

git log --since "$since" --oneline
```

**Shortcut:** `.claude/skills/cooker-weekly/weekly-candidates.sh` prints a priority-ordered list of candidates (P1 issues → P2 open chains → P3 newly-introduced chains → P4 quick-win themes), plus the lookback window and recent commit summary. Run it first; pick the first item from the highest available bucket.

### 2. Pick exactly one item

Selection priority (highest first):

1. **A new issue tagged `bug` or `security`** that's reproducible from the description.
2. **The highest-severity STILL-OPEN chain** in `chain-recheck.md` that isn't blocked by a roadmap-only item (dual-key rotation, OIDC issuer migration, replay buffer).
3. **A "newly introduced" chain** from `chain-recheck.md`'s own bottom section — these are bugs the remediation pass introduced.
4. **A quick win** (Low / Medium item) if all the above are blocked.

**Hard skip rules:**

- Don't pick anything labelled `roadmap` or `needs-design` — not weekly material.
- Don't pick Phase 0 work (T1, T2, T3, T5) without asking the user — those are security hot-fixes that may already be in flight.
- Don't pick more than one item per run.

### 3. Branch

```bash
git switch -c "claude/weekly-$(date -u +%F)-<slug>" main
```

### 4. Implement

Follow the **cooker-improve** skill's pattern:

- One focused commit; no opportunistic cleanup.
- Race-detector clean (`.claude/skills/cooker-improve/check-pkg.sh internal/<pkg>`).
- Commit message references what it closes:
  ```
  weekly(<area>): <short title>
  
  <why + how>
  
  Closes [chain-id] from docs/audits/chain-recheck.md
  (or: Closes T<n> from docs/audits/remediation-plan.md).
  Selected for the week-of-<YYYY-MM-DD> run.
  ```

### 5. Update audit docs

If the fix closes a previously-Open chain in `chain-recheck.md`, edit the relevant row:

- `**Open**` → `**Closed by Tweekly-<YYYY-MM-DD>**`
- Add the commit SHA in the Notes column.

### 6. Open a draft PR

Title: `weekly: <theme>` (e.g. `weekly: in-mem idempotency cap`).

Body must include:

- The audit-doc finding ID (file:line)
- The chain ID from `chain-recheck.md` if applicable
- A short test-plan checklist
- The cron tag idea: `T-weekly-<YYYY-MM-DD>` so the next run can find this point

### 7. Tag main once merged

```bash
git tag T-weekly-<YYYY-MM-DD>
git push origin T-weekly-<YYYY-MM-DD>
```

The next weekly skill run uses this as its lookback boundary.

## Helpful commands

```bash
# What changed since last weekly tag?
last_tag=$(git tag --list 'T-weekly-*' --sort=-creatordate | head -1)
git log "${last_tag:-HEAD~50}"..HEAD --oneline

# How many chains are still open?
grep -c '\*\*Open\*\*' docs/audits/chain-recheck.md

# Issues that mention an audit finding ID
gh issue list --state open --json number,title,body \
  | jq -r '.[] | select(.body // "" | test("\\[A[0-9]+-[0-9]+\\]|T[0-9]+")) | "\(.number) \(.title)"'

# This week's candidate chains (open + has a citation)
awk '/\*\*Open\*\*/{print}' docs/audits/chain-recheck.md | head -20
```

## Out of scope

- Don't bundle multiple themes into one PR — defeats the rhythm.
- Don't open more than one PR per run; file the rest as issues for next week.
- Don't change CI workflows or hooks here — that's a separate ops concern.
- Don't run the cooker-weekly skill more than once per Monday morning; if you hit a problem, document it and try again next week.

## When you find nothing actionable

If every Open chain is blocked, every new issue needs design input, and the codebase is otherwise quiet:

1. Open one **maintenance** PR — bump dependencies, prune dead `// TODO`s with no associated finding, or migrate one test to table-driven form.
2. Or write a one-paragraph note in `docs/audits/chain-recheck.md`'s "Verdict" section confirming the state of the world this week.

The point is to leave a visible trace each week so a maintainer can see the cadence working without having to dig.
