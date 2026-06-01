# 2026-05 W4 — `SECURITY.md` post-W3 walk

**Branch:** `claude/w4-research-security-walk-post-w3`
**Scope:** Verify `SECURITY.md` end-to-end after the W3 supply-chain section landed (PR #60). Cross-check every claim against `main` tip `bc9226b`.
**Method:** Read-only. Source file:line cites against each claim, keyed to `docs/audits/2026-05-security-review.md` (`S26-05-*` IDs).
**Companions:** `docs/audits/2026-05-security-review.md`, `docs/audits/2026-05-action-pinning.md`.

Severity legend: **Correct** / **Drift** / **Missing**.

---

## 1. Auth section (`SECURITY.md:27-87`)

### 1.1 Generic OIDC verify-failure (`S26-05-01`)
- Claim: `SECURITY.md:37` — `{"error":"authentication failed"}` (401) and `{"error":"provider unavailable"}` (503+Retry-After); upstream diagnostic logged server-side only.
- Source: `backend/internal/auth/oidc.go:166, 188, 200, 209, 222, 238` — six call sites all emit the two generic strings.
- **Correct.** `S26-05-01` closure holds.

### 1.2 RBAC table (`S26-05-19` — new sub-drift)
- Claim: `SECURITY.md:67-71` lists three roles (admin / operator / viewer). Operator row credits "approve environment promotions."
- Source: `backend/internal/auth/rbac.go:12-17` defines **four** roles including `RoleApprover`. `DefaultGroupRoleMap` (`rbac.go:120-125`) ships `cooker-approvers → approver` out of the box. `CanApprovePromotion` (`rbac.go:92-102`) gates on `RoleAdmin || RoleApprover` only — **operators are excluded**. Enforced at `handler/environment.go:271-275`.
- **Drift (MEDIUM).** Doc omits `approver` role; operator wrongly credited with promotion-approval rights.
- Remediation: add `approver` row; correct operator row; extend the built-in group-role-map example on line 73.

### 1.3 Step-up MFA
- Claim: `SECURITY.md:75-84`.
- Source: `backend/internal/auth/rbac.go:54-87` (`RequireMFA`).
- **Correct.**

### 1.4 WebSocket single-use tickets
- Claim: `SECURITY.md:152` — `POST /api/v1/ws-tickets`, 60s, consumed on first use.
- Source: `backend/internal/server/wsticket.go:13-28, 64-128`.
- **Correct.** `S26-05-06` is a benign LOW elsewhere.

---

## 2. Secrets section (`SECURITY.md:140-146`)

### 2.1 Adapter inventory
- Claim: `SECURITY.md:143` — "e.g., HashiCorp Vault, AWS Secrets Manager."
- Source: `backend/internal/config/config.go:411-433` switches on five backends: `database` (default), `keepsave`, `vault`, `aws`, `gcp`. Matches `internal/secrets/{database,keepsave,vault,awsm,gcpsm}/`.
- **Drift (LOW / INFO).** Doc only hints; doesn't enumerate. `COOKER_SECRETS_BACKEND` not surfaced.
- Remediation: short bullet list naming the five backends + the per-backend required env vars enforced by `Validate()`.

### 2.2 KeepSave wiring vs. `CLAUDE.md`
- `CLAUDE.md` (project root) says "KeepSave (P2 / PR G) is parked pending a walkthrough." Code at HEAD ships `backend/internal/secrets/keepsave/{keepsave.go (121L), client.go (178L), keepsave_test.go (158L)}` with `Validate()` gating at `config.go:413-423`.
- **Drift (INFO) — but in `CLAUDE.md`, not `SECURITY.md`.** Out of scope for a SECURITY.md PR; flag to CLAUDE.md owner.

### 2.3 Helm Postgres password (`S26-05-13`)
- Claim: `SECURITY.md:143`.
- Source: `deploy/helm/cooker/values.yaml` has no default `cooker` password; `templates/_helpers.tpl` `required`-guards `database.passwordSecretRef.name`.
- **Correct.** Closure holds.

### 2.4 Postgres `sslmode` (`S26-05-10`)
- Claim: `SECURITY.md:144`.
- Source: `backend/internal/config/config.go:373-389` parses URL, accepts only `require | verify-ca | verify-full` for non-localhost in production.
- **Correct.** Closure holds.

---

## 3. Container hardening (`SECURITY.md:127-138, 191-198`)

### 3.1 UID 65532
- Source: `deploy/docker/Dockerfile:90` — `USER 65532:65532`. **Correct.**

### 3.2 Raw-manifest docker.sock (`S26-05-04`)
- Claim: `SECURITY.md:138` — raw K8s manifest does NOT mount the socket.
- Source: `deploy/kubernetes/deployment.yaml:50-55` is an inline warning comment block only; no `docker-socket` volume or mount remains.
- **Correct.** Closure holds.

### 3.3 Pusher gate F-02 (PR #47)
- Source: `backend/internal/config/config.go:474-479` — `COOKER_PUSHER=docker is forbidden in production`.
- **Correct.**

### 3.4 Builder gate (PR #21)
- Source: `backend/internal/config/config.go:468-472` — `COOKER_BUILDER=docker is unsafe in production`.
- **Correct.**

---

## 4. Network section (`SECURITY.md:148-156`)

### 4.1 Postgres TLS — see §2.4. **Correct.**

### 4.2 Redis backends for multi-replica
- Claim: `SECURITY.md:178` covers only the rate limiter's multi-replica caveat.
- Source: `backend/internal/config/config.go:482-495` enforces **all three** — `RateLimit.Backend`, `WSTicket.Backend`, `WSHub.Backend` — must be `redis` when multi-replica in production.
- **Drift (INFO).** Reader scaling Cooker horizontally learns only at `Validate()` failure that WS-ticket + WS-hub also need Redis.
- Remediation: one-sentence callout in §Network Security or §Rate limiting naming all three env-vars (`COOKER_RATE_LIMIT_BACKEND`, `COOKER_WS_TICKET_BACKEND`, `COOKER_WS_HUB_BACKEND`).

### 4.3 NetworkPolicy
- Source: `deploy/helm/cooker/templates/networkpolicy.yaml:1-2` gated by `networkPolicy.enabled`. **Correct.** `S26-05-20` (any same-namespace pod allowed) is still open separately and SECURITY.md does not claim it closed.

### 4.4 CORS deny-all + empty refusal (`S26-05-19`)
- Source: `backend/internal/config/config.go:420-424`. **Correct.**

---

## 5. Audit log (`SECURITY.md:158-165`)

### 5.1 Env-var contract (PR #21)
- Source: `backend/internal/config/config.go:342-344` defines `COOKER_AUDIT_ENABLED`, `COOKER_AUDIT_DESTINATION`, `COOKER_AUDIT_FILE_PATH`; validated at `config.go:458-465`.
- **Correct.**

### 5.2 Body-capture invariant
- Source: `backend/internal/audit/audit.go` IsRedacted exists. The claim "bodies are never captured" is accurate at HEAD; `S26-05-11` (no test pinning the contract) is open separately and SECURITY.md doesn't claim otherwise.
- **Correct (as stated).**

---

## 6. Supply chain / release signing (NEW in W3, PR #60)

### 6.1 Cosign keyless
- Claim: `SECURITY.md:86-103`.
- Source: `.github/workflows/release.yml:20` (`id-token: write`), `:57-58` (`sigstore/cosign-installer@d7d6bc7722e3daa8354c50bcb52f4837da5e9b6a # v3.8.1`), comments at `:107-108` describing the OIDC→Fulcio exchange.
- **Correct.**

### 6.2 `S26-05-15` action-pinning closure
- Claim: `SECURITY.md:105-107` — "All third-party actions in `.github/workflows/release.yml` are pinned to 40-character SHAs."
- Source for the narrow claim: `release.yml:34,45,58,68,74,84,99,125` — every `uses:` is a 40-char SHA. `grep -nE "uses: [^@]+@v[0-9]+($|[^a-f])" release.yml` returns nothing.
- Broader source: `docs/audits/2026-05-action-pinning.md` enumerates **17 still-unpinned `uses:` across 3 workflows** (`ci.yml`, `cooker-weekly.yml`, `oci-conformance.yml`). Confirmed by direct grep: `ci.yml:32,33,46,68,90,91,112,113,317,321`, `cooker-weekly.yml:26,34,38,57`, `oci-conformance.yml:38,40,80`. The most sensitive is `anthropics/claude-code-action@v1` at `cooker-weekly.yml:57`, which runs with `contents: write` + `pull-requests: write`.
- **Drift (MEDIUM, information omission).** SECURITY.md gives readers the impression `S26-05-15` is fully closed. Only the release-workflow half is.
- Remediation: one-line known-gap under §"Pinned action SHAs": "Non-release workflows (`ci.yml`, `cooker-weekly.yml`, `oci-conformance.yml`) currently reference floating major-version tags; closure tracked at `docs/audits/2026-05-action-pinning.md`."

### 6.3 OCI registry signature verification
- Claim: `SECURITY.md:113-119`.
- Source: `ghcr.io/santapong/cooker` path at `release.yml:115, 148`; verify commands at `docs/RELEASING.md:134, 148`.
- **Correct.**

### 6.4 `docs/SECURITY-RELEASE-VERIFY.md` link
- Claim (per task brief): SECURITY.md should link to `docs/SECURITY-RELEASE-VERIFY.md`.
- Source: file exists at `docs/SECURITY-RELEASE-VERIFY.md`. `grep -n "SECURITY-RELEASE-VERIFY" SECURITY.md docs/RELEASING.md` returns **zero matches** — orphaned from the doc graph.
- **Drift (MEDIUM, discoverability).** Walking from SECURITY.md only reaches `RELEASING.md §Step 4` (operator how-to); the security-side post-publish checklist is unreachable.
- Remediation: under §"Verifying a release" (`SECURITY.md:109-111`) add: "For the security-side post-publish checklist, see [`docs/SECURITY-RELEASE-VERIFY.md`](../guides/SECURITY-RELEASE-VERIFY.md)."

---

## 7. Bonus — `S26-05-23` SQL half

- Per `docs/audits/2026-05-security-review.md:420`, the operator-tunability half closed in PR #39; the SQL `fmt.Sprintf` half is "still tracked separately."
- Source: `backend/internal/store/postgres/run.go:142-143` retains `fmt.Sprintf("%d milliseconds", threshold.Milliseconds())` at HEAD.
- **Open finding correctly acknowledged.** No drift. SECURITY.md doesn't claim closure either way — right posture.

---

## Drift findings summary

All six drift findings from this W4 walk have been **closed** in `claude/w5-security-drift-bundle` (PR following #71). See the "Closed" section below for the per-finding closure note. The table is retained for historical reference.

| # | Section | Severity | Finding | Status |
|---|---|---|---|---|
| 1 | §1.2 RBAC table | Drift (MEDIUM) | `approver` role missing; operator wrongly credited with promotion-approval | **Closed** — see Closed §1 |
| 2 | §2.1 Secrets adapters | Drift (LOW) | Five backends ship; doc names only two managers; `COOKER_SECRETS_BACKEND` not surfaced | **Closed** — see Closed §2 |
| 3 | §2.2 KeepSave (CLAUDE.md, not SECURITY.md) | Drift (INFO) | CLAUDE.md says parked; adapter ships at HEAD | **Closed** — see Closed §3 |
| 4 | §4.2 Multi-replica Redis triad | Drift (INFO) | Rate-limit caveat only; WS-ticket + WS-hub Redis requirement undocumented | **Closed** — see Closed §4 |
| 5 | §6.2 `S26-05-15` scope | Drift (MEDIUM) | Implies action-pinning fully closed; 17 `uses:` across `ci.yml`/`cooker-weekly.yml`/`oci-conformance.yml` still float, including `claude-code-action@v1` with PR-write perms | **Closed** — see Closed §5 |
| 6 | §6.4 `SECURITY-RELEASE-VERIFY` link | Drift (MEDIUM) | File exists but unlinked from SECURITY.md / RELEASING.md | **Closed** — see Closed §6 |

**Drift count: 6.** 0 CRITICAL/HIGH; 3 MEDIUM, 1 LOW, 2 INFO. All documentation drift, none RCE-shaped. All six closed in `claude/w5-security-drift-bundle`.

## Explicitly verified Correct

`S26-05-01`, `S26-05-04`, `S26-05-10`, `S26-05-13`, `S26-05-19`, F-02 (PR #47), PR #21 (builder gate + audit env vars), PR #60 cosign keyless, `S26-05-23` SQL half (correctly tracked as still-open follow-up).

## Recommended SECURITY.md edits (out of scope here; for follow-up PR)

1. Approver role row in RBAC table + correct operator row.
2. Five-row secrets-backend table with env var per backend.
3. One-sentence multi-replica Redis triad callout.
4. `S26-05-15` known-gap line under §"Pinned action SHAs".
5. `SECURITY-RELEASE-VERIFY` cross-link under §"Verifying a release".

Each is < 5 lines of doc; no code changes implied.

---

## Cross-references

- `docs/audits/2026-05-security-review.md` (`S26-05-*` IDs).
- `docs/audits/2026-05-action-pinning.md` (broader S26-05-15 scope, 17 unpinned refs).
- `docs/SECURITY-RELEASE-VERIFY.md` (now cross-linked from `SECURITY.md` §"Verifying a release" and `docs/RELEASING.md` §"Step 4").
- `docs/RELEASING.md:116-167` (Step 4 verification commands).
- `backend/internal/auth/rbac.go:12-17, 92-102, 120-125` (approver role).
- `backend/internal/config/config.go:373-389, 411-433, 458-495` (Validate gates).
- `backend/internal/store/postgres/run.go:142-143` (S26-05-23 open SQL half).

---

## Closed

All six drift findings landed in `claude/w5-security-drift-bundle` (follow-up to PR #71). Documentation only — no code touched.

### 1. RBAC table omits `approver` role — MEDIUM (closed)

`SECURITY.md` §"Authorization (RBAC)" rewritten as a four-row table (admin / operator / approver / viewer). Operator row no longer credits promotion-approval rights; new approver row added; the `DefaultGroupRoleMap` example below the table expanded to include `cooker-approvers → approver` and `cooker-viewers → viewer`. Source-line cites added: `backend/internal/auth/rbac.go:12-17, 92-102, 120-125`.

### 2. Secrets adapter inventory understated — LOW (closed)

`SECURITY.md` §"Data Security" → "Secrets" bullet replaced the `(e.g., HashiCorp Vault, AWS Secrets Manager)` hint with a five-row table enumerating every adapter that ships: `database`, `keepsave`, `vault`, `aws`, `gcp`. Each row records the `COOKER_SECRETS_BACKEND` value, the package path, and the per-backend required env vars enforced by `Config.Validate()` (`backend/internal/config/config.go:411-433`).

### 3. CLAUDE.md drift on KeepSave — INFO (closed)

`CLAUDE.md` bottom-of-file note "KeepSave (P2 / PR G) is parked pending a walkthrough" replaced with an accurate status: adapter ships at HEAD (`backend/internal/secrets/keepsave/`), Helm wiring renders `COOKER_SECRETS_KEEPSAVE_{URL,PROJECT_ID,API_KEY}` (the API key via `secretKeyRef`), and `Config.Validate()` (`config.go:413-423`) gates the required env vars. Selectable with `COOKER_SECRETS_BACKEND=keepsave`.

### 4. Multi-replica Redis triad understated — INFO (closed)

`SECURITY.md` §"Rate limiting" bullet rewritten. Previously: "Multi-replica deployments must disable this." Now: explicit three-bullet callout naming `COOKER_RATE_LIMIT_BACKEND`, `COOKER_WS_TICKET_BACKEND`, `COOKER_WS_HUB_BACKEND` — each with the failure mode that flips when it isn't pointed at Redis — plus a pointer to `Config.Validate()`'s `config.go:482-499` enforcement. Sticky sessions kept as a fallback path.

### 5. `S26-05-15` action-pinning closure scope — MEDIUM (closed)

`SECURITY.md` §"Pinned action SHAs" reworded to be honest about scope. The release-workflow half (`release.yml`, cosign trust chain) is described as fully pinned. A "Known gap — non-release workflows" sub-paragraph names `ci.yml`, `cooker-weekly.yml`, and `oci-conformance.yml`, references the 17 unpinned `uses:` count, points to `docs/audits/2026-05-action-pinning.md`, and explicitly calls out `anthropics/claude-code-action@v1` in `cooker-weekly.yml` as the highest-write-permission unpinned action (`contents: write` + `pull-requests: write`) so blast-radius reasoning is visible to readers.

### 6. `docs/SECURITY-RELEASE-VERIFY.md` orphaned — MEDIUM (closed)

Two inbound links added:

- `SECURITY.md` §"Verifying a release" now points to `docs/SECURITY-RELEASE-VERIFY.md` for the security-side post-publish checklist (Rekor lookup, identity drift checks, expected workflow subjects).
- `docs/RELEASING.md` §"Step 4 — Verify the release artifacts" carries a blockquote callout to the same file, framed as the security curator's checklist counterpart to the operator commands below it.

The orphan walk now reaches the file from both the security entry point and the release how-to.
