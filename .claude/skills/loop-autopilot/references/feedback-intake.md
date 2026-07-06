# Feedback Intake

How the loop reads what to work on, from four sources, and how it avoids acting on the same thing twice. All GitHub reads use the `github` MCP tools (available in Claude Code on the web; auth via the GitHub proxy — your token never enters the container).

## 1. GitHub issues (polled)

Cloud Routines cannot trigger on issues or issue-comments, so issues are **polled each run**:

- `list_issues` — enumerate open issues (filter by `state: open`, `labels`, `since` for new/updated).
- `search_issues` — targeted queries (e.g. `is:open label:bug`, updated-since a timestamp). Put only search criteria in the query; use the separate `sort`/`order` params. Call `get_me` first for identity.
- `issue_read` — pull a single issue's full body and comment thread once it's selected.

Filter to **actionable, unassigned, not-already-in-a-PR** issues. Prefer a label convention (e.g. only act on issues labeled `automate` or `good-first-issue`) so the loop's scope is opt-in, not the whole tracker.

## 2. PR comments + CI failures

- `pull_request_read` methods: `get_comments` (conversation), `get_review_comments` (inline/diff review threads), `get_reviews`, `get_status` / `get_check_runs` (CI). This is the core "what feedback is on this PR" call.
- `subscribe_pr_activity` — stream new review comments and CI results into the session for reactive handling; a `pull_request` Routine trigger fires a fresh run on PR events.
- Act only on **clear, unambiguous** comments and **real** CI failures. For an ambiguous or architecturally-significant comment, open a reply asking for clarification instead of guessing — never implement a guess.

## 3. Idle → research improvements

When 1 and 2 return nothing fresh, switch to discovery with `loop-research`:

- Scan **market/ecosystem trends** and **research papers** (the `alphaXiv` MCP for academic sources) relevant to the project's domain.
- Read the project itself to ground ideas in what's actually here.
- Produce **cited, verified** improvement proposals — every idea carries a source and has survived the skill's refute-first check, so the loop proposes evidence, not speculation.

## 4. Tech-debt scan (proactive)

Also surface improvement candidates from the code itself:

- `loop-review` — security/quality issues worth fixing.
- `loop-audit` — risk/coverage gaps in recent changes.
- `loop-scout` — places the project reinvents something a maintained library already does.

## Dedup — the intake gate

Before any item becomes work, drop it if it is already covered:

1. Query **open PRs** (`list_pull_requests`) and **open issues** — if an item is already tracked, skip it.
2. Keep a `seen` set within the run keyed on issue/PR number or `kind:title`.
3. For research/tech-debt ideas, check that no open issue already proposes the same thing before opening a new one.

The output of intake is a deduped, prioritized list of `{ id, kind, title, detail, ref }` items — `kind` ∈ `issue | pr-comment | ci-failure | research-idea | tech-debt`. That list is what the act→verify→propose pipeline consumes.
