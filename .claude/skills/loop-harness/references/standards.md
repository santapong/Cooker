# Security & configuration standards for the harness

The authoritative standards a harness is built *against*. A Claude Code harness is a security boundary and a piece of automation config — so it answers to the same established frameworks as any least-privilege system, hardened baseline, or CI/CD pipeline. This file names each standard, pins the edition to design against as of 2026, and maps it to a concrete step in this skill. The other references tell you *how* to write the config; this one tells you *what good looks like* and *whose rules you are following*.

Cite the edition, don't reinvent the principle. Where a standard cross-cuts a specific pillar, the mapping points at `permissions.md`, `hooks.md`, `mcp.md`, or `automation-loops.md`.

## Least Privilege & Zero-Trust — the design principle behind allow/deny/ask

| | |
|---|---|
| **Standard** | Principle of Least Privilege (POLP); **NIST SP 800-207, *Zero Trust Architecture*** |
| **Issuing body** | NIST (National Institute of Standards and Technology), U.S. Dept. of Commerce |
| **Edition (2026)** | SP 800-207 (August 2020), current. Complemented by **SP 800-207A** (2023, ZTA for cloud-native/multi-cloud). No successor to 800-207 announced. |

POLP and zero-trust are the *why* behind the whole permission model. **Never trust by default; grant the narrowest capability that lets the work proceed; verify at every boundary.** In this skill that is literal: the `deny` list is the never-trust floor, `allow` is an explicit, minimal grant (not `Bash(*)`), `ask` is per-action verification for anything irreversible, and `defaultMode` must never be `bypassPermissions`. See `permissions.md` — "keep the allow-list tight and specific" *is* least privilege. A PreToolUse guard hook (`hooks.md`) is the boundary re-verification zero-trust demands even for allowed tools.

## CIS Benchmarks & Controls — hardened configuration baselines

| | |
|---|---|
| **Standard** | **CIS Benchmarks** (per-platform config baselines) and **CIS Critical Security Controls** |
| **Issuing body** | Center for Internet Security (CIS) |
| **Edition (2026)** | **CIS Controls v8.1** (2024) is current. Benchmarks are versioned *per technology* and revised continuously — always pull the latest benchmark for the specific OS/tool, not a memorized version number. |

CIS gives the "secure default" posture the harness should encode. Map its ideas onto config, don't just cite them: no default-permissive settings, remove unused capability, log security-relevant actions. Concretely — `settings.json` ships `settings.local.json` gitignored (CIS Control 3, data protection), the `deny` floor blocks credential-file reads (Control 6, access management), a PostToolUse/Stop hook can log actions (Control 8, audit log). For MCP servers you enable, apply the vendor's CIS Benchmark to the *server's* host, not just the client config.

## The 12-Factor App — config in the environment

| | |
|---|---|
| **Standard** | **The Twelve-Factor App**, Factor III *Config* (and Factor X *Dev/prod parity*) |
| **Issuing body** | Originated by Adam Wiggins / Heroku (2011); the canonical community reference |
| **Edition (2026)** | The original twelve factors remain the stable canonical reference; there is no formal versioned revision. Treat "beyond 12-factor" writings as commentary, not a superseding edition. |

Factor III — **strict separation of config from code, config lives in the environment** — is exactly what makes the harness portable and secret-safe. This is why `.mcp.json` uses `${VAR}` / `${VAR:-default}` expansion instead of literal URLs and tokens (`mcp.md`), and why `settings.json` carries an `env` block for non-secret config while machine-specific values go in gitignored `settings.local.json`. When you review a harness, a hardcoded token or endpoint in a committed file is a Factor III violation — flag it and move it to the environment.

## Secrets management — never in source, always in env/secret-store

| | |
|---|---|
| **Standard** | **OWASP Secrets Management Cheat Sheet**; **NIST SP 800-57** (key management); CWE-798 *Use of Hard-coded Credentials* |
| **Issuing body** | OWASP Foundation; NIST |
| **Edition (2026)** | OWASP cheat sheet — current maintained edition. NIST SP 800-57 Part 1 Rev. 5 (2020). CWE per the `loop-review` skill's `owasp-cwe.md`. |

The rule the harness enforces mechanically: **secrets never appear in a committed file, in source, or in a prompt — they resolve at runtime from the environment or a secret store.** Two layers implement it here: (1) *prevention* — `${VAR}` expansion in `.mcp.json` and `env`, gitignored `settings.local.json` for real values (`mcp.md`); (2) *containment* — the `deny` list blocks `Read(.env)`, `Read(**/.env*)`, `Read(**/.ssh/**)`, `Read(**/.aws/**)`, `Read(**/secrets/**)`, backed by a PreToolUse `guard-secrets.sh` hook as belt-and-suspenders (`permissions.md`, `hooks.md`). A hard-coded credential is CWE-798 — cross-reference the `loop-review` skill when auditing a harness that touches real code.

## OWASP CI/CD Security Top 10 — risks for the automation layer

| | |
|---|---|
| **Standard** | **OWASP Top 10 CI/CD Security Risks** |
| **Issuing body** | OWASP Foundation |
| **Edition (2026)** | **2022 edition** (CICD-SEC-1 … CICD-SEC-10), the current published list. |

The moment a harness runs *unattended* — Routines, headless `claude -p`, GitHub Actions (`automation-loops.md`) — it is a CI/CD pipeline and inherits these risks. Map the ones the harness controls:

| Risk | Harness control |
|---|---|
| **CICD-SEC-1** Insufficient Flow Control | Routines default to pushing only `claude/`-prefixed branches; never auto-merge |
| **CICD-SEC-2** Inadequate IAM | least-privilege `allow`; scoped tokens for MCP/Actions |
| **CICD-SEC-4** Poisoned Pipeline Execution | `--bare` headless runs (skip hooks/MCP/CLAUDE.md) for untrusted-input CI |
| **CICD-SEC-6** Insufficient Credential Hygiene | env-only secrets, gitignored local settings (see secrets section) |
| **CICD-SEC-7** Insecure System Configuration | CIS-hardened baseline; no `bypassPermissions` |
| **CICD-SEC-10** Insufficient Logging | Stop/PostToolUse logging hooks; audit unattended runs |

## SLSA — provenance for what the automation produces

| | |
|---|---|
| **Standard** | **SLSA — Supply-chain Levels for Software Artifacts** |
| **Issuing body** | OpenSSF (Open Source Security Foundation) / Linux Foundation |
| **Edition (2026)** | **SLSA v1.0** (2023), Build track levels **L1–L3**. (The pre-1.0 four-level scheme is superseded — do not cite "SLSA 4".) |

When the harness *builds or releases* artifacts through automation, SLSA governs how you prove where they came from. Aim for **Build L2+** on anything automation ships: a hosted, trusted build (a Routine or GitHub Action, not a developer laptop) that emits signed **provenance** attesting the source and build steps. In this skill that shapes `automation-loops.md` choices — prefer a durable, isolated runner over an ad-hoc local `/loop` for release-producing work, and have the pipeline generate provenance rather than trusting an unattested artifact.

## Edition discipline

Standards get revised; a harness cited against a stale edition drifts from what auditors and tools expect. Rules:

- **Pin the edition you designed against** (done per-section above) and record it in the harness's own notes when you ship one.
- **Re-check on a cadence** — roughly annually, or when a section's issuing body publishes a new edition. Watch especially: OWASP CI/CD Top 10 (a refresh would re-key the mapping table), CIS Benchmarks (revised per-platform, continuously), and SLSA (active track expansion beyond Build).
- **Never mix editions inside one harness or one review.** If you update to a newer edition, re-map every affected section, the same discipline `owasp-cwe.md` applies to the OWASP Top 10 2021→2025 transition.
- **Name-don't-fabricate.** If unsure of an exact version, cite the standard and "current edition" rather than inventing a number.
