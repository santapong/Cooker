// Template: BUG DIAGNOSIS — parallel hypothesis testing to localize a root cause.
// Built on the workflow skill patterns: an optional hypothesis-generation agent, then a
// parallel fan-out with a BARRIER (harness policy H2 — the pick step needs every
// hypothesis verdict at once), and a final synthesis over the confirmed hypotheses.
//
// The engine is ELIMINATION, not confirmation: each investigator is told to try to
// ELIMINATE its hypothesis, and a hypothesis that survives only because no one tried to
// kill it is not confirmed (harness policy H4 — verification is adversarial). This mirrors
// the reproduce -> localize -> root-cause -> fix method the loop-debug skill drives.
//
// Invoke with: Workflow({ script, args: { symptom, context, hypotheses } })
// input.symptom    — the bug: observed vs expected behaviour, error/stack trace
// input.context    — reproduction steps + evidence already gathered (logs, diffs, env)
// input.hypotheses — optional array of { id, statement, test }; if omitted, an agent
//                    generates them from the symptom + context

export const meta = {
  name: 'bug-diagnosis-template', // EDIT ME
  description: 'Generate candidate root-cause hypotheses, test each in parallel to eliminate or confirm it, then synthesize a minimal fix', // EDIT ME
  phases: [
    { title: 'Hypothesize', detail: 'enumerate candidate root causes' }, // EDIT ME: mirror framework phase names
    { title: 'Investigate', detail: 'one investigator per hypothesis, eliminate or confirm' },
    { title: 'Synthesize', detail: 'root cause + minimal fix + regression test' },
  ],
}

// Some harnesses deliver args as a JSON-encoded string — normalize before use.
const input = typeof args === 'string' ? JSON.parse(args) : args

// EDIT ME: how many hypotheses to generate when the caller supplies none.
const HYPOTHESIS_COUNT = 5

const HYPOTHESES_SCHEMA = {
  type: 'object',
  properties: {
    hypotheses: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          id: { type: 'string' },
          statement: { type: 'string' }, // a falsifiable claim about the root cause
          test: { type: 'string' }, // the cheapest observation that would eliminate it
        },
        required: ['id', 'statement', 'test'],
      },
    },
  },
  required: ['hypotheses'],
}

const VERDICT_SCHEMA = {
  type: 'object',
  properties: {
    id: { type: 'string' },
    verdict: { type: 'string', enum: ['confirmed', 'eliminated', 'inconclusive'] },
    evidence: { type: 'string' }, // the concrete observation (code, log, trace) behind the verdict
    rootCause: { type: 'string' }, // filled only when confirmed
  },
  required: ['id', 'verdict', 'evidence'],
}

const FIX_SCHEMA = {
  type: 'object',
  properties: {
    rootCause: { type: 'string' },
    fix: { type: 'string' }, // the MINIMAL change that addresses the root cause, not the symptom
    regressionTest: { type: 'string' }, // a test that fails before the fix and passes after
  },
  required: ['rootCause', 'fix', 'regressionTest'],
}

// Phase Hypothesize: use the caller's list if given, else generate a diverse set.
// Diversity matters (harness policy H4) — hypotheses that all blame the same layer test nothing.
let hypotheses = input && Array.isArray(input.hypotheses) ? input.hypotheses : null
if (!hypotheses) {
  const gen = await agent(
    `A bug is reported. Enumerate ${HYPOTHESIS_COUNT} DISTINCT, falsifiable root-cause hypotheses spanning different layers (input/validation, state/data, control flow, dependency/config, environment/timing). For each, give the cheapest observation that would ELIMINATE it. Do not investigate yet — just enumerate.\nSymptom: ${input.symptom}\nContext (repro + evidence): ${input.context}`,
    { label: 'hypothesize', phase: 'Hypothesize', schema: HYPOTHESES_SCHEMA },
  )
  hypotheses = (gen && gen.hypotheses) || []
}
log(`hypothesize: ${hypotheses.length} candidate root causes to test`)

if (hypotheses.length === 0) {
  return { rootCause: null, fix: null, note: 'no hypotheses to test — refine the symptom/context', confirmed: 0, eliminated: 0 }
}

// Phase Investigate: BARRIER — the pick step below needs every verdict at once (harness policy H2).
// Each investigator is framed to ELIMINATE its hypothesis; it may only return "confirmed" with
// direct evidence, and defaults to "inconclusive" when it can neither kill nor prove it (H4).
const results = await parallel(
  hypotheses.map((h) => () =>
    agent(
      `Investigate ONE root-cause hypothesis for this bug by trying to ELIMINATE it against the codebase. Read the relevant code and reproduce/trace as needed. Return "eliminated" if evidence contradicts it, "confirmed" ONLY with a direct causal observation, else "inconclusive". Do not confirm on a hunch.\nSymptom: ${input.symptom}\nContext: ${input.context}\nHypothesis ${h.id}: ${h.statement}\nEliminating test: ${h.test}`,
      { label: `investigate:${h.id}`, phase: 'Investigate', schema: VERDICT_SCHEMA },
    ),
  ),
)

// .filter(Boolean): a skipped/dead investigator resolves to null (harness policy H5). Pick in plain JS.
const verdicts = results.filter(Boolean)
const confirmed = verdicts.filter((v) => v.verdict === 'confirmed')
const eliminated = verdicts.filter((v) => v.verdict === 'eliminated')
const inconclusive = verdicts.filter((v) => v.verdict === 'inconclusive')
log(`investigate: ${confirmed.length} confirmed, ${eliminated.length} eliminated, ${inconclusive.length} inconclusive`)

if (confirmed.length === 0) {
  // Nothing survived as a confirmed cause — report the eliminations so the next round narrows.
  return {
    rootCause: null,
    fix: null,
    note: 'no hypothesis confirmed; broaden hypotheses or gather more evidence',
    confirmed: 0,
    eliminated: eliminated.length,
    inconclusive: inconclusive.map((v) => ({ id: v.id, evidence: v.evidence })),
  }
}

// Phase Synthesize: one agent states the root cause and the MINIMAL fix from confirmed evidence only.
const synthesis = await agent(
  `The following hypotheses were CONFIRMED with evidence for this bug. State the single root cause, propose the MINIMAL fix that addresses the cause (not the symptom), and write a regression test that fails before the fix and passes after.\nSymptom: ${input.symptom}\nContext: ${input.context}\nConfirmed (JSON): ${JSON.stringify(confirmed)}`,
  { label: 'synthesize', phase: 'Synthesize', schema: FIX_SCHEMA, effort: 'high' },
)

return {
  rootCause: synthesis ? synthesis.rootCause : null,
  fix: synthesis ? synthesis.fix : null,
  regressionTest: synthesis ? synthesis.regressionTest : null,
  confirmed: confirmed.length,
  eliminated: eliminated.length,
  inconclusive: inconclusive.length,
}
