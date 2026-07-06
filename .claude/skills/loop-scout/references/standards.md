# Authoritative Standards for Prior-Art and Candidate Evaluation

The objective signals this skill leans on so a build-vs-buy call rests on published standards rather than vibes. When you rate a candidate on `evaluation-criteria.md`'s axes — license, maturity, security/supply-chain, version discipline — cite the standard here instead of asserting from memory. Each entry names the issuing body, pins the edition current as of 2026, and maps to a specific step of the workflow. Where a standard is versioned continuously (a tool or a list), say **current release** rather than guess a number.

`where-to-look.md` tells you *where* to discover candidates; this file tells you *how to score what you find* against recognized frameworks.

## License identity — SPDX

| Field | Value |
|---|---|
| **Standard** | SPDX License List + SPDX License Expression syntax |
| **Issuing body** | The SPDX project, hosted by the **Linux Foundation** (SPDX is also ISO/IEC 5962:2021) |
| **Edition (2026)** | SPDX Specification **3.0** (2024); the **License List** ships on its own rolling number and is updated roughly quarterly — always resolve against the current release |

**Maps to the license-compatibility axis.** When you score axis 3 in `evaluation-criteria.md`, do not eyeball "it's MIT-ish." Read the candidate's declared license as a canonical **SPDX identifier** (`MIT`, `Apache-2.0`, `BSD-3-Clause`, `GPL-3.0-only`, `AGPL-3.0-only`) and its `LICENSE`/`SPDX-License-Identifier` metadata. Compound cases use SPDX expression operators — `Apache-2.0 OR MIT` (dual-licensed, pick one), `GPL-2.0-or-later WITH Classpath-exception-2.0` (a carve-out that changes the obligation). A deprecated id (`GPL-3.0` without `-only`/`-or-later`) is a signal the metadata is stale. Reducing every candidate to an SPDX id is what makes the AGPL-network-service trap and copyleft obligations comparable across a shortlist.

## Supply-chain trust — SLSA provenance

| Field | Value |
|---|---|
| **Standard** | SLSA — Supply-chain Levels for Software Artifacts |
| **Issuing body** | **OpenSSF** (Open Source Security Foundation), Linux Foundation |
| **Edition (2026)** | SLSA **v1.0** (2023), the current stable track; later drafts extend it but v1.0 is what to map to |

**Maps to the security & supply-chain axis.** SLSA grades how trustworthy a build artifact's *origin* is, on **Build Levels L0–L3**: L1 = provenance exists; L2 = signed provenance from a hosted build service; L3 = hardened, non-falsifiable provenance. When a candidate is a shipped binary or package (not just source), check for a provenance attestation and treat its SLSA level as the supply-chain evidence for the "provenance and signing" sub-point in `evaluation-criteria.md`. Higher level = less "did this artifact really come from that repo?" risk.

## Objective maturity/health — OpenSSF Scorecard & Criticality Score

| Standard | Issuing body | Edition (2026) | What it measures |
|---|---|---|---|
| **OpenSSF Scorecard** | OpenSSF / Linux Foundation | current release (continuously versioned tool) | 0–10 automated score across ~18 checks: Maintained, Code-Review, Branch-Protection, Signed-Releases, Dependency-Update-Tool, Vulnerabilities, Token-Permissions, Fuzzing, SAST, Pinned-Dependencies |
| **OpenSSF Criticality Score** | OpenSSF / Linux Foundation | current release | 0–1 influence/importance score from usage and dependency signals — how much the ecosystem *depends* on the project |

**Maps to the maturity axis (axis 2).** These convert "is it alive and trusted?" from opinion into numbers. Read **Scorecard** as the health signal — a low Maintained or Signed-Releases check corroborates a stale-project or weak-provenance concern; it complements, not replaces, the human read of release cadence and issue responsiveness. Read **Criticality Score** as the adoption/importance signal, a durable stand-in for the "dependents over raw stars" rule. Use both to rank the top two or three candidates before writing the verdict line.

## Version & stability discipline — Semantic Versioning

| Field | Value |
|---|---|
| **Standard** | Semantic Versioning (SemVer) |
| **Issuing body** | semver.org — specification authored by Tom Preston-Werner (community-stewarded) |
| **Edition (2026)** | **2.0.0** — stable and unchanged since 2013; no newer edition pending |

**Maps to maturity + lock-in.** `MAJOR.MINOR.PATCH` tells you how a dependency manages change: MAJOR = breaking, MINOR = additive, PATCH = fixes. Read a candidate's version history as evidence of discipline — a project still on `0.x` signals an unstable public API (SemVer says anything may break); frequent MAJOR bumps signal high upgrade churn (a real TCO cost per axis 6); long, clean MINOR/PATCH streaks signal stability. This also sets the constraint you would pin in a manifest (`^1.2` vs `~1.2`) and feeds the "upgrade/maintenance burden" you weigh in `build-vs-buy.md`. Note the ecosystem dialects: npm ranges, Python PEP 440, Go's SemVer-based module versioning, Rust's Cargo `^` defaults — all trace back to SemVer 2.0.0.

## Discovery sources — landscapes & registries

| Source | Steward | Role in this skill |
|---|---|---|
| **CNCF Landscape** | Cloud Native Computing Foundation (Linux Foundation) | Curated map of cloud-native tools by category with maturity tiers (Sandbox / Incubating / Graduated) — a fast survey of the field's real options |
| **TODO Group** landscape method | TODO Group (Linux Foundation) | The open-source-program-office approach to cataloguing and comparing tools; the pattern behind category "landscapes" generally |
| **npm** | OpenJS Foundation ecosystem | JS/TS registry — weekly downloads, last publish, dependents |
| **PyPI** | Python Software Foundation | Python registry — releases, maintained status (pair with pypistats) |
| **crates.io** | Rust Foundation | Rust registry — downloads, recent version, reverse deps |
| **Maven Central** | Sonatype | Java/Kotlin registry — latest version, usages |
| **pkg.go.dev** | Go team / Google | Go module index — imported-by, versions |

**Maps to Step 2 (check the boring options first).** Treat these as the *authoritative* discovery surfaces, not blogs or model memory. A category **landscape** (CNCF-style) gives you the shortlist of serious contenders for a capability; the **ecosystem registry** gives you each candidate's canonical metadata — version, license id, release recency, dependents — which is exactly the input to the SPDX, SemVer, and Scorecard reads above. See `where-to-look.md` for the full source order (stdlib and platform first).

## Edition discipline

Standards get revised; a mapping is only as good as the edition it names.

- **Pin the edition you map to** — SPDX Spec 3.0, SLSA v1.0, SemVer 2.0.0 — and don't silently mix editions across a shortlist.
- **Rolling artifacts** (Scorecard, Criticality Score, the SPDX License List, the CNCF Landscape) have no fixed number; always read the **current release** at evaluation time rather than caching a value.
- **Re-check on a cadence.** SLSA is on a v1.x track with further levels drafted; the SPDX License List changes quarterly. Re-verify roughly every two quarters, and when a new edition lands, update the pins here before relying on them.
- **Verify, don't assert.** For any maturity, license, provenance, or version claim that decides a recommendation, confirm it against the primary source (repo, registry, attestation) via the `loop-research` skill — the same rule `evaluation-criteria.md` applies to the scoring axes.
