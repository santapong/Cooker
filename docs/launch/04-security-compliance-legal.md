# 04 — Security, Privacy, Compliance & Legal: What It Takes to Launch Cooker as a Product

**Author:** security agent (Cooker security domain)
**Date:** 2026-06-20
**Scope:** The *product / organizational* gates to ship Cooker commercially — payments/PCI, data-protection/privacy, multi-tenant isolation (compliance angle), SOC 2 readiness, and legal must-haves. **Not** a code re-audit.

**Baseline assumption:** the code-level CRITICALs from the 2026-06 full audit (`docs/audits/2026-06-full-audit.md`) — CR-1…CR-6, including the WebSocket-authz gap (CR-6) and the Vault read-modify-write race (CR-2) — are **fixed and merged in PR #115**. The four HIGHs from the 2026-05 review (`S26-05-04`, `-10`, `-13`, `-19`) are likewise closed (see that doc's "Closed in `claude/sec-quickwins-2026-05`" log). This document treats the binary as code-secure-at-HEAD and asks: *what does the **business** still owe before charging money?*

**Cross-references:** doc `02-*` (tenant-isolation build-farm design) — referenced here for the **risk/compliance** framing only, not duplicated. `docs/adr/0004-multi-tenancy.md` (the `owner_team_id`-now / `tenant_id`-deferred decision). `docs/product-plan.md` §7 (monetization ladder; explicitly: *"No solo-operated paid SaaS on today's codebase — fix-first list and an external pen-test come first."*).

---

## 0. The one-paragraph verdict

A **self-hosted launch** (Helm chart / binary, operator runs it on their own infra) is reachable today with light legal paperwork and a disclosure-policy fix — Cooker never touches the customer's secrets or payment data in that model, so the compliance surface is almost entirely the operator's. A **hosted-SaaS launch** is materially gated: Cooker would custody customer secrets, deploy credentials, and source/build artifacts **and** run untrusted customer build code in a shared farm, which is the single largest unsolved risk and the reason `docs/adr/0004-multi-tenancy.md` deferred the `tenant_id` boundary. SOC 2 Type II is premature until enterprise demand is proven, but the **evidence-generating controls** Cooker already ships (audit trail, RBAC, OIDC, signed releases, CI gates) mean the gap is process, not product. Payments are the easy part: Stripe-hosted Checkout keeps Cooker in PCI **SAQ-A** scope as long as no card data ever transits or lands in Cooker.

---

## 1. Payments / PCI posture

**Model: Stripe-hosted Checkout + Customer Portal. No card data touches Cooker → PCI DSS v4.0 SAQ-A.**

SAQ-A is the smallest PCI scope — it exists precisely for merchants who **fully outsource** cardholder-data handling to a validated third party (Stripe is a Level 1 PCI Service Provider). Cooker's server never sees a PAN; the browser is redirected to a Stripe-hosted page (or a Stripe-hosted/iframe-isolated Checkout), Stripe returns a token/`customer`/`subscription` id, and Cooker stores only those opaque references.

### Conditions that KEEP you in SAQ-A

| Condition | Why it matters | Cooker action |
|---|---|---|
| All card entry happens on a **Stripe-hosted page or Stripe iframe** (Checkout / Payment Element hosted fields) | The moment a PAN field is served from *your* DOM, you leave SAQ-A for SAQ-A-EP or SAQ-D | Use Checkout Session redirect or hosted Payment Element only; never a self-built card form |
| Cooker stores **only** Stripe object ids (`cus_…`, `sub_…`, `pi_…`), never PAN/CVV/expiry | Storing any card data → SAQ-D + full DSS | Billing table holds Stripe ids + plan/state; pin with a test asserting no card-shaped fields |
| Webhook endpoint verifies the **Stripe signature** (`Stripe-Signature` + signing secret) | An unverified billing webhook is a privilege/fraud vector (forge "payment succeeded") | Reuse Cooker's existing HMAC-verify discipline (`hmac.Equal`, constant-time — same pattern as the GitHub webhook, `source/github/webhook.go`); store the Stripe signing secret via `secretKeyRef`, never `values.yaml` |
| The **checkout page is served over TLS** and the SPA loading it is integrity-controlled (CSP, SRI on the Stripe JS) | SAQ-A v4.0 added explicit requirements (6.4.3 / 11.6.1) for scripts on payment pages and change-detection | Cooker already ships a strict CSP (`middleware_security.go`); add `js.stripe.com` to `script-src`/`connect-src` and nothing else; enable SRI |
| Annual SAQ-A self-attestation signed by the merchant | SAQ-A is self-assessed, not audited — but it must actually be completed yearly | Calendar reminder + keep the signed PDF in the compliance folder |

### What BREAKS SAQ-A (do not do these)

- Building a custom card form / collecting PAN in Cooker's frontend → **SAQ-A-EP or SAQ-D**.
- Proxying card data through the backend "to be helpful" → **SAQ-D**, plus you now custody PAN.
- Storing the CVV anywhere, ever → prohibited outright.
- Logging a full webhook body that contains card metadata into the audit trail. (Cooker's audit middleware is **body-free by construction** — `SECURITY.md` §Audit logging, and the `IsRedacted` contract — so this is already safe *as long as* the Stripe webhook route is added to the same body-free path and not given bespoke body-capture. Add the Stripe route to the `audit.IsRedacted` allowlist defensively per the `S26-05-11` recommendation.)

**Effort:** SAQ-A paperwork is ~half a day once Checkout is wired. The integration itself is doc-03 / billing scope; the security ask here is just the five "keep SAQ-A" guardrails above.

---

## 2. Data protection / privacy (GDPR / CCPA-CPRA)

### 2.1 Data inventory — what Cooker holds, and how sensitive it is

| Data class | Where it lives | Self-hosted custody | Hosted-SaaS custody | Sensitivity |
|---|---|---|---|---|
| User identity (OIDC subject, email, group→role) | `users` table; OIDC claims in-flight | Operator | **Cooker (controller)** | PII |
| User/env **secrets** (registry creds, deploy creds, SSH private keys) | `database` backend = envelope-encrypted Postgres rows; or external (Vault/AWS/GCP/KeepSave) | Operator | **Cooker (highest-value)** | Critical |
| Pipeline / app / environment definitions | Postgres (JSONB) | Operator | Cooker | Confidential (repo paths, cluster ids, build plans — the `S26-05-09` metadata leak class) |
| Run history, stage logs | Postgres + log sinks | Operator | Cooker | Confidential (logs can contain whatever a build prints, incl. secrets — `SECURITY.md` notes this repeatedly) |
| **Audit log** (subject, email, route template, status, IP) | stdout / file / `audit_events` table | Operator | Cooker | PII (email + IP), security-relevant |
| **Customer source code + build artifacts** | only in SaaS: build PVC / Kaniko context, pushed images | n/a (operator's own registry) | **Cooker (in the build farm)** | Critical IP |
| Billing references | `cus_…`/`sub_…` ids | n/a | Cooker | Low (no card data — see §1) |

The two rows that flip the entire compliance posture for SaaS are **secrets** and **customer source/artifacts** — in self-hosted these never leave the operator's trust boundary; in SaaS they become Cooker's to protect, breach-notify on, and erase on request.

### 2.2 Encryption status (confirmed against code/docs)

| Control | Status at HEAD | Evidence |
|---|---|---|
| Secrets **at rest** — AES-256-GCM envelope encryption | **Present & confirmed.** AEAD, per-encryption nonce, integrity built in | `SECURITY.md` "App webhook secrets and environment secrets sealed with AES-GCM (256-bit)"; `crypto/codec.go:57-66` per the 2026-05 review "What's good" list |
| Secrets **in transit** to external backends | **Present.** TLS enforced; KeepSave `http://` rejected by `Config.Validate()` | `SECURITY.md` secrets table; review "Validate rejects … KeepSave http://" |
| **Postgres TLS** in production | **Enforced** (`sslmode=require`+) | `S26-05-10` CLOSED; `Config.Validate()` refuses non-localhost `sslmode=disable` |
| Key **rotation** for `COOKER_SECRET_KEY` | **GAP — open.** Single-key codec, rotation needs coordinated restart | `S26-05-08` (MEDIUM, open). For SaaS this becomes a compliance requirement, not a nicety — a hosted provider must rotate KEKs without downtime |
| Audit log integrity / tamper-evidence | Append + async drop-on-full; **no signing/WORM** | `S26-05-24` — fine self-hosted, a SOC 2 / forensics gap for SaaS |

**Bottom line:** the cryptographic primitives are sound. The **operational** gap for SaaS is rotation (`S26-05-08`) and tamper-evident audit storage.

### 2.3 GDPR / CCPA obligations (these attach in the **SaaS** model; in self-hosted the operator is controller and Cooker is at most a software vendor)

| Obligation | What's needed | Status |
|---|---|---|
| **Data inventory / RoPA** (Art. 30) | §2.1 above is the seed | Draft exists here; needs to become a maintained record |
| **DPA** with each customer (Art. 28) | Standard processor DPA, SCCs annex for EU↔US | **Not written** — legal must-have for SaaS |
| **Sub-processor list & notice** | Disclose Stripe, the cloud provider (build farm + Postgres), the IdP if Cooker hosts one, Anthropic (only if AI triage is enabled — note it's **off by default** and egresses sanitized data, `SECURITY.md` §AI failure triage) | **Not published** — public page required |
| **Data residency** | Customers (esp. EU) will ask where secrets/source live. Single-region at launch is fine **if stated**; cross-region build scheduling must be disclosed | Decision + disclosure needed |
| **Right to erasure** (Art. 17) | Mechanics: delete user, their team's resources, secrets (incl. external-backend entries), audit rows after legal retention, and build artifacts/images. Today there's no tenant boundary (`ADR-0004` deferred `tenant_id`), so "erase one customer" is **not cleanly executable** in a shared DB | **Hard blocker for SaaS erasure SLAs** — see §3 |
| **Retention** | Audit DB already sweeps after `COOKER_AUDIT_DB_RETENTION` (default 90d). Need documented retention for secrets, logs, run history, artifacts | Audit retention exists; rest undocumented |
| **DSAR / access & portability** | Export a user's/team's data on request | No tooling; manual at launch is acceptable if volume is low |
| **Breach notification** (Art. 33/34: 72h to DPA) | Process + the audit trail to scope a breach | Audit trail supports scoping; the *process* is unwritten — see §5 |

**Key compliance fact:** the absence of a tenant boundary (`ADR-0004`, `S26-05-09`) is not just a security finding — it directly blocks **clean per-customer erasure and DSAR export** in a hosted multi-customer DB. That makes `tenant_id` (ADR-0004 Appendix A, ~3 weeks) a **GDPR prerequisite for SaaS**, not merely a feature.

---

## 3. Tenant-isolation security (the SaaS top risk — compliance/risk framing; design is doc 02)

**The threat in one sentence:** in hosted SaaS, Cooker executes **arbitrary customer-authored code** — Test stages, Custom shell scripts, and Dockerfiles `RUN` steps — in a shared build farm (`SECURITY.md` §"Test/Custom stage execution model", §"Image build isolation"), so a malicious customer build that escapes its sandbox can reach **other customers' secrets, source, and artifacts**, or the control plane. This is categorically the highest-severity risk class for any CI-as-a-service product.

### Why it is *contained* today and *exposed* in SaaS

- Cooker already does the right structural thing for **single-tenant**: user code never runs on the Cooker process — always a throwaway container (`kube` runner = one-shot `batch/v1.Job`, drops `ALL` caps, no privilege escalation, no host docker.sock). `noop` (default) doesn't run code at all. This is sound isolation *for one trust domain*.
- The exposure is **cross-tenant**: a single shared build namespace, a shared context PVC, shared node kernel, and (critically) **no `tenant_id` boundary in the data model** (`ADR-0004` deferred it) mean tenant A's job and tenant B's secrets coexist with only Kubernetes/kernel boundaries between them. The `docker`/`buildah` paths are even sharper — `buildah` needs `CAP_SETUID`/`CAP_SETGID` (PSA `baseline`), widening the kernel attack surface.

### Controls the SaaS model *requires* (compliance angle — design lives in doc 02)

| Control area | Requirement | Compliance driver |
|---|---|---|
| Build sandbox strength | Per-tenant kernel isolation (gVisor/Kata or per-tenant node pools); never the `docker`/host-socket path; consider rootless + user-namespaces over `CAP_SET*` | SOC 2 CC6 (logical access), customer DPA |
| Namespace/PVC isolation | One build namespace + context PVC **per tenant**, RBAC scoped so a job cannot read another tenant's Job/Secret/PVC | GDPR Art. 32 (separation), erasure |
| Network egress from build pods | Per-tenant NetworkPolicy; today the chart egress is wide-open on 443 (`S26-05-21`) — acceptable single-tenant, a cross-tenant exfil path in SaaS | Art. 32, exfil prevention |
| Data-model tenancy | `tenant_id` everywhere (ADR-0004 Appendix A) so authz, erasure, and audit filter by tenant | GDPR erasure/DSAR; `S26-05-09` |
| Secret scoping | A build job receives **only its own tenant's** resolved `secretRefs` | Critical-data confidentiality |
| Resource quotas / abuse | Per-tenant quotas + the existing per-user rate limiter (Redis-backed for multi-replica) to stop fork-bomb / cryptomining abuse | AUP enforcement, availability |

**Compliance verdict:** running untrusted multi-tenant build code is the control SOC 2 auditors and security-conscious customers will probe first. It is **not launch-ready for SaaS** on today's single-shared-namespace model. Self-hosted is unaffected — the operator owns the one trust domain. See doc 02 for the concrete farm design.

---

## 4. SOC 2 Type II — readiness gap & when it actually matters

**When it matters:** SOC 2 Type II is an **enterprise-sales unlock**, not a launch gate. Self-hosted and SMB SaaS buyers rarely require it; mid-market+ procurement does. `docs/product-plan.md` is explicit that multi-tenancy and SAML are enterprise features deferred from launch — SOC 2 sits in the same bucket. **Pursue it when a deal asks for it, not before** (a Type II report also requires a 3–12 month observation window, so you cannot retroactively shortcut it — start the *clock*, i.e. control operation + evidence capture, early, but don't pay for the audit until there's demand).

### What Cooker already has (evidence Cooker *generates* today)

| TSC area | Cooker has | Evidence source |
|---|---|---|
| **Logical access (CC6)** | OIDC + PKCE, RBAC 4-tier default-deny, step-up MFA on destructive routes, API-token least-privilege with hash-only storage | `SECURITY.md` §Auth, §RBAC |
| **Change management (CC8)** | CI gates (`go vet`, `-race`, lint, build), PR-based flow, signed releases (cosign keyless + Rekor), SHA-pinned Actions | `SECURITY.md` §Supply chain; CLAUDE.md §CI |
| **Logging/monitoring (CC7)** | Structured per-mutation audit trail (subject, email, route, status, IP), queryable `audit_events`, drop-counter metric | `SECURITY.md` §Audit logging |
| **Vendor mgmt (CC9)** | Pinned deps, conformance suite, but no formal vendor register | partial |
| **Data security** | AES-GCM at rest, TLS in transit, Postgres TLS enforced, non-root container, NetworkPolicy | `SECURITY.md` §Data Security |

### The org-process gaps (these are *people/process*, not code)

| Gap | What's missing | Effort |
|---|---|---|
| Policies | Written InfoSec, access-control, incident-response, change-mgmt, vendor-mgmt, data-retention policies | M (templates exist; ~1–2 wks to adapt) |
| Access reviews | Periodic review of who has admin to prod/GitHub/cloud; offboarding checklist | Process |
| Monitoring → alerting | Audit log exists, but no documented alert/triage runbook tied to it; tamper-evident storage missing (`S26-05-24`) | M |
| Vendor register & risk | Formal list of sub-processors with risk ratings (Stripe, cloud, IdP, Anthropic-if-enabled) | S |
| Background/HR controls | For any employees/contractors | Process |
| Vendor security review | An **external pen-test** before SaaS (product-plan §7 names this as a precondition) | external |

**Verdict:** Cooker is unusually well-positioned on the *technical* controls for a pre-revenue product — the audit trail, RBAC, OIDC, and signed-release supply chain are real SOC 2 evidence. The gap is the **organizational program** (policies, reviews, formal vendor mgmt, an IR process) plus the **observation window**. Don't start the audit until enterprise demand is concrete; *do* turn on evidence capture (alerting on the audit log, access-review cadence) early so the clock can start cheaply.

---

## 5. Legal / launch must-haves

| Artifact | Self-hosted | Hosted-SaaS | Notes |
|---|---|---|---|
| **Terms of Service** | License (OSS license + any commercial-use terms) | Required | SaaS ToS: liability cap, no-warranty, termination |
| **Privacy Policy** | Minimal (telemetry only, if any) | Required | Must list data collected (§2.1) and sub-processors |
| **DPA + SCCs** | Not needed (operator is controller) | **Required** (Art. 28) | Offer a signable DPA; SCCs for EU↔US transfer |
| **Acceptable Use Policy** | Recommended | **Required & load-bearing** | Users run **arbitrary build/deploy code** — the AUP must prohibit cryptomining, DoS, malware build/distribution, scanning, and reserve the right to kill abusive builds. This is the legal backstop to the §3 technical controls |
| **SLA document** | n/a | Required for paid tiers | Uptime target + credits; be honest — single-replica + in-memory state is a SPOF (`2026-06-full-audit.md` §"in-memory state SPOF") until Redis-backed HA is the default |
| **`security.txt` + disclosure policy** | **Both** | **Both** | `SECURITY.md` has a reporting address (`security@cooker-ci.example.com`) and a 48h-ack / 7-day-fix commitment — but the address is a **placeholder `.example.com`** and there is **no `/.well-known/security.txt`** (RFC 9116). Fix both before any public launch — this is cheap and currently broken |
| **Status page** | Optional | Required | Public status + incident history; ties to the SLA |
| **Incident-notification commitment** | n/a | Required | GDPR 72h-to-DPA; contractual customer-notice window in the DPA. The audit trail (§4) is what lets you scope an incident; the *process* is unwritten |
| **Vulnerability disclosure / bug bounty** | Recommended | Recommended | The `SECURITY.md` policy is a good start once the email is real |

**Immediate, cheap, currently-broken:** the `SECURITY.md` reporting email is a `.example.com` placeholder and there is no `security.txt`. These should be fixed regardless of launch model — a researcher cannot reach you today.

---

## 6. Prioritized launch checklist

Legend — **M**andatory / **R**ecommended / **L**ater. Effort: S (<1d), M (days–1wk), L (weeks).

### 6.1 Self-hosted launch (operator runs Cooker on their infra)

| # | Item | Priority | Effort |
|---|---|---|---|
| 1 | Replace placeholder `security@cooker-ci.example.com` with a real monitored address | **M** | S |
| 2 | Publish `/.well-known/security.txt` (RFC 9116) | **M** | S |
| 3 | OSS license + any commercial-use terms; clear "you operate it, you're the data controller" statement | **M** | S |
| 4 | Privacy Policy covering any product telemetry (or "we collect nothing") | **M** | S |
| 5 | Ship the production security checklist (`SECURITY.md` already has it) as launch docs; ensure OIDC-on, Kaniko/crane, TLS, scoped RBAC are the documented defaults | **M** | S |
| 6 | `COOKER_SECRET_KEY` rotation runbook (interim coordinated-restart procedure for `S26-05-08`) | **R** | S |
| 7 | Status page / changelog for the project | **R** | S |
| 8 | Enable Renovate (digest pinning — `S26-05-14`/`-15` close automatically) | **R** | S |

### 6.2 Hosted-SaaS launch (Cooker custodies customer data + runs their code)

Everything in 6.1, **plus**:

| # | Item | Priority | Effort |
|---|---|---|---|
| 1 | **Tenant isolation: `tenant_id` data model** (ADR-0004 Appendix A) — gates erasure/DSAR + cross-tenant authz | **M** | L (~3wk) |
| 2 | **Per-tenant build-farm isolation** (gVisor/Kata or per-tenant node pools; per-tenant namespace+PVC+NetworkPolicy; never host-socket builders) — see doc 02 | **M** | L |
| 3 | **External penetration test** (product-plan §7 precondition) | **M** | external |
| 4 | DPA + SCCs offered to customers | **M** | M (legal) |
| 5 | SaaS ToS, Privacy Policy w/ sub-processor list (Stripe, cloud, IdP, Anthropic-if-AI-on), AUP for arbitrary code | **M** | M (legal) |
| 6 | Stripe Checkout/Portal wired keeping **SAQ-A** (§1 guardrails); annual SAQ-A attestation | **M** | S–M |
| 7 | Right-to-erasure + DSAR-export tooling (depends on #1) | **M** | M |
| 8 | `COOKER_SECRET_KEY` **online rotation** (dual-key codec, `S26-05-08`) — a hosted KEK must rotate without downtime | **M** | M |
| 9 | HA / no-SPOF: Redis-backed rate-limit + WS ticket + WS hub as the **default** (Cooker already gates this in `Config.Validate()` for multi-replica — make it the shipped config) | **M** | M |
| 10 | Per-tenant quotas + abuse controls (cryptomining/DoS) backing the AUP | **M** | M |
| 11 | Tamper-evident / WORM audit storage + alerting on the audit stream (`S26-05-24`) | **R** | M |
| 12 | Public status page + SLA doc + incident-notification process (72h GDPR + contractual) | **M** | M |
| 13 | Tighten NetworkPolicy egress from build pods (`S26-05-21`) and same-namespace ingress (`S26-05-20`) for the multi-tenant farm | **M** | M |

### 6.3 Enterprise tier (sold to mid-market+ / regulated buyers)

Everything in 6.2, **plus**:

| # | Item | Priority | Effort |
|---|---|---|---|
| 1 | **SOC 2 Type II** — start evidence capture early, commission the audit when a deal requires it (not before) | **M** (when demand) | L (3–12mo window) |
| 2 | Written InfoSec / access-control / IR / change-mgmt / vendor-mgmt / retention policies | **M** | M |
| 3 | Scoped grant model — per-team/namespace roles (ADR-0004 notes flat `groupRoleMap` is insufficient; W11 Enterprise §4) | **M** | L |
| 4 | SAML (roadmap C2) for enterprise IdPs | **R** | L |
| 5 | RBAC group-map live reload / session revocation (`S26-05-03`, `-05`) — enterprises expect deprovisioning to take effect without a restart | **R** | M |
| 6 | Periodic access reviews, vendor risk register, customer-facing security questionnaire pack | **M** | Process |
| 7 | Data-residency choice (region pinning) + sub-processor change-notice commitments | **R** | M |

---

## 7. Cross-references

- `SECURITY.md` — current threat model; AES-GCM at-rest, audit body-free contract, build-isolation model, CSP.
- `docs/audits/2026-05-security-review.md` — `S26-05-08` (key rotation), `S26-05-09` (IDOR/authz → tenancy), `S26-05-11` (audit redaction contract), `S26-05-20`/`-21` (NetworkPolicy), `S26-05-24` (audit rotation/tamper).
- `docs/audits/2026-06-full-audit.md` — CR-6 (WS authz, fixed PR #115), CR-2 (Vault race, fixed), in-memory-state SPOF theme (HA gating).
- `docs/adr/0004-multi-tenancy.md` — `owner_team_id` now / `tenant_id` deferred (Appendix A is the SaaS-erasure prerequisite).
- `docs/product-plan.md` §7 — monetization ladder; *"No solo-operated paid SaaS on today's codebase — fix-first list and an external pen-test come first."*
- doc `02-*` (peer) — multi-tenant build-farm isolation design (referenced, not duplicated).
- doc `03-*` (peer) — billing/Stripe integration (this doc owns the PCI-scope guardrails only).
```
