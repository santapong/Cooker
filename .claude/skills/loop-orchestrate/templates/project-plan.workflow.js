// Template: PROJECT PLAN — realize one phase's typed task DAG with per-node model +
// effort routing. This is an ordinary ../../loop-engine script: it obeys every rule in
// ../../loop-engine/SKILL.md step 5 and the harness policy. The PM additions are the TASKS
// routing table, the cast+cost ledger log(), and the per-node model/effort pass-through.
//
// The DAG here is intentionally small and concrete so the two shapes are visible:
//   Scout (parallel fan-out of INDEPENDENT nodes) ─┐
//                                                  ├─▶ Plan (synthesis, needs ALL scouts)
//                                                  ┘        │
//                                          Build (per-item PIPELINE: draft ─▶ verify)
// Edges are dependencies, not a schedule: the parallel()/pipeline() shapes extract the
// parallelism. See ../references/model-routing.md for the model+effort table and the two
// override modifiers, and ../../loop-engine/references/harness-policy.md for H1/H2/H6/H8.
//
// Invoke with: Workflow({ script, args: { task: "...", items: [...] } })
// input.task  — one-line description threaded into every agent prompt
// input.items — the known Build work-list (discover it BEFORE authoring; loop policy L6)

export const meta = {
  name: 'project-plan-construction', // EDIT ME: kebab-case name for this phase's run
  description: 'Scout in parallel, synthesize a batch plan, then draft+verify each item with per-node model routing', // EDIT ME
  phases: [
    // EDIT ME: titles MUST match the opts.phase strings below and mirror the framework phases (H9).
    { title: 'Scout', detail: 'independent inventory + analysis, fanned out' },
    { title: 'Plan', detail: 'one synthesis over the full scout set' },
    { title: 'Build', detail: 'per-item draft then max-effort verify' },
  ],
}

// Some harnesses deliver args as a JSON-encoded string — normalize before use.
const input = typeof args === 'string' ? JSON.parse(args) : args

// ---------------------------------------------------------------------------
// Schemas — every machine-consumed agent() result carries one (harness policy H3).
// ---------------------------------------------------------------------------
const INVENTORY_SCHEMA = {
  type: 'object',
  properties: {
    items: {
      type: 'array',
      items: {
        type: 'object',
        properties: { name: { type: 'string' }, location: { type: 'string' }, note: { type: 'string' } },
        required: ['name'],
      },
    },
  },
  required: ['items'],
}

const PLAN_SCHEMA = {
  type: 'object',
  properties: {
    batches: {
      type: 'array',
      items: {
        type: 'object',
        properties: { name: { type: 'string' }, items: { type: 'array', items: { type: 'string' } } },
        required: ['name', 'items'],
      },
    },
  },
  required: ['batches'],
}

const DRAFT_SCHEMA = {
  type: 'object',
  properties: { summary: { type: 'string' }, diff: { type: 'string' } },
  required: ['summary'],
}

const VERDICT_SCHEMA = {
  type: 'object',
  properties: { ok: { type: 'boolean' }, reason: { type: 'string' } },
  required: ['ok', 'reason'],
}

// ---------------------------------------------------------------------------
// TASKS — the typed DAG for THIS phase, and the single source of truth for routing.
// Each node: { id, label, taskType, phase, model, effort, rationale, prompt, schema }.
//
// Routing rule of thumb (../references/model-routing.md, harness policy H8):
//   • Mechanical / high-fan-out  → set model to a Haiku id, effort low or OMITTED.
//   • Judgment / synthesis        → OMIT model (inherit the session tier), lift effort.
//   • Correctness-critical verify → pin Opus at effort 'max' (a false "all clear" ships the bug).
// model:null means "omit opts.model so the agent inherits the session model" — the default
// for the MAJORITY of nodes. Only deviate when the taskType justifies it. effort:null omits it.
// ---------------------------------------------------------------------------
const TASKS = [
  // --- Scout phase: three INDEPENDENT nodes → run concurrently in a parallel() fan-out.
  {
    id: 'scout-inventory',
    label: 'scout:inventory',
    taskType: 'scout',
    phase: 'Scout',
    // EDIT ME: mechanical enumeration multiplied across the codebase → route DOWN to Haiku.
    // Wide fan-out makes budget, not reasoning, the constraint (routing modifier A, harness policy H6).
    // Haiku is cheap enough that effort can be omitted entirely (routing table: "omit or low").
    model: 'claude-haiku-4-5',
    effort: null,
    rationale: 'mechanical inventory, budget-bound fan-out → Haiku, effort omitted (routing modifier A / H6)',
    prompt: 'Enumerate every call site / config touchpoint relevant to the task. Return raw data only.',
    schema: INVENTORY_SCHEMA,
  },
  {
    id: 'analyze-risk',
    label: 'analyze:risk',
    taskType: 'analyze',
    phase: 'Scout',
    // EDIT ME: real judgment → OMIT model to inherit the session tier (H8), standard-high effort.
    model: null,
    effort: 'high',
    rationale: 'risk analysis is judgment work → inherit session model, high effort (H8)',
    prompt: 'Identify the highest-risk areas the change will touch and why. Return raw data only.',
    schema: INVENTORY_SCHEMA,
  },
  {
    id: 'analyze-arch',
    label: 'analyze:arch',
    taskType: 'analyze',
    phase: 'Scout',
    model: null, // inherit — most nodes look like this (H8)
    effort: 'high',
    rationale: 'architecture read is judgment work → inherit session model, high effort (H8)',
    prompt: 'Map the structural constraints (boundaries, invariants) the change must respect. Return raw data only.',
    schema: INVENTORY_SCHEMA,
  },

  // --- Plan phase: ONE synthesis node. It consumes the FULL scout set, which is what
  // earns the barrier below (harness policy H2). Decomposition gates every Build node, so
  // it is high-error-cost → inherit Opus and lift effort to xhigh (routing modifier B / H8).
  {
    id: 'plan-batches',
    label: 'plan:batches',
    taskType: 'synthesize',
    phase: 'Plan',
    model: null,
    effort: 'xhigh',
    rationale: 'decomposition every later node inherits → inherit session Opus, effort xhigh (modifier B / H8)',
    prompt: 'Group the inventory into dependency-safe batches for the Build phase. Return raw data only.',
    schema: PLAN_SCHEMA,
  },

  // --- Build phase: a per-item PIPELINE (draft → verify), no barrier between stages (H1).
  {
    id: 'draft',
    label: 'draft', // per-item label is set at dispatch (draft:<item>)
    taskType: 'implement',
    phase: 'Build',
    // EDIT ME: production edit. Omitting model inherits the session tier. If the session
    // runs above the implementation tier and you want to route DOWN to save budget, set
    // model: 'claude-sonnet-5' here (../references/model-routing.md, worked example task 2).
    model: null,
    effort: 'high',
    rationale: 'production implementation → inherit session tier, high effort (H8)',
    prompt: 'Implement the change for this item. Return the diff and a one-line summary as raw data.',
    schema: DRAFT_SCHEMA,
  },
  {
    id: 'verify',
    label: 'verify', // per-item label set at dispatch (verify:<item>)
    taskType: 'verify',
    phase: 'Build',
    // EDIT ME: a false "all clear" here ships a broken change to every downstream item, so
    // this is correctness-critical → pin the top of the fleet at max effort. Model is set
    // EXPLICITLY (not inherited) so the verify stays Opus even if the session runs lower —
    // the one place model-routing.md endorses spelling Opus out (modifier B / harness policy H4).
    model: 'claude-opus-4-8',
    effort: 'max',
    rationale: 'false "all clear" ships the bug → pin Opus at max effort (routing modifier B / H4)',
    prompt: 'Try to REFUTE that the draft is correct and complete. Default to ok=false if uncertain. Return raw data only.',
    schema: VERDICT_SCHEMA,
  },
]

// Lookup + opts builder. optsFor() turns a node's routing decision into the agent() opts,
// OMITTING model/effort when null so the agent inherits the session default (harness policy H8).
const byId = (id) => TASKS.find((t) => t.id === id)
function optsFor(n, label) {
  const opts = { label: label || n.label, phase: n.phase, schema: n.schema }
  if (n.model) opts.model = n.model // omit → inherit session model (H8)
  if (n.effort) opts.effort = n.effort // omit → inherit session effort
  return opts
}

// Cast + cost ledger — one line per node BEFORE dispatch, so the routing is legible and no
// coverage is silently narrowed (harness policy H6, no silent caps). 'inherit' = model omitted.
for (const n of TASKS) {
  log(`cast ${n.id} [${n.taskType}] @${n.phase} → model:${n.model || 'inherit'} effort:${n.effort || 'default'} — ${n.rationale}`)
}

// ---------------------------------------------------------------------------
// Scout phase — PARALLEL fan-out of the independent scout/analyze nodes.
// BARRIER earned per harness policy H2: plan-batches (next) needs the FULL scout set to
// decompose. This is NOT "cleaner code" — it is a genuine cross-item dependency.
// ---------------------------------------------------------------------------
const scoutNodes = TASKS.filter((t) => t.phase === 'Scout')
const scoutRuns = await parallel(
  scoutNodes.map((n) => () => agent(`Task: ${input.task}\n${n.prompt}`, optsFor(n))),
)
// A skipped/dead agent resolves to null — filter before consuming (harness policy H5).
const liveScouts = scoutRuns.filter(Boolean)
const inventory = liveScouts.flatMap((s) => s.items || [])
log(`Scout: ${liveScouts.length}/${scoutNodes.length} nodes live, ${inventory.length} inventory items`)

// ---------------------------------------------------------------------------
// Plan phase — single synthesis over the full scout set (consumes the barrier above).
// ---------------------------------------------------------------------------
const planNode = byId('plan-batches')
const plan = await agent(
  `Task: ${input.task}\n${planNode.prompt}\nInventory: ${JSON.stringify(inventory)}`,
  optsFor(planNode),
)
const batches = plan ? plan.batches : []
log(`Plan: ${batches.length} dependency-safe batch(es) over ${inventory.length} items`)

// ---------------------------------------------------------------------------
// Build phase — per-item PIPELINE: draft, then verify as soon as ITS draft lands (no barrier,
// harness policy H1). Stage callbacks receive (prevResult, originalItem, index).
// EDIT ME: this runs over the known work-list input.items (loop policy L6). If you instead
// cap or sample it, log() what was dropped — no silent caps (harness policy H6).
// ---------------------------------------------------------------------------
const draftNode = byId('draft')
const verifyNode = byId('verify')
const built = await pipeline(
  input.items || [],
  // Stage 1: draft the change for each item.
  (item) =>
    agent(
      `Task: ${input.task}\n${draftNode.prompt}\nItem: ${JSON.stringify(item)}`,
      optsFor(draftNode, `draft:${item}`),
    ),
  // Stage 2: adversarially verify each draft (Opus at max effort — routing modifier B).
  (draft, item) =>
    agent(
      `Task: ${input.task}\n${verifyNode.prompt}\nItem: ${JSON.stringify(item)}\nDraft: ${JSON.stringify(draft)}`,
      optsFor(verifyNode, `verify:${item}`),
    ).then((verdict) => ({ item, draft, verdict })),
)

// Dead items resolve to null (H5); keep only drafts that survived verification.
const rows = built.filter(Boolean)
const shipped = rows.filter((r) => r.verdict && r.verdict.ok)
log(`Build: ${shipped.length}/${(input.items || []).length} items passed max-effort verify`)

// Structured summary — the phase deliverable plus the routing ledger for the PM report.
return {
  phase: 'Construction',
  scouts: { live: liveScouts.length, total: scoutNodes.length, inventory: inventory.length },
  batches,
  shipped: shipped.map((r) => r.item),
  rejected: rows.filter((r) => !(r.verdict && r.verdict.ok)).map((r) => r.item),
  ledger: TASKS.map((n) => ({
    id: n.id,
    taskType: n.taskType,
    phase: n.phase,
    model: n.model || 'inherit',
    effort: n.effort || 'default',
    rationale: n.rationale,
  })),
}
