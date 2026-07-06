// Template: LOOP-UNTIL-BUDGET — scale depth to the user's token target ("+500k").
// Runs rounds while headroom remains. The budget.total && guard is MANDATORY:
// without a target, budget.remaining() is Infinity and an unguarded loop runs
// straight into the 1000-agent backstop (loop policy L2).
//
// Invoke with: Workflow({ script, args: { task: "..." } })
// input.task — one-line description used in agent prompts

export const meta = {
  name: 'loop-until-budget-template', // EDIT ME
  description: 'Budget-scaled discovery: keep running rounds while token headroom remains', // EDIT ME
  phases: [{ title: 'Find', detail: 'one round per budget window' }],
}

// Some harnesses deliver args as a JSON-encoded string — normalize before use.
const input = typeof args === 'string' ? JSON.parse(args) : args

// EDIT ME: cost of one full round plus verification headroom (loop policy L2)
const FLOOR = 50_000
const MAX_ROUNDS = 20 // hard backstop (loop policy L4)

const ITEMS_SCHEMA = {
  type: 'object',
  properties: {
    items: {
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
  required: ['items'],
}

const key = (b) => `${b.location}::${b.title}`
const seen = new Set()
const results = []
let round = 0

if (!budget.total) {
  // No token target set: do ONE bounded round instead of looping (loop policy L2).
  log('no budget target set — running a single round')
}

do {
  round++
  const sweep = await agent(
    // EDIT ME: vary the prompt by round — agents don't remember prior rounds (loop policy L8)
    `Task: ${input.task}\nRound ${round}: find items not yet covered. Return raw data.`,
    { label: `find:r${round}`, phase: 'Find', schema: ITEMS_SCHEMA },
  )

  const fresh = (sweep ? sweep.items : []).filter((b) => !seen.has(key(b)))
  fresh.forEach((b) => seen.add(key(b)))
  results.push(...fresh)

  log(
    `round ${round}: +${fresh.length} fresh (${results.length} total), ` +
      (budget.total ? `${Math.round(budget.remaining() / 1000)}k tokens remaining` : 'no budget target'),
  )
} while (budget.total && budget.remaining() > FLOOR && round < MAX_ROUNDS)

log(`done after ${round} round(s): ${results.length} items`)
return { items: results, rounds: round }
