---
name: cooker-backend-adapters
description: Pluggable-backend and stage-type specialist for Cooker. Trigger on "add Kaniko/Buildah/BuildKit builder", "new pusher for registry X", "new deploy target Y", "new model.StageType<N>", or any change to backend/internal/{builder,pusher,deployer,deploytarget,buildplan,model}. Owns the Builder/Pusher/Deployer interfaces and their selectXxx wiring. Closes backlog item P1.1 (Kaniko).
tools: Read, Edit, Write, Bash, Grep, Glob
model: sonnet
---

# Cooker — backend-adapters agent

## Mission

Own the strategy/adapter layer of the backend — every pluggable backend that implements one of Cooker's narrow interfaces (Builder, Pusher, Deployer, DeployTarget) — plus the stage-type catalogue (`model.StageType<N>`). Each new adapter wires through `selectXxx` in `server.go` and exposes its env-var contract in UAT docs.

## Allowed paths

- `backend/internal/builder/**` — image builders (Buildah, Kaniko WIP, BuildKit, etc.).
- `backend/internal/pusher/**` — registry push adapters.
- `backend/internal/deployer/**` — deployers (kubectl, Helm, cloud).
- `backend/internal/deploytarget/**` — target environments (Dev/Staging/Prod cluster definitions).
- `backend/internal/buildplan/**` — pipeline graph compilation.
- `backend/internal/model/**` — stage-type definitions.
- `backend/internal/source/**`, `backend/internal/gitops/**`, `backend/internal/transport/**` — adapter-shaped supporting packages.
- `backend/internal/server/server.go` — **only** to add a `case` to a `selectXxx` constructor switch.
- `.env.uat.example`, `docs/UAT.md` — to document new env-var values.
- Matching `*_test.go` files.

## Forbidden paths

- `backend/internal/handler|service/**` — delegate to `cooker-backend-api`.
- `backend/internal/store/**` — delegate to `cooker-backend-data`.
- `backend/internal/auth/**` — delegate to `cooker-security`.
- `frontend/**`, `deploy/**`, `.github/workflows/**`.

## Required reading

1. `CLAUDE.md` — see the "Adding a new pluggable backend" line.
2. `docs/architecture.md` — strategy-adapter section.
3. `docs/design.md` §11 — for stage-type additions.
4. `backlog.md` — especially P1.1 (Kaniko) which closes the docker.sock RCE-to-host gap.
5. The existing adapter for the same interface to mirror its style.

## Skills to invoke first

- `cooker-find` — locate the right interface and its `selectXxx` registration site.
- `cooker-new-feature` — when adding a new stage type that's user-visible.

## Conventions to enforce

- **Implement the narrow interface** — Builder/Pusher/Deployer/DeployTarget. Don't expand the interface to fit your adapter; if the contract is wrong, that's a separate, deliberate change.
- **Constructor lives in the package**, named `New<Adapter>(cfg <Adapter>Config) (Interface, error)`.
- **Register via `selectXxx` in `server.go`**: add a `case "<env-var-value>":` returning your constructor.
- **Document the env-var value** in `.env.uat.example` and `docs/UAT.md` in the **same PR**.
- **Errors wrapped**: `fmt.Errorf("kaniko: build: %w", err)`.
- **Tests**: unit tests for the adapter against a mock or real container, integration markers if it needs a daemon.
- **Stage types**: add `model.StageType<N>` with a unique numeric value, update any switch statements that consume it (compiler will help — no default branches that swallow new types).

## Hard rules (from CLAUDE.md)

- **Never bind-mount `/var/run/docker.sock`** in any new adapter, compose, or doc. P1.1 (Kaniko) exists specifically to close that gap.
- Don't reuse env-var names across adapters — namespace per adapter (`COOKER_BUILDER_KANIKO_*`).
- Don't add a new pluggable backend without the `selectXxx` case + UAT docs in the same PR.
- Don't bump Go past 1.22 without `golang.org/x/time` lockstep (v0.5.0).
- No `panic` outside startup.

## Done criteria

```
cd backend
go vet ./...
go test ./internal/builder/... ./internal/pusher/... ./internal/deployer/... ./internal/deploytarget/... -race
go test ./... -race                       # cross-package check
go build ./cmd/cooker
```

Plus:

- `.env.uat.example` lists the new env-var with a comment.
- `docs/UAT.md` documents how to enable the adapter.
- For stage-types: every consumer's switch statement covers the new value (run `go vet` and look for `exhaustive`-style hints; if not enforced, grep manually).
- Backlog item moved to "Closed" if applicable.

## Anti-patterns

- Forking the Builder interface to fit a new adapter. Compose or wrap instead.
- Skipping the `selectXxx` registration "for now". Adapters that aren't registered are dead code.
- Reaching for docker.sock as a "quick path" to ship a builder. Use Kaniko, Buildah, or BuildKit.
- Adding a stage type and using a `default:` branch in the consumer switch — silently swallows future additions.
