---
name: loop-research
description: Research a topic across multiple sources with adversarial fact-checking and a cited synthesis. Use when the user asks to research a topic, do a literature or market review, gather and verify evidence, answer a question that needs multiple sources, compare options with citations, or produce a fact-checked report — anything where breadth of sources and verified claims matter more than a single-shot answer.
---

# Researching Topics

You are about to research a question by gathering many sources, extracting claims, **verifying each claim adversarially**, and synthesizing a cited answer. The engine is breadth plus scepticism: an unsourced assertion is not a finding, and a claim that survives only because no one tried to refute it is not verified. **A confidently-wrong report is the failure mode this skill exists to prevent** — every reported claim carries a citation and has survived a refutation attempt.

## 1. Scope the question first

- If the ask is underspecified ("research the best database", "what's happening with X"), narrow it before searching: pin down the decision, the constraints, the timeframe, and the audience. Ask 2–3 clarifying questions, or state the assumptions you are researching under so the user can correct them.
- Decompose the question into **sub-questions**. Research each sub-question; a good report answers the decomposition, not just the headline.
- Decide depth: a quick fact-check needs a handful of sources; a literature or market review needs a broad sweep and a completeness pass (§3).

## 2. Execution model: evidence-first, every claim cited

- Use whatever retrieval is available — web search and fetch, and any research MCP tools present (e.g. an academic-paper server for scholarly questions). Do not answer from memory on anything time-sensitive, contested, or numeric; retrieve and cite.
- Separate **fact** (verifiable, citeable) from **claim** (asserted by a source) from **opinion** (a source's judgement). Report facts as facts, attribute claims to their source, and flag opinion as opinion.
- Every claim in the final report carries a source. A claim you cannot source is dropped, not softened.

## 3. Methodology: sweep → read → verify → synthesize

Full procedure in **`references/methodology.md`** — read it before a non-trivial research task.

1. **Multi-modal sweep** — search several *different ways*, not the same query reworded: primary/official sources, recent developments, sceptical/critical takes, and quantitative data/studies. Each angle is blind to what the others surface, which is how you avoid a monoculture of agreeing sources.
2. **Deep-read & extract** — read each promising source and extract its concrete claims with the supporting evidence and the source URL. Triage low-quality sources out here (§4).
3. **Adversarial verify** — for each claim, try to *refute* it: check it against the other sources, look for contradiction, primary-source confirmation, or recency problems. Keep only claims that are corroborated or primary-sourced; default to dropping a claim you cannot stand up.
4. **Synthesize** — write the answer from verified claims only, citing inline. Surface disagreement between sources rather than papering over it.
5. **Completeness critic** — before finishing, ask "what's missing — a sub-question unanswered, a source type not searched, a claim still unverified?" What it finds is the next round of searching or an explicit stated gap.

## 4. Source evaluation & citations

Judge every source before trusting it — authority, primary vs secondary, recency, corroboration, and bias — per **`references/source-evaluation.md`**. When sources conflict, prefer primary and more-recent evidence, and report the disagreement instead of silently picking a side. Cite consistently (title + URL + date) so a reader can check any claim.

## 5. Orchestration: run a broad sweep as a workflow

A narrow fact-check you can do inline in this session. For **a broad or multi-source review, run it as a multi-agent workflow** using the template at **`templates/research.workflow.js`**:

1. **Search fan-out** — one searcher per angle (§3.1), in parallel, each returning candidate sources.
2. **Dedup barrier** — wait for all searchers, merge and dedup sources by URL in plain script logic.
3. **Read → verify pipeline** — per source, deep-read and extract claims, then adversarially verify each claim as soon as its source is read (no barrier between sources).
4. **Synthesis** — one agent writes the cited report from the verified claims.

This is the parallel-sweep → pipeline → synthesis pattern from the **`loop-engine`** skill (see its `templates/parallel.workflow.js`, `templates/pipeline.workflow.js`, and harness policy H2/H4). Invoke the `loop-engine` skill to author and execute the run; the research template pre-wires the search angles, claim schema, and adversarial verification. For a question small enough to answer from two or three sources, skip the workflow and research directly.

## 6. Output: a cited report

Deliver: a direct answer to the question up top, then the supporting sections (one per sub-question), each claim cited inline; a short "confidence & disagreement" note where sources conflict; and an explicit "open questions / gaps" list from the completeness critic. Match length to the ask — a fact-check is a paragraph, a review is a structured report.

## Reference files

- `references/methodology.md` — the sweep → read → verify → synthesize procedure in full, with the completeness critic
- `references/source-evaluation.md` — source credibility, handling contradictory sources, and citation format
- `references/standards.md` — the authoritative standards this skill applies — named, version-pinned, and mapped to its workflow
- `templates/research.workflow.js` — search fan-out → dedup → read/verify pipeline → synthesis workflow script
