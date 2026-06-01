# 12 · Reality Check — Gotchas, Defaults & What's Not (Yet) Real

> **Purpose:** a single honest page that gathers the facts most likely to mislead a reader who reasons
> from intuition or marketing rather than source. If a claim elsewhere in these docs (or in your head)
> contradicts this page, **this page wins** — every entry here was confirmed against the code.

This complements the per-chapter detail; it exists so you don't have to read all 11 chapters to avoid
the common false assumptions. Three buckets: surprising-but-true, not-implemented, and
aspirational/non-blocking.

## A · Surprising-but-true (reality ≠ intuition)

| Assumption a reader might make | What's actually true | Where |
|---|---|---|
| "It builds with Docker out of the box." | Builder/Pusher/Deployer **default to `noop`** (`COOKER_BUILDER`/`_PUSHER`/`_DEPLOYER` = `noop`). A fresh install does nothing dangerous until you opt into a real backend. | [05](05-extension-points.md) |
| "Secrets are just local AES or just KeepSave." | There are **five** secrets backends: `database` (default, AES-GCM), `keepsave`, `vault`, `aws`, `gcp`. | [05](05-extension-points.md) |
| "With the default secrets backend, secret endpoints just work." | The `database` backend needs `COOKER_SECRET_KEY`; without it the codec is inactive and secret endpoints return **503** (fail-safe, not fail-open). | [05](05-extension-points.md), [06](06-auth-and-security.md) |
| "Secret promotion works for any backend." | Only **KeepSave** implements `secrets.Promoter`. The `database` backend returns **501** (`ErrPromotionUnsupported`). | [09](09-environments-and-promotion.md) |
| "Environments are hardcoded alpha/beta/prod." | Environments are **user-defined** by `Name` and sequenced by an integer **`Order`**. There are no hardcoded tier names anywhere. | [09](09-environments-and-promotion.md) |
| "All entities use optimistic concurrency." | Four tables carry a `version` column / 409 conflict path: **pipelines, environments, apps, hosts**. Runs don't. | [04](04-data-model.md) |
| "Idempotency caches every response." | Only **2xx** responses are cached (TTL **24h**); replays set `Idempotency-Replayed: true`. Non-2xx is never cached. | [02](02-backend.md) |
| "WebSocket messages are structured JSON." | Frames carry the **raw payload**; the client infers meaning from the channel name. No envelope. | [07](07-realtime-and-concurrency.md) |
| "It uses golang-migrate." | The migration runner is **custom** (`//go:embed` + `pg_advisory_lock` + `schema_migrations`, per-migration transaction). | [04](04-data-model.md) |
| "The OpenAPI spec is generated from code." | [`../openapi.yaml`](../openapi.yaml) is **hand-maintained** (OpenAPI 3.1) with full route coverage; it is not auto-generated. The `swaggo`-generated `backend/docs/api/swagger.*` covers flagship endpoints only. | [02](02-backend.md) |
| "The error envelope has codes." | It's a flat `{"error": "<string>"}` — no error code, no nested object. | [02](02-backend.md) |
| "UAT enables auth." | UAT is **auth-off by design** (dev-admin injected). Enabling OIDC requires a `.env.uat` preset — never flip the flag in compose. | [08](08-deployment.md) |

## B · Designed / present but NOT (fully) implemented

Don't assume these work just because the scaffolding or an ADR exists.

| Feature | Status | Where |
|---|---|---|
| **`CKR-LOG/1` binary log protocol** | **Proposal only** ([`../protocols.md`](../reference/protocols.md)). Today's WS frames are raw payloads. | [07](07-realtime-and-concurrency.md) |
| **Multi-tenancy** | [ADR-0004](../adr/0004-multi-tenancy.md) **Accepted** (Q4-2026) but **not implemented** — resources are effectively single-tenant today. No tenant isolation. | [10](10-platform-subsystems.md) |
| **Run-state FSM enforcement** | `internal/runstate` (wraps `looplab/fsm`) exists, but the executor still **writes statuses directly**; the FSM isn't yet the single source of transition enforcement. | [10](10-platform-subsystems.md) |
| **Governance on pipeline deploys** | The admission hook gates **app-deploy only**. Pipeline-defined deploys are **not gated yet** (slated v1.1). | [10](10-platform-subsystems.md) |
| **Helm lint / kubeconform in CI** | Backlog **P6.1** — YAML is staged in the backlog, not wired into CI yet. | [08](08-deployment.md) |

## C · Aspirational / non-blocking (looks enforced, isn't)

| Thing | Reality | Where |
|---|---|---|
| **golangci-lint** | Runs in CI but with `continue-on-error: true` — a lint failure does **not** block the job today. Effectively advisory. | [11](11-code-patterns-and-conventions.md) |
| **Strict layering / error-wrap / no-`panic`** | Convention enforced by **code review**, not by any linter or CI gate. | [11](11-code-patterns-and-conventions.md) |
| **No `localStorage` outside `auth/`, no backend URLs in components** | Convention / review-only — no automated rule enforces it. | [11](11-code-patterns-and-conventions.md) |

> **What *is* hard-enforced in CI:** `gofmt -l .`, `go vet ./...`, `go test -race`, frontend `tsc` /
> `npm run build`, and `npm run lint`. Everything else above is convention or aspirational. See
> [11-code-patterns-and-conventions.md](11-code-patterns-and-conventions.md).

## How to keep this page honest

When you change one of these realities (e.g. flip golangci-lint to blocking, implement multi-tenancy,
generate the OpenAPI spec), **move the entry** out of this page in the same PR — update the relevant
chapter and delete/relocate the row here. A stale reality-check page is worse than none. For the open
work backlog that feeds bucket B, see [`../../backlog.md`](../../backlog.md).

---

> _Verified against `main` @ `dd93402` on 2026-05-30. If you change the described behaviour, update this chapter in the same PR._
