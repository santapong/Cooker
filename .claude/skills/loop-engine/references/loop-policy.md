# Loop Engineering Policy

Rules for iteration inside workflow scripts. A loop is justified only when the size of the work is unknown up front; everything else is a work-list. Orchestration shape is governed by the Harness Engineering Policy (`harness-policy.md`).

## L1. Loop-until-dry for unknown-size discovery

When the task is to find "all" of something (bugs, issues, edge cases, sources), keep spawning finder rounds until **K consecutive rounds produce nothing new** (default K=2). Fixed-count loops (`while count < N`) miss the tail; the dry counter is the stop signal, not a quota.

```js
let dry = 0
while (dry < 2) {
  const fresh = /* this round's new items after dedup */
  if (!fresh.length) { dry++; continue }
  dry = 0
  // process fresh items
}
```

## L2. Loop-until-budget must guard `budget.total`

Budget-scaled loops run while headroom remains — but `budget.remaining()` is `Infinity` when no target was set, so an unguarded loop runs straight into the 1000-agent backstop:

```js
while (budget.total && budget.remaining() > FLOOR) { ... }   // correct
while (budget.remaining() > FLOOR) { ... }                   // WRONG: runaway when no target
```

Choose `FLOOR` as the cost of one full round plus verification headroom (e.g. 50k tokens), so the loop never starts a round it can't finish.

## L3. Converge by deduping against everything *seen*

Maintain a `seen` set keyed on a stable identity (e.g. file+line+kind) and dedup each round's findings against it — **not** against the confirmed/accepted list. If you dedup only against confirmed results, judge-rejected findings reappear every round and the loop never goes dry.

```js
const seen = new Set(), confirmed = []
// each round:
const fresh = found.filter(item => !seen.has(key(item)))
fresh.forEach(item => seen.add(key(item)))     // add BEFORE judging
// judge fresh → push survivors to confirmed
```

Dedup is plain JavaScript in the script body — never spend an agent on it.

## L4. Every loop needs a terminal condition — ideally two

At least one of: a dry counter (L1), a budget floor (L2), or a hard round cap. Prefer a primary signal plus a hard cap as backstop (`round < MAX_ROUNDS`). The 1000-agent limit is a runaway backstop, not a design parameter — hitting it means the loop was mis-designed.

## L5. Log every round

Each iteration must `log()` its progress: items found so far, fresh count this round, dry counter or remaining budget. A silent loop is indistinguishable from a hung one, and post-hoc debugging depends on the narration.

## L6. Do not loop over known work

If the items are enumerable up front (files in a diff, channels in a list, modules in a package), that is a single `pipeline(items, ...)` — not a loop. Scout inline first (list the files, scope the diff) to discover the work-list, then pipeline over it. Loop only when discovery itself is the work.

## L7. Keep judgment out of the loop condition

The loop condition tests cheap facts computed in plain JS (fresh count, budget, round number). Never make continuing the loop depend on an agent's opinion ("should I keep going?") — agents are for producing and verifying items, and an agent-driven stop signal makes convergence unfalsifiable.

## L8. Rounds vary, agents don't remember

Each round's agents are fresh — they have no memory of prior rounds. Vary the prompt by round or index (different lens, different starting corner) to avoid re-finding the same items, and pass the `seen` summary into finder prompts when it meaningfully steers them away from covered ground (keep it short; a 500-item list is noise).
