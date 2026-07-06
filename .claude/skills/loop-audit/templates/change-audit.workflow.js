// Template: CHANGE AUDIT — per-area analyze → impact/risk assess → synthesized report.
// A specialization of the workflow skill's PIPELINE pattern (harness policy H1: pipeline is the
// default shape, no barrier between stages). Each area flows Analyze → Assess independently:
// area A can be in Assess while area B is still being analyzed. The ONLY barrier is the final
// synthesis, which is genuinely earned (H2) — the report needs every area's assessment at once.
//
// This template REPORTS impact and risk; it does not hunt for exploitable defects. The security
// dimension is delegated to the loop-review skill (see loop-audit SKILL §6): run that
// skill on the same base...head range and fold its confirmed findings into the report's risk
// section rather than re-deriving them here. Keep the boundary sharp — an audit that turns into a
// bug hunt produces a noisy half-review and a missing report.
//
// Invoke with: Workflow({ script, args: { areas: [...], base: "main", head: "HEAD" } })
// input.areas — array of changed file-groups to audit; each item is one unit of work in the
//               pipeline. Partition the diff BEFORE authoring (SKILL §8). Shape is up to you —
//               a string path-glob, or an object { name, files:[...], note }.
// input.base  — base ref of the change set (e.g. a tag, branch, or SHA).
// input.head  — head ref of the change set.

export const meta = {
  name: 'change-audit', // EDIT ME: kebab-case name for this run
  description: 'Analyze each changed area, assess its impact and risk, then synthesize an audit report', // EDIT ME
  phases: [
    { title: 'Analyze', detail: 'classify + summarize each changed area' },
    { title: 'Assess', detail: 'blast radius, risk, and coverage gaps per area' },
    { title: 'Synthesize', detail: 'assemble the audit report from all assessments' },
  ],
}

// Some harnesses deliver args as a JSON-encoded string — normalize before use.
const input = typeof args === 'string' ? JSON.parse(args) : args

const base = (input && input.base) || 'HEAD~1' // EDIT ME: default base ref if the caller omits one
const head = (input && input.head) || 'HEAD' // EDIT ME: default head ref if the caller omits one
const areas = (input && input.areas) || [] // EDIT ME: the partitioned change set (SKILL §8)

// A tiny label helper so string and object area shapes both log readably.
const areaLabel = (a) => (typeof a === 'string' ? a : a.name || (a.files && a.files[0]) || 'area')

// Stage 1 (Analyze) output — classify each logical change and summarize it (SKILL §2).
const ANALYSIS_SCHEMA = {
  type: 'object',
  properties: {
    // EDIT ME: constrain to your class vocabulary; mixed commits should split into entries upstream.
    classification: { type: 'string', enum: ['feature', 'fix', 'refactor', 'breaking', 'docs', 'chore'] },
    summary: { type: 'string' }, // what changed and why, in the area's own terms
    changedSymbols: { type: 'array', items: { type: 'string' } }, // functions/exports/types touched
  },
  required: ['classification', 'summary', 'changedSymbols'],
}

// Stage 2 (Assess) output — blast radius and risk from what depends on the change (SKILL §3–§5).
const ASSESSMENT_SCHEMA = {
  type: 'object',
  properties: {
    blastRadius: { type: 'string' }, // direct callers, transitive dependents, API/schema/config/wire reach
    riskLevel: { type: 'string', enum: ['low', 'medium', 'high', 'critical'] },
    factors: { type: 'array', items: { type: 'string' } }, // what drives the rating (complexity, reversibility, state/IO)
    coverageGaps: { type: 'array', items: { type: 'string' } }, // changed behavior with no covering test
  },
  required: ['blastRadius', 'riskLevel', 'factors'],
}

// Final report — one synthesized memo plus a rolled-up verdict and follow-ups (SKILL §7).
const REPORT_SCHEMA = {
  type: 'object',
  properties: {
    report: { type: 'string' }, // the assembled audit memo
    riskLevel: { type: 'string', enum: ['low', 'medium', 'high', 'critical'] }, // overall verdict
    followUps: { type: 'array', items: { type: 'string' } }, // e.g. delegate coverage gaps to loop-test
  },
  required: ['report', 'riskLevel'],
}

if (areas.length === 0) {
  // H5: nulls/empties are first-class — return an explicit empty result, don't throw.
  return { report: 'No changed areas supplied; nothing to audit.', riskLevel: 'low', followUps: [], areas: 0 }
}

log(`auditing ${areas.length} area(s) across ${base}...${head}`)

// Pipeline over areas (harness policy H1: pipeline is the default, NO barrier between stages).
// Each area is analyzed, then assessed as soon as ITS analysis completes — area A can be in
// Assess while area B is still in Analyze.
const assessed = await pipeline(
  areas,
  // Stage 1 — Analyze: classify and summarize the area's changes. opts.phase, not global phase() (H9).
  (area) =>
    agent(
      // EDIT ME: the per-area analysis prompt. Give the agent the diff for THIS area only.
      `Audit target: the change set ${base}...${head}.\n` +
        `Analyze ONLY this area and classify its changes (feature/fix/refactor/breaking/docs/chore), ` +
        `summarize what changed and why, and list the changed symbols. Split a mixed change into the ` +
        `single most-load-bearing class. Inspect the diff with git; return raw data.\n` +
        `Area: ${JSON.stringify(area)}`,
      { label: `analyze:${areaLabel(area)}`, phase: 'Analyze', schema: ANALYSIS_SCHEMA },
    ).then((analysis) => ({ area, analysis })),
  // Stage 2 — Assess: trace blast radius and rate risk. Stage callbacks receive (prevResult, area, index).
  (prev) =>
    prev && prev.analysis
      ? agent(
          // EDIT ME: the per-area impact/risk prompt.
          `Assess the impact and risk of this ${prev.analysis.classification} in ${base}...${head}.\n` +
            `Trace the blast radius: direct callers, transitive dependents, public API/exports, and any ` +
            `schema/config/wire/data-migration surface. Rate risk on likelihood × blast radius (not line ` +
            `count); escalate breaking changes, migrations, and irreversible ops. Flag changed behavior ` +
            `with no covering test as a coverage gap — do NOT write the tests here.\n` +
            `Area: ${JSON.stringify(prev.area)}\n` +
            `Analysis: ${JSON.stringify(prev.analysis)}`,
          { label: `assess:${areaLabel(prev.area)}`, phase: 'Assess', schema: ASSESSMENT_SCHEMA },
        ).then((assessment) => ({ ...prev, assessment }))
      : null, // a dead Analyze stage carries through as null — filtered below (H5)
)

// .filter(Boolean): a dead agent or skipped area resolves to null (harness policy H5).
const entries = assessed.filter(Boolean).filter((e) => e.assessment)
log(`assessed ${entries.length}/${areas.length} area(s)`) // no silent caps (H6)

if (entries.length === 0) {
  return { report: `All ${areas.length} area(s) failed to assess; re-scope the change set.`, riskLevel: 'low', followUps: [], areas: areas.length }
}

// Synthesize — one agent assembles the report from every area's assessment. This is the sole
// barrier and it is earned (H2): the report and overall verdict need all assessments at once.
// EDIT ME: if you ran loop-review on this range (SKILL §6), pass its confirmed findings into
// this prompt so the synthesis folds them into the risk section rather than re-deriving them.
const synthesis = await agent(
  `Assemble a change-audit report for ${base}...${head} from these per-area assessments. ` +
    `Lead with a summary and an overall risk verdict, then a per-area entry (class, blast radius, ` +
    `risk + one-line justification, coverage gaps, rollback note for breaking/irreversible changes), ` +
    `and an explicit open-questions/gaps list. This is a REPORT — do not open PRs, edit code, or ` +
    `write tests. Recommend follow-ups (e.g. delegate coverage gaps to the loop-test skill).\n` +
    `Assessments (JSON): ${JSON.stringify(entries)}`,
  { label: 'synthesize', phase: 'Synthesize', schema: REPORT_SCHEMA, effort: 'high' },
)

return {
  report: synthesis ? synthesis.report : null,
  riskLevel: synthesis ? synthesis.riskLevel : null,
  followUps: synthesis ? synthesis.followUps || [] : [],
  areas: entries.length,
}
