// Template: RESEARCH — multi-source research with adversarial fact-checking and cited synthesis.
// Built on the workflow skill patterns: parallel search fan-out with a dedup barrier
// (harness policy H2), a read -> verify pipeline per source (H1, no barrier), and a
// final synthesis over verified claims.
//
// Invoke with: Workflow({ script, args: { question: "...", angles: [...] } })
// input.question — the research question (scope it BEFORE authoring; see the skill, step 1)
// input.angles   — optional array of distinct search angles; falls back to a default set

export const meta = {
  name: 'research-template', // EDIT ME
  description: 'Fan out searches, deep-read sources, adversarially verify each claim, synthesize a cited report', // EDIT ME
  phases: [
    { title: 'Search', detail: 'one searcher per angle' },
    { title: 'Read', detail: 'deep-read + claim extraction per source' },
    { title: 'Verify', detail: 'adversarial fact-check per claim' },
    { title: 'Synthesize', detail: 'cited report from verified claims' },
  ],
}

// Some harnesses deliver args as a JSON-encoded string — normalize before use.
const input = typeof args === 'string' ? JSON.parse(args) : args

// EDIT ME: multi-modal sweep — each angle searches a DIFFERENT way (harness policy H4).
const ANGLES = (input && input.angles) || [
  'primary and authoritative sources: official docs, standards, source data, filings',
  'recent developments and news (note dates)',
  'critical, sceptical, or contrarian takes and known failure modes',
  'quantitative data: benchmarks, studies, datasets with methodology',
]

const SOURCES_SCHEMA = {
  type: 'object',
  properties: {
    sources: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          url: { type: 'string' },
          title: { type: 'string' },
          why: { type: 'string' },
        },
        required: ['url', 'title'],
      },
    },
  },
  required: ['sources'],
}

const CLAIMS_SCHEMA = {
  type: 'object',
  properties: {
    claims: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          claim: { type: 'string' },
          evidence: { type: 'string' },
          sourceUrl: { type: 'string' },
          date: { type: 'string' },
        },
        required: ['claim', 'sourceUrl'],
      },
    },
  },
  required: ['claims'],
}

const VERDICT_SCHEMA = {
  type: 'object',
  properties: {
    supported: { type: 'boolean' },
    confidence: { type: 'number' },
    note: { type: 'string' },
  },
  required: ['supported', 'confidence'],
}

const REPORT_SCHEMA = {
  type: 'object',
  properties: {
    answer: { type: 'string' },
    report: { type: 'string' },
    openQuestions: { type: 'array', items: { type: 'string' } },
  },
  required: ['answer', 'report'],
}

// Phase Search: BARRIER — dedup needs every searcher's results at once (harness policy H2).
const sweeps = await parallel(
  ANGLES.map((a, i) => () =>
    agent(
      `Research question: ${input.question}\nFind high-quality sources via this angle: ${a}. Use web search/fetch and any research MCP tools available. Return candidate sources as raw data — do not deep-read yet.`,
      { label: `search:${i}`, phase: 'Search', schema: SOURCES_SCHEMA },
    ),
  ),
)

// .filter(Boolean): a dead searcher resolves to null (harness policy H5). Dedup by URL in plain JS.
const allSources = sweeps.filter(Boolean).flatMap((s) => s.sources)
const sources = [...new Map(allSources.map((s) => [s.url, s])).values()]
log(`search: ${allSources.length} candidate sources -> ${sources.length} after dedup`)

if (sources.length === 0) {
  return { answer: 'No sources found for the question.', report: '', verifiedClaims: 0, sources: 0 }
}

// Phase Read -> Verify: pipeline per source (no barrier). Each source's claims are verified
// as soon as that source is read; source A can be in Verify while source B is still being read.
const perSource = await pipeline(
  sources,
  (src) =>
    agent(
      `Read this source for the question "${input.question}" and extract its concrete claims with evidence. Fetch the page. Source: ${src.title} — ${src.url}. Return raw data; drop low-quality/undated/circular sources by returning no claims.`,
      { label: `read:${src.url}`, phase: 'Read', schema: CLAIMS_SCHEMA },
    ).then((r) => ({ src, claims: (r && r.claims) || [] })),
  (read) =>
    parallel(
      read.claims.map((c) => () =>
        agent(
          `Try to REFUTE this claim, don't confirm it. Check it against other independent sources and the primary source; look for contradiction or a recency problem. Default supported=false, confidence low, if you cannot stand it up.\nClaim: ${c.claim}\nStated evidence: ${c.evidence || '(none given)'}\nFrom: ${c.sourceUrl}`,
          { label: `verify:${c.claim.slice(0, 40)}`, phase: 'Verify', schema: VERDICT_SCHEMA },
        ).then((v) => ({ ...c, verdict: v })),
      ),
    ),
)

// Keep only corroborated/standable claims. EDIT ME: tune the confidence floor.
const verified = perSource
  .filter(Boolean)
  .flat()
  .filter(Boolean)
  .filter((c) => c.verdict && c.verdict.supported && c.verdict.confidence >= 0.7)
log(`verify: ${verified.length} claims survived adversarial checking`)

if (verified.length === 0) {
  return { answer: 'No claims survived verification; the question needs better sources.', report: '', verifiedClaims: 0, sources: sources.length }
}

// Phase Synthesize: one agent writes the cited report from verified claims only.
const synthesis = await agent(
  `Write a cited report answering: ${input.question}\nUse ONLY these verified claims and cite each inline with its sourceUrl. Lead with a direct answer, then a section per sub-question. Surface any disagreement between sources. Do not add unsourced assertions.\nVerified claims (JSON): ${JSON.stringify(verified)}`,
  { label: 'synthesize', phase: 'Synthesize', schema: REPORT_SCHEMA, effort: 'high' },
)

return {
  answer: synthesis ? synthesis.answer : null,
  report: synthesis ? synthesis.report : null,
  openQuestions: synthesis ? synthesis.openQuestions || [] : [],
  verifiedClaims: verified.length,
  sources: sources.length,
}
