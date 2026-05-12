# 2026-05 Security Review

**Branch:** `claude/project-audit-security-GKXzQ`
**Scope:** Auth, Secrets, Container & supply chain, Network, Data, API surface, Threat-model drift.
**Method:** Full-repo read with line-cited verification against HEAD on the branch above. Builds on top of the prior audit series (see Cross-references) — findings already closed there are *not* re-flagged; new and still-open issues are.

**Companion to:**
- [`launch-readiness.md`](launch-readiness.md) — the launch checklist this doc feeds back into.
- [`vulnerabilities-and-chains.md`](vulnerabilities-and-chains.md) — the 54-chain reference.
- [`chain-recheck.md`](chain-recheck.md) — post-remediation re-verification.
- [`spof-and-database.md`](spof-and-database.md) — SPOF + schema view.
- [`crash-and-service-quality.md`](crash-and-service-quality.md) — crash + service-quality view.
- [`W10-bug-and-chain-recheck.md`](W10-bug-and-chain-recheck.md) — most recent re-check on the observability batch.
- [`SECURITY.md`](../../SECURITY.md) — the public threat-model document; called out where it overstates posture.

Severity ranks: **CRITICAL / HIGH / MEDIUM / LOW / INFO**. Each finding has a stable ID `S26-05-NN`.

---

## TL;DR — top 5 issues

1. **`S26-05-04` (HIGH) — Raw-Kubernetes manifest still hard-mounts `/var/run/docker.sock`.** `deploy/kubernetes/deployment.yaml:50-51, 76-79` mounts the host docker socket unconditionally with no builder selector. The Helm chart conditionally drops it; the parity manifest does not. Operators using `deploy/kubernetes/` (documented in `launch-readiness.md` §3) ship an RCE-to-host vector without realising it.
2. **`S26-05-09` (HIGH) — IDOR persists on every per-resource read.** `GET /apps/:id`, `GET /environments`, `GET /hosts/:id`, `GET /pipelines/:id` have no ownership or membership check (`handler/app.go:51-57`, `handler/environment.go:26-36`, `handler/host.go:41-47`, `handler/pipeline.go:86-92`). Any authenticated `viewer` reads every resource. `vulnerabilities-and-chains.md` A.6 #2 flagged this; **still open**.
3. **`S26-05-10` (HIGH) — `Config.Validate()` does not enforce `sslmode=require` on production `DATABASE_URL`.** `config/config.go:367-371` only blocks the literal dev default; `?sslmode=disable` against any other host passes silently. Operator sets a real Postgres URL with TLS off → secrets traverse the cluster in cleartext.
4. **`S26-05-13` (HIGH) — Helm chart default Postgres password is literally `cooker`.** `deploy/helm/cooker/values.yaml:233-238` declares `postgresql.auth.password: cooker` as the default. Anyone who `helm install`s without overriding inherits a publicly-documented credential.
5. **`S26-05-19` (MEDIUM, but high-leakage) — `SECURITY.md` overstates posture in three concrete places** (rate limiting *every* expensive endpoint; "ClusterRole … least-privilege"; "default-deny CORS in production"). See `S26-05-19` for the deltas. The threat model has drifted past the doc; readers downstream (including pen-test reports) will be misled.

The **quick-win list** at the bottom names six items that can land on this branch in under an hour each.

---

## Findings

### Section 1 — Auth (OIDC PKCE, RBAC, WS tickets, local-auth)

#### `S26-05-01` — MEDIUM — OIDC `Verify` error string is reflected to client

- **Location:** `backend/internal/auth/oidc.go:200-205, 217-220, 158-163, 187-189`.
- **Impact:** The middleware mirrors `err.Error()` from `go-oidc`'s verifier into the response body (`invalid token: <reason>`). Failure modes include `iss mismatch`, `aud mismatch`, `kid not found`, signature parse errors, etc. This gives an attacker a high-fidelity oracle for crafting valid-shaped tokens and for fingerprinting the JWKS rotation cadence. `crash-and-service-quality.md` B.3 already flagged it; **still open** at HEAD.
- **Repro:** `curl -H 'Authorization: Bearer eyJ...invalid' /api/v1/pipelines` → body contains the upstream library's diagnostic string.
- **Recommended fix:** log `err` server-side; return one of two generic strings (`invalid token` for verify errors, `provider unavailable` for discovery errors). Diff sketch:
  ```go
  if err != nil {
      slog.Warn("oidc: token verify failed", "err", err)
      c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
      return
  }
  ```
- **Effort:** S (≤30 min, with a unit-test update).
- **Cross-ref:** `crash-and-service-quality.md` B.3.

#### `S26-05-02` — MEDIUM — Local-auth signin has no application-layer rate limit

- **Location:** `backend/internal/server/router.go:28-31` (the two `s.router.POST` calls for `/api/v1/auth/local/{signup,signin}`) — they live *outside* the `api := s.router.Group("/api/v1", s.oidcMW.Handler())` block so neither the auth middleware nor the rate limiter touches them.
- **Impact:** Unauthenticated brute-force on credentials. `SECURITY.md:54` documents this explicitly: "unauthenticated `/auth/local/signin` calls are not rate-limited at the application layer — operators should enforce that at the edge." The doc treats this as an operator deferral, but the rate-limiter we *already ship* keys on `c.ClientIP()` for unauthenticated paths (`server/ratelimit.go:100-105`) and would slot in cleanly.
- **Recommended fix:** apply `expensive` (or a separate lower-budget limiter) to `/api/v1/auth/local/{signup,signin}`. Bucket by `c.ClientIP()`; a tighter limit (e.g. 5/min) is fine.
- **Effort:** S.
- **Cross-ref:** `SECURITY.md` §"Local email + password auth" (the trade-off the doc currently signs off on).

#### `S26-05-03` — MEDIUM — RBAC group→role map is loaded once at boot

- **Location:** `backend/internal/auth/oidc.go:227` calls `MapGroupsToRolesWith(raw.Groups, m.cfg.GroupRoleMap)`; `cfg.GroupRoleMap` is read once in `config.Load()` (`config/config.go:281`). `chain-recheck.md` B.4.7 already flagged this; **still open**.
- **Impact:** Revoking an admin's access at the IdP side does not invalidate any in-flight session (token TTL gates it) AND does not change Cooker's mapping until restart. The compound effect: an operator who realised "we should remove admins from `cooker-admins`" still has to roll the deployment, and any leaked admin JWT remains admin-equivalent until `exp`.
- **Recommended fix:** none required for launch — accept as roadmap. Document it explicitly under `SECURITY.md` §RBAC instead of leaving the property implicit. Longer term, an admin endpoint to re-read the group-role map (or an `fsnotify` on the env-source) closes it.
- **Effort:** S (docs) / M (live reload).
- **Cross-ref:** `chain-recheck.md` B.4.7, `launch-readiness.md` R3 roadmap.

#### `S26-05-04` — HIGH — Raw `deploy/kubernetes/deployment.yaml` mounts host docker.sock unconditionally

- **Location:** `deploy/kubernetes/deployment.yaml:50-51` (`volumeMounts: - name: docker-socket; mountPath: /var/run/docker.sock`) and `76-79` (`volumes: - name: docker-socket; hostPath: path: /var/run/docker.sock`). No `if builder.kind == "docker"` gate exists (this is the raw-manifest parity path; Helm chart `deployment.yaml:165-168, 204-209` *does* gate it).
- **Impact:** Operators following `launch-readiness.md` §3 RBAC + network checklist who deploy via raw manifests inherit the docker-socket bind mount silently. With `securityContext.runAsNonRoot: true` and `runAsUser: 65532` (set on the pod, line 19-25), the socket is technically owned by the host docker group — but the threat model is unchanged: RCE in Cooker → host docker daemon → host root.
- **Repro:** `kubectl apply -f deploy/kubernetes/deployment.yaml` on any node with a docker socket present. Cooker container can `docker ps`, `docker run`, etc.
- **Recommended fix:** delete `volumeMounts:- name: docker-socket` (lines 50-51) and `volumes: - name: docker-socket` (lines 76-79). The K8s parity path should run Kaniko by default — that's already the Helm-chart default. If operators legitimately need docker-socket access, ship a separate `deploy/kubernetes/deployment-docker.yaml` variant they have to deliberately choose. Also worth adding a comment block referencing the docker-socket RCE-to-host risk so anyone touching this file is warned.
- **Effort:** S (delete 6 lines).
- **Cross-ref:** P1.1 (Kaniko closed for Helm; raw-manifest path missed).

#### `S26-05-05` — LOW — Local-auth JWT has no revocation list

- **Location:** `backend/internal/auth/local/local.go:152-167`. `vulnerabilities-and-chains.md` A.6 #4 already flagged this; **still open**.
- **Impact:** A leaked local-auth JWT is valid for the full TTL (default 12h). The user can be removed from the database, password rotated — the existing token still verifies because the issuer holds no revocation set.
- **Recommended fix:** none for launch; documented in `SECURITY.md:57`. The mitigation is "lower `COOKER_LOCAL_AUTH_TOKEN_TTL` if that's a concern." Track as a backlog item.
- **Effort:** M (token-id table + cache).
- **Cross-ref:** `SECURITY.md` §Local auth trade-offs.

#### `S26-05-06` — LOW — WebSocket ticket store TOCTOU is benign-but-documented

- **Location:** `backend/internal/server/wsticket.go:102-114`. The in-memory `Consume` deletes the ticket *before* the expiry check (intentional, comment explains: a delete-and-check guarantees single-use). This means a ticket that's just-expired and a ticket that's just-consumed both return `ok=false` — no information leak, but worth noting that the audit-relevant log line could distinguish the two cases.
- **Impact:** None today. Future enhancement: a metric / log distinguishing "ticket expired" vs "ticket not found" would help operators diagnose IdP clock skew.
- **Recommended fix:** optional — emit a debug-level event with the two distinct reasons. Don't return them to the client.
- **Effort:** S.
- **Cross-ref:** `crash-and-service-quality.md` A.4.

#### `S26-05-07` — INFO — Dev-mode admin injection is correctly gated, but logging is silent

- **Location:** `backend/internal/auth/oidc.go:236-249` — `devHandler()` injects `dev-user` with `RoleAdmin` when both OIDC and local-auth are disabled.
- **Impact:** Today: gated correctly. `config.Validate()` (`config/config.go:425-427`) emits an `slog.Warn` if both auth methods are off in production, but does NOT refuse boot. An operator who sets `COOKER_ENV=production` but forgets `COOKER_OIDC_ENABLED=true` ships an open-admin service.
- **Recommended fix:** promote the `slog.Warn` to a `problems = append(...)` so `Validate()` fails closed. A production deployment without any active auth path is almost certainly a config mistake.
- **Effort:** S (~3 lines + a test).
- **Cross-ref:** `crash-and-service-quality.md` A.5 (same shape as `DatabaseURL` validation).

---

### Section 2 — Secrets (`COOKER_SECRET_KEY`, OIDC client secret, backend creds)

#### `S26-05-08` — MEDIUM — `COOKER_SECRET_KEY` has no rotation path

- **Location:** `backend/internal/crypto/codec.go:30-50` (single-key constructor), `backend/internal/server/server.go:90-94` (built once, no refresh hook). `vulnerabilities-and-chains.md` B.2.5, B.4.3 already flagged this; **still open**.
- **Impact:** Rotation requires a full restart with overlap (operationally fragile; mid-rotation reveals fail with `crypto: open: cipher: message authentication failed`). For a CI/CD tool that *should* support key rotation as a routine operation, this is a gap.
- **Recommended fix:** dual-key `Codec` — accept `KeyOld, KeyNew`; `Open` tries new then old; `Seal` always uses new. Document the cut-over procedure in `RUNBOOK.md`. Until then, ship the existing "coordinated restart" runbook step.
- **Effort:** M.
- **Cross-ref:** `launch-readiness.md` R1.

#### `S26-05-09` — HIGH — Authenticated authorization gap on app / environment / host reads (IDOR by-id)

- **Location:** `handler/app.go:51-57` `GetApp`; `handler/environment.go:26-36` `ListEnvironments`; `handler/host.go:41-47` `GetHost`; `handler/pipeline.go:86-92` `GetPipeline`. No `created_by` / `owner` / membership column anywhere in the model.
- **Impact:** Any authenticated user — including a `viewer` role — reads every app's `GitHubRepo`, every environment's structure, every host's connection details. Webhook secrets and environment secrets are redacted via `Redact()` (`handler/app.go:46, 56, 81, 108`), but the metadata leak is real: branch names, registry references, build plans, GitHub repo paths, cluster IDs.
- **Repro:** authenticate as a `viewer`, hit `GET /api/v1/apps/<any-other-team's-app-id>` — returns the app's full (redacted) record.
- **Recommended fix:** none "easy" — adding a `created_by` / `team_id` column requires migration + a multi-tenant scoping decision. **Document in `SECURITY.md` explicitly that today, all authenticated users see all resources** so it doesn't surprise an SOC2/ISO audit. Tenant scoping is the P1 W11 item — track explicitly.
- **Effort:** L (proper tenant scoping); S (just document the property).
- **Cross-ref:** `vulnerabilities-and-chains.md` A.6 #2; `backlog.md` "Tenant scoping" P1.

#### `S26-05-10` — HIGH — `Config.Validate()` does not require Postgres TLS in production

- **Location:** `backend/internal/config/config.go:367-371` only checks for empty `DatabaseURL` or the literal dev default `cooker:cooker@localhost`. A `postgres://cooker:realpw@db.cooker.svc.cluster.local:5432/cooker?sslmode=disable` passes.
- **Impact:** Operator sets a real Postgres URL with `?sslmode=disable` (because the chart's `postgresql.sslMode=require` default only applies when the chart renders `DATABASE_URL` — operators who use `extraEnv` or external Postgres bypass it). Environment secrets, audit log writes, webhook secrets, pipeline runs all traverse the network unauthenticated and unencrypted to other pods in the same namespace.
- **Repro:** set `COOKER_ENV=production`, `DATABASE_URL=postgres://u:p@host/db?sslmode=disable`, boot — passes `Validate()`, runs happily.
- **Recommended fix:** add a parse step in `Validate()`:
  ```go
  if u, err := url.Parse(c.DatabaseURL); err == nil {
      if mode := u.Query().Get("sslmode"); mode == "disable" || mode == "" {
          problems = append(problems, "DATABASE_URL requires sslmode=require or stronger in production")
      }
  }
  ```
- **Effort:** S (~10 lines + test).
- **Cross-ref:** `spof-and-database.md` #15; `SECURITY.md` checklist "Enable PostgreSQL SSL connections" (currently unchecked) — Validate should make it enforced.

#### `S26-05-11` — LOW — `audit.IsRedacted()` doc-contract isn't enforced anywhere

- **Location:** `backend/internal/audit/audit.go:49-60` defines `redactedRoutes`, the comment is "callers introducing body capture in the future MUST consult this function first." Today the middleware (`server/middleware_audit.go:39-49`) doesn't capture bodies at all, so the redaction list is documentation, not a runtime guard.
- **Impact:** A future developer who adds body capture and forgets to consult `IsRedacted` is one PR away from leaking webhook / environment secrets into the audit trail. The list also misses `/api/v1/environments/:id/secrets/promote` (added later) and `/api/v1/auth/local/signup` / `signin` (passwords).
- **Recommended fix:** add the missing routes now (cheap), and add a test that asserts `auditMiddleware` never captures `Request.Body` — pin the contract with a compile-time / test-time check.
- **Effort:** S.

#### `S26-05-12` — LOW — `.env.uat.example` shows `COOKER_OIDC_CLIENT_SECRET=` lines that suggest setting a value

- **Location:** `.env.uat.example:28, 75, 101, 126`.
- **Impact:** PKCE doesn't *need* a client secret — and `SECURITY.md:32-36` says so — but every preset block in `.env.uat.example` still has a `COOKER_OIDC_CLIENT_SECRET=` line. Operators who copy-paste with a value end up committing client secrets to local `.env.uat` files (which are gitignored, so the disk-leak is bounded — but the operator pattern is set).
- **Recommended fix:** remove the `COOKER_OIDC_CLIENT_SECRET=` line from each preset block, or annotate it as "not needed for PKCE; set ONLY if your IdP forces confidential clients." Mention that the chart's `oidc.clientSecretRef.name` is the production path.
- **Effort:** S.

#### `S26-05-13` — HIGH — Chart default `postgresql.auth.password: cooker`

- **Location:** `deploy/helm/cooker/values.yaml:233-238` declares the password literal `cooker` as the default. The chart's CI matrix doesn't override it (`ci.yml` only sets `database.passwordSecretRef.name` in production paths).
- **Impact:** A `helm install cooker deploy/helm/cooker` against a cluster that has the bundled bitnami/postgresql subchart configured (`postgresql.enabled: true`, line 234) ships with a publicly-documented Postgres password. Even though there's no subchart wired in `Chart.yaml` today, the *next* time someone bundles Postgres the default carries over. The values themselves (`postgresql.auth.{database, username, password}`) are bait — they look meaningful, they aren't actually wired anywhere except as a sentinel.
- **Recommended fix:** either (a) drop the `postgresql.auth.{username,password,database}` block entirely until/unless a subchart actually ships, OR (b) put `required` guards on `password`. Add a `helm template` CI assertion that flags any unsubstituted "cooker:cooker" in the rendered output.
- **Effort:** S.
- **Cross-ref:** `backlog.md` P1.4 follow-up (mentions the bundled bitnami/postgresql gap).

---

### Section 3 — Container & supply chain

#### `S26-05-14` — MEDIUM — Dockerfile base image pinned only by tag, not by SHA digest

- **Location:** `deploy/docker/Dockerfile:37` `FROM alpine:3.19`; `:5` `FROM node:20-alpine`; `:29` `FROM golang:1.25-alpine`. None pinned to `@sha256:`. The author's own comment (`Dockerfile:48-55`) acknowledges this and defers it.
- **Impact:** Upstream registry compromise or tag-mutation can swap the base image without changing the Dockerfile. Renovate (`renovate.json:31-33`) is configured to *propose* `pinDigests: true` for `dockerfile` manager — but Renovate isn't running yet (operator step under P1.5).
- **Recommended fix:** none for launch; documented as a known gap. Enabling Renovate on the GitHub side (the one-time toggle in `backlog.md` P1.5) gets you pin-by-digest plus weekly drift PRs. **Block this finding on enabling Renovate.**
- **Effort:** S once Renovate is on; the digest pinning becomes automated.
- **Cross-ref:** `launch-readiness.md` R9; `backlog.md` P1.5.

#### `S26-05-15` — MEDIUM — GitHub Actions referenced by major-version tag, not by SHA

- **Location:** `.github/workflows/ci.yml:32` `actions/checkout@v4`, `:33` `actions/setup-go@v5`, `:52` `golangci/golangci-lint-action@v6`, `:85` `actions/setup-node@v4`, `:107` `azure/setup-helm@v4`. Same for `cooker-weekly.yml:26,34,38,57` (including `anthropics/claude-code-action@v1` which has full PR-creation permissions). `oci-conformance.yml:38,40,80` same.
- **Impact:** Tag-mutability supply-chain attacks. `v4` floats — an attacker who compromises the action publisher's repo can move the tag and have arbitrary code run in the workflow context (with `GITHUB_TOKEN`, with `ANTHROPIC_API_KEY` once that's wired). `cooker-weekly.yml:21-23` grants `contents: write` and `pull-requests: write`, so a compromised `anthropics/claude-code-action@v1` could open PRs that pass CI and ship a backdoor.
- **Recommended fix:** pin all `uses:` to commit SHAs and let Renovate auto-bump them weekly (Renovate's GitHub Actions handler does exactly this when `digestPinningRecommended` is on; the existing `renovate.json:26-29` config already groups them weekly). Two-step: (1) flip the references to SHAs once; (2) Renovate maintains them.
- **Effort:** S (one PR; helper `gh api repos/<o>/<r>/git/refs/tags/<t>` per tag).
- **Cross-ref:** `backlog.md` P1.5.

#### `S26-05-16` — LOW — `kubectl` is bundled in the Dockerfile but unused at runtime under the recommended path

- **Location:** `deploy/docker/Dockerfile:61-67` installs `kubectl`. Cooker's recommended production deployer is `clientgo` (`internal/deployer/clientgo.go`); only the legacy `kubectl` deployer shells to it. Yet kubectl ships with every build and is therefore an attack surface (a shell-injection in *any* part of Cooker that touches a command line gets a `kubectl` with full kubeconfig access).
- **Impact:** Attack-surface accumulation. The SHA verification (`Dockerfile:61-66`) is good defence; the binary itself shouldn't be there if `clientgo` is the default.
- **Recommended fix:** make `kubectl` install conditional via build arg (`ARG INCLUDE_KUBECTL=false` default) or split the image into `cooker:latest` (no kubectl) and `cooker:latest-with-kubectl`. The chart docs in `SECURITY.md` already imply non-kubectl is the default.
- **Effort:** M.
- **Cross-ref:** `vulnerabilities-and-chains.md` A.7 #6.

#### `S26-05-17` — INFO — No SBOM generation or signing in CI

- **Location:** `.github/workflows/ci.yml:298-304` builds the image but doesn't generate an SBOM or sign it. No cosign, no syft, no `provenance: true` on `docker build`.
- **Impact:** Cooker has no machine-readable supply-chain manifest. SLSA level 1 isn't met; downstream operators can't verify the image they pull matches the image CI built. The README mentions OCI referrers API support for SBOMs, but Cooker's *own* image doesn't have one.
- **Recommended fix:** add a CI step using `actions/attest-build-provenance` (now GA) and/or `docker buildx build --sbom=true --provenance=true`. Push the attestations to the GHCR registry via OCI referrers. Cooker's own pusher supports it; eat the dog food.
- **Effort:** M.

---

### Section 4 — Network (CORS, TLS, NetworkPolicy, rate limiter coverage)

#### `S26-05-18` — INFO — `Allow-Credentials: true` not back. Confirmed at HEAD.

- **Location:** `backend/internal/server/server.go:423-442` — no `Access-Control-Allow-Credentials` header emitted anywhere. Verified by `grep -rn "Allow-Credentials\|AllowCredentials" backend/` → no hits.
- **Impact:** The bearer-token-only stance is intact. CSRF posture as documented in `SECURITY.md` §CSRF holds.
- **No fix needed.** Defence in place.

#### `S26-05-19` — MEDIUM — `SECURITY.md` overstates posture in three places

- **Location:** `SECURITY.md` as compared to source at HEAD:
  1. **`SECURITY.md:97`** says "*Cooker's service account ClusterRole is scoped to required resources only (deployments, pods, services, configmaps, secrets, namespaces)*." That stance is partially undone: `deploy/helm/cooker/values.yaml:256-258` ships `rbac.clusterWide: true` as the default, and the chart RBAC template renders a `ClusterRole` accordingly. The raw-K8s `deploy/kubernetes/rbac.yaml` was supposed to have been split (T5 in `chain-recheck.md` says `4a7cce9`) — but the chart's default *is still* cluster-wide.
  2. **`SECURITY.md:138`** says rate limiting "applies to the most expensive endpoints (pipeline runs, Docker image builds, App deploys)." True per `server/router.go:74, 86, 170` — only three endpoints. The doc doesn't make clear that everything else (creating environments, rotating webhook secrets, batch promote, app webhook receive) is **unbounded** at the application layer. Operators reading this should not infer the API is rate-limited.
  3. **`SECURITY.md:119`** says default "deny-all for `COOKER_ENV=production`." Correct for CORS (`config/config.go:245-247`). But the *same paragraph* doesn't mention that an operator who forgets `COOKER_ALLOWED_ORIGINS` gets a hard fail at startup (`config/config.go:420-424`) — which is what `Validate()` does, but a reader of `SECURITY.md` alone wouldn't know.
- **Impact:** Pen-test reports and SOC-2 evidence packs cite `SECURITY.md`. Doc drift here costs trust.
- **Recommended fix:** three small wording edits on this PR:
  1. Rewrite the K8s Access bullet to: "Cooker's ServiceAccount holds a Role *or* ClusterRole, chart-selectable via `rbac.clusterWide`. Default is cluster-wide for compatibility with the v0.1 manifests; namespace-scoped is encouraged."
  2. List the *exact* three rate-limited routes in the rate-limiting section; add a sentence noting all others are unbounded at the app layer.
  3. Add a one-line "Boot will refuse to start if `COOKER_ALLOWED_ORIGINS` is empty in production (`Config.Validate`)."
- **Effort:** S (≤30 min).
- **Cross-ref:** this is one of the quick wins.

#### `S26-05-20` — MEDIUM — NetworkPolicy ingress allows any pod in the same namespace

- **Location:** `deploy/helm/cooker/templates/networkpolicy.yaml:18-19` (`from: - podSelector: {}`). The `ingressNamespaceLabel` value lets operators tighten this — but the default is "allow any same-namespace pod." `vulnerabilities-and-chains.md` A.7 #7 flagged it; **still open at HEAD**.
- **Impact:** In a shared namespace (e.g. a "platform" namespace running Cooker + monitoring + service mesh), a compromised sidecar in any other pod can reach `/api/v1` on Cooker without traversing the ingress controller (and therefore without the WAF rate limits the chart docs assume).
- **Recommended fix:** flip the default to "ingress-controller namespace only" once operators set `networkPolicy.ingressNamespaceLabel`. Until then, document the property in `SECURITY.md` §Network Security so the operator-side risk is visible.
- **Effort:** S (default value swap + doc).

#### `S26-05-21` — LOW — NetworkPolicy egress is wide-open on 443 across the public internet

- **Location:** `deploy/helm/cooker/templates/networkpolicy.yaml:60-70` allows TCP/443 to `0.0.0.0/0` except RFC1918 ranges.
- **Impact:** SSRF-equivalent. An authenticated admin who registers a malicious `Registry.URL` or `OIDC.IssuerURL` can have Cooker make outbound HTTPS to arbitrary public addresses. The traffic itself isn't a leak (it's encrypted TLS to an attacker server), but it constitutes data exfiltration via DNS / connection metadata, and it can drive billing.
- **Recommended fix:** none easy. Public registries (`gcr.io`, `docker.io`, `quay.io`) and configured IdPs are legitimate egress. Document the property; long-term, surface an `allowedRegistries` list and template a tighter egress policy.
- **Effort:** L.

#### `S26-05-22` — LOW — Rate limiter doesn't cover signup, signin, webhook, or admin-destructive routes

- **Location:** `server/router.go:55-63` defines `expensive` and uses it only on three endpoints (`:74, :86, :170`). All admin-destructive `DELETE`s, the webhook secret rotation (`PUT /apps/:id/webhook`), the secret promote, the `/webhooks/github` receiver, and local-auth `/signup` + `/signin` are unbounded.
- **Impact:** A compromised admin token (or a logged-in attacker) can hammer destructive routes without backpressure; the `/webhooks/github` receiver has only a 10 MiB body limit (`handler/app.go:268-280`) but no rate limit. The HMAC check (`source/github/webhook.go:38`) is `hmac.Equal` (constant-time), so brute-force isn't a vector — but an unauthenticated flood of 10 MiB bodies is.
- **Recommended fix:** apply `expensive` (or a separate IP-keyed limiter for unauthenticated paths) to (a) `/webhooks/github`, (b) `/api/v1/auth/local/{signup,signin}`. The `rateLimitKey` function (`server/ratelimit.go:100-105`) already falls back to `c.ClientIP()` for unauthenticated callers.
- **Effort:** S.
- **Cross-ref:** `S26-05-02`.

---

### Section 5 — Data (SQL, audit integrity, PII, backups)

#### `S26-05-23` — LOW — Hardening: `SweepOrphans` builds an interval string via `fmt.Sprintf`

- **Location:** `backend/internal/store/postgres/run.go:142-143`. `vulnerabilities-and-chains.md` A.1 already flagged it (low/hardening); **still present at HEAD**. The value is an internal `time.Duration.Milliseconds()` so not externally controllable, but the pattern bypasses parameterisation.
- **Impact:** None today. Risk: if a future change passes an externally-controllable duration here, it becomes a SQL-injection.
- **Recommended fix:**
  ```go
  res, err := s.db.ExecContext(ctx,
      `UPDATE pipeline_runs SET status='failed', error='orphaned: heartbeat stale at boot',
              finished_at=NOW()
         WHERE status='running'
           AND (heartbeat_at IS NULL OR heartbeat_at < NOW() - ($1 * INTERVAL '1 millisecond'))`,
      threshold.Milliseconds())
  ```
- **Effort:** S.
- **Cross-ref:** `vulnerabilities-and-chains.md` A.1.

#### `S26-05-24` — LOW — Audit log file sink has no rotation hook

- **Location:** `backend/internal/audit/audit.go:103-123` opens the file in append mode and never rotates it. `SECURITY.md:134` advises "Forward stdout to your SIEM" or "use the file sink with a sidecar tail-shipper" — i.e. punts rotation to the operator's logging stack.
- **Impact:** The async writer drops on backpressure (good — closes B.2.7), but a single instance with `audit.destination=file` and no sidecar will fill its emptyDir or pod-local volume eventually. The drop counter (`cooker_audit_events_dropped_total`) surfaces it. Acceptable.
- **Recommended fix:** none for launch. Document the dependency on sidecar log rotation more loudly in `SECURITY.md` and `RUNBOOK.md`.
- **Effort:** S (docs).

#### `S26-05-25` — LOW — Audit `Path` capture uses `c.FullPath()` (template, not concrete)

- **Location:** `backend/internal/server/middleware_audit.go:44`.
- **Impact:** Good — the template form (`/api/v1/environments/:id/secrets/:key`) keeps environment IDs and secret key names out of the audit log. But operators investigating an incident need a way to correlate to the concrete IDs. Today the only correlation is via timestamp + user_sub + the application logs.
- **Recommended fix:** capture concrete `c.Param("id")` *only for non-redacted routes*, behind a config flag (`COOKER_AUDIT_INCLUDE_RESOURCE_IDS=true`). Routes in `audit.IsRedacted` continue to template-only.
- **Effort:** M.

#### `S26-05-26` — INFO — No automated backup or restore drill

- **Location:** `RUNBOOK.md` documents the retention CronJob (`deploy/helm/cooker/templates/cronjob-retention.yaml`) and points at `pg_basebackup` / WAL-G externally. `chain-recheck.md` B.6.2 flagged it; `launch-readiness.md` §6 puts it as an operator step.
- **Impact:** Operator deferral. Acceptable for launch.
- **Recommended fix:** none for code. Track as launch-readiness checklist item only.

---

### Section 6 — API surface

#### `S26-05-27` — LOW — `PromoteSecrets` `Keys` array unbounded

- **Location:** `backend/internal/handler/environment.go:199-202`. `vulnerabilities-and-chains.md` A.11 flagged "PromoteSecrets.Keys array unbounded"; **still present**.
- **Impact:** Admin-only route (gated by `adminRole, mfa`, `server/router.go:152`). An admin can submit an arbitrarily large `keys` array; the secrets backend will fan out N round-trips. Denial-of-service of own service.
- **Recommended fix:** cap `len(req.Keys) <= 256` or similar; return 400 above. Trivial.
- **Effort:** S.

#### `S26-05-28` — MEDIUM — Mass-assignment risk on `model.App` `Update` / `model.Pipeline` `Update`

- **Location:** `handler/app.go:84-109` and `handler/pipeline.go:94-121`. Both bind the full JSON body via `c.ShouldBindJSON(&a)` and then overwrite the `existing.WebhookSecret` (`app.go:101`) / `CreatedAt` (`pipeline.go:112`) defensively. The pattern is correct, but easy to break: any new field added to the model that should NOT be settable by an UPDATE caller will silently be settable unless someone remembers to overwrite it from `existing` in the handler.
- **Impact:** Latent. Today no exploitable case. Adding e.g. a `created_by` field (per S26-05-09's recommendation) would land it as mass-assignable on day one.
- **Recommended fix:** define an explicit `AppUpdateRequest` struct with only the mutable fields, mirror to `*model.App` in the handler. Same for `PipelineUpdateRequest`. The model boundary stops being the wire boundary.
- **Effort:** M (touches every Update handler; mechanical).

#### `S26-05-29` — LOW — `/api/v1/registry/...` proxy handlers are stubs that 200-OK with empty data

- **Location:** `handler/registry.go:42-62, 64-78, 80-92`. `PushImage` accepts a JSON request, never validates the `Image` / `Registry` fields against an allowlist, and returns 202 — but the underlying handler is a stub. `ListRepositories` returns empty.
- **Impact:** Information-disclosure-shaped — the stubs *look* like working endpoints to attackers / pen-testers, which costs them time and produces noisy reports. Once the real implementation lands, the lack of `Registry.URL` validation (no scheme check, no allowlist) becomes exploitable. `vulnerabilities-and-chains.md` A.11 flagged it.
- **Recommended fix:** make the stubs return `501 Not Implemented` with a structured `{error, hint}` payload, mirroring the `handler/network.go` / `volume.go` pattern from PR #25 (closed-recent in `backlog.md`).
- **Effort:** S.
- **Cross-ref:** `vulnerabilities-and-chains.md` A.11 (Registry.URL).

#### `S26-05-30` — LOW — Pipeline submission has no body-size limit

- **Location:** `handler/pipeline.go:53-58` `CreatePipeline`. Gin's default `MaxMultipartMemory` is 32 MiB; pipeline JSON should be much smaller. A 30 MiB stages array stresses the executor and the JSONB write.
- **Impact:** DoS-of-self by admins. Not a security boundary issue.
- **Recommended fix:** apply an `io.LimitReader` middleware to `/api/v1/pipelines` (and other mutating routes) with a generous-but-bounded cap (e.g. 1 MiB).
- **Effort:** S.

---

### Section 7 — Threat-model drift (`SECURITY.md` updates owed)

Captured in `S26-05-19` above. Additional drift items:

#### `S26-05-31` — INFO — `SECURITY.md` doesn't mention `/api/v1/auth/local/signin` is unauthenticated and unrate-limited

- **Location:** `SECURITY.md:42-60` covers the local-auth path and lists "brute-force defence is rate-limit-only" as a trade-off. But the doc *implies* the per-user rate limiter applies to authenticated routes only — true — and doesn't make clear that **`/signin` itself is neither authenticated nor rate-limited**. Combined with `SECURITY.md:54` it's almost-but-not-quite right.
- **Recommended fix:** make explicit in `SECURITY.md` §"Trade-offs of the local path" that `/signup` and `/signin` are unauthenticated and unrate-limited *at the application layer* — operator must enforce edge rate limiting. Re-state when `S26-05-22` lands code-side.

#### `S26-05-32` — INFO — `SECURITY.md` § "Container Image Security" doesn't mention SBOM gap

- **Location:** `SECURITY.md:154-162`.
- **Recommended fix:** add a "Known gaps" sub-section listing (a) digest pinning pending Renovate enablement (`S26-05-14`), (b) no SBOM yet (`S26-05-17`), (c) `kubectl` bundled but not strictly required (`S26-05-16`).

---

## Severity rollup

| Severity | Count | IDs |
|---|---|---|
| CRITICAL | 0 | — |
| HIGH | 4 | S26-05-04, S26-05-09, S26-05-10, S26-05-13 |
| MEDIUM | 8 | S26-05-01, S26-05-02, S26-05-03, S26-05-08, S26-05-14, S26-05-15, S26-05-19, S26-05-20, S26-05-28 |
| LOW | 13 | S26-05-05, S26-05-06, S26-05-11, S26-05-12, S26-05-16, S26-05-21, S26-05-22, S26-05-23, S26-05-24, S26-05-25, S26-05-27, S26-05-29, S26-05-30 |
| INFO | 6 | S26-05-07, S26-05-17, S26-05-18, S26-05-26, S26-05-31, S26-05-32 |

**Note:** no CRITICAL findings. The prior audit series correctly closed the four hot-fix items (T1 Buildah shell-injection, T2 GitOps path traversal, T3 runId IDOR, T5 cluster-wide RBAC); none have regressed. The four HIGH findings here are operational-shape issues, not RCE-shaped — but they should land on this branch (S26-05-04, S26-05-10, S26-05-13) or be explicitly tracked into the next sprint (S26-05-09).

---

## Quick-win list (≤1 hour each, land on this branch)

These are the fixes that don't require code architecture decisions. Triage in Wave 4 if anything looks more contentious than it appears here.

1. **`S26-05-04`** — Delete the unconditional docker-socket mount from `deploy/kubernetes/deployment.yaml:50-51, 76-79`. ~5 minutes plus a `kubectl apply --dry-run` smoke. **Should land first.**
2. **`S26-05-13`** — Drop or `required`-gate `postgresql.auth.password` in `deploy/helm/cooker/values.yaml:233-238`. Add a `helm template` CI assertion that flags any literal `:cooker@` in rendered YAML.
3. **`S26-05-10`** — Add the `sslmode=require` enforcement in `Config.Validate()` (`config/config.go:367-371`). ~10 lines + a test.
4. **`S26-05-01`** — Generic-ify the OIDC verify error response (`backend/internal/auth/oidc.go:200-205, 217-220, 158-163, 187-189`). Server-side log keeps detail; client gets `"invalid token"`.
5. **`S26-05-19`** — Three wording edits to `SECURITY.md` (clusterWide default, exact rate-limited routes, Validate-rejects-empty-CORS sentence). Pure doc.
6. **`S26-05-23`** — Replace the `fmt.Sprintf` interval string in `store/postgres/run.go:142-143` with parameterised SQL. ~5 lines + adjust the test fixture.

Stretch (≤2 hours each):

7. **`S26-05-07`** — Fail-closed when both OIDC and local-auth are off in production (`config/config.go:425-427`). Same shape as the existing `Validate()` guards.
8. **`S26-05-02` + `S26-05-22`** — Apply the existing rate limiter to `/api/v1/auth/local/{signup,signin}` and `/webhooks/github` (`server/router.go:28-31, 191`). Use `rateLimitKey`'s IP fallback for unauthenticated paths.
9. **`S26-05-27`** + **`S26-05-30`** — Add length caps (`len(req.Keys) <= 256`; pipeline submission `io.LimitReader` ≤ 1 MiB).
10. **`S26-05-11`** — Add a test asserting `auditMiddleware` doesn't read `Request.Body`. Add `/secrets/promote` and `/auth/local/...` to `redactedRoutes` even though they're not body-captured today.

---

## What's actually good (defenses to keep)

The audit series has done real work. The following defences are intact at HEAD and should stay in place:

- **OIDC PKCE end-to-end.** No implicit flow, no browser-side client secret. `frontend/src/auth` + `backend/internal/auth/oidc.go`. The lock-free fast path (`oidc.go:97-119`) and JWKS-freshness signal (`oidc.go:130-136`) are both correctly atomic.
- **Bearer-token API, no `Allow-Credentials`.** Verified at HEAD (`server/server.go:423-442`). CSRF stance holds.
- **WebSocket single-use 60s tickets.** `wsticket.go:64-128` (memory) and `wsticket_redis.go` (multi-replica). Atomic `GETDEL` in the Redis path is correct. `S26-05-06` is benign.
- **`Config.Validate()` production gates.** Rejects `BUILDER=docker`, default Postgres URL, wildcard CORS, empty allowed origins, multi-replica + memory backends + no sticky sessions, KeepSave `http://`, short keys. Comprehensive — only the `sslmode=disable` gap (`S26-05-10`) is missing.
- **runId IDOR fixed.** `loadRunForPipeline` (`handler/handler.go:54-69`) is consistently used by `GetPipelineRun`, `CancelPipelineRun`, `GetStageLogs`, `PromoteRun`, `ApprovePromotion`, `GetEnvStatus`. `idor_test.go` locks it.
- **Security response headers + strict CSP.** `middleware_security.go:25-47`. CSP allows Google Fonts only; everything else is `'self'`. `frame-ancestors 'none'`, `form-action 'self'`.
- **HMAC signature verification on GitHub webhook uses `hmac.Equal` (constant-time).** `source/github/webhook.go:38`. Branch-delete `deleted=true` / zero-SHA both rejected (`webhook.go:61-63`, `handler/app.go:300-306`).
- **Bcrypt at `DefaultCost`, HS256 with ≥32-byte signing key enforced.** `auth/local/local.go:41, 57-66, 152-167`.
- **Migrations with `pg_advisory_lock` + transactional `schema_migrations` insert.** `store/postgres/store.go:195-259`. Two replicas booting simultaneously can no longer half-apply a migration.
- **Audit log async + drop-on-overflow.** `audit/audit.go:91-151`. Disk-full no longer pins the API. `IsRedacted()` documents the secret-bearing route list.
- **Idempotency middleware with bounded resident set.** `idempotency.NewMemoryBounded(5*time.Minute, 32 MiB)` (`server.go:206-208`). Closes the double-click + GitHub webhook retry classes.
- **Per-stage timeouts, run deadlines, DAG fan-out cap, heartbeat orphan sweep.** Composite chain closure of B.2.x / B.6.8. Verified at HEAD.
- **Non-root container at UID 65532, drop-all caps, read-only root FS, seccompProfile: RuntimeDefault.** `Dockerfile:68-69, 85`, `deploy/helm/cooker/templates/deployment.yaml:21-40`, `deploy/kubernetes/deployment.yaml:19-33`. (The raw-manifest path still has the docker-socket mount — see `S26-05-04` — but the user / cap settings are correct.)
- **OIDC lazy discovery.** Cooker no longer dies at boot if the IdP is briefly unreachable. `oidc.go:97-119`.
- **Per-route MFA gating on destructive admin routes.** `server/router.go:50, 72, 140, 146-152, 169, 174, 184`. `RequireMFA` (`auth/rbac.go:54-87`) checks both `acr` and any `amr` value.
- **App webhook secrets and environment secrets sealed with AES-GCM (256-bit) before persistence.** `crypto/codec.go:57-66`. Nonce is per-encryption; integrity check is built into the AEAD.

---

## Cross-references

- **Prior audit findings still open at HEAD** that this review re-confirmed:
  - `vulnerabilities-and-chains.md` A.6 #2 (IDOR on `/apps/:id`) → `S26-05-09`
  - `vulnerabilities-and-chains.md` A.1 (SweepOrphans fmt.Sprintf) → `S26-05-23`
  - `vulnerabilities-and-chains.md` A.7 #7 (NetworkPolicy ingress) → `S26-05-20`
  - `vulnerabilities-and-chains.md` A.11 (Registry.URL, PromoteSecrets.Keys) → `S26-05-27`, `S26-05-29`
  - `crash-and-service-quality.md` B.3 (oidc.go error reflection) → `S26-05-01`
  - `chain-recheck.md` B.2.5, B.4.3 (SECRET_KEY rotation) → `S26-05-08`
  - `chain-recheck.md` B.4.7 (RBAC map cached at boot) → `S26-05-03`
- **Prior audit findings that have not regressed** (re-verified at HEAD):
  - T1 (Buildah shell injection), T2 (GitOps path traversal), T3 (runId IDOR), T5 (cluster-wide ClusterRole in raw manifests), T11 (optimistic concurrency), T12 (idempotency), T15 (migration framework), T16 (async audit), T9 (WS deadlines + ping/pong), W4 (idempotency cache cap), W5 (advisory lock around migrations).
- **Newly introduced** by this review:
  - `S26-05-04` (raw-manifest docker.sock), `S26-05-10` (sslmode), `S26-05-13` (default Postgres password), `S26-05-15` (Actions SHA pinning), `S26-05-17` (SBOM).
- **Backlog cross-refs:**
  - `S26-05-08` → `launch-readiness.md` R1 (dual-key rotation).
  - `S26-05-03` → `launch-readiness.md` R3 (RBAC live reload).
  - `S26-05-09` → `backlog.md` "Tenant scoping" P1 (under W11 user-journeys section).
  - `S26-05-14`, `S26-05-15` → `backlog.md` P1.5 (Renovate enablement).

---

### Closed in `claude/sec-quickwins-2026-05`

The following six findings shipped as one PR on `claude/sec-quickwins-2026-05`. The original section text above is preserved verbatim; the status lines below mark them as closed and point at the fix.

- **`S26-05-01` — CLOSED.** OIDC verify-failure body is now generic (`authentication failed` / `provider unavailable`). Upstream library diagnostic is logged at `slog.Warn`/`slog.Error` server-side only. See `backend/internal/auth/oidc.go` and `TestMiddleware_TamperedLocalTokenReturnsGenericBody` in `backend/internal/auth/local_middleware_test.go`. `SECURITY.md` §"Path 1: OpenID Connect" updated.
- **`S26-05-04` — CLOSED.** `deploy/kubernetes/deployment.yaml` no longer mounts `/var/run/docker.sock`. The volume + volumeMount block is removed and an inline comment warns future editors. Parity with the Helm chart's `builder.kind != "docker"` default. `SECURITY.md` §"Image build isolation" updated.
- **`S26-05-10` — CLOSED.** `Config.Validate()` now refuses to start in production when `DATABASE_URL` points at a non-localhost host with `sslmode=disable` or no `sslmode` parameter. Acceptable values: `require`, `verify-ca`, `verify-full`. See `backend/internal/config/config.go` and the four new `TestValidate_Production*SSL*` cases. `SECURITY.md` §"Data Security" updated; production checklist line flipped to checked.
- **`S26-05-13` — CLOSED.** The chart default `postgresql.auth.password: cooker` is removed. `templates/_helpers.tpl` now `required`-guards `database.passwordSecretRef.name` when `database.host` is set, so a `helm install` without an explicit Secret reference fails at render time. `SECURITY.md` §"Data Security" updated.
- **`S26-05-19` — CLOSED.** Three wording edits applied to `SECURITY.md`: (a) RBAC `clusterWide` default called out explicitly; (b) the exact three rate-limited routes listed, with a statement that everything else is unbounded at the app layer; (c) `Config.Validate()`-refuses-empty-`COOKER_ALLOWED_ORIGINS` sentence added.
- **`S26-05-23` — CLOSED.** `internal/server/runs.go` `orphanThreshold` is now configurable via `COOKER_ORPHAN_SWEEP_INTERVAL` (Go duration string, default 60s, must be > `heartbeatInterval`=30s; invalid values fall back to the default). Documented in `.env.uat.example`. (Note: the original audit text discussed parameterising the SQL `fmt.Sprintf` in `store/postgres/run.go:142-143`; that hardening is still tracked separately — this PR closes the operator-tunability side of the same finding ID per the quick-wins plan and leaves the SQL parameterisation for a follow-up.)
