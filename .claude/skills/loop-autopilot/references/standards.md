# Standards — grounding the loop in real frameworks

The loop in this skill uses homegrown vocabulary — *rounds*, *trust weights*, *credit horizon*, *propose gate*. None of it is invented from scratch; each concept has an authoritative framework behind it. This file names those frameworks, pins the edition to map against as of 2026, and points at the exact loop step each one governs. Cite the edition, not "the framework in general" — these get revised, and a mapping keyed to a superseded edition drifts silently.

Use this file the way `owasp-cwe.md` is used in `loop-review`: as the naming authority. When you justify a guard, a metric, or the propose gate to a human, cite the standard from here, not from memory.

## DORA — the Four Keys (throughput & stability)

**Framework:** DORA (DevOps Research and Assessment), now part of **Google Cloud**. Published annually as the **State of DevOps Report** (the current edition is the most recent annual report; treat the report as the moving edition and the Four Keys as the stable metric set beneath it).

The **Four Keys** are the measurement backbone for the loop's *output* — is it shipping small, safe, reversible change, or piling up risk?

| Metric | What it measures | How the loop reads it |
|---|---|---|
| **Deployment Frequency** | How often change reaches users | Proxy: draft PRs opened per period. The loop optimizes for *many small* proposals, not few large ones. |
| **Lead Time for Changes** | Commit → production latency | The `intake → propose` span of one round. Short-lived `claude/` branches keep this low. |
| **Change Failure Rate** | Share of changes that fail / need remediation | The exact signal the credit ledger accumulates: `rejected / proposed` per kind (`credit-horizon.md`). |
| **Failed Deployment Recovery Time** | Time to restore after a bad change | For a propose-only loop, this collapses to "time to close/revert a bad draft PR" — usually near-zero because nothing merged. |

**Edition note.** The 2024 report **renamed** "Time to Restore Service" (a.k.a. MTTR) to **Failed Deployment Recovery Time** and continues to refine a fifth **reliability**/operational-performance dimension. Map to the Four Keys as named above; when you quote a benchmark (elite/high/medium/low cluster thresholds), cite the specific annual report you pulled it from — the cluster boundaries move year to year.

## SPACE — don't optimize one number

**Framework:** **SPACE** — Forsgren, Storey, Maddila, Zimmermann, Houck, Butler, *"The SPACE of Developer Productivity"* (ACM Queue, **2021**). A framework, not a versioned spec; the 2021 paper is authoritative and unrevised.

SPACE is the counterweight to DORA here. DORA measures throughput; SPACE forbids optimizing throughput alone. Its five dimensions — **S**atisfaction & wellbeing, **P**erformance, **A**ctivity, **C**ommunication & collaboration, **E**fficiency & flow — say: pick metrics from **at least two** dimensions, and never let an activity metric (PRs opened) stand in for value.

| SPACE dimension | The loop's failure mode if ignored | Guarded by |
|---|---|---|
| **Satisfaction** | Flooding reviewers with low-signal PRs; reviewers stop reading | `comprehension-rot.md`; verify-before-propose (`loop-design.md` §Verify) |
| **Performance** | Counting PRs opened instead of PRs *merged* | Credit ledger keys on merged outcome, not proposal count (`credit-horizon.md`) |
| **Activity** | Mistaking rounds-run for progress | Dry-counter guard (L1) stops idle spinning |
| **Communication** | Silent auto-changes nobody understands | Every PR carries an `loop-audit` risk memo + summary comment |
| **Efficiency & flow** | Long lead time, batched giant PRs | One item → one focused PR (`loop-design.md` §Act) |

**Applying it:** when the loop reports its own health, pair a throughput number (DORA Deployment Frequency) with a satisfaction/performance number (merge rate from the ledger). A dashboard that shows only "42 PRs opened" is the SPACE anti-pattern.

## Autonomous-agent safety — the propose-only discipline

This is the **emerging** discipline; treat everything here as pre-consolidation and re-check often. Two real, citable anchors ground the loop's structural guardrails:

- **NIST AI Risk Management Framework (AI RMF 1.0)**, NIST, **January 2023**, plus the **Generative AI Profile** (NIST-AI-600-1, **July 2024**). Its four functions — **Govern, Map, Measure, Manage** — map cleanly onto this skill: *Govern* = the never-merge rule and `claude/`-branch scope; *Measure* = the credit ledger; *Manage* = the anti-pattern pre-flight (`anti-patterns.md`).
- **OWASP Top 10 for LLM Applications** (current edition is the **2025** list) and the **OWASP Agentic Security Initiative** ("Agentic AI — Threats and Mitigations"). *Excessive Agency* is the named risk this loop mitigates structurally.
- **ISO/IEC 42001:2023** (AI management systems) for teams that need a certifiable management-system framing around the loop.

The core practice these converge on — and the one the loop enforces — is **human-in-the-loop (HITL) gating on any state-changing action**. This skill's concrete form:

| Safety practice | Where enforced |
|---|---|
| **Propose-only** (never merge, never push `main`) | `loop-design.md` §Propose; SKILL.md §6 rule 1 — harness policy **H11**, one workflow per human gate |
| **Least privilege** (push only to `claude/` branches) | SKILL.md §6 rule 2 |
| **Bounded autonomy** (budget/round/dry caps) | `loop-design.md` §Guards (L1/L2/L4) |
| **Adversarial self-check before action** | `loop-design.md` §Verify (`loop-review`); `anti-patterns.md` AP1 (Nodding Loop) |

**Edition note.** This is the least stable section in this file. The OWASP LLM/Agentic lists and NIST profiles are actively revised; name the standard and pin the edition you cite, and re-check this section on a **quarterly** cadence rather than the annual cadence the others tolerate.

## GitHub Flow & Trunk-Based Development — the branch discipline

The draft-PR + short-lived-branch mechanics aren't ad hoc; they are two named, established workflows.

- **GitHub Flow** — **GitHub**'s lightweight, branch-per-change model (branch → commit → open PR → review → merge). Living documentation, not a versioned spec; map to the current published guide. This *is* the shape of the loop's Act→Propose stages: a `claude/`-prefixed branch, a **draft** PR, a human reviewer as the merge gate.
- **Trunk-Based Development** — codified at trunkbaseddevelopment.com and validated as a high-performance practice by DORA/`Accelerate` (Forsgren, Humble, Kim, **2018**). Its core rule is **short-lived branches** (merge or close within a day or two) to avoid long-lived divergence.

| Discipline | Standard behind it | Loop mechanism |
|---|---|---|
| One change = one branch = one PR | GitHub Flow | `loop-design.md` §Act — one item, one focused PR |
| Branches live hours, not weeks | Trunk-Based Development | Each round opens and hands off a branch same-round |
| Isolation between concurrent branches | (both) | Worktree isolation on the Act stage — `anti-patterns.md` AP5 |
| Human review before trunk | GitHub Flow merge gate | Propose-only draft PR (never auto-merge) |

## Credit assignment & experience batching — the optimization framing

`credit-horizon.md`'s three knobs are **reinforcement-learning** terminology, not house jargon. The authoritative reference is **Sutton & Barto, *Reinforcement Learning: An Introduction*, 2nd edition (2018)** — the standard text; use its vocabulary precisely.

| RL concept | Origin | This loop's instance |
|---|---|---|
| **Temporal credit assignment** | Minsky (1961); Sutton (1984) | Which triage decision earned a merge/rejection — here the chain is ~1 step (triage → PR → outcome), so the classic hard version mostly evaporates |
| **Experience replay / batching** | Lin (1992); Mnih et al., DQN (2015) | `BATCH_SIZE` — accumulate outcomes before recomputing `trustWeight`, so one rejected PR doesn't jerk the policy |
| **Optimism under uncertainty** | RL exploration (e.g. UCB) | `trustWeight` starts at 0.6 — untested kinds get a fair shot, not a cold zero |

This framing is a *conceptual* grounding, not a spec you version. Cite the 2nd edition (2018) of Sutton & Barto for the definitions; the ideas themselves are stable.

## Edition discipline

Standards get revised; a mapping silently rots when its target edition is superseded. Rules for this file:

- **Cite the edition you map to**, never "the framework." DORA benchmarks especially are per-report — quote the year.
- **Re-check on a cadence.** The autonomous-agent-safety section (OWASP LLM/Agentic, NIST profiles) moves fast — review **quarterly**. DORA's report is **annual**. SPACE, GitHub Flow, Trunk-Based Development, and the RL framing are effectively stable — review opportunistically, when something looks off.
- **Never fabricate a version number.** If you can't confirm an exact edition, name the standard and write "current edition" rather than invent one — the same rule `owasp-cwe.md` applies to CWE/OWASP ids.
- **Don't mix editions inside one report.** If you cite DORA's 2024 metric rename, use its 2024 benchmarks too; don't splice thresholds across years.
