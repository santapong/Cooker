# ADR-0001 — Strategy-pattern interfaces for builder / pusher / deployer / secrets / deploytarget
Date: 2026-05-02
Status: accepted

## Context

Cooker has to interoperate with several runtimes that don't share an SDK shape:
- Image builders: Docker daemon (today), BuildKit (tomorrow), Kaniko (production target, P1.1).
- Image pushers: Docker daemon, `crane` (`go-containerregistry`).
- Cluster deployers: `kubectl apply`, `client-go` dynamic client.
- Deploy targets: Kubernetes, Cloud Run, ECS/Fargate, Fly.io, Render.
- Secrets stores: in-process AES-GCM + Postgres, KeepSave, Vault (P2 follow-up), cloud-native SMs.

We want operators to swap any of these via configuration, contributors to add a new adapter without touching the handler layer, and tests to exercise everything against an in-process fake.

## Decision

For each domain define a small, verb-oriented Go interface with the operations Cooker actually performs. Adapters live in subpackages and register or are selected by a `select<Domain>(kind string)` function in `internal/server/server.go`. Configuration uses a single `COOKER_<DOMAIN>_BACKEND` environment variable per domain.

Concrete interfaces:

| Domain | Interface | File |
|---|---|---|
| Image build | `builder.Builder` | `backend/internal/builder/builder.go` |
| Image push | `pusher.Pusher` | `backend/internal/pusher/pusher.go` |
| Cluster deploy | `deployer.Deployer` | `backend/internal/deployer/deployer.go` |
| Secrets | `secrets.Manager` | `backend/internal/secrets/manager.go` |
| Deploy target | `deploytarget.Target` | `backend/internal/deploytarget/target.go` |

Unknown values fall back to a `Noop` implementation with a log line so booting never fails on an env-var typo. Production startup validation (`Config.Validate`) catches misconfiguration that *would* leave secrets unencrypted, not misconfiguration of the no-op runtime adapters.

## Consequences

+ New adapters are independent units of work — no handler changes required.
+ Tests against fakes are trivial; the in-memory store + fake managers cover the contract.
+ Operators can mix backends (e.g. `COOKER_PUSHER=crane` while keeping `COOKER_BUILDER=docker`).

− Five interfaces means five places to look when debugging "why doesn't my build use the new flag?" Mitigation: handlers always go through the manager/builder/pusher field, never the codec or Docker SDK directly.
− Adapters that share infrastructure (e.g. Kaniko + client-go both want a Kubernetes client) duplicate the construction. Acceptable for now; revisit if it becomes painful.
