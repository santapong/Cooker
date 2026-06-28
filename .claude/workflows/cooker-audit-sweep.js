// cooker-audit-sweep — comprehensive Cooker audit. Multi-modal finder
// fan-out over the codebase, dedup against a ledger, adversarial verify,
// loop until two dry rounds.
//
// Run:  Workflow({ name: "cooker-audit-sweep" })
// Pairs with ADLC Phase 10 (Improve). EXPENSIVE — only on an explicit
// "comprehensive/thorough audit" ask. Mirrors the cooker-audit skill's
// anti-patterns. See .claude/skills/workflow/SKILL.md.
//
// Pattern: loop-until-dry + multi-modal sweep + adversarial verify.
// Dedup is against `seen` (NOT `confirmed`) so judge-rejected findings
// don't reappear every round.

export const meta = {
  name: 'cooker-audit-sweep',
  description: 'Multi-modal audit fan-out over the Cooker codebase (comment-vs-code, per-process state, unbounded I/O, shell injection, dev defaults), dedup + adversarial verify, loop until two dry rounds.',
  whenToUse: 'A once-a-quarter or pre-launch deep audit. EXPENSIVE — only when the user asks for a comprehensive sweep.',
  phases: [
    { title: 'Sweep' },
    { title: 'Verify' },
  ],
}

// Each finder searches a DIFFERENT way and is blind to the others.
// These mirror the highest-yield anti-patterns from the cooker-audit skill.
const FINDERS = [
  {
    lens: 'comment-vs-code',
    prompt:
      'Audit Cooker for comments that promise behaviour the code does not implement ' +
      '(deadlines, retries, cleanup, "extended with", timeouts). Grep for declarative ' +
      'comments and check the code under them.',
  },
  {
    lens: 'per-process-state',
    prompt:
      'Audit backend/internal/server for per-process in-memory state (map/channel/sync.Map) ' +
      'in features that claim multi-replica support. Ask: what breaks at replicaCount=3 ' +
      'without sticky sessions?',
  },
  {
    lens: 'unbounded-io',
    prompt:
      'Audit Cooker for external I/O without a per-call timeout, and unbounded resource growth ' +
      '(io.ReadAll on request bodies, unbounded slice appends, channel buffer sizes).',
  },
  {
    lens: 'shell-injection',
    prompt:
      'Audit Cooker for fmt.Sprintf shell-string interpolation fed to /bin/sh -c or ' +
      'exec.Command("sh","-c",...), especially in builder adapters.',
  },
  {
    lens: 'dev-defaults',
    prompt:
      'Audit Cooker for dev defaults reaching production: getEnv("X","<default>") values not ' +
      'gated by Config.Validate for IsProduction() in backend/internal/config.',
  },
]

const BUGS_SCHEMA = {
  type: 'object',
  properties: {
    bugs: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          title:    { type: 'string' },
          file:     { type: 'string' },
          severity: { type: 'string', enum: ['critical', 'high', 'medium', 'low'] },
        },
        required: ['title', 'file', 'severity'],
      },
    },
  },
  required: ['bugs'],
}

const VERDICT_SCHEMA = {
  type: 'object',
  properties: {
    real:   { type: 'boolean' },
    reason: { type: 'string' },
  },
  required: ['real', 'reason'],
}

const keyOf = b => `${b.file}::${b.title}`.toLowerCase()
const seen = new Set()
const confirmed = []
let dryRounds = 0
let round = 0

while (dryRounds < 2) {
  round += 1
  log(`audit sweep: round ${round} (${confirmed.length} confirmed so far)`)

  // Barrier: collect every finder's bugs this round before deduping.
  const found = (await parallel(FINDERS.map(f => () =>
    agent(
      `${f.prompt}\n\nDo not re-flag anything already catalogued in docs/audits/. ` +
      `Read the cited file before reporting; cite file:line.`,
      { label: `sweep:${f.lens}:r${round}`, phase: 'Sweep', schema: BUGS_SCHEMA },
    ))))
    .filter(Boolean)
    .flatMap(r => r.bugs)

  const fresh = found.filter(b => !seen.has(keyOf(b)))
  if (fresh.length === 0) { dryRounds += 1; continue }
  dryRounds = 0
  fresh.forEach(b => seen.add(keyOf(b)))

  const judged = await parallel(fresh.map(b => () =>
    agent(
      `Adversarially verify this Cooker finding by reading the cited file. Default real=false ` +
      `if you cannot confirm it. Check the known-false-positive list in the cooker-audit skill ` +
      `(AES-GCM-sealed secrets, webhook FullName matching, wsHub launch order, etc.).\n\n` +
      `Title: ${b.title}\nFile: ${b.file}\nSeverity: ${b.severity}`,
      { label: `verify:${b.file}:r${round}`, phase: 'Verify', schema: VERDICT_SCHEMA },
    ).then(v => ({ ...b, verdict: v }))))

  confirmed.push(...judged.filter(Boolean).filter(j => j.verdict?.real))
}

log(`audit sweep complete: ${confirmed.length} confirmed finding(s) after ${round} round(s)`)
return { confirmed }
