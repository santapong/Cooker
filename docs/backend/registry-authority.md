# Registry authority and accepted credentials

SEC-04 separates administrator permission from operator selection. A registry
hostname, App destination prefix or saved connection ID alone cannot grant
credentials. Connections default to empty grants. An administrator grants exact
App/Pipeline IDs with `allowedAppIds` / `allowedPipelineIds`; an operator selects
connections in `registryConnectionIds` on an App build plan or authored stage.

The grant endpoint is `PATCH /api/v1/settings/registries/:id/grants`, carrying the
current `version` and both grant lists. Both grant mutation and creation carrying grants require the Settings admin/MFA policy.
A stale version returns 409. The standalone handler does not install the route or
provide authentication itself; server wiring must enforce that policy.

## Admission and execution contract

`RegistryAuthorityService.Prepare(ctx, subject, selectedIDs, stageBindings)` checks
every requested connection before reading provider metadata. The native stage
union must equal the selected set. It pins config generation, canonical endpoint,
host, username and nonsecret credential-version metadata. Connections with the
same host are ambiguous inside one App/stage; separate authored stages can select
different connections explicitly. Names/repositories do not define identity.

`ValidateCurrent(ctx, subject, plan)` checks ownership, grants, scope and current
config generations. `Resolver(ctx, subject, plan, stageID)` clones the accepted
evidence and checks it before and after each credential read. It never lists all
connections or borrows an unrelated same-host connection. External values use
only `GetPinned`; a missing historical version fails. Database credentials keep
an environment generation+creation fence and fail after rotation. No plaintext,
plaintext digest or credential-bearing auth file enters the accepted plan.

An explicit empty `connections` map means anonymous. Missing accepted evidence
is invalid, including old jobs; they require new admission. The deprecated
host-only `RegistryAuthResolver` returns a fixed error without any global read.
PostgreSQL sequence/trigger generations prevent config-version reuse after
deletion/recreation. Memory stores clone grant lists and reject duplicate IDs.

## Portable credential scope and compatibility

New credentials receive a random, durable incarnation UUID in private
`registry:v2:<UUID>` metadata. Their internal environment ID **and name** are
`registry_<UUID hex>`, with the key `password`. This avoids GCP's invalid dotted
names and KeepSave's invalid spaced internal name, and isolates local database
generations per credential incarnation. Registry configuration and provider
account/mount/prefix remain separately bound by the version reference.

Old `registry:<connectionID>` references retain `_registries` plus
`registry_password.<ID>` only for a generation-bound database manager with a
matching environment creation/version fence. Legacy external references fail
with a fixed replacement-required error. No automatic legacy provider migration,
latest lookup or credential copying is performed. KeepSave still requires the
corresponding upstream environment to exist; Cooker does not provision it here.

Failed registry creation attempts delete their newly scoped value best effort;
provider cleanup can fail, so orphan cleanup remains an operator task. Successful
credential writes and provider-side immutable history depend on the configured
backend's permissions and protocol. New external writes must produce a valid
immutable reference before the connection row is accepted.

These are private Settings credential environments. Root integration must reject
them from App/native runtime bindings, hide them from general environment controls,
and prevent their names from being used as aliases. Use
`IsRegistryCredentialEnvironmentID` before lookup and
`IsRegistryCredentialEnvironment` after lookup. The latter checks the name too:
KeepSave resolves by friendly name, so another UUID with the same reserved name
would otherwise read the registry value through runtime environment resolution.

## Adapter boundary

BuildKit supplies an explicit scoped or anonymous credential RPC session. Crane
uses scoped auth or anonymous auth; nil never uses the host Docker keychain.
The legacy explicit `Auth` callback remains caller-owned; accepted executions
must use the accepted resolver. Remote image admission must use that same
resolver and revalidate the plan after its reads.

Docker CLI build/push uses a private 0700 directory with a 0600 auth file, containing
only explicitly accepted credentials. Anonymous plans include an empty
`cooker-anonymous.invalid` marker, because Docker otherwise auto-discovers OS
credential helpers for a completely empty auth map. `WithDockerConfig` removes it on every return;
`DockerEnvironment` overrides inherited Docker auth settings. Build contexts
cannot include the auth directory, including resolved broad/symlink contexts.
The wire DTO deliberately avoids `authn.AuthConfig.MarshalJSON`, which recomputes
the encoded auth field. [Pinned auth schema](https://github.com/google/go-containerregistry/blob/v0.21.5/pkg/authn/authn.go),
[Docker configuration behavior](https://docs.docker.com/reference/cli/docker/).

Accepted Kaniko/Buildah requests always carry a resolver, even when anonymous.
They return `builder.ErrUnavailable` before Kubernetes mutation until a scoped
Pod credential transport is implemented. Legacy nil requests are not evidence
of accepted SEC-04 execution and must not be a production fallback. Provider
service-account IAM and image-pull secrets are separate authorization boundaries.

Docker run, Compose and test runtime pulls require the root-owned scoped config
hook too. `WithDockerConfig` / `DockerEnvironment` can support local CLI paths;
remote hosts need their own reviewed transport. Build/push checks alone do not
prove that runtime pulls cannot borrow host authority. Host-only auth callbacks
also do not establish repository-level ACLs; provider token permissions do that.

Registry credentials are ephemeral auth input, never BuildArgs or runtime secret
bindings. Root wiring must also reject runtime secret keys/known values in all
build inputs, including legacy Compose interpolation and literal native/App
BuildArgs. Known plaintext, URL-escaped password and Basic auth forms are added
to the attempt's redaction context before external tools receive them. Arbitrary
transformed secrets cannot be independently recognized. WithLogSecrets must be
initialized by the execution attempt, even when its initial list is empty.

## Verification scope

Standalone evidence is recorded under
`/mnt/data/cooker-roadmap-20261002/registry-authority/`. Checks use frozen source,
owned fake CLI/Kubernetes/SDK fixtures, loopback OCI receivers and an owned
disposable PostgreSQL database only. They do not read host credentials or invoke
cloud resources. Source presence and standalone checks do not complete SEC-04;
App/native queue admission, execution, runtime pull/secret boundaries, admin/MFA
routes, UI and end-to-end release acceptance require root integration.

## Atomic queue admission — bounded local slice, 3 October 2026

`storepg.SyncRegistryAuthorityAdmission(ctx, tx, job)` runs first in the production
PostgreSQL `OnEnqueue` chain, before scheduler, trigger or publication receipts.
For `app-deploy` and `pipeline-run`, missing evidence is rejected even when the
intended selection is anonymous. Other job kinds are untouched. The job INSERT
and all hook writes share one transaction.

The fence checks the exact App BuildPlan selection or every authored stage's
selection and subject. It takes sorted registry row `FOR SHARE` locks, validates
grants, generation and endpoint/username/credential scope, then locks sorted
database credential environments and verifies version, creation time and key
presence. The SQL returns only metadata and a key-existence boolean, never
ciphertext values. No provider request, decryption or repinning occurs under the
transaction. External references remain structurally pinned; workers validate
their configured backend scope and historic availability before reading values.

Grant/deletion/configuration writes and database credential rotation cannot pass
these locks until admission commits or rolls back. Revocation after commit still
blocks the worker; it does not rewrite/delete the accepted receipt. Retry and
restart recover the original job before preparation, including after revocation.

`bootJobQueue` opens its own pool, separate from the primary CRUD pool. Bind the
shared authority's RegistryConfigStore and EnvironmentStore explicitly to the
queue pool before starting/enabling workers. Check:

```go
storepg.RegistryAdmissionStoresMatch(jobs, authority.Registries, authority.Environments)
```

This requires concrete PostgreSQL stores backed by the same `*sql.DB` instance
as the queue, with an admission callback configured. Distinct pools may address
the same database, but the helper cannot prove that without configuration; they
fail closed until explicitly rebound. It does not inspect DSNs or prove a
callback's contents. Production must wire the registry fence first and retain
the common intended control-plane database for CRUD and database secret reads.

Native workers now require durable evidence when either their Runner or Executor
has registry authority configured. Omitting the Runner pointer cannot turn a
worker call into fresh inline preparation. Missing evidence fails before source,
credential or stage effects; a previously started attempt retains uncertainty.

Verification: `registry-authority/atomic-admission/frozen-v2` passed 705 default
affected race cases (seven optional live fixtures skipped), 52 actual PostgreSQL
cases on a port-free owned PostgreSQL 17.11 C/UTF8 instance, and tagged scoped vet.
The 62 focused cases overlap the affected suite and must not be added to it.
Tests cover atomic App trigger receipts, native concurrent/restarted admission,
row-lock blocking, stale/substituted plans, database failures and cancellation.
This adds no claim of real registry/build/runtime/provider/UI acceptance or
completion of SEC-04; coherent root App/server wiring remains separate.
