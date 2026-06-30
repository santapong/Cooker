---
name: my-framework          # CHANGE ME: lowercase, must match the filename (my-framework.md)
description: One line — when someone should reach for this framework instead of the others.
default: false              # leave false unless you are replacing aidlc as the repo default
---

# My Framework

<!--
  Copy this file to frameworks/<your-name>.md and fill it in. The workflow skill discovers it by
  filename and VALIDATES that `## Stages` and `## Hard gates` exist before running — a framework
  with no gates is refused (fail-closed). Keep stages ordered and gates concrete.
  See README.md for the full format and how selection works.
-->

## When to use
One short paragraph: the work this framework fits, and what it is explicitly NOT for (point at
`aidlc` / `sdlc` / `lightweight` for those).

## Stages
Ordered. One block per stage:

**1. <Stage name>** — <purpose, one line>.
- Exit gate: <the condition that must be true to leave this stage>
- Human-gated: yes | no
- Harness: <inline | one Explore agent | one Plan agent | pipeline over the work-list |
  adversarial parallel verify | /code-review phase>

**2. <Stage name>** — ...
- Exit gate: ...
- Human-gated: ...
- Harness: ...

<!-- add as many stages as the framework needs -->

## Hard gates
List the non-negotiables that BLOCK the run and must never be routed around. At minimum, restate
the repo invariants this framework must still honour (the skill and the repo's CLAUDE.md are the
source for those). If a gate cannot be met, the run stops and reports — it does not work around it.

## Tracker sync (optional)
Off by default. If this framework should mirror status into an external tracker, name the tool
(ClickUp / GitHub Issues) and the stage → status mapping here. Otherwise leave this note as-is.
