<!-- DRAFT dev.to article -->

# OCI Compliance for People Who Haven't Read the Spec

*Tags: docker, kubernetes, devops, opensource*

---

When Cooker describes itself as "OCI-compliant," that phrase is doing real work — it is not just a way of saying "builds Docker images." This article explains what the three OCI specifications actually say, how Cooker implements them, and why the distinction between "builds Docker images" and "builds OCI images" matters in practice.

Background: OCI stands for Open Container Initiative. It was formed in 2015 to standardise the container image format and runtime after Docker donated its image specification. The three specs are maintained at `opencontainers.org`. They are short, readable documents — the image spec is about 20 pages. Most people in the container space have heard of them without having read them, which is fine until you're debugging a push failure or a multi-arch build.

---

## Three Specs, Three Jobs

**1. image-spec (v1.1):** Defines what a container image is. Specifically: a Manifest (a JSON document describing the image's config and layers), a Config (another JSON document describing how to run the container), and one or more Layers (gzipped tarballs of filesystem changes). Each component is identified by a content-addressable digest: `sha256:<hex>`.

**2. runtime-spec (v1.2):** Defines what a container runtime must do when it receives an unpacked image. The rootfs, the process environment, namespaces, cgroups — all of it. Docker Engine, containerd, and CRI-O all implement this spec. Cooker surfaces runtime-spec concepts (mounts, env vars, namespace isolation) in the pipeline stage configuration UI; the actual `config.json` generation and execution is delegated to Docker Engine or to the in-cluster builder (Kaniko or Buildah).

**3. distribution-spec (v1.1):** Defines how images are stored in and retrieved from registries. The API endpoints: `GET /v2/<name>/manifests/<reference>`, `PUT /v2/<name>/manifests/<reference>`, `GET /v2/<name>/tags/list`, and the referrers API at `GET /v2/<name>/referrers/<digest>`. Any registry that implements these endpoints can serve OCI images — Docker Hub, GHCR, ECR, GCR, Quay, a self-hosted Harbor instance, a Zot registry on a Raspberry Pi.

---

## How Cooker Uses These Specs

**image-spec:** The types in `internal/oci/manifest.go` mirror the OCI image-spec v1.1 structs directly:

```go
type Manifest struct {
    SchemaVersion int               `json:"schemaVersion"` // Must be 2
    MediaType     string            `json:"mediaType"`     // application/vnd.oci.image.manifest.v1+json
    Config        Descriptor        `json:"config"`
    Layers        []Descriptor      `json:"layers"`
    Annotations   map[string]string `json:"annotations,omitempty"`
}
```

The `Descriptor` type carries the three fields the spec requires for every content-addressable component: `mediaType`, `digest`, and `size`. Nothing more. When Cooker pushes an image, it constructs a `Manifest` with these fields, serialises it, and sends it to the registry. When it pulls an image digest to verify a push, it validates the returned manifest against `ValidateManifest` — which checks `schemaVersion == 2`, validates that the config digest is non-empty, and confirms at least one layer exists.

For multi-arch images, the spec adds an Image Index: a Manifest that contains a list of per-platform Manifests. `internal/oci/manifest.go` has an `Index` type for this. `ValidateIndex` checks that each entry in the index has a non-empty digest and a `Platform` field (architecture + OS). Without the Platform field, a multi-arch image is technically valid but unusable — the runtime can't decide which manifest to pull.

**Media types:** The full list of valid media types lives in `internal/oci/mediatype.go`. The important ones:

```go
MediaTypeImageManifest = "application/vnd.oci.image.manifest.v1+json"
MediaTypeImageIndex    = "application/vnd.oci.image.index.v1+json"
MediaTypeImageConfig   = "application/vnd.oci.image.config.v1+json"
MediaTypeImageLayerGzip = "application/vnd.oci.image.layer.v1.tar+gzip"
```

The Docker compatibility types are also defined: `application/vnd.docker.distribution.manifest.v2+json` and its friends. `IsOCIManifest` returns true for both the OCI type and the Docker manifest v2 type — because in practice, older registries and older clients still produce Docker manifest v2 even when serving what is functionally an OCI image.

**distribution-spec:** The push path uses `crane` from [go-containerregistry](https://github.com/google/go-containerregistry). This library implements the distribution-spec API — it handles chunked blob uploads, retries, and the referrers API for supply-chain metadata. Cooker does not implement the distribution-spec itself; it delegates to a well-tested library that does.

---

## The Conformance Workflow

In `.github/workflows/oci-conformance.yml`, Cooker runs the upstream OCI distribution-spec conformance suite against a test registry on every CI run. The conformance suite is the official test harness maintained by the OCI community — it spins up a test registry, pushes and pulls images via the library under test, and validates that every API response matches the spec.

If you look at the README badges, there is an `OCI Conformance` badge. When it is green, the distribution-spec conformance suite passed. When it fails, a push operation in Cooker's pipeline would fail against a registry that strictly implements the distribution-spec. This is the difference between "it works against Docker Hub" and "it works against any spec-compliant registry."

Why does this matter? Because the space of registries operators actually run is wider than Docker Hub. ECR has known spec deviations in its referrers implementation. Harbor's versions before 2.6 did not implement the referrers API at all. GCR has a different chunked upload timeout. Conformance testing gives you a baseline guarantee that the push path works before you discover these edge cases in production at 02:00.

---

## What "OCI-Compliant Builder" Means in Practice

Not all builders produce OCI images. The docker socket builder — which shells out to `docker build` on the host — produces Docker manifest v2 images by default. These are compatible with most registries and runtimes. They are not OCI images.

Kaniko (the default in production) and Buildah produce OCI image manifests natively. The output of a Kaniko build is an image whose manifest is `application/vnd.oci.image.manifest.v1+json` with layer media types in the `application/vnd.oci.image.layer.v1.tar+gzip` family. This matters when you are using supply-chain tooling (cosign, Syft, Grype) that operates on the referrers API — these tools attach signatures and SBOMs as referrers to the image digest. A Docker manifest v2 image can carry referrers, but the referrers API path (`GET /v2/<name>/referrers/<digest>`) is only defined in the OCI distribution-spec, and older Docker-format images have historically been inconsistent about this.

The practical upshot: if you are running a supply-chain signing step in your pipeline (cosign sign, Syft generate, Grype scan), use Kaniko or Buildah, not the docker socket builder. The OCI-native output is cleaner for tools that read the referrers API.

---

## What Cooker Does Not Do

**It does not implement a registry.** Cooker pushes to registries; it does not serve them. If you want a self-hosted registry, Zot, Harbor, and Distribution (the reference implementation) are the right choices.

**It does not implement the runtime-spec at the container level.** That is Docker Engine's or containerd's job. Cooker tells the runtime what to run; the runtime does the cgroup and namespace plumbing.

**Multi-arch build is builder-dependent.** Kaniko can produce a multi-arch image if you build for multiple platforms and assemble an Image Index. Buildah has similar capability. The Cooker UI surfaces the platform fields but the actual multi-platform build execution depends on the builder you select and your cluster's node architecture.

---

## The Short Version

OCI compliance means: the image format is defined by the image-spec, the push/pull API is defined by the distribution-spec, and both are tested continuously against the official conformance suite. Using a spec-compliant image format means your images work with any compliant registry, any compliant runtime, and any supply-chain tooling that reads the referrers API.

That is not marketing. It is a property of the output format that matters when you are debugging a push failure at 02:00 and you need to know exactly what the registry should accept.

Try it: `docker compose up` — repo at github.com/santapong/Cooker
