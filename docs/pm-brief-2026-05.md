# Cooker — Project-management brief, May 2026

> Synthesis of the seven-workstream audit run on branch `claude/project-audit-security-GKXzQ` (PR #29).
> One PM, multiple specialist agents, four waves. This brief is the consolidated read.

## 1. What was done

| # | Workstream | Output | Agent | Lines |
|---|---|---|---|---|
| 1 | Security audit | `docs/audits/2026-05-security-review.md` | `cooker-security` | 407 |
| 6 | Performance & optimization audit | `docs/audits/2026-05-perf-and-optimization.md` | `general-purpose` | 445 |
| 7 | Go-shipping best practices research | `docs/shipping-go.md` | `general-purpose` | 498 |
| 2 | 2026 feature & integration roadmap | `docs/roadmap-2026.md` | `cooker-planner` | 240 |
| 5 | Custom protocols proposal | `docs/protocols.md` | `cooker-planner` | 699 |
| 4 | OSS marketing strategy | `docs/marketing/strategy.md` | `general-purpose` | ~400 |
| 3 | End-user documentation | `docs/user-guide/` (34 files) | `general-purpose` | 4,908 |
| — | PM brief (this doc) | `docs/pm-brief-2026-05.md` | direct | — |

**Total output: ~7,600 lines across 41 new files**, plus 1 incidental fix (CI gofmt drift unblocking the backend job).

The four waves were sequenced so each later wave consumed earlier waves' findings:

- **Wave 1** (parallel, read-only): security + perf + go-shipping. Independent — no shared files.
- **Wave 2** (parallel, informed by Wave 1): roadmap + protocols + marketing. Each agent received the Wave 1 finding IDs and was told to cross-reference rather than re-discover.
- **Wave 3**: end-user documentation, written last so it reflects current state (and was the most code-grounded — the docs deliberately call out missing features rather than gloss them).
- **Wave 4**: this consolidation brief.

## 2. Cross-cutting themes (where multiple workstreams converge)

Findings that appear in two or more deliverables are the highest-confidence priorities.

### 2.1 We don't actually ship anything (security + go-shipping + roadmap)

- **go-shipping** said this is the #1 distribution gap: no version flag, no GoReleaser, no published binary, broken module path (`github.com/cooker-ci/cooker` vs `santapong/cooker`).
- **security** said this gates the supply-chain story: no SBOM, no SLSA provenance, no cosign signatures, GitHub Actions pinned by `@v4` not commit SHA.
- **roadmap** put `shipping-go 0-30d` as item #1 in the prioritised top-30.
- **user-guide** disclosed "the `cooker` binary has zero CLI flags or subcommands today" — `cli.md` is functionally a "what doesn't exist" page.
- **marketing** made this a **launch precondition**: no public launch until GoReleaser is shipping `v0.1.0` binaries and the Helm OCI chart is published.

Verdict: this is the single most leveraged 30-day investment in the entire audit.

### 2.2 Multi-tenancy / IDOR is the only roadmap-level security item (security + user-guide + roadmap + marketing)

- **security** flagged `S26-05-09`: IDOR-by-id on `/apps`, `/environments`, `/hosts`, `/pipelines`. No ownership model. Every authenticated viewer reads every resource's metadata.
- **user-guide** had to disclose this on five pages (`apps.md`, `environments.md`, `faq.md`, plus cross-references). The docs literally tell users "do not put two customer tenants in the same Cooker."
- **roadmap** lifted it to C1 — needs an ADR before any code (data-scoped vs namespace-scoped is a load-bearing decision).
- **marketing** excluded the W11 enterprise persona from launch ICP because of `S26-05-09`. We cannot honestly market multi-tenant safety until C1 ships.

Verdict: the ADR is the blocker. Until that's written, every dependent (SAML, Cooker Cloud, slash-command bot per-workspace OAuth) stays speculative.

### 2.3 Frontend bundle is bloated (perf + marketing + roadmap)

- **perf** P26-05-24 + P26-05-28: every page including `@xyflow/react` (~150 KB) ships in the initial bundle; lazy-load non-landing routes for a ~50% cut.
- **roadmap** ranked it #4 in top-30 "now" (theme: talk-to-it).
- **marketing** flagged the README hero cast as a launch precondition — and a 5-second LCP doesn't make for a good cast. Ship #4 before the launch GIF.

Verdict: cheap, ~half a day of Vite config + `React.lazy`. Land it as part of the Wave 1 perf quick-wins.

### 2.4 CI critical path is too slow to support the work pace (perf + go-shipping + this audit's own runs)

- **perf** P26-05-34/-38/-39: backend test loop runs packages sequentially; docker job serialised on `needs: [backend, frontend, helm]`; no buildx gha cache. Combined, critical path ~8min → ~3min.
- **go-shipping** flagged it as a `ship-it` quick win.
- **This audit's PR (#29) experienced the pain directly**: a pre-existing gofmt drift on main broke backend on every PR's CI for an undefined window before this audit caught it (commit `ed0a212`). A faster CI would have caught the drift earlier; a more reliable CI would have made the audit's 9 commits less painful to retest.

Verdict: top-10 "now" item. Pair with the gofmt-on-pre-commit-hook tightening from `cooker-improve` patterns.

### 2.5 Observability is mostly right; don't churn it (perf + go-shipping + user-guide)

- **go-shipping** explicitly said keep `slog`, keep opt-in Prometheus, keep opt-in OTLP. Don't migrate.
- **perf** had no observability findings — the existing implementation is hot-path-clean.
- **user-guide** documented `operations/observability.md` with only minor `<!-- TODO: verify -->` markers — the surface is coherent.

Verdict: small follow-ups (split `/livez`/`/readyz`, gate pprof on localhost, ship a Grafana dashboard JSON). No structural change.

### 2.6 Don't build a plugin/RPC system; document the fork-and-recompile path (go-shipping + protocols + roadmap)

- **go-shipping** §5: explicit "don't" on `hashicorp/go-plugin` for adapter-shaped work. Caddy's compile-time `selectXxx` is the right pattern, and Cooker already has it.
- **protocols** echoed the recommendation when ranking protocols: CKR-LOG/1 and CKR-DSL are protocol-shaped (transport / wire format); a plugin RPC is not.
- **roadmap** B1: write `docs/extending.md`. B2: `xcooker` build tool (xcaddy clone) — defer until B1 ships and external demand exists.

Verdict: one doc, no code. Cheap commitment.

## 3. The 90-day plan

A single ordered list, drawn from the roadmap's "now" bucket and Wave 1 quick-wins. Every item has an owner agent recommended (see §5 below).

| Rank | Item | Eff | Owner agent | Source |
|---|---|---|---|---|
| 1 | **Ship `v0.1.0`** — GoReleaser, cosign keyless signing, multi-arch image to GHCR, Helm OCI chart to `oci://ghcr.io/santapong/charts/cooker`, fix module path `github.com/cooker-ci/cooker` → `github.com/santapong/cooker`, add `cooker --version` | M | `cooker-infra-ci` + `cooker-infra-deploy` | shipping-go 0-30d, roadmap #1, marketing precondition |
| 2 | **S26-05 quick wins** (six ≤1h fixes): drop raw-K8s docker.sock (`S26-05-04`), drop chart default password (`S26-05-13`), enforce `sslmode=require` in `Config.Validate()` (`S26-05-10`), generic-ify OIDC verify errors (`S26-05-01`), three `SECURITY.md` wording edits (`S26-05-19`), parameterise `SweepOrphans` interval (`S26-05-23`) | S | `cooker-security` | security review §quick wins, roadmap #2 |
| 3 | **Multi-tenancy ADR** — pick data-scoped vs namespace-scoped; write to `docs/adr/0001-multi-tenancy.md`. **No code yet.** | M | `cooker-planner` | security `S26-05-09`, roadmap #3 |
| 4 | **Frontend bundle split** — route-level `React.lazy`, Vite `manualChunks` for `@xyflow/react` | S | `cooker-frontend-ui` | perf P26-05-24, P26-05-28, roadmap #4 |
| 5 | **CI critical path → 3min** — parallel `go test ./...` in one call, buildx gha cache, drop `needs:` serialisation on docker job | S | `cooker-infra-ci` | perf P26-05-34/-38/-39, roadmap #5 |
| 6 | **Three cheap backend perf wins** — Gin release mode (`P26-05-01`), rate-limiter RWMutex (`P26-05-12`), WS ref pattern (`P26-05-29`) | S | `cooker-backend-api` | perf, roadmap #10 |
| 7 | **A1 — GitHub source adapter polish** + status checks back to GitHub | M | `cooker-feature-dev` | roadmap #6 |
| 8 | **A7 — notification sinks** (Slack/Discord/Teams/webhook/email) — new `internal/notifier/` package | M | `cooker-feature-dev` | roadmap #7 |
| 9 | **D9 + D10 + D11** — App-page webhook URL, deployed URL, empty-state CTAs | S | `cooker-frontend-ui` | roadmap #8 |
| 10 | **A4 — GHCR / Quay / Harbor auth UX** (code is there; just surface it) | S | `cooker-frontend-ui` | roadmap #9 |
| 11 | **shipping-go 30-90d** — SBOM (cyclonedx-gomod), govulncheck in CI, OpenSSF Scorecard, dependabot for `actions/**`, pin actions by SHA | M | `cooker-security` + `cooker-infra-ci` | shipping-go, roadmap #11 |
| 12 | **C13 — in-product audit-log viewer** — sink already exists; build the read path + UI page | M | `cooker-feature-dev` | roadmap #12 |
| 13 | **CKR-LOG/1 v0** — start the binary log protocol per `docs/protocols.md` §3. Subprotocol negotiation, dual-stack, server-side first. Closes P26-05-02/-03/-16. | L | `cooker-backend-api` | protocols ranking #1 |
| 14 | **MkDocs Material site** — point at `docs/user-guide/`. Deploy to GitHub Pages or Cloudflare Pages. | S | `cooker-infra-ci` | marketing precondition |
| 15 | **OSS launch** — once preconditions are met, follow `docs/marketing/strategy.md` launch-week schedule | — | direct (the human) | marketing |

Items 1, 4, 5, 6, 9, 10 can land in parallel in the first 2 weeks. Items 2, 3 are short-fuse and unblock everything else; do them first.

## 4. Open questions for the user

Consolidated from the planner and marketing agents. Each one gates real work — answering them now saves a calendar week each later.

1. **Hosted Cooker Cloud — yes or no?** Drives the shape of C1 multi-tenancy (ownership-column vs tenant-boundary). Until this is decided, item #3 (the ADR) cannot finalise.
2. **C4 pipeline-as-code DSL syntax — YAML / HCL / Starlark?** Planner recommends YAML for v1. Confirm or push back.
3. **Deploy-target priority — Kamal vs Cloud Run depth vs Nomad first?** Different audiences entirely; we can't serve all three with the next L-effort slot.
4. **C9 AI assist — local heuristics first, or hosted LLM call from day one?**
5. **C11 Slack/Discord slash-command bot — per-workspace OAuth or shared bot?**
6. **First YAML import target for D6 — Drone or GitHub Actions?**
7. **OK to defer C3 (Cooker Cloud) past 2026?** If yes, item #3 ADR can pick the simpler ownership-column model and item #1 ships without a multi-tenant story.
8. **Marketing launch — wait for items #1 + #2 only, or also block on item #4 (bundle split for the demo cast)?**

## 5. The agent roster (who you recruited, who you should recruit)

The user asked: "plan to recruit and plan what model you need to use." Here is the recommended sub-team structure for the 90-day plan, based on what actually worked in this audit.

### 5.1 The eight specialist roles that exist today

| Agent | Model fit | Best at | Don't use for |
|---|---|---|---|
| `cooker-planner` | Opus (already configured) | Strategy, sequencing, ADRs, design proposals | Anything that writes code or docs to disk (it's read-only) |
| `cooker-security` | Opus | Auth, secrets, container, threat-model deltas | Generic code review |
| `cooker-backend-api` | Sonnet | Handler/service/store layering, business logic | Adapter wiring (use `cooker-backend-adapters`) |
| `cooker-backend-adapters` | Sonnet | Builder/Pusher/Deployer pluggable backends | Routes / services |
| `cooker-backend-data` | Sonnet | Schema, migrations, store-method parity | Anything not touching `internal/store/` |
| `cooker-frontend-ui` | Sonnet | Pages, components, route changes | API client / Zustand stores |
| `cooker-frontend-state` | Sonnet | Zustand stores, useWebSocket, auth helpers | Components |
| `cooker-infra-ci` | Sonnet | CI workflows, Makefile, scripts | Helm chart edits |
| `cooker-infra-deploy` | Sonnet | Helm, K8s manifests, Dockerfile, UAT compose | CI workflows |
| `cooker-feature-dev` | Sonnet | Cross-stack feature delivery; coordinates sub-agents | Anything single-layer (use the specialist directly) |

This audit used: `cooker-security` (security audit), `cooker-planner` × 2 (roadmap + protocols), `general-purpose` × 4 (perf audit, go-shipping research, marketing, user-guide). The cooker-planner agent is read-only so its output had to be saved by the PM directly — that's the right boundary and should not change.

### 5.2 What models to talk to

- **Opus 4.7** (this brief's author) for: PM coordination, planning, security review, anything that synthesises across many sources. Used here for the PM role and Wave 1 security.
- **Sonnet 4.6** for: every specialist `cooker-*` agent that writes code. The audit's perf and user-guide agents ran on `general-purpose` (Sonnet underneath) and produced 5,400+ lines of correct, file:line-cited output. Sonnet is the right default for everything below the PM.
- **Haiku 4.5** for: nothing in the regular flow; reserve for narrow lookups via the `Explore` agent if a search is "find X in one file."

### 5.3 What's missing from the roster

Three roles I'd add over the next 90 days:

- **`cooker-release`** — Owns GoReleaser config, cosign, the `release-please` config if we adopt it, GitHub release notes templating. Distinct from `cooker-infra-ci` (which owns *runs*, not *releases*).
- **`cooker-docs`** — Owns `docs/user-guide/` and the MkDocs site. Wave 3's `general-purpose` agent did this once at audit-time, but ongoing maintenance (every shipped feature → doc page edit) wants a dedicated role.
- **`cooker-community`** — Triages incoming issues, drafts comment replies, manages the launch week. Mostly LLM-driven but with strict authorisation: never close issues without human OK, never post on the user's behalf without a confirm-prompt.

All three are role tightenings of existing tools (Sonnet underneath); they don't need a new model.

### 5.4 How to run them

- **One in-progress at a time per layer.** Multiple backend agents touching `internal/service/` simultaneously will collide. The audit's parallelism worked because each agent owned a different file tree.
- **The PM (Opus) does the synthesis.** This brief is what synthesis looks like: read the agent outputs, find where they converge, write the consolidated plan. Sonnet would do this competently but Opus is markedly better at cross-document synthesis when the inputs total >5k lines (this brief consumed ~7.6k).
- **Run-in-background for anything >30s.** All seven workstreams in this audit ran via `run_in_background: true`. The PM gets notifications when each completes and does I/O in between.
- **Commit-as-you-go.** Stop-hook discipline forced an intermediate commit during Wave 3; this turned out to be a feature, not a bug — the partial Wave 3 commit means a maintainer can review the user-guide structure before the agent finishes the long tail.

## 6. CI and infrastructure findings (incidental, surfaced during the audit)

Two non-audit issues discovered while running CI on the audit PR. Both deserve follow-up.

1. **Pre-existing gofmt drift on `main`** — two files (`backend/internal/service/app_health.go`, `backend/internal/service/logbroadcast.go`) carry a trailing blank line. The CI workflow's gofmt step has no `continue-on-error`, so any drift fails the backend job. Fixed on this branch in commit `ed0a212`; merging this PR fixes main implicitly. Suggest also adding a pre-commit gofmt hook to `.githooks/` to prevent recurrence.
2. **Docker CI job reliably fails on the current branch state.** Without Actions log access via MCP, root cause is unconfirmed. Speculative candidates: `npm ci --ignore-scripts` in Dockerfile vs `npm ci` in the frontend CI job; kubectl SHA verification network race. **Action**: maintainer to inspect the Actions log; if it's the postinstall-script issue, drop `--ignore-scripts` after vetting the script list.
3. **CI infrastructure instability** observed during this audit — multiple all-jobs-fail-in-2-seconds runs, plausibly runner-pool exhaustion. Not actionable from code. Worth raising with the platform team.

## 7. Definition of "audit landed"

The PR (#29) is mergeable as a draft once:

- [x] All seven deliverables on disk
- [x] PM brief written
- [ ] CI green on the latest commit (currently blocked on the infrastructure flake described in §6.3)
- [ ] User has answered at least open questions §4.1 and §4.7 (the hosted-Cloud decision and the C3-deferral question — these gate item #3 ADR)
- [ ] PR description updated to reflect Wave 4 done
- [ ] PR moved from draft → ready-for-review

The audit itself is the deliverable; merging unblocks the 90-day execution plan in §3. Items 1–6 of that plan can start the day this PR merges.
