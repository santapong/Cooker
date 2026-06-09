# W11 — User-journey persona walkthroughs

Outside-in audit. The team has been shipping features layer-by-layer (handler → service → store → frontend) but hasn't recently checked the experience end-to-end. This doc walks the product as four concrete personas, identifies where each one hits friction, and lists the gaps as backlog candidates.

Format borrowed from `docs/audits/launch-readiness.md`'s pre-UAT checklist. Each persona has:

- **Sketch** — one paragraph, who they are.
- **Goal** — one sentence, what they want to do.
- **Walkthrough** — step-by-step table: action → what they see → what works → what doesn't.
- **Gaps** — bullet list, each tagged with a P-tier guess (P0/P1/P2/P3) and a one-line description that becomes a backlog candidate.

Personas confirmed at planning time (this doc):

1. **Solo developer / indie hacker** — single side-project, single-node k3s, zero-ops.
2. **SaaS platform team** — 50-person company, OIDC, multi-env promotion.
3. **Enterprise SRE / Platform engineer** — 500-person, multi-cluster, compliance-first.
4. **AI / ML engineer** — long builds, GPU node pools, large repos.

---

## Persona 1 — Solo developer / indie hacker

### Sketch

Building a Next.js side-project after their day job. Has a single VPS running k3s. Doesn't want to learn pipelines, doesn't want to write a Dockerfile if a buildpack works, doesn't have a "platform team" — they're the platform team. Pays $0–$10/month for everything.

### Goal

Push to `main` on GitHub → live at `https://app.example.com` within five minutes, without reading a wiki.

### Walkthrough

| Step | What they do | What works | What doesn't |
|---|---|---|---|
| 1 | Helm-install Cooker into k3s | Chart is documented in `deploy/helm/cooker/` ✅ | Default `cookerEnv: production` requires real secrets / OIDC. The "I just want to try it" path is `cookerEnv=dev` but the chart's README doesn't shout that — they hit `Validate()` rejection on first install. |
| 2 | Sign in (dev mode auto-injects admin) | Dev-admin is convenient ✅ | Sign-in page is themed but doesn't tell a fresh installer what to do next. Indie hacker lands on `/` (likely `Apps`) with an empty list and no CTA other than "+ New App". |
| 3 | Click "+ New App" | `NewAppWizard` exists; 4 steps (name → repo → build recipe → environment) ✅ | Step 3 (build recipe) lists Go / Node / Worker but doesn't auto-detect from `package.json` / `go.mod` / `Dockerfile`. Indie hacker has to guess. |
| 4 | Pick deploy target | — | The wizard assumes a deploy target already exists. Indie hacker has to `cancel → go to Hosts → create a host → come back` to wire k3s. The "use the bundled k3s" easy-button doesn't exist. |
| 5 | Set up GitHub webhook for auto-deploy | `app.AutoDeploy` toggle ✅; webhook secret rotation flow shipped ✅ | The path from "I toggled AutoDeploy" → "what URL do I paste into GitHub's webhook config?" requires reading `docs/architecture.md` § §App webhook. The wizard / detail page should show the webhook URL right next to the toggle. |
| 6 | Push commit → first deploy | Webhook → run kicks off ✅ | RunPage live-log streaming is great (PR #26 shipped this) ✅. But the indie hacker still has to dig for "what's my app's URL?" — the `Status.URL` from the deploy target is in the run details, not surfaced on AppDetailPage. |
| 7 | Want a PR-preview env | — | **Doesn't exist.** Cooker has Pipelines + Apps + Environments but no first-class concept of a per-PR ephemeral environment with a unique URL. |

### Gaps (with P-tier guesses)

- **[P2]** First-run empty-state CTAs on Apps / Pipelines / Environments. A new install should narrate "Create your first deploy target → import your first app → see your first deploy."
- **[P2]** Build-recipe auto-detect on the New App wizard. Read `package.json`, `go.mod`, `pyproject.toml`, `Dockerfile` from the repo and pre-select Step 3.
- **[P2]** Webhook URL surfaced on `AppDetailPage` next to the AutoDeploy toggle, with a copy-button.
- **[P2]** Surface the deployed `URL` on `AppDetailPage` (read it from `DeployTarget.Status.URL` after a successful deploy). Currently buried in run details.
- **[P3]** "Use the bundled k3s" easy-button on the New App wizard / Hosts page. Cooker's chart already runs in k3s; offer it as an in-cluster deploy target without operator setup.
- **[P3]** PR-preview environments. Per-PR ephemeral environments with unique subdomains. Ties together app webhook → environment lifecycle → ingress provisioning. Substantial — multi-week feature.

---

## Persona 2 — SaaS platform team

### Sketch

Platform engineer at a 50-person SaaS. Stack: 8 Go services in one GitHub org, 3 environments (Dev / Staging / Prod), Okta for SSO. Compliance is "SOC 2 Lite" (auditable approval gates, immutable history) but not full enterprise. Hates manual click-ops.

### Goal

Stand up Cooker for the team, import all 8 repos as apps, wire OIDC against Okta, get every PR through the Dev → Staging → Prod gate with one Prod approval click per release.

### Walkthrough

| Step | What they do | What works | What doesn't |
|---|---|---|---|
| 1 | Wire OIDC (Okta) via Helm chart | `oidc.{enabled,issuerUrl,clientId,clientSecretRef}` chart values ✅; group→role mapping configurable via `oidc.groupRoleMap` ✅ | The mapping is a flat CSV. Platform team wants to say "platform-admins → admin, eng-leads → operator, eng → viewer". One-line CSV is fine for that — but no Helm-side validation on the role names; typos are silent. |
| 2 | Seed environments (Dev / Staging / Prod) | EnvironmentsPage ✅; environment.Targets list ✅ | No bulk import / template; each env created click-by-click. For 3 envs that's fine. For 30 (per-team subaccounts) it isn't. |
| 3 | Wire approval policy on Prod | Promotion + approval flow shipped ✅; `auth.RequireMFA` step-up on admin destructive routes ✅ | Approver doesn't get pre-warned that step-up MFA will be required when they click "approve". The 403 → re-auth flow happens but feels unexpected the first time. |
| 4 | Import 8 GitHub repos as Apps | NewAppWizard works one-by-one ✅ | **No bulk import.** Platform team types each one. For 8 it's tolerable; for 25 it's painful. No "import all repos from this GitHub org" tool. |
| 5 | Wire CI/CD policy: auto-Dev on push, manual Staging, manual Prod | App-level AutoDeploy ✅; per-environment promotion policy via Pipelines ✅ | The relationship between Apps and Pipelines is documented in `docs/design.md` but unintuitive on first use. Platform team typically thinks "this repo deploys to these environments" — Cooker thinks "this app uses this pipeline which has these env-statuses." Conceptual translation cost. |
| 6 | Compliance: who approved what, when? | `internal/audit/` audit log middleware ✅; structured slog → stdout/file ✅ | **No in-product audit-log viewer.** Operators have to grep stdout / file. Most SOC 2 controls expect an interactive query view. *(Since shipped — roadmap M5: db sink + `/admin/audit` viewer.)* |
| 7 | Compare Staging vs Prod secrets | Per-environment secret put/reveal/delete ✅; `KeepSave` promotion endpoint ✅ | **No diff view.** Operator sees "set of keys in Staging" + "set of keys in Prod" but no side-by-side that shows which keys differ in value or are missing on one side. |
| 8 | Rotate a webhook secret across all 8 apps | Per-app rotation endpoint ✅ | **No bulk rotation.** Platform team rotates each app individually. |

### Gaps

- **[P1]** In-product audit-log viewer. Filter by user / route / date / app. Reads from the existing audit sink. SOC-2-shaped feature; high marginal ROI vs. the existing slog-to-stdout setup. *(Since shipped — roadmap M5.)*
- **[P2]** Bulk import: "import all repos from this GitHub org as Apps". Wizard that lists the org's repos and lets the operator multi-select.
- **[P2]** Per-environment secret diff view (`Staging vs Prod`). Same UX shape as `git diff` for env-vars.
- **[P2]** Approver pre-warning: when a user lands on a promotion that requires step-up MFA, show the badge before they click. Reduces 403 surprise.
- **[P3]** Bulk webhook-secret rotation across N selected apps.
- **[P3]** Helm `groupRoleMap` schema validation (refuse boot with typo'd role names).
- **[P3]** Reduce conceptual cost of Apps-vs-Pipelines for "I just want a per-repo pipeline" cases. Could be a `make-pipeline-for-app` button on AppDetailPage.

---

## Persona 3 — Enterprise SRE / Platform engineer

### Sketch

SRE at a 500-person enterprise. Compliance is the table-stakes constraint: SOC 2 Type II, ISO 27001, internal SOC 1. Stack runs on multi-cluster EKS (`eks-staging`, `eks-prod-east`, `eks-prod-west`). OIDC via Entra (Azure AD). HashiCorp Vault for secrets. Per-team K8s namespaces (`auth-team`, `billing-team`, `search-team`, `ml-team`). Wants a **fenced-off** Cooker instance per business unit, or one Cooker with hard tenant boundaries.

### Goal

Deploy Cooker into the SRE platform cluster. Configure NetworkPolicy + MFA + Vault. Wire 4 teams as separate "tenants" with no cross-team visibility. Pass an internal audit.

### Walkthrough

| Step | What they do | What works | What doesn't |
|---|---|---|---|
| 1 | Helm install with `cookerEnv=production`, replicaCount=3, Redis backends, NetworkPolicy on, ingress TLS | Chart enforces these defaults ✅; `Validate()` refuses production boot with `BUILDER=docker` ✅; multi-replica + memory backends without sticky sessions also refused ✅ | First-time installer doesn't get a "production-readiness checklist" — `launch-readiness.md` exists but isn't surfaced. |
| 2 | Wire HashiCorp Vault secrets backend | `internal/secrets/vault` shipped ✅; `COOKER_SECRETS_BACKEND=vault` ✅ | No in-product test page that says "your Vault config is reachable" — operators run a manual `kubectl exec` curl to verify. *(Since shipped — roadmap M5: Settings → Secrets test.)* |
| 3 | Wire OIDC against Entra with MFA enforcement | `auth.RequireMFA` middleware ✅; configurable ACR values ✅ | Entra and ADFS flow with `acr_values` works fine. SAML-only IdPs (some legacy enterprise) aren't supported. Out of scope, but worth a doc note. |
| 4 | Configure 4 teams with tenant boundaries | — | **Cooker is single-org.** All 4 teams' pipelines / apps / environments live in one shared list. The `groupRoleMap` is a flat CSV: cannot say "auth-admin: admin in `auth-team` namespace, viewer elsewhere." |
| 5 | Multi-cluster deploy targets | Multi-cluster works at the deploy-target level ✅ | UI doesn't surface "which cluster did this deploy land on?" prominently. RunPage shows the run; clicking through to find the target cluster takes 2-3 clicks. |
| 6 | Audit-log retention / immutability | Audit log → stdout / file → cluster log shipper ✅ | **No append-only / immutable storage backend.** SOC 2 expects a write-once log; today Cooker delegates to whatever the cluster logging stack provides. Operator has to add a sidecar. |
| 7 | Pen-test report comes back: "no rate limit on `/health/ready`" | — | `/health/ready` is intentionally unauthenticated (probe endpoint) and not rate-limited. Pen-tester flags it as an enumeration vector. Disputed; document the threat model decision. |
| 8 | Internal audit asks for "who has admin role today" | — | **No /admin/users dashboard.** OIDC users are ephemeral (no row in the user store unless local-auth is on). Audit answer is "go ask Entra". Workable but awkward. |

### Gaps

- **[P1]** **Tenant scoping.** Either (a) data-scoped: every Pipeline / App / Environment has an `owner_team_id` and store-layer queries filter by it; (b) namespace-scoped: a "Cooker namespace" that wraps a slice of resources visible to a subset of OIDC groups. Substantial — multi-week feature; design doc first.
- **[P2]** Production-readiness checklist surfaced in-product on first boot (read from `launch-readiness.md` or hardcoded).
- **[P2]** Per-team RBAC. Extend `groupRoleMap` to allow scoped grants like `auth-admin: admin in tenant=auth-team`.
- **[P2]** "Test secrets backend connectivity" page (`/settings/secrets/test`) that calls Vault / KeepSave / AWS / GCP and shows green / red. *(Since shipped — roadmap M5.)*
- **[P2]** Surface "deployed to cluster X (namespace Y)" prominently on AppDetailPage and on the Run page header.
- **[P2]** Append-only audit-log adapter (eg. AWS CloudWatch with a "no-delete" policy, or a write-once S3 backend). Out of scope for the existing stdout adapter — a new `audit.Sink` impl.
- **[P3]** SAML auth method (out of scope for the OIDC-only design today; would require a separate adapter and `auth.methods` extension).
- **[P3]** `/me/admins` dashboard listing every user with admin role today (sourced from OIDC groups + the `groupRoleMap`).
- **[P3]** Document the `/health/ready` rate-limiting decision in `SECURITY.md` to short-circuit pen-test reports.

---

## Persona 4 — AI / ML engineer

### Sketch

Trains models on a single GitHub repo (~5 GiB including model weights). Builds Docker images with PyTorch / CUDA / cuDNN. Deploys inference services to a GPU node pool (`nvidia.com/gpu: 1`). Builds take 45–60 minutes because the wheel install dominates. Cares deeply about build-cache reuse and not retransferring 5 GiB on every run.

### Goal

Configure an App that builds a CUDA image from a 5 GiB repo, caches the wheels across builds, deploys to the GPU node pool, and finishes within 30 min on the second-and-later runs.

### Walkthrough

| Step | What they do | What works | What doesn't |
|---|---|---|---|
| 1 | Configure App with custom Dockerfile | Buildplan supports `dockerfile` ✅ | — |
| 2 | Set buildArgs (`CUDA_VERSION=12.4`) | `BuildPlan.Args` supports it ✅ | — |
| 3 | Wire build context to a fat PVC | `builder.kaniko.contextPVC` chart value ✅ | Context staging from GitHub clone → PVC isn't documented end-to-end. ML engineer has to read the Kaniko adapter source. |
| 4 | Hope for build cache reuse across runs | — | **No `--cache-from`/`--cache-to` registry-cache surface in the UI / config.** Kaniko supports `--cache=true --cache-dir=...` and `--cache-repo=...` but Cooker doesn't expose them. ML engineer must shoehorn them into `BuildPlan.Args` (which doesn't pass through to the Kaniko Job spec by default). |
| 5 | First run: 47 min (cold) | The new per-stage live-log feed (PR #26) makes the wait bearable ✅ | Run deadline default is 30 min (`runDeadline`); at 30 min the run is force-cancelled. ML engineer has to set `COOKER_RUN_DEADLINE=2h` cluster-wide. **No per-pipeline / per-App override.** |
| 6 | GPU scheduling | — | **Kaniko Job spec doesn't expose `nodeSelector` / tolerations.** ML engineer can't pin the build to a CPU pool (away from precious GPU nodes); can't pin the deployed workload to GPU nodes via a Cooker setting. The Helm chart values for `builder.kaniko.{namespace, serviceAccount, contextPVC}` are great as far as they go; tolerations / selectors are missing. |
| 7 | Deploy to GPU pool | k8s deployer ✅ | DeployTarget model has `Namespace` and `HostID` but no `NodeSelector` / `Tolerations`. ML engineer ends up post-editing the deployed manifest with `kubectl edit`. |
| 8 | DVC / HuggingFace model fetch | — | **No first-class stage type for ML data fetch.** Engineer has to use a `custom` stage that shells out to `dvc pull` or `huggingface-cli download`. Logs work via PR #26 but there's no native progress / artifact tracking. |
| 9 | Second run | — | Wheel install repeats from scratch because of W10-9 (no `--cache-from` plumbing). |

### Gaps

- **[P1]** Per-App / per-Pipeline `runDeadline` override. Cluster default of 30 min is too tight for ML; promote to a per-resource setting.
- **[P1]** Kaniko / Buildah Job `nodeSelector` + `tolerations` exposure. Add `builder.kaniko.nodeSelector` / `tolerations` chart values; thread through to the Job spec.
- **[P1]** Build-cache plumbing. Surface `--cache-from` / `--cache-to` (Kaniko: `--cache-repo`) as a Pipeline / App config. Massive ROI for ML and any iterative-build workload.
- **[P2]** `DeployTarget.NodeSelector` + `DeployTarget.Tolerations` model fields. Write through to the Kubernetes deployer's Deployment spec.
- **[P3]** First-class ML stage type (`StageTypeMLPull`?) with `dvc` / `huggingface` provider plugins. Speculative; gate on real demand.
- **[P3]** Document the GitHub clone → PVC staging path end-to-end in `docs/architecture.md` so users don't have to read Kaniko source.

---

## Cross-persona patterns

Three themes recur across the four personas:

1. **Empty-state / onboarding.** Indie hacker, SaaS team, and enterprise SRE all hit a fresh-instance paper-cut on first install. None of them gets a guided "here's the order to set things up" experience.
2. **Bulk operations.** SaaS team, enterprise SRE, and ML engineer all hit "I have N of these and Cooker only lets me do them one at a time" — repos to import, environments to seed, secrets to rotate.
3. **Compliance / audit surface.** SaaS team and enterprise SRE both want an in-product audit-log viewer; today both grep stdout. The audit log middleware is in place; the viewer isn't.

These three themes alone would generate ~6 of the P1/P2 backlog candidates above. They're high-leverage because each fix benefits multiple personas at once.

---

## Verdict

**Total gaps surfaced:** 31 (4 P1, 13 P2, 14 P3).

**Highest-leverage candidates** (touch ≥ 2 personas, P1 or P2):

- In-product audit-log viewer (SaaS + Enterprise).
- First-run / empty-state onboarding (Indie + SaaS + Enterprise).
- Tenant scoping (Enterprise) — design-doc gate before implementation.
- Per-Pipeline `runDeadline` override (ML, but also applies to large monorepo Go builds).
- Build-cache plumbing (ML, but also benefits any iterative-build workload).

These five are the natural inputs for the **next 4–6 weeks of weekly PRs**. They're now appended to `backlog.md` under the new "Discovered via user-journey W11" section.

**Out of scope deliberately:** SAML auth (P3), PR-preview environments (P3 multi-week design effort), first-class ML stage types (P3 speculative).

---

## Cross-references

- **Adjacent doc:** `docs/audits/W10-bug-and-chain-recheck.md` — the bug + chain audit that ran in the same audit week.
- **Format precedent:** `docs/audits/launch-readiness.md` (per-persona-style narrative) and `docs/audits/chain-recheck.md` (verdict tables).
- **Backlog updates:** see `backlog.md` § "Discovered via user-journey W11" for the gaps mapped to P-tiers.
