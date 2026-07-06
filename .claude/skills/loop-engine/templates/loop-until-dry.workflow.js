// Template: LOOP-UNTIL-DRY — unknown-size discovery ("find ALL the X").
// Keeps spawning finder rounds until DRY_LIMIT consecutive rounds surface nothing
// new (loop policy L1), with a hard round cap as backstop (L4).
//
// Invoke with: Workflow({ script, args: { task: "..." } })
// input.task — one-line description used in agent prompts

export const meta = {
  name: 'loop-until-dry-template', // EDIT ME
  description: 'Discovery loop: find until K consecutive dry rounds, judging each fresh item', // EDIT ME
  phases: [
    { title: 'Find', detail: 'diverse finder rounds' },
    { title: 'Judge', detail: 'diverse-lens majority vote per fresh item' },
  ],
}

// Some harnesses deliver args as a JSON-encoded string — normalize before use.
const input = typeof args === 'string' ? JSON.parse(args) : args

const DRY_LIMIT = 2 // rounds with nothing new before stopping (loop policy L1)
const MAX_ROUNDS = 10 // hard backstop (loop policy L4)

// EDIT ME: finder angles — rounds vary, agents don't remember (loop policy L8)
const ANGLES = [
  'start from the entry points',
  'start from the data model',
  'start from the edge cases and error paths',
]

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
        required: ['title', 'detail', 'location'],
      },
    },
  },
  required: ['items'],
}

const VERDICT_SCHEMA = {
  type: 'object',
  properties: { real: { type: 'boolean' }, reason: { type: 'string' } },
  required: ['real'],
}

const key = (b) => `${b.location}::${b.title}`
const seen = new Set() // dedup vs everything SEEN, not confirmed (loop policy L3)
const confirmed = []
let dry = 0

for (let round = 0; round < MAX_ROUNDS && dry < DRY_LIMIT; round++) {
  const angle = ANGLES[round % ANGLES.length]
  const sweeps = await parallel(
    ANGLES.map((a, i) => () =>
      agent(
        // EDIT ME: the discovery prompt
        `Task: ${input.task}\nFind items, approach: ${a}. Round ${round + 1}. Return raw data.`,
        { label: `find:r${round + 1}:${i}`, phase: 'Find', schema: ITEMS_SCHEMA },
      ),
    ),
  )

  const found = sweeps.filter(Boolean).flatMap((s) => s.items)
  const fresh = found.filter((b) => !seen.has(key(b)))
  log(`round ${round + 1} (${angle}): ${found.length} found, ${fresh.length} fresh, dry=${dry}`)

  if (!fresh.length) {
    dry++
    continue
  }
  dry = 0
  fresh.forEach((b) => seen.add(key(b))) // add BEFORE judging (loop policy L3)

  // Diverse-lens majority vote per fresh item (harness policy H4).
  const judged = await parallel(
    fresh.map((b) => () =>
      parallel(
        ['correctness', 'reproducibility', 'impact'].map((lens) => () =>
          agent(
            `Judge via the ${lens} lens — is this real and worth reporting? Default real=false if uncertain.\nItem: ${b.title} at ${b.location} — ${b.detail}`,
            { label: `judge:${lens}:${b.title}`, phase: 'Judge', schema: VERDICT_SCHEMA },
          ),
        ),
      ).then((votes) => ({
        item: b,
        real: votes.filter(Boolean).filter((v) => v.real).length >= 2,
      })),
    ),
  )

  confirmed.push(...judged.filter(Boolean).filter((j) => j.real).map((j) => j.item))
  log(`round ${round + 1}: ${confirmed.length} confirmed so far`)
}

log(`done: ${seen.size} seen, ${confirmed.length} confirmed`)
return { confirmed, totalSeen: seen.size }
