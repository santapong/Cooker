---
name: loop-orchestrate
description: Plan and orchestrate multi-agent project work: decompose a project into a task DAG, choose pipeline or parallel workflow shapes, and assign the right Claude model and effort tier to each task (right model for the right job). Use when the user asks to manage or orchestrate a project, break a large task into subtasks across agents, decide which model to use for which job, or drive a multi-phase build, audit, or migration at scale.
argument-hint: <project> [--budget <tokens>] [--dry-run]
---

# Orchestrating Projects

You are the project manager for a multi-agent job. This skill is a **planning layer on top of the sibling `loop-engine` skill (`../loop-engine`)** — it does not introduce a new execution engine. The Workflow tool, the harness and loop policies, the JS templates, and the AIDLC framework are all **unchanged**; you reuse them exactly as `../loop-engine/SKILL.md` prescribes. Your job is to decide *what work exists, in what order, and who runs it* — then hand each phase to the workflow engine to execute.

The PM adds four things on top of a raw workflow run:

1. A typed **task DAG** — the project decomposed into dependency-ordered nodes.
2. A per-node **orchestration-shape** decision, made under the unchanged harness/loop rules.
3. A per-node **model + effort tier** assignment — the right model for the right job.
4. A **cast + cost ledger** and a completeness critic layered onto the normal workflow reporting.

## Execution flow

Follow these steps in order.

### 1. Parse the project

From the skill args, extract:

- **project** — everything that is not a flag: the goal to orchestrate. If empty, ask the user what project to run.
- **`--budget <tokens>`** — a total token ceiling for the whole project (e.g. `--budget 2000000`). Splits across phases in the ledger (step 8). Omit for no ceiling.
- **`--dry-run`** — if present, produce the plan (DAG + ledger + first-phase script) and show it to the user, but do NOT execute.

### 2. Load the governing documents (unchanged)

Read, and treat as read-only law:

1. `../loop-engine/references/harness-policy.md` — orchestration-shape rules (H1–H12).
2. `../loop-engine/references/loop-policy.md` — iteration rules (L1–L8).
3. `../loop-engine/frameworks/AIDLC.md` — the default lifecycle framework (Inception → Construction → Operation, with human gates).

Then read this skill's own planning references:

4. `references/task-decomposition.md` — how to build the typed task DAG.
5. `references/model-routing.md` — the "right model for the right job" table (model + effort tier per task type).

If the user named a different framework, load `../loop-engine/frameworks/<name>.md` instead of AIDLC — the PM layer is framework-agnostic.

### 3. Decompose the project into a typed task DAG

Per `references/task-decomposition.md`, break the project into **nodes**. Each node is a plain object:

```
{
  id,          // stable kebab-case identifier, unique in the DAG
  description, // one line: what this task produces
  taskType,    // scout | analyze | implement | verify | judge | synthesize | critic | doc
  dependsOn,   // [] of node ids that must complete first — this is the DAG edge set
  fanOut,      // 1 for a single agent, or the item-count / "unknown" for a sweep
  model,       // assigned in step 5 (leave null here)
  effort,      // assigned in step 5 (leave null here)
  isolation,   // 'none' by default; 'worktree' only for concurrent file mutation (H7)
  schema,      // JSON schema for machine-consumed output (H3), or null for terminal prose
  phase        // the framework phase this node belongs to (must match meta.phases)
}
```

Rules:

- **Edges are dependencies, not schedule.** `dependsOn` records what must finish first; the workflow engine (via `pipeline()`) extracts the parallelism. Don't serialize nodes that don't actually depend on each other.
- **A fan-out is one node, not N.** A "verify each finding" sweep is a single node with `fanOut` = the finding count (or `"unknown"` for discovery); it becomes one `parallel()`/loop inside a stage.
- **Group nodes by `phase`.** Every node carries the framework phase it belongs to; the phase set becomes `meta.phases` (H9).

### 4. Choose the orchestration shape per node (unchanged rules)

For each node, pick the shape using the **unchanged** harness/loop policy — do not invent new rules:

- **Default to `pipeline()`** (H1). A chain of dependent nodes with a known work-list is one pipeline, no barriers.
- **Earn every barrier** (H2). Use a `parallel()` barrier before a node only when it needs cross-item context from all of its dependencies (dedup/merge, zero-count early-exit, "compare against the other findings"). "Cleaner code" or "I need to flatten first" is not a barrier.
- **Loop only for unknown size** (L1/L6). A node whose `fanOut` is `"unknown"` (find *all* of something) is loop-until-dry; a `--budget`-scaled depth sweep is loop-until-budget with the `budget.total &&` guard (L2). A known work-list is a pipeline, never a loop.
- **Verification scales to the ask** (H4): single-vote for "any bugs", 3–5-vote adversarial or perspective-diverse for "thorough audit", judge panel for wide solution spaces.

### 5. Assign a model + effort tier per node

This is the PM's signature move: **the right model for the right job.** Consult `references/model-routing.md` and set `model` and `effort` on every node.

- The harness default is to **omit `model`** and inherit the session model (H8) — that remains the correct choice for the majority of nodes. Only assign a model when the node's `taskType` justifies deviating.
- The routing table maps `taskType` → tier. In short: mechanical/scout/doc work → cheapest capable model at `effort: 'low'`; core analysis/implementation → mid tier at `effort: 'high'`; the hardest reasoning, adversarial judging, and long-horizon synthesis → top tier at `effort: 'xhigh'`/`'max'`. See the table for the exact model IDs and the escape hatches.
- Record a one-line **rationale** per node for the ledger (step 8) — why this tier, not a cheaper one. A node you can't justify above the session default should stay at the default.

### 6. Compose with AIDLC — default cast + human gates

Map the DAG onto the framework's phases and give each phase a **default cast** — the set of `taskType`s that phase expects (per `AIDLC.md`): Inception is scouts + a synthesizer; Construction is implement → verify chains plus an adversarial review sweep; Operation is a verification loop + doc/critic nodes. A node whose `taskType` doesn't fit its phase's cast is a decomposition smell — revisit step 3.

**Honor the human gates.** AIDLC ends each phase at a gate (H11). At each gate the PM stops, presents the phase deliverable, and **re-plans and re-budgets the next phase** against what actually happened — the DAG downstream of a gate is provisional until the gate is passed. Author one workflow per gated phase (step 7), not one monolith for the whole project.

### 7. Author the plan

Start from `templates/project-plan.workflow.js` and fill its `EDIT ME` slots for the **current phase's** sub-DAG. The template is an ordinary `../loop-engine` script — it obeys every rule in `../loop-engine/SKILL.md` step 5 (pure-literal `meta` first, plain JS, no `Date.now()`/`Math.random()`, `args`-parameterized, `.filter(Boolean)` on fan-outs, `schema` on every consumed `agent()`, `log()` progress). The PM additions the template carries:

- Per-node `model` / `effort` / `isolation` passed through to each `agent()` call from the node's assignment (step 5).
- A `log()` line per node emitting its ledger row (model, effort, est tokens, running spend vs budget).
- `meta.phases` mirroring the framework phase names for the nodes in this workflow.

If `--dry-run`, print this script plus the DAG and ledger, and stop.

### 8. Execute — hand to the workflow engine

Call the **Workflow tool** with the current phase's script inline and the DAG parameters as `args` (never a JSON-encoded string). Note the returned `scriptPath` and `runId`; to iterate a phase, edit the persisted file and re-invoke with `{scriptPath, resumeFromRunId}`. Between gated phases, author the next phase as a fresh Workflow invocation after the user approves — the PM session stays in the loop.

### 9. Report — journal + cast/cost ledger + completeness critic

Reuse the normal workflow reporting and add the PM layer on top:

- **Reuse `meta.phases` + `log()` + the run journal.** Relay the structured result in prose; if a result looks wrong, read `<transcriptDir>/journal.jsonl` before diagnosing (it records each agent's actual return value).
- **Emit the cast + cost ledger.** One row per executed node: `id`, `taskType`, `model`, `effort`, estimated tokens, **rationale**, and **running spend vs `--budget`**. This is the PM's accountability artifact — it shows where the token budget went and why each node ran at its tier. Flag any node whose actual spend materially exceeded its estimate.
- **Run a completeness critic** (H12). End the project (or each comprehensive phase) with a critic node asking "what's missing — a node not run, a dependency unverified, a phase skipped?" Its findings become the next round of work or are reported as known gaps.
- **Present the gate deliverable.** If a framework gate was reached, show the phase's deliverable and the re-plan/re-budget for the next phase, and ask the user to approve before authoring it.

## What this layer does NOT change

- It does **not** modify or fork `../loop-engine` — the harness policy, loop policy, JS templates, and AIDLC framework are consumed read-only.
- It does **not** add orchestration primitives. Every node still becomes a `pipeline()`, `parallel()`, or loop from the unchanged templates.
- It does **not** override the human gates. AIDLC's gates are the PM's phase boundaries.
- The DAG, model routing, and ledger are **planning and reporting artifacts** layered around an otherwise-standard workflow run.

## Files in this skill

- `references/task-decomposition.md` — building the typed task DAG (node schema, edge rules, fan-out vs loop, phase grouping).
- `references/model-routing.md` — the "right model for the right job" table: `taskType` → model + effort tier, with rationale and escape hatches.
- `references/standards.md` — the authoritative standards this skill applies — named, version-pinned, and mapped to its workflow
- `templates/project-plan.workflow.js` — a `../loop-engine` script template that realizes one phase's sub-DAG with per-node model/effort/isolation and the ledger `log()` lines.
