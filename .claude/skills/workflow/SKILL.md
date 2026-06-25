---
name: workflow
description: Author and run deterministic multi-agent workflows for Cooker (the Workflow tool). Trigger on "create a workflow", "run a workflow", "fan out agents", "orchestrate this with subagents", "run cooker-review", "run cooker-audit-sweep", or "do a comprehensive/thorough audit". Workflows are OPT-IN — only reach for one when the user explicitly asks for multi-agent orchestration. Bias toward a single agent or a skill for ordinary work.
---

# Cooker — workflows (multi-agent orchestration)

A **workflow** is a JavaScript script that orchestrates many subagents deterministically — loops, barriers, conditionals, fan-out — and returns structured data. Use it when one context can't hold the work, when you need to be *comprehensive* (cover everything), or when you need to be *confident* (adversarially verify before committing to a finding).

Workflows live in `.claude/workflows/<name>.js` and run via the `Workflow` tool. This skill is the "way to create" them. For where workflows sit relative to skills and agents, see [`docs/engineering/harness-engineering.md`](../../../docs/engineering/harness-engineering.md).

## ⚠️ Opt-in only — read this first

A workflow can spawn dozens of agents and burn a lot of tokens. **Only invoke the `Workflow` tool when the user has explicitly opted in.** That means one of:

- The user said "use a workflow", "run a workflow", "fan out agents", "orchestrate with subagents", or named a saved workflow ("run `cooker-review`").
- The user typed the `ultracode` keyword, or ultracode is on for the session.
- A skill/command told you to call `Workflow`.

For any other task — even one that would clearly benefit from parallelism — use a **single `Agent`** call, or briefly tell the user a workflow is available and roughly what it'd cost, and let them ask. Don't infer the opt-in.

## Which primitive? (vs a skill or a single agent)

| Situation | Use |
|---|---|
| Encode a repeatable routine | a **skill** (`cooker-*`) |
| One bounded delegated task, or isolating a big read | a **single `Agent`** |
| "Audit *everything*", "review the *whole* diff", "be *comprehensive/thorough*" | a **workflow** |
| Need findings *verified* before you trust them | a **workflow** (adversarial-verify pattern) |
| Routine on a schedule | a **skill + cron** (see `cooker-weekly`) |

## Anatomy of a workflow script

Every script starts with a **pure-literal** `meta` block, then a body using the provided hooks (`agent`, `pipeline`, `parallel`, `phase`, `log`, plus `args`, `budget`, `workflow`).

```js
export const meta = {
  name: 'my-workflow',                 // matches the file name and the {name:"…"} you invoke
  description: 'One line shown in the permission dialog.',
  whenToUse: 'Optional — shown in the workflow list.',
  phases: [                            // one entry per phase() / opts.phase used
    { title: 'Find' },
    { title: 'Verify' },
  ],
}
// ── body (async context; use await directly) ──
const found = await agent('Find X in the Cooker backend. Cite file:line.', {
  label: 'find:x', phase: 'Find', schema: FINDINGS_SCHEMA,   // schema → returns a validated object
})
```

Key hooks:

- `agent(prompt, opts?)` → the subagent's text, or (with `opts.schema`) a validated object. Returns `null` if it's skipped or dies — `.filter(Boolean)`. `opts`: `label`, `phase`, `schema`, `model`, `effort`, `agentType` (e.g. `'Explore'`, `'cooker-security'`), `isolation: 'worktree'` (only when agents mutate files in parallel).
- `pipeline(items, stage1, stage2, …)` → **the default for multi-stage work.** Each item flows through all stages independently; no barrier between stages. Stage callbacks get `(prevResult, originalItem, index)`.
- `parallel(thunks)` → a **barrier**: awaits all. Only when a stage genuinely needs *all* prior results at once (dedup, total-count early-exit).
- `phase(title)` / `log(msg)` → progress UI. Inside `pipeline`/`parallel` stages, pass `opts.phase` instead of calling `phase()` (avoids racing the global).
- `budget` → `budget.total` (the turn's token target, or `null`), `budget.remaining()`. Guard loops on `budget.total` so they don't run to the agent cap when no target is set.

**Hard constraints (the script won't run otherwise):**

- `meta` must be a **pure literal** — no variables, calls, or interpolation.
- Plain **JavaScript**, not TypeScript — no type annotations / interfaces / generics.
- `Date.now()`, `Math.random()`, and argless `new Date()` **throw** (they'd break resume). Vary labels by index/round; stamp timestamps after the workflow returns.
- Concurrency is capped (~10 agents at once); a single `pipeline`/`parallel` takes ≤ 4096 items.
- **Default to `pipeline`.** Use a `parallel` barrier only when a stage needs every prior result together.

## The patterns (compose freely)

- **Pipeline (default).** Each finding verifies the moment its review completes — no wasted wall-clock waiting on the slowest reviewer.
- **Adversarial verify.** For each finding, spawn skeptics prompted to *refute* it; keep it only if it survives. Prevents plausible-but-wrong findings. Default the verdict to "not real" when uncertain.
- **Loop-until-dry.** For unknown-size discovery, keep sweeping until *K* consecutive rounds find nothing new. Dedup against a `seen` set (not against the confirmed list, or rejected findings reappear forever).
- **Multi-modal sweep.** Parallel finders each searching a *different way* (by-comment, by-state, by-I/O, by-injection) — each blind to the others.
- **Judge panel.** Generate N independent attempts from different angles, score with parallel judges, synthesize from the winner.
- **No silent caps.** If you bound coverage (top-N, no-retry), `log()` what was dropped.

Scale to the ask: "find any bugs" → a few finders, single-vote verify. "Thoroughly audit this" → a larger pool, 3–5-vote adversarial verify, a synthesis stage.

## Cooker's saved workflows

| Workflow | What it does | Run it |
|---|---|---|
| [`cooker-review`](../../workflows/cooker-review.js) | Reviews the current git diff across {bugs, layering, security, migration-safety}, adversarially verifies each finding, synthesizes a go/no-go. Pairs with ADLC Phase 5. | `Workflow({ name: "cooker-review" })` |
| [`cooker-audit-sweep`](../../workflows/cooker-audit-sweep.js) | Multi-modal finder fan-out over the codebase (mirrors `cooker-audit`'s anti-patterns), dedup + adversarial verify, loop until two dry rounds. Pairs with ADLC Phase 10. EXPENSIVE. | `Workflow({ name: "cooker-audit-sweep" })` |

Read those two before writing a new one — they're the reference implementations of the pipeline-verify and loop-until-dry patterns.

## Authoring a new workflow

1. **Scaffold:** `.claude/skills/workflow/new-workflow.sh <slug>` writes `.claude/workflows/<slug>.js` with the `meta` block and a pipeline skeleton, and prints the path.
2. **Edit** the dimensions/finders, the schemas, and the synthesis stage. Keep each agent prompt Cooker-specific and demand `file:line` citations.
3. **Run it inline first** to iterate: invoke `Workflow` with the `script` inline (or `scriptPath`) before committing. Every invocation persists the script and returns a `scriptPath` + `runId` — edit that file and re-invoke with `{scriptPath}` to iterate; resume an interrupted run with `{scriptPath, resumeFromRunId}`.
4. **Save** the finished script at `.claude/workflows/<slug>.js` so it's invokable by `{name: "<slug>"}`, and add a row to the table above.
5. **Schedule** (optional): kick a saved workflow from a GitHub Action on a cron, the way `.github/workflows/cooker-weekly.yml` kicks the `cooker-weekly` skill.

## Anti-patterns to refuse

- **Invoking `Workflow` without an explicit opt-in.** This is the cardinal rule. A single `Agent` is almost always the right call for ordinary work.
- **A `parallel` barrier where a `pipeline` fits.** If you `parallel(...)` → transform → `parallel(...)` and the middle transform has no cross-item dependency, it should be one `pipeline`.
- **Deduping a loop-until-dry against the confirmed list.** Use a `seen` set, or judge-rejected findings reappear every round and it never converges.
- **Trusting an unverified finding.** Run the adversarial-verify pass before reporting; default uncertain verdicts to "not real".
- **`isolation: 'worktree'` by default.** It's expensive (~200–500 ms + disk per agent). Use it only when agents mutate files in parallel.
- **Putting `Date.now()`/`Math.random()` in a script.** They throw. Vary by index; stamp time after.

## Checklist before declaring a workflow done

- [ ] The user explicitly opted into multi-agent orchestration.
- [ ] `meta` is a pure literal; `meta.phases` titles match the `phase()` / `opts.phase` strings used.
- [ ] Multi-stage work uses `pipeline`; any `parallel` barrier is justified by a real cross-item dependency.
- [ ] Findings go through an adversarial-verify pass; uncertain verdicts default to "not real".
- [ ] Loop-until-dry (if used) dedups against `seen`, not the confirmed list.
- [ ] No `Date.now()`/`Math.random()`; labels vary by index/round.
- [ ] Saved at `.claude/workflows/<slug>.js` and listed in the table above (if it's reusable).
