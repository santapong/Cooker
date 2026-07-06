# OWASP ASVS v4.0.3 Level-2 Audit — Cooker

**Date:** 2026-07-02
**Branch:** `claude/three-project-vercel-plan-hrd22f`
**Scope:** Cooker backend (Go/Gin), frontend (React/TS), and deploy tooling (Helm/K8s/Dockerfile/CI), assessed against OWASP ASVS v4.0.3 Level 2. Cross-referenced to the internal `S26-05` finding series.

---

## Executive summary

Cooker's security posture is strong: parameterized SQL throughout, constant-time webhook HMAC on all four Git providers, single-use short-TTL WebSocket tickets, a strict app-served CSP, a non-root drop-all-caps container, API tokens stored only as SHA-256 hashes of 256-bit random secrets, and CORS that never sends `Allow-Credentials`. The docker-socket RCE-to-host path has already been removed from the raw K8s manifests.

This audit found **no Critical issues** and **one High** (an IDOR / broken-object-level-authorization gap that is deliberately deferred pending a multi-tenancy product decision). The Medium findings — a production fail-open auth path, an unthrottled credential endpoint, a vulnerable frontend dependency, and the absence of CI dependency scanning — are all self-contained and **fixed in this PR**.

`govulncheck` could not be run in the audit sandbox (egress to `vuln.go.dev` is blocked), so it is added as a CI step instead of run locally. `npm audit` on the frontend reported 2 moderate advisories (react-router open redirect), now fixed by FIX-3.

### Severity rollup

| Severity | Count | Disposition |
|---|---|---|
| Critical | 0 | — |
| High | 1 | Report-only (F1 IDOR — needs multi-tenancy decision + migration) |
| Medium | 4 | **All Fixed-in-PR** (FIX-1 … FIX-4) |
| Low | 4 | Report-only |
| Info | 1 | By design |

---

## Fixed-in-PR findings

### FIX-1 — Production boots with no auth path (dev-admin fail-open)

- **ASVS:** V1.2.3, V14.1.1 (secure-by-default config); **CWE-1188** (insecure default initialization).
- **Severity:** Medium (CVSS 3.1 **9.6 if triggered** — a misconfigured prod boot yields a fully unauthenticated admin API).
- **CVSS 3.1 vector:** `AV:N/AC:L/PR:N/UI:N/S:C/C:H/I:H/A:H` (9.6, if the fail-open state is reached).
- **Location:** `backend/internal/config/config.go` (former `~:625-627`); reached via `auth/oidc.go` `devHandler()`.
- **S26-05 cross-ref:** new (S26-05 config-hardening series).
- **Reachability:** Only when `COOKER_ENV=production` **and** both `COOKER_OIDC_ENABLED=false` and `COOKER_LOCAL_AUTH_ENABLED=false`. Previously `Validate()` only `slog.Warn`-ed and booted; the OIDC middleware then fell back to `devHandler()`, injecting a dev user with `RoleAdmin` on every request → unauthenticated admin API.
- **Remediation:** Promoted the warning to a hard validation error appended to `problems`, so `Validate()` fails closed in production. Added `TestValidate_Production_NoAuth` (asserts the error) and `TestValidate_Production_LocalAuthOnlySatisfiesAuth` (proves local-auth-only remains valid). Two existing multi-replica "OK" tests were updated to set a real auth path (`OIDC.Enabled: true`), reflecting the new invariant.
- **Status:** **Fixed-in-PR.**

### FIX-2 — Credential endpoints have no application-layer rate limit

- **ASVS:** V2.2.1 (anti-automation on authentication); **CWE-307** (improper restriction of excessive authentication attempts).
- **Severity:** Medium.
- **CVSS 3.1 vector:** `AV:N/AC:L/PR:N/UI:N/S:U/C:L/I:N/A:N` (5.3).
- **Location:** `backend/internal/server/router.go` (`~:14-17`) — `POST /api/v1/auth/local/{signup,signin}` registered on `s.router` **outside** the `/api/v1` group, so the per-user limiter never applied.
- **S26-05 cross-ref:** relates to S26-05-19 (rate-limit coverage).
- **Reachability:** Only when `COOKER_LOCAL_AUTH_ENABLED=true`. Without a throttle these routes allowed unbounded password guessing / signup spam per source IP.
- **Remediation:** Added a dedicated **in-memory per-source-IP** limiter with a tight fixed budget (**5/min, burst 5**), mirroring the existing webhook-limiter precedent (`newRateLimiter` + a new `localAuthMiddleware()` keyed on `ClientIP()`). Wired ahead of both handlers in `registerRoutes`; the limiter is held on `Server.localAuthRateLimiter` so `Close()` stops its gc goroutine. Added `TestLocalAuthRateLimit_ThrottlesPerIP` and `TestLocalAuthRateLimit_PerIPIsolation`. Documented in `SECURITY.md` as in-memory/per-process (multi-replica needs edge limiting).
- **Status:** **Fixed-in-PR.**

### FIX-3 — react-router open redirect (vulnerable frontend dependency)

- **ASVS:** V5.1.5 (safe redirects), V14.2.1 (no vulnerable components); **CWE-601** (open redirect) via **GHSA-2j2x-hqr9-3h42**.
- **Severity:** Medium.
- **CVSS 3.1 vector:** `AV:N/AC:L/PR:N/UI:R/S:C/C:L/I:L/A:N` (6.1).
- **Location:** `frontend/package-lock.json` — `react-router` / `react-router-dom` in the vulnerable `6.7.0 – 6.30.3` range (a `//`-prefixed path is reinterpreted as a protocol-relative URL → open redirect).
- **S26-05 cross-ref:** new (frontend dependency hygiene).
- **Reachability:** Only where the app performs a same-origin redirect from user-influenced path input; still fixed as defense-in-depth.
- **Remediation:** `npm audit fix` (non-`--force`) bumped `react-router` / `react-router-dom` to **6.30.4** — a patch bump **within 6.x**, no breaking API change; `package.json` spec (`^6.22.0`) unchanged, only the lockfile moved. Post-fix `npm audit --omit=dev` reports **0 vulnerabilities**; `npm run lint`, `npm run build`, and `npm test` all pass. (Remaining audit noise is dev-only — vite/esbuild/vitest — and out of scope for shipped code.)
- **Status:** **Fixed-in-PR.**

### FIX-4 — CI has no dependency / vulnerability scanning

- **ASVS:** V14.2.1, V1.14.6 (component inventory & vuln monitoring); **CWE-1104** (use of unmaintained/unvetted third-party components).
- **Severity:** Medium.
- **CVSS 3.1 vector:** `AV:N/AC:H/PR:N/UI:N/S:U/C:L/I:L/A:L` (4.8).
- **Location:** `.github/workflows/ci.yml` — no `govulncheck` (backend) and no `npm audit` gate (frontend).
- **S26-05 cross-ref:** new (supply-chain hardening; complements S26-05-15 action pinning).
- **Reachability:** Process gap — a future vulnerable dependency could merge undetected.
- **Remediation:** Added a `govulncheck` step to the backend job (pinned `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` — call-graph aware, does **not** modify `go.mod`, respecting the no-`go mod tidy` constraint), and an `npm audit --audit-level=high --omit=dev` step to the frontend job (scoped to browser-shipped deps). **Not run locally** — the sandbox blocks `vuln.go.dev`; the gate runs in CI.
- **Status:** **Fixed-in-PR.**

---

## Report-only findings

### F1 — IDOR / broken object-level authorization (High)

- **ASVS:** V4.2.1 (object-level access control); **CWE-639** (authorization bypass through user-controlled key).
- **Severity:** High.
- **CVSS 3.1 vector:** `AV:N/AC:L/PR:L/UI:N/S:U/C:H/I:L/A:N` (7.1).
- **Location (representative):** `backend/internal/handler/app.go:53` (`GetApp`), `environment.go:28` (`ListEnvironments`), `host.go:66` (`GetHost`), `pipeline.go:129` (`GetPipeline`) — plus the full read/update route list for apps, environments, hosts, and pipelines.
- **S26-05 cross-ref:** **S26-05-09.**
- **Reachability:** Every authenticated viewer can read (and, per role, update) **any** app/env/host/pipeline by id — there is no per-object ownership check. Object ids are sequential/guessable. Today the RBAC role gate is the only control; there is no tenant or owner scoping.
- **Remediation (sequenced):**
  1. Make a **multi-tenancy product decision** (ADR-0004) — is Cooker single-tenant-per-install or multi-tenant?
  2. **Before** adding any ownership column, land the explicit Update-DTO split (see Mass-assignment below) so a new `created_by` / `team_id` field can't be mass-assigned.
  3. Add a `created_by` / `team_id` ownership column via migration `024_owner_team`, backfill, and enforce ownership scoping in the store/service layer (not the handler).
- **Status:** **Report-only** — requires a product decision + schema migration; out of scope for this self-contained-fix PR.

### Mass-assignment latent risk (Low)

- **ASVS:** V5.1.2 (mass-assignment protection); **CWE-915**.
- **Severity:** Low (latent).
- **Location:** `UpdateApp` / `UpdatePipeline` handlers — bind the whole model on update.
- **S26-05 cross-ref:** **S26-05-28.**
- **Reachability:** Safe **today** (no privilege-bearing fields on the bound structs), but becomes a privilege-escalation vector the moment F1's ownership field lands — a caller could set `created_by`/`team_id` on themselves.
- **Remediation:** Introduce explicit `UpdateApp` / `UpdatePipeline` request DTOs that whitelist mutable fields, landed **before** F1's column. This is the ordering dependency called out in F1 step 2.
- **Status:** **Report-only.**

### S26-05-08 — Single-key `COOKER_SECRET_KEY`, no rotation (Low)

- **ASVS:** V6.4.1 (key management); **CWE-320**.
- **Severity:** Low.
- **Reachability:** A single AES-256 key encrypts all secrets at rest with no key-versioning / rotation path; compromise or routine rotation requires a full re-encrypt with downtime.
- **Remediation:** Add a key-id/version envelope so ciphertext records which key sealed them, enabling rolling rotation. Track as a follow-up (keyed to the KeepSave/secret-backend roadmap).
- **Status:** **Report-only.**

### Webhook decrypt-before-HMAC (Low, mitigated)

- **ASVS:** V13.1.3; **CWE-407** (inefficient algorithmic complexity under adversarial input).
- **Severity:** Low (mitigated).
- **Location:** `/webhooks/{github,gitlab,gitea,bitbucket}` — each receiver does a DB lookup + per-repo secret decryption **before** the HMAC/token check.
- **Reachability:** An unauthenticated sender could amplify that pre-verification work — but the **dedicated per-source-IP webhook limiter** (C-webhook-logs) runs ahead of the receiver and the idempotency middleware, bounding the amplification. In-memory/per-process (edge limiting still recommended for multi-replica).
- **Remediation:** None required; documented residual. Optionally reorder to a keyed lookup that avoids decryption until after a cheap constant-time token pre-filter where the provider protocol allows.
- **Status:** **Report-only** (accepted, mitigated).

### MFA passthrough-by-default + token-type MFA bypass (Info, by design)

- **ASVS:** V2.8 (context of MFA); **CWE-287**.
- **Severity:** Informational (documented design).
- **Reachability:** `RequireMFA` is a no-op unless `COOKER_OIDC_MFA_ACR_VALUES` is configured; local-auth and API-token principals carry no `acr`/`amr` claims, so MFA-gated routes are reachable by those principal kinds even when MFA is configured for OIDC users.
- **Remediation:** Documented in `SECURITY.md` ("Trade-offs and limits of the local path"). Operators requiring MFA on destructive routes must route those users through OIDC. No code change.
- **Status:** **Report-only** (by design).

---

## Notable PASSing controls

- **Parameterized SQL everywhere** — no string-concatenated queries; includes the `$1::interval` cast fix in the retention path (no interpolated interval literal).
- **Safe shell-out** — process invocations use an argument slice (`exec.Command(name, args...)`), never `sh -c` with an interpolated string, so there is no shell-metacharacter injection surface.
- **Constant-time webhook HMAC on all four providers** — GitHub, GitLab, Gitea, Bitbucket signature checks use constant-time comparison.
- **WebSocket auth** — single-use, 60-second tickets issued via `POST /api/v1/ws-tickets`, bound to a user and consumed atomically; no Bearer token in the query string.
- **Strict app-served CSP** — set in `internal/server/middleware_security.go`.
- **Container hardening** — image runs as **non-root UID 65532** with all capabilities dropped; Helm `securityContext` is secure-by-default.
- **API tokens** — 256-bit `crypto/rand` secrets, persisted only as **SHA-256 hashes** (no plaintext at rest); expiry and revocation honored.
- **CORS** — no `Allow-Credentials: true` (deliberately removed; a bearer-token API has no need for credentialed CORS).
- **docker.sock** — the raw-K8s docker-socket bind-mount has already been removed; production `Config.Validate()` refuses `COOKER_BUILDER=docker` / `COOKER_PUSHER=docker`.

---

## Scanning notes

- **`govulncheck`:** could not run in the audit sandbox (egress to `vuln.go.dev` is blocked). Added to CI (backend job) instead — pinned `go run golang.org/x/vuln/cmd/govulncheck@latest ./...`, which is call-graph aware and does not modify `go.mod`.
- **`npm audit` (frontend):** pre-fix = **2 moderate** (react-router open redirect, GHSA-2j2x-hqr9-3h42); **fixed by FIX-3** (patch bump to 6.30.4). Post-fix production audit (`--omit=dev`) = **0 vulnerabilities**. A frontend CI gate (`npm audit --audit-level=high --omit=dev`) was added to keep it that way.
