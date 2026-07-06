# Authoritative standards — the frameworks this skill leans on

The named, external references behind the house rules in the other files. When a design decision needs an authority to cite — in an ADR, a review, or a quality gate — pull it from here rather than from memory, and cite the **edition**. This skill's opinions (monolith-first, per-domain consistency, cache-aside) are house style; the standards below are the industry baselines those opinions are calibrated against. Each entry: the framework, its issuing body, the edition current as of 2026, and how it lands in this skill's workflow.

## Edition discipline (read first)

Standards get revised, and a mapping keyed to a stale edition quietly rots. Rules:

- **Cite the edition, always** — "ISO/IEC 25010:2023," not "ISO 25010." A characteristic count or a pillar name that shifted between editions makes a design doc look sloppy and breaks cross-review consistency.
- **Map to one edition per artifact.** Don't mix the ISO 25010:2011 eight-characteristic model with the 2023 nine-characteristic one inside a single NFR table (see the note under ISO/IEC 25010).
- **Re-check on a cadence** — roughly annually, and whenever an ADR cites a spec as load-bearing. OpenAPI 4.0 ("Moonwalk") and future TOGAF/AWS revisions are moving targets; the pins below are 2026-current.

## arc42 + TOGAF — architecture description and EA method

| Standard | Body | Edition (2026) | Role in this skill |
|---|---|---|---|
| **arc42** | arc42 (Starke/Hruschka), open | v8 (current) | Documentation **template** for step 8: 12 sections (context, constraints, solution strategy, building blocks, runtime, deployment, crosscutting, decisions, quality, risks). |
| **TOGAF Standard** | The Open Group | 10th Edition (2022) | Enterprise-architecture **method** (the ADM) — the wider governance frame when a design spans an org, not one system. |
| **C4 model** | Simon Brown | living (unversioned) | The **diagram** notation, already used in `templates/c4-context.md` / `c4-container.md`. |

**How they compose.** C4 gives the pictures; **arc42 gives the prose around them** — drop your C4 Context and Container diagrams into arc42 sections 3 and 5, and your ADRs into section 9. arc42 is the complementary template the C4 templates slot into. Reach for **TOGAF's ADM** only at enterprise scale (multiple systems, capability planning); for a single-system design it is heavier than the step-8 artifacts this skill emits. Cross-ref: `architecture-patterns.md` (style selection feeds arc42 §4 "solution strategy").

## ISO/IEC 25010 — the software product quality model

The **issuing body is ISO/IEC** (JTC 1/SC 7), under the **SQuaRE** family (ISO/IEC 2501n). It is the canonical taxonomy of *non-functional* quality — the vocabulary `nfr.md` operates in.

**Edition discipline here matters.** The widely-cited **2011** edition defined **8** product-quality characteristics. The current **ISO/IEC 25010:2023** revision reorganized to **9**: it added **Safety**, renamed *Usability → Interaction Capability* and *Portability → Flexibility*, and kept the rest. Map to **:2023** and note it; if a stakeholder's checklist still uses the 2011 eight, translate rather than mix.

| ISO/IEC 25010:2023 characteristic | Where it lives in `nfr.md` |
|---|---|
| **Performance Efficiency** | Latency SLIs, scalability, capacity — the "numbers" intake |
| **Reliability** | Availability math (nines), RTO/RPO, SLOs & error budgets |
| **Security** | Threat-modeled via the **loop-review** skill (not asserted) |
| **Maintainability** | Modularity, ADRs, build-vs-buy, operational surface |
| **Compatibility** | Interop / co-existence — API & event contracts (below) |
| **Interaction Capability** (was Usability) | Frontend UX — `frontend.md`, Core Web Vitals |
| **Flexibility** (was Portability) | Adaptability, install/replace — lock-in, IaC (`deployment.md`) |
| **Functional Suitability** | Does it meet the brief — the requirements intake gate |
| **Safety** (new in :2023) | Fail-safe / hazard limits — only for safety-relevant systems |

Use it as the **checklist axis** in step 7: for each characteristic that matters to the brief, name a number and a mechanism. It complements, not replaces, the AWS pillars below — 25010 is the vocabulary, Well-Architected is the review lens.

## Google SRE — SLI/SLO/error-budget rigor

Two books from **Google (O'Reilly)**: *Site Reliability Engineering* (2016, "the SRE book") and *The Site Reliability Workbook* (2018, the applied companion). Unversioned but stable; the workbook is the how-to.

This is the authority behind the **SLI → SLO → error-budget → burn-rate** apparatus in `nfr.md` step 7. Apply it literally: define an SLI as `good / valid events` measured where the user feels it, set the SLO just above user need and below 100%, spend the resulting error budget on releases, and page on **multi-window multi-burn-rate** alerts against the symptom, not the cause. When a design's reliability target is contested, cite the workbook's chapters on SLO engineering rather than arguing from taste.

## CAP + PACELC — the consistency trade-off theorems

Not committee standards but the **named theorems** the per-domain consistency policy in `backend.md` rests on — cite them so a trade-off reads as principled, not arbitrary.

| Theorem | Origin | What it forces |
|---|---|---|
| **CAP** | Brewer (2000), proved by Gilbert & Lynch (MIT, 2002) | Under a network **P**artition, choose **C**onsistency *or* **A**vailability — never both. |
| **PACELC** | Daniel Abadi (2010) | Completes CAP: **P**→**A**/**C**; **E**lse (no partition) → **L**atency/**C**onsistency. Even healthy, strong consistency costs latency. |

Use them to **classify each store and each domain**: Postgres synchronous replication is **PC/EC** (favors consistency, pays latency); a Dynamo-style store is **PA/EL**. Money/inventory/auth → the C columns; feeds/counts/search → the A/L columns. If you can't say how stale a read may be, the domain isn't designed. See `backend.md` "CAP and PACELC."

## OpenAPI + AsyncAPI — the contract standards

The machine-readable contract formats that make `api-design.md`'s "design the contract first" concrete. Both live under the **Linux Foundation**.

| Standard | Body | Edition (2026) | Covers |
|---|---|---|---|
| **OpenAPI Specification** | OpenAPI Initiative | **3.1.x** (3.1 aligns with JSON Schema) | Synchronous **request/response** APIs — REST/HTTP contracts. |
| **AsyncAPI Specification** | AsyncAPI Initiative | **3.0** (2023) | **Event-driven / message** contracts — Kafka, MQ, WebSocket channels. |

**Edition watch:** OpenAPI **4.0 ("Project Moonwalk")** is in development — stay on 3.1 until it ships and tooling catches up. **Map by interaction shape:** the synchronous API from step 4 gets an **OpenAPI** doc; the events and queues from `backend.md` (outbox, broker, event-driven style) get an **AsyncAPI** doc. Contract-first means the spec is the source of truth (codegen, mock, validate from it), and its versioning obeys the same additive-change discipline `api-design.md` prescribes.

## DDD, CQRS, and event-driven/event-sourcing patterns

Established **named patterns**, not standards bodies — cite the canonical sources so the vocabulary is unambiguous.

- **Domain-Driven Design (DDD)** — Eric Evans, *Domain-Driven Design* (2003); *bounded context*, *aggregate*, *ubiquitous language*. In this skill, the **bounded context is the unit of a service** (`architecture-patterns.md`) and the aggregate is the transaction/consistency boundary (`backend.md`).
- **CQRS** — Greg Young / Udi Dahan. Split the write model from the read model; see `backend.md` "CQRS." Reach for it only when one model genuinely can't serve both sides.
- **Event Sourcing** — persist the event log as the source of truth, derive state by replay. Pairs with CQRS and the **outbox**; justified for ledgers/audit where a replayable history is a requirement, not by default (see the event-driven failure modes in `architecture-patterns.md`).

These are the patterns behind steps 2–3; treat them as tools with a cost, invoked against a named requirement — the same premature-complexity discipline the skill applies everywhere.

## AWS Well-Architected Framework + Lenses

**Issuing body: AWS**; continuously updated (no fixed edition — cite the year you reviewed it). The **six pillars** — Operational Excellence, Security, Reliability, Performance Efficiency, Cost Optimization, **Sustainability** (added 2021) — are the review lens already indexed in `nfr.md`.

Beyond the pillars, the framework ships **Lenses** — pillar questions re-specialized for a workload class. Apply the lens that matches the system under design instead of the generic pillars alone:

| Lens | Use when designing… |
|---|---|
| **Serverless** | Lambda/functions-first architectures |
| **SaaS** | multi-tenant products (tenant isolation, per-tenant cost) |
| **Data Analytics** | pipelines, lakes, warehouses |
| **Machine Learning** | training/inference systems |
| **IoT / Financial Services / others** | domain-specific compliance and scale |

The pillars are cloud- and vendor-neutral in spirit — read "AWS" as "your platform." Run the design past the pillar questions in step 7, and pull the matching lens for the workload-specific ones; the point is to let a pillar's question *change the design*, per `nfr.md`'s warning against treating it as a compliance ritual.

## Re-check cadence

Re-verify these pins annually and whenever an ADR cites one as load-bearing. Live edition risks to watch: **OpenAPI 4.0** (Moonwalk), the next **ISO/IEC 25010** and **TOGAF** revisions, and rolling **AWS Well-Architected** updates. Update the table, re-map the affected cross-reference, and never mix editions inside one design doc.
