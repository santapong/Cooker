# Cooker — Engineering Process

How Cooker is built: the lifecycle a change moves through, and the Claude Code harness (agents + skills + workflows) that drives it.

| Doc | Read when |
|---|---|
| [`ADLC.md`](ADLC.md) | You want the full Application Development Life Cycle — phases, gates, and which agent/skill/workflow owns each. Start here. |
| [`harness-engineering.md`](harness-engineering.md) | You're adding or changing an agent/skill/workflow, or want to understand how the `.claude/` fleet is built and kept honest. |
| [`../../.claude/skills/loop-engine/SKILL.md`](../../.claude/skills/loop-engine/SKILL.md) | You want to author or run a multi-agent workflow (the `/loop-engine` skill, from TheLoopSkill; use `--framework Cooker-AIDLC` for this repo's gated lifecycle). |

These describe **how we build Cooker**. For how Cooker itself *works*, see [`../system-design/`](../system-design/README.md) and [`../reference/`](../README.md#reference). For *what's open*, see [`../../backlog.md`](../../backlog.md).
