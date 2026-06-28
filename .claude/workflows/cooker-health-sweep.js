// cooker-health-sweep — ADLC discovery + plan for a bug-hunt +
// performance/reliability initiative.
//
// Run:  Workflow({ name: "cooker-health-sweep" })
// Pairs with ADLC Phase 0/10 (Intake + Improve) feeding Phase 1 (Plan).
//
// Pattern: multi-modal finder fan-out (correctness / performance /
// reliability / concurrency) -> adversarial verify (default-reject, must
// still be OPEN) -> synthesize a prioritized remediation plan with a
// concrete fix method per finding. Reads the existing audit docs first so
// it does NOT re-flag already-closed work.

export const meta = {
  name: 'cooker-health-sweep',
  description: 'Discovery + plan for the bug-hunt + perf/reliability initiative: fan out finders across correctness, performance, reliability/SPOF and concurrency; adversarially verify (default-reject, must still be OPEN); synthesize a prioritized remediation plan with a concrete fix method per finding.',
  whenToUse: 'Driving an ADLC health initiative — surface OPEN bugs + perf + reliability gaps and the method to fix each, without re-flagging closed audit items.',
  phases: [
    { title: 'Discover' },
    { title: 'Verify' },
    { title: 'Plan' },
  ],
}

const PRELUDE =
  'You are auditing the Cooker repo (Go backend under backend/internal, React frontend under frontend/src). ' +
  'FIRST read the relevant existing audit docs so you do NOT re-flag closed work: ' +
  'docs/audits/chain-recheck.md (Open/Closed state of the chain failures), ' +
  'docs/audits/remediation-plan.md (themes T1-T24), docs/audits/launch-readiness.md, ' +
  'docs/audits/dag-performance.md, docs/audits/spof-and-database.md. ' +
  'Use .claude/skills/cooker-audit/audit-greps.sh and .claude/skills/cooker-find/where-is.sh to navigate. ' +
  'Only report issues that are still OPEN or genuinely NEW. READ the cited file before reporting — never trust grep alone. ' +
  'Cite file:line and one line of evidence for every finding.'

const DIMENSIONS = [
  { key: 'correctness', focus:
    'Correctness bugs: stub stages that silently succeed (executor.go executeTest/executeApproval/executeCustom), ' +
    'nil-deref on stageMap mismatch, dagrunner fail-stop with no retry/backoff, missing handler input validation, ' +
    'raw err.Error() leaking into 5xx response bodies, and comments that promise behaviour the code never implements.' },
  { key: 'performance', focus:
    'Performance: N+1 queries in internal/store/postgres, missing indexes on hot columns, costly JSONB scans, ' +
    'per-request allocations in hot loops, large/unbounded io.ReadAll, DAG-runner scheduling cost (see dag-performance.md), ' +
    'and WebSocket fan-out cost. Name the hot path and quantify where you can.' },
  { key: 'reliability', focus:
    'Reliability / SPOF: per-process in-memory state in multi-replica features (wsticket, ratelimit, idempotency), ' +
    'external I/O without a per-call context timeout (Push/Build/Deploy/Apply/Get), boot-path lifecycle ordering and ' +
    'cleanup-on-early-return, orphan running-row sweep gaps, OIDC issuer/JWKS staleness.' },
  { key: 'concurrency', focus:
    'Concurrency / resource leaks: goroutine join races (server/runs.go), channel close-twice (server/websocket.go), ' +
    'map mutation under RLock, unbounded channels/slices, and context cancellation not propagated to spawned goroutines.' },
]

const FINDINGS_SCHEMA = {
  type: 'object',
  properties: {
    findings: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          title:    { type: 'string' },
          file:     { type: 'string', description: 'path:line' },
          severity: { type: 'string', enum: ['critical', 'high', 'medium', 'low'] },
          evidence: { type: 'string', description: 'one line quoting or paraphrasing the cited code' },
          tracking: { type: 'string', description: 'existing audit ID (e.g. [A3-2], T10, B.5.1) or "new"' },
        },
        required: ['title', 'file', 'severity', 'evidence', 'tracking'],
      },
    },
  },
  required: ['findings'],
}

const VERDICT_SCHEMA = {
  type: 'object',
  properties: {
    isReal:    { type: 'boolean', description: 'true ONLY if reproduced in the cited file' },
    stillOpen: { type: 'boolean', description: 'false if already closed/mitigated in the audit docs' },
    severity:  { type: 'string', enum: ['critical', 'high', 'medium', 'low'] },
    reasoning: { type: 'string' },
    fixSketch: { type: 'string', description: 'one-line concrete fix method (which file + which pattern)' },
  },
  required: ['isReal', 'stillOpen', 'severity', 'reasoning', 'fixSketch'],
}

phase('Discover')

const reviewed = await pipeline(
  DIMENSIONS,
  d => agent(
    `${PRELUDE}\n\nDIMENSION: ${d.key}.\n${d.focus}\n\nReturn up to 5 of the highest-signal findings (quality over quantity).`,
    { label: `discover:${d.key}`, phase: 'Discover', schema: FINDINGS_SCHEMA },
  ),
  (rev, d) => parallel((rev?.findings ?? []).map(f => () =>
    agent(
      `Adversarially verify this ${d.key} finding by READING the cited Cooker file ${f.file}. ` +
      `Set isReal=true ONLY if it actually reproduces in the code. Set stillOpen=false if it is already ` +
      `closed/mitigated in docs/audits/chain-recheck.md or remediation-plan.md, or if it is on the ` +
      `cooker-audit known-false-positive list (AES-GCM-sealed secrets, webhook FullName matching, ` +
      `wsHub launch order, SIGTERM nil srv.Close, kubectl sha format). Default isReal=false when unsure. ` +
      `Confirm the severity and give a one-line concrete fixSketch (which file + which existing pattern: ` +
      `internal/retry, internal/validate, internal/idempotency, a new index/migration, a per-call timeout, ` +
      `a Redis-backed store, etc.).\n\n` +
      `Title: ${f.title}\nFile: ${f.file}\nSeverity: ${f.severity}\nEvidence: ${f.evidence}\nTracking: ${f.tracking}`,
      { label: `verify:${d.key}:${f.file}`, phase: 'Verify', schema: VERDICT_SCHEMA },
    ).then(v => ({ ...f, dimension: d.key, verdict: v }))
  )),
)

const confirmed = reviewed.flat().filter(Boolean)
  .filter(f => f.verdict?.isReal && f.verdict?.stillOpen)
  .map(f => ({
    title: f.title, file: f.file, dimension: f.dimension,
    severity: f.verdict.severity || f.severity, tracking: f.tracking,
    evidence: f.evidence, fixSketch: f.verdict.fixSketch, reasoning: f.verdict.reasoning,
  }))

log(`health-sweep: ${confirmed.length} confirmed open finding(s) across ${DIMENSIONS.length} dimensions`)

phase('Plan')

const plan = await agent(
  `Act as cooker-planner. Turn these CONFIRMED, still-open Cooker findings into a prioritized remediation ` +
  `plan — the "method to solve". For EACH finding give: the concrete fix (file + which existing pattern to apply), ` +
  `effort (S/M/L), risk, and the exact verification command (e.g. ` +
  `\`.claude/skills/cooker-improve/check-pkg.sh internal/<pkg>\`). Then give a recommended sequence ` +
  `(quick wins first; call out dependencies and anything that needs an ADR), and map each item to its ` +
  `ADLC path (bug-fix vs improvement vs feature). Keep it dense; paths and commands beat prose.\n\n` +
  `CONFIRMED FINDINGS JSON:\n${JSON.stringify(confirmed, null, 2)}`,
  { label: 'remediation-plan', phase: 'Plan' },
)

return { confirmed, plan }
