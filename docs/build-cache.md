# Build layer cache

Cooker's builders run cold by default: every build re-pulls base layers
and re-executes every Dockerfile step. For iterative workloads (the
W11 ML persona's 47-minute build) a registry-backed layer cache turns
the second build into minutes. This page explains the knobs that
landed with the `CacheSpec` plumbing.

## Where to configure it

| Surface | How | Applies to |
|---|---|---|
| Pipeline editor | Build stage → "Layer cache" section (mode + ref) | That stage only |
| API | `stage.config.cache = {"mode": "registry", "ref": "<image ref>"}` | That stage only |
| App deploys | `COOKER_BUILD_CACHE_REPO=<image ref>` env (chart: `builder.cache.{enabled,ref}`) | Every build stage Cooker synthesizes for "Deploy" clicks and webhooks |

`mode` is one of:

- `registry` — push/pull layer cache through an OCI registry ref.
- `oci` — same, with OCI media types forced (BuildKit).
- `disabled` — explicit off (same effect as omitting the block).

`ref` is a normal image ref used *only* for cache artifacts, e.g.
`registry.example.com/org/app:buildcache`. Use a dedicated repository —
cache layers are content artifacts, not runnable images.

## Per-builder semantics (DR-1)

| Builder | What CacheSpec does |
|---|---|
| **Kaniko** | Appends `--cache=true --cache-repo=<ref>`. Kaniko stores each cached layer as an image under the repo and consults it before executing a Dockerfile step. |
| **Buildah** | Appends `--layers --cache-from=<ref> --cache-to=<ref>` (discrete argv through the hardened `$@` mechanism — see the shell-safety note below). |
| **BuildKit** | Sets `CacheImports`/`CacheExports` of type `registry` on the Solve. `inline: true` additionally exports `mode=max` metadata. Cooker does **not** flip the image exporter to `push=true` — pushing the built image remains the push stage's job. |
| **docker** (socket) | Ignored with a log line. The local daemon cache applies implicitly. |
| **noop** | Ignored. |

## Credentials

The cache ref is pushed/pulled by the **builder's** identity, not
Cooker's:

- Kaniko/Buildah: the Job's ServiceAccount must mount registry
  credentials that allow push to the cache ref (same mechanism as the
  build's `--destination` push).
- BuildKit: buildkitd's registry auth applies.

First build with cache enabled is still cold (it *writes* the cache);
the win starts on the second build.

## Validation and shell safety

`cache.ref` is validated at pipeline save (`validate.CacheSpec`):
strict registry-ref grammar, no whitespace or shell metacharacters.
This matters because the ref reaches builder argv (Buildah's Job
command); the validator is the same class of gate as the T1 Buildah
injection fix.

## Operational notes

- Cache repos grow without bound; registries with retention policies
  (or a periodic `crane delete` sweep) keep them in check. Kaniko has
  no TTL flag for cache entries.
- A shared cache ref across pipelines is safe (content-addressed) but
  noisy; per-app refs (`.../cache/<app>`) give better hit rates and
  cheaper sweeps.
- Multi-replica Cooker needs no coordination — the registry is the
  shared state.
