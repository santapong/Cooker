---
name: loop-review
description: Review code for security vulnerabilities and quality issues using OWASP Top 10, CWE Top 25, and ASVS standards. Use when the user asks to review code, run a security scan or audit, check for vulnerabilities, do a code-quality or best-practices review, or assess a diff, PR, or repo for injection, auth, crypto, secrets, SSRF, or dependency risks.
---

# Reviewing Code

You are about to run a security and code-quality review. The engine is you reading code — not an external scanner. Your job is to find real, exploitable defects and report them at a signal-to-noise ratio a human will trust. A review that cries wolf gets ignored; **suppressing false positives is the primary quality metric of this skill**, not raw finding count.

## 1. Scope: default to the diff

- **Default (diff-scoped)**: review only what changed — the working tree, a named commit range, or a PR. This is the mode for "review this", "look at my PR", "check this diff". Gather context from the whole repo (§3 phase 1) but only *report* findings in changed lines and code they directly reach.
- **Full-repo audit (opt-in)**: sweep the entire codebase. Only enter this mode when the user explicitly asks for a "full audit", "scan the whole repo", or names no diff. It is slower and noisier — confirm scope if ambiguous.
- Establish scope before reading code. If the user said "review my changes", run `git diff` / `git diff --stat` against the base to get the changed set.

## 2. Execution model: LLM-native, zero required dependencies

This skill has **no required external tools**. Do not install Semgrep, gitleaks, or anything else to run a review, and never block on a missing binary — you are the analysis engine.

**Optional scanner ingestion.** If — and only if — the user *already* runs these, treat their output as extra leads to triage, never as ground truth. Confirm every scanner hit against the source yourself before reporting it; scanners are noisy and this skill's job is to cut that noise.

| If the user already has… | Ingest its output as leads for… |
|---|---|
| Semgrep / CodeQL | injection, authz, misconfiguration patterns |
| gitleaks / TruffleHog | hardcoded secrets and credential leaks |
| OSV-Scanner / Trivy / `npm audit` | vulnerable & outdated components (SCA) |

Ask for the report path or paste; if none exists, proceed without it. A scanner finding you cannot confirm in the code is a lead you drop, not a finding you report.

## 3. Methodology: three phases

Run the review in three passes. Full procedure in **`references/methodology.md`** — read it before a non-trivial review.

1. **Repo & context gathering** — map the stack, frameworks, entry points, auth model, trust boundaries, and existing security conventions. You cannot judge a deviation without the baseline.
2. **Comparative deviation analysis** — compare changed code against the repo's own established patterns. Code that departs from how the rest of the codebase handles the same concern (validation, escaping, auth checks) is the highest-yield signal.
3. **Data-flow source → sink assessment** — for each candidate, trace tainted input from its source (request, file, env, third party) to a dangerous sink (query, exec, deserializer, template, file path). No reachable source-to-sink path → not a vulnerability. This trace is what separates a real finding from a pattern match.

## 4. Categories to sweep

Sweep every category below. Each has detection heuristics, source/sink pairs, and confirm/refute tests in **`references/vulnerability-playbooks.md`** — open it per category rather than working from memory.

- Injection (SQL, NoSQL, OS command, LDAP, template)
- Broken access control / authorization (IDOR, missing checks, privilege escalation)
- Authentication & session management
- Cryptographic failures (weak algorithms, bad randomness, misused primitives)
- Server-side request forgery (SSRF)
- Insecure deserialization
- Hardcoded secrets & credential leakage
- Input validation & output encoding (XSS, path traversal, open redirect)
- Security misconfiguration (headers, CORS, defaults, debug flags)
- Vulnerable & outdated components (SCA / dependency risk)
- Security logging & monitoring failures

## 5. Severity, confidence, and the false-positive bar

Score every candidate on severity and confidence per **`references/severity-model.md`**.

- **Report only HIGH and MEDIUM severity findings at confidence ≥ 0.8.** Everything else stays out of the report (mention as a brief "lower-confidence notes" appendix only if the user asked to see them).
- Confidence is earned by the data-flow trace and a passed confirm/refute test — not by a suspicious-looking pattern.
- When you cannot construct a concrete exploit path, **default to dropping the finding.** A missed low-severity issue costs less than a false alarm that erodes trust in the whole review.

## 6. Standards mapping

Map each reported finding to its standard identifiers — OWASP Top 10 category, CWE id, and relevant ASVS control — using **`references/owasp-cwe.md`**. This gives the user a canonical name and a remediation reference, and keeps categories consistent across reviews.

## 7. Code-quality (non-security) checks

When the user asks for a quality, best-practices, or general review (not strictly security), also apply **`references/quality-checks.md`**: correctness traps, error handling, resource leaks, concurrency, dead/duplicated code, and maintainability. Keep security and quality findings in separate report sections so severity is not conflated.

## 8. Orchestration: scale past a small diff

A small diff you can review inline in this session. For **anything larger — a big PR, multiple files, or a full-repo audit — run it as a multi-agent workflow** using the template at **`templates/security-review.workflow.js`**:

1. **Finder per category** — fan out one finder agent per §4 category (parallel), each returning structured candidate findings.
2. **Dedup barrier** — wait for all finders, then merge and dedup in plain script logic (the same category surfaces from multiple lenses).
3. **Adversarial verify** — one skeptic per deduped candidate, prompted to *refute* it and default to not-a-finding if the source→sink path is unproven. Only survivors that clear the §5 bar get reported.

This is the parallel finder → dedup → adversarial-verify pattern from the **`loop-engine`** skill (see its `templates/parallel.workflow.js` and harness policy H2/H4). Invoke the `loop-engine` skill to author and execute the run; the security template is a specialization of that pattern with the categories, severity model, and false-positive suppression pre-wired. For a diff small enough to hold in context, skip the workflow and review directly — do not spin up agents for a two-file change.

## Reference files

- `references/methodology.md` — the three-phase procedure in full
- `references/vulnerability-playbooks.md` — per-category detection, source/sink pairs, confirm/refute tests
- `references/severity-model.md` — severity × confidence scoring and the reporting bar
- `references/owasp-cwe.md` — OWASP Top 10, CWE Top 25, and ASVS mappings
- `references/quality-checks.md` — non-security code-quality checklist
- `references/standards.md` — the authoritative standards this skill applies — named, version-pinned, and mapped to its workflow
- `templates/security-review.workflow.js` — finder-per-category → dedup → adversarial-verify workflow script
