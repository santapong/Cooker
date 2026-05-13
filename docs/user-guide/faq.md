# FAQ

Answers to questions that don't have a natural home in the rest of the guide.

## What is Cooker?

A self-hosted CI/CD tool with a graph-based UI for visually building pipelines, building OCI images, and deploying to Kubernetes (or Cloud Run, ECS, Fly, Render). Single Go binary serves both API and frontend on port 8080.

## Why "Cooker"?

The README doesn't say; ask the maintainer.

## How is Cooker different from Drone / Woodpecker / Argo Workflows / Tekton / Dagger / Jenkins / GitHub Actions?

The [roadmap](https://github.com/santapong/cooker/blob/main/docs/roadmap-2026.md#1-strategic-frame) has the honest framing. Short version:

- vs **Drone / Woodpecker** — Cooker wins on the graph editor and first-class environment / promotion model. They win on maturity.
- vs **Argo Workflows / Jenkins X** — Cooker wins on single-binary deployability and a unified Apps abstraction. They win on K8s-CRD purity.
- vs **Dagger / pipelines-as-Go-code** — Cooker wins because operators get a UI without writing Go. They win on programmability.
- vs **Jenkins / GitHub Actions** — Cooker wins on a coherent self-hosted model. They win on plugin ecosystems and SaaS scale.

Explicit non-goals: Cooker does NOT try to be Argo CD on GitOps-as-source-of-truth, and does NOT try to match Tekton on K8s-native primitive purity. Both are deliberate.

## Is Cooker production-ready?

It depends on your bar.

Today (pre-v1.0), Cooker:

- Has end-to-end OIDC PKCE auth, RBAC, audit logging, secrets at rest.
- Builds, pushes, and deploys reliably against Kubernetes.
- Has a working live-log streaming UX.

Today, Cooker also:

- Does not yet have a tagged release stream or a published Docker image. You build from source. See [`docs/shipping-go.md`](../shipping-go.md).
- Is single-tenant. Every authenticated user sees every Pipeline / App / Environment's metadata (RBAC gates writes, not reads). Tenant scoping is roadmap `C1`.
- Has open known security findings — see [`docs/audits/2026-05-security-review.md`](../audits/2026-05-security-review.md).

If your bar is "self-host this for a small team of trusted operators": yes, go. If your bar is "deploy this as a SaaS to external customers": not yet.

## Where do I get the binary / image?

You don't yet — there's no published binary. Build from source: `make build` or `make docker-build`. Publishing is on the shipping-go roadmap.

## Can I run Cooker without Kubernetes?

Partially. You need Postgres. Beyond that:

- **No Kubernetes**: use the `docker-host` deploy target with a managed Host (Docker daemon over TCP or tailnet). The Kubernetes deploy target is the most-developed; `docker-host` is partial.
- **Cloud Run / ECS / Fly / Render**: each works as an App's deploy target without K8s. Cooker itself can run anywhere that can run the binary.

## Can I run Cooker in a single container?

Yes — `docker run -p 8080:8080 cooker:latest` works if you also provide Postgres (and Redis if you go multi-replica). The compose stack is the convenient form. See [Quickstart](getting-started/quickstart.md).

## Can I export my pipeline as code?

Not yet. Pipelines are JSON in the database. A YAML pipeline-as-code DSL is roadmap `C4`. Until then: `GET /api/v1/pipelines/:id` returns the JSON document; you can store it in git, but Cooker doesn't reconcile from git.

## Can I import a pipeline from Drone / Woodpecker / GitHub Actions?

Not yet. Roadmap `D6`. The blocker is that Cooker doesn't have a parseable pipeline format itself (see previous question).

## Can multiple teams share one Cooker?

Operationally yes (one Helm install, two teams use it). Securely no — all Pipelines / Apps / Environments are visible to every authenticated user. There's no team / tenant boundary. Roadmap `C1` (multi-tenancy ADR) gates the eventual fix.

If you need isolation today, run separate Cooker installs per team.

## How do I add a new builder / pusher / deployer / deploy target?

Implement the relevant interface in `backend/internal/<kind>/`, register it in `selectXxx` in `server.go`, and document the env var. See [`docs/design.md` §11](../design.md#11-adding-a-new-feature--checklist). This requires a code change and a rebuild; there's no plugin system.

## Does Cooker store any data outside Postgres?

In-memory: idempotency cache (bounded 32 MiB), rate-limit buckets (when backend=memory), WS tickets (when backend=memory). All transient.

Optionally in Redis: the three pieces of state above when their backends are flipped to redis.

Optionally on the filesystem: audit log file when `COOKER_AUDIT_DESTINATION=file`.

Nothing else. No `/var/lib/cooker`, no implicit caches on disk.

## Can I run Cooker on ARM?

Yes — the Dockerfile is multi-arch-friendly (Alpine base, native Go cross-compile). The chart works. ARM-native registries (`linux/arm64`) and ARM-native build targets work via Kaniko / Buildah's multi-arch flags.

## Why does Cooker need both Postgres AND Redis?

It doesn't — Redis is optional. Postgres is the source of truth for everything persistent. Redis is the multi-replica-shared-state backend for the rate limiter, WS tickets, and WS hub. Single-replica installs can leave Redis off entirely.

## How do I back up?

`pg_dump` against your `DATABASE_URL`. Also: keep `COOKER_SECRET_KEY` safe — it decrypts the database-backend secrets. Without it, the DB backup is useless. See [Self-hosting: backup strategy](guides/self-hosting-tips.md#backup-strategy-summary).

## Can I rotate `COOKER_SECRET_KEY`?

Not safely today. Rotation invalidates every previously sealed secret in the database. Plan a one-shot re-encrypt step before changing the key. The dual-key Codec is roadmap; tracked as `S26-05-08`.

## Does Cooker support SAML?

No. OIDC only (PKCE). SAML is roadmap `C2` and deprioritised — most modern IdPs support OIDC, including the ones that historically only spoke SAML.

## Does Cooker have an audit log?

Yes. Every authenticated mutating call produces a structured event. Default destination is stdout (your log shipper picks it up). See [Observability: Audit log](operations/observability.md#audit-log) and [`SECURITY.md`](../../SECURITY.md#audit-logging). There's no in-product viewer yet — operators query their SIEM. The viewer is roadmap `C13`.

## How do I integrate with Slack / Discord / Teams?

You don't, natively, yet. Workaround: a Custom stage that `curl`s your webhook. See [Notifications](guides/notifications.md). First-party notifier package is roadmap `A7`.

## Is Cooker secure?

It's been audited; findings are at [`docs/audits/2026-05-security-review.md`](../audits/2026-05-security-review.md). At time of writing: zero CRITICAL, four HIGH (operational-shape, none RCE-shaped), several MEDIUM, mostly hardening. Read the audit before deciding for your context.

## How do I report a security vulnerability?

Email `security@cooker-ci.example.com`. Do NOT open a public GitHub issue. See [`SECURITY.md`](../../SECURITY.md#reporting-a-vulnerability).

## What's the license?

MIT. See [LICENSE](https://github.com/santapong/cooker/blob/main/LICENSE).

## Who maintains Cooker?

See [`docs/design.md`](../design.md) for governance and code-organization conventions. Contributions welcome.

## Where do I get help?

- **GitHub Issues** for bugs: https://github.com/santapong/cooker/issues
- **GitHub Discussions** for questions (when enabled): https://github.com/santapong/cooker/discussions
- **Security**: `security@cooker-ci.example.com`

## Where do I find what's planned?

- **`backlog.md`** at the repo root — open work items.
- **`docs/roadmap-2026.md`** — themes, big-picture plan.
- **`docs/shipping-go.md`** — release engineering / distribution gaps.

## Anything I should know that the docs don't say?

Read the [audit series](../audits/) before anything important. The audits are blunt about gaps in a way that the rest of the docs sometimes aren't.
