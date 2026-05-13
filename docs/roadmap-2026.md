# Cooker — 2026 Roadmap

> Status: planning document. Reviewed against Wave 1 audits (`2026-05-security-review.md`,
> `2026-05-perf-and-optimization.md`, `shipping-go.md`) and the W11 persona walkthroughs.
> Author: planner pass, 2026-05.

## 1. Strategic frame

Cooker is a **visual, OCI-native, single-binary CI/CD tool** that sits between the
"Jenkins is too much" and "GitHub Actions YAML is too little" markets. Our edge
over Drone/Woodpecker is the graph editor + first-class environment/promotion
model; over Argo Workflows and Jenkins X it is the single-binary deployability
and unified Apps abstraction; over Dagger and the "pipelines-as-Go-code" class
it is that operators get a UI without writing Go. The one differentiator to
lean into in 2026 is **"deploy anywhere from one binary, via a graph you can
hand to a junior":** every roadmap item below either deepens the
deploy-target / source / registry surface, or makes the graph cheaper to author.

We are NOT competing with Argo CD on GitOps-as-source-of-truth, and NOT
competing with Tekton on Kubernetes-native primitive purity. Both are
deliberate non-goals.

## 2. 2026 themes

Every item carries one tag.

- **`ship-it`** — Distribution, releases, supply chain, install paths. Wave 1's
  shipping-go audit said this is the #1 gap.
- **`trust-it`** — Security closeouts (the 4 HIGHs from S26-05), audit
  surface, multi-tenancy, MFA depth, SAML.
- **`extend-it`** — Integrations (VCS, registries, deploy targets, secrets,
  notifications, observability sinks) and the extension model.
- **`talk-to-it`** — Frontend perf, slash-command bot, AI assist, pipeline DSL,
  template gallery — anything that lowers cost-of-authoring.
- **`run-it`** — Operational quality-of-life (bulk actions, search, retry
  policies, cost insights, time-travel).

## 3. Per-dimension roadmap

### A. Integrations

| # | What | Why (user value) | Effort | Risk | Theme | Depends on | Open question |
|---|------|-----|---|---|---|---|---|
| A1 | **GitHub webhook → trigger** (already partial via `/webhooks/github`) — flesh out branch/path filters, status checks back to GitHub. | Closes the indie-hacker "push to main, see deploy" loop end-to-end. W11 §Indie step 5. | M | Low | extend-it | none | Should we ship a GitHub App (richer perms, marketplace listing) or stick with webhooks? |
| A2 | **GitLab + Gitea source adapters** (mirror the GitHub source). | Self-hosted Gitea is the natural pair with self-hosted Cooker; GitLab unlocks the SaaS midmarket. | M | Low | extend-it | A1 patterns | — |
| A3 | **Bitbucket source adapter.** | Long-tail enterprise; lower priority than A2. | M | Low | extend-it | A1 patterns | Defer to demand-driven? |
| A4 | **Registry: GHCR, GitLab CR, Quay, Harbor support** in the pusher adapter (`internal/pusher/crane.go` already uses go-containerregistry — mostly config and auth). | Cooker pushes to "anywhere OCI" today *in code* but doesn't surface the auth UX. Multi-registry teams need it explicit. | S | Low | extend-it | none | — |
| A5 | **Registry: AWS ECR with IRSA** auth. | Largest single requested cloud registry. ECS deploy target already shipped (P9.2). | M | Med | extend-it | none | — |
| A6 | **Registry: GCR / Artifact Registry with Workload Identity.** | Pairs with the Cloud Run deploy target already shipped. | M | Med | extend-it | none | — |
| A7 | **Notification sinks: Slack, Discord, Teams, generic webhook, Email.** New `internal/notifier/` package with a `Notifier` interface, `selectNotifier`-style registration, env-var configured per env. | The single highest-asked-for feature class for tools at our maturity. | M | Low | extend-it | none | Per-pipeline vs per-environment routing — pick one shape. |
| A8 | **PagerDuty notifier** as a flavour of A7. | Enterprise SRE requirement. | S | Low | extend-it | A7 | — |
| A9 | **Vault, AWS SM, GCP SM secrets backends** — already shipped (backlog P2). Adds **Doppler + Infisical** as community-asked alternates. | Closes the indie/SaaS secrets-management long tail. | M | Low | extend-it | none | Are Doppler/Infisical worth the maintenance vs deferring to ESO? |
| A10 | **External Secrets Operator example manifests + docs.** No code; `deploy/eso/` + `docs/secrets.md`. | shipping-go §4.6 — closes the most common K8s production-secret question. | S | Low | extend-it | none | — |
| A11 | **Buildah builder** — shipped (P9.5). No new item. | — | — | — | — | — | — |
| A12 | **BuildKit-rootless adapter** (third in-cluster builder alongside Kaniko/Buildah). | Operators who already run BuildKit prefer not to add Kaniko-specific Pod patterns. | M | Med | extend-it | builder-adapter pattern is set | — |
| A13 | **depot.dev remote-build adapter** (hosted BuildKit). | Differentiator for ML/long-build users; W11 §ML step 9. | M | Low | extend-it | A12's interface shape | Commercial relationship needed; defer until ML cohort asks. |
| A14 | **Nomad deploy target.** | The most-requested non-k8s orchestrator. | L | Med | extend-it | deploytarget interface | — |
| A15 | **SSH + systemd deploy target** (`internal/deploytarget/ssh/`). | The single biggest indie/homelab unlock. Pairs with W11 §Indie persona. | L | Med | extend-it | none | Use Kamal under the hood vs hand-roll? |
| A16 | **Kamal deploy target** (sibling to A15 — wraps the Kamal CLI/library). | Rails/Hanami community uptake; cheap differentiator. | M | Low | extend-it | go-git path is set | Bundle Kamal binary or shell-out? |
| A17 | **Railway deploy target.** | Indie hacker market; gives Cooker a "deploy from graph to Railway" story. | M | Low | extend-it | flyio/render adapter pattern | — |
| A18 | **Datadog, Honeycomb, Grafana Cloud OTLP sinks** — already free via the existing OTLP exporter (P4). Doc-only follow-up. | Closes the "where does my trace go?" question for the three commonest hosted backends. | S | Low | extend-it | OTel shipped | — |
| A19 | **S3-compatible build-cache sink** for Kaniko/Buildah `--cache-to`. | W11 §ML step 4 — massive ROI on iterative builds. | M | Low | extend-it | A4–A6 auth | — |
| A20 | **OCI artifact build cache** (Kaniko `--cache-repo=oci://...`). | Same cache benefit, no extra storage backend. | S | Low | extend-it | A4 | — |
| A21 | **Issue tracker link-out: ticket-ID field on runs + clickable link** (Jira / Linear / GitHub Issues). | Cheapest compliance/audit-trail win in the doc. | S | Low | run-it | none | — |

### B. Extensions / extensibility model

| # | What | Why | Effort | Risk | Theme | Depends on | Open question |
|---|---|---|---|---|---|---|---|
| B1 | **`docs/extending.md`** documenting the fork-and-recompile path (per shipping-go §5). Defines what's API-stable vs internal across `internal/{builder,pusher,deployer,deploytarget,notifier,secrets,source}`. | shipping-go's core recommendation. Locks down the contract before we have third-party adapters that get broken. | S | Low | extend-it | none | Which interfaces are we willing to call stable? |
| B2 | **`xcooker` build tool** (xcaddy-equivalent) — Go program that codegens a `main.go` with extra `import _ "..."` lines and `go build`s a custom binary. | The "I want one Cooker with my private deploy target" story. Defer until B1 ships and we have ≥3 community-asked adapters. | L | Med | extend-it | B1 | Hold until external demand? |
| B3 | **Custom stage types as data, not code** — extend `model.StageType` so user-defined `{kind: "custom", command: "...", image: "..."}` is a first-class node in the graph editor (today there's a `custom` type but UX is thin). | The graph editor's superpower is meaningless if every stage type is hard-coded. | M | Med | talk-to-it | none | — |
| B4 | **WebHook custom-step** — a stage that POSTs to a user URL with the run context, then waits for a signed callback. The "do any thing not yet integrated" escape hatch. | Closes the "Cooker can't do X" problem definitively; operators wire X via webhook + a separate worker. | M | Med | extend-it | B3 | Signing: HMAC vs OIDC-issued JWT? |
| B5 | **"BYO Runner" protocol** (high-level only — full protocol spec is the protocols-agent's deliverable). A long-poll or WS-based agent that registers, claims work, reports back. | Unlocks self-hosted runners on networks Cooker can't reach (air-gapped clusters, customer-premise). | XL | High | extend-it | B4 patterns | Defer until end of 2026; spec first. |

### C. Interesting / differentiating features

| # | What | Why | Effort | Risk | Theme | Depends on | Open question |
|---|---|---|---|---|---|---|---|
| C1 | **Multi-tenancy v1 — ownership column** (`owner_user_id` / `owner_team_id` on Pipeline / App / Environment / Host). Closes `S26-05-09` IDOR. | The single largest "trust-it" gap. Wave 1 security review's only roadmap-shaped HIGH. | XL | High | trust-it | ADR first | **Decision: A3-defer (ADR-0004, 2026-05-13). Unblocked.** Ship `owner_team_id` now; `tenant_id` migration documented as ADR-0004 Appendix A (revisit Q4 2026). |
| C2 | **SAML auth method** alongside OIDC. | Enterprise unblocker for the dual-license tier; W11 §Enterprise. Some legacy IdPs are SAML-only. | L | Med | trust-it | none | Worth the lift if the hosted offering is the play? |
| C3 | **Cooker Cloud free tier** (hosted SaaS playground). | The single most leveraged adoption mechanism; reduces "Helm-install first" friction to zero. | XL | High | ship-it | C1 (tenant isolation) | **The roadmap's biggest open question: freemium tier yes/no?** |
| C4 | **Pipeline-as-code DSL** — supplement the graph UI with a YAML spec checked into the repo. Parse → same `model.Pipeline`. Graph stays authoritative; DSL is the export/import format. | Without it, Cooker can't compete in any "everything in git" shop. The protocols agent is deep-diving the syntax. | L | Med | talk-to-it | none | YAML (boring, universal) vs Starlark (powerful, learning curve) vs HCL (enterprise familiarity). |
| C5 | **GitOps mode** — Cooker watches a Git repo for pipeline + app YAML and reconciles. | Pairs with C4. Closes the ArgoCD-overlap question by being explicit: "Cooker reads from git, deploys from git, doesn't try to replace Argo for app-state reconciliation." | L | Med | talk-to-it | C4 | Scope to pipeline-defs only (cheap) or also apps + envs (real GitOps)? |
| C6 | **Time-travel / replay** — re-run any historical run with original artifacts pinned (image digest, env-var snapshot, pipeline-def version). | High differentiator vs Drone/Woodpecker which discard run state. | L | High | run-it | run-events log (sibling of P26-05-05 fix) | Storage cost; cap at 90 days? |
| C7 | **Cost insights** — track wall-clock + container-seconds per run / pipeline / env. Tag in OTel metrics; show in UI. | Differentiator that maps to a hosted-tier billing primitive. | M | Low | run-it | OTel metrics shipped | — |
| C8 | **Promotion policies** — declarative "deploy to prod only if staging green for 24h AND no open SEV-1". DSL extension. | Closes the SaaS-team feature gap for "real" change-management. | L | Med | run-it | C4 (DSL) | — |
| C9 | **AI assist: generate pipeline from repo URL.** Static analyse `package.json` / `go.mod` / `Dockerfile` / `pyproject.toml` and propose a graph. | The "Cooker writes your first pipeline for you" experience. Pairs with W11 §Indie step 3 (build-recipe auto-detect). | M | Med | talk-to-it | none | Local heuristics first, or hosted-LLM call from day one? |
| C10 | **Template gallery — "first pipeline in 60 seconds".** Curated starter pipelines / app templates, one-click instantiate. | Cheapest activation-rate win on the table. | S | Low | talk-to-it | none | Bundled in-binary or fetched from a /templates repo? |
| C11 | **Slack/Discord slash-command bot** ("/cooker deploy myapp to staging"). | Closes the loop on A7 (notifications) by making the inbound side conversational. | L | Med | extend-it | A7 | Per-workspace OAuth or shared bot? |
| C12 | **Visual diff between pipeline versions** — graph delta of two pipeline revisions. | Pairs with C4 (DSL → version history meaningful) and C6 (replay). Differentiator vs every text-based CI. | M | Med | talk-to-it | pipeline version history (does not exist today) | — |
| C13 | **In-product audit-log viewer.** Filter by user/route/date/app. | Already in backlog (W11 §SaaS step 6, §Enterprise step 6). Belongs on the roadmap. | M | Low | trust-it | audit middleware shipped | — |

### D. Quality-of-life

| # | What | Why | Effort | Risk | Theme | Depends on | Open question |
|---|---|---|---|---|---|---|---|
| D1 | **Bulk re-run / bulk cancel** on the runs page. | High-frequency action; cheap. | S | Low | run-it | none | — |
| D2 | **Run search + filters** (status, branch, author, duration, env). | Linear-scan of runs is painful past ~100. | M | Low | run-it | `LIMIT` on runs query (already flagged spof-and-database #12) | — |
| D3 | **Run-level approvals (manual gate) with audit-log entry.** | Subset of the existing promotion-approval flow generalised to any stage. | M | Med | trust-it | C13 | — |
| D4 | **Per-stage retry policy.** Already in `dag-performance.md` T10 — closed. No new work. | — | — | — | — | — | — |
| D5 | **Pipeline templates + parameters.** Reusable pipelines with `{{ .Param.X }}` substitution. | Cheap, addresses the SaaS-team "8 services need the same pipeline" pain. | M | Low | run-it | none | — |
| D6 | **Pipeline import from Drone / Woodpecker / GitHub Actions YAML.** | Migration on-ramp; the single highest-leverage adoption tool we don't have. | L | Med | talk-to-it | C4 (DSL has a parse path) | Start with which? Drone is closest in shape. |
| D7 | **Per-Pipeline / per-App `runDeadline` override.** | W11 §ML step 5; backlog P1. | S | Low | run-it | none | — |
| D8 | **Build-cache plumbing in the UI** (toggle, cache-repo field). Pairs with A19/A20. | W11 §ML step 4. | S | Low | run-it | A19 or A20 | — |
| D9 | **Webhook URL surfaced on AppDetailPage** + copy button. | W11 §Indie step 5. | S | Low | run-it | none | — |
| D10 | **Deployed URL surfaced on AppDetailPage** (read `DeployTarget.Status.URL`). | W11 §Indie step 6. | S | Low | run-it | none | — |
| D11 | **First-run empty-state CTAs** on Apps/Pipelines/Environments. | W11 §Indie step 2. | S | Low | talk-to-it | none | — |
| D12 | **Bulk import GitHub org → Apps.** | W11 §SaaS step 4. | M | Low | run-it | A1 | — |
| D13 | **Per-environment secret diff view.** | W11 §SaaS step 7. | M | Low | trust-it | none | — |
| D14 | **Approver pre-warning** for step-up MFA. | W11 §SaaS step 3. | S | Low | trust-it | none | — |
| D15 | **Production-readiness checklist in-product** on first boot. | W11 §Enterprise step 1. | S | Low | trust-it | none | — |

## 4. Prioritised top-30 backlog

Single ordered list. `now` = land this quarter; `next` = next quarter (Q3 2026);
`later` = 2026-H2 / 2027 candidate.

| Rank | ID | Theme | Effort | Bucket | One-line rationale |
|---|---|---|---|---|---|
| 1 | **shipping-go 0–30d** (release pipeline) | ship-it | M | **now** | shipping-go #1 finding: "we don't ship anything." Unblocks everything. |
| 2 | **S26-05 quick wins (6 items)** | trust-it | S | **now** | All ≤1h; lands on the current security-audit branch. |
| 3 | **C1 — Multi-tenancy ADR** (not the code, just the design decision) | trust-it | M | **now** | Locks down S26-05-09 trajectory before C3 / SAML choices. |
| 4 | **P26-05-24 + P26-05-28** — frontend bundle split | talk-to-it | S | **now** | ~50% initial-bundle cut. Wave 1's biggest perf win. |
| 5 | **P26-05-34, P26-05-39, P26-05-38** — CI critical-path cut to ~3min | ship-it | S | **now** | Speeds every other roadmap item. |
| 6 | **A1 polish — GitHub source adapter + status checks** | extend-it | M | **now** | Closes indie-hacker loop. |
| 7 | **A7 — notification sinks (Slack/Discord/Teams/email/webhook)** | extend-it | M | **now** | Highest-asked-for class at our maturity. |
| 8 | **D9 + D10 + D11** — App-page + empty-state UX | talk-to-it | S | **now** | One day of frontend work, persona-validated. |
| 9 | **A4 — first-class GHCR / Quay / Harbor auth UX** | extend-it | S | **now** | Code is there; just UX. |
| 10 | **P26-05-01 (gin Release mode), P26-05-12 (rate-limiter RWMutex), P26-05-29 (WS ref pattern)** | run-it | S | **now** | Three cheap backend perf wins. |
| 11 | **shipping-go 30–90d** — cosign, SBOM, govulncheck, Scorecard | trust-it | M | **now** | Required for any enterprise procurement story. |
| 12 | **C13 — in-product audit-log viewer** | trust-it | M | **now** | SOC-2 marquee feature, persona-validated, sink already exists. |
| 13 | **shipping-go — `/livez`+`/readyz` split, MkDocs site, `cooker config print`** | run-it | M | **next** | Closes operator UX gap; depends on #1. |
| 14 | **C4 — Pipeline-as-code DSL** | talk-to-it | L | **next** | Foundation for C5/C6/C8/D6. Land the parser before any feature that needs it. |
| 15 | **D2 — run search + filters + pagination** | run-it | M | **next** | spof-and-database #12 dependency. |
| 16 | **A14 — Nomad deploy target** | extend-it | L | **next** | Largest non-k8s orchestrator request. |
| 17 | **A15 — SSH + systemd deploy target** | extend-it | L | **next** | Indie/homelab unlock; W11 §Indie. |
| 18 | **C10 — template gallery** | talk-to-it | S | **next** | Activation-rate win once the install path works (post #1). |
| 19 | **A19/A20 — build-cache plumbing** (Kaniko/Buildah) | run-it | M | **next** | W11 §ML; large ROI per-user. |
| 20 | **D12 — bulk import GitHub org → Apps** | run-it | M | **next** | W11 §SaaS. |
| 21 | **D5 — pipeline templates + parameters** | run-it | M | **next** | Tied to C10; same parse path. |
| 22 | **A5 + A6 — ECR/IRSA + Artifact Registry/Workload Identity** | extend-it | M | **next** | Cloud parity; closes the "I'm on AWS/GCP" objection. |
| 23 | **P26-05-25 — Zustand store selectors sweep** | talk-to-it | M | **next** | Editor render-storm fix; substantial UX uplift. |
| 24 | **P26-05-19 — pgx/v5 stdlib swap** | run-it | S | **next** | 20-50% DB throughput on JSONB. |
| 25 | **C9 — AI-assist: generate pipeline from repo** | talk-to-it | M | **next** | Indie activation; pairs with C10. |
| 26 | **C1 — Multi-tenancy v1 implementation** | trust-it | XL | **later** | Only after the ADR (#3) decides shape. |
| 27 | **A2 + A3 — GitLab/Gitea/Bitbucket source adapters** | extend-it | M | **later** | Marginal adopters once GitHub is rock-solid. |
| 28 | **C6 — time-travel/replay** | run-it | L | **later** | Wait for D6 import → critical mass of historical runs to justify. |
| 29 | **C5 — GitOps mode** | talk-to-it | L | **later** | Real value only after C4 is landed. |
| 30 | **C3 — Cooker Cloud free tier** | ship-it | XL | **later** | Whole-business decision; gated by the open question below. |

Items off the top-30 (still tracked, not yet ranked): A8, A12, A13, A16, A17, A18,
A21, B2, B3, B4, B5, C2 (SAML), C7, C8, C11, C12, D3, D6, D13, D14.

## 5. What we deliberately deprioritise

- **GUI-built secrets manager.** Integrate (Vault / AWS SM / GCP SM / ESO); don't replace.
  KeepSave stays as one option, documented as a Cooker-team product per shipping-go §4.5.
- **`hashicorp/go-plugin` RPC plugin model.** shipping-go §5 explicit "don't"; B1's
  compile-time path is correct for adapter-shaped work.
- **WASM plugins.** Not in 2026; revisit when `wazero`'s host-bindings mature.
- **Windows binaries.** Cooker is a server-side tool; users `helm install` once
  per cluster. shipping-go §2 confirms.
- **A pipeline-graph 3D view / VR editor / any of that.** Hard no.
- **Replacing the graph editor with text-only.** C4 (DSL) is an *addition*; the graph
  stays canonical.
- **Tekton primitive parity.** Tekton wins on K8s-CRD purity; we win on
  single-binary deploy-anywhere. Don't refight the K8s-purity war.
- **ArgoCD-style app-state reconciliation.** GitOps mode (C5) reads pipeline-defs from
  git; it does not try to be the app-state controller. Document the line clearly.
- **`semantic-release`, Dagger, hand-rolled Makefile release** — shipping-go §1 explicit "don't"s.
- **SIGHUP config reload.** shipping-go §4 — restart-only is the right call.
- **Multi-tenant in OSS without a hosted business model.** Tied to the C3 open question
  below; if hosted is "no," C1 stays single-tenant + ownership-column, never goes
  to schema-per-tenant.

## 6. Open questions for the user

These need a call before the roadmap can be executed.

1. **Hosted Cooker Cloud — yes or no?** Drives whether C1 multi-tenancy targets
   "ownership column for IDOR closeout" (no-hosted) or "tenant boundary
   suitable for shared-customer DB" (hosted). Touches C2 (SAML), C3, C11,
   pricing.
2. **C4 DSL syntax — YAML / HCL / Starlark?** Trade-off paragraph: YAML is
   boring-but-universal (lowest learning cost; weakest expressiveness). HCL is
   enterprise-familiar (Terraform halo; OK expressiveness; rarer outside
   ops). Starlark is most powerful (Bazel/Buck halo; meaningful learning
   cost; the most "interesting" choice). **Recommended: YAML for C4 v1**;
   add Starlark later if C8 (promotion policies) needs real expressiveness.
3. **Deploy-target priority — Kamal vs Cloud Run depth vs Nomad first?** A14
   vs A15/A16 vs deepening A17. Different audiences entirely.
4. **C9 AI assist — local heuristics first, or hosted LLM call from day one?**
   Local-heuristics-only is shippable in a sprint; LLM call needs an API-key
   story, abuse controls, and a privacy story.
5. **C11 Slack/Discord slash-command bot — per-workspace OAuth or shared bot?**
   Shared bot is easier to build, terrifying to operate. Per-workspace is the
   right answer for any non-toy deployment.
6. **A2 / A3 prioritisation order (GitLab vs Bitbucket vs Gitea).** Demand-driven
   or platform-driven?
7. **D6 — first YAML import target.** Drone is closest-in-shape; GitHub
   Actions is most-asked-for. Pick one.
8. **Are we ok deferring C3 (Cooker Cloud) past 2026?** If yes, C1
   multi-tenancy can land as the simpler ownership-column model and we save a
   quarter.
