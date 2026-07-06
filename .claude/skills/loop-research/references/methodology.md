# Research Methodology

The full procedure behind the `loop-research` skill: turn a question into a cited, fact-checked answer through a multi-modal sweep, deep reading, adversarial verification, and a completeness pass. The orchestration shape (parallel sweep, read/verify pipeline, synthesis) is governed by the `loop-engine` skill's harness and loop policies.

## Step 0 — Decompose the question

Break the headline question into sub-questions before searching. "Should we adopt X?" decomposes into: what is X, what are the alternatives, what does it cost, who uses it and at what scale, what are the known failure modes, how mature is it. Research targets the sub-questions; the final report is organized around them. Write the decomposition down — it is also the checklist the completeness critic (Step 5) audits against.

## Step 1 — Multi-modal sweep

Search several *different ways*, not one query reworded. Reworded queries return the same cluster of agreeing pages; distinct angles surface disagreement and primary evidence. Run at least these angles:

- **Primary / authoritative** — official docs, standards, source data, the thing itself (a paper, a spec, a filing).
- **Recent developments** — news, changelogs, releases; anything time-sensitive. Note dates.
- **Critical / sceptical** — known problems, criticisms, post-mortems, "X considered harmful", failure stories. This angle is the one people skip and the one that prevents a rosy monoculture.
- **Quantitative** — benchmarks, studies, datasets, surveys with numbers and methodology.

Each angle is blind to the others' results. For scholarly questions, use an academic-paper MCP tool if one is available rather than general web search. Collect candidate sources as `{url, title, why-promising}`; do not read deeply yet.

## Step 2 — Deep-read & extract claims

Read each promising source and pull out its concrete **claims**, each with the evidence it rests on and its source URL: `{claim, evidence, sourceUrl, date}`. While reading, triage: drop content-farm, SEO-spam, undated, or circular-citation sources here (see `source-evaluation.md`). Distinguish what the source *shows* (primary evidence) from what it *asserts* (a claim to be checked) from what it *thinks* (opinion to attribute).

## Step 3 — Adversarial verification

For each claim, try to **refute it**, not confirm it. Default to *unsupported* unless it clears the bar:

- **Corroboration** — is it independently stated by another credible source? Two sources citing the same origin are one source; look for genuine independence.
- **Primary confirmation** — does the primary source actually say this? Secondary sources routinely distort.
- **Contradiction** — does any credible source dispute it? If so, the claim is "contested", not "verified".
- **Recency** — is it current, or superseded? Stamp claims with their as-of date.

Keep only claims that are corroborated or primary-sourced. Mark contested claims as contested and report both sides. A claim you cannot stand up is dropped. When a workflow runs this at scale, use independent verifier agents prompted to refute (harness policy H4); a claim a majority of skeptics can't refute survives.

## Step 4 — Synthesize

Write the answer from the surviving claims only. Lead with a direct answer to the headline question, then a section per sub-question with claims cited inline. **Surface disagreement** — where sources conflict, say so and characterize each side, rather than silently choosing. Do not introduce new unsourced assertions in synthesis; if the write-up needs a claim you didn't verify, that's a new research loop, not a guess.

## Step 5 — Completeness critic

Before finishing, audit against the Step 0 decomposition: which sub-questions are answered, which source types were never searched, which claims are still unverified or contested? Anything material becomes another sweep. What remains genuinely unknowable after that is reported as an explicit **open question / gap**, not omitted — a silent gap reads as "covered" when it wasn't.

## Depth control

- **Fact-check / narrow question** — a handful of sources, single verification pass, a paragraph of answer. Do it inline; don't spin up a workflow.
- **Literature / market review** — broad sweep across all angles, per-claim verification, completeness pass, structured report. Run it as the `templates/research.workflow.js` workflow. If discovery keeps surfacing new material, loop the sweep until two consecutive rounds add nothing new (loop-until-dry, loop policy L1) rather than stopping at a fixed source count.
