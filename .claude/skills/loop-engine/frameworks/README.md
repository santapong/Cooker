# Frameworks

A framework defines the **lifecycle phases** a workflow follows: what each phase produces, how it fans out agents, and where the human approval gates sit. The workflow skill maps the user's task onto the chosen framework's phases, then authors one Workflow script per gated phase.

## How resolution works

`/loop-engine <task> --framework <name>` loads `frameworks/<name>.md` (case-sensitive file name, e.g. `--framework AIDLC` → `AIDLC.md`). With no flag, the default is **AIDLC**. If the named file doesn't exist, the skill lists this directory (excluding `README.md` and `_TEMPLATE.md`) and asks which framework to use.

## Available frameworks

| Framework | Summary |
|---|---|
| [AIDLC](AIDLC.md) | AI-Driven Development Life Cycle — Inception → Construction → Operation, with a human gate after each phase. |

## Adding a framework

1. Copy `_TEMPLATE.md` to `<Name>.md` (the file name is the `--framework` argument).
2. Fill in the frontmatter (`name`, `summary`, `when-to-use`) and one `## Phase:` section per phase, keeping the template's field headings — the skill reads **Purpose / Entry criteria / Agent activities / Orchestration hint / Exit gate** when mapping a task onto phases.
3. In each **Orchestration hint**, justify barriers and loops by citing the relevant rule in `../references/harness-policy.md` or `../references/loop-policy.md`.
4. Add a row to the table above.

No changes to `SKILL.md` are needed — frameworks are discovered from this directory.
