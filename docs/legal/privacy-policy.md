> ⚠️ **TEMPLATE — requires review by qualified legal counsel before use.**
> This is a non-binding draft, not legal advice. Whether you are a controller or processor,
> whether SCCs apply, and the exact rights/retention language are legal determinations.
> Do not publish until counsel has reviewed it. Fill in every `[BRACKET]` placeholder.

# Cooker — Privacy Policy (Template)

**Last updated:** [DATE]
**Data controller / publisher:** [LEGAL ENTITY NAME], [ADDRESS] ("**Cooker**", "**we**").
**Privacy contact / DPO:** [PRIVACY EMAIL] · [DPO NAME, if appointed].

This Privacy Policy explains what personal data Cooker processes, why, who we share it
with, and the rights you have. It covers the **hosted Cooker service**. For **self-hosted**
deployments, see [§9](#9-self-hosted-deployments).

---

## 1. Roles (controller vs processor)

- **[Hosted]** For account, billing, and security data we act as a **controller**. For the
  Customer Data you put into the Service (pipeline definitions, secrets, logs, run history)
  we act as a **processor** acting on your instructions; a Data Processing Addendum
  ("**DPA**") governs that relationship.
- **[Self-hosted]** **You** are the controller of all data the software processes; we are at
  most a software vendor and generally do not receive your data at all (see [§9](#9-self-hosted-deployments)).

## 2. Data we process

The table mirrors Cooker's actual data inventory
(`docs/launch/04-security-compliance-legal.md` §2.1).

| Data class | Examples | Why we process it | Sensitivity |
|---|---|---|---|
| **User identity** | OIDC subject id, email, group→role mapping | Authentication, RBAC, audit attribution | PII |
| **User / environment secrets** | Registry creds, deploy creds, SSH private keys | To build and deploy on your instruction; stored **envelope-encrypted (AES-256-GCM)** at rest or in an external secrets backend you configure | Critical |
| **Pipeline / app / environment definitions** | Repo paths, cluster ids, build plans | To run the product | Confidential |
| **Run history & stage logs** | Build/deploy outcomes, log output | Display, debugging, audit; **logs may contain whatever your build prints** | Confidential |
| **Audit log** | Subject, email, route, status, **IP address**, timestamp | Security, compliance, incident scoping | PII + security-relevant |
| **Customer source code & build artifacts** *(Hosted only)* | Build context, pushed images | To build/push on your instruction | Critical IP |
| **Billing references** *(Hosted only)* | Stripe customer / subscription ids (`cus_…`, `sub_…`) — **never card numbers** | Subscription management | Low |
| **Product/telemetry** *(if enabled)* | [Describe any usage analytics, or state "none"] | Product improvement | Low |

We **do not** store payment card numbers, CVV, or expiry — card data is handled entirely
by our payment processor (see [§4](#4-sub-processors)).

## 3. Legal bases (GDPR Art. 6)

- **Contract** — to provide the Service you signed up for.
- **Legitimate interests** — security, fraud/abuse prevention, audit logging, service
  improvement (balanced against your rights).
- **Legal obligation** — tax, accounting, lawful requests.
- **Consent** — only where required (e.g. optional analytics cookies). You may withdraw
  consent at any time.

## 4. Sub-processors

> **[PLACEHOLDER — maintain a live, public sub-processor list and notify customers of
> changes.]** The list below is illustrative. Confirm each vendor, its purpose, and its
> data-processing region before publishing.

| Sub-processor | Purpose | Data category | Region |
|---|---|---|---|
| **[Stripe]** | Payment processing | Billing identifiers, payment metadata (card data handled by Stripe, not us) | [US/EU] |
| **[Cloud provider — e.g. AWS / GCP]** | Hosting of the control plane, Postgres, build farm | All hosted Customer Data | [REGION] |
| **[Identity provider — if Cooker hosts one]** | Authentication | User identity | [REGION] |
| **[Email / notifications provider]** | Transactional email, alerts | Email address | [REGION] |
| **[Anthropic]** *(only if AI failure-triage is enabled)* | AI-assisted failure triage — **off by default**; egresses sanitized data only | Sanitized log excerpts | [REGION] |

We require sub-processors to provide adequate protection and to process data only on our
instructions. We will give **[30] days'** notice before adding a sub-processor so you can
object.

## 5. International transfers

Where personal data is transferred outside your region (e.g. EU/UK → US), we rely on
appropriate safeguards such as **Standard Contractual Clauses (SCCs)** and, where
applicable, the UK Addendum. **[State your primary data-residency region; if cross-region
build scheduling occurs, disclose it.]**

## 6. Retention

| Data | Retention | Notes |
|---|---|---|
| Account / identity | For the life of the account + **[30] days** | Then deleted or anonymized |
| Secrets / deploy credentials | Until you delete them or close the account | Encrypted at rest |
| Pipeline / app / env definitions | For the life of the account | |
| Run history & stage logs | **[90] days** (or your configured retention) | Configurable |
| Audit log | **[90] days** by default (`COOKER_AUDIT_DB_RETENTION`), then swept | Longer if law requires |
| Build artifacts / images *(Hosted)* | **[Specify]** | |
| Billing records | As required by tax/accounting law (**[7] years**) | |

After the retention period, data is deleted or irreversibly anonymized, subject to legal
holds. **Note:** loss of the encryption key (`COOKER_SECRET_KEY`) makes encrypted secrets
permanently unrecoverable by design.

## 7. Your rights

Depending on your location (GDPR / UK GDPR / CCPA-CPRA and similar), you may have the right
to: **access**, **correct**, **delete**, **port**, **restrict**, or **object to**
processing, and to **withdraw consent**. Under CCPA-CPRA you may also opt out of any "sale"
or "sharing" of personal information — **we do not sell personal information**.

To exercise rights, contact **[PRIVACY EMAIL]**. We will respond within the period required
by law (generally **[30] days**). We will not discriminate against you for exercising your
rights.

> **Erasure caveat (Hosted).** Cooker's current data model does not yet carry a per-tenant
> boundary (`tenant_id` is deferred — see `docs/adr/0004-multi-tenancy.md`). Until that
> lands, fully isolating and erasing **one** customer's data from a shared database is
> operationally constrained. Account-level deletion is supported; per-tenant erasure SLAs
> are gated on the tenancy work (`docs/launch/04-security-compliance-legal.md` §2.3).

## 8. Security

We use AES-256-GCM envelope encryption for secrets at rest, TLS in transit (including
enforced Postgres TLS in production), OIDC + PKCE authentication, default-deny RBAC,
non-root containers, and a structured audit trail. No system is perfectly secure; we
maintain a vulnerability-disclosure process (`SECURITY.md`). For data-breach handling we
aim to notify affected parties and regulators within the periods required by law (e.g.
GDPR's **72-hour** authority-notification window).

## 9. Self-hosted deployments

If you run Cooker yourself, **you are the data controller**. The software runs on your
infrastructure and we generally do not receive your data. This Policy then applies only to
any data you send us directly (e.g. support requests, optional telemetry if you enable it).
**[If the self-hosted build collects telemetry, describe exactly what and how to disable
it; otherwise state "the self-hosted build collects no telemetry."]**

## 10. Cookies **[Hosted]**

The hosted UI uses cookies/local storage strictly necessary for authentication and session
management (token storage is handled by the OIDC client library). **[List any non-essential
cookies and the consent mechanism, or state there are none.]**

## 11. Children

The Service is not directed to children under **[16]** and we do not knowingly collect their
data.

## 12. Changes

We may update this Policy and will post the new "Last updated" date; material changes will
be notified per [the Terms](terms-of-service.md) §13.

---

**Contact:** [PRIVACY EMAIL] · **EU representative (if required):** [NAME] · **DPO (if
appointed):** [NAME]
**Related:** [Terms of Service](terms-of-service.md) · [Acceptable Use Policy](acceptable-use-policy.md) · DPA *(to be drafted)*
