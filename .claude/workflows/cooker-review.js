// cooker-review — review the current Cooker git diff across dimensions,
// adversarially verify each finding, and synthesize a go/no-go.
//
// Run:  Workflow({ name: "cooker-review" })
// Pairs with ADLC Phase 5 (Review). See .claude/skills/workflow/SKILL.md.
//
// Pattern: pipeline (each dimension's findings verify the moment that
// dimension's review completes) + adversarial verify + synthesis.

export const meta = {
  name: 'cooker-review',
  description: 'Review the current git diff across {bugs, layering, security, migration-safety}, adversarially verify each finding, synthesize a go/no-go.',
  whenToUse: 'Before flipping a Cooker PR from draft to ready, or when asked to review the working diff.',
  phases: [
    { title: 'Review' },
    { title: 'Verify' },
    { title: 'Synthesize' },
  ],
}

// Each dimension reviews the SAME diff through a different lens.
const DIMENSIONS = [
  {
    key: 'bugs',
    prompt:
      'Review the current Cooker git diff (`git diff main...HEAD`) for correctness bugs: ' +
      'nil-derefs, race conditions (CI runs `-race`), unhandled errors, off-by-one, ' +
      'goroutine-join races, channel close-twice. Cite file:line for each.',
  },
  {
    key: 'layering',
    prompt:
      'Review the current Cooker git diff for layering violations: business logic in handlers, ' +
      'HTTP types (*gin.Context) leaking into services, `panic` outside startup, and store ' +
      'changes that break memory/postgres parity. Cite file:line for each.',
  },
  {
    key: 'security',
    prompt:
      'Review the current Cooker git diff for security issues: fmt.Sprintf shell-string ' +
      'interpolation fed to a shell, dev defaults reaching production without a Config.Validate ' +
      'gate, raw err.Error() in 5xx response bodies, IDOR on path params, a mutating route ' +
      'missing the right RequireRole. Cite file:line for each.',
  },
  {
    key: 'migration',
    prompt:
      'Review the current Cooker git diff for migration-safety issues: a new handler request ' +
      'field without a matching internal/store/postgres/migrations entry in the same diff, a ' +
      'NOT NULL column added without a DEFAULT (breaks rolling deploys), or a missing .down.sql. ' +
      'Cite file:line for each.',
  },
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
          file:     { type: 'string' },
          severity: { type: 'string', enum: ['critical', 'high', 'medium', 'low'] },
          detail:   { type: 'string' },
        },
        required: ['title', 'file', 'severity', 'detail'],
      },
    },
  },
  required: ['findings'],
}

const VERDICT_SCHEMA = {
  type: 'object',
  properties: {
    isReal:    { type: 'boolean' },
    reasoning: { type: 'string' },
  },
  required: ['isReal', 'reasoning'],
}

phase('Review')

const reviewed = await pipeline(
  DIMENSIONS,
  d => agent(d.prompt, { label: `review:${d.key}`, phase: 'Review', schema: FINDINGS_SCHEMA }),
  (review, d) => parallel((review?.findings ?? []).map(f => () =>
    agent(
      `Adversarially verify this ${d.key} finding by reading the cited Cooker file. ` +
      `Default isReal=false if you cannot confirm it from the code.\n\n` +
      `Title: ${f.title}\nFile: ${f.file}\nSeverity: ${f.severity}\nDetail: ${f.detail}`,
      { label: `verify:${d.key}:${f.file}`, phase: 'Verify', schema: VERDICT_SCHEMA },
    ).then(v => ({ ...f, dimension: d.key, verdict: v }))
  )),
)

const confirmed = reviewed.flat().filter(Boolean).filter(f => f.verdict?.isReal)

phase('Synthesize')

const summary = await agent(
  `Synthesize a Cooker code-review report from these confirmed findings. Group by severity, ` +
  `then by dimension. For each: file:line and a one-line fix sketch. End with an explicit ` +
  `GO / NO-GO recommendation for flipping the PR from draft to ready, and what blocks it.\n\n` +
  JSON.stringify(confirmed, null, 2),
  { label: 'synthesize', phase: 'Synthesize' },
)

log(`cooker-review: ${confirmed.length} confirmed finding(s) across ${DIMENSIONS.length} dimensions`)
return { confirmed, summary }
