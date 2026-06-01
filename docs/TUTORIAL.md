# Cooker — Feature Tutorial

A practical, **skippable** walkthrough of every feature. Each section is tagged so you can jump
straight to what you need:

- 🟢 **Start here** — do this first.
- 🔵 **Core** — the main workflow.
- 🟡 **Optional** — skip unless you need it.
- ⚪ **Advanced / ops** — for operators and HA deployments.
- 🚧 **Not yet** — designed or stubbed, not usable today (so you don't waste time).

> Task-oriented by design. For *how it works inside*, see
> [`system-design/`](system-design/README.md). For the full endpoint list, see
> [`system-design/14-api-reference.md`](system-design/14-api-reference.md).

---

## 0. 🟢 Run Cooker locally (start here)

Dev mode has **auth OFF** (injects a dev-admin), so you can click around immediately.

```bash
make dev          # builds backend+frontend, runs the single binary on :8080
# open http://localhost:8080
```

- **Builders default to `noop`** (`COOKER_BUILDER/PUSHER/DEPLOYER=noop`) — a fresh install won't
  actually build or deploy until you opt into a real backend (§7). Intentional and safe.

**Skip if:** you only want the API/spec — load [`openapi.yaml`](openapi.yaml) into Swagger UI.

---

## 1. 🔵 Build a pipeline (the core feature)

A **pipeline** is a graph (DAG) of **stages** connected by **edges**. Stage types: `build`, `test`,
`deploy`, `push`, `approval`, `custom`, `gitops-commit`.

1. **Pipelines → New** → drag stages onto the canvas, connect them with edges.
2. Configure each stage in the side panel.
3. **Validate** (catches cycles + type errors), then **Run**.
4. Watch live logs stream per stage; nodes tint live as the run progresses.

**How edges run:** stages execute **level-by-level** — everything with satisfied dependencies runs in
parallel (capped at 16). If an upstream stage fails, downstream stages don't run.

> ⚠️ **Edge conditions** (`failure` / `always`) are **rejected at save** today — only the default
> (run-on-success) works. See 🚧 §13.

**Inter-stage outputs.** A stage can reference an upstream stage's outputs in its own string config
fields with `${stages.<stageId>.<key>}`. The reference is substituted just before the stage runs.
The headline case is a Push stage that pins the digest the Build stage produced:

```
Push stage → repository:  reg/app@${stages.build.digest}
```

Emitted keys by stage type:

| Stage | Keys |
|---|---|
| `build` | `digest` (image ID), `tag` (first tag), `tags` (comma-joined) |
| `push` | `digest`, `ref` (resolved destination) |
| `deploy` | `resources` (comma-joined applied resources) |
| `gitops-commit` | `commit` (SHA), `ref` (`repo@branch`) |

Rules: the referenced stage must be an **ancestor** (there must be an edge path to it) — validated at
save. Unknown stage / non-ancestor references are rejected at save; an unknown output **key** fails the
stage at run time (keys only exist once the upstream runs). Other `${...}` tokens (e.g. `${IMAGE}`
runtime templating) pass through untouched. `script` is intentionally **not** interpolated. Outputs are
capped at 4 KiB per value / 32 KiB per stage. Disable the whole feature with
`COOKER_OUTPUTS_ENABLED=false`.

**Skip if:** you deploy single apps (use §3) or compose stacks (use §4c).

---

## 2. 🟡 Use a pipeline template

**Templates → pick one → Create** produces a ready pipeline you can edit. Admins manage the catalog
under **Admin → Templates**.

---

## 3. 🔵 Deploy an App (the easy path)

An **App** points at a git repo; Cooker figures out how to build and deploy — no manual pipeline.

1. **Apps → New** → repo URL + branch.
2. Pick a **deploy target** (Kubernetes / Docker host / Cloud Run / ECS / Fly / Render — §6).
3. **Deploy.** Cooker clones, **detects the build plan** (`Dockerfile` → `docker-compose` → buildpack),
   synthesizes a pipeline, runs it, streaming logs. Image tag: `<registry>/<app>:<unix-ts>`.

---

## 4. 🔵 Docker & Docker Compose — what actually happens

### 4a. Build a single image
**Docker → Images → Build** builds through the configured builder backend (§7). Rate-limited.

### 4b. Visualize a Compose file
**Docker → Compose → Parse a file** draws a **topology graph** — one node per service, edges from
`depends_on` and env-var references. **View-only**: it visualizes relationships; nothing runs.

### 4c. 🔵 Deploy a Compose stack as a per-service DAG (Deployment DAG World)

This is the headline feature. When an **App's repo has a `docker-compose.yml`** and you deploy it,
Cooker builds a **real per-service execution DAG** — not one opaque unit:

- **one `build → push → deploy` sub-chain per service** (build/push skipped for `image:`-only
  services — they deploy the prebuilt image);
- **cross-service edges from `depends_on`** — a service's *deploy* waits on its dependencies' deploys
  (build/push still parallelize across services);
- services are grouped into **group boxes** in the canvas, auto-derived from a label
  (`labels: { com.cooker.group: backend }`) → else the service's sole network → else `default`;
- **per-service resource limits** from the compose file (`mem_limit` / `cpus` /
  `deploy.resources.limits`) are applied — as K8s `resources.limits` *or* `docker run --memory/--cpus`.

**Your example — a web app with a database, an API gateway, and more:**

```
build-web → push-web → deploy-web ─┐ (web depends_on api)
build-api → push-api → deploy-api ─┤
                       deploy-db ──┘ (api depends_on db; db is image-only)
```
…rendered as group boxes (e.g. `frontend` = web; `backend` = api, db, gateway), tinting live as each
stage runs.

**Two runtimes** — picked by the App's deploy target:

| Target | Runtime |
|---|---|
| Kubernetes | per-service Deployment+Service applied via client-go, with `resources.limits` |
| Docker host | `docker run -d --restart=always --memory --cpus …` per service (or `docker compose up`) |

After deploy, open the **deployment view** (the 202 response gives you the URL): it shows the grouped
DAG, tints nodes live, and — **click any service node** — opens a **runtime panel** with the live
container/pod state (id, image, applied limits) and a **tailing log viewer** (§11b).

> ⚠️ **Docker-host runtime needs the Docker socket** — same RCE-to-host risk as the docker builder.
> Dev / single-node only; use a Kubernetes target in clusters.

**Skip if:** you deploy single images (§4a) or hand-built pipelines (§1).

---

## 5. 🔵 Environments & promotion (Dev → Staging → Prod)

**Environments** are *your* named tiers, ordered by an integer `Order` (no hardcoded names).

1. **Environments → New** per tier; set `Order` + a target.
2. Give each a **promotion policy**: `auto` or `manual` (with N required approvers).
3. After a successful run, **Promote** to the next environment; `manual` policy waits in
   **awaiting_approval** until an **approver** (or admin) approves.

---

## 6. 🟡 Deploy targets

Per-app target kinds: **Kubernetes** (full), **Docker host** (per-service `docker run`), plus
**Cloud Run, ECS, Fly.io, Render, SSH** adapters. Set the target on the App (§3).

---

## 7. ⚪ Real build/push/deploy backends (required for real work)

Defaults are `noop`. To actually run:

| Env var | Options | Notes |
|---|---|---|
| `COOKER_BUILDER` | `docker`, `kaniko`, `buildah`, `buildkit`, `noop` | **`kaniko`/`buildah`** for prod (rootless, no socket) |
| `COOKER_PUSHER` | `docker`, `crane`, `noop` | `crane` for prod |
| `COOKER_DEPLOYER` | `kubectl`, `clientgo`, `noop` | `clientgo` for prod K8s |

The Docker-host **deploy** runtimes (`docker run` / `compose up`) are selected automatically for
docker-host App targets — see §4c.

> ⚠️ `docker` builder/pusher + the docker-host deploy runtime bind the host Docker socket — **forbidden
> in production**. Use `kaniko`/`buildah`/`crane` and a Kubernetes target.

---

## 8. ⚪ Secrets & per-environment config

Each environment holds plain vars + encrypted **secrets** (`COOKER_SECRETS_BACKEND`: `database`
default, `keepsave`, `vault`, `aws`, `gcp`). Reveal is admin+MFA gated. With `database`, an unset
`COOKER_SECRET_KEY` makes secret endpoints return **503** (fail-safe). Secret promotion works only on
KeepSave (database returns **501**).

---

## 9. 🟡 Auth, roles & MFA

OIDC (PKCE) in prod; map IdP groups → roles via `COOKER_OIDC_GROUP_MAP`. Roles: `admin`, `operator`,
`approver`, `viewer`. MFA step-up on destructive routes (UI re-challenges on 403). Local username/
password JWT for IdP-less setups.

---

## 10. 🟡 Git webhooks (auto-deploy on push)

Set an app's webhook secret (admin+MFA), point the provider at
`POST /webhooks/{github|gitlab|bitbucket|gitea}`. Cooker verifies the signature (HMAC; GitLab uses a
token), dedupes via idempotency key, and triggers the app's deploy.

---

## 11. ⚪ Real-time views

### 11a. Run / stage logs
Run pages stream **live stage logs** (build, push, deploy) over WebSocket — fetch a single-use 60s
ticket (`POST /ws-tickets`), then connect with `?ticket=`.

### 11b. Runtime tracing (deployment view)
In the deployment DAG (§4c), **clicking a service node** opens a panel that tails the **actual
container/pod logs** and shows live runtime info (state, image, applied resource limits) — Docker via
`docker inspect`/`logs`, Kubernetes via `kubectl`.

> ✅ **Stage-log replay (memory backend, single-replica):** connect mid-run → you get the backlog so
> far, then live lines; reconnect with `?since=<seq>` → only the lines after `seq`. A dropped slow
> client gets a `stream-truncated` signal rather than silence. History survives stage completion in a
> bounded in-memory buffer (lost on restart; the REST snapshot remains the durable record). Durable /
> multi-replica `postgres`/`redis` backends are still future (§13).

---

## 12. ⚪ Operating at scale (HA)

Several subsystems default to in-memory (per-replica). For multi-replica, switch to shared backends:
`COOKER_WS_HUB_BACKEND=redis`, `COOKER_WS_TICKET_BACKEND=redis`, `COOKER_RATE_LIMIT_BACKEND=redis`,
`COOKER_JOBQUEUE_ENABLED=true`, `COOKER_SCHEDULER_ENABLED=true`. Details:
[`system-design/16-non-functional.md`](system-design/16-non-functional.md),
[`guides/MULTI_REPLICA.md`](guides/MULTI_REPLICA.md). UAT sandbox: `make uat-up`
([`guides/UAT.md`](guides/UAT.md)).

---

## 13. 🚧 Not yet available

| Feature | Status |
|---|---|
| **Conditional edges** (`failure` / `always`) | Rejected at save; only success-edges run (§1) |
| **Build caching** (Kaniko/BuildKit cache) | Not wired — every build is cold |
| **Post-stage hooks** (`always`/`failure` cleanup) | Not built |
| **Stage-log replay / history over WS** | Live now via the **memory** backend (mid-run join + `?since=` reconnect, single-replica, §11a); durable/multi-replica `postgres`/`redis` backends still pending |
| **Docker/K8s list & inspect REST endpoints** | Stubs — return empty/sample data |
| **Multi-tenancy** | Designed (ADR-0004), not implemented — single-tenant today |

Roadmap: [`proposals/dag-adaptation-2026.md`](proposals/dag-adaptation-2026.md) and
[`proposals/execution-observability-redesign-2026.md`](proposals/execution-observability-redesign-2026.md).
Canonical "what's real vs not": [`system-design/12-reality-check.md`](system-design/12-reality-check.md).

---

## Quick reference

| I want to… | Section |
|---|---|
| Just try it | §0 |
| Build a custom workflow | §1 |
| Deploy a repo with minimal setup | §3 |
| See compose services as a graph | §4b |
| **Deploy a multi-service stack as a live DAG** | **§4c** |
| Ship through Dev→Staging→Prod | §5 |
| Make builds/deploys actually happen | §7 |
| Trace a running container/pod | §11b |
| Run in production / HA | §12 |
| Know what's *not* real yet | §13 |
