# Model Routing — the right model for the right job

Every agent you spawn runs on *some* model at *some* effort. Picking well is the difference between a project that finishes cheap and correct and one that burns Opus tokens formatting a table or ships a bug because a $0.02 Haiku "verified" a security fix. This file is the router: task type in, model + effort out, with two override modifiers layered on top.

**The default fleet caps at Opus 4.8.** Opus is the ceiling for judgment work — `xhigh` and `max` effort live here. **Fable 5 is opt-in only** (see the last section); it is off the default routing path and never reached by the table below unless the user or a deliberate router decision puts it there.

## The fleet

| Model | ID | Reach for it when |
|---|---|---|
| Haiku 4.5 | `claude-haiku-4-5-20251001` | The work is mechanical and voluminous — cheap and fast is the whole point. |
| Sonnet 5 | `claude-sonnet-5` | The work is real production output — most coding and drafting. |
| Opus 4.8 | `claude-opus-4-8` | The work needs judgment — decompose, synthesize, review, or judge. |
| Fable 5 | `claude-fable-5` | Opt-in only. The single hardest narrow long-horizon reasoning task, eyes open to the caveats. |

Effort is a separate dial, ordered `low < medium < high < xhigh < max`. Model sets the *class* of reasoning available; effort sets how hard the model works within it. Route both.

## Routing table (base route)

Match the task to the most demanding row it fits, then apply the modifiers below.

| Task type | Model | Effort |
|---|---|---|
| Mechanical / high-fan-out / extraction / formatting / simple search | Haiku 4.5 | omit (or `low`) |
| Implementation / drafting / most coding / moderate reasoning | Sonnet 5 | `medium`–`high` |
| Judgment / decomposition / synthesis / architecture or security review / verification | Opus 4.8 | `high`–`xhigh` |
| Correctness-critical verify or judge (high downstream error cost) | Opus 4.8 | `max` |

Read the rows top-down and stop at the first that genuinely describes the task. "Rewrite this call site to the new API" is implementation (Sonnet), not extraction (Haiku), even though it touches one line. "Does this auth change leak a session across tenants?" is a security review (Opus `xhigh`) whose *wrong answer ships a breach* — so it is really the last row (Opus `max`).

Within a band, pick the higher effort when the input is ambiguous, the output is long-horizon, or a mistake is expensive to catch later; pick the lower effort when the task is well-specified and self-checking.

## Override modifier A — wide fan-out pushes a tier DOWN

When a stage fans out across many items, the governing constraint is no longer "which model reasons best" but **budget** — the token target is a hard ceiling, and once it is spent further `agent()` calls throw (harness policy H6, see `../../loop-engine/references/harness-policy.md`). Protect that ceiling by dropping the per-item tier one step:

- A 300-endpoint inventory that "should" be Sonnet becomes Haiku — the per-item judgment is small and multiplied 300×, the aggregate bill is not.
- If per-item work is genuinely mechanical, this bottoms out at Haiku (`low`); Haiku is the floor, not a starting point you drop below.
- Split when a fan-out is bimodal: route the trivially mechanical items to Haiku and let only the ambiguous minority ride up a tier, rather than paying the top tier for the whole set.

Whatever you cap, `log()` it — a silently narrowed fan-out reads as full coverage when it was not (harness policy H6, no silent caps).

## Override modifier B — high downstream error-cost pushes a tier UP

Verification is asymmetric: the cost of *accepting a wrong result* dwarfs the cost of the check. When an agent's output gates everything after it — a verify that decides "ship it", a decomposition every later task inherits, a judge that picks the winner — push the tier **up** even if the task looks cheap:

- "Confirm the migration dropped no log lines" is nominally a search (Haiku), but a false "all clear" corrupts 40 services silently → Opus `max`.
- A one-line release-notes edit is formatting (Haiku), but if downstream auditors treat it as the compliance record, lift it to Sonnet with a real review.

The two modifiers can both apply and pull opposite ways — a wide verify fan-out over correctness-critical items. Resolve it by keeping the *model* high (error cost wins on the tier) while controlling spend through **fewer, sharper verifiers at higher effort** rather than many cheap ones (harness policy H4: diversity beats redundancy).

## Mapping to `agent()` opts

Routing decisions become two options on the Workflow `agent()` call — `opts.model` and `opts.effort` (harness policy H8):

```js
agent(prompt, { label: 'verify:concurrency', phase: 'Verify',
                model: 'claude-opus-4-8', effort: 'max', schema })
```

**Omit `model` by default.** Agents inherit the session model, and the session already caps at Opus 4.8 — so a judgment or verify stage usually needs *no* `model` override at all; you just set `effort`. Set `opts.model` only when the router has a clear reason to leave the session tier: routing **down** to Haiku/Sonnet for a cheap or wide stage (modifier A), or the rare case of a session running below Opus that needs one heavy stage lifted. If you find yourself writing `model: 'claude-opus-4-8'` on every call, drop it — that is just the session model, spelled out.

## Worked example — routing a 5-task project

Project: *migrate 40 services off a deprecated logging library.* Session model is Opus 4.8 (the default cap).

| # | Task | Base task type → route | Modifier | Final route (`agent()` opts) |
|---|---|---|---|---|
| 1 | Scan each repo, list every call site of the old lib | extraction → Haiku 4.5, `low` | wide fan-out (40 repos): already at the floor | `model:'claude-haiku-4-5-20251001'`, `effort:'low'` |
| 2 | Rewrite each call site to the new API | implementation → Sonnet 5, `high` | wide fan-out (≈2k sites): push down; split off the trivial ones | trivial sites `model:'claude-haiku-4-5-20251001'`; ambiguous sites `model:'claude-sonnet-5'`, `effort:'medium'` |
| 3 | Decompose the rollout into dependency-safe batches | decomposition → Opus 4.8, `high` | gates every later task (error-cost) → bump effort | omit `model` (inherits Opus), `effort:'xhigh'` |
| 4 | Confirm no service silently drops log lines post-migration | verification → Opus 4.8, `xhigh` | correctness-critical, false "all clear" is costly | omit `model`, `effort:'max'` |
| 5 | Regenerate the migration changelog table | formatting → Haiku 4.5, omit effort | none | `model:'claude-haiku-4-5-20251001'` |

Notes on the routing:

- **Tasks 3 and 4 carry no `model` override** — they are Opus work and the session is already Opus. Only their `effort` is set. Spelling out `model:'claude-opus-4-8'` there would be noise.
- **Tasks 1, 2, 5 carry an explicit `model`** — every one routes *down* from the session tier to protect budget, which is exactly when `opts.model` earns its place.
- **Task 2 is split by the fan-out modifier**, not routed as a monolith: the mechanical majority drops to Haiku, the ambiguous minority stays on Sonnet. Log the split so the coverage is legible.
- **Nothing reaches for Fable 5** — no task here is a hard enough narrow long-horizon problem to justify its caveats.

## Fable 5 — opt-in only

Fable 5 (`claude-fable-5`) is reserved for the **single hardest narrow long-horizon reasoning task** in a project — a deep proof-style derivation, a gnarly multi-step planning problem, one the-whole-project-hinges-on-it analysis. It is never the default and never selected by the table above on its own. Treat it as a deliberate, per-task opt-in, and weigh the caveats first:

- **Latency.** It is markedly slower per call. Never place it on a gate-blocking interactive step where a human or the pipeline is waiting on it — the run stalls.
- **Retention.** Its usage carries a 30-day data-retention consideration distinct from the rest of the fleet; confirm that is acceptable for the data in play before routing anything sensitive to it.
- **Refusal risk.** It refuses a broader class of prompts. Do not put it anywhere a refusal silently drops an item — and specifically **never use it for security-audit fan-out**, where a refused finding reads as a clean bill of health.

If a task tempts you toward Fable 5 but is *wide* (a fan-out) or *blocking* (a gate), that temptation is the signal to route it to Opus 4.8 at `max` effort instead. Fable is a scalpel for one deep cut, not a tier you spread across a phase.
