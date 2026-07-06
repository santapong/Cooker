# Testing Standards — the sources this skill applies

The named frameworks, taxonomies, and quality standards behind the techniques in `test-design.md` and the SKILL steps. `test-design.md` tells you *how* to design a case; this file names the **authoritative source** each technique comes from, pins the edition to cite, and maps it to the step where you apply it. Attribute from this file, not from memory — the taxonomies below (especially the double kinds) are precise, and a mislabeled "mock" that is actually a stub trains the whole team to reason wrong.

## The test-double taxonomy — Meszaros

The five-kind double vocabulary in `test-design.md` §2 is not folklore; it is **Gerard Meszaros, *xUnit Test Patterns: Refactoring Test Code*** (Addison-Wesley, **2007** — still the canonical edition; no revision issued). Meszaros coined the umbrella term **Test Double** and the sub-taxonomy the whole industry now cites.

| Meszaros term | Definition (verbatim intent) | Where you apply it |
|---|---|---|
| **Dummy** | object passed but never used | filling a signature — SKILL §4 |
| **Stub** | provides canned answers to indirect inputs | driving a path — `test-design.md` §2 |
| **Spy** | a stub that also records the calls made to it | verifying an outbound effect after the fact |
| **Mock** | pre-loaded with expectations, verifies interaction | interaction-is-the-contract cases only |
| **Fake** | a working but lightweight implementation | in-memory repo / fake clock |

Reproduce these names exactly when you justify a double in review. Meszaros's own guidance — **prefer state verification (stub/fake) over behavior verification (mock)** — is the source of the "prefer a fake or a stub over a mock" rule in `test-design.md` §2; cite it when someone over-mocks.

## Test-first discipline — Beck (TDD)

**Kent Beck, *Test-Driven Development: By Example*** (Addison-Wesley, **2002**; the foundational text, not revised) is the source of the **red → green → refactor** cycle. This skill applies its *red* step literally: SKILL §5.3 requires a regression test to **fail on the unfixed code first**. That is Beck's discipline — a test that was never red proves nothing. When you add a bug-fix regression test, you are running one turn of the TDD loop even if the production code already exists.

## FIRST principles — the unit-test quality bar

**FIRST** (Ottinger & Schuchert, Object Mentor; popularized via Robert C. Martin's *Clean Code*, 2008) names five properties every unit test must hold. They map one-to-one onto `test-design.md` §3 and SKILL §4:

| FIRST | Meaning | Skill rule it anchors |
|---|---|---|
| **F**ast | milliseconds; slow suites run less | §3.3 / SKILL §4 fast-and-isolated |
| **I**ndependent | no order or shared-state coupling | §3.4 isolated |
| **R**epeatable | same result every run/machine | §3.2 deterministic — inject clock & RNG |
| **S**elf-validating | pass/fail with no human inspection | §3 assert on outcomes, not logs |
| **T**imely | written with (or just before) the code | Beck's test-first, SKILL §5.3 |

Cite FIRST as the checklist name when explaining *why* a flaky or order-dependent test must be fixed rather than tolerated.

## Suite shape — the Pyramid (Cohn) and the Trophy (Dodds)

Two named models for **how to distribute tests across tiers** — the tiering decision behind `test-design.md` §3 (keep slow integration tests in a separate tier) and SKILL §7 orchestration.

- **Testing Pyramid** — **Mike Cohn, *Succeeding with Agile*** (Addison-Wesley, **2009**). Many fast unit tests at the base, fewer service/integration tests, fewest slow end-to-end tests at the tip. The default shape for logic-heavy code.
- **Testing Trophy** — **Kent C. Dodds** (2018, current formulation via testingjavascript.com). Weights **integration** tests heaviest, on top of a static-analysis/type base, reflecting UI and I/O-bound systems where unit isolation buys little confidence.

Name the model you are matching to the codebase: pyramid for a computation/domain core, trophy for a UI or glue-heavy app. Neither is a law — both exist to push the *slow, brittle* tests to the minority.

## Behavior specs — BDD and Gherkin (Cucumber)

**Behaviour-Driven Development** (Dan North, 2006) and its **Gherkin** `Given/When/Then` grammar (the language of **Cucumber**, current release) are the source of the **name-the-behavior** discipline in SKILL §4 and the "assert the contract, not the implementation" through-line of `test-design.md`. Even when a repo uses no Cucumber runner, apply Gherkin's structure: a test name is the *Then* (`rejects_charge_when_card_expired`), Arrange is the *Given*, Act is the *When*. Use literal Gherkin feature files only when the repo already has them (detect per `framework-conventions.md`); otherwise borrow the structure, not the tooling.

## Contract testing — Pact

**Pact** (the **Pact Specification, current major version v4**) is the standard for **consumer-driven contract testing** across a service boundary. It operationalizes the boundary rule in `test-design.md` §2 ("mock only at trust boundaries"): instead of a hand-written stub that can silently drift from the real provider, the consumer's expectations become a shared **pact** the provider verifies in its own CI. Reach for it when the seam you would otherwise stub is a network API you own both sides of — it closes the gap a stub leaves open (your stub says 200, the provider now returns 422 and your green suite never noticed).

## Coverage criteria taxonomy — and MC/DC (DO-178C)

`test-design.md` §5 treats coverage as a floor, never a target, and ranks **line < branch < path**. That ordering is a slice of the formal **structural coverage hierarchy**, whose apex in regulated software is **MC/DC**:

| Criterion | Requires | Rigor |
|---|---|---|
| **Statement / Line** | every statement executed | weakest |
| **Decision / Branch** | every decision outcome taken both ways | working default (`test-design.md` §5) |
| **Condition** | every boolean sub-condition both ways | — |
| **Condition/Decision** | both of the above | — |
| **MC/DC** (Modified Condition/Decision Coverage) | each condition shown to *independently* affect the decision outcome | strongest practical |
| **Multiple Condition** | every combination of conditions | exhaustive, rarely feasible |

**MC/DC** is mandated by **DO-178C, *Software Considerations in Airborne Systems and Equipment Certification*** (RTCA, **2011** — the current edition, superseding DO-178B) for **Level A** (catastrophic-failure) avionics software. You will rarely target MC/DC outside safety-critical domains, but name it when a codebase has genuinely life-critical branch logic — it is the recognized bar there, and it is *why* branch coverage alone is the everyday compromise: MC/DC is expensive precisely because it demands the independent-effect cases branch coverage skips.

## Test-quality via mutation testing — Stryker / PIT

Coverage measures execution, not assertion (`test-design.md` §5). **Mutation testing** measures assertion strength directly: it seeds small faults ("mutants") into the code and checks that some test goes red. Surviving mutants are behaviors your suite runs but does not verify — exactly the "vacuous test" failure mode SKILL §5.2 warns against, made measurable. The de-facto tools (rolling releases — pin your **mutation-score threshold**, not a tool version):

| Tool | Ecosystem |
|---|---|
| **Stryker Mutator** | JavaScript/TypeScript, C#, Scala |
| **PIT (pitest)** | Java / JVM |
| **mutmut / Cosmic Ray** | Python |

Reach for a mutation run to *audit* an existing suite ("100% coverage — but does anything fail if I break the code?"), not on every change; it is slow. A surviving-mutant report is the strongest evidence that a coverage number is lying.

## Edition discipline

Standards split into two kinds — cite each accordingly and re-check on a cadence:

- **Foundational texts** (Meszaros 2007, Beck 2002, Cohn 2009, FIRST, Dodds' Trophy 2018) have not been revised and are unlikely to be — cite the original; they define stable vocabulary. Attribute by name, not by chasing a "latest edition."
- **Living standards** drift and must be pinned: **Pact Specification (v4 current)**, **DO-178C (2011 current; no successor announced as of 2026)**, and the mutation tools (**Stryker / PIT**, continuous releases). Cite the version you mapped to, and re-verify on roughly an annual cadence — the same edition-discipline rule `loop-review/references/owasp-cwe.md` applies to the OWASP Top 10. Do not mix a mapped edition with a newer one mid-report; update this file when a successor lands, then re-map.

For which of these a given repo actually uses (Cucumber vs plain xUnit, Pact vs hand-stubbed boundaries, a mutation tool in CI or not), detect per `framework-conventions.md` and **match what exists** — introduce a new standard only when the repo has none and the user asks you to establish one.
