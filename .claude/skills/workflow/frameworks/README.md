# Workflow frameworks

A **framework** is a markdown file in this directory that describes *how a task moves from
intake to done* — its stages, the gate that must hold before each stage, and which stages a
human must sign off. The `workflow` skill reads the selected framework and sequences the run
(a single agent, or a multi-agent `Workflow`) to match it.

The default is `aidlc` (the file with `default: true`). It encodes **this repo's** real gates,
so the out-of-the-box behaviour follows the project's AI-Assisted Development Life Cycle. You can
switch to another framework per task, or **write your own** — no code change.

## Selecting a framework

- Default: just invoke `/workflow` → uses the `default: true` framework (`aidlc`).
- Switch for one run: `/workflow framework=sdlc` (or say "run this as SDLC", "use the lightweight
  framework", "do this lazy").
- The skill resolves the name to `frameworks/<name>.md`, validates it (below), and follows it.

## Bundled frameworks

| File | `name` | Use it for |
|---|---|---|
| `aidlc.md` | `aidlc` | **Default.** Anything non-trivial or security-sensitive in this repo — full gates. |
| `sdlc.md` | `sdlc` | A classic requirements→design→build→test→deploy→maintain pass when you want phase-gated rigor without the AI-governance specifics. |
| `lightweight.md` | `lightweight` | Small, low-risk, reversible changes — skip the heavy gates, keep the safety floor. |
| `TEMPLATE.md` | — | Skeleton to copy when authoring your own framework. Not selectable. |

## File format (every framework follows this)

```markdown
---
name: <id>            # lowercase, matches the filename (my-framework.md → my-framework)
description: <one line — when to reach for this framework>
default: false        # exactly ONE framework in this dir sets true (the repo default)
---

# <Framework name>

## When to use
One short paragraph: the kind of work this fits, and what it is NOT for.

## Stages
Ordered. One entry per stage:

**N. <Stage name>** — <purpose, one line>.
- Exit gate: <the condition that must be true to leave this stage>
- Human-gated: yes | no   (yes = a person must sign off before continuing)
- Harness: <how to run it — inline | one Explore agent | one Plan agent | pipeline over the
  work-list | adversarial parallel verify | /code-review phase>

## Hard gates
The non-negotiables that BLOCK the run and must never be routed around. Repo invariants live
here. If one cannot be satisfied, the run stops and reports — it does not work around the gate.

## Tracker sync (optional)
Off by default. If the user opts in, how this framework maps stages to an external tracker.
```

## Authoring your own framework

1. `cp TEMPLATE.md frameworks/<your-name>.md`
2. Fill in the frontmatter (`name` = filename, `default: false`) and the four sections.
3. Keep `## Stages` and `## Hard gates` — the skill **validates** they exist before running, and
   refuses a framework that is missing either (fail-closed: a framework with no gates is not a
   framework).
4. Run it: `/workflow framework=<your-name>`.

Don't set `default: true` on a second file — exactly one default per repo. To change the default,
move `default: true` onto your file and set the old one to `false`.

## Tracker sync seam (optional, off by default)

This repo's source of truth for work stays the in-repo tracker (see the repo's backlog/followups
doc). If a team wants run/stage status mirrored into an external project-management tool, a
framework's `## Tracker sync` section can opt in to:

- **ClickUp** (when the ClickUp MCP tools are connected): create a task at the first stage
  (`clickup_create_task`) and update its status at each gate / on Land (`clickup_update_task`).
- **GitHub Issues/Projects** (GitHub MCP): open/track an issue and link it to the PR at Land.

It is tool-agnostic — name the tool and the stage→status mapping in the framework file. Nothing
syncs unless a framework turns it on and the user asks for it.
