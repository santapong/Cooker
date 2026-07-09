# Backend review — patterns and improvement areas

Snapshot review of the Cooker Go backend (`backend/`, module `github.com/santapong/cooker`, Go 1.25). ~28k LOC of non-test source across ~50 packages under `internal/`, plus `cmd/`. This is the "what's the current best-practice pattern, and what can we improve" reference. It also seeds the deploy-simplification (Phase 1) and domain-restructure (Phase 3) work.

## How the backend is organised today

Packages are grouped **by technical layer**, not by feature:

- `internal/handler/` — thin HTTP layer, one file per domain (27 files).
- `internal/service/` — business logic (`Executor`, `AppDeployer`, `Promoter`, canary, …).
- `internal/store/` — persistence interfaces + `memory` and `postgres` implementations.
- Strategy/adapter packages — `builder/`, `pusher/`, `deployer/`, `deploytarget/`, `stagerunner/`, `secrets/`, `notifier/`, `source/`.
- Cross-cutting leaves — `config/`, `auth/`, `crypto/`, `audit/`, `observability/`, `jobqueue/`, `scheduler/`, `model/`, etc.
- Composition root — `internal/server/` (`New()` wires everything; `router.go` holds the route table).

Finding one feature end-to-end means opening `handler/X.go`, `service/X.go`, and `store/**/X.go` in three different folders. That "what depends on what for feature X" cost is the motivation for the Phase 3 domain restructure.

## Patterns done well (keep these)

- **Strict handler → service → store/strategy layering, and it's honoured.** `grep gin-gonic/gin internal/service` returns nothing — no HTTP types leak into business logic. Trace: `handler/app.go:219 DeployApp` parses the request and delegates to `service/app_deployer.go` which drives `service/executor.go`. Handlers do HTTP; services do logic; adapters implement narrow interfaces.
- **Narrow strategy interfaces with a `Noop` default and an `ErrUnavailable` sentinel** — `builder.Builder`, `pusher.Pusher`, `deployer.Deployer`, `secrets.Manager`, `stagerunner.Runner`. Each is a single-method (or near) port with a safe no-op fallback, selected by config.
- **Optional-capability interfaces, type-asserted at the seam.** Only Kubernetes deployers implement `deployer.WeightedDeployer` / `CanaryProber`; the service checks `if wd, ok := deploy.(deployer.WeightedDeployer); ok { … }` (`server.go:332`) instead of widening the base interface. Clean capability negotiation.
- **Dependency-cycle inversion via consumer-defined interfaces.** `handler.RunSpawner` / `handler.JobEnqueuer` and `config.SSHHostLister` are declared where they're *used*, so lower layers don't import upward. Idiomatic Go, cycle-free (verified: `service` never imports `handler`/`server`).
- **Typed store errors checked via `errors.Is`.** `store.ErrNotFound` / `ErrConflict`, funnelled through `handler.abortStoreErr` (`handler.go:185`). Error wrapping carries a package prefix (`fmt.Errorf("oidc: discover: %w", err)`).
- **`panic` confined to startup/registration** — all non-test panics are in `Register`/`MustRegister`/FSM-builder paths, never in request handling.
- **Careful lifecycle** — LIFO `cleanups` teardown stack, drain-with-timeout on shutdown, embedded migrations applied under a `pg_advisory_lock`.

## Tech-debt hot spots (ranked; the "hard to maintain" list)

1. **God-files.** A handful of files carry too many responsibilities and are the hardest to change safely:
   - `service/executor.go` (~1520 LOC) — DAG scheduling **+** build/push/deploy dispatch **+** edge-condition evaluation **+** outputs interpolation **+** approval-gate polling **+** progress persistence **+** log capping, all in one file. Prime split candidate (`scheduler` / `stagedispatch` / `outputs` / `progress`).
   - `server/server.go` (~1184 LOC) — a ~600-line linear `New()` constructor that also holds every `selectXxx` factory and the audit sink. Hard to unit-test in isolation.
   - `config/config.go` (~864 LOC) — the entire env-var surface, parsing, and `Validate()` in one file.
   - Others: `service/app_deployer.go` (~812), `store/memory/memory.go` (~963, all 15 store interfaces in one file), `deploytarget/ssh/ssh.go` (~778), `handler/app.go` (~765), `service/canary.go` (~620).

2. **God-object assembly points / brittle constructors.**
   - `handler.Handler` carries ~24 injected, mostly-optional dependencies, each nil-checked at its call site to return 503. It's the single assembly point for nearly every feature.
   - `store.New(...)` takes **17 positional arguments** (`store/store.go:345`) — easy to mis-order, painful to extend. An options struct or `store.Config` would remove the footgun.

3. **Two competing plugin-registration idioms for the same concept.** Builder/pusher/deployer/stagerunner/secrets use `selectXxx` switch factories in `server.go`; deploytarget/notifier/jobqueue use global self-registration registries (`Register`/`MustRegister` + package-global maps). A reader must learn both to follow "how does a new backend get wired." Pick one.

4. **Package-level handlers that break the `*Handler` convention.** `handler.ListDockerImages` / `BuildDockerImage` (`handler/docker.go:75,86`) and `handler.ScaleWorkload` (`handler/kubernetes.go:148`) are bare package functions that shell out to `docker`/`kubectl` directly and rely on **module-global mutable state** — `var composeBaseDir` with a `SetComposeBaseDir` setter (`handler/docker.go:20-25`). Everywhere else handlers are methods on an injected `*Handler`. This is I/O + logic + global state in the HTTP layer.

5. **Doc/code drift (correctness of the mental model).**
   - Docs claim the SPA is embedded via `//go:embed` (`docs/system-design/01-overview.md:14`, `docs/system-design/11-code-patterns-and-conventions.md:17`). It is **not** — the frontend is served from the hardcoded on-disk path `/usr/share/cooker/static` (`server/router.go` + `static.go`). Only the SQL migrations are embedded.
   - `make migrate-up` / `migrate-down` invoke `go run ./cmd/cooker migrate up`, but `cmd/cooker/main.go` has **no subcommand parsing** — it ignores the positional args and boots the server. Migrations actually run implicitly at startup.

6. **`internal/deployer` mixes two runtimes.** Kubernetes backends (`clientgo`, `kubectl`, `weighted`) and Docker backends (`compose`, `dockerrun`) live in one package despite being very different targets.

## Where the improvements land

- **Items 1, 2, 3, 4, 6** are addressed by the Phase 3 domain restructure (split the God-files, unify the plugin idiom, replace the positional constructors, convert the package-level handlers, split the deployer package by runtime).
- **Item 5** is addressed in Phase 1 (add a real `migrate` subcommand; make the static dir configurable via `COOKER_STATIC_DIR`; correct the docs).

## Net assessment

The layering contract is sound and well-observed in the service/store core — this is a healthy codebase, not a tangle. The debt is concentrated and mechanical: a few oversized files, two God-object assembly points, two plugin idioms where there should be one, a small set of convention-breaking handlers, and some doc drift. None of it requires re-architecting; it's decomposition and consistency work, which is exactly what the phased plan targets.
