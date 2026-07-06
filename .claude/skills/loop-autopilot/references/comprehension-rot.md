# Comprehension Rot

The one cost this plugin's other guards don't touch. Budget floors, dedup, and
`safeToPropose` all fail *loudly* — a run stops, an item gets dropped, you can see it
in the log. Comprehension rot doesn't: the gap between what the loop has shipped and
what you actually understand grows in silence, because nothing ever forces you to look.
A human rubber-stamping merges without reading the risk memo is still AP1 (Nodding
Loop) — it's just the anti-pattern living in the reviewer instead of the loop.

The fix is not "try to review more carefully." That's a resolution, not a mechanism,
and resolutions decay under load exactly when the loop is running well and volume is
high. The fix is a mechanism that makes the rot visible if it's happening.

## The mechanism: forced random sampling, not recency

A digest that shows you the *last* 3 merged PRs tests nothing — you skimmed those
already, or they're too recent to reveal a pattern. Sample **uniformly at random**
from everything the loop has merged in the window, so the digest can surface something
from three weeks ago you've genuinely forgotten.

## Weekly digest — what it does

Runs on a schedule (weekly; tune to your merge volume), separate from the improvement
loop itself. Shipped as a copy-paste Routine prompt at
`templates/comprehension-digest.routine.md` — a **live** Claude session does the random
sampling, which is why this is a Routine prompt rather than a `.workflow.js` template
(workflow scripts can't call `Math.random()` — harness policy H10).

1. **Query** all PRs opened by the loop in the trailing 7 days (filter on the
   `automated` label applied at creation — see `deployment.md`). Bucket by outcome:
   `merged`, `merged-with-changes`, `closed-unmerged`, `still-open`.
2. **Sample** 3 items uniformly at random from the `merged` + `merged-with-changes`
   buckets (skip the sample if fewer than 3 exist — don't pad with recent ones).
3. **Post** a new issue, `🔍 Weekly comprehension check — <date>`, labeled
   `comprehension-check`, containing:
   - The bucket counts for the week (the stats, not just the sample).
   - The 3 sampled PR links, each with one open question to force actual reading —
     not "did you review this?" but something that can't be answered without opening
     the diff, e.g. *"what would break if this PR were reverted?"*
4. **Do not auto-close it.** It closes when a human answers the three questions in a
   comment and closes it themselves.

## Why an unanswered issue is the point, not a bug

This is the load-bearing design choice: the digest issue is deliberately a nuisance if
ignored. An accumulating count of open `comprehension-check` issues is a visible,
countable signal — `open comprehension-check issues > 2` means the loop has been
running unread for weeks, and now you can *see* that number instead of only
discovering it the day something the loop shipped turns out to be wrong. Comprehension
rot converted from silent to loud is the entire fix; the three questions are just what
make "answering" require actually opening the diff instead of clicking close.

## Implementation note

Reuses the same `github` MCP tools as the rest of the plugin (`search_issues` /
`list_pull_requests` filtered by label and merge date, `create_issue`). No new
infrastructure — deploy as a second, lighter-weight Cloud Routine alongside the main
improvement loop (`deployment.md`), triggered on a weekly schedule rather than
`pull_request` events.
