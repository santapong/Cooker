# Severity × Confidence — the scoring rubric

The gate every candidate passes through before it reaches the report. Two independent axes: **severity** (how bad if real) and **confidence** (how sure it *is* real). The reporting bar filters on both. `methodology.md` produces candidates and proves their exploit path; this file turns a proven candidate into a labeled, filtered finding, and `owasp-cwe.md` names it.

The axes are orthogonal — do not collapse them. A hardcoded root credential is Critical severity even if you are only 0.4 confident it is live; a missing null-check is Low severity even at 1.0 confidence. Score each axis on its own, then apply the grid.

**Suppressing false positives is the primary quality metric of this skill.** Every rule below is tuned to that end: a review that surfaces three real, exploitable defects beats one that surfaces thirty maybes, because the maybes train the reader to ignore the tool. When a candidate is borderline on either axis, the default is to drop it — see [The reporting bar](#the-reporting-bar) and [False-positive exclusions](#false-positive-exclusions).

## Severity — the CVSS band as shared vocabulary

Severity uses the **CVSS v4.0 qualitative scale** so a finding's label means the same thing to the reviewer, the CI pipeline, and the human triaging the PR. The numeric-to-label mapping is fixed:

| Band | CVSS score | Use for |
|---|---|---|
| **None** | 0.0 | Not a security finding (drop, or route to `quality-checks.md`) |
| **Low** | 0.1 – 3.9 | Real but low-impact: info leak with no PII, defense-in-depth gap behind other controls |
| **Medium** | 4.0 – 6.9 | Exploitable but constrained: needs auth, limited blast radius, or non-default config |
| **High** | 7.0 – 8.9 | Directly exploitable with meaningful impact: authz bypass, injection reachable by an authenticated user |
| **Critical** | 9.0 – 10.0 | Unauthenticated RCE, full-account or cross-tenant takeover, secret granting production access |

You are not required to compute a full CVSS vector for every finding — for a source-code defect you assign the **band** by judgment: impact (what an attacker gains) crossed with exploitability (how reachable and how hard). Reserve full vectors for the case where they already exist:

- **Dependency / known-CVE findings (A06)** ship a published CVSS base score — use it. This is the CVSS-B (base-only) layer.
- **Source findings you discovered** get a band, not a vector. When in doubt between two bands, let reachability decide: unauthenticated and trivially reachable pushes up a band; behind auth or requiring an unusual precondition pushes down one. Note the auth requirement in the finding rather than silently lowering.

CVSS v4.0's layered nomenclature is worth knowing because the next section uses it: **CVSS-B** is base alone; **CVSS-BT** adds the *Threat* layer (real-world exploit maturity); **CVSS-BTE** adds your *Environmental* re-scoring. A base score is a starting point, not a priority.

## Threat-adjusting severity — EPSS and CISA KEV

A CVSS base score says how bad a flaw *could* be; it says nothing about whether anyone is exploiting it *today*. For dependency and known-CVE findings, adjust the base with two real-world signals — this is the CVSS-BT threat layer made concrete:

- **CISA KEV (Known Exploited Vulnerabilities catalog)** — a binary, authoritative signal: the CVE is on the list or it is not. **On KEV means it is being actively exploited in the wild.** Treat any KEV hit as an escalation regardless of base score: a Medium-base CVE on KEV is a High-priority finding, and it carries a real remediation deadline for anyone under the federal directive. Always check the finding's CVE against KEV.
- **EPSS (Exploit Prediction Scoring System)** — a probability from 0 to 1 that the CVE will be exploited in the next 30 days, refreshed daily by FIRST. Use it to **order** findings that share a band: a High CVE at EPSS 0.90 gets fixed before a High CVE at EPSS 0.02. EPSS is a prioritization multiplier, not a severity band — a low EPSS never demotes a KEV entry, and a high EPSS on an unreachable dependency (see [exclusions](#false-positive-exclusions)) is still not exploitable *here*.

Precedence when they disagree: **KEV > reachability-in-this-codebase > CVSS base > EPSS.** KEV means someone is already through the door; reachability means the door exists in your build; base is the theoretical blast radius; EPSS breaks ties. State the adjustment in the finding ("CVSS 6.1 base, on CISA KEV → escalated to High").

## Confidence — 0 to 1, earned by the trace

Confidence is how sure you are the finding is real and exploitable, not how bad it would be. It is **earned by the data-flow trace and a passed confirm/refute test** (`vulnerability-playbooks.md`), never granted by a suspicious-looking pattern.

| Confidence | Meaning |
|---|---|
| **≥ 0.8** | Unbroken source→sink path read end-to-end; every intervening step inspected, not assumed; the relevant confirm/refute test passed. |
| **0.5 – 0.79** | Path is plausible but one step is assumed — a sanitizer whose correctness you did not verify, a caller you did not open, reachability you inferred. |
| **< 0.5** | Pattern match without a proven path; the source, the sink, or the connection between them is unconfirmed. |

The way to raise confidence is mechanical: pull in the neighbor you assumed (methodology "Sizing the neighborhood"), read the sanitizer you skipped, confirm the entry point is live. If you *cannot* resolve the assumption, the confidence stays where it is — do not round up because the pattern looks bad. A guessed step caps a finding below the bar by design.

## The reporting bar

Combine the axes. **Default rule: report Critical/High/Medium at confidence ≥ 0.8; drop everything else.**

| Severity ↓ / Confidence → | **≥ 0.8** | **0.5 – 0.79** | **< 0.5** |
|---|---|---|---|
| **Critical** | Report | Escalate to verify pass; report only if it clears ≥ 0.8 | Drop, but **log it** — never silently discard a possible Critical |
| **High** | Report | Escalate to verify pass; else drop | Drop |
| **Medium** | Report | Drop (appendix only if the user asked for lower-confidence notes) | Drop |
| **Low** | Drop (appendix only if asked) | Drop | Drop |

Rules the grid encodes:

- **Confidence ≥ 0.8 is the hard floor for the report body.** A finding you cannot get to 0.8 does not belong in the report; it belongs in the verification pass or the bin.
- **Never silently drop a plausible Critical.** A genuine Critical at 0.6 confidence deserves the adversarial verifier (`../../loop-engine/references/harness-policy.md` H4), not the trash. Route it there; if it clears, report it; if it dies, log why so a human can second-guess the drop.
- **Low is out regardless of confidence.** Certainty that something is minor does not make it worth the reader's attention. This skill reports defects, not style — a certain-but-trivial issue is noise.
- **When torn between reporting and dropping, drop.** Let the verification pass be where uncertain findings die, not the reader's inbox. Log every drop and every suppression (H6, no silent truncation) so the choice is auditable.

The multi-agent template wires this bar into the adversarial-verify stage: only survivors that clear Critical/High/Medium at ≥ 0.8 are emitted. See `../SKILL.md` §8 and `templates/security-review.workflow.js`.

## False-positive exclusions

The suppression precedents below are **defaults, not law** — each project keeps its own allowlist, seeded from the security conventions you mapped in methodology Phase 1. A pattern that is a finding in a payment service may be intentional in an internal CLI. Customize per repo, but apply the same discipline every time: suppress by default, and record *what* you suppressed and *why*, so a human can catch an over-suppression. The escape hatch on each line is the condition under which the exclusion does **not** apply — check it before you drop.

- **Test code and fixtures** — hardcoded credentials, weak crypto, and open endpoints inside `*_test.*`, `tests/`, `spec/`, `__mocks__/`, and fixture files are usually intentional. *Escape hatch:* a test helper imported by production code, or a fixture "dummy" secret that is also a real deployed credential.
- **Examples, samples, docs** — code under `examples/`, `docs/`, `*.example`, `*.sample` demonstrates, it does not ship. *Escape hatch:* a sample users are told to copy verbatim into a running quickstart, or an example wired into the default build.
- **Intentional / documented patterns** — a suppression comment with a rationale (`# nosec: validated above`, `eslint-disable-next-line ... -- reviewed`) or a repo-wide accepted pattern from the conventions note. *Escape hatch:* the suppression has *no* rationale, or the "internal only" justification is false — the component is network-reachable after all.
- **Unreachable / dead code** — no live entry point reaches the sink (methodology Phase 3 reachability failed). No path, no finding. *Escape hatch:* reachable via reflection, dependency injection, a route/registry table, a serialization hook, or a feature flag that can be flipped on.
- **Framework-handled cases** — the framework neutralizes the class by default: React/JSX auto-escaping, an ORM's parameterized queries, Django/Jinja autoescape, Rails strong parameters, a framework's built-in CSRF token. *Escape hatch:* the code opts out — `dangerouslySetInnerHTML`, a raw-SQL string builder, `|safe` / `mark_safe`, `.html_safe`, `raw()`, disabled CSRF middleware.
- **Generated and vendored code** — `node_modules/`, `vendor/`, `*.pb.*`, generated clients, lockfiles, migrations. Report the *source* that produced them, not the artifact; vendored dependency risk is A06 SCA (a version-level finding), not a line-level review of the vendored file.

If a candidate matches a precedent but trips its escape hatch, it is back in scope — the hatch is the whole point of listing the precedent rather than blanket-ignoring the path.

## Structured output — SARIF and inline PR comments

Emit findings as structured records, not prose, so the same result drives both a CI gate and a PR comment. Give every finder's result a `schema` (per the harness rules) carrying the fields both sinks below need.

**SARIF for CI.** SARIF (the OASIS Static Analysis Results Interchange Format) is the JSON that GitHub code scanning and most CI security tabs ingest. Map each reported finding onto a SARIF `result`:

| Finding field | SARIF field | Notes |
|---|---|---|
| CWE id (from `owasp-cwe.md`) | `ruleId` | e.g. `CWE-89`; register the OWASP category and ASVS control in the rule's `properties`. |
| Severity band | `level` | Critical/High → `error`, Medium → `warning`, Low → `note`. |
| CVSS numeric | `properties.security-severity` | A string like `"8.1"`. GitHub re-derives the band from this — and its thresholds (≥9.0 critical, 7.0–8.9 high, 4.0–6.9 medium, <4.0 low) are **the same CVSS bands above**, so the label stays consistent end to end. |
| One-line summary + exploit path | `message.text` | State inputs → effect, not a category label. |
| Anchored file + line | `locations[].physicalLocation` | `artifactLocation.uri` + `region.startLine` — the changed line the finding sits on. |
| Stable identity | `partialFingerprints` | Hash of CWE + file + normalized snippet, so the same defect dedups across re-runs instead of re-alerting. |

Confidence is **not** a SARIF-standard field — carry it in `properties.confidence` if you emit it at all, but note that the reporting bar is applied *before* SARIF emission: everything that reaches the file has already cleared ≥ 0.8, so CI never sees the maybes.

**Inline PR comments.** The same `locations` anchor drives a review comment on the exact changed line. Each comment body carries: the one-line summary, the exploit path as inputs → effect, the severity/confidence, the `owasp-cwe.md` tags (OWASP category · CWE · ASVS control), and the concrete fix. One comment per surviving finding, anchored to the line — never a wall of comments on one hunk, and never a comment for anything below the bar.
