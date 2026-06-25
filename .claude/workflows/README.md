# Cooker — saved workflows

Deterministic multi-agent orchestration scripts, invokable via the `Workflow` tool by name. **Opt-in only** — see [`../skills/workflow/SKILL.md`](../skills/workflow/SKILL.md) for the rules, patterns, and how to author a new one (`../skills/workflow/new-workflow.sh <slug>`).

| Workflow | Pattern | Pairs with | Run |
|---|---|---|---|
| [`cooker-review.js`](cooker-review.js) | pipeline + adversarial verify | ADLC Phase 5 (Review) | `Workflow({ name: "cooker-review" })` |
| [`cooker-audit-sweep.js`](cooker-audit-sweep.js) | loop-until-dry + multi-modal sweep | ADLC Phase 10 (Improve) | `Workflow({ name: "cooker-audit-sweep" })` |

These are the reference implementations — read them before writing a new workflow. See [`../../docs/engineering/ADLC.md`](../../docs/engineering/ADLC.md) for where they fit in the lifecycle.
