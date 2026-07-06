# Root-Cause & Debugging Standards — the authoritative reference

The established frameworks behind this skill's reproduce → localize → root-cause → fix loop. `methodology.md` tells you *how* to run each step and `hypothesis-testing.md` *how* to eliminate candidate causes; this file names the **authoritative standard** each step rests on, pins the edition to cite, and maps it to where it earns its keep in the workflow. Cite the standard by name and edition when you record a diagnosis, so a root cause reads as a defensible finding rather than a hunch. When a standard has a numbered edition, pin it — techniques get revised (see the edition-discipline note at the end).

## 5 Whys — iterative causal drill-down

**Framework.** The *5 Whys* technique, originated at **Toyota** (Sakichi Toyoda) and codified in the **Toyota Production System** by Taiichi Ohno. It is a technique, not a numbered standard — there is no version to pin; cite it as "Toyota / TPS 5 Whys."

**Maps to the skill.** This is the discipline behind **§5 Root cause, not symptom**: keep asking "why did *that* happen?" down the causal chain until you reach a cause you can act on, not the surface symptom. The "five" is a rule of thumb, not a quota — stop when toggling the cause turns the bug on and off (`methodology.md` Step 5), not at a fixed count. **Caution:** a single 5 Whys chain assumes one linear cause and invites confirmation bias; when several independent causes are plausible, drive it under the *set*-of-hypotheses discipline in `hypothesis-testing.md` §1 instead of a single chain.

## Ishikawa (fishbone) diagram — cause categorization

**Framework.** The **Ishikawa cause-and-effect diagram** (a.k.a. fishbone), introduced by **Kaoru Ishikawa** and standardized as one of the *Seven Basic Tools of Quality*. Classic spine categories are the **6 Ms** (Method, Machine, Material, Measurement, Man/People, Milieu/Environment); software adaptations substitute code, config, data, dependencies, environment, and concurrency.

**Maps to the skill.** The fishbone's category spines are the ancestor of the **bug-class checklist** in `methodology.md` Step 4 and the "generate a *set*, not a favorite" rule in `hypothesis-testing.md` §1 — both force breadth across cause categories before you commit to one. Use it as a coverage check: for a hard bug, sweep each spine (did config drift? a dependency version? a race? stale cache?) so no whole category of cause goes unexamined.

## Orthogonal Defect Classification (ODC) — defect taxonomy

**Framework.** **Orthogonal Defect Classification**, developed at **IBM Research** (Ram Chillarege et al.). ODC tags each defect with orthogonal attributes — most usefully **Defect Type** (assignment, checking, algorithm, timing/serialization, interface, function…) and **Trigger** (what surfaced it). Cite it as "IBM ODC (current published attribute set)"; the attribute taxonomy is the stable artifact, so name the attributes rather than a version number.

**Maps to the skill.** ODC is the classification vocabulary for the **regression test and report** at **§6**. The ODC **Trigger** is precisely the reproduction trigger captured in **§1**; the ODC **Defect Type** is the bug class identified in `methodology.md` Step 4. Recording both makes a fixed bug analyzable in aggregate (which defect types cluster, which triggers your tests miss) rather than a one-off note.

| ODC Defect Type | Corresponding bug class (`methodology.md` Step 4) |
|---|---|
| **Assignment / Initialization** | Null / undefined, state mutation |
| **Checking** | Off-by-one, boundary / empty input, missing guard |
| **Algorithm / Method** | Wrong logic, type coercion |
| **Timing / Serialization** | Async / race / ordering |
| **Interface** | Dependency version, contract mismatch |
| **Function / Configuration** | Config / env drift, caching / staleness |

## Fault Tree Analysis (FTA) — causal-chain modeling

**Framework.** **Fault Tree Analysis**, standardized internationally as **IEC 61025** (current edition **IEC 61025:2006**, Ed. 2.0) and documented for practitioners in the **NASA Fault Tree Handbook with Aerospace Applications** (2002). FTA models a **top event** (the observed failure) decomposed through **AND/OR gates** down to **basic events** (root causes).

**Maps to the skill.** FTA is the formal shape of the causal chain you narrate in **§5** and, more importantly, the model behind **§8 orchestration: many candidate causes**. An **OR gate** = independent causes that each alone produce the failure → fan out one hypothesis agent per branch (`hypothesis-testing.md` §7). An **AND gate** = a failure that needs several conditions to coincide (the classic race or config-plus-input bug) → no single toggle will switch it, so test the conjunction. Drawing even a two-level tree keeps you from stopping at the first branch that fits.

## Delta debugging & scientific debugging — Zeller

**Framework.** Andreas **Zeller**, *Why Programs Fail: A Guide to Systematic Debugging* (**2nd edition, 2009**, Morgan Kaufmann) — the reference text for the scientific-method framing this whole skill uses. Its **delta debugging** algorithm (`ddmin`, Zeller & Hildebrand, *Simplifying and Isolating Failure-Inducing Input*, IEEE TSE 2002) systematically minimizes a failing input and isolates the failure-inducing change.

**Maps to the skill.** Two direct anchors. (1) **`ddmin` is the algorithm behind "minimize the repro"** in **§1** / `methodology.md` Step 1 — bisect the *input* to the smallest failing case, don't trim by intuition. (2) The **`git bisect` regression hunt** in **§3** is delta debugging applied to the change history: `git bisect run` *is* the automated delta search over commits. Cite Zeller when you attribute the reproduce → hypothesize → eliminate loop and the bisection technique it already uses.

## OpenTelemetry — observability standard for evidence

**Framework.** **OpenTelemetry (OTel)**, the **CNCF** observability standard unifying the three signals — **traces, metrics, and logs** — over the **OTLP** wire protocol. Pin to the **OpenTelemetry Specification 1.x**; the tracing, metrics, and logs signals and OTLP are all at stable maturity in the current release. Cite "OpenTelemetry Specification, current stable (1.x)."

**Maps to the skill.** OTel is the evidence-gathering standard for **§2 Read the evidence** and the instrumentation in `methodology.md` Step 3. A distributed **trace** reconstructs the request path so you can walk the stack backward to the first frame you own (§2); **span attributes and events** are the structured instrumentation that makes an invisible intermediate state observable without adding ad-hoc `print` lines; correlated **logs** let you diff a good run against a bad run (Step 2) by trace id. When a bug spans services, an OTel trace is the localization tool — the distributed analogue of a single-process stack trace.

## How the standards map to the skill's steps

| Skill step | Authoritative standard |
|---|---|
| **§1 Reproduce** (minimize) | Delta debugging `ddmin` (Zeller) |
| **§2 Read the evidence** | OpenTelemetry 1.x (traces / metrics / logs) |
| **§3 Localize** (bisect) | Delta debugging over history (Zeller; `git bisect`) |
| **§4 Hypothesize** (generate a set) | Ishikawa fishbone categories |
| **§5 Root cause** (causal chain) | 5 Whys; Fault Tree Analysis (IEC 61025:2006) |
| **§6 Fix + regression test** (classify) | IBM ODC (Defect Type + Trigger) |
| **§8 Many candidate causes** | FTA OR/AND gates → parallel fan-out |

## Edition discipline

Standards get revised; cite the edition you mapped to and re-check on a cadence.

- **FTA is pinned to IEC 61025:2006 (Ed. 2.0)** — the current international edition. If a newer edition supersedes it, update the citation before treating gate semantics as authoritative.
- **Zeller is pinned to the 2nd edition (2009)**; the `ddmin` paper is 2002 and unchanged. These are stable; no imminent revision.
- **OpenTelemetry is a living CNCF spec (1.x)** — signals graduate and the spec versions frequently. Confirm which signals are *stable* (vs. experimental) at the version your tooling runs before relying on them.
- **5 Whys and Ishikawa are unversioned techniques** — cite by name and originating body, not a version number.
- **ODC's attribute taxonomy is the stable artifact** — name the Defect Type and Trigger attributes rather than a release number, which IBM does not publish as a single canonical version.

Re-verify these editions on a periodic cadence (mirror the re-check discipline in `../../loop-review/references/owasp-cwe.md`), and do not mix editions of the same standard within one diagnosis.
