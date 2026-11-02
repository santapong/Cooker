# Registry access

Candidate documentation checked 3 October 2026. Scoped access and its UI are
locally verified on frozen inputs; genuine private-registry/runtime acceptance
and owner rollout remain open. The current localhost4644 owner build does not
contain this slice. The [full plan](../../plans/2026-10-02-consolidated-feature-roadmap.md)
tracks the remaining gates.

Cooker uses an OCI registry to publish built images and pull images for supported
runtimes. Registry credentials are private authentication input. They are not
build arguments, application environment values or a consequence of matching a
hostname.

## Administrator: connect and grant

1. Open **Registry** and add the connection’s name, endpoint, optional username
   and password/token. The password is write-only; the page reports whether
   credentials are stored.
2. Save the App or native pipeline first so it has a stable ID.
3. Expand the connection’s grants. Choose the exact Apps and native pipelines
   permitted to use it, then save the grant lists.
4. Use **Revoke all access** to save explicit empty grants when access must stop.

Imported Vercel apps/flows cannot receive executable registry grants. Renaming an
App does not change its identity. A partial or unavailable target list is labelled;
existing unlisted grants are retained for review instead of silently erased.
Registry mutations require the existing administrator/MFA policy. A concurrent
edit returns409: reload the current version and review before trying again.

## Operator: select access for a deployment

For an App, open its **Registry access** panel and choose granted connections,
then **Save registry selections**. The saved build plan owns that selection;
derived deployment graphs retain App scope.

For a native pipeline, select connections under **Registry access for this step**
in each stage’s inspector, then save the pipeline. Build, Push and runtime Pull
can have distinct selections. The server validates each stage’s exact scope.

Empty selection means anonymous access. Unavailable, unverified or revoked IDs
stay visible and cannot authorize credentials. Remove them explicitly or select
**Use anonymous access**. A stale App draft is retained on conflict until an
explicit reload; polling does not silently replace its version.

## Runtime and credential behavior

Metadata-only checks run before source and private environment inspection. Before
admission, Cooker pins selected connection/configuration generations and supported
credential versions. Registry and database credential rows are fenced in the same
transaction as the accepted job and receipt. Workers check grants and evidence
again. Revoking access stops a fresh worker effect; an existing receipt remains
available for inspection and retry recovery.

BuildKit session authentication, Crane publication and Docker/Compose/test pulls
use their accepted request scope or explicit anonymous access. They do not borrow
the host’s Docker login or an unrelated same-host connection. The current scoped
deployment transports are Docker and host-context Compose. Kubernetes/Helm/cloud
pull transports need separate adapters; connected authored source deployment
currently supports Docker only.

Private Settings credential environments are hidden from general environment
inventory and cannot become App/native runtime bindings. Rotation does not copy
plaintext into jobs, run receipts or browser responses. Older external credentials
without immutable references require explicit replacement; there is no silent
migration to the latest value.

An endpoint path identifies configuration, not a repository ACL. Registry-side
token permissions still govern allowed repositories. Configuring an ECR or Google
registry credential is separate from AWS/GCP service management and cloud IAM;
those ecosystems have their own roadmap acceptance.

## Developer interfaces

| Operation | Interface |
| --- | --- |
| Read nonsecret connection metadata | `GET /api/v1/settings/registries` |
| Create connection | `POST /api/v1/settings/registries` |
| Update grants | `PATCH /api/v1/settings/registries/:id/grants` with `version`, `allowedAppIds`, `allowedPipelineIds` |
| Select App connections | App `buildPlan.registryConnectionIds` on its versioned PUT |
| Select native stage connections | Stage `config.registryConnectionIds` on the versioned pipeline PUT |

New grants default empty. A known connection version with omitted grant arrays
means empty grants; an older/malformed response without version evidence remains
unverified. Supported selections are bounded to32 unique IDs. Full authority,
legacy compatibility, ephemeral auth files and verification limits are documented
in [Registry authority](../../backend/registry-authority.md).
