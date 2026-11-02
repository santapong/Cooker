# Immutable external-secret references

Accepted deployments must read the secret version that was reviewed, even if the
provider's current value rotates before execution. The optional
`secrets.VersionedManager` contract separates `Pin` (review-time metadata) from
`GetPinned` (execution-time exact version). Existing `Manager.Get/Put/Delete/List`
remain the editing interface. Database-backed environments continue to use their
existing environment-version fence; this contract does not replace that fence.

The adapter slice was checked on 2 October 2026. It does not, by itself, prove
accepted App/Pipeline wiring, historical runtime-log redaction, live IAM access,
or deployment acceptance. Those gates require the separate service/runtime
integration checks.

## Reference contract

`VersionReference` schema version 1 contains only provider, exact resource,
exact version, optional creation/incarnation metadata, and `VersionScope`.
Scope binds authority, region/project/mount/prefix, Cooker environment ID, and
key. KeepSave binds the resolved environment name as `Scope.Prefix` too.
All fields are bounded identifiers. No value, plaintext hash, authentication
credential, arbitrary metadata map, or provider diagnostic is persisted.

`ValidateScope` runs before a provider read. Each adapter then validates its own
resource/version protocol and the returned metadata before exposing bytes.
Changed backend, endpoint, region, project, mount, prefix, environment, or key
fails closed. Credential rotation alone need not invalidate a reference if the
new credential can still read the same exact resource and version.
AWS binds the SDK's effective BaseEndpoint override when present; GCP binds the
constructed gRPC endpoint, including a different universe or mTLS authority.
Unsupported endpoint forms fail before the version provider call.

Only `Pin` may resolve current/latest aliases. `GetPinned` never lists keys,
selects a newer version, substitutes a recreated resource, or falls back to
`Manager.Get`. Unsupported adapters return `ErrVersionUnsupported`; unavailable
versions return `ErrVersionUnavailable`; metadata/scope mismatches and malformed
references have fixed safe sentinels. These errors contain no provider body.

An accepted explicit empty key snapshot stays empty. The caller must distinguish
that state from a missing historical snapshot; these adapters do not infer keys
or repair a missing plan with current values.

## Provider behavior

| Backend | Pin | Exact read and deletion/recreation fence |
| --- | --- | --- |
| AWS Secrets Manager | `DescribeSecret`, exactly one `AWSCURRENT` VersionId | Complete ARN (including random resource suffix) plus VersionId. Validate ARN, name, and returned VersionId; no version stage is sent. |
| GCP Secret Manager | Scoped `GetSecret` and `GetSecretVersion(latest)` | Exact numeric version resource, secret creation time, and version creation time. Check version metadata before and after payload access; require `ENABLED`. Verify CRC32C when supplied. |
| Vault KV v2 | Read metadata, current version and its creation time | Explicit `version=N` data read; validate atomic response metadata `version`, `created_time`, `destroyed`, and `deletion_time`. Creation time rejects path/version reuse. |
| KeepSave versioned vault | Current secret UUID, revision, environment UUID and secret creation time | `GET /api/v1/projects/:project/secrets/:id/versions/:revision`. Validate exact ID, revision, project, environment, key and creation time. Legacy `version`-only responses are unsupported. |

AWS's current stage is mutable, while VersionId addresses one version. A full ARN
prevents a deleted name from silently referring to a newly created secret.
Unlabelled old versions may be removed by the provider: plans then fail rather
than reading the new current value. See [DescribeSecret](https://docs.aws.amazon.com/secretsmanager/latest/apireference/API_DescribeSecret.html)
and [GetSecretValue](https://docs.aws.amazon.com/secretsmanager/latest/apireference/API_GetSecretValue.html).

GCP version resource names and creation timestamps are provider metadata;
`latest` is an alias. This adapter supports global secrets and a configured
project ID or project number; the scoped metadata request resolves the exact
returned resource. Regional secrets are outside this adapter's current naming
contract. State changes are availability checks; mutable etags are not used as
immutable identity. See [SecretVersion schema](https://docs.cloud.google.com/secret-manager/docs/reference/rest/v1/projects.secrets.versions)
and [version access](https://docs.cloud.google.com/secret-manager/docs/reference/rest/v1/projects.secrets.versions/access).

Vault reads explicit versions and reports their creation/deletion state with the
payload. The immutable-read client refuses redirects; configure an active
endpoint or trusted load balancer. Legacy editing retains the original SDK
client behavior. Versioned reads explicitly use the root namespace: namespace
configuration is not supported in this slice. KV v1, missing metadata, soft
deletion, destroyed versions and Unix-socket version authorities fail closed.
See [KV v2 API](https://developer.hashicorp.com/vault/api-docs/secret/kv/kv-v2).

KeepSave's actual local source was inspected at commit
`3878e696cbcdeb97615320e5af864eae0885e4df` (clean tracked checkout at inspection).
`backend/internal/vault/service.go` increments the current revision under a
project transaction lock; `migrations/postgres/018_vault_journal.sql` gives
history a `(secret_id, revision)` primary key and
`022_vault_constraints.sql` rejects history updates/deletes and identity changes.
`handlers_version.go` exposes the exact revision read and verifies authorization.
The legacy repository derives counters from retained rows, so a legacy response
cannot establish the stronger journal contract and is rejected.

KeepSave's current list API includes plaintext values and requires value-read
access. `Pin` uses a bounded allow-list DTO that ignores those values, and never
stores or logs the body. The historical endpoint returns a successful authorized
value read; its `omitempty` encoding represents an empty value by omitting
`value`, which Cooker preserves as an empty byte slice. Pin/read response bodies
are capped at 1 MiB; redirects are refused. Deleted journal entries and denied or
disabled revision routes remain unavailable, even if older encrypted history
still exists. A differently configured or legacy KeepSave server is not inferred
to support history from its name.

## Permissions, retention and validation

Pinning adds metadata-read requirements: AWS `DescribeSecret`; GCP
`secretmanager.secrets.get` and `secretmanager.versions.get`; Vault KV v2 metadata
read; KeepSave current collection access. Execution still needs exact value-read
authorization. Missing permission, expired credentials, rate limits and provider
outages all fail with safe fixed errors; SDK retries remain bounded by their
configured policies and the caller's context deadline. There is no automatic
credential refresh or secret-copy fallback in this contract.

Operators must retain reviewed versions for queued runs and any supported
historical inspection. This design does not prevent administrative destruction,
provider garbage collection, or database backup rollback. Creation timestamps
are provider-reported incarnation metadata, not independent cryptographic
attestations. Historical log redaction must fail closed when pinned values are
unavailable; a current secret is not an acceptable substitute.

SDK schemas were checked against the candidate's pinned dependencies:
AWS Secrets Manager `v1.41.7`, GCP Secret Manager `v1.20.0`, and
`vault-client-go v0.4.3`. Focused acceptance uses only in-process SDK fakes and
owned loopback HTTP servers. It covers rotation, exact version requests,
deletion/recreation (including a GCP access race), configuration substitution,
unsupported legacy protocols, malformed/nil responses, empty values, checksum
failure, denied reads, redirect refusal, and secret canaries in rejected error
bodies/metadata. No real cloud account, secret, credential file, or KeepSave
instance was read or changed for these tests.

Evidence and the frozen source manifest live under
`/mnt/data/cooker-roadmap-20261002/secret-versions`. Run the affected checks from
the frozen backend with `go test -race -count=1 ./internal/secrets/...` and
`go vet ./internal/secrets/...`; broader plan/runtime acceptance is recorded
separately by the integrating agent.

## App, native DAG and historical-log integration

App admission records a private `EnvironmentSecretPlan` with the environment
generation, backend mode, explicitly observed key set and version references.
An empty array is admission evidence; an absent old plan is not. Workers and
image admission use that plan without discovering current keys. Database secrets
retain their environment-generation fence; rotation makes an unavailable old
generation fail rather than implying that the database archives secret history.

Authored DAG admission records the exact union of each stage's `SecretRefs`,
grouped by its `EnvironmentID`, together with stage permissions. Manual and
scheduled queue paths share preparation. A fresh worker validates those bindings
before resolving any values. Values enter copied execution configs only and do
not alter the stored Pipeline or queue payload. This slice supports explicit
secret injection into Docker deploy, Test and Custom stages. Other transports
fail closed; generic build credentials and per-App registry authority remain
separate work packages.

Both runtime-log entry points require the deployment's captured secret plan.
Legacy external deployments can replay only when their captured service
permissions prove plain-only inputs; an empty current external inventory does
not prove that history. A missing/deleted pinned value denies historical replay.
Output protection includes metadata keys and artifact types as well as values,
and public automation failures use fixed messages rather than nested private
provider errors. These checks protect known values; they do not establish
redaction of arbitrary encodings or every other product output boundary.

The coherent combined source and verification are under
`/mnt/data/cooker-roadmap-20261002/secret-versions/combined-v4`. Its owned
PostgreSQL tests cover JSONB round trips, the retained App receipt digest and a
fresh native worker resolving the original version after rotation. That is
separate from a process-kill campaign, live IAM/retention acceptance or rollout
to the owner's running server.
