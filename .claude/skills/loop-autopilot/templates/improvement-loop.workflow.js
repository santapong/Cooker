// Template: IMPROVEMENT LOOP — the autonomous engineering loop's engine.
// Adapts loop-until-budget.workflow.js: each round pulls a deduped batch of feedback,
// runs act->verify per item (pipeline, no barrier), and collects PROPOSALS; when there
// is no fresh feedback it does a research/tech-debt round instead. Guarded by budget
// floor (loop-policy L2), dry counter (L1), and a hard round cap (L4); dedups against
// everything seen + open issues/PRs (L3). Ends every unit of work at a propose gate — it
// NEVER merges (harness policy H11).
//
// SAFETY: defaults to mode:"dry" — it returns proposal objects and opens NOTHING.
// Only wire the live PR-creation step (marked EDIT ME) once you trust it, and even then
// open DRAFT PRs on claude/ branches and never merge.
//
// Invoke with: Workflow({ script, args: { repo: {owner,name}, mode: "dry"|"live", maxRounds, floor } })

export const meta = {
  name: 'improvement-loop-template', // EDIT ME
  description: 'Autonomous improvement loop: intake feedback, act+verify per item, propose; research when idle', // EDIT ME
  phases: [
    { title: 'Intake', detail: 'read + dedup feedback' },
    { title: 'Act', detail: 'triage + implement per item' },
    { title: 'Verify', detail: 'self-review + risk memo' },
    { title: 'Research', detail: 'improvement ideas when idle' },
  ],
}

const input = typeof args === 'string' ? JSON.parse(args) : args
const REPO = (input && input.repo) || { owner: 'OWNER', name: 'REPO' } // EDIT ME
const MODE = input && input.mode === 'live' ? 'live' : 'dry' // default dry = safe
const FLOOR = (input && input.floor) || 60000 // one round + verification headroom (L2)
const MAX_ROUNDS = (input && input.maxRounds) || 6 // hard backstop (L4)
const DRY_LIMIT = 2 // stop after K idle rounds (L1)

const ITEMS_SCHEMA = {
  type: 'object',
  properties: {
    items: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          id: { type: 'string' },
          kind: { type: 'string', enum: ['issue', 'pr-comment', 'ci-failure', 'tech-debt'] },
          title: { type: 'string' },
          detail: { type: 'string' },
          ref: { type: 'string' },
          priority: { type: 'number' },
        },
        required: ['kind', 'title'],
      },
    },
  },
  required: ['items'],
}
const TRIAGE_SCHEMA = {
  type: 'object',
  properties: { approach: { type: 'string' }, skill: { type: 'string' }, worthDoing: { type: 'boolean' } },
  required: ['approach', 'worthDoing'],
}
const ACT_SCHEMA = {
  type: 'object',
  properties: { branch: { type: 'string' }, summary: { type: 'string' }, filesChanged: { type: 'array', items: { type: 'string' } }, testAdded: { type: 'boolean' } },
  required: ['summary'],
}
const VERIFY_SCHEMA = {
  type: 'object',
  properties: { safeToPropose: { type: 'boolean' }, risk: { type: 'string', enum: ['low', 'medium', 'high'] }, memo: { type: 'string' } },
  required: ['safeToPropose', 'risk'],
}
const RESEARCH_SCHEMA = {
  type: 'object',
  properties: {
    proposals: {
      type: 'array',
      items: {
        type: 'object',
        properties: { title: { type: 'string' }, rationale: { type: 'string' }, source: { type: 'string' } },
        required: ['title', 'rationale'],
      },
    },
  },
  required: ['proposals'],
}

const key = (it) => `${it.kind || 'idea'}:${it.ref || it.title}`
const seen = new Set()
const proposals = []
let dry = 0
let round = 0

if (MODE === 'dry') log('DRY mode: no branches, PRs, or merges — returning proposal objects only')
if (!budget.total) log('no budget target set — running a single bounded round')

do {
  round++

  // INTAKE — read-only. Reads real issues/PRs via the GitHub tools; dedups against open work.
  const intake = await agent(
    `Repo: ${REPO.owner}/${REPO.name}. Using the GitHub tools, gather ACTIONABLE, deduplicated feedback: open issues (list_issues/search_issues) not already covered by an open PR, plus unresolved review comments and failing CI on open PRs (pull_request_read get_comments/get_review_comments/get_check_runs). Skip anything already tracked by an open issue/PR. Return raw items; [] if none.`,
    { label: `intake:r${round}`, phase: 'Intake', schema: ITEMS_SCHEMA },
  )
  const fresh = ((intake && intake.items) || []).filter((it) => !seen.has(key(it)))
  fresh.forEach((it) => seen.add(key(it)))
  log(`round ${round}: ${fresh.length} fresh feedback items` + (budget.total ? `, ${Math.round(budget.remaining() / 1000)}k left` : ''))

  if (!fresh.length) {
    // IDLE → one research/tech-debt round (produces proposal ideas, not code).
    dry++
    const research = await agent(
      `Repo: ${REPO.owner}/${REPO.name}. No pending feedback. Propose high-value improvements grounded in the project plus market/ecosystem trends and research (use research tools if available). Verify each idea against a real source; dedup against any open issue proposing the same. Return proposals.`,
      { label: `research:r${round}`, phase: 'Research', schema: RESEARCH_SCHEMA },
    )
    const ideas = ((research && research.proposals) || []).filter((p) => !seen.has(key({ title: p.title })))
    ideas.forEach((p) => seen.add(key({ title: p.title })))
    proposals.push(...ideas.map((p) => ({ source: 'research', title: p.title, rationale: p.rationale, ref: p.source })))
    log(`round ${round}: idle -> ${ideas.length} research proposals (dry=${dry})`)
    continue
  }
  dry = 0

  // ACT -> VERIFY per item, no barrier (H1). Prioritize highest first.
  const ordered = fresh.slice().sort((a, b) => (b.priority || 0) - (a.priority || 0))
  const handled = await pipeline(
    ordered,
    (it) =>
      agent(
        `Triage this ${it.kind} for ${REPO.owner}/${REPO.name} and decide the approach + which sibling skill owns it (loop-debug, loop-design, loop-test, loop-scout). Item: ${it.title} — ${it.detail || ''} (${it.ref || ''}). Set worthDoing=false to skip.`,
        { label: `triage:${key(it)}`, phase: 'Act', schema: TRIAGE_SCHEMA },
      ).then((t) => ({ it, triage: t })),
    (prev) => {
      if (!prev || !prev.triage || !prev.triage.worthDoing) return prev
      const verb = MODE === 'live'
        ? 'Implement the change on a NEW claude/-prefixed branch (design -> implement -> add a fails-before/passes-after test -> update docs). Do NOT push to main or merge.'
        : 'Describe the change you WOULD make (design, files, the test you would add) without editing anything.'
      // AP5 (Tangled Loop) fix: in live mode the Act stage mutates files and multiple
      // items can run concurrently under pipeline() (H1), so each gets its own git
      // worktree (H7). Dry mode is read-only and needs no isolation.
      const actOpts = { label: `act:${key(prev.it)}`, phase: 'Act', schema: ACT_SCHEMA }
      if (MODE === 'live') actOpts.isolation = 'worktree'
      return agent(
        `${verb}\nItem: ${prev.it.title} — ${prev.it.detail || ''}\nApproach: ${prev.triage.approach}`,
        actOpts,
      ).then((a) => ({ ...prev, act: a }))
    },
    (prev) => {
      if (!prev || !prev.act) return prev
      return agent(
        `Adversarially review this change and write its impact/risk memo (loop-review + loop-audit). Set safeToPropose=false if it is unsafe, unclear, or unverified.\nChange: ${prev.act.summary}\nFiles: ${(prev.act.filesChanged || []).join(', ')}`,
        { label: `verify:${key(prev.it)}`, phase: 'Verify', schema: VERIFY_SCHEMA },
      ).then((v) => ({ ...prev, verify: v }))
    },
  )

  // PROPOSE gate — collect proposals. NEVER merge.
  for (const h of handled.filter(Boolean)) {
    if (!h.triage || !h.triage.worthDoing) { log(`skip ${key(h.it)}: triaged out`); continue }
    if (!h.act || !h.verify || !h.verify.safeToPropose) { log(`skip ${key(h.it)}: not safe to propose`); continue }
    // EDIT ME (live mode): open a DRAFT PR from h.act.branch and post a summary comment here —
    //   mcp__github__create_pull_request({ draft: true, head: h.act.branch, base: 'main', ... })
    //   mcp__github__add_issue_comment(...). NEVER call merge_pull_request.
    // Apply the `automated` label AND a `kind:${h.it.kind}` label (or a hidden
    //   `<!-- credit-kind: ${h.it.kind} -->` body marker) so credit-ledger.workflow.js
    //   can attribute this PR's outcome back to its kind. See references/credit-horizon.md.
    proposals.push({ source: 'feedback', item: h.it, change: h.act, risk: h.verify })
  }
} while (budget.total && budget.remaining() > FLOOR && round < MAX_ROUNDS && dry < DRY_LIMIT)

log(`done: ${round} round(s), ${proposals.length} proposals (${MODE} mode) — nothing merged`)
return { mode: MODE, rounds: round, proposals }
