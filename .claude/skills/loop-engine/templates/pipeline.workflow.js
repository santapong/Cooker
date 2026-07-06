// Template: PIPELINE — known items flowing through independent stages.
// Default choice per harness policy H1. No barrier between stages: item A can be
// in Verify while item B is still in Analyze.
//
// Invoke with: Workflow({ script, args: { items: [...], task: "..." } })
// input.items — the work-list (discover it BEFORE authoring; see loop policy L6)
// input.task  — one-line description used in agent prompts

export const meta = {
  name: 'pipeline-template', // EDIT ME: kebab-case name for this run
  description: 'Analyze each item, then verify each finding, with no barrier between stages', // EDIT ME
  phases: [
    { title: 'Analyze', detail: 'one agent per item' }, // EDIT ME: mirror framework phase names
    { title: 'Verify', detail: 'adversarial check per result' },
  ],
}

// Some harnesses deliver args as a JSON-encoded string — normalize before use.
const input = typeof args === 'string' ? JSON.parse(args) : args

// EDIT ME: schema for what the Analyze stage returns (harness policy H3)
const ANALYSIS_SCHEMA = {
  type: 'object',
  properties: {
    findings: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          title: { type: 'string' },
          detail: { type: 'string' },
          location: { type: 'string' },
        },
        required: ['title', 'detail'],
      },
    },
  },
  required: ['findings'],
}

const VERDICT_SCHEMA = {
  type: 'object',
  properties: {
    isReal: { type: 'boolean' },
    reason: { type: 'string' },
  },
  required: ['isReal', 'reason'],
}

const results = await pipeline(
  input.items,
  // Stage 1: analyze each item. opts.phase (not global phase()) — harness policy H9.
  (item) =>
    agent(
      // EDIT ME: the per-item analysis prompt
      `Task: ${input.task}\nAnalyze this item and return findings as raw data: ${JSON.stringify(item)}`,
      { label: `analyze:${item}`, phase: 'Analyze', schema: ANALYSIS_SCHEMA },
    ),
  // Stage 2: verify each finding as soon as ITS analysis completes (no barrier).
  // Stage callbacks receive (prevResult, originalItem, index).
  (analysis, item) =>
    parallel(
      analysis.findings.map((f) => () =>
        agent(
          // Adversarial framing per harness policy H4.
          `Try to refute this finding from "${item}". Default to isReal=false if uncertain.\nFinding: ${f.title} — ${f.detail}`,
          { label: `verify:${f.title}`, phase: 'Verify', schema: VERDICT_SCHEMA },
        ).then((v) => ({ ...f, item, verdict: v })),
      ),
    ),
)

// Dead agents/items resolve to null — harness policy H5.
const confirmed = results
  .filter(Boolean)
  .flat()
  .filter(Boolean)
  .filter((f) => f.verdict && f.verdict.isReal)

log(`${confirmed.length} confirmed findings across ${input.items.length} items`)
return { confirmed }
