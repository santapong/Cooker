# 01 — Billing, Monetization & Self-Hosted Licensing

> **Status:** Design proposal (greenfield — no billing code exists in the repo today). Written 2026-06-20 for the production-readiness / launch-readiness team.
> **Scope:** Stripe billing for a hosted "Cooker Cloud" SaaS, **and** an offline license-key model for self-hosted commercial tiers. This document is a design, not an implementation; no code was changed to produce it.
> **Hard dependency, stated up front:** every *hosted, billed-per-customer* feature below depends on the **deferred `tenant_id` work** in [`docs/adr/0004-multi-tenancy.md`](../adr/0004-multi-tenancy.md). That ADR shipped `owner_team_id` and explicitly **deferred** `tenant_id` (PM decision A3-defer, 2026-05-13, revisit Q4 2026). You cannot safely bill or quota a "customer" until the customer is a first-class isolation boundary. §3 and §6 make this dependency explicit and unskippable.

---

## 0. TL;DR

| Question | Answer |
|---|---|
| What are we building? | Two revenue surfaces that share one `internal/billing` package: (a) a hosted SaaS subscription path on Stripe, (b) an offline license-key gate for self-hosted commercial tiers. |
| Where does it land in the layering? | `internal/billing/` (domain + Stripe adapter + entitlements engine + license verifier), `internal/handler/billing.go` (thin HTTP: checkout-session create, portal redirect, webhook receiver), one new store interface (`BillingStore`) with memory + postgres impls, migration `024_billing.up.sql`. Follows the handler→service→store layering in [`CLAUDE.md`](../../CLAUDE.md). |
| PCI scope? | **Minimal — SAQ-A**. We use Stripe-hosted **Checkout** + **Customer Portal**; no card number, CVC, or PAN ever touches Cooker. Cooker stores only opaque Stripe IDs (`cus_…`, `sub_…`, `price_…`). |
| Webhook safety? | Reuse the two patterns Cooker already ships: HMAC signature verification (mirror `internal/source/github/webhook.go:VerifySignature`) and the idempotency middleware (`internal/server/middleware_idempotency.go`, keyed here on Stripe's `event.id` instead of `X-GitHub-Delivery`). |
| Blocking prerequisite for *hosted* billing | `tenant_id` from ADR-0004 Appendix A (`010_tenancy.up.sql`). Self-hosted licensing has **no** such prerequisite and can ship first. |
| Stripe product structure (verified 2026-06) | Products → Prices → Subscriptions, with the new **Meter / Meter Events** API for usage (the legacy `subscriptionItems.createUsageRecord` was removed in API version `2025-03-31.basil`). |

---

## 1. Pricing / plan model

### 1.1 Reconciling with the shipped mockup

The pricing mockup at [`design_handoff_cosmic_theme/design-references/cosmic-pricing.html`](../../design_handoff_cosmic_theme/design-references/cosmic-pricing.html) already commits to three named tiers and — critically — a **per-replica** metering axis, not per-seat:

| Mockup tier | Mockup price | Mockup positioning | FAQ commitment |
|---|---|---|---|
| **Explorer** | $0 / forever | solo · side projects | "unlimited pipelines and runs", self-hosted single binary |
| **Crew** | $49 / replica / mo | teams · production | multi-replica HA, OIDC/RBAC/MFA, managed secrets, 14-day trial |
| **Constellation** | Custom | enterprise · multi-tenant | SSO group→role, KeepSave multi-tenant secrets, air-gapped, SLA |

The mockup's FAQ makes two binding promises we must honour or change deliberately:

1. *"You're billed **per running Cooker process**, not per user or per pipeline. **Seats are unlimited** on every paid tier."*
2. *"Explorer is the single-binary build, self-hosted… You only pay when you need multi-replica HA, enterprise SSO, or managed secret backends."*

This is an unusual but **defensible** choice for an infra tool, and it maps cleanly onto things Cooker already gates by configuration: multi-replica needs Redis-backed hub/tickets/rate-limit (see `product-plan.md` §6.3 matrix row 3 and `docs/guides/MULTI_REPLICA.md`); OIDC/MFA, Vault/AWS/GCP/KeepSave secrets backends, and audit/OTLP are all already feature-flagged. **The mockup is effectively a feature-flag-and-replica license, not a usage meter.** That is the cheapest thing to build and the recommendation for v1.

### 1.2 The tension: "per replica" (mockup) vs. usage metering (this brief)

The brief asks me to propose metering dimensions that "fit Cooker" — seats, concurrent builds, build minutes, deploy targets, retention, environments. Per-replica billing ignores all of these. The honest resolution:

- **For self-hosted (license-key) tiers: keep per-replica + feature-flag gating.** It is verifiable offline (count running processes via a heartbeat or honour-system), it needs no usage pipeline, and it matches the mockup verbatim. This is §4.
- **For hosted Cooker Cloud: per-replica is meaningless** (the customer doesn't run the process — we do). Cloud must meter something the customer consumes. The natural Cloud meters are **concurrent builds** (the scarce, costly resource — every build burns a build pod) and **build-minutes** (the compute the customer actually spends). Seats stay unlimited to honour the mockup promise; environments / deploy targets / retention become **plan limits** (hard caps per tier), not metered overages.

So the model is **hybrid**: tier membership gates *features and limits*; Cloud additionally meters *build consumption*. This matches where the industry landed in 2026 — Stripe itself shipped LLM-token usage billing in March 2026 and is pushing "subscription + usage upgrade" as the default SaaS shape ([PYMNTS 2026](https://www.pymnts.com/news/artificial-intelligence/2026/stripe-introduces-billing-tools-to-meter-and-charge-ai-usage/)).

### 1.3 Proposed concrete tiers

I keep the mockup's three names so the marketing page doesn't change, and add the Cloud-only meter columns. **"Self-hosted" and "Cloud" are two delivery modes of the same tier ladder.**

| Dimension | Explorer (Free) | Crew (Pro/Team) | Constellation (Enterprise) |
|---|---|---|---|
| **Self-hosted price** | $0 / forever | $49 / replica / mo (mockup) | Custom (annual contract) |
| **Cloud price (proposed)** | $0 | $39 / mo base + usage | Custom |
| **Seats** | unlimited | unlimited (per mockup FAQ) | unlimited |
| **Replicas (self-hosted)** | 1 | unlimited | unlimited |
| **Concurrent builds (Cloud meter)** | 1 | 3 included, then metered | negotiated |
| **Build-minutes (Cloud meter)** | 200 / mo | 2,000 / mo included, then $/min | pooled / committed |
| **Environments** | Dev only (1) | Dev/Staging/Prod (3) | unlimited |
| **Deploy targets** | K8s + Fly + Render (mockup) | + ECS + Cloud Run + SSH | all + air-gapped |
| **Run retention** | 7 days | 90 days (chart default) | configurable / export |
| **Secrets backends** | Postgres (AES-GCM) | + Vault / AWS / GCP | + KeepSave multi-tenant |
| **OIDC + RBAC + MFA** | — | ✓ | ✓ + SSO group→role map |
| **Cron-triggered runs** | — | ✓ | ✓ |
| **Audit log + OTLP** | — | basic | full + append-only export |
| **Support** | community | priority | SLA + dedicated |

Notes on choices:
- **Seats are deliberately NOT a meter** — honours the mockup FAQ and sidesteps the most common SaaS-pricing complaint. If we ever reverse this, it's a marketing decision and a one-line plan-limit change, not an architecture change.
- **Concurrent builds is the right Cloud scarcity meter** because each build is a pod that costs us money and is already the thing the per-user rate limiter throttles (`pipelines/:id/run`, `docker/images/build`, `apps/:id/deploy` — see CLAUDE.md "Rate limiting"). The entitlements engine in §3 extends exactly that limiter.
- **Environments / targets / retention are hard limits, not overage meters.** Simpler UX ("upgrade to add Staging") and no surprise invoices. They map to plan flags, enforced at create-time.

### 1.4 Plan → entitlement mapping (the canonical table the code consumes)

This table is the source of truth that `internal/billing/entitlements.go` encodes as a Go map keyed by plan ID. It is the contract between billing and the rest of the system.

| Entitlement key | Type | Explorer | Crew | Constellation |
|---|---|---|---|---|
| `max_replicas` | int (0=unlimited) | 1 | 0 | 0 |
| `max_concurrent_builds` | int | 1 | 3 | 0 |
| `included_build_minutes` | int/mo | 200 | 2000 | 0 |
| `max_environments` | int | 1 | 3 | 0 |
| `allowed_deploy_targets` | set | {k8s,fly,render} | +{ecs,cloudrun,ssh} | * |
| `retention_days` | int | 7 | 90 | 0 |
| `feature.oidc_mfa` | bool | false | true | true |
| `feature.managed_secrets` | bool | false | true | true |
| `feature.sso_group_map` | bool | false | false | true |
| `feature.audit_otlp` | bool | false | true | true |

---

## 2. Stripe integration architecture

### 2.1 Which Stripe products to use (verified 2026-06)

Stripe's current building blocks, confirmed against current docs ([Stripe Meter Events API](https://docs.stripe.com/api/billing/meter-event), [usage-based pricing plans](https://docs.stripe.com/billing/subscriptions/usage-based/pricing-plans)):

| Stripe object | What it is | Cooker usage |
|---|---|---|
| **Product** | A sellable thing | One Product per tier: `Cooker Crew`, `Cooker Constellation`. Explorer is free → no Product needed. |
| **Price** | A price point attached to a Product | A recurring flat Price per tier (`$39/mo`), plus a **metered** Price for build-minute overage (Cloud only). Use `lookup_key` so code references stable keys, not generated `price_…` IDs. |
| **Subscription** | A customer's recurring commitment to one or more Prices | One Subscription per tenant; flat Price + (Cloud) metered Price as subscription items. |
| **Meter / Meter Events** | The 2024-GA usage system | One Meter `cooker_build_minutes`. We POST **Meter Events** as builds complete. **Important:** the legacy `subscriptionItems.createUsageRecord` API was **removed in API version `2025-03-31.basil`** — any metered Price now *requires* a backing Meter. Design for Meters from day one. |
| **Checkout Session** | Stripe-hosted payment page | The only place a card is entered. Created server-side, user redirected to Stripe. |
| **Customer Portal** | Stripe-hosted self-serve billing | Plan upgrade/downgrade, card update, cancel, invoice history — we build none of this UI. |

**Build vs. buy verdict (the brief's §5):** **Buy Stripe Billing wholesale.** Do not build an invoicing/dunning/proration engine. Stripe Billing already does proration on mid-cycle plan change, retries on failed payment (dunning via Smart Retries), tax (Stripe Tax), and the entire portal UI. A metered add-on (Metronome, Orb, m3ter) is **overkill** at launch — Cooker's only meter is build-minutes, which Stripe Meters handles natively. Revisit a dedicated metering vendor only if usage dimensions proliferate (e.g. per-GB egress, per-image-scan) past what Stripe Meters models comfortably.

### 2.2 Checkout vs. Subscriptions API vs. Portal — what we call when

| Flow | Mechanism | Why |
|---|---|---|
| First purchase / start trial | **Checkout Session** (`mode: subscription`) | Stripe-hosted; SAQ-A PCI scope; handles SCA/3DS, tax, trial. |
| Upgrade / downgrade / cancel / update card | **Customer Portal** session | Zero UI to build; Stripe enforces proration and emits the same webhooks. |
| Programmatic edge cases (Enterprise custom price, seat grant) | **Subscriptions API** server-side | Only for sales-assisted Constellation deals; not the self-serve path. |
| Usage reporting (Cloud) | **Meter Events API** | POST `{event_name: "cooker_build_minutes", payload:{stripe_customer_id, value}}` as each build finishes, with an idempotency key = run/stage ID. |

### 2.3 Webhook events to handle

| Event | Action in Cooker |
|---|---|
| `checkout.session.completed` | Provision: link `cus_…`/`sub_…` to the tenant, set `plan`, set status `active`/`trialing`. The activation moment. |
| `customer.subscription.created` | Belt-and-suspenders alongside the above; idempotent so double-handling is safe. |
| `customer.subscription.updated` | Plan change, trial→active, cancel-at-period-end set, `past_due` transitions. Re-read entitlements. **Source of truth for plan state.** |
| `customer.subscription.deleted` | Downgrade tenant to Explorer (Free) — do **not** hard-delete data; the §3 graceful-degradation rules apply. |
| `invoice.payment_failed` | Enter **dunning**. Mark tenant `past_due`; surface a banner; Stripe Smart Retries do the actual retrying. After final retry exhaustion Stripe emits `subscription.deleted` → downgrade. |
| `invoice.paid` | Clear any `past_due` flag; confirm renewal. |
| `customer.subscription.trial_will_end` | (Optional) Notify via existing Slack/Email notifier 3 days out. |

**Plan state lives in Stripe, mirrored in Cooker.** Cooker's DB is a *cache* of Stripe's truth, kept fresh by webhooks. On webhook receipt we re-fetch the Subscription object rather than trust the event body's mutable fields, to avoid event-ordering races (Stripe does not guarantee ordered delivery).

### 2.4 Idempotent, signature-verified webhook handling — reuse what Cooker already has

Cooker already solved both halves of safe webhook handling. The billing receiver **reuses both patterns**, it does not invent new ones:

1. **Signature verification** — mirror `internal/source/github/webhook.go`'s `VerifySignature` (constant-time HMAC compare, returns a typed `ErrBadSignature` → 401). Stripe signs with `Stripe-Signature` (HMAC-SHA256 over `timestamp.payload`, plus a replay-window timestamp check). The official `stripe-go` SDK exposes `webhook.ConstructEvent(payload, sigHeader, secret)` which does exactly this — **use the SDK helper** rather than re-implementing the timestamp-tolerance logic, but model the handler's error posture on the GitHub verifier (drop + 401 on mismatch, never 500).

2. **Idempotency** — Cooker's `internal/server/middleware_idempotency.go` already de-dupes mutating requests keyed by `Idempotency-Key` or `X-GitHub-Delivery`. **Extend the same middleware to also read Stripe's `event.id`** as the dedup key on the webhook route, OR (cleaner, recommended) de-dupe *inside* the billing service with a dedicated `processed_stripe_events` table keyed on `event.id` with a unique constraint — webhook handlers are not idempotent by replaying a cached HTTP response (the side effect is a DB write, not the response body), so a DB-level unique-insert is the correct dedup primitive here. The existing `internal/idempotency` `Store` interface is the right shape to model it on.

> Design note: the HTTP-response-replay idempotency middleware is correct for *Run* buttons (where replaying the 202 body is the desired outcome). For Stripe webhooks the desired outcome is "apply the state change exactly once" — that's a DB unique constraint on `event.id`, not a cached response. Use the right tool: middleware for client-facing mutations, unique-insert for webhook side effects.

### 2.5 Where it lands in the layering

Strictly follows CLAUDE.md's handler→service→store rule. No HTTP types in the service; no business logic in the handler; no `panic` outside startup.

```
backend/internal/
├── billing/                         NEW — domain + adapters (no HTTP, no Gin)
│   ├── billing.go                   Plan, Entitlements, Subscription domain types
│   ├── entitlements.go              the §1.4 table as a Go map; Resolve(plan) → Entitlements
│   ├── stripe/                      Stripe adapter (the ONLY package importing stripe-go)
│   │   ├── client.go                CreateCheckoutSession, CreatePortalSession, ReportMeterEvent
│   │   └── webhook.go               ConstructEvent wrapper; maps Stripe event → domain command
│   └── license/                     self-hosted license verifier (see §4) — no Stripe import
├── handler/
│   └── billing.go                   NEW — thin HTTP: POST /checkout, POST /portal, POST /webhooks/stripe
├── service/
│   └── billing_service.go           NEW — orchestrates: webhook event → store write → entitlements refresh
└── store/
    ├── store.go                     ADD BillingStore interface (Get/UpsertSubscription, MarkEventProcessed)
    ├── memory/billing.go            NEW — memory impl (store-parity rule: both impls always)
    └── postgres/
        ├── billing.go               NEW — postgres impl
        └── migrations/
            ├── 024_billing.up.sql   NEW (next free number after 023_api_tokens)
            └── 024_billing.down.sql NEW (reversible — drops tables)
```

New env vars (registered in `internal/config/config.go` alongside the existing `getEnv*` block, and enforced by `Config.Validate()` in production when `COOKER_BILLING_ENABLED=true`, mirroring how KeepSave secrets are validated at `config.go:413-423`):

| Env var | Purpose | Validated in prod when |
|---|---|---|
| `COOKER_BILLING_ENABLED` | master switch (default false → self-hosted gets no billing) | — |
| `COOKER_STRIPE_SECRET_KEY` | Stripe API key (via `secretKeyRef` in Helm) | billing enabled |
| `COOKER_STRIPE_WEBHOOK_SECRET` | webhook signing secret (`whsec_…`, via `secretKeyRef`) | billing enabled |
| `COOKER_STRIPE_PRICE_CREW` / `_CONSTELLATION` | Price `lookup_key`s | billing enabled |
| `COOKER_BILLING_PORTAL_RETURN_URL` | where Portal returns the user | billing enabled |

Helm wiring mirrors the existing OIDC/KeepSave secret pattern: `COOKER_STRIPE_SECRET_KEY` and `COOKER_STRIPE_WEBHOOK_SECRET` rendered via `secretKeyRef`, never inline. SECURITY.md gets a "Stripe webhook" entry (signature secret handling, the 401-on-bad-sig posture) per CLAUDE.md's "changes to auth/secrets must update SECURITY.md" rule.

### 2.6 Proposed schema (`024_billing.up.sql`)

```sql
-- One row per billed boundary. tenant_id is the FK once ADR-0004 Appendix A lands.
-- Until then (self-hosted-licensing-only phase) this table is unused on self-hosted;
-- Cloud MUST NOT enable billing until tenant_id exists (see §6 gate).
CREATE TABLE billing_subscriptions (
  tenant_id            BIGINT PRIMARY KEY REFERENCES tenants(id),   -- depends on 010_tenancy
  stripe_customer_id   TEXT NOT NULL UNIQUE,
  stripe_subscription_id TEXT UNIQUE,
  plan                 TEXT NOT NULL DEFAULT 'free'
                         CHECK (plan IN ('free','crew','constellation')),
  status               TEXT NOT NULL DEFAULT 'active'
                         CHECK (status IN ('trialing','active','past_due','canceled')),
  current_period_end   TIMESTAMPTZ,
  cancel_at_period_end BOOLEAN NOT NULL DEFAULT false,
  updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Webhook dedup: exactly-once application of Stripe events.
CREATE TABLE billing_processed_events (
  event_id     TEXT PRIMARY KEY,          -- Stripe event.id
  event_type   TEXT NOT NULL,
  processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Cloud usage ledger (build-minutes), pre-aggregation, for reconciliation vs Stripe Meters.
CREATE TABLE billing_usage_events (
  id           BIGSERIAL PRIMARY KEY,
  tenant_id    BIGINT NOT NULL REFERENCES tenants(id),
  meter        TEXT NOT NULL,             -- 'cooker_build_minutes'
  value        NUMERIC NOT NULL,
  run_id       BIGINT,                    -- idempotency anchor for Meter Event
  reported_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (meter, run_id)                  -- dedup meter reporting per run
);
```

> The `REFERENCES tenants(id)` FKs are the load-bearing dependency on ADR-0004 Appendix A. On a self-hosted-licensing-only build these tables are simply never created/used; the migration is guarded behind `COOKER_BILLING_ENABLED` documentation and the Cloud-only deployment. Per CLAUDE.md, the migration ships as a reversible up/down pair tested against ephemeral Postgres in CI, and adds no fields to existing `handler/*.go` requests without this migration.

---

## 3. Entitlements / quota enforcement

### 3.1 The entitlements service

A new `internal/billing/entitlements.go` resolves `plan → Entitlements` (the §1.4 table). It is the single read-path the rest of the system consults. It is **pure and cached** — given a plan ID it returns the limit struct with no I/O; the plan ID itself is read from `BillingStore` (which is webhook-fed) and cached in-process with a short TTL, invalidated on `customer.subscription.updated`.

```
Entitlements struct {
    MaxReplicas, MaxConcurrentBuilds, IncludedBuildMinutes int
    MaxEnvironments, RetentionDays int
    AllowedDeployTargets map[string]bool
    Features map[string]bool   // oidc_mfa, managed_secrets, sso_group_map, audit_otlp
}
```

### 3.2 How limits map to runtime checks (extend, don't replace)

| Limit | Enforcement point | Mechanism |
|---|---|---|
| `max_concurrent_builds` | the **existing per-user rate limiter** on `pipelines/:id/run`, `docker/images/build`, `apps/:id/deploy` | Generalize it from per-*user* to per-*tenant*, and from a fixed rate to a plan-derived concurrency gate. This is the cleanest extension point — the throttle already lives on exactly these three routes (CLAUDE.md "Rate limiting"). Over-limit → HTTP 429 with an upgrade CTA in the body, not a silent drop. |
| `included_build_minutes` (Cloud) | build-completion hook in the executor / run FSM | On stage/run completion, compute minutes, write `billing_usage_events`, POST a Stripe Meter Event. Crossing the included threshold doesn't *block* — it bills overage (Stripe handles the invoice). |
| `max_environments` | `handler/environment.go` create path | Pre-create check against `BillingStore`+entitlements → 402/403 with "upgrade to add Staging". |
| `allowed_deploy_targets` | `selectXxx` deploy-target wiring / create path | Reject target creation outside the plan's set; 402. |
| `max_replicas` (self-hosted) | startup + license check | Counted via heartbeat (Cloud) or honour-system/license claim (self-hosted, §4); soft-warn not hard-fail. |
| `retention_days` | the existing retention CronJob (`retention.enabled`, `daysToKeep`) | Plan-derived `daysToKeep`. |
| `feature.*` | feature flags at the relevant subsystem boot | A locked feature returns 402 "available on Crew" rather than 404. |

**Design principle: entitlements are checked at the boundary, enforced by existing mechanisms.** We are not building a parallel enforcement framework — we are feeding plan-derived numbers into the rate limiter, the retention CronJob, the create handlers, and the feature flags that already exist.

### 3.3 Graceful over-limit UX

| Situation | Behaviour |
|---|---|
| Concurrent-build cap hit | 429 + "You're at your plan's N concurrent builds. Builds queue, or upgrade." Build **queues** (the durable job queue already exists) rather than failing. |
| Build-minutes exhausted (Cloud) | No block — overage meters and bills. Banner: "Over included minutes; usage billed at $X/min." |
| Environment / target create over cap | 402 Payment Required + inline upgrade link. Never a silent failure. |
| `past_due` (dunning) | **Read-only grace**, not lockout: existing pipelines keep running for the grace window; new runs/builds gated with a "payment failed, update card" banner deep-linking to the Stripe Portal. Honours the marketing brand rule against punitive UX. |
| `canceled` → downgraded to Free | Data retained; features above Free locked, not deleted. Re-subscribe restores access. |

### 3.4 The tenant-isolation dependency (explicit)

Quota enforcement is meaningless without a tenant to scope it to. Today, per ADR-0004, the isolation unit shipped is `owner_team_id`; `tenant_id` is **deferred**. Concretely:

- **A "subscription" bills a tenant, not a team.** A tenant may contain many teams (ADR-0004 §3 makes `tenant_id` a strict superset of `owner_team_id`). Billing the wrong boundary (team) would let one paying org's free teams enumerate another's resources — the exact `S26-05-09` IDOR the ADR closes only at the team layer.
- **`billing_subscriptions.tenant_id` FK requires `tenants(id)`**, which only exists after `010_tenancy.up.sql` (ADR-0004 Appendix A).
- **Therefore: hosted Cooker Cloud billing is gated on `tenant_id`.** Self-hosted licensing (§4) is **not** — a single self-hosted install is implicitly one tenant, so the license just gates features/replicas for the whole instance with no isolation requirement.

This is the single most important sequencing constraint in this document and is re-stated in the §6 phase gates.

---

## 4. Self-hosted licensing

Self-hosted is where revenue can land **first**, because it has no `tenant_id` dependency (a self-hosted instance is one tenant by definition). The model:

### 4.1 Offline signed license keys (recommended) — not phone-home

**Recommendation: offline, signed, asymmetric license keys (Ed25519-signed JWT or a compact custom token). No mandatory phone-home.**

Rationale:
- Cooker's whole pitch is *self-hosted, single binary, air-gapped-capable* (Constellation tier explicitly lists "air-gapped"). A phone-home activation server that can revoke or brick an air-gapped install contradicts the product promise and is a support liability.
- Offline keys mean Cooker only needs the **public** key embedded in the binary to verify; the private signing key lives in our license-issuance service (out of band, tiny, not in the repo).
- A license is a signed claims blob:

```jsonc
{
  "lic_id": "lic_...",
  "tier": "constellation",
  "max_replicas": 0,            // 0 = unlimited
  "features": ["sso_group_map","audit_otlp","managed_secrets","air_gapped"],
  "customer": "ACME Corp",
  "issued_at": 1750000000,
  "expires_at": 1781536000,     // annual; expiry enforced offline
  "sig": "<Ed25519 over the canonical claims>"
}
```

- Verification lives in `internal/billing/license/verify.go`: parse, check Ed25519 signature against the embedded public key, check `expires_at`, return an `Entitlements` struct **identical in shape** to the Stripe path's output. This is the key design unification — see §4.3.

### 4.2 What to gate (and what NOT to)

| Gate | How | Notes |
|---|---|---|
| Tier features (SSO group→role, KeepSave multi-tenant secrets, air-gapped, full audit/OTLP) | feature flags read from license claims | Same flags the Stripe path sets. |
| `max_replicas` (the mockup's per-replica axis) | soft check | **Honour-system / soft-warn, NOT hard-fail.** Hard-blocking replicas in a self-hosted OSS-core tool invites circumvention and breaks HA during incidents. Log + dashboard warning if running replicas exceed the licensed count; escalate to sales, not to a crash. |
| License expiry | offline `expires_at` check | On expiry, **degrade to Explorer (Free) features**, do not brick — the instance keeps running, paid features lock. |

**Do NOT gate:** the core CI/CD loop, number of pipelines/runs (mockup promises "unlimited"), or anything that would make an expired license take down production deploys. The license gates *commercial features*, never *availability*.

### 4.3 How SaaS and self-hosted coexist (one entitlements shape, two sources)

The unifying design: **both paths produce the same `Entitlements` struct.** The rest of Cooker never knows whether the plan came from Stripe or a license file.

```
                         ┌─────────────────────────┐
   Stripe webhook ──────▶│  billing_service        │──┐
   (Cloud)               │  reads BillingStore.plan │  │
                         └─────────────────────────┘  │
                                                       ├──▶ Entitlements ──▶ rate limiter,
   License file   ──────▶┌─────────────────────────┐  │                     create handlers,
   (self-hosted)         │  license.Verify(file)    │──┘                     feature flags,
                         │  → claims                │                        retention CronJob
                         └─────────────────────────┘
```

- `COOKER_BILLING_ENABLED=true` + Stripe configured → Cloud path (entitlements from `BillingStore`, webhook-fed).
- `COOKER_LICENSE_FILE=/etc/cooker/license.jwt` → self-hosted path (entitlements from verified license claims).
- Neither set → Explorer (Free) defaults. Dev mode (`COOKER_OIDC_ENABLED=false`) implicitly gets full features for local development (mirrors the existing dev-admin injection).
- They are **mutually exclusive per install** — a hosted Cloud node is never also running a license file. The entitlements resolver picks the source at boot and logs which one is active.

This means **the entitlements engine, the rate-limiter extension, and every enforcement point in §3.2 are built once and serve both revenue models.** Only the *source adapter* differs.

---

## 5. Build vs. buy summary

| Concern | Build | Buy | Decision |
|---|---|---|---|
| Card capture, PCI | — | Stripe Checkout (SAQ-A) | **Buy.** No card data in Cooker, ever. |
| Subscription lifecycle, proration | — | Stripe Billing Subscriptions | **Buy.** |
| Dunning / failed-payment retries | — | Stripe Smart Retries | **Buy.** |
| Self-serve plan management UI | — | Stripe Customer Portal | **Buy.** |
| Tax | — | Stripe Tax (toggle) | **Buy** when international sales start. |
| Usage metering | thin Meter-Event reporter | Stripe Meters (native) vs. Metronome/Orb (dedicated) | **Buy Stripe Meters** — single meter (build-minutes) doesn't justify a metering vendor. Revisit if dimensions multiply. |
| Entitlements → runtime enforcement | the §3 engine + rate-limiter extension | (no off-the-shelf fit; couples to Cooker internals) | **Build** — small, and it's the only genuinely Cooker-specific part. |
| Self-hosted license issuance + verify | Ed25519 sign (issuer) + verify (binary) | license-vendor SaaS (Keygen, etc.) | **Build verify** (tiny, must be offline/air-gap-safe); **issuer** can be a 50-line internal tool or a vendor later. |

**Stripe API assumptions noted (verified 2026-06):** Products/Prices/Subscriptions are stable. The **Meters / Meter Events** API is GA and is the *only* supported usage path — the legacy `subscriptionItems.createUsageRecord` was removed in API version `2025-03-31.basil`, so any usage design that references usage records is stale; this design targets Meters. Stripe's March-2026 push toward usage-based (LLM-token) billing confirms Meters as the strategic surface. Pin the `Stripe-Version` header explicitly in the `stripe-go` client so an API-version bump can't silently change behaviour.

Sources:
- [Stripe Meter Events — API reference](https://docs.stripe.com/api/billing/meter-event)
- [Stripe usage-based pricing plans](https://docs.stripe.com/billing/subscriptions/usage-based/pricing-plans)
- [Stripe metered-billing implementation guide, 2026](https://www.buildmvpfast.com/blog/stripe-metered-billing-implementation-guide-saas-2026)
- [Stripe introduces tools to meter and charge AI usage, 2026](https://www.pymnts.com/news/artificial-intelligence/2026/stripe-introduces-billing-tools-to-meter-and-charge-ai-usage/)

---

## 6. Phased delivery plan

Effort estimates follow the repo's house style (solo-maintainer days). **Prerequisites are hard gates, not suggestions.**

### Phase B0 — Self-hosted licensing (ship first; no tenant dependency)

| Item | Effort | Prereq |
|---|---|---|
| `internal/billing/license/` verify (Ed25519 + claims + expiry) | ~1 d | none |
| `internal/billing/entitlements.go` (§1.4 table → struct) | ~0.5 d | none |
| Wire entitlements into the **existing rate limiter** (per-tenant concurrency), env-create check, feature flags | ~1.5 d | none |
| License-issuance internal tool (Ed25519 sign) | ~0.5 d | none |
| Soft `max_replicas` warning + dashboard surface | ~0.5 d | heartbeat exists? |
| Docs (SECURITY.md license-verify note, UAT.md, admin guide) | ~0.5 d | — |
| **Phase total** | **~4.5 d** | **— (ships independently)** |

**Outcome:** Constellation/Crew can be sold to self-hosted customers via signed license files. Revenue with zero Stripe and zero `tenant_id`.

### Phase B1 — Hosted Cooker Cloud billing (GATED on `tenant_id`)

> **GATE — do not start B1 until ADR-0004 Appendix A `010_tenancy.up.sql` is merged.** PM committing to Cooker Cloud is itself a precondition the ADR names (Decision A → "yes"). Estimated tenancy work: **~3 weeks** (ADR-0004 Appendix A "Estimate"). This dwarfs the billing work and is the real critical path.

| Item | Effort | Prereq |
|---|---|---|
| **`tenant_id` multi-tenancy** (ADR-0004 App. A) | **~3 wk** | PM commits to Cloud |
| `024_billing.up/down.sql` (the §2.6 schema) | ~0.5 d | tenant_id |
| `BillingStore` interface + memory + postgres impls (parity) | ~1 d | tenant_id |
| `internal/billing/stripe/` adapter (Checkout, Portal, ConstructEvent) | ~1.5 d | Stripe account |
| `handler/billing.go` + service (webhook receiver, dedup via unique-insert, sig verify mirroring GitHub) | ~1.5 d | above |
| Config + Helm `secretKeyRef` wiring + `Config.Validate()` gates | ~0.5 d | above |
| Frontend: pricing page is already designed (mockup); add Checkout/Portal redirect buttons + `past_due` banner | ~1 d | above |
| Tests (race, webhook replay, dunning state machine) | ~1 d | above |
| **Phase total (billing only, excl. tenancy)** | **~8 d** | **tenant_id (~3 wk)** |

### Phase B2 — Cloud usage metering (build-minutes)

| Item | Effort | Prereq |
|---|---|---|
| Stripe Meter `cooker_build_minutes` + metered Price | ~0.25 d | B1 |
| Build-completion → Meter Event reporter (idempotent on run_id) + `billing_usage_events` ledger | ~1 d | B1 |
| Reconciliation job (ledger vs. Stripe, drift alert) | ~0.5 d | above |
| Overage UX banner | ~0.5 d | above |
| **Phase total** | **~2.25 d** | **B1** |

### Critical-path summary

```
Self-hosted revenue:  B0 (4.5 d)  ──────────────▶ shippable NOW, no blockers
Cloud revenue:        [tenant_id ~3 wk] ──▶ B1 (8 d) ──▶ B2 (2.25 d)
                              ▲
                              └─ HARD GATE: PM commits to Cloud + ADR-0004 App. A merged
```

**Recommendation:** ship **B0 first** — it monetizes the Enterprise/self-hosted audience the marketing brief excludes from the *free* launch ICP, requires no tenancy work, and validates the entitlements engine that B1 reuses. Treat B1/B2 as a separate business decision gated on the (still-unmade, per ADR-0004) Cooker Cloud commitment. Do not build B1 speculatively — the marketing brief (`docs/marketing/strategy.md` §7) and product-plan (§7 anti-goals) both warn against a solo-operated paid SaaS before fix-first + external pen-test, and ADR-0004 warns against tenancy work before a hosted-Cloud signal.

---

## 7. Open questions for the PM

1. **Per-replica vs. per-seat vs. usage** — the mockup commits to per-replica + unlimited seats. This design honours it for self-hosted and adds build-meters for Cloud. Confirm the mockup's promises are binding, or we re-price.
2. **Cloud commitment** — B1/B2 and the entire `tenant_id` 3-week investment are gated on a "yes" to Cooker Cloud that ADR-0004 says is unmade. Is that decision being made now, or does B0 (self-hosted licensing) stand alone for this launch?
3. **License expiry posture** — confirm "degrade to Free, never brick" is acceptable (it's the air-gap-safe choice and the one I recommend).
4. **Open-core licensing** — product-plan §7 says keep the core Apache-2.0 and add a CLA before open-core. The commercial *features* gated by license/Stripe must live in a clearly-delineated part of the tree (or a separate module) to keep the OSS-core promise clean. Recommend deciding the source-tree split (and CLA) before B0 lands.
