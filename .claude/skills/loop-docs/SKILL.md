---
name: loop-docs
description: Write and maintain technical documentation: READMEs, API references, docstrings, guides, and ADRs. Use when the user asks to write, generate, update, or improve documentation, a README, API docs, docstrings, a how-to guide, or to keep docs in sync with code. Follows the Diataxis doc-type model and matches repo conventions.
---

# Writing Docs

You are about to write or update technical documentation. The engine is you reading the actual code and rendering it into prose a specific reader can act on — not paraphrasing what the code *should* do from its names. Documentation earns trust by being correct and loses all of it the first time a reader follows it and it fails. **Documenting aspirational behavior — what the code was meant to do rather than what it provably does — is the failure mode this skill exists to prevent**; every claim you write is checked against the source (§5).

## 1. Pick the right TYPE first

**Using the wrong doc type is the most common documentation failure** — a reference dump when the reader needed a tutorial, or a chatty walkthrough when they needed a lookup table. Classify the need before writing a line. The Diataxis model splits docs along two axes (study vs. work, practical vs. theoretical):

- **Tutorial** — learning-oriented; take a beginner through a guaranteed-success first run.
- **How-to guide** — task-oriented; get a competent user through one real goal.
- **Reference** — information-oriented; describe the API/config/CLI exhaustively and neutrally for lookup.
- **Explanation** — understanding-oriented; the why, the trade-offs, the background.

Plus the repo-artifact types: **README** (orientation + fastest path to running), **docstrings/inline** (contract at the call site), **ADR** (one architecture decision, its alternatives, its consequences), **changelog** (what changed, for upgraders). Full decision criteria, per-type skeletons, and the "what goes where" map are in **`references/doc-types.md`** — read it before drafting anything non-trivial. When one request mixes needs (most do), write each part as its own type rather than blending them.

## 2. Fix the audience and purpose

**Name the one reader and the one thing they need to do before writing.** A doc addressed to "everyone" serves no one — the beginner drowns in the reference and the expert can't find the flag. Pin down: who reads this (new user, integrator, maintainer, operator), what they know already, what task or decision they arrived with, and what "done" looks like for them. Purpose and type are coupled — an integrator wanting one endpoint needs a how-to, not the whole reference. If the audience is genuinely split, split the document.

## 3. Match repo conventions

**Documentation that fights the repo's existing style reads as bolted-on and gets abandoned.** Before writing, survey what's already there: where docs live (`README`, `docs/`, `/website`, docstring style), the format (Markdown, reStructuredText, MDX, JSDoc/Google/NumPy docstring convention), the tone (terse vs. narrative), and the tooling (Sphinx, MkDocs, Docusaurus, TypeDoc, Doxygen — build config reveals it). Place new docs where a maintainer expects them, follow the established heading depth and code-fence conventions, and reuse the existing voice. When conventions conflict or none exist, state the convention you're adopting and apply it uniformly.

## 4. Write: accurate first, example-driven, no rot

Draft against **`references/style.md`** (open it before a substantial doc). The load-bearing rules:

- **Accurate before polished** — a well-written wrong sentence is worse than a blunt correct one. Correctness is §5 and it dominates.
- **Lead with a working example** — a reader copies the snippet before reading the prose. Show the common case first, then the parameters.
- **Active voice, present tense, direct address** — "call `save()` to persist" not "the data may be persisted".
- **No doc-rot bait** — avoid duplicating facts the code already states (version numbers, default values, exhaustive param lists better generated from source), and never hand-transcribe a value you can point to. Every hand-copied fact is a future lie.

## 5. ACCURACY: verify every claim against the source

**Docs must match the code as it is, not as intended — verify, never assume.** This is the non-negotiable core of the skill. Before you state that a function returns X, takes params Y, throws Z, or that a flag defaults to D: read the actual definition and confirm it. Run the example if you can; a tutorial whose first command errors is worse than no tutorial. Signatures, return types, error conditions, config defaults, env vars, endpoint paths, and CLI flags all get checked at the source — treat names and comments as hints to verify, not facts to copy. If behavior is genuinely undefined or you cannot confirm a claim, say so explicitly rather than inventing a confident description. Aspirational documentation is the one defect this skill must never ship.

## 6. Keep docs in sync with code

**Note where the prose couples to code so the coupling is visible when the code changes.** Docs rot because nothing signals which doc a change invalidates. As you write, minimize hand-copied facts (§4) and, where a doc necessarily tracks a specific symbol, flag it — a comment near the code (`// keep in sync with docs/api.md#auth`), a pointer in the doc back to the source of truth, or a note in your summary listing which docs a future change to this module must update. For an update task, diff the code against the current doc and correct every drifted claim, not just the one the user flagged.

This skill maintains prose. It **complements the `loop-design` skill**, which *emits* the first draft of ADRs and C4 diagrams as part of a design (`templates/adr-template.md`, `templates/c4-context.md`). When a design decision is being made, that skill produces the record; this skill keeps that record and the surrounding prose docs accurate and readable over time.

## 7. Orchestration: document many modules as a workflow

A single doc you write inline in this session. For **documenting many modules, packages, or endpoints at once — a whole API surface, a monorepo's package READMEs — run it as a multi-agent workflow** using the template at **`templates/doc-generation.workflow.js`**. Per area it runs a three-stage pipeline:

1. **Extract intent from code** — one agent per module reads the source and returns the verified contract as structured data (public symbols, signatures, defaults, errors).
2. **Draft** — turn that extracted intent into prose of the chosen §1 type, in the repo's conventions.
3. **Verify accuracy** — an adversarial agent checks each drafted claim back against the source and kills anything unconfirmed (§5), so the fan-out cannot ship aspirational docs.

This is the per-item **pipeline → adversarial-verify** pattern from the **`loop-engine`** skill (see its `templates/pipeline.workflow.js` and harness policy H1 pipeline-default, H4 adversarial-verify). Invoke the `loop-engine` skill to author and execute the run; the doc-generation template pre-wires the extract/draft/verify stages and the accuracy schema. For one or two modules you can hold in context, skip the workflow and write directly — don't spin up agents for a single README.

## Reference files

| File | Read it when |
|---|---|
| `references/doc-types.md` | Choosing a type; per-type skeletons and the "what goes where" map |
| `references/style.md` | Drafting; the writing rules, example patterns, and anti-rot practices |
| `references/standards.md` | Authoritative standards this skill applies — Diátaxis, Google/MS style, CommonMark, Conventional Commits, MADR — named, pinned, and mapped |
| `templates/doc-generation.workflow.js` | Documenting many modules: extract-intent → draft → verify-accuracy pipeline |
