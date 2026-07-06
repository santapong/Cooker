// Template: DOC-GENERATION — document many modules/areas at once.
// Built on the workflow skill's pipeline pattern (harness policy H1, pipeline-default):
// each area flows through extract -> draft -> verify-accuracy with NO barrier between
// stages — area A can be in Verify while area B is still being read. The verify stage
// embodies the loop-docs ACCURACY rule (SKILL.md §5): a drafted doc that cannot be
// stood up against the source is aspirational, and this stage is what stops the fan-out
// from shipping it.
//
// Invoke with: Workflow({ script, args: { areas: [...], docType: "reference" } })
// input.areas   — modules/paths to document (discover the work-list BEFORE authoring; see loop policy L6)
// input.docType — Diataxis/artifact type hint the drafters target (reference | how-to | readme | ...)

export const meta = {
  name: 'doc-generation-template', // EDIT ME: kebab-case name for this run
  description: 'Extract intent per module, draft docs in repo conventions, verify every claim against the source', // EDIT ME
  phases: [
    { title: 'Extract', detail: 'read code, pull public surface per area' },
    { title: 'Draft', detail: 'write the doc per area in repo conventions' },
    { title: 'Verify', detail: 'adversarial accuracy check per drafted doc' },
  ],
}

// Some harnesses deliver args as a JSON-encoded string — normalize before use.
const input = typeof args === 'string' ? JSON.parse(args) : args

// EDIT ME: doc type the drafters target — see loop-docs SKILL.md §1 (Diataxis + artifact types).
const DOC_TYPE = (input && input.docType) || 'reference'

// EDIT ME: schema for the verified contract the Extract stage pulls from the source (harness policy H3).
// Names/comments are hints to verify, not facts to copy (SKILL.md §5) — extract from the definitions.
const INTENT_SCHEMA = {
  type: 'object',
  properties: {
    purpose: { type: 'string' }, // what the module is for, in one or two sentences
    publicApi: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          symbol: { type: 'string' }, // exported name
          signature: { type: 'string' }, // params + return type, read off the definition
          summary: { type: 'string' }, // what it does
          errors: { type: 'string' }, // thrown/returned error conditions
        },
        required: ['symbol', 'signature'],
      },
    },
    examples: { type: 'array', items: { type: 'string' } }, // runnable usage snippets
  },
  required: ['purpose', 'publicApi'],
}

// EDIT ME: the Draft stage writes the doc file itself and returns only where it landed.
const DRAFT_SCHEMA = {
  type: 'object',
  properties: {
    path: { type: 'string' }, // absolute path of the doc the agent wrote
    doc: { type: 'string' }, // title/type it produced, for the run log
  },
  required: ['path'],
}

const ACCURACY_SCHEMA = {
  type: 'object',
  properties: {
    accurate: { type: 'boolean' },
    corrections: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          claim: { type: 'string' }, // the drafted sentence that is wrong or unconfirmed
          fix: { type: 'string' }, // what the source actually says
        },
        required: ['claim', 'fix'],
      },
    },
  },
  required: ['accurate'],
}

if (!input || !Array.isArray(input.areas) || input.areas.length === 0) {
  return { documented: [], note: 'No areas supplied — pass input.areas as a non-empty array of modules/paths.' }
}

// Pipeline per area, NO barrier (harness policy H1). Stage callbacks receive
// (prevResult, originalArea, index); thread data forward by carrying it in the return value.
const results = await pipeline(
  input.areas,
  // Stage 1 — Extract: read the source and pull the verified public surface (SKILL.md §5).
  (area) =>
    agent(
      // EDIT ME: the per-area extraction prompt.
      `Read the code for this area and extract its intent as raw data. Read the actual definitions — treat names and comments as hints to verify, not facts to copy. Return the purpose, the public API (each exported symbol's real signature, summary, and error conditions), and runnable usage examples.\nArea: ${JSON.stringify(area)}`,
      { label: `extract:${area}`, phase: 'Extract', schema: INTENT_SCHEMA },
    ).then((intent) => ({ area, intent })),
  // Stage 2 — Draft: turn extracted intent into prose of the chosen type, in repo conventions.
  // The agent uses Write to create the doc file and returns only its path.
  (prev) =>
    agent(
      // EDIT ME: point the drafter at where docs live and the repo's format/tone/tooling (SKILL.md §3).
      `Write a "${DOC_TYPE}" doc for this area, matching the repo's existing doc conventions (location, format, heading depth, voice). Lead with a working example, use active voice, and do NOT hand-copy facts the code already states. Use the Write tool to create the file, then return its absolute path.\nArea: ${JSON.stringify(prev.area)}\nExtracted intent (JSON): ${JSON.stringify(prev.intent)}`,
      { label: `draft:${prev.area}`, phase: 'Draft', schema: DRAFT_SCHEMA },
    ).then((draft) => ({ ...prev, draft })),
  // Stage 3 — Verify: adversarial accuracy check (harness policy H4). This stage IS the
  // SKILL.md §5 accuracy rule — the checker re-reads the SOURCE and the drafted doc and
  // defaults to accurate=false on any claim it cannot confirm against the code.
  (prev) =>
    agent(
      `Try to REFUTE this doc, don't rubber-stamp it. Re-read the source for the area, then read the drafted doc, and check every claim — signatures, return types, error conditions, defaults, example commands — against the actual code. Default accurate=false and list a correction for any claim you cannot confirm or that describes intended rather than provable behavior.\nArea: ${JSON.stringify(prev.area)}\nDrafted doc path: ${prev.draft && prev.draft.path}`,
      { label: `verify:${prev.area}`, phase: 'Verify', schema: ACCURACY_SCHEMA },
    ).then((accuracy) => ({ ...prev, accuracy })),
)

// Dead agents/areas resolve to null — harness policy H5. Split accurate docs from those
// that still carry unconfirmed claims so the caller knows which need a correction pass.
const documented = results.filter(Boolean)
const accurate = documented.filter((r) => r.accuracy && r.accuracy.accurate)
const needsFixes = documented.filter((r) => !(r.accuracy && r.accuracy.accurate))

log(`docs: ${documented.length}/${input.areas.length} areas drafted — ${accurate.length} verified accurate, ${needsFixes.length} need corrections`)

return {
  accurate: accurate.map((r) => ({ area: r.area, path: r.draft && r.draft.path })),
  needsFixes: needsFixes.map((r) => ({
    area: r.area,
    path: r.draft && r.draft.path,
    corrections: (r.accuracy && r.accuracy.corrections) || [],
  })),
}
