# Cooker — File Structure Pattern

Derived from a read-only whole-repo map (loop-engine fan-out, 10 subsystem readers → synthesis,
run `wf_9af9e64a-dd3`). This is the **target standard** for where code lives and when a file must
split. The repo is already exceptionally consistent, so most of this is *descriptive* of an
existing pattern; the deviations at the bottom are the gaps.

## Universal rules (apply everywhere)

1. **Strict backend layering:** handler → service → store/strategy. Handlers parse/validate HTTP
   only; business logic in `internal/service`; adapters implement narrow interfaces. No business
   logic in handlers, no HTTP types in services, no `panic` outside startup.
2. **One file per domain/entity/concern** is the universal split axis. Cohesion via file-name
   prefixes (`app_*`, `pipeline*`, `run_*`, `stage*`, `webhook_*`), not deep nesting.
3. **Interface-first + pluggable backends** wherever persistence or strategy is involved: define an
   interface, ship an in-memory/noop impl for dev+tests and a real impl (postgres/redis/cloud) for
   prod; select via a `selectXxx` switch in `server.go` or a config-gated registry.
4. **Errors** wrap package/step prefix with `%w` (`fmt.Errorf("oidc: discover: %w", err)`); packages
   expose typed sentinels (`store.ErrNotFound/ErrConflict`, adapter `ErrUnavailable`) matched via
   `errors.Is` at the boundary.
5. **Colocated white-box tests:** every non-trivial `*.go` ships a `<source>_test.go` in the same
   package; large units split into concern-scoped `*_test.go`. Race detector on in CI; `go vet` first.
   Test doubles inline and role-named (`fakeX`/`mockX`/`stubX`); inject via narrow local interfaces +
   `WithXxx`/`NewWithXxx`.
6. **Size ceiling ~400 lines/Go file, ~300–350/React file.** One exported service/struct per Go file.
   Files start small and single-purpose and subdivide (per-entity, per-channel, per-provider,
   per-concern, per-panel) as they grow. **Over the ceiling = a split signal.**
7. **Store parity:** memory and Postgres impls stay behaviorally symmetric and say so in code (sort to
   match `ORDER BY`, strip `Logs` to match list projection, return copies so callers never race the
   executor/heartbeat); optimistic concurrency via an int `Version` + `ErrConflict`. Any new handler
   request field ⇒ a numbered migration pair under `store/postgres/migrations/` **and** matching
   impls in both `memory/` and `postgres/`.
8. **Frontend lockstep:** `api/`, `stores/`, `types/` carry one file per backend domain each. Data
   flows page → Zustand store → `api/<domain>.ts` → `api/client.ts`. Components never call `fetch`
   or hold backend URLs; `localStorage` is confined to `auth/`.
9. **Fail-safe cross-cutting services:** an unconfigured feature degrades to no-op/stub/503, never
   blocks the request path; config structs mirror `config.Config` to avoid importing `internal/config`.
10. **Same-PR doc coupling:** UAT behavior → `docs/guides/UAT.md`; auth/secrets/Dockerfile →
    `SECURITY.md`; feature → `design.md §11` + backlog Closed log; harness → `.claude/`. Vendored
    `loop-*`/`ponytail-*` skills are never edited in place. Deps via targeted `go get`, never
    `go mod tidy`; Go 1.25 pinned in `go.mod` and CI in lockstep.
11. **Generated artifacts** (`docs/api/*`, `docs/openapi.yaml`, `go.sum`) are DO-NOT-EDIT; their size
    tracks API surface and is not a smell.

## Per-layer template

| Layer | Path | Split axis | Where new code goes |
|---|---|---|---|
| Entry points | `backend/cmd/<binary>/` | thin `main()` + responsibility siblings (`client.go`, `commands.go`) | new binary = new `cmd/<name>/` with thin `main()`→`run()` seam; new subcommand = noun file |
| HTTP handlers | `backend/internal/handler/` | one `<domain>.go`, methods on `*Handler`; `webhook_<provider>.go` | new domain = `<domain>.go` + Handler field + test; new webhook = `webhook_<provider>.go` |
| Composition root | `backend/internal/server/` | `server.go` root, `router.go` routes, `middleware_<concern>.go`, `<subsystem>_boot.go`, `<infra>.go`+`<infra>_redis.go` | new backend wiring = a `selectXxx` case; new subsystem = `<name>_boot.go`; new route = `router.go` |
| Services | `backend/internal/service/` | flat, one `XxxService`/domain-noun per snake_case file + `NewXxx` | new service = `<domain>.go` + `NewXxx` + colocated test; declare narrow interface locally |
| Stage adapters | `backend/internal/{builder,pusher,deployer,stagerunner}/` | `<pkg>.go` iface + `<backend>.go` impls + `noop.go`; `selectXxx` switch | new backend = `<backend>.go` + `New*` + iface assertion + `selectXxx` case + UAT env doc |
| Deploy targets | `backend/internal/deploytarget/` | `target.go` iface+registry; one **sub-package** per backend | new target = `deploytarget/<name>/` sub-pkg + config-gated `Register` in `server/deploytargets.go` |
| SCM sources | `backend/internal/source/` | one sub-package per host, **structural parity** (`PushEvent`), no shared iface | new provider = `source/<provider>/` mirroring `PushEvent` + verify/parse + handler dispatch |
| Pure helpers | `backend/internal/{oci,buildplan}/` | flat, one concern per file, pure fns over `model.*` | new helper = `<concern>.go` + `<concern>_test.go` |
| Store contracts | `backend/internal/store/` | single `store.go` (ifaces + aggregate `Store` + sentinels) | new entity = iface in `store.go` + `Store` field + `New()` wiring (prefer grouped/options ctor) |
| Store — Postgres | `backend/internal/store/postgres/` | **strict one file per entity** (`XStore`+`NewXStore`+scan helpers) | new entity = `<entity>.go` + migration pair + `<entity>_test.go` |
| Store — memory | `backend/internal/store/memory/` | **should mirror postgres** — one file per entity | new entity = `memory/<entity>.go` (follow `license.go`/`stageapproval.go`), never appended to `memory.go` |
| Migrations | `backend/internal/store/postgres/migrations/` | numbered `NNN_name.{up,down}.sql` pairs | new column/entity = next-numbered pair (required when a request gains a field) |
| Domain model | `backend/internal/model/` | pure data, one file per entity/value cluster | new type = `<entity>.go`; new stage type extends `pipeline.go` `StageType` enum |
| Subsystems | `backend/internal/{jobqueue,logstore,idempotency,runstate}/` | interface-first, split **by concern** (`worker.go`, `backoff.go`) | new subsystem = new pkg w/ interface-first shape; new concern = `<concern>.go` + test |
| Platform | `backend/internal/{audit,observability,notifier,retry,kube,cloudinventory,gitops,scheduler,triage,templates,transport}/` | single-file until it earns a per-channel/provider/concern split | new capability = new pkg, single-file until it grows; new channel = sibling file (`slack.go`) |
| Config | `backend/internal/config/` | env load + prod `Validate()`; **target: split** structs/load/validate/env | new field = its struct + Load line + Validate gate + UAT env doc |
| Auth/security | `backend/internal/auth/` | one file per concern, fail-closed | new concern = `<concern>.go` + test + `SECURITY.md` update |
| FE app shell | `frontend/src/` | `main.tsx` entry, `App.tsx` route table (lazy) | new route = `lazy()` entry in `App.tsx` → `pages/` component |
| FE api | `frontend/src/api/` | one `<domain>.ts` (`xxxApi` object) + shared `client.ts` | new domain = `api/<domain>.ts` in lockstep w/ store + types |
| FE auth | `frontend/src/auth/` | one file per concern; dual-export provider+helpers | new concern = new file; keep dual-export; localStorage only here |
| FE hooks | `frontend/src/hooks/` | one `use<Thing>.ts` per hook | new hook = `use<Thing>.ts`; pure helpers → `utils/` |
| FE stores | `frontend/src/stores/` | Zustand, one `<domain>Store.ts` | new domain = `<domain>Store.ts` lockstep w/ api + types |
| FE pages | `frontend/src/pages/` | one route file; heavy pages extract panels to `pages/<page>/` | new route = `<Route>Page.tsx` + `App.tsx` lazy entry; push panels to `pages/<page>/`, logic to a hook |
| FE components | `frontend/src/components/` | feature subtrees split by React-Flow role; one file per widget | new stage node = thin wrapper → `StageNode`; new primitive = own file in `components/ui/` (not `atoms.tsx`) |
| FE theme | `frontend/src/theme/` | `ThemeProvider.tsx` + `tokens.ts` | new tokens → `tokens.ts` |
| FE types/utils | `frontend/src/{types,utils}/` | one file per domain (types) / topic (utils), pure | new type = `types/<domain>.ts`; new helper = `utils/<topic>.ts` + test |
| Deploy artifacts | `deploy/{docker,helm/cooker,kubernetes,aws}/` | one artifact per resource kind; helm↔raw 1:1 filenames | new env var = `deployment.yaml` (helm) + synced raw + gated values key + CI render assert |
| Ops/CI | `scripts/` + `.github/workflows/` | one script per action; one job per concern; SHA-pinned actions | new routine = `scripts/<name>.sh` + Makefile target; new CI concern = new job/workflow |
| Docs | `docs/` | split by purpose; index-and-link READMEs; numbered ordered sets | reference → `reference/`; how-to → `guides/`; decision → next `adr`; proposal → `proposals/` |
| Harness | `.claude/{agents,skills,workflows}/` | templated: agent `.md` skeleton, skill dir + routing table, workflow `.js` | new agent = `cooker-<role>.md`; new repo skill = `cooker-<name>/` over a `loop-*` counterpart |

## Adapter wiring — three legitimate mechanisms (pick the one the sibling package uses)

1. **`selectXxx(kind)` switch in `server.go`** — pipeline-stage families (`builder`, `pusher`,
   `deployer`, `stagerunner`). Unknown kind → `Noop`.
2. **Self-registration registry** — `deploytarget` (`Register`/`MustRegister`/`Lookup`, config-gated
   at boot in `server/deploytargets.go`, not `init()`).
3. **Structural parity, no shared interface** — `source` SCM adapters (identically-shaped
   `PushEvent`, dispatched at the handler layer).

## Current deviations — the refactor backlog (39)

Ranked within each area by value×safety (safest/highest-value first).

### Backend — oversized / low-cohesion
| Path | Issue | Target |
|---|---|---|
| `store/memory/memory.go` | 963 LOC, all 12 entities in one file (postgres already per-entity) | one file per entity per `license.go`/`stageapproval.go` precedent; keep only `New()` in `memory.go` |
| `config/config.go` | 864 LOC: ~30 structs + Load + Validate + env | `structs.go` / `load.go` / `validate.go` / `env.go` |
| `service/app_deployer.go` | 812 LOC: core + synth + manifests | `app_deployer.go` / `app_deployer_synth.go` / `app_deployer_manifest.go` |
| `handler/app.go` | 765 LOC: CRUD+deploy+canary+detect+GitHubWebhook | `app.go` / `app_deploy.go` / `webhook_github.go` |
| `service/executor.go` | 1520 LOC (~4× ceiling) | `executor.go` core + `_options` / `_outputs` / `_progress` / `_approval` / `_stageruntime` |
| `server/server.go` | 1184 LOC: ~600-line `New()` + factories + audit + cors | `server.go` + `factories.go` + `cors.go` + `audit_sink.go` |
| `deploytarget/ssh/ssh.go` | 778 LOC (~2×): seams+wrappers+pool+methods | `ssh.go` + `transport.go` + `pool.go` (reuse `known_hosts.go`) |
| `handler/pipeline.go` | 565 LOC | `pipeline.go` / `pipeline_run.go` / `pipeline_analytics.go` |
| `cmd/cookerctl/commands.go` | 609 LOC: dispatcher + 8 cmds + follow | `pipelines.go` / `runs.go` / `follow.go` / `helpers.go` |
| `handler/environment.go` | 412 LOC | `environment.go` + `environment_secrets.go` |
| `server/router.go` | 399 LOC monolithic `registerRoutes` | per-domain `router_<area>.go` registrars |
| `store/store.go` | 382 LOC; `New()` 17 positional args | grouped `Backends` struct or functional options |

### Backend — convention drift
| Path | Issue | Target |
|---|---|---|
| `model/apitoken_gen.go` | `_gen` implies generated, but hand-written crypto | rename `apitoken_crypto.go` (or fold into `apitoken.go`) |
| `handler/docker.go` (+registry, k8s-write) | legacy package-level funcs vs methods-on-`*Handler` | convert to methods on `*Handler` |
| `service/settingsconfig.go` | two services in one file, no test | `registryconfig.go` + `clusterconfig.go`, each with test |
| `service/host.go` | 270 LOC security-sensitive, no test | add `host_test.go` (key validation, secrets write, cache evict) |
| `store/postgres` | ~11 of 14 entity impls have no `_test.go` | add gated `<entity>_test.go` per impl behind CI Postgres |

### Backend — security surface (missing tests + DRY; no oversized files)
| Path | Issue | Target |
|---|---|---|
| `governance/executor_hook.go` | 117 LOC, security-relevant deny/fail-closed branching, **no test** (highest priority) | add `executor_hook_test.go` (fake store + fake authorizer) |
| `governance/extractor.go` | 54 LOC store-lookup short-circuits, no test | add `extractor_test.go` (memory store: 404→ok=false, missing-env) |
| `governance/client.go` | 381 LOC; `Authorize`/`AuthorizeOnBehalf` ~90% duplicated | extract shared `doAuthorize(...)`; optionally split HTTP into `authorize.go` |
| `secrets/vault/vault.go` | 177 LOC path-mapping + 404 classification, no test | add `vault_test.go` (isNotFound, keyPath, List trim) |
| `secrets/awsm/awsm.go` | 143 LOC, no test | add `awsm_test.go` (name mapping, 404→ErrNotFound) |
| `secrets/gcpsm/gcpsm.go` | 159 LOC, no test | add `gcpsm_test.go` (secretID/parent/version, 404 mapping) |
| `secrets/keepsave/client.go` | 178 LOC HTTP transport, only Manager tested | add `client_test.go` (httptest: do() error envelope, 404-as-success delete) |

### Frontend
| Path | Issue | Target |
|---|---|---|
| `pages/SettingsPage.tsx` | 909 LOC (extraction pattern already exists) | finish `pages/settings/{Secrets,Registries,Clusters,Tokens}Panel.tsx` |
| `pages/RunPage.tsx` | 1221 LOC: 5 sub-components + polling | `pages/run/*` + `hooks/useRunPolling.ts`; thin route |
| `pages/AppDetailPage.tsx` | 800 LOC monolithic component | `pages/appdetail/{Overview,Deployments,Services}Panel.tsx` |
| `components/ui/atoms.tsx` | 638 LOC, 20+ primitives mislabeled "atoms" | one file per primitive under `components/ui/` + `ui/helpers.ts` |
| `pages/NewAppWizard.tsx` | 657 LOC multi-step wizard | `pages/newapp/` one component per step |
| `pages/settings/LicensePanel.tsx` | 613 LOC (big inline `TIERS` literal) | move `TIERS` to a data module; panel = presentation |
| `pages/DockerPage.tsx` | 605 LOC images+containers | `components/docker/{Images,Containers}Panel.tsx` |
| `pages/AppsPage.tsx` | 551 LOC, eager-loaded landing route | extract `components/apps/{AppCard,AppsGrid}.tsx` |
| `pages/EnvironmentsPage.tsx` | 513 LOC list+editor | `components/environments/{EnvironmentList,EnvironmentEditor}.tsx` |

### Deploy / CI
| Path | Issue | Target |
|---|---|---|
| `deploy/docker/Dockerfile.{backend,frontend}` | orphaned dev stubs on stale `golang:1.22-alpine` | delete; the multi-stage `Dockerfile` is authoritative |
| `.github/workflows/ci.yml` | 517 LOC; helm job duplicates `make helm-validate` | extract `scripts/helm-validate.sh` used by both |
| `deploy/kubernetes/deployment.yaml` | 256-LOC hand-synced parity mirror, drift-prone | add CI diff of `helm template` vs `deploy/kubernetes/` |

### Harness (.claude) — doc drift the harness doc itself predicts
| Path | Issue | Target |
|---|---|---|
| 5 agents (`cooker-infra-ci`, `cooker-planner`, `cooker-backend-{data,api,adapters}`) | stale Go 1.22 / x/time v0.5.0 pin | update to Go 1.25 / v0.15.0 |
| ~8 agent/skill files | broken flat doc links (`docs/architecture.md` etc.) | rewrite to `docs/reference/` + `docs/guides/` |
| `cooker-security.md` | frontmatter `model: opus` vs harness table `sonnet` | reconcile to one |

## Explicitly NOT deviations (do not "fix")
Generated files (`docs/api/*`, `docs/openapi.yaml`, `go.sum`); large-but-idiomatic gated config
surfaces (helm `values.yaml` at 563 gated sections); per-`StageType` node wrappers (6–15 lines by design).

## Security & secrets surface (coverage gap now closed)
A follow-up read mapped `auth`, `crypto`, `secrets`, `license`, `entitlements`, `governance`,
`validate` in full. The surface is **structurally clean** — no production file exceeds ~400 lines
(tallest: `governance/client.go` 381, `auth/oidc.go` 373). It reinforces the universal pattern with
security-specific rules:
- **Interface-first, adapters-in-subpackages** for `secrets` (`manager.go` = interface + sentinels;
  one subpackage per backend selected by `COOKER_SECRETS_BACKEND`; every adapter translates its
  backend 404 to `secrets.ErrNotFound`; authorization stays in the handler, never the adapter).
- **Pure, dependency-free verifiers** for `license`/`entitlements`/`validate` (verify-before-unmarshal;
  `entitlements` degrades to `Free()` and never errors, so an expired license can't brick an install).
- **Fail-closed / degrade-safe defaults everywhere**; secrets never logged; generic client-facing auth
  errors (no verifier-oracle); constant-time / bcrypt credential comparison; lock-free hot paths
  (`atomic.*`, `sync.Map`).

The only gap is **test coverage, not structure** — the `secrets` cloud adapters and `governance`
hooks lack tests (folded into the backlog above). Intentional (non-defect) test-naming splits to
document rather than fix: `auth/local_middleware_test.go` (covers `oidc.go`'s local-token branch),
`validate/validate_{cache,m2}_test.go`, and `auth/oidc_lazy_test.go`.
