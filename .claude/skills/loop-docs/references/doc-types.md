# Doc Types: Diátaxis Modes and Concrete Artifacts

Every documentation page serves exactly one of four user needs. Pick the mode from the reader's need, not from what you happen to know. **The cardinal rule of this reference: never mix two modes in one document** — a tutorial that stops to explain design rationale, or a reference page that drifts into a how-to, fails both readers at once. When you feel a page pulling toward a second mode, that is the signal to split it and cross-link.

This file has two parts: the four Diátaxis modes (§1–§5), and the concrete artifacts you actually write and their required structure (§6–§10). The decision table (§11) maps a user need to the doc type to reach for.

## 1. The four modes at a glance

**Key rule: orientation is the axis — is the reader *studying* or *working*, and do they need *practical* steps or *theoretical* understanding?** Diátaxis places the four modes on those two axes:

| Mode | Orientation | Reader is… | Answers |
|---|---|---|---|
| Tutorial | learning | studying, at practice | "teach me to get started" |
| How-to guide | task | working, at practice | "help me do this specific thing" |
| Reference | information | working, at study | "tell me exactly what this is" |
| Explanation | understanding | studying, at study | "help me understand why" |

A doc set needs all four. A README that only quickstarts, or an API reference with no explanation of the model behind it, leaves a predictable gap. Diagnose gaps by asking which quadrant is empty.

## 2. TUTORIAL — learning-oriented

**Key rule: a tutorial guarantees a first success; the learner must reach a working result by following you, not by making decisions.** The tutorial is a lesson, and you are the teacher holding their hand.

- Deliver one concrete, complete, repeatable outcome ("build a working X"), not a tour of features.
- Every step must produce a visible result the learner can check against what you show. No dead ends, no "if you want, you could…".
- Make **no** choices the learner has to arbitrate — pick the path for them. Defer options, alternatives, and rationale; those are how-to and explanation material, and pulling them in here breaks the single-mode rule.
- Assume near-zero prior knowledge of *this* system. State prerequisites up front and keep them minimal.
- It must work every time. A tutorial that fails on step 7 destroys the beginner's confidence, so pin versions and test it end to end on a clean environment.

Smell test: if the reader could already do the task and just needs the recipe, they want a how-to, not a tutorial.

## 3. HOW-TO GUIDE — task-oriented

**Key rule: a how-to serves a competent user with a goal already in mind; it is a sequence of steps to that goal, not a lesson.** It assumes the reader knows what they want and roughly why.

- Title it as the goal, in the user's words: "How to rotate the signing key", "How to configure CORS for a subdomain".
- Give the ordered steps to reach the result and nothing that does not serve reaching it. Omit conceptual detour; link to an explanation page for the "why".
- It is fine — expected — to address real-world messiness: branches, preconditions, "if you use Docker, instead do…". This is what separates a how-to from a tutorial, which must have no branches.
- Do not try to be complete about the domain; be complete about the *task*. Coverage of every flag lives in reference.

Smell test: if there is no concrete end goal and you are just describing what each option does, you are writing reference, not a how-to.

## 4. REFERENCE — information-oriented

**Key rule: reference describes the machinery accurately and exhaustively, and does nothing else — it is consulted, not read.** The reader arrives mid-task to look one thing up and leave.

- Mirror the structure of the code/CLI/config it documents, so a reader can navigate by the shape they already know.
- Be accurate above all, and stay consistent in structure and tone across every entry — a reference is a lookup table, and uniform entries make it scannable.
- State *what is*, not *how to* or *why*. Descriptive, austere, neutral. A worked example per entry is welcome; a tutorial narrative is not.
- Keep it current with the thing it describes. Reference that lies is worse than none — prefer generating it from the source (docstrings, OpenAPI, `--help`) so it cannot drift.

Smell test: if you are motivating a choice or walking through a task, that content belongs in explanation or how-to; keep the reference entry to the facts.

## 5. EXPLANATION — understanding-oriented

**Key rule: explanation illuminates the *why* — design, trade-offs, history, alternatives — and is the one mode you read away from the keyboard.** It gives the mental model the other three assume.

- Discuss and connect: why the system is built this way, what was rejected, how pieces relate, where the boundaries and trade-offs are.
- It is allowed to hold an opinion and to admit messiness and history that reference must stay silent on.
- Do not instruct. The moment you write numbered steps to accomplish a task, you have crossed into how-to — link out instead.
- This is where ADRs (§9) and architecture narratives live; an ADR is a specialized, immutable explanation of one decision.

Smell test: if the reader could follow your page to *do* something, it is not pure explanation.

## 6. README — the front door

**Key rule: a README answers "what is this, why do I care, and how do I run it in five minutes" — it is a mode-spanning landing page whose job is orientation and routing, not depth.** It is the one intentional exception to single-mode purity, but keep each section in its lane and link out for depth.

Order sections so a stranger can self-qualify fast:

1. **Name + one-line what/why** — what the project is and the problem it solves, in a sentence. Lead with this; badges and logos come after.
2. **Status/quick facts** — language, license, build/version badges (optional).
3. **Quickstart** — the shortest path from clone to a working result, copy-pasteable. This is a mini-tutorial; keep it to the happy path.
4. **Install** — real prerequisites and install steps for actual use, beyond the quickstart.
5. **Usage** — the two or three most common tasks (how-to snippets), each linking to fuller docs.
6. **Links** — where to go next: full docs, contributing guide, changelog, license, support/issues.

Keep it skimmable. A README that tries to be the manual buries the quickstart; push detail into the `docs/` tree and link.

## 7. API REFERENCE — per endpoint or function

**Key rule: every entry carries the same six fields in the same order, so the page is a uniform lookup table.** Generate it from the source of truth (OpenAPI, type signatures) whenever you can so it cannot drift from the code.

Per endpoint / function, document:

1. **Signature** — HTTP method + path, or the function/method signature with types.
2. **Parameters** — each: name, type, required/optional, default, constraints, meaning. One row per param.
3. **Returns** — type and shape of a successful result, with field meanings.
4. **Errors** — the failure modes: status codes / exception types, when each fires, and how to recover.
5. **Example** — one minimal, runnable request+response (or call+result). Real values, not `foo`.
6. **Notes** — auth scope, rate limits, idempotency, side effects, version/deprecation — only what a caller must know.

Skipping the **Errors** field is the most common defect: callers need the failure surface as much as the success one.

## 8. DOCSTRINGS — reference at the code level

**Key rule: a docstring is API reference for one unit; write it for the caller who sees only the signature and this text.** Follow the language's dominant convention (Google/NumPy/reST for Python, TSDoc/JSDoc for TS/JS, rustdoc for Rust) so tooling can render it.

Include, in this order:

1. **Summary** — one imperative line: "Fetch and decode the manifest." Fits on the signature's line in an editor tooltip.
2. **Extended description** — optional paragraph for non-obvious behavior, only if needed.
3. **Params** — each argument: meaning, type (if not enforced), constraints, default.
4. **Returns** — what comes back and its type/shape.
5. **Raises / Throws** — each exception the caller can hit and the condition that triggers it.
6. **Example** — a short, ideally doctestable, usage snippet for anything non-trivial.

Document behavior the signature cannot express (side effects, units, invariants); do not restate what the types already say.

## 9. ADR — Architecture Decision Record (Nygard)

**Key rule: one ADR records one significant, hard-to-reverse decision, immutably — you supersede an ADR, you never rewrite it.** An ADR is a specialized explanation (§5): it captures *why*, frozen at the moment of choosing, so the reasoning survives even after the context shifts.

Nygard's three load-bearing sections:

- **Context** — the forces in play: the requirement, constraint, or problem forcing a choice now. State facts and tensions, not the answer. Cite the concrete numbers (scale, latency, team size) the decision answers to, so a future reader can tell whether they still hold.
- **Decision** — "We will…", active voice, present tense, specific enough to act on.
- **Consequences** — what becomes true afterward, good and bad. Every real decision has both; name the mitigation for each cost.

Number ADRs sequentially, keep a `Status` (Proposed · Accepted · Deprecated · Superseded), and store them in `docs/adr/NNNN-slug.md`. For the full blank template, section-by-section guidance, and the point that the rejected **Alternatives Considered** are what make it an ADR rather than a changelog entry, use **`../../loop-design/templates/adr-template.md`** — do not duplicate that structure here, fill it in there.

## 10. CHANGELOG — Keep a Changelog + SemVer

**Key rule: a changelog is written for humans, grouped by release, newest first — it is a curated list of notable changes, not a dump of git log.** Follow the [Keep a Changelog](https://keepachangelog.com) format and version with [Semantic Versioning](https://semver.org).

- One section per released version: `## [1.4.0] - 2026-07-03`, newest at top, plus an `## [Unreleased]` section at the very top for changes landed but not yet cut.
- Group each version's entries under the standard headings: **Added**, **Changed**, **Deprecated**, **Removed**, **Fixed**, **Security**. Omit headings with no entries.
- Write each entry from the user's perspective — what changed for them — not the implementation detail or PR number.
- **SemVer** dictates which number bumps: **MAJOR** for incompatible/breaking changes, **MINOR** for backward-compatible additions, **PATCH** for backward-compatible fixes. A `Removed` or breaking `Changed` entry forces a MAJOR bump; anything under `Added` is at least MINOR.
- Link each version heading to its compare/diff view so a reader can jump from the summary to the commits.

## 11. Decision table: user need → doc type

Read the reader's sentence, match the verb, pick the mode. When a request spans two rows, that is two documents.

| The reader says / needs… | Write a… | Mode |
|---|---|---|
| "I'm new — teach me to get my first result" | Tutorial | learning |
| "Get me started fast" (in a project's front door) | README quickstart | learning (routing) |
| "How do I accomplish *this specific task*?" | How-to guide | task |
| "What are the exact params / flags / fields?" | API reference / docstring / config reference | information |
| "What does this endpoint return and how does it fail?" | API reference entry | information |
| "Why is it built this way? What were the trade-offs?" | Explanation | understanding |
| "Why did we choose X over Y, back then?" | ADR | understanding (immutable) |
| "What changed in the latest release?" | Changelog entry | information |
| "What is this project and should I use it?" | README | routing (multi-mode) |
| "Help me understand the whole system's shape" | Explanation + C4/architecture doc | understanding |

When in doubt, name the reader's verb — *learn*, *do*, *look up*, *understand* — and the mode falls out. If two verbs are present, you have found a place where a single page was trying to do two jobs; split it and cross-link, per the harness's single-responsibility discipline (see the sibling `loop-engine` skill's H1 pipeline-default: one stage, one job).
