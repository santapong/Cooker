# Plan — SSH remote + IaC plane + cloud breadth (2026-05)

## Context

After Phase 1 + Phase 2 (PR #89) and the deferred-backlog close-out
(PR #91), Cooker is solid on the architectural primitives — durable
job queue, run/stage FSM, permission middleware, notifier, cron
scheduler, multi-provider git webhooks, pipeline templates, admin
CRUD UI.

The next theme is **reach**: Cooker should deploy to *anything an
operator already has*, not only the cloud runtimes we have native
adapters for today (k8s, ecs, fly, render, cloudrun). The three
threads to ship together:

1. **SSH remote** — operator registers `user@host`, Cooker pulls the
   built image on that host and `docker run`s it (Dokploy / Coolify
   model). No K8s required, no agent on the box, no cloud APIs.
2. **IaC plane** — both an `iac` stage type that runs the operator's
   own Pulumi or Terraform program inside a pipeline, AND official
   bootstrap modules so installing Cooker itself is one `terraform
   apply` or `pulumi up`.
3. **Cloud breadth** — DigitalOcean App Platform, Azure Container
   Apps, Hetzner (via IaC), AWS EC2 (via SSH). Each is its own
   deploytarget adapter, shipped incrementally per cloud.

## IaC engine choice — Pulumi-first, Terraform-equal

I'd commit to **Pulumi as the embedded engine** and **Terraform as
the catalog format**. Rationale:

- **For the runtime `iac` stage**: Pulumi's **Automation API** is a
  Go library. Cooker can `import "github.com/pulumi/pulumi/sdk/v3/
  go/auto"` and drive `pulumi up` *in-process* from a job-queue
  worker — no subprocess, no separate CLI binary in the image, no
  YAML translation. Terraform, by contrast, only exposes `terraform`
  the binary; running it from Cooker means shelling out and parsing
  text output. Pulumi wins for embeddability in a Go service.
- **For the operator's own code**: ship adapters for *both*. The
  `iac` stage's `engine` field picks one. Operators with existing
  Terraform shouldn't be forced to rewrite; operators starting
  greenfield should be steered to Pulumi because it integrates
  tighter.
- **For the bootstrap modules**: ship both a `terraform/cooker`
  module and a `pulumi/cooker` component package, both producing
  identical K8s manifests via the existing Helm chart. Operators
  pick the one their org already uses.

The default in docs and quick-start is Pulumi. Terraform is "first-
class peer", not "afterthought".

## Scope per thread

### Thread 1 — SSH remote deploy target

**Goal:** `POST /api/v1/apps/:id/deploy` with `deployTarget.kind=ssh`
runs `docker pull <image>` then `docker run -d --restart=always …`
on a registered SSH host. Image pulled with the host's registry
credentials (or anonymous if public).

**Files (backend):**

- `backend/internal/deploytarget/ssh/ssh.go` — adapter implementing
  the `deploytarget.Target` interface. Uses
  `golang.org/x/crypto/ssh` to run commands. Each `Deploy` step:
  1. `docker pull <image>`
  2. `docker stop <container>` (best effort, ignore "no such")
  3. `docker rm <container>` (best effort)
  4. `docker run -d --restart=always --name <container> -p … -e …
     <image>` with env / port / volume args composed from
     `Spec.AppSpec`
  5. `docker inspect` to capture the running container ID
  - Errors propagate verbatim; `LogWriter` gets stdout of every step
    so the run page tails real `docker pull` progress.
- `backend/internal/deploytarget/ssh/known_hosts.go` — server key
  verification. Default policy: pin known-host on first connect,
  refuse on key change (TOFU). Override via per-host
  `strictHostKey=false` for dev.
- `backend/internal/model/host.go` — extend the existing `Host`
  model with `SSH *SSHCredentials` field. `SSHCredentials` has
  `User`, `KeyRef` (secrets-manager URI to the private key), and
  `Port` (default 22).
- `backend/internal/store/postgres/migrations/014_ssh_hosts.up.sql`
  — adds `ssh_user`, `ssh_key_ref`, `ssh_port`, `ssh_known_host`
  columns to `hosts`.
- `backend/internal/deploytarget/registry.go` — register the SSH
  adapter alongside the existing ones; selected when
  `Host.Kind=ssh-docker` (new kind constant).

**Files (frontend):**

- `frontend/src/pages/HostsPage.tsx` — extend the "Add host" form
  with an SSH-host option: user, port, key (pasted PEM or selected
  from existing secrets), strictHostKey toggle.
- `frontend/src/api/hosts.ts` — types updated to carry the SSH
  fields; key field is write-only (never returned in GET, matches
  the existing webhook-secret redaction pattern).

**Security gates:**

- The SSH private key is stored through `secrets.Manager` like every
  other credential. `GET /api/v1/hosts/:id` redacts it; `PUT
  /api/v1/hosts/:id` accepts `sshPrivateKeyPem` and re-encrypts.
- Connection enforces `ssh.HostKeyCallback` — refuse default
  `ssh.InsecureIgnoreHostKey()` in production, `Config.Validate`
  fails boot if any host has `strictHostKey=false` AND
  `COOKER_ENV=production`.
- Audit: every deploy logs `(host_id, host_addr, image, container)`
  to the existing audit middleware.

### Thread 2 — IaC plane

**Goal A — `iac` stage type:**

`StageConfig.Type=iac` runs an operator-provided IaC program against
their cloud account. Two engines, switched by `StageConfig.Engine`:

- `pulumi` — Cooker uses Pulumi Automation API (`go-auto`). The
  operator's program is a Go / TypeScript directory checked into
  the same repo as their pipeline definition; Cooker clones the
  repo, runs `pulumi up --stack <name> --yes` in-process.
- `terraform` — Cooker shells out to the `terraform` binary
  (bundled in the Cooker image at `/usr/local/bin/terraform`).
  Runs `init`, then `apply -auto-approve`.

**Files (backend):**

- `backend/internal/iac/iac.go` — `Runner` interface:
  `Apply(ctx, Spec) (Outputs, error)` / `Plan(...)` / `Destroy(...)`.
- `backend/internal/iac/pulumi/pulumi.go` — Pulumi engine. Uses
  `auto.NewLocalWorkspace` pointing at a checked-out repo path.
  Streams `events` channel into the executor's `LogWriter`.
- `backend/internal/iac/terraform/terraform.go` — Terraform engine.
  Wraps `os/exec` with timeout + log streaming.
- `backend/internal/model/pipeline.go` — `StageTypeIAC` constant +
  `StageConfig.IACEngine`, `StageConfig.IACStack`,
  `StageConfig.IACWorkdir` fields.
- `backend/internal/service/executor.go` — new dispatch case
  `case model.StageTypeIAC: e.executeIAC(...)`. State machine: same
  pending→running→succeeded/failed pattern as build/push/deploy.
- `backend/internal/iac/outputs.go` — IaC outputs land in
  `StageRun.Outputs` (JSONB), reachable from later stages via
  `${stages.<id>.outputs.<key>}` interpolation (reuses the existing
  variable-substitution machinery in `service/pipeline.go`).

**Files (deploy/ — Goal B — bootstrap modules):**

- `deploy/terraform/cooker/main.tf` + `variables.tf` + `outputs.tf`
  — Terraform module that wraps the Helm chart with `helm_release`,
  exposes the same values surface as `values.yaml`. Tested via
  `terraform validate` in CI.
- `deploy/terraform/cooker/examples/eks/` and
  `deploy/terraform/cooker/examples/aks/` — full-cluster example
  configs.
- `deploy/pulumi/cooker/` — Pulumi component package (Go module)
  wrapping the same Helm chart via `pulumi-kubernetes`. Same input
  surface. Builds via `pulumi-go`.
- `deploy/pulumi/cooker/examples/{eks,gke,aks}/` — example programs.

**Bundling Terraform binary:**

- `deploy/docker/Dockerfile` adds a `terraform` install step in the
  runtime stage (hashicorp/terraform:1.9 from public registry,
  pinned digest). Adds ~80 MB to the image; gated behind a build
  arg so operators who don't need IaC can keep the slim image.

### Thread 3 — Cloud breadth (new deploytargets)

Four new adapters, each implementing `deploytarget.Target` with
`Deploy / Status / Logs / Rollback`. Same shape as existing
`deploytarget/{ecs,flyio,render,cloudrun}`.

- `backend/internal/deploytarget/digitalocean/` — DO App Platform
  API. `Deploy` calls `PUT /v2/apps/:id` with updated `image.tag`;
  `Status` polls `GET /v2/apps/:id/deployments/:latest`.
- `backend/internal/deploytarget/azurecontainerapps/` — Azure
  Container Apps via `armappcontainers` SDK. Uses managed-identity
  auth where available, AAD client cred otherwise.
- `backend/internal/deploytarget/hetzner/` — Hetzner doesn't have a
  PaaS-like API for containers. **The Hetzner "deploy target" is
  actually a thin shim that drives the `iac` engine from Thread 2**
  with a built-in Pulumi component (`deploy/pulumi/hetzner-runner/`)
  that provisions a Server + cloud-init that runs `docker run`.
  This is the marquee example of why IaC + deploytarget compose
  cleanly.
- `backend/internal/deploytarget/ec2ssh/` — AWS EC2 via SSH.
  Lightweight: reads instance address from an existing EC2 Auto-
  Scaling Group tag, then delegates to the **SSH adapter from
  Thread 1**. Demonstrates adapter composition.

Each cloud also needs:
- `backend/internal/config/config.go` env vars (`COOKER_DO_TOKEN`,
  `COOKER_AZURE_TENANT_ID`, etc.) with `Config.Validate` gates in
  production.
- `docs/UAT.md` selector value documentation.
- `.env.uat.example` example block.
- `backend/internal/deploytarget/<cloud>/<cloud>_test.go` table-
  driven happy-path + auth-failure unit tests with stubbed HTTP.

## Team / subagent management

This is a 7–10 day cross-stack feature. **Spawn pattern**:

```
cooker-feature-dev  (root orchestrator)
├── Thread 1 (SSH)
│   ├── cooker-backend-data         schema migration 014_ssh_hosts
│   ├── cooker-backend-adapters     ssh deploytarget adapter
│   ├── cooker-backend-api          host CRUD + SSH key fields
│   ├── cooker-frontend-ui          HostsPage SSH form
│   └── cooker-security             SSH key handling + known_hosts review
├── Thread 2 (IaC)
│   ├── cooker-backend-adapters     iac.Runner + pulumi/terraform engines
│   ├── cooker-backend-api          iac stage dispatch in executor
│   ├── cooker-backend-data         stage outputs JSONB column
│   ├── cooker-infra-deploy         terraform module + pulumi component
│   ├── cooker-infra-ci             helm-validate + terraform-validate + pulumi-preview gates
│   └── cooker-security             review credential surface for both engines
└── Thread 3 (cloud breadth)
    ├── cooker-backend-adapters     DO, Azure, Hetzner, EC2SSH adapters
    ├── cooker-backend-api          config + selector wiring
    └── cooker-infra-ci             new env-var coverage in CI smoke tests
```

**Rules for the orchestrator** (cooker-feature-dev) when this lands:

1. **Sequence threads, parallelise within a thread.** Thread 1 ships
   first because Thread 3 (EC2SSH, Hetzner) depends on it. Thread 2
   ships second because Hetzner's deploytarget reuses the Pulumi
   engine. Thread 3 ships last.
2. **Within a thread, run cooker-backend-data + cooker-backend-
   adapters in parallel** (schema and adapter logic don't conflict;
   their files don't overlap). Join, then run cooker-backend-api +
   cooker-frontend-ui in parallel.
3. **Mandatory security review gates**:
   - SSH adapter + known_hosts before SSH lands → cooker-security
   - IaC credential surface (cloud creds passed to operator code)
     before IaC ships → cooker-security
4. **One PR per thread**, not one mega-PR. Each thread is reviewable
   in isolation; reviewer fatigue is the silent killer of cross-
   stack features.
5. **Verification per thread**:
   - Local: `go vet`, `go test ./... -race`, `npm run lint && npm
     run build`
   - Integration (gated by env var, same pattern as the live-DB
     jobqueue tests in PR #91): live-SSH against a sandbox box;
     `terraform plan` against `examples/eks/`; Pulumi preview
     against `examples/gke/`.

**Estimated effort:**
- Thread 1: 2 days (1 backend, 0.5 frontend, 0.5 security)
- Thread 2: 4 days (Pulumi Automation API + Terraform shell-out are
  fiddly; bootstrap modules are 1 day each)
- Thread 3: 3 days (1 per cloud, parallelisable)
- Total: ~7–10 calendar days for one engineer + agent help

## Acceptance criteria

Per thread, "done" means:

- All new files have unit tests (race detector on)
- `go vet ./...` + `go test ./... -race` green
- Frontend: `npm run lint` + `npm run build` clean
- Integration tests gated by their own env var, build clean, skip
  cleanly
- Each PR has its own draft PR with a Test Plan checklist
- Docs updated: `README.md` (new env vars, new targets), `docs/
  UAT.md`, `docs/architecture.md` (new subsystem rows)
- `CHANGELOG.md` entry under `[Unreleased]`
- `backlog.md` items moved into "Closed" log with PR numbers

## Out of scope (deferred to a later cycle)

- Pulumi YAML projects (Go and TypeScript only in v1)
- Terraform Cloud / Pulumi Cloud state backends (local + S3-backed
  state only in v1)
- Multi-cluster K8s federation
- GitOps for the IaC modules themselves (operators can use Argo CD
  / Flux if they want; Cooker doesn't enforce)
- Swarm-mode multi-host SSH (single-host only in v1 per your scope
  answer)

---

# Next-session prompt (copy-paste ready)

Paste the block below into the next Cooker session. It's self-
contained and tells the agent exactly which plan to execute.

```
Execute the plan at docs/plans/2026-05-ssh-iac-cloud-breadth.md
on a new branch `claude/ssh-iac-cloud-breadth-<short-slug>`.

Scope:
  Thread 1 — SSH remote deploy target (Dokploy-style ssh → docker run)
  Thread 2 — IaC plane (iac stage type + bootstrap terraform/pulumi modules)
  Thread 3 — Cloud breadth (DigitalOcean App Platform, Azure Container Apps,
              Hetzner via iac, AWS EC2 via SSH)

Engine choice for IaC (already decided, do not re-litigate):
  Embedded engine: Pulumi Automation API (Go)
  Catalog/bootstrap parity: Terraform + Pulumi shipped as equals
  Default in docs + quick-start: Pulumi
  Operators with existing Terraform get a first-class adapter, not a shim

Sequence:
  1. Thread 1 lands first. Open PR. Wait for review (or unblock with
     cooker-security signoff if no human reviewer in the window).
  2. Thread 2 lands second. Open PR.
  3. Thread 3 lands third. Open PR. Hetzner depends on Thread 2; AWS
     EC2/SSH depends on Thread 1.

Team plan: spawn cooker-feature-dev as the root coordinator. It
spawns cooker-backend-{adapters,api,data}, cooker-frontend-ui, and
cooker-security inside each thread per the matrix in
docs/plans/2026-05-ssh-iac-cloud-breadth.md §"Team / subagent
management". Parallelise schema + adapter work within a thread;
serialise threads.

Definition of done per thread:
  - go vet ./... clean
  - go test ./... -race green (38+ packages)
  - npm run lint + npm run build clean
  - integration tests gated by env var, build clean, skip without it
  - Draft PR opened with full Test Plan
  - README + docs/UAT.md + docs/architecture.md + CHANGELOG.md updated
  - backlog.md updated if applicable

Hard rules (DO NOT violate):
  - No InsecureIgnoreHostKey in SSH adapter; known_hosts TOFU only
  - No cloud credentials in env vars when COOKER_ENV=production
    unless wrapped through secrets.Manager (Config.Validate enforces)
  - One PR per thread; never bundle threads
  - Run cooker-security review BEFORE landing Thread 1 (SSH) and
    BEFORE landing Thread 2 (IaC credential surface)
  - CI infrastructure is environmental-failing on this repo; verify
    locally and skip CI-failure rabbit holes (see PR #91 history)
  - Don't reintroduce Allow-Credentials: true on CORS
  - Don't bind-mount /var/run/docker.sock anywhere (Cooker-side or
    on the SSH remote)

Start by reading:
  docs/plans/2026-05-ssh-iac-cloud-breadth.md   (the full plan)
  CLAUDE.md                                     (project conventions)
  docs/design.md §11                            ("adding a feature")
  backend/internal/deploytarget/ecs/ecs.go      (closest existing
                                                 adapter to learn the
                                                 Target interface shape)
  backend/internal/handler/host.go              (Host CRUD pattern)

Then spawn cooker-feature-dev with this prompt and let it orchestrate.

When all three PRs are open as drafts, summarise in one final
message with the three PR URLs and the verification status of each.
```
