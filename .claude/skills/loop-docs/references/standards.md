# Authoritative Standards — the frameworks this skill writes to

The external standards behind the practices in this skill. `doc-types.md` tells you *which* doc to write and `style.md` tells you *how* to word it; this file names the published frameworks those two files implement, **pins the edition you should cite as of 2026**, and maps each one to a step in the SKILL workflow. Cite the edition, not "the standard in general" — editions get revised and a stale citation dates the doc (see **Edition discipline** at the end).

Reach for these to justify a convention, to settle a house-style dispute, or to configure tooling — not to pad a doc with standards-name-dropping. When a repo already commits to one (a Vale config, a `CONTRIBUTING.md` naming a style guide), that choice wins over the defaults here; match it, per SKILL §3.

## The standards at a glance

| Standard | Issuing body | Edition to cite (2026) | Where it bites in this skill |
|---|---|---|---|
| **Diátaxis** | Daniele Procida (open framework) | Living framework, continuously revised | §1 pick the type — see `doc-types.md` |
| **Google developer documentation style guide** | Google | Online, continuously updated | §4 draft prose — see `style.md` |
| **Microsoft Writing Style Guide** | Microsoft | Online, continuously updated | §4 draft prose — see `style.md` |
| **CommonMark** | CommonMark community | Spec **0.31.2** (pre-1.0) | §3 match format; every Markdown file |
| **Conventional Commits** | open spec (conventionalcommits.org) | **1.0.0** | §6 sync; feeds the changelog |
| **MADR** (Markdown ADR) | ADR community | **4.x** (current major) | ADR authoring — `doc-types.md §9` |
| **Vale** | Vale / errata-ai | **3.x** (CLI) | §6 sync; §7 verify stage enforcement |
| **ISO/IEC/IEEE 26514, ISO/IEC 82079-1** | ISO/IEC/IEEE | **26514:2022**, **82079-1:2019** | Process/governance framing (name-level) |

## Diátaxis — the doc-type model

**Diátaxis** (Daniele Procida) is the core taxonomy this skill classifies by: four modes on the study/work × practical/theoretical axes — tutorial, how-to, reference, explanation. It is a **living framework** with no version number; it is revised in place, so cite it as "the Diátaxis framework" and re-read the source periodically rather than pinning an edition. In this skill it drives **SKILL §1 (pick the type first)**; the full mode definitions, skeletons, and the single-mode rule live in **`doc-types.md`** — apply it there, this entry only names the source.

## Prose style: Google and Microsoft

Two published house styles supply the sentence-level rules that `style.md` codifies. Both are **online and continuously updated** (no dated edition to pin); cite them by name and re-check the live word lists.

- **Google developer documentation style guide** — the default for developer-facing, API, and CLI docs. Concrete on second person, present tense, sentence-case headings, and code-formatting conventions — the same defaults `style.md §4` mandates. Its **word list** settles term-by-term casing/spelling disputes.
- **Microsoft Writing Style Guide** — successor to the *Microsoft Manual of Style* (the 4th edition, 2012, was the last print run). Strong on accessibility, inclusive language, and UI-text/voice guidance; reach for it for product-UI and end-user prose.

Pick **one** per repo and apply it uniformly (SKILL §3) — do not blend Google and Microsoft rulings in one doc set. When neither is adopted, state which you follow, per SKILL §3.

## CommonMark — the Markdown spec

**CommonMark** is the unambiguous specification of Markdown itself — the format most docs in this skill are written in. Pin **spec version 0.31.2**; note it is deliberately **pre-1.0**, so features stabilize slowly and the version string moves rarely. Most toolchains extend it: **GitHub Flavored Markdown (GFM)** adds tables, task lists, strikethrough, and autolinks on top of CommonMark. In this skill CommonMark governs **SKILL §3 (match the repo's format)** — before using a table or a footnote, confirm the repo's renderer (MkDocs, Docusaurus, plain GitHub) supports that extension, because a construct valid in GFM may not parse under strict CommonMark. The mode and decision tables in `doc-types.md §1`/§11 and the fenced-code examples in `style.md §3` assume GFM.

## Conventional Commits — the changelog feed

**Conventional Commits 1.0.0** is a lightweight convention for commit-message structure: `type(scope): description`, with `feat:`, `fix:`, and a `!` / `BREAKING CHANGE:` marker carrying semantic meaning. It is the machine-readable input that lets tools auto-generate the **changelog** and pick the **SemVer** bump described in `doc-types.md §10` (Keep a Changelog): `feat:` → MINOR, `fix:` → PATCH, breaking → MAJOR. In this skill it lands in **SKILL §6 (keep docs in sync)** — when a repo follows Conventional Commits, the changelog is a *product* of the commit history, not a hand-curated list, and your job shifts from writing entries to verifying the generated ones read for humans.

| Commit type | Changelog heading (`doc-types.md §10`) | SemVer bump |
|---|---|---|
| `feat:` | Added | MINOR |
| `fix:` | Fixed | PATCH |
| `feat!:` / `BREAKING CHANGE:` | Changed / Removed | MAJOR |
| `docs:`, `refactor:`, `chore:` | (usually omitted from user changelog) | none |

## MADR — a concrete ADR format

`doc-types.md §9` teaches the ADR *shape* from Nygard's original template. **MADR** (Markdown Any/Architecture Decision Records) is a concrete, widely adopted file format for that shape — pin the **current major, 4.x**. It fixes a filename scheme (`NNNN-title-with-dashes.md`), a `Status`/`Deciders`/`Date` header, and named sections including **Considered Options** and **Decision Outcome** with per-option pros/cons — the rejected-alternatives discipline that `doc-types.md §9` calls load-bearing. Use MADR when a repo wants a standard tool-parseable ADR layout; the fill-in template stays in `../../loop-design/templates/adr-template.md`, which this skill's `loop-design` sibling emits.

## Vale — style-as-code enforcement

**Vale 3.x** is the prose linter that turns the two house styles above into **executable rules** ("style as code"): it checks Markdown against configurable rule packages, including maintained Vale packages for the **Google** and **Microsoft** style guides, plus `write-good`, `proselint`, and `alex`. It belongs to **SKILL §6/§7**: wire Vale into CI so drift from the adopted style fails a check, and use it as the automated first pass of the **verify stage** in the `templates/doc-generation.workflow.js` pipeline before the adversarial accuracy review (SKILL §7). Vale checks *style*, never *truth* — it cannot catch an aspirational or wrong claim, which is why the human/agent accuracy verification of SKILL §5 stays mandatory.

## ISO/IEC documentation-process standards (name-level)

For regulated, safety-critical, or contract-governed documentation, the formal process standards apply. Name them; consult the actual text when a project is bound by one — do not paraphrase requirements from memory.

- **ISO/IEC/IEEE 26514:2022** — *Design and development of information for users*. The core standard for the docs process (planning, structure, review, delivery); part of the **ISO/IEC/IEEE 265xx** family (26511 management, 26515 agile, 26513 testing/reviewing).
- **ISO/IEC 82079-1:2019** — *Preparation of information for use (instructions for use) of products — Part 1: Principles and general requirements*. Governs instructions-for-use, common in hardware, medical, and CE-marked products.

These shape *governance* (what the process must guarantee), not sentence-level wording — that is Google/Microsoft's job. If a project cites one as a compliance requirement, treat it as a constraint on SKILL §2–§3 (audience, conventions) and verify against the standard itself.

## Edition discipline

Standards get revised; a citation is only as good as its pinned edition. Rules:

- **Cite the edition you mapped to**, as in the glance table — "CommonMark 0.31.2", "Conventional Commits 1.0.0", "ISO/IEC/IEEE 26514:2022" — never a bare name. This mirrors the version-stamping `style.md §7` demands of any time-bound fact.
- **Do not mix editions** inside one doc set. If the repo adopts a newer Vale package or a superseded ISO edition lands, migrate the whole set and update this file, rather than half-tagging.
- **Living frameworks** (Diátaxis, the Google and Microsoft guides) have no edition to pin — cite them by name and **re-read the source on a cadence**, because they change silently without a version bump.
- **Re-check this file periodically** (a yearly pass is reasonable). When you confirm an edition, stamp the check. Watch specifically for a CommonMark 1.0, a new MADR major, and any ISO 265xx revision.

Verified against published editions as of **2026-07**.
