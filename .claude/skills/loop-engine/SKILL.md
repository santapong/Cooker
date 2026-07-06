---
name: loop-engine
description: Orchestrate a task with multi-agent Workflow scripts (pipeline/parallel/loop) governed by the harness & loop engineering policies and a pluggable lifecycle framework (default AIDLC). Use when the user asks to run a task as a workflow, orchestrate with subagents, fan out agents, or execute a phase-structured job (audit, migration, review sweep, feature build) at multi-agent scale.
argument-hint: <task> [--framework <name>] [--dry-run]
---

# Workflow Skill

You are about to author and execute a multi-agent Workflow script for the user's task. Invoking this skill is the user's explicit opt-in to multi-agent orchestration via the Workflow tool.

## Execution flow

Follow these steps in order.

### 1. Parse arguments

From the skill args, extract:

- **task** — everything that is not a flag. This is the job to orchestrate. If empty, ask the user what task to run.
- **`--framework <name>`** — which lifecycle framework governs the phases. Default: `AIDLC`.
- **`--dry-run`** — if present, author the script and show it to the user, but do NOT execute it.

### 2. Load governing documents

Read, from this skill's directory:

1. `references/harness-policy.md` — the Harness Engineering Policy (orchestration design rules)
2. `references/loop-policy.md` — the Loop Engineering Policy (iteration rules)
3. `references/standards.md` — prior art the harness/loop policies formalize (fan-out/fan-in, DAG execution)
4. `frameworks/<name>.md` — the chosen framework

If `frameworks/<name>.md` does not exist, list the `frameworks/` directory (ignore `README.md` and `_TEMPLATE.md`), show the user the available framework names, and ask which to use.

### 3. Map the task onto the framework

The framework file defines phases, each with: purpose, entry criteria, agent activities, an orchestration hint (pipeline / parallel / loop), and an exit gate.

- Decide which phases apply to this task. Small tasks may need only one phase; do not force every phase onto every task.
- For each applicable phase, decide the concrete fan-out: what items, what each agent does, what schema it returns.
- Note the framework's human-in-the-loop gates: at those points the workflow (or you, between workflows) must stop and return results for user approval before continuing. Prefer one Workflow invocation per gated phase so the user stays in the loop between phases.

### 4. Choose the orchestration shape (per the harness policy)

- Default to `pipeline()` — no barrier between stages.
- Use a `parallel()` barrier between stages only when a stage genuinely needs ALL prior results at once (cross-item dedup/merge, early-exit on zero findings, prompts that reference "the other findings").
- Use a loop (per the loop policy) only for unknown-size discovery. A known work-list is a single `pipeline()`, not a loop.
- Apply the verification rules from the harness policy: schema on every machine-consumed result, adversarial or diverse-lens verification for findings, judge panels for wide solution spaces.

### 5. Author the script

Start from the closest template in `templates/`:

| Template | When |
|---|---|
| `pipeline.workflow.js` | Known items flowing through independent stages (default choice) |
| `parallel.workflow.js` | Fan-out finders whose results must be merged/deduped before the next stage |
| `loop-until-dry.workflow.js` | Unknown-size discovery (find "all" of something) |
| `loop-until-budget.workflow.js` | User gave a token target ("+500k") to scale depth against |

Copy the template's structure and fill the `EDIT ME` slots. Requirements for the finished script:

- `export const meta = {...}` first, as a **pure literal** (no variables, calls, spreads, or template strings). `meta.phases` titles must exactly match the `phase()` / `opts.phase` strings used in the body, and should mirror the framework phase names.
- Plain JavaScript, NOT TypeScript — no type annotations, interfaces, or generics.
- No `Date.now()`, `Math.random()`, or argless `new Date()` — pass timestamps in via `args`; stamp results after the workflow returns.
- Parameterize with `args` (pass real JSON values in the Workflow call, never a JSON-encoded string) — and still normalize defensively at the top of the script, since some harnesses deliver `args` as a string: `const input = typeof args === 'string' ? JSON.parse(args) : args`.
- `.filter(Boolean)` on every `parallel()` result before use; skipped/dead agents resolve to `null`.
- Pass a `schema` to every `agent()` whose output the script consumes.
- `log()` progress each round/stage, and `log()` anything dropped by a cap — no silent truncation.

### 6. Execute

- **If `--dry-run`**: print the full script in a fenced code block, explain the phase structure in a sentence or two, and stop. Do not call Workflow.
- **Otherwise**: call the Workflow tool with the script inline (`script`) and the task parameters as `args`. Note the returned `scriptPath` and `runId` — to iterate, edit the persisted script file and re-invoke with `{scriptPath, resumeFromRunId}` rather than resending the script.

### 7. Report

- Relay the workflow's structured result to the user in plain prose: what ran, what was found/produced, and anything dropped or skipped.
- If the result is empty or looks wrong, read `<transcriptDir>/journal.jsonl` from the run before diagnosing — it records each agent's actual return value.
- If a framework gate was reached, present the gate's deliverable and ask the user to approve before authoring the next phase's workflow.

## Adding frameworks

Frameworks are pluggable: drop a new `<Name>.md` into `frameworks/` following `frameworks/_TEMPLATE.md`. See `frameworks/README.md`. No changes to this file are needed.
