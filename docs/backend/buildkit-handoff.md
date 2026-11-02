# Source build and image publication contract

**3 October candidate update:** per-App/native stage grants, immutable authority and registry-first admission now replace host-only global credential lookup. Read [Registry access](../user-guide/guides/registries.md) and [Registry authority](registry-authority.md). The28September handoff below remains historical build/OCI evidence; genuine source plus scoped private registry/runtime and owner rollout are separate pending gates.

Implementation checkpoint: 28 September 2026. This is the BuildKit/Crane portion of M1, not acceptance of the complete backend roadmap or a production deployment.

## What changes

Source builds can now use an isolated BuildKit daemon and Crane. BuildKit receives the selected checkout as its build context and exports a private OCI image layout. The layout stays on the worker running the current DAG. The Push stage receives an opaque in-memory handle and publishes that exact image or multi-platform index; it does not try to pull a nonexistent image from a remote registry or consult the Docker daemon's image store.

Crane verifies the published digest before returning success. Push outputs include the original target in `ref`, the registry digest in `digest`, and `repository@sha256:…` in `image`. Synthesized deployment stages consume `image`. A Docker Compose service gets a private image override file so parallel service stages do not rewrite the shared reviewed Compose file.

Artifact paths never enter pipeline JSON or user-supplied configuration. Normal completion, failure and cancellation release the run's artifacts. Process termination can leave a `cooker-build-*` temporary directory; operators must apply a worker disk quota and remove orphaned directories only after confirming that the owning run is no longer active. This is a transient execution handoff, not a durable image archive or a cross-worker resume mechanism. An interrupted attempt must reconcile its external outcome before rebuilding.

## Operator configuration

For a local isolated daemon reached through its Unix socket:

```dotenv
COOKER_BUILDER=buildkit
COOKER_PUSHER=crane
COOKER_BUILDKIT_ADDR=unix:///run/buildkit/buildkitd.sock
```

For a separate production build worker, use mutual TLS:

```dotenv
COOKER_BUILDER=buildkit
COOKER_PUSHER=crane
COOKER_BUILDKIT_ADDR=tcp://buildkit.example.internal:1234
COOKER_BUILDKIT_TLS_CA=/run/secrets/buildkit-ca.pem
COOKER_BUILDKIT_TLS_CERT=/run/secrets/buildkit-client.pem
COOKER_BUILDKIT_TLS_KEY=/run/secrets/buildkit-client-key.pem
# Optional when the certificate uses a different DNS identity:
COOKER_BUILDKIT_TLS_SERVER_NAME=buildkit.example.internal
```

The daemon must trust the client certificate and present a server certificate for the selected identity. Production configuration rejects plaintext TCP and mismatched certificate/key settings. It continues to reject Docker socket builder/pusher backends. Source builds also require stage outputs enabled, because digest interpolation is part of the immutable image handoff. Image-only deployments do not depend on the builder.

BuildKit is a trusted execution service, not a place to share the control-plane database credentials. Put it in a separate execution boundary with CPU, memory, disk and network policy. Do not mount the host Docker socket or control-plane environment into builds. TLS authenticates the channel; it does not replace isolation or resource limits. Local Docker runtime deployment and production cluster/provider readiness remain separate capabilities.

## Registry credentials

The server resolves registry credentials from Cooker's existing Settings connections and secret manager. Only the exact requested registry host matches; Docker Hub aliases normalize to the same host. A configured but unreadable password fails the stage, and duplicate host connections are rejected as ambiguous. Resolved passwords are registered with the attempt's log redactor before delivery to an adapter.

Credentials reach BuildKit through its session authentication RPC, and Crane through a request-scoped keychain. They are not written into build arguments, Dockerfiles, run JSON or environment variables. The server does not fall back to an unrelated control-host Docker login. The development Docker CLI builder/pusher retains its existing local Docker credential behavior.

The current connection store is instance-scoped. Per-App credential authorization, external cloud workload identity and immutable credential-version records are broader M1/provider work; this handoff does not claim those are finished. Private base-image authorization is covered at the session contract level; the live fixture below uses `FROM scratch` and does not establish a private-registry build UAT result.

## Verification

- Go build/pusher/config tests passed, including actual HTTP registry publication of a local OCI image and an index through authenticated registry requests, exact digest preservation, missing-artifact failure and rejection of the Docker/OCI mismatch.
- Executor tests passed for artifact transfer, success/failure cleanup, run isolation, separate Compose overlays and late registry-secret redaction registration.
- A disposable BuildKit v0.18.2 daemon and registry executed the actual Executor → BuildKit → private OCI layout → Crane → registry flow for a single-platform image and a linux/amd64 + linux/arm64 index. Both runs were repeated over mutual TLS. The fixture only copies a marker into a scratch image; no external source account, real application deployment, platform emulation or cloud service was exercised.
- The test containers were uniquely named and used only fixture data. No existing service, credential or production registry was modified.

The repeatable live test is `TestBuildKitCraneLiveHandoff` in `backend/internal/service/buildkit_handoff_integration_test.go`, behind the `integration` build tag. It requires operator-created disposable endpoints through `COOKER_TEST_BUILDKIT_ADDR` and `COOKER_TEST_REGISTRY`; optional test TLS variables are `COOKER_TEST_BUILDKIT_TLS_CA`, `_TLS_CERT`, `_TLS_KEY`. Never point this fixture at a production registry. It writes `cooker-handoff:single` and `cooker-handoff:multi` tags.

Pinned SDK behavior was checked against BuildKit v0.18.2 source. OCI export shape follows the [Docker OCI exporter reference](https://docs.docker.com/build/exporters/oci-docker/) and [BuildKit's v0.18.2 documentation](https://github.com/moby/buildkit/blob/v0.18.2/README.md). This is a tested dependency version, not a claim that it is the latest supported or security-patched release.
