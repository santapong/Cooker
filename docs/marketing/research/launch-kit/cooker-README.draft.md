<!-- DRAFT README rewrite — for review. Promote to /README.md after the license decision (§C of the
launch-readiness tracker) and once the hero cast + demo URL exist. Placeholders are marked {{...}}. -->

<p align="center">
  <img src="docs/assets/cooker-wordmark.svg" alt="cooker" height="64"><br>
  <strong>CI/CD you can see.</strong>
</p>

<p align="center">
  <!-- {{BADGES: build · release · license · OpenSSF Scorecard · Artifact Hub — add once published}} -->
  <a href="#license"><img src="https://img.shields.io/badge/license-{{MIT_or_Apache--2.0}}-blue" alt="license"></a>
  <a href="https://github.com/santapong/Cooker/releases"><img src="https://img.shields.io/badge/release-v0.1.0-informational" alt="release"></a>
  <img src="https://img.shields.io/badge/single-binary-Go-00ADD8" alt="single Go binary">
</p>

> **Cooker is an open-source, self-hosted CI/CD tool with a drag-drop graph editor for building OCI images (Kaniko, BuildKit, Buildah) and deploying to Kubernetes, ECS, Cloud Run, Fly.io, and Render — single Go binary, {{MIT/Apache-2.0}}-licensed, no SaaS, no agents.**
>
> <sub>{{CANONICAL SENTENCE — pending CMO/maintainer sign-off; keep identical to `llms.txt` and `what-is-cooker.md`}}</sub>

<p align="center">
  <!-- {{HERO CAST — embed the 60s asciinema/gif here; this is the single highest-leverage asset (strategy.md §2). Demo URL goes ABOVE the install commands.}} -->
  <a href="{{LIVE_DEMO_URL}}"><img src="docs/assets/hero-cast.gif" alt="Cooker: drag Build → Test → Push → Deploy, draw edges, click Run"></a>
</p>

---

## Why Cooker

Every other self-hosted CI is **YAML-first** (Woodpecker, Drone, Concourse) or has no real pipeline
editor at all (Coolify, Dokploy, CapRover). Cooker inverts that: **the graph you draw is the
authoritative data model** — not a viewer bolted onto YAML.

- 🎨 **Visual DAG editor** — drag Build / Test / Push / Deploy nodes, draw edges, click **Run**. Watch logs stream live.
- 📦 **OCI-native builds** — Kaniko by default (no `docker.sock`), or BuildKit / Buildah. Pushes to any OCI registry.
- 🚀 **Deploy anywhere** — Kubernetes, AWS ECS/Fargate, Google Cloud Run, Fly.io, Render, or SSH-Docker. One pipeline, many targets.
- 🪜 **Promotion + approval gates** — first-class Dev → Staging → Prod with RBAC-gated approvals.
- 🔑 **OIDC + RBAC + MFA** built in (no plugin), with four roles (admin / operator / approver / viewer).
- 🔐 **Pluggable secrets** — Postgres (AES-GCM), Vault, AWS SM, GCP SM, KeepSave — same API.
- 📟 **One Go binary** — serves the API + React UI on `:8080`. Runs on a tiny VPS, k3s, or your laptop.

## Quickstart — first green run in under a minute

> 🔗 **Live demo:** {{LIVE_DEMO_URL}} (read-only)

```bash
git clone https://github.com/santapong/Cooker && cd Cooker
docker compose up
# open http://localhost:8080 → drag Build → Push → Deploy → click Run
```

That's it — no agent to install, no YAML to write. See the [60-second walkthrough]({{DOCS}}/getting-started/).

## How Cooker compares

Honest, balanced comparisons (we list where the other tool wins, too):

- [Cooker vs GitHub Actions]({{DOCS}}/compare/cooker-vs-github-actions/) — self-hosted, with a first-class deploy story
- [Cooker vs Coolify]({{DOCS}}/compare/cooker-vs-coolify/) — Coolify deploys; Cooker also **builds your images and runs your CI**
- [Cooker vs Drone]({{DOCS}}/compare/cooker-vs-drone/) · [vs Woodpecker]({{DOCS}}/compare/cooker-vs-woodpecker/) — graph-first vs YAML-first
- [Cooker vs Argo Workflows]({{DOCS}}/compare/cooker-vs-argo-workflows/) — single binary vs CRD plumbing (they coexist)

## What's not done yet

This is a young project with a bus factor of one. Being honest up front (so you can decide):

- **Single-tenant.** Every authenticated user can see every pipeline / app / environment. Multi-tenancy is on the roadmap; we don't pretend it ships today.
- **No in-product audit-log viewer** yet (the audit middleware writes structured JSON; query it from your log stack).
- **No PR-preview environments** as a first-class feature.
- **Builder choice matters:** the `docker` builder mounts the host socket and is **dev-only**; Kaniko / BuildKit / Buildah are the production options.
- **OIDC, not SAML.** **Linux/macOS server** (Docker Desktop works on Mac); not Windows-native.

If any of these is a blocker for you, wait. If not, `docker compose up` takes 30 seconds.

## Install

| Mode | How | When |
|---|---|---|
| **Dev** | `docker compose up` | try it / local dev |
| **Single binary** | download from [Releases](https://github.com/santapong/Cooker/releases) | a VPS / UAT |
| **Production** | Helm OCI chart: `helm install cooker oci://ghcr.io/santapong/charts/cooker` | k3s / Kubernetes |

Full guides: [Install]({{DOCS}}/guides/INSTALL/) · [UAT]({{DOCS}}/guides/UAT/) · [Production rollout]({{DOCS}}/guides/ROLLOUT/).

## Documentation

[Quickstart]({{DOCS}}/getting-started/) · [What is Cooker]({{DOCS}}/what-is-cooker/) · [Architecture]({{DOCS}}/reference/architecture/) · [API]({{DOCS}}/reference/api/) · [Security]({{REPO}}/blob/main/SECURITY.md)

## Community

- 💬 {{DISCORD_INVITE — open at day 30 per the launch plan}}
- 🐛 [Issues]({{REPO}}/issues) — "good first issue" labels are active; contributors welcome.

## Contributing

PRs welcome — see [CONTRIBUTING.md]({{REPO}}/blob/main/CONTRIBUTING.md). {{A CLA bot must be live before
the first external PR merges — see launch-readiness tracker G4.}}

## License

{{MIT or Apache-2.0 — maintainer decision pending. The team recommends Apache-2.0 (patent grant; matches
Woodpecker / Argo / Tekton). Must be consistent across this README, LICENSE, and the binary `--version`.}}

---

<sub>No astroturfing, no inflated numbers, no "enterprise-ready" claims while single-tenant. If a sentence
sounds like a marketing department wrote it, rewrite it. (Brand rules: `docs/marketing/strategy.md` §7.)</sub>
