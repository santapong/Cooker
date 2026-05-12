# Docker / OCI builds

Cooker builds container images via pluggable builders. The active builder is selected at boot by `COOKER_BUILDER`:

| Value | Implementation | Security | Production-ready? |
|---|---|---|---|
| `docker` | Shells out to the host Docker daemon. | RCE-to-host risk via the bind-mounted socket. | NO — refused by `Config.Validate()` in production. |
| `kaniko` | Submits a one-shot `batch/v1.Job` to K8s running the Kaniko executor. | No host docker.sock. Namespace-scoped RBAC. | YES — chart default. |
| `buildah` | Same shape as Kaniko, full Dockerfile parity (heredocs, `--mount=type=cache`). | Needs PSA `baseline` or custom (CAP_SETUID + CAP_SETGID). | YES. |
| `buildkit` | gRPC against an external `buildkitd`. | Depends on the buildkitd deployment. | **Stub** — not yet wired. |
| `noop` | Accepts builds without doing anything. | n/a | Test only. |

## When to pick which

- **`kaniko` *(chart default)*.** Most operators. In-cluster Job, no host paths, mature.
- **`buildah`.** When your Dockerfile uses features Kaniko silently ignores (heredocs `<<EOF`, `RUN --mount=type=cache`). Trade-off: needs a more permissive PSA profile in the build namespace.
- **`docker`.** Dev only. Convenient on single-node test clusters; gives the Cooker container root-equivalent access to the host's Docker. **An RCE in Cooker -> host takeover.** `Config.Validate()` refuses this in production.
- **`buildkit`.** **Stub.** Listed for completeness; not wired in this release. Roadmap `A12`.

## kaniko setup

The Helm chart provisions the RBAC objects when `builder.kind=kaniko`. You need to provide:

- A `PersistentVolumeClaim` shared between the Cooker pod and the Kaniko Job. The Cooker pod stages the cloned source there; the Kaniko Job reads it as the build context.

```yaml
# values.yaml
builder:
  kind: kaniko
  kaniko:
    image: gcr.io/kaniko-project/executor:latest
    serviceAccount: ""       # empty = the namespace default
    contextPVC: cooker-builds
```

PVC requirements: `ReadWriteMany` (so Cooker and the Job mount it simultaneously) — typically NFS, CephFS, or EFS.

`Config.Validate()` will refuse to start with `kaniko` if `contextPVC` is empty (production); in dev it falls back to an `emptyDir` which won't see Cooker's source.

### Build Job RBAC

Cooker's ServiceAccount needs `Job` + `Pod` create/get/watch/delete in the build namespace. The chart ships `templates/rbac.yaml` rendering this when `builder.kind=kaniko` or `buildah`. Default `builder.namespace=cooker`.

For namespace-scoped RBAC (not cluster-wide), set:

```yaml
rbac:
  clusterWide: false
  namespaces: [cooker]      # the build namespace
```

## buildah setup

Almost identical to Kaniko:

```yaml
builder:
  kind: buildah
  buildah:
    image: quay.io/buildah/stable:latest
    serviceAccount: ""
    contextPVC: cooker-builds
    storageDriver: vfs       # "vfs" (no kernel mods) or "overlay" (faster, needs fuse-overlayfs)
```

### PSA profile

Rootless Buildah needs `CAP_SETUID` and `CAP_SETGID` for its user-namespace setup. The build namespace must be:

- **PSA `baseline`** (works), or
- **A custom PSA profile** that permits both capabilities, or
- **PSA `privileged`** (works but is overkill).

PSA `restricted` drops these capabilities and Buildah will fail with `setuid: operation not permitted`.

Label the build namespace:

```sh
kubectl label namespace cooker pod-security.kubernetes.io/enforce=baseline
```

### Storage driver

- **`vfs`** — slowest, but works on any kernel and without extra packages. Choose this if you're not sure.
- **`overlay`** — faster, but the build node needs `fuse-overlayfs` installed.

For most teams, `vfs` is fine. The build time difference matters only for very large Dockerfiles.

## docker setup

For dev only. Mount the host's docker socket:

```yaml
builder:
  kind: docker
extraVolumes:
  - name: docker-socket
    hostPath:
      path: /var/run/docker.sock
extraVolumeMounts:
  - name: docker-socket
    mountPath: /var/run/docker.sock
```

The chart automatically drops this volume + mount when `builder.kind != docker`, so no leftover host paths.

**Security note.** The Cooker container runs as UID 65532, but having access to the docker socket means it can spawn containers as root on the host. This is RCE-to-host equivalent. The raw `deploy/kubernetes/deployment.yaml` historically had this mount unconditional (`S26-05-04`); use the Helm chart instead, or apply the fix from the [security review](../../audits/2026-05-security-review.md).

## Multi-arch builds

Set `Stage.config.platforms` to a list:

```yaml
type: build
config:
  dockerfile: Dockerfile
  tags: [hello-world:${COMMIT_SHA}]
  platforms:
    - linux/amd64
    - linux/arm64
```

The builder produces an OCI Image Index (manifest list) referencing per-platform manifests. The Push stage pushes the index and all referenced manifests.

> **Builder support varies.** `kaniko` supports multi-arch builds natively. `buildah` does too via `--platform`. `docker` requires `buildx` to be installed in the daemon. `buildkit` (when wired) handles it well.

## Build cache

> **Partial.** No first-class build-cache surface in the UI / config today. Kaniko's `--cache=true --cache-dir=...` and `--cache-repo=...` flags exist but aren't exposed through Cooker's config. Roadmap `A19` / `A20` tracks `--cache-from` / `--cache-to` plumbing (S3-compatible sink and OCI artifact cache). Until those land:
>
> - For Kaniko: shoehorn flags into `BuildPlan.Args` (only some pass through).
> - For Buildah: use a Custom stage that runs `buildah bud --cache-from=...` by hand.
> - For ML workloads (45-minute builds), this is the single biggest pain point — see [W11 ML persona](../../audits/W11-user-journeys.md#persona-4--ai--ml-engineer).

## Resource requirements

Build Jobs run in their own pods. Size based on:

| Factor | Effect |
|---|---|
| Source repo size | Bigger context = more PVC space + more memory for tar/extract. |
| Layer cache hit rate | Cold builds need 2-4x the warm-build CPU. |
| Compiler-heavy stages (Go, Rust, native deps) | CPU-bound; benefit from multi-core nodes. |
| Test stages | Same as test-suite resource needs. |

Default Job resource requests in the chart are conservative; tune `builder.kaniko.resources` / `builder.buildah.resources` for your largest expected build.

## Node selection for build Jobs

> **Known limitation.** The chart does NOT expose `nodeSelector` / `tolerations` for Kaniko / Buildah Job specs. ML teams who want to pin builds AWAY from GPU node pools (so they don't waste a $2/hr GPU on a build job) can't do it via Helm values today. Workaround: a `PodDisruptionBudget` or a node taint that the Cooker namespace's default ServiceAccount cannot tolerate.
>
> Tracked in [W11 ML persona walkthrough](../../audits/W11-user-journeys.md#persona-4--ai--ml-engineer); roadmap `D7` / `D8`.

## OCI conformance

Cooker pushes through `go-containerregistry`, which natively implements the OCI distribution-spec v1.1. The push path is exercised against the upstream conformance suite weekly via `.github/workflows/oci-conformance.yml`. The referrers API is supported for SBOM / signature attachment via the standard endpoints.

## Cross-references

- **[Registries](../guides/registries.md)** — push destinations.
- **[`SECURITY.md` § Image build isolation](../../../SECURITY.md#image-build-isolation)** — the threat model per builder.
- **[Reference: env vars](../reference/env-vars.md#builders-and-pushers)** — `COOKER_BUILDER` / `COOKER_KANIKO_*` / `COOKER_BUILDAH_*`.
