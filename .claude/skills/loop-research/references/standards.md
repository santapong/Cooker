# Research Standards — authoritative frameworks for evidence and rigor

The named standards behind this skill's judgement calls. `methodology.md` gives the sweep → read → verify → synthesize procedure and `source-evaluation.md` gives the credibility checklist; this file pins the **established frameworks** those steps operationalize, so a reviewer can say "we rated this source with CRAAP" or "this review follows PRISMA" instead of appealing to taste. Apply from this file, not from memory — the mnemonics are stable but the reporting standards (PRISMA, OCEBM) carry edition numbers, and citing the wrong edition makes a review look sloppy.

Each entry names the framework and its issuing body, pins the current edition as of 2026, and maps it to a specific step of the methodology.

## Source-credibility: the CRAAP test

**Framework.** The **CRAAP Test** — Currency, Relevance, Authority, Accuracy, Purpose. **Issuing body:** Sarah Blakeslee, Meriam Library, California State University, Chico (introduced 2004). **Edition:** a stable pedagogical mnemonic, not a versioned standard; there is no numbered revision to track.

**Maps to Step 2 (deep-read & triage).** CRAAP is the per-source scorecard you run while extracting claims — it is the named form of the `source-evaluation.md` credibility checklist. Run all five before trusting a source's claims:

| Letter | Question | Kills a source when… |
|---|---|---|
| **Currency** | When was it published / updated? | undated on a time-sensitive topic, or stale relative to the question |
| **Relevance** | Does it actually address the sub-question, at the right depth and audience? | tangential or shallow |
| **Authority** | Who published it, and are they positioned to know? | anonymous, no credentials, content farm |
| **Accuracy** | Is it evidence-backed and traceable to a primary source? | unsourced assertions, uncheckable claims |
| **Purpose** | Why does it exist — inform, sell, persuade? | undisclosed commercial or advocacy incentive |

CRAAP judges a page **in isolation**; pair it with SIFT below, which judges a source by leaving it.

## Verification: SIFT and lateral reading

**Framework.** **SIFT — the Four Moves** (Stop, Investigate the source, Find better coverage, Trace claims to the original), built on the **lateral reading** research from the Stanford History Education Group (Wineburg & McGrew). **Issuing body:** Mike Caulfield (SIFT, 2019); lateral-reading evidence base from Stanford. **Edition:** current, unversioned method.

**Maps to Step 3 (adversarial verification).** SIFT is the *how* of refutation. Where CRAAP reads a source top-to-bottom, **lateral reading** means leaving the source and opening new tabs to check who is behind it and whether the claim holds up elsewhere — the operational move behind "corroboration" and "primary confirmation" in the methodology.

| Move | In this skill |
|---|---|
| **Stop** | before quoting, pause — do you know this source? is the claim load-bearing? |
| **Investigate the source** | read laterally: what do *other* sources say about this outlet/author, not what it says about itself |
| **Find better coverage** | seek independent, higher-quality corroboration; two outlets on one wire story count as one |
| **Trace** | follow the claim back to the **primary** source; secondary summaries routinely distort |

## Evidence hierarchy: levels of evidence

**Framework.** The **levels-of-evidence pyramid** — formalized as the **OCEBM Levels of Evidence**. **Issuing body:** Oxford Centre for Evidence-Based Medicine. **Edition:** the **2011 "Levels of Evidence 2"** table is the current published version; the concept generalizes beyond clinical questions to any evidence ranking.

**Maps to Steps 2–3 (extract and weight claims).** When two sources conflict, this is the tiebreak `source-evaluation.md` invokes as "primary and more-recent evidence outranks secondary and older." Rank the *evidence type* behind a claim, not the confidence of its author:

| Tier | Evidence type (research) | Generalized analogue (any topic) |
|---|---|---|
| **Strongest** | systematic review / meta-analysis | multiple independent primary sources in agreement |
| ↑ | randomized controlled trial | controlled experiment / benchmark with disclosed method |
| ↑ | cohort / observational study | field data, real-world usage at scale |
| ↑ | case series, single report | single anecdote, one deployment |
| **Weakest** | expert opinion, editorial | a pundit's take, a vendor's marketing |

A high-tier source outranks a low-tier one *on the same question*; note the tier when you record a claim so weighting is explicit at synthesis.

## Systematic-review reporting: PRISMA

**Framework.** **PRISMA — Preferred Reporting Items for Systematic reviews and Meta-Analyses.** **Issuing body:** the PRISMA Group, hosted by the **EQUATOR Network**. **Edition:** **PRISMA 2020** (published 2021), which superseded the original 2009 statement — current as of 2026.

**Maps to the whole methodology when depth = literature review.** PRISMA is the reporting discipline for the `templates/research.workflow.js` path: it makes a review **reproducible** by documenting how sources were found, screened, and excluded. The **PRISMA flow diagram** (records identified → screened → excluded with reasons → included) is the auditable form of the Step 5 completeness critic — it turns "we searched broadly" into a countable trail. Adopt its spirit, not clinical bureaucracy: record search angles used (Step 1), inclusion/exclusion reasons at triage (Step 2), and the final included-source count. This is how a market or literature review answers "what did you leave out and why."

## Certainty of a body of evidence: GRADE

**Framework.** **GRADE — Grading of Recommendations, Assessment, Development and Evaluation.** **Issuing body:** the **GRADE Working Group** (methodology maintained via the GRADE Handbook and GRADEpro). **Edition:** a continuously maintained methodology rather than a numbered edition; cite "current GRADE guidance."

**Maps to Step 4 (synthesize) — the confidence note.** Levels-of-evidence rates a *single* source; **GRADE rates the whole body** of evidence behind a conclusion, which is exactly what the "confidence & disagreement" section of the output report needs. Rate each headline conclusion **High / Moderate / Low / Very Low**, starting from the evidence tier and downgrading for:

- **Risk of bias** — weak methods or conflicted sources
- **Inconsistency** — sources disagree (report as *contested*, don't average away)
- **Indirectness** — evidence answers a nearby but different question
- **Imprecision** — small samples, wide ranges, single data point
- **Publication bias** — the sceptical/critical sweep angle came back empty (suspicious, not reassuring)

The GRADE rating is what distinguishes a verified-but-thin claim from a well-established one in the final synthesis.

## How this maps to the skill

| Step | Standard applied |
|---|---|
| 1 Sweep | PRISMA (record search angles for the audit trail) |
| 2 Read & triage | **CRAAP** (per-source score), levels-of-evidence (tier the source) |
| 3 Verify | **SIFT / lateral reading** (refute by leaving the source), levels-of-evidence (weight conflicts) |
| 4 Synthesize | **GRADE** (rate certainty of each conclusion) |
| 5 Completeness | PRISMA flow (what was excluded and why) |

## Edition discipline

Reporting standards get revised; the mnemonics do not. **PRISMA 2020 replaced the 2009 statement — cite the year, and never mix 2009 and 2020 checklists in one review.** OCEBM's current table is the **2011** revision. CRAAP, SIFT, and GRADE are living or unversioned — cite them by name and date-check on adoption. Re-verify the PRISMA and OCEBM editions on a roughly annual cadence (EQUATOR Network for PRISMA, CEBM for OCEBM); when a new edition lands, update this file and the map above before running the next review, and do not straddle editions within a single report. See `source-evaluation.md` for the credibility checklist these frameworks formalize and `methodology.md` for the step-by-step procedure they slot into.
