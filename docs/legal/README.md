> ⚠️ **TEMPLATE — requires review by qualified legal counsel before use.**
> Every document in this directory is a **non-binding starting point**, not legal advice.
> Do not publish, link to, or rely on any of it until a qualified attorney licensed in
> your jurisdiction has reviewed and adapted it to your business, your data flows, and
> the laws that apply to you. Placeholders in `[BRACKETS]` must be filled in.

# Cooker — Legal Documents (Templates)

This directory holds **launch-blocking legal templates** for shipping Cooker as a
product. They cover both deployment models:

- **Self-hosted** — the operator runs Cooker on their own infrastructure (Helm chart /
  binary). Cooker the project is a *software vendor*; the operator is the **data
  controller** and bears almost the entire compliance surface.
- **Hosted-SaaS (future)** — a Cooker-operated control plane that custodies customer
  secrets, deploy credentials, source, and build artifacts, and runs untrusted customer
  build code. This model is **materially gated** and is **not launch-ready today** (see
  `docs/launch/04-security-compliance-legal.md` §3 and §6.2).

## Why these exist

`docs/launch/04-security-compliance-legal.md` §5 ("Legal / launch must-haves") lists the
baseline legal artifacts a launch needs. This directory is the first draft of those
artifacts. A launch needs *baseline legal docs* and a *status-page plan* before charging
money — and because Cooker users run **arbitrary build/deploy code**, the Acceptable Use
Policy is load-bearing, not boilerplate.

## Index

| Document | Purpose | Applies to |
|---|---|---|
| [`terms-of-service.md`](terms-of-service.md) | Contract governing use of the software / service: license, warranty disclaimer, liability cap, termination. | Self-hosted (light) + Hosted (full) |
| [`privacy-policy.md`](privacy-policy.md) | What data is stored, why, who processes it (sub-processors), GDPR/CCPA rights, retention. | Mainly Hosted; self-hosted only if telemetry exists |
| [`acceptable-use-policy.md`](acceptable-use-policy.md) | Prohibited uses of the build/deploy execution surface — no mining, no attacks, resource limits. **Load-bearing.** | Recommended self-hosted, **required** Hosted |
| [`sla.md`](sla.md) | Best-effort tier (self-hosted) + placeholder for a future hosted contractual SLA, mapped to the SLO targets. | Self-hosted (best-effort) + Hosted (placeholder) |

## Related documents (not in this directory)

- **DPA + SCCs** — a Data Processing Addendum and Standard Contractual Clauses are
  required for the Hosted-SaaS model (GDPR Art. 28). They are **not yet written** and are
  a legal must-have for SaaS. Track in `docs/launch/04-security-compliance-legal.md` §2.3.
- **`security.txt` + vulnerability disclosure** — see `SECURITY.md`. The reporting
  address there is a placeholder that must be replaced before public launch.
- **OSS / source license** — the license that governs the source code itself lives at the
  repository root (`LICENSE`), separate from the Terms of Service that govern *use* of the
  software or service.

## The legal-review disclaimer (read this)

These templates are written to be a *useful, opinionated draft* — they reference Cooker's
actual data model and execution model so a lawyer is not starting from a blank page. They
are **not** a substitute for legal advice. Specifically:

1. **Jurisdiction.** Governing law, consumer-protection carve-outs, and enforceability of
   liability caps and warranty disclaimers vary by country and (in the US) by state. The
   `[GOVERNING LAW]` placeholders must be set by counsel.
2. **GDPR / CCPA-CPRA.** The privacy templates describe *mechanics* (what data exists,
   retention, erasure). Whether you are a controller or processor, whether SCCs are
   required, and the exact rights language are legal determinations.
3. **Liability and indemnity.** The caps and disclaimers here are conventional but not
   guaranteed enforceable; a lawyer must confirm them for your jurisdiction and customer
   class (consumer vs business).
4. **AUP enforceability.** The right to suspend/terminate abusive builds must be drafted
   so it is actually enforceable while not exposing you to "we monitored everything"
   liability. Counsel should align the AUP, ToS, and Privacy Policy.

**Owner:** [LEGAL / FOUNDER NAME] · **Last drafted:** [DATE] · **Status:** template, unreviewed.
