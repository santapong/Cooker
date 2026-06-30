# Model selection & orchestration (workflow `.js`)

How to choose the model per scope, auto-decide it, see model + role *while a run is happening*, and
how the orchestration layers fit together. This is guidance for authoring `Workflow` scripts — the
existing saved scripts inherit the session model and are unchanged.

## 1. Selecting the model & effort

A workflow agent picks its model from `opts.model`; if you omit it, the agent **inherits the
session model** (the recommended default — usually correct).

```js
// per-agent — the real override:
await agent(prompt, { model: 'opus', effort: 'high' })

// model ∈ 'sonnet' | 'opus' | 'haiku' | 'fable'
// effort ∈ 'low' | 'medium' | 'high' | 'xhigh' | 'max'   (omit to inherit session effort)
```

**Per-phase "scope":** there is no single switch for a phase — you apply the same model to every
`agent()` in that phase (a `MODELS` map or `pickModel()` keeps it DRY):

```js
await parallel(items.map(i => () => agent(p(i), { model: MODELS.verify, phase: 'Verify' })))
```

`meta.phases[].model` is **declarative only** (shown in `/workflows` and telemetry) — it documents
the intent; it does **not** enforce the override. The override must be on the `agent()` call.

`agentType` (a custom subagent like `Explore` or `cooker-security`) brings its own frontmatter
model; `opts.model` overrides it when both are set.

> Reading it back: `agent()` returns the agent's text or your `schema` object — **never** the
> model. A script can *set* the model, not query it. The resolved model is recorded per-agent in
> `/workflows` and in the run result's `workflowProgress[].model`.

## 2. Auto-deciding the model (a policy, not magic)

There's no built-in difficulty detector. "Automatic" = a deterministic policy function the script
calls, keyed on the **role** of the work. Honest and predictable:

```js
// Respect this repo's model policy — see frameworks/aidlc.md (Hard gates) and AGENTS-PLAN.md.
// e.g. Governance forbids Haiku entirely; keep it out of the map there.
const pickModel = (role) => ({
  read:       'sonnet',   // bulk reading / search / inventory
  mechanical: 'sonnet',   // copy files, lint, format, rename
  build:      'sonnet',   // focused implementation
  judge:      'opus',     // judgment / architecture / hard forks
  verify:     'opus',     // adversarial verification
}[role] ?? 'sonnet')      // unknown role → safe default (never an unallowed tier)

const pickEffort = (role) => (role === 'verify' || role === 'judge' ? 'high' : 'medium')
```

Pick the **cheapest tier that holds** for the role (pairs with the `ponytail` ethos), and escalate
only the judgment/verify stages. The main loop (the human-facing agent) can also choose models
dynamically when it authors the script — that's the other "automatic" path.

## 3. Telling you the model + role *while it runs*

Wrap `agent()` so each step narrates itself via `log()` before it starts. `label` carries the role.

```js
async function runAgent({ role, model, phase, prompt, schema }) {
  const m = model ?? pickModel(role)
  log(`▶ ${phase} · role=${role} · model=${m} · effort=${pickEffort(role)}`)
  return agent(prompt, { label: role, phase, model: m, effort: pickEffort(role), schema })
}
```

Now a live run (watch `/workflows`) shows lines like
`▶ Verify · role=verify · model=opus · effort=high`, and the per-agent telemetry independently
records the resolved model. Use `runAgent(...)` instead of bare `agent(...)` whenever you want the
model/role surfaced.

## 4. Orchestration layers

| Layer | What it is | When it decides |
|---|---|---|
| **The `.js` body** | The deterministic orchestrator: `phase()` / `pipeline()` / `parallel()` / loops / conditionals. | Authoring time — fixed control flow. |
| **Main loop** (the session agent) | Launches workflows, reads each result, picks the next phase/workflow. | Run time — dynamic, between workflows. |
| **`workflow(name, args)`** | One workflow calling another inline (one level of nesting). | A workflow needs a self-contained sub-orchestration. |
| **Agent rosters** | Dynamic delegators built on the *Agent* tool, not the Workflow tool: `project-manager` (Governance), `cooker-feature-dev` (Cooker), `ceo-orchestrator` (research). | When delegation should be model-driven, not scripted. |

Rule of thumb: scripted/repeatable fan-out → the `.js` body; judgment-driven delegation → an agent
roster; "do phase A, then decide phase B from its output" → the main loop across turns.

## 5. Worked example

```js
export const meta = {
  name: 'example-with-models',
  description: 'Shows MODELS map, pickModel, runAgent live logging, and a pinned verify phase',
  phases: [
    { title: 'Scan' },                 // mechanical → sonnet
    { title: 'Verify', model: 'opus' },// declarative hint; real override is on the agent() call
  ],
}

const pickModel  = (role) => ({ scan: 'sonnet', verify: 'opus' }[role] ?? 'sonnet')
const pickEffort = (role) => (role === 'verify' ? 'high' : 'low')
async function runAgent({ role, phase, prompt, schema }) {
  const model = pickModel(role)
  log(`▶ ${phase} · role=${role} · model=${model}`)
  return agent(prompt, { label: role, phase, model, effort: pickEffort(role), schema })
}

phase('Scan')
const findings = await runAgent({ role: 'scan', phase: 'Scan',
  prompt: 'List candidate issues in <area>.', schema: FINDINGS })

phase('Verify')
const verdicts = await parallel((findings.items ?? []).map(f => () =>
  runAgent({ role: 'verify', phase: 'Verify',
    prompt: `Adversarially refute: ${f.title}`, schema: VERDICT })))

return { findings, verdicts: verdicts.filter(Boolean) }
```

Mechanical work runs on `sonnet` at low effort; verification is pinned to `opus` at high effort; and
every step logs its role + model so the running workflow tells you exactly what it's using. Keep the
map aligned with the repo's model policy (Governance: never Haiku).
