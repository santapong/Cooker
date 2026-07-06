# Change-Audit Standards — the authoritative frame

The audit in `methodology.md` runs on a homegrown model: classify each change, trace its blast radius, score it on likelihood × impact, and check coverage. That model is not invented from scratch — each moving part maps to an established, real-world standard. This file names those standards, pins the edition to key against, and shows where each one lands in the five-step method. Cite the edition, not memory; standards get revised on their own cadence (see the closing note).

Use this file to *justify* the audit's structure to a skeptical reader ("why this risk matrix? why call this breaking?") and to keep the report's vocabulary aligned with frameworks a release manager already trusts.

## ISO 31000 — the risk model behind Step 4

**Standard:** ISO 31000, *Risk management — Guidelines*. **Issuing body:** International Organization for Standardization (ISO). **Edition to pin:** **ISO 31000:2018** (current edition; it superseded the 2009 first edition and has no announced successor as of 2026).

ISO 31000 defines risk as the *effect of uncertainty on objectives* and frames its assessment as three moves: **identify → analyse → evaluate**. That is exactly Step 4's job. The skill's homegrown "likelihood × impact" scoring is the standard **risk matrix** that ISO 31000 and its companion **IEC 31010:2019** (*Risk management — Risk assessment techniques*) formalize — map the method's two axes onto it:

| Step 4 axis | ISO 31000 term | What the audit reads it from |
|---|---|---|
| **likelihood** | probability of the risk event | complexity, coverage (Step 5), concurrency/state/IO, how well-understood the code is |
| **impact** | consequence / severity | blast radius (Step 3), reversibility, public/data-surface reach |
| **rating** (low/med/high/critical) | risk level = f(likelihood, consequence) | the matrix cell the two axes select |

The four-level Low/Medium/High/Critical rating the report assigns on a likelihood × impact basis is a risk matrix in the IEC 31010:2019 sense; the 4×4 likelihood-by-impact grid used to select a cell is this file's formalization of that scale, not a structure the sibling files spell out. When a reader asks why a small diff scored "critical," answer in this vocabulary: low likelihood but severe, irreversible consequence still lands in a high-risk cell — consequence is not outranked by probability. ISO 31000's **treatment** step (avoid / mitigate / accept) is where the report's rollback note and follow-up recommendations belong.

## DORA change failure rate — why the audit exists

**Standard/source:** DORA (**DevOps Research and Assessment**) Four Keys, published in the annual *State of DevOps Report* and *Accelerate*. **Issuing body:** the DORA research program (now under Google Cloud). **Edition to pin:** the **most recent annual State of DevOps Report** (published each fall — verify the latest year before citing any benchmark numbers; a fresh annual edition supersedes its predecessor, so re-check the year rather than assuming the one you remember is current).

Of DORA's four keys, one *is* this skill's reason for existing: **change failure rate** — the share of changes to production that result in degraded service and require remediation (hotfix, rollback, patch). The audit is the pre-merge instrument that drives that number down: every change flagged breaking, irreversible, or untested is a change failure caught before it ships.

| DORA key | Relation to this skill |
|---|---|
| **Change failure rate** | The metric the audit directly targets — Steps 2, 4, 5 exist to predict and prevent it. |
| **Failed deployment recovery time** | Informs the **rollback note** (Step 4): reversibility is a first-class risk factor. |
| Deployment frequency / lead time | Context, not the audit's job — do not let audit rigor be used to argue for slowing delivery; the goal is *safe* throughput. |

Frame the report's overall risk verdict in this light: "this change set raises change-failure risk on N surfaces, M of them irreversible" speaks the language leadership already tracks.

## Semantic Versioning — the impact-classification standard

**Standard:** Semantic Versioning. **Issuing body / author:** the SemVer specification (originated by Tom Preston-Werner). **Edition to pin:** **SemVer 2.0.0** (stable; the specification is versioned and 2.0.0 is current).

SemVer is the objective backbone of Step 2's classification and Step 3's blast-radius call. It gives "breaking" a definition that isn't a matter of taste:

| SemVer bump | Meaning | Maps to the audit |
|---|---|---|
| **MAJOR** | backward-incompatible public-API change | the **breaking** class — removed/renamed API, changed signature, changed default, schema/wire migration |
| **MINOR** | backward-compatible new functionality | **feature** — new surface, additive |
| **PATCH** | backward-compatible bug fix | **fix** — corrects behavior without contract change |

The rule to carry into the report: **blast radius is bounded by the smallest correct SemVer bump.** A change that forces a MAJOR bump can break *every* consumer by definition, which is why Step 2 says flag it loudly regardless of diff size. When the audit and the author's proposed version bump disagree — author calls it a patch, the trace shows a changed default — that disagreement is itself a headline finding.

## ITIL change enablement — the change-type frame

**Standard:** ITIL (IT Infrastructure Library), *Change Enablement* practice. **Issuing body:** AXELOS / PeopleCert. **Edition to pin:** **ITIL 4** (current; it renamed the v3 "Change Management" process to **Change Enablement**).

ITIL 4 classifies changes into three types. Stamping each audited change set with its ITIL type tells the reader *what review path it should travel* and calibrates how much scrutiny the audit itself owes it:

| ITIL 4 change type | Definition | Audit posture |
|---|---|---|
| **Standard** | pre-authorized, low-risk, routine (well-trodden path) | light audit; confirm it truly fits the standard template and hasn't drifted |
| **Normal** | must be assessed and authorized before deployment | the full Steps 1–5 pass; this is the audit's default subject |
| **Emergency** | needed as fast as possible (e.g. outage fix) | audit still runs but records the compressed path and any deferred coverage as explicit debt |

ITIL frames the audit's role as feeding a **Change Advisory** decision: the report is the evidence a change authority (human or automated gate) uses to authorize, defer, or reject. That is why `report-template.md` leads with an overall verdict and rollback note — those are the fields a change authority acts on. Map an "emergency" type onto Step 4's ranking by noting that speed traded away assessment is itself a risk factor to surface, not hide.

## How this maps to the skill

Read this file alongside **`methodology.md`** — it supplies the *why* behind that file's *how*. In short: SemVer defines **breaking** (Step 2), ISO 31000 / IEC 31010 supply the **likelihood × impact matrix** (Step 4), DORA's **change failure rate** names the outcome the whole audit is trying to move, and ITIL's **change types** tell the reader which review path the change set belongs on. The report in **`report-template.md`** is the Change-Advisory evidence package these standards frame.

## Edition discipline

Standards revise on their own schedules; a citation without an edition rots. Pin and re-check:

- **ISO 31000:2018** — stable, no announced successor; re-check the ISO catalogue on a yearly cadence.
- **IEC 31010:2019** — the current risk-assessment-techniques edition (superseded the 2009 first edition); re-check alongside ISO 31000.
- **DORA** — the *State of DevOps Report* is **annual** with a fall release, so a newer edition than the one you remember may already be current; do not assert a specific year is "the most recent." Benchmark thresholds (elite/high/medium/low) shift year to year, so verify the latest published year and quote it before citing a number, not just the metric name.
- **SemVer 2.0.0** — stable spec; unlikely to move, but confirm the major version if you cite clause text.
- **ITIL 4** — current framework; note that older reports may reference the v3 term "Change Management" — treat it as the same practice, renamed, and do not present the two as separate standards.

Cite the edition you mapped to in the report's methodology footnote. When any of these publishes a new edition, update the mapping tables here in one pass rather than mixing editions across a single audit — the same discipline `loop-review`'s `owasp-cwe.md` applies to the OWASP Top 10 refresh.
