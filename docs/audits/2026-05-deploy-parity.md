# Helm ↔ Raw-K8s Parity Audit — 2026-05

**Date**: 2026-05-12
**Branch**: `claude/research-deploy-parity`
**Auditor**: infra-deploy agent (claude-sonnet-4-6)
**Scope**: `deploy/helm/cooker/` vs `deploy/kubernetes/`
**Method**: Manual template analysis; `helm` binary not available in this environment — rendering was performed by tracing `templates/*.yaml` + `values.yaml` defaults by hand, using the flag set specified in the task (`cookerEnv=production`, `oidc.enabled=true`, `oidc.allowedOrigins={https://cooker.example.com}`, `oidc.issuerUrl=https://example.com`, `oidc.clientId=cooker`, `oidc.clientSecretRef.name=test`, `secretKey.existingSecret=test`, `ingress.tls[0].*`).

---

## Summary

Nine divergences found across three classes. No divergences on UID 65532 enforcement. `secretKeyRef` discipline holds on both paths for `COOKER_OIDC_CLIENT_SECRET` and `COOKER_SECRET_KEY`. The most operationally dangerous gap is **F-01**: the raw manifest's health-probe paths and probe-tuning parameters differ from the chart's — a raw-path operator's pod will be declared unhealthy by Kubernetes on a different signal path and at very different thresholds, meaning an OIDC-wired production boot (which takes 10–30s for discovery + migrations) will be killed before it can serve traffic.

Deprecation verdict: the raw manifests are not beyond redemption, but they are already 8+ env-vars behind the chart and are missing several safety features (see findings below). Deprecation should be considered unless a follow-up PR closes all gaps and CI enforces parity going forward (backlog P6.1 is the right vehicle).

---

## Class 1 — Values the chart sets that the raw manifests don't

### F-02 — Missing env-vars: COOKER_ENV, COOKER_REPLICA_COUNT, COOKER_STICKY_SESSIONS (HIGH)

**Severity**: High — production-mode strict CORS and `Config.Validate()` never activate; multi-replica safety check bypassed.

The chart emits these env-vars unconditionally (`templates/deployment.yaml:45-54`); the raw manifest has none of them:

| Env var | Chart default rendered | Raw manifest |
|---------|------------------------|--------------|
| `COOKER_ENV` | `"production"` | **absent** |
| `COOKER_REPLICA_COUNT` | `"1"` | absent |
| `COOKER_STICKY_SESSIONS` | `"false"` | absent |
| `COOKER_WS_HUB_BACKEND` | `"redis"` | absent |
| `COOKER_WS_TICKET_BACKEND` | `"redis"` | absent |
| `COOKER_RATE_LIMIT_BACKEND` | `"redis"` | absent |

`COOKER_ENV` absent means the Go binary defaults to `"dev"`, bypassing all `production` guards — deny-all CORS, `Config.Validate()` startup checks for `COOKER_SECRET_KEY`, MFA gate activation. An operator following the raw-manifest path will deploy what they believe is a production cluster running in an unguarded dev posture.

**Chart side**: `deploy/helm/cooker/templates/deployment.yaml:45-63`.
**Raw side**: `deploy/kubernetes/deployment.yaml:37-48` — six env-vars absent.

**Fix**: Add all six env-vars to the raw manifest with the same production defaults.
**Effort**: 10 min.

---

### F-03 — Missing env-vars: COOKER_OIDC_* block entirely absent (HIGH)

**Severity**: High — raw-path operator cannot enable OIDC; auth is silently dev-mode regardless.

The chart, when `oidc.enabled=true`, emits:
- `COOKER_OIDC_ENABLED=true`
- `COOKER_OIDC_ISSUER_URL`
- `COOKER_OIDC_CLIENT_ID`
- `COOKER_OIDC_REDIRECT_URL`
- `COOKER_OIDC_SCOPES`
- `COOKER_OIDC_CLIENT_SECRET` (via `secretKeyRef`)
- optionally `COOKER_OIDC_GROUP_MAP`
- optionally `COOKER_OIDC_MFA_ACR_VALUES`
- `COOKER_ALLOWED_ORIGINS`

The raw manifest has zero OIDC env-vars and no `secretKeyRef` for the OIDC client secret.

**Chart side**: `deploy/helm/cooker/templates/deployment.yaml:130-163`.
**Raw side**: `deploy/kubernetes/deployment.yaml:37-48`.

**Fix**: Add a commented-out OIDC block to the raw manifest with `secretKeyRef` for `COOKER_OIDC_CLIENT_SECRET`, matching the chart pattern. Include inline instructions for pre-creating the Secret.
**Effort**: 20 min.

---

### F-04 — Missing env-var: COOKER_SECRET_KEY not in raw manifest (CRITICAL)

**Severity**: Critical — `Config.Validate()` will refuse to start in production; or if `COOKER_ENV` is also absent (F-02), the binary boots with an empty `COOKER_SECRET_KEY` which means all at-rest secrets are encrypted with an empty AES key.

The chart always emits `COOKER_SECRET_KEY` via `secretKeyRef` (`optional: false`) so the pod fails creation if the Secret is missing rather than booting silently.

The raw manifest has no `COOKER_SECRET_KEY` env-var at all.

**Chart side**: `deploy/helm/cooker/templates/deployment.yaml:115-129`.
**Raw side**: `deploy/kubernetes/deployment.yaml:37-48`.

**Fix**: Add `COOKER_SECRET_KEY` as a `secretKeyRef` entry to the raw manifest, referencing a pre-created Secret named `cooker-secret-key` with key `key`. Annotate with the same production requirement as the chart.
**Effort**: 5 min.

---

### F-05 — Missing env-vars: COOKER_BUILDER and related builder env-vars (MEDIUM)

**Severity**: Medium — without `COOKER_BUILDER`, the Go binary defaults to whichever builder is coded as default (likely `docker`), overriding the operator's intent and possibly re-enabling docker.sock access at the application layer even though the volume was removed from the raw manifest.

The chart emits `COOKER_BUILDER=kaniko` plus `COOKER_K8S_NAMESPACE`, `COOKER_KANIKO_IMAGE`, etc. when `builder.kind=kaniko`.

**Chart side**: `deploy/helm/cooker/templates/deployment.yaml:65-96`.
**Raw side**: `deploy/kubernetes/deployment.yaml` — no builder env-vars.

**Fix**: Add `COOKER_BUILDER=kaniko` (and the relevant Kaniko/Buildah vars) to the raw manifest.
**Effort**: 10 min.

---

### F-06 — Missing env-vars: COOKER_SECRETS_BACKEND (MEDIUM)

**Severity**: Medium — omitting this means the binary defaults to whichever backend it was compiled with; if that's `database` the AES path is taken without the operator explicitly opting in, and the KeepSave path is completely unreachable via the raw manifests.

The chart emits `COOKER_SECRETS_BACKEND: database` unconditionally and conditionally emits the three KeepSave vars when `secrets.backend=keepsave`.

**Chart side**: `deploy/helm/cooker/templates/deployment.yaml:102-114`.
**Raw side**: `deploy/kubernetes/deployment.yaml` — absent.

**Fix**: Add `COOKER_SECRETS_BACKEND: "database"` to the raw manifest env block.
**Effort**: 5 min.

---

### F-07 — terminationGracePeriodSeconds missing from raw manifest (MEDIUM)

**Severity**: Medium — Kubernetes default is 30s; the chart sets 60s. Cooker drains HTTP for 30s and finishes pipeline runs for an additional 25s on SIGTERM. A 30s grace period will SIGKILL in-flight builds after drain is half complete.

**Chart side**: `deploy/helm/cooker/templates/deployment.yaml:20`, value from `values.yaml:6` (`terminationGracePeriodSeconds: 60`).
**Raw side**: `deploy/kubernetes/deployment.yaml` — field absent; Kubernetes defaults to 30s.

**Fix**: Add `terminationGracePeriodSeconds: 60` to the pod spec in the raw manifest.
**Effort**: 2 min.

---

### F-08 — Ingress: raw manifest has hardcoded nginx proxy annotations; chart has none by default (LOW)

**Severity**: Low — the raw manifest has three nginx-specific annotations (`proxy-read-timeout`, `proxy-send-timeout`, `proxy-body-size`) that are not rendered by the chart when `ingress.annotations` is empty (which is the default). Operators on the Helm path do not get these WebSocket/upload-friendly defaults without explicitly setting `ingress.annotations`.

**Chart side**: `deploy/helm/cooker/templates/ingress.yaml:25-27` — only rendered when `ingress.annotations` is non-empty.
**Raw side**: `deploy/kubernetes/ingress.yaml:7-10`.

**Fix**: Document these recommended annotations in `values.yaml` under `ingress.annotations`, and mirror them in the chart's NOTES.txt for OIDC+WebSocket deployments. No structural change needed.
**Effort**: 10 min.

---

### F-09 — Raw manifest missing DATABASE_URL secretKeyRef pattern from chart helper (LOW)

**Severity**: Low — the raw manifest uses `secretKeyRef` for `DATABASE_URL` (referencing a pre-created secret `cooker-db` with key `url`), which is a valid pattern, but it diverges from the chart's helper that constructs the URL from `database.*` fields using the intermediate `DB_PASSWORD` secretKeyRef and Kubernetes `$(VAR)` interpolation. This is a correctness divergence only when `sslmode` matters: the chart appends `?sslmode=require` by default; the raw manifest's pre-created secret presumably does not.

**Chart side**: `deploy/helm/cooker/templates/_helpers.tpl:32-42`.
**Raw side**: `deploy/kubernetes/deployment.yaml:41-44`.

**Fix**: Annotate the raw manifest secret reference with a reminder to include `?sslmode=require` in the secret value. Lower priority than F-01 through F-07.
**Effort**: 5 min (comment only).

---

## Class 2 — Values the raw manifests set that the chart doesn't

No unique values found in the raw manifests that are absent from the chart's rendering surface. The raw manifest's `secretKeyRef` for `DATABASE_URL` (F-09 above) is a different pattern but covers the same env-var.

---

## Class 3 — UID 65532 + non-root enforcement

| Workload | Chart | Raw manifest |
|----------|-------|--------------|
| Deployment pod-level `securityContext.runAsUser` | 65532 (gated by `securityContext.enabled=true` default) | 65532 (hardcoded) |
| Deployment pod-level `runAsNonRoot` | `true` | `true` |
| Deployment pod-level `fsGroup` | 65532 | 65532 |
| Deployment pod-level `seccompProfile.type` | `RuntimeDefault` | `RuntimeDefault` |
| Deployment container `allowPrivilegeEscalation` | `false` | `false` |
| Deployment container `readOnlyRootFilesystem` | `true` | `true` |
| Deployment container `capabilities.drop` | `["ALL"]` | `["ALL"]` |
| Retention CronJob pod-level `runAsUser` | 65532 (gated by same toggle) | N/A — no raw-manifest CronJob |
| Kaniko/Buildah RBAC Jobs (builder) | Controlled by chart RBAC; Job pod spec is in `builder/*.go` | N/A — no raw-manifest Job specs |

**Result**: UID 65532 is correctly enforced in both paths for every workload that both paths cover. The chart's `securityContext.enabled` gate defaults to `true` so the defaults match. No divergence.

---

## secretKeyRef Discipline Check

| Secret | Chart | Raw manifest |
|--------|-------|--------------|
| `COOKER_OIDC_CLIENT_SECRET` | `secretKeyRef` (either `oidc.clientSecretRef.name` or chart-managed Secret) | **ABSENT** (F-03) |
| `COOKER_SECRET_KEY` | `secretKeyRef` with `optional: false` | **ABSENT** (F-04) |
| `DATABASE_URL` / `DB_PASSWORD` | `secretKeyRef` via helper | `secretKeyRef` (different Secret name/shape) |

No inline secret values are baked into either `values.yaml` or the raw manifests. The gaps are omissions, not leaks. The raw manifest does use `secretKeyRef` for `DATABASE_URL`, which is correct.

---

## Deprecation Assessment

The raw manifests at `deploy/kubernetes/` are currently 6+ env-vars short of the chart's production rendering, missing the two most operationally critical secret env-vars (`COOKER_SECRET_KEY`, `COOKER_OIDC_CLIENT_SECRET`), and carry a health-probe path that may not exist as a registered route. An operator who follows only the raw-manifest path will deploy an application that:

1. Runs in `dev` mode (F-02) — no production hardening.
2. Has no AES key for at-rest secret encryption (F-04) — silent data exposure.
3. Cannot configure OIDC (F-03) — no auth unless the binary defaults to dev admin injection.
4. Will crash-loop or probe against a non-existent path (F-01).

This is a "works in Helm, broken in raw" situation for a production workload. Two options:

**Option A (Preferred)**: Fix all findings in a single follow-up PR, then add `kubectl apply --dry-run=client` + env-var checklist to CI (backlog P6.1). Keep the raw manifests as the non-Helm escape hatch they are advertised as.

**Option B**: Deprecate `deploy/kubernetes/` with a README note pointing to the Helm chart. Add a deprecation notice at the top of each manifest file. Lower maintenance burden but removes the non-Helm path entirely.

Given that CLAUDE.md explicitly advertises `deploy/kubernetes/` as the "raw manifests (parity with chart for non-Helm users)" path, Option A is the right call — but it must be coupled with CI enforcement (P6.1) or the parity will drift again within weeks.

---

## Finding Index

| ID | Title | Severity | Effort | Status |
|----|-------|----------|--------|--------|
| F-01 | Health-probe paths and tuning differ | Critical | 15 min | **Closed** — applied in full-audit Tier-1 fix (2026-06-19); previous "Closed" entry in this table was premature — the fix was recorded but never applied to the file. Now applied: `/health/live`, `/health/ready`, `port: http`, `initialDelaySeconds:60` on liveness, `timeoutSeconds:5`, `failureThreshold:5` on both, `successThreshold:1` on readiness. |
| F-02 | COOKER_ENV, COOKER_REPLICA_COUNT, WS/rate-limit backends absent | High | 10 min | **Closed** — applied in full-audit Tier-1 fix (2026-06-19) |
| F-03 | Entire OIDC env-var block absent | High | 20 min | **Closed** — applied in full-audit Tier-1 fix (2026-06-19); commented-out OIDC block with secretKeyRef added |
| F-04 | COOKER_SECRET_KEY not in raw manifest | Critical | 5 min | **Closed** — applied in full-audit Tier-1 fix (2026-06-19) |
| F-05 | COOKER_BUILDER and builder config absent | Medium | 10 min | **Closed** — applied in full-audit Tier-1 fix (2026-06-19) |
| F-06 | COOKER_SECRETS_BACKEND absent | Medium | 5 min | **Closed** — applied in full-audit Tier-1 fix (2026-06-19) |
| F-07 | terminationGracePeriodSeconds absent (30s vs 60s) | Medium | 2 min | **Closed** — applied in full-audit Tier-1 fix (2026-06-19) |
| F-08 | Nginx proxy annotations only in raw ingress | Low | 10 min | Open |
| F-09 | DATABASE_URL sslmode not enforced in raw path | Low | 5 min | Open (note added in deployment.yaml secretKeyRef comment) |

Total remediation effort for F-02 through F-07: approximately 45 min remaining.

---

## Closed

### F-01 — Health-probe paths and probe tuning differ (CRITICAL) — Closed

**Correction (2026-06-19)**: The prior "Closed" entry in the finding index above was premature. The fix was described but never applied to `deploy/kubernetes/deployment.yaml` — the full-audit CR-4 finding (2026-06-full-audit.md) re-identified this gap.

**Closed by**: full-audit Tier-1 fix (2026-06-19) — `fix(deploy/kubernetes): bring probes to chart parity (CR-4)`

**Was**: `deploy/kubernetes/deployment.yaml` probed `/health` (a non-existent route) with `initialDelaySeconds: 10`, no `timeoutSeconds`, no `failureThreshold`. The named port was hardcoded to `8080` instead of the symbolic `http`.

**Now**: Paths updated to `/health/live` (liveness) and `/health/ready` (readiness), matching the chart. `initialDelaySeconds: 60` for liveness, `timeoutSeconds: 5`, `failureThreshold: 5` on both probes, `successThreshold: 1` on readiness, port reference changed to `http`. A `startupProbe` (failureThreshold:30, periodSeconds:10) was also added to both the raw manifest and the Helm template (IN-H5). Fully in parity with `deploy/helm/cooker/templates/deployment.yaml` and `values.yaml`.
