// Template: PARALLEL FAN-OUT + BARRIER — independent finders whose results must be
// merged/deduped across the whole set before the next (expensive) stage.
// The barrier is earned here per harness policy H2: dedup needs ALL finder output at once.
//
// Invoke with: Workflow({ script, args: { task: "..." } })
// input.task — one-line description used in agent prompts

export const meta = {
  name: 'parallel-template', // EDIT ME
  description: 'Multi-lens fan-out, dedup across all results, then verify survivors', // EDIT ME
  phases: [
    { title: 'Find', detail: 'one finder per lens' }, // EDIT ME: mirror framework phase names
    { title: 'Verify', detail: 'adversarial check per deduped finding' },
  ],
}

// Some harnesses deliver args as a JSON-encoded string — normalize before use.
const input = typeof args === 'string' ? JSON.parse(args) : args

// EDIT ME: multi-modal sweep — each finder searches a DIFFERENT way (harness policy H4).
const LENSES = [
  { key: 'correctness', prompt: 'Search for correctness problems' },
  { key: 'security', prompt: 'Search for security problems' },
  { key: 'performance', prompt: 'Search for performance problems' },
]

const FINDINGS_SCHEMA = {
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
        required: ['title', 'detail', 'location'],
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

// BARRIER: wait for all finders, because dedup needs the full result set.
const sweeps = await parallel(
  LENSES.map((lens) => () =>
    agent(`Task: ${input.task}\n${lens.prompt}. Return findings as raw data.`, {
      label: `find:${lens.key}`,
      phase: 'Find',
      schema: FINDINGS_SCHEMA,
    }),
  ),
)

// .filter(Boolean): a skipped/dead finder resolves to null (harness policy H5).
const all = sweeps.filter(Boolean).flatMap((s) => s.findings)

// Dedup in plain JS — never spend an agent on it (loop policy L3).
const key = (f) => `${f.location}::${f.title}`
const deduped = [...new Map(all.map((f) => [key(f), f])).values()]
log(`${all.length} raw findings → ${deduped.length} after dedup`) // no silent caps (H6)

// Early-exit is the other legitimate use of the barrier.
if (deduped.length === 0) {
  return { confirmed: [], note: 'no findings from any lens' }
}

const verified = await parallel(
  deduped.map((f) => () =>
    agent(
      `Try to refute this finding. Default to isReal=false if uncertain.\nFinding: ${f.title} at ${f.location} — ${f.detail}`,
      { label: `verify:${f.title}`, phase: 'Verify', schema: VERDICT_SCHEMA },
    ).then((v) => ({ ...f, verdict: v })),
  ),
)

const confirmed = verified.filter(Boolean).filter((f) => f.verdict.isReal)
log(`${confirmed.length}/${deduped.length} findings survived verification`)
return { confirmed }
