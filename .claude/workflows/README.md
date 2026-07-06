# Cooker — saved workflows

Deterministic multi-agent orchestration scripts, invokable via the `Workflow` tool by name. **Opt-in only** — see [`../skills/loop-engine/SKILL.md`](../skills/loop-engine/SKILL.md) (the `/loop-engine` skill) for the rules, patterns, and script templates (`../skills/loop-engine/templates/`); run it with `--framework Cooker-AIDLC` for Cooker's gated lifecycle. Model-selection and live-narration guidance for authoring scripts lives in [`ORCHESTRATION.md`](ORCHESTRATION.md).

| Workflow | Pattern | Pairs with | Run |
|---|---|---|---|
| [`cooker-review.js`](cooker-review.js) | pipeline + adversarial verify | ADLC Phase 5 (Review) | `Workflow({ name: "cooker-review" })` |
| [`cooker-audit-sweep.js`](cooker-audit-sweep.js) | loop-until-dry + multi-modal sweep | ADLC Phase 10 (Improve) | `Workflow({ name: "cooker-audit-sweep" })` |
| [`cooker-health-sweep.js`](cooker-health-sweep.js) | multi-modal finders → verify → remediation plan | ADLC Phase 0/10 → 1 (Plan) | `Workflow({ name: "cooker-health-sweep" })` |

These are the reference implementations — read them before writing a new workflow. See [`../../docs/engineering/ADLC.md`](../../docs/engineering/ADLC.md) for where they fit in the lifecycle.
