# Smallest viable cache-plumb for P#4 (2026-05)

> Status: **read-only research deliverable**. No code changes.
> Branch: `claude/w2-research-cache-plumb`.
> References: `docs/dag-adaptation-2026.md` §7.4, §DR-1; `docs/audits/W11-user-journeys.md` §ML step 4.

---

## 1. Current state — `builder.Request` shape today

`backend/internal/builder/builder.go` defines the interface contract:

```go
type Request struct {
    ContextDir string
    Dockerfile string
    Tags       []string
    BuildArgs  map[string]string
    Platforms  []string
    LogWriter  io.Writer
}
```

No cache field exists. `StageConfig` in `backend/internal/model/pipeline.go:45-90` likewise has no
`Cache` field. Every build is a cold start: `kaniko.go:161-176` constructs the executor args without
any `--cache` flag; `buildkit.go:63-74` constructs `SolveOpt` without `CacheImports` or
`CacheExports` (the comment at lines 14-19 explicitly defers these). This is the finding cited by
`dag-performance.md` §1 and confirmed as the highest-ROI change by W11 §ML step 4: the ML engineer's
47-minute cold build drops to ~8 minutes on the second run once layer caching is enabled.

---

## 2. `CacheSpec` model

Per `dag-adaptation-2026.md` §7.4, the proposed struct is:

```go
// CacheSpec controls build-layer caching for a Build stage.
// An absent or zero-value CacheSpec (Mode == "") is equivalent to Mode == "disabled"
// and preserves today's no-cache behaviour exactly.
type CacheSpec struct {
    // Mode selects the cache backend.
    //   "registry" — read/write layers to a registry path (Kaniko + BuildKit).
    //   "oci"      — OCI-layout cache; BuildKit uses oci-mediatypes=true; Kaniko uses same --cache-repo flag.
    //   "disabled" — explicit no-op; same as absent.
    Mode string `json:"mode,omitempty"`

    // Ref is the fully-qualified registry reference used as the cache store,
    // e.g. "ghcr.io/org/app:cache". Required when Mode == "registry" or "oci".
    Ref string `json:"ref,omitempty"`

    // Inline enables BuildKit's inline-cache export (embeds cache metadata inside the
    // pushed image manifest). Ignored by Kaniko and Buildah. Default true when Mode != "".
    Inline bool `json:"inline,omitempty"`
}
```

`StageConfig` gains `Cache CacheSpec \`json:"cache,omitempty"\`` alongside the existing Build fields
(after `Platforms`, before the Test block).

`builder.Request` gains `Cache CacheSpec` as a new field. No interface method signature changes —
`Builder.Build(ctx, req Request)` stays identical; adapters that don't implement caching simply ignore
`req.Cache`.

---

## 3. Per-adapter changes

### 3a. Kaniko (`backend/internal/builder/kaniko.go`)

Change site: `buildJob()`, the args slice constructed at lines 161-176.

```go
// After the existing --destination and --build-arg flags:
if req.Cache.Mode == "registry" || req.Cache.Mode == "oci" {
    args = append(args, "--cache=true")
    if req.Cache.Ref != "" {
        args = append(args, "--cache-repo="+req.Cache.Ref)
    }
}
```

No structural changes to the Job spec — the flags are passed to the existing `Args` field of the
Kaniko container. The `--cache-repo` value must point at a registry the Kaniko pod's service account
can authenticate to; credential wiring is unchanged (operators already mount registry secrets via the
`KanikoConfig.ServiceAccount`). For `"oci"` mode, Kaniko accepts the same `--cache-repo` flag with
an OCI-compatible registry; no separate path.

`KanikoConfig` gains no new fields — the cache ref comes from `Request.Cache.Ref` at call time.

### 3b. BuildKit (`backend/internal/builder/buildkit.go`)

Change site: the `solveOpt` literal assembled at lines 63-74.

```go
if req.Cache.Mode == "registry" || req.Cache.Mode == "oci" {
    attrs := map[string]string{"ref": req.Cache.Ref}
    if req.Cache.Mode == "oci" {
        attrs["oci-mediatypes"] = "true"
    }
    if req.Cache.Inline {
        // inline cache embeds metadata in the pushed image manifest
        solveOpt.Exports[0].Attrs["push"] = "true"
        attrs["mode"] = "max"
    }
    solveOpt.CacheImports = []client.CacheOptionsEntry{{
        Type:  "registry",
        Attrs: map[string]string{"ref": req.Cache.Ref},
    }}
    solveOpt.CacheExports = []client.CacheOptionsEntry{{
        Type:  "registry",
        Attrs: attrs,
    }}
}
```

`BuildKit.Build` today opens a new `client.Client` per call; no connection-pool change is needed.

### 3c. Buildah (`backend/internal/builder/buildah.go`)

Change site: `buildahScript` (the static shell script at line 145) and `buildJob()` env vars.

The Buildah adapter uses a shell script rather than direct CLI args. Cache support requires adding
`--layers` to `buildah bud` (enables layer caching within the run) and, for registry caching,
`--cache-to` and `--cache-from` flags (available in Buildah >= 1.28):

```bash
# In buildahScript, after the bud invocation gains --layers when CACHE_MODE is set:
buildah bud --storage-driver="$STORAGE_DRIVER" -f "$DOCKERFILE" \
  ${CACHE_FLAGS} -t cooker-build:current "$CONTEXT_DIR" "$@"
```

The `buildJob()` method adds an env var `CACHE_FLAGS` derived from `req.Cache`:

```go
cacheFlags := ""
if req.Cache.Mode == "registry" || req.Cache.Mode == "oci" {
    cacheFlags = fmt.Sprintf(
        "--layers --cache-from=registry://%s --cache-to=registry://%s",
        req.Cache.Ref, req.Cache.Ref,
    )
}
env = append(env, corev1.EnvVar{Name: "CACHE_FLAGS", Value: cacheFlags})
```

Shell injection risk: `CACHE_FLAGS` is expanded inside a double-quoted `${CACHE_FLAGS}` in the
script. Because `req.Cache.Ref` is validated as a registry reference (no whitespace or shell
metacharacters) by the service layer before reaching the adapter, this is safe. A pre-validation
helper (`validateCacheRef`) should reject refs containing spaces, semicolons, or backticks.

### 3d. DockerSock (`backend/internal/builder/docker.go`)

DockerSock is dev/single-node only. Classic `docker build` does not support content-addressable
registry-based layer caching (`--cache-from` accepts a local image reference, not a remote registry
push target). Per `dag-adaptation-2026.md` §DR-1, this adapter is documented as unsupported for
caching:

```go
// In DockerSock.Build, before constructing args:
if req.Cache.Mode != "" && req.Cache.Mode != "disabled" {
    // DockerSock does not support registry-based layer caching.
    // Cache spec is silently ignored; the build proceeds as a cold start.
    // Use COOKER_BUILDER=kaniko or COOKER_BUILDER=buildkit for cache support.
}
```

Return value: `Result` with no cache-related fields (Result has none today and gains none in this
primitive). No error returned — the build still succeeds; the cache simply has no effect.

---

## 4. Test strategy

All adapter tests mock Job submission or the BuildKit client — no real daemon required.

**Kaniko** (`backend/internal/builder/kaniko_test.go`): call `k.buildJob(req)` with
`req.Cache.Mode = "registry"` and assert `job.Spec.Template.Spec.Containers[0].Args` contains both
`"--cache=true"` and `"--cache-repo=ghcr.io/org/app:cache"`. Add a parallel test with
`Mode = "disabled"` (or `""`) asserting neither flag appears.

**BuildKit** (`backend/internal/builder/buildkit_test.go`): intercept `SolveOpt` by extracting the
`solveOpt` construction into a helper `(b *BuildKit) solveOptions(req Request) client.SolveOpt`;
unit-test the helper directly. Assert `CacheImports[0].Attrs["ref"]` and
`CacheExports[0].Attrs["ref"]` equal `req.Cache.Ref` when `Mode == "registry"`. Assert both slices
are nil when `Mode == ""`.

**Buildah** (`backend/internal/builder/buildah_test.go`): call `b.buildJob(req)` with
`req.Cache.Mode = "registry"` and assert the `CACHE_FLAGS` env var contains `--layers` and
`--cache-from=registry://<ref>`.

**DockerSock** (`backend/internal/builder/docker_test.go` or `builder_test.go`): pass
`req.Cache.Mode = "registry"` and assert the command args do _not_ contain any `--cache` flag; build
returns non-error.

**Service integration** (`backend/internal/service/executor_test.go`): stage `Config.Cache.Mode =
"registry"` flows into `builder.Request.Cache.Mode`. Mock `Builder` captures the `Request` and
asserts the field is set.

---

## 5. Helm values to surface

Under the existing `builder:` block in `deploy/helm/cooker/values.yaml`, add a new `cache:` stanza
applicable to both `kaniko` and `buildkit` builder kinds:

```yaml
builder:
  # ... existing kind / kaniko / buildah blocks unchanged ...

  cache:
    # enabled controls whether CacheSpec is populated from chart values.
    # When false (default), all builds are cold-start regardless of per-stage config.
    enabled: false

    # ref is the fully-qualified registry reference used as the cache store.
    # Example: "ghcr.io/org/app-build-cache:latest"
    # The build pod's service account must have push/pull access to this ref.
    ref: ""

    # maxLayers caps the number of cached layers Kaniko writes per build
    # (--cache-ttl is the Kaniko control; BuildKit has no direct equivalent).
    # 0 means no cap (Kaniko default).
    maxLayers: 0
```

Note: `maxLayers` maps to Kaniko's `--cache-ttl` duration, not a layer count directly. The Kaniko
flag controls time-to-live for cached layers, not a numeric cap. Naming `maxLayers` in Helm is
user-facing convenience; the Go config layer maps it to `--cache-ttl=<N>h`. BuildKit has no
equivalent knob; the field is a no-op for BuildKit and Buildah. This discrepancy is worth documenting
in the forthcoming `docs/build-cache.md`.

The environment-level `cacheRepo` default referenced in `dag-adaptation-2026.md` §7.4 ("defaults from
the Environment's `cacheRepo`") is not yet a model field; its addition is an open question (see §6).

---

## 6. Open questions for the P#4 PR

1. **`CacheSpec.Inline` default.** §7.4 says `Inline: true` when `Mode != ""`. But enabling inline
   cache silently changes the pushed image manifest to embed cache metadata, which can surprise
   operators inspecting manifests. Should the default be `false` and require opt-in?

2. **`maxLayers` vs `--cache-ttl` naming.** Kaniko's cache expiry is time-based
   (`--cache-ttl=336h` is the default 14-day TTL), not layer-count-based. The §7.4 field
   `Layers int` (max layers) doesn't have a clean Kaniko mapping. Either rename to `TTL` or accept
   the semantic mismatch and document it.

3. **Environment-level `cacheRepo` default.** §7.4 mentions a per-environment `cacheRepo` field that
   pre-populates the per-stage `CacheSpec.Ref`. This field doesn't exist in
   `backend/internal/model/environment.go` today. Adding it requires a Postgres migration and a
   store/handler/service change — outside the backend-adapters scope, needs coordination with
   `cooker-backend-data`.

4. **Credential wiring for the cache registry.** The cache ref may point at a different registry than
   the push target. Kaniko picks up credentials from the pod's mounted `config.json`; BuildKit
   uses its own credential helper. The P#4 PR must document (in `docs/build-cache.md`) that operators
   are responsible for ensuring the build pod SA has credentials for both the destination registry
   and the cache registry. No new Cooker-level credential mechanism is implied by §7.4.

5. **`"oci"` mode vs `"registry"` mode — Buildah divergence.** Buildah's `--cache-from` / `--cache-to`
   flags accept `registry://` and `oci://` prefixes independently; the proposed single `Mode` field
   may need a fourth value (`"oci-local"`) for Buildah's local OCI layout. Or Buildah can be
   limited to `"registry"` only in v1 of P#4.

6. **Cache warming on first run.** The W11 ML walkthrough step 5 records a 47-minute cold build. On
   the _first_ run after enabling cache, Kaniko writes the cache to the registry but still runs cold
   end-to-end. The UI should surface "cache miss — first run" so the ML engineer knows the 47-minute
   duration is expected. No model change needed; the LogWriter stream already carries Kaniko's layer
   messages — the frontend just needs to parse "pushed layer" vs "cached layer" counts.

7. **Primitive ordering dependency.** §7 notes that P#3 (Outputs) is recommended before P#4 because
   `CacheSpec.Ref` could interpolate `${stages.build.digest}` for content-addressed cache keys. If
   P#4 ships without P#3, `Ref` must be a static string — usable but less precise. The P#4 PR
   should document this limitation explicitly rather than blocking on P#3.

---

*Cap: 188 lines of body (within the 200-line target).*
*Source citations: `dag-adaptation-2026.md` §7.4, §DR-1; `W11-user-journeys.md` §ML step 4.*
