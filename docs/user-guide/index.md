# Cooker user guide

Cooker is a self-hosted CI/CD tool. You author pipelines as a graph in the browser, Cooker builds OCI-compliant container images, pushes them to a registry, and deploys to Kubernetes (or Cloud Run, ECS, Fly, Render). The whole server is a single Go binary that also serves the React frontend on one port.

## The 60-second pitch

- **Visual pipeline editor.** Drag stages onto a canvas, connect them, hit Run. No YAML, no DSL (yet — see the [roadmap](https://github.com/cooker-ci/cooker/blob/main/docs/roadmap-2026.md)).
- **OCI native.** Builds produce OCI v1.1 manifests; pushes use the distribution-spec; the referrers API is supported for supply-chain metadata.
- **Apps, not just pipelines.** An "App" is a higher-level shortcut: point at a GitHub repo, pick a deploy target, click Deploy. Cooker synthesises a Clone -> Build -> Push -> Deploy run.
- **One binary.** API, frontend, migrations, and OIDC client all ship in one container.
- **Pluggable backends.** Builders (`docker` / `kaniko` / `buildah` / `buildkit`), pushers (`docker` / `crane`), deployers (`kubectl` / `clientgo`), secrets (`database` / `keepsave` / `vault` / `aws` / `gcp`), deploy targets (Kubernetes / Cloud Run / ECS / Fly / Render). Selected at boot via env vars.

## Where to start

| You are… | Read |
|---|---|
| The operator about to `docker compose up` for the first time | [Quickstart](getting-started/quickstart.md) |
| The operator preparing a production install | [Helm install](getting-started/helm-install.md), then [Auth & RBAC](operations/auth-and-rbac.md) |
| A developer who wants to build and deploy an App | [Your first pipeline](guides/first-pipeline.md) |
| An SRE wiring observability | [Observability](operations/observability.md) |
| Debugging | [Troubleshooting](operations/troubleshooting.md) |

## What this guide is and isn't

This is the **end-user operational** guide. It covers install, configuration, day-2 operations, and authoring pipelines.

It does NOT cover:

- **Contributor docs.** See [`docs/design.md`](../design.md) for architecture-level patterns, layering rules, and the new-feature checklist.
- **Security policy / threat model.** See [`SECURITY.md`](../../SECURITY.md). Where this guide and `SECURITY.md` overlap, `SECURITY.md` is authoritative.
- **Marketing.** See [`docs/marketing/strategy.md`](../marketing/strategy.md).

## Stability notice

Cooker is pre-1.0. Treat every minor version as potentially breaking until [`UPGRADING.md`](https://github.com/cooker-ci/cooker/blob/main/CHANGELOG.md) is stable (tracked under shipping-go 30-90d in the [roadmap](https://github.com/cooker-ci/cooker/blob/main/docs/roadmap-2026.md)). Where a feature in this guide is incomplete, it is called out inline with a note like:

> **Partial.** This works for X but does not yet do Y. Tracked in the roadmap as [item ID].

When in doubt, [the roadmap](https://github.com/cooker-ci/cooker/blob/main/docs/roadmap-2026.md) and [backlog](https://github.com/cooker-ci/cooker/blob/main/backlog.md) are the honest list of what isn't done.
