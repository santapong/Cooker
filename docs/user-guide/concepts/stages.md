# Stages

A stage is one node in the pipeline graph. Each stage has a **type** that determines what runs. Cooker ships seven types today, enumerated in `model.StageType` (`backend/internal/model/pipeline.go:24-32`):

| Type constant | UI label | What it does |
|---|---|---|
| `StageTypeBuild` | Build | Build an OCI image from a Dockerfile or compose file. |
| `StageTypeTest` | Test | Run a container against the just-built image, fail on non-zero exit. |
| `StageTypePush` | Push | Push a built image to an OCI registry. |
| `StageTypeDeploy` | Deploy | Apply a Kubernetes manifest (raw YAML or Helm chart). |
| `StageTypeApproval` | Approval | Pause for a human approver. Resumes on `POST /api/v1/pipelines/:id/runs/:runId/approve`. |
| `StageTypeCustom` | Custom | Run an arbitrary shell script inside a container image. |
| `StageTypeGitOpsCommit` | GitOps commit | Commit a rendered manifest to a Git repo. |

If you add a new stage type, you must update both the Go enum and the React Flow node registry — see [`docs/design.md` §11](../../design.md#11-adding-a-new-feature--checklist).

## Common config

Every stage has these fields (`model.Stage`):

| Field | Required | Purpose |
|---|---|---|
| `id` | yes | Stable identifier; used by edges and the WebSocket log channel. |
| `name` | yes | Human label. Shown on the node. |
| `type` | yes | One of the constants above. |
| `config` | type-dependent | The `StageConfig` struct. |
| `environmentId` | no | Which [Environment](environments.md) this stage belongs to (drives variable / secret injection). |
| `position` | yes | `{x, y}` for the editor. Auto-assigned on first drop. |

`StageConfig` is a union — fields are interpreted per type. See `model.StageConfig` at `backend/internal/model/pipeline.go:45-90`.

## Build (`StageTypeBuild`)

Builds a container image. Config:

| Field | Purpose |
|---|---|
| `dockerfile` | Path to the Dockerfile (default: `Dockerfile`). |
| `context` | Build context path (default: repo root). |
| `buildArgs` | Map injected as `--build-arg`. |
| `tags` | Image tags. The first one is the deploy reference. |
| `platforms` | Multi-arch list, e.g. `["linux/amd64", "linux/arm64"]`. Produces an OCI Image Index. |

The actual builder is selected by `COOKER_BUILDER` (see [Docker builds](../operations/docker-builds.md)).

## Test (`StageTypeTest`)

Runs a container against the just-built image. Config:

| Field | Purpose |
|---|---|
| `image` | Image to run. Often a `${BUILD.image}` placeholder referring to the previous Build stage's output. |
| `command` | Override the image's entrypoint. |

Non-zero exit fails the stage.

> **Partial.** `StageTypeTest` today shells out via the configured Pusher's host or relies on the upstream Docker daemon. The `clientgo` deployer does not yet run Test stages — they fall through to a `kubectl run`. Acceptable for UAT; production teams typically run tests in their own CI before pushing to Cooker.

## Push (`StageTypePush`)

Pushes a built image to a registry. Config:

| Field | Purpose |
|---|---|
| `registry` | Registry URL (e.g. `ghcr.io/org`). Falls back to `COOKER_REGISTRY`. |
| `repository` | Image name within the registry. |
| `tags` | Tag list — usually inherited from the upstream Build stage. |

Credentials are sourced from `internal/settings` `RegistryConfig` entries today (`POST /api/v1/settings/registries`). The UX for this is rudimentary; tracked in roadmap `A4`.

## Deploy (`StageTypeDeploy`)

Applies workloads to Kubernetes. Config:

| Field | Purpose |
|---|---|
| `namespace` | Target namespace. |
| `manifestPath` | Path to a raw `.yaml` in the repo, applied with `kubectl apply -f` (or server-side apply when `COOKER_DEPLOYER=clientgo`). |
| `helmChart` | Path to a chart directory; uses `helm template` then applies. |
| `helmValues` | Inline values map for the chart. |

Use one of `manifestPath` or `helmChart`, not both. The deploy target itself is configured per-environment (see [Environments](environments.md)).

> **Partial.** Helm support in the Deploy stage is `helm template`-shaped — Cooker renders and applies the manifests but does not track Helm release state. For a real Helm release lifecycle, use a Custom stage with `helm install --atomic`.

## Approval (`StageTypeApproval`)

Pauses the run. The stage transitions to `awaiting_approval`. Resume via:

```bash
POST /api/v1/pipelines/:id/runs/:runId/approve
```

The endpoint enforces `approver` or `admin` role. The approving user's identity is recorded in `EnvironmentStatus.ApprovedBy`.

Combine with environment promotion (`PromotionPolicy.Strategy = "manual"`) for change-controlled releases.

## Custom (`StageTypeCustom`)

The escape hatch. Config:

| Field | Purpose |
|---|---|
| `image` | Image the script runs inside. |
| `script` | Shell script body. Runs as the image's entrypoint. |
| `command` | Optional override for `script`. |
| `timeout` | Stage timeout (e.g. `15m`). Defaults to the cluster-wide `COOKER_RUN_DEADLINE`. |
| `retries` | Stage retries on failure. |

Use Custom for: data migrations, smoke tests post-deploy, ad-hoc CLI calls (`aws s3 sync`, `gcloud …`), database seeding, etc.

> **Partial.** The Custom stage's UI today is "image + script in a textarea." No syntax highlighting, no template variable picker. Tracked in roadmap `B3`.

## GitOpsCommit (`StageTypeGitOpsCommit`)

Commits a rendered manifest to a Git repo. Useful for ArgoCD / Flux-style GitOps where Cooker is the writer.

| Field | Purpose |
|---|---|
| `gitopsRepo` | Git remote, e.g. `git@github.com:org/gitops.git`. |
| `gitopsBranch` | Default `main`. |
| `gitopsPath` | Path inside the repo to update. |
| `gitopsMessage` | Commit message template (supports `${IMAGE}` etc.). |
| `gitopsContent` | The manifest body to commit. |

Implementation lives at `backend/internal/gitops/gogit.go` (via `go-git/v5`).

> **Partial.** Today only commits + push are wired; merge-conflict resolution is "fail and let the operator retry." For repos with multiple writers, this is acceptable but not bulletproof.

## Stage config: env and secrets

Two fields on `StageConfig` are universal:

- `env` — string-keyed map merged into the stage's runtime env (highest precedence over `Pipeline.Variables` and `Environment.PlainVars`).
- `secretRefs` — list of secret names from the stage's `EnvironmentID`. The executor decrypts and injects them just before run; the stage never sees ciphertext.

See [Secrets](../guides/secrets.md) for the storage backends.

## Adding a stage type

This requires Go code, not just config:

1. Add the constant to `model.StageType` (`backend/internal/model/pipeline.go`).
2. Add the executor switch case in `internal/service/executor.go`.
3. Add a React Flow node component under `frontend/src/components/`.
4. Register it in the toolbar.

See [`docs/design.md` §11](../../design.md#11-adding-a-new-feature--checklist) for the contributor checklist.

## Cross-references

- **[Pipelines](pipelines.md)** — DAG semantics.
- **[Runs](runs.md)** — what happens once a stage starts executing.
- **[Docker builds](../operations/docker-builds.md)** — which builder runs Build stages.
- **[Promotions](../guides/promotions.md)** — combining Approval stages with environment promotion.
