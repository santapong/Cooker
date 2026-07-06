// Template: CREDIT-LEDGER RECONCILE — the credit-horizon hook's write side.
// Runs on its own schedule (daily), separate from the improvement loop. Finds every
// PR the loop has ever opened, classifies terminal outcomes, and updates the
// trust-weight ledger (persisted as a pinned, labeled GitHub issue body) that
// improvement-loop.workflow.js's triage stage will read. See credit-horizon.md.
//
// PREREQUISITE (one small addition to improvement-loop.workflow.js's Propose stage):
// tag each opened PR with its item's kind — either a `kind:<kind>` label, or a hidden
// marker in the body (`<!-- credit-kind: tech-debt -->`). Everything else — including
// counting `proposed` — happens lazily in here the first time this pass sees a PR, so
// that's the ONLY change the main loop needs.
//
// H10: workflow scripts can't read the clock (Date.now()/argless new Date() throw), so
// the current time is passed in via args. The deploying Routine supplies it.
//
// Invoke with: Workflow({ script, args: { repo: {owner,name}, ledgerIssueNumber, nowMs, nowIso } })
//   nowMs  — current time in epoch milliseconds (Number)
//   nowIso — current time as an ISO-8601 string (used for lastRecalc)

export const meta = {
  name: 'credit-ledger-reconcile-template', // EDIT ME
  description: 'Reconcile closed/stale automated PRs into the credit ledger; recompute trust weights in batches',
  phases: [
    { title: 'Fetch', detail: 'automated PRs, open or closed, since last look' },
    { title: 'Classify', detail: 'merged / merged-with-changes / rejected / stale / still pending' },
    { title: 'Update', detail: 'ledger counts + batched trust-weight recalc' },
  ],
}

const input = typeof args === 'string' ? JSON.parse(args) : args
const REPO = (input && input.repo) || { owner: 'OWNER', name: 'REPO' } // EDIT ME
const LEDGER_ISSUE = (input && input.ledgerIssueNumber) || null // EDIT ME: the pinned ledger issue #
const NOW_MS = (input && input.nowMs) || 0 // current time (epoch ms), supplied by the Routine
const NOW_ISO = (input && input.nowIso) || null // current time (ISO-8601), for lastRecalc
const BATCH_SIZE = 10 // recompute trustWeight after this many NEW outcomes total (knob 3)
const STALE_DAYS = 21 // open this long with no activity -> counted as stale, not pending

const KINDS = ['issue', 'pr-comment', 'ci-failure', 'tech-debt', 'research-idea']

function emptyLedger() {
  const kinds = {}
  for (const k of KINDS) kinds[k] = { proposed: 0, merged: 0, mergedWithChanges: 0, rejected: 0, stale: 0, trustWeight: 0.6 }
  return { kinds, counted: [], resolved: [], pendingSinceRecalc: 0, lastRecalc: null }
}

function extractKind(pr) {
  const label = (pr.labels || []).map((l) => (l && l.name) || l).find((n) => typeof n === 'string' && n.startsWith('kind:'))
  if (label) return label.slice(5)
  const m = /<!--\s*credit-kind:\s*([\w-]+)\s*-->/.exec(pr.body || '')
  return m ? m[1] : null
}

// Returns a terminal outcome string, or null if the PR is still legitimately pending.
// Uses NOW_MS (passed via args) + Date.parse (a static parse of a fixed string) so it
// never reads the clock inside the script — H10.
function classify(pr) {
  if (pr.merged) {
    // Heuristic: commits pushed after the first review-requesting comment suggest
    // changes were requested and addressed, vs. a clean first-shot merge.
    // EDIT ME if you have a more precise signal (e.g. a recorded changes-requested review).
    return (pr.commitsAfterFirstReview || 0) > 0 ? 'mergedWithChanges' : 'merged'
  }
  if (pr.state === 'closed') return 'rejected'
  if (!NOW_MS) return null // no clock supplied — treat everything open as still pending
  const ageDays = (NOW_MS - Date.parse(pr.updatedAt)) / 86400000
  return ageDays > STALE_DAYS ? 'stale' : null
}

if (!LEDGER_ISSUE) {
  log('no ledgerIssueNumber provided — create the pinned "🤖 Credit Ledger" issue once, by hand, then pass its number')
} else {
  // 1. FETCH — every PR labeled "automated", any state (open ones may have just gone stale).
  const prs = await agent(
    `Repo: ${REPO.owner}/${REPO.name}. Using the GitHub tools, list ALL pull requests labeled "automated" ` +
      `(any state). For each return: number, merged (bool), state, updatedAt, labels, body, and ` +
      `commitsAfterFirstReview (commits pushed after the first review-requesting comment, 0 if none/unclear).`,
    {
      label: 'fetch-automated-prs',
      phase: 'Fetch',
      schema: {
        type: 'object',
        properties: {
          items: {
            type: 'array',
            items: {
              type: 'object',
              properties: {
                number: { type: 'number' },
                merged: { type: 'boolean' },
                state: { type: 'string' },
                updatedAt: { type: 'string' },
                labels: { type: 'array' },
                body: { type: 'string' },
                commitsAfterFirstReview: { type: 'number' },
              },
              required: ['number', 'merged', 'state', 'updatedAt'],
            },
          },
        },
        required: ['items'],
      },
    },
  )

  // 2. Read current ledger from the pinned issue body (heal if missing/corrupt).
  const current = await agent(
    `Repo: ${REPO.owner}/${REPO.name}. Read issue #${LEDGER_ISSUE} and return its body verbatim as "json".`,
    { label: 'read-ledger', phase: 'Update', schema: { type: 'object', properties: { json: { type: 'string' } }, required: ['json'] } },
  )
  let ledger
  try { ledger = JSON.parse((current && current.json) || '') } catch { ledger = null }
  if (!ledger || !ledger.kinds) ledger = emptyLedger()
  for (const k of KINDS) if (!ledger.kinds[k]) ledger.kinds[k] = emptyLedger().kinds[k]
  if (!ledger.counted) ledger.counted = []
  if (!ledger.resolved) ledger.resolved = []

  // 3. CLASSIFY. `proposed` increments the first time a PR is ever seen (any state);
  //    an outcome bucket increments exactly once, the first time that PR reaches a
  //    terminal state — never on a later re-run of an already-resolved PR.
  const countedSet = new Set(ledger.counted)
  const resolvedSet = new Set(ledger.resolved)
  const items = (prs && prs.items) || []
  let newOutcomes = 0

  for (const pr of items) {
    const kind = extractKind(pr)
    if (!kind || !ledger.kinds[kind]) continue // untagged or unknown kind — skip, don't guess
    if (!countedSet.has(pr.number)) {
      ledger.kinds[kind].proposed++
      countedSet.add(pr.number)
    }
    if (resolvedSet.has(pr.number)) continue // outcome already recorded once
    const outcome = classify(pr)
    if (!outcome) continue // still legitimately pending
    ledger.kinds[kind][outcome]++
    resolvedSet.add(pr.number)
    newOutcomes++
  }
  ledger.counted = Array.from(countedSet)
  ledger.resolved = Array.from(resolvedSet)
  ledger.pendingSinceRecalc += newOutcomes
  log(`saw ${items.length} PRs, ${newOutcomes} new terminal outcomes, pendingSinceRecalc=${ledger.pendingSinceRecalc}`)

  // 4. Batched trust-weight recalc (knob 3) — only fires once enough new evidence exists.
  if (ledger.pendingSinceRecalc >= BATCH_SIZE) {
    for (const k of KINDS) {
      const c = ledger.kinds[k]
      if (c.proposed > 0) c.trustWeight = Math.max(0, Math.min(1, (c.merged + 0.5 * c.mergedWithChanges) / c.proposed))
    }
    ledger.pendingSinceRecalc = 0
    ledger.lastRecalc = NOW_ISO
    log(`batch threshold hit — recalculated: ${JSON.stringify(Object.fromEntries(KINDS.map((k) => [k, ledger.kinds[k].trustWeight])))}`)
  }

  // 5. Write the ledger back, replacing the issue body entirely.
  await agent(
    `Repo: ${REPO.owner}/${REPO.name}. Replace issue #${LEDGER_ISSUE}'s body with exactly this JSON, ` +
      `no commentary before or after it:\n${JSON.stringify(ledger, null, 2)}`,
    { label: 'write-ledger', phase: 'Update', schema: { type: 'object', properties: { updated: { type: 'boolean' } }, required: ['updated'] } },
  )
}

return { done: true }

// NOTE for high-volume repos: `counted`/`resolved` grow one PR-number per entry
// forever. Fine for hundreds of PRs; if this repo does thousands, prune numbers older
// than your longest plausible re-open window instead of keeping the full history.
