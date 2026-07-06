// Template: PRIOR-ART SEARCH — find existing frameworks/libraries/services/standards
// that already solve a need, evaluate them, and recommend reuse vs. build.
// Built on the workflow skill patterns: parallel discovery fan-out with a dedup barrier
// (harness policy H2), per-candidate evaluation (H1 pipeline / parallel), then a decisive
// build-vs-buy synthesis. Pairs with the loop-research skill for source-verified claims.
//
// Invoke with: Workflow({ script, args: { need: "...", constraints: "...", sources: [...] } })
// input.need        — the capability required, stated solution-neutrally (see SKILL.md step 1)
// input.constraints — hard constraints (language/runtime, license, deployment, scale)
// input.sources     — optional array of source-lenses to search; falls back to a default set

export const meta = {
  name: 'prior-art-search-template', // EDIT ME
  description: 'Search sources for existing solutions, evaluate each, and recommend reuse vs. adapt vs. build', // EDIT ME
  phases: [
    { title: 'Discover', detail: 'one searcher per source lens' },
    { title: 'Evaluate', detail: 'score each candidate against the need' },
    { title: 'Decide', detail: 'build-vs-buy recommendation' },
  ],
}

// Some harnesses deliver args as a JSON-encoded string — normalize before use.
const input = typeof args === 'string' ? JSON.parse(args) : args

// EDIT ME: search the boring options first (see references/where-to-look.md). Each lens is a
// DIFFERENT place to look, so the sweep isn't a monoculture (harness policy H4).
const SOURCES = (input && input.sources) || [
  'the language standard library and the platform/runtime already in use',
  'ecosystem package registries (npm, PyPI, crates.io, Maven, Go modules, etc.)',
  'managed / cloud services that provide the capability',
  'relevant standards and specifications (IETF/W3C/ISO/OpenAPI, file formats, protocols)',
  'existing internal / monorepo code',
]

const CANDIDATES_SCHEMA = {
  type: 'object',
  properties: {
    candidates: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          name: { type: 'string' },
          kind: { type: 'string', enum: ['stdlib', 'library', 'framework', 'service', 'standard', 'internal'] },
          url: { type: 'string' },
          whatItDoes: { type: 'string' },
        },
        required: ['name', 'kind', 'whatItDoes'],
      },
    },
  },
  required: ['candidates'],
}

const EVAL_SCHEMA = {
  type: 'object',
  properties: {
    name: { type: 'string' },
    fit: { type: 'number' }, // 0..1 against the MUST-haves
    maturity: { type: 'string' },
    license: { type: 'string' },
    security: { type: 'string' },
    lockIn: { type: 'string' },
    tco: { type: 'string' },
    verdict: { type: 'string', enum: ['reuse', 'adapt', 'reject'] },
    notes: { type: 'string' },
  },
  required: ['name', 'fit', 'verdict'],
}

const DECISION_SCHEMA = {
  type: 'object',
  properties: {
    recommendation: { type: 'string', enum: ['reuse', 'adapt', 'build'] },
    pick: { type: 'string' },
    rationale: { type: 'string' },
    counterArgument: { type: 'string' },
    runnerUp: { type: 'string' },
  },
  required: ['recommendation', 'rationale'],
}

// Phase Discover: BARRIER — dedup needs every searcher's candidates at once (harness policy H2).
const sweeps = await parallel(
  SOURCES.map((src, i) => () =>
    agent(
      `Need (state solution-neutrally): ${input.need}\nConstraints: ${input.constraints || '(none given)'}\nSearch THIS source for existing solutions that already meet the need: ${src}. Use web search/fetch and any research tools available. Return real candidates as raw data — do not evaluate yet.`,
      { label: `discover:${i}`, phase: 'Discover', schema: CANDIDATES_SCHEMA },
    ),
  ),
)

// .filter(Boolean): a dead searcher resolves to null (harness policy H5). Dedup by lowercased name.
const allCandidates = sweeps.filter(Boolean).flatMap((s) => s.candidates)
const candidates = [...new Map(allCandidates.map((c) => [c.name.toLowerCase(), c])).values()]
log(`discover: ${allCandidates.length} candidates -> ${candidates.length} after dedup`)

// Early-exit: nothing exists -> building is justified (SKILL.md step 4). This is the other
// legitimate use of the barrier.
if (candidates.length === 0) {
  return {
    recommendation: 'build',
    rationale: 'No existing solution found across the searched sources that meets the need.',
    candidates: 0,
  }
}

// Phase Evaluate: score each candidate against the need in parallel.
const evaluations = await parallel(
  candidates.map((c) => () =>
    agent(
      `Evaluate this candidate against the need honestly (fit to MUST-haves, maturity, license, security/supply-chain, lock-in, TCO). Do not reject over a nice-to-have — prefer adapt. Verify maturity/security claims against primary sources.\nNeed: ${input.need}\nConstraints: ${input.constraints || '(none given)'}\nCandidate: ${c.name} (${c.kind}) — ${c.whatItDoes} ${c.url || ''}`,
      { label: `evaluate:${c.name}`, phase: 'Evaluate', schema: EVAL_SCHEMA },
    ),
  ),
)

const scored = evaluations.filter(Boolean)
const viable = scored.filter((e) => e.verdict !== 'reject')
log(`evaluate: ${scored.length} scored, ${viable.length} viable (reuse/adapt)`)

// Phase Decide: one agent makes the decisive build-vs-buy call from the evaluations.
const decision = await agent(
  `Make the build-vs-buy decision for the need: ${input.need}\nDefault to reuse; the ladder is reuse -> adapt -> build, and building must be earned (no viable candidate, core differentiation, unacceptable license/security/lock-in, or trivial-to-build). Pick decisively, name the strongest counter-argument, and give the runner-up.\nEvaluations (JSON): ${JSON.stringify(scored)}`,
  { label: 'decide', phase: 'Decide', schema: DECISION_SCHEMA, effort: 'high' },
)

return {
  recommendation: decision ? decision.recommendation : null,
  pick: decision ? decision.pick : null,
  rationale: decision ? decision.rationale : null,
  counterArgument: decision ? decision.counterArgument : null,
  runnerUp: decision ? decision.runnerUp : null,
  candidatesConsidered: candidates.length,
  viable: viable.length,
}
