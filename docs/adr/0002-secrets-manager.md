# ADR-0002 — Pluggable `secrets.Manager` with KeepSave as system of record
Date: 2026-05-02
Status: accepted

## Context

Until this ADR, Cooker stored environment secrets as AES-GCM ciphertext in a JSONB column on `environments`. The key (`COOKER_SECRET_KEY`) lived in the Cooker process. This works for single-tenant deployments but couples secret rotation to Cooker's lifecycle, gives no cross-system audit trail, and offers no per-env access scoping beyond what Cooker enforces.

[KeepSave](https://github.com/santapong/keepsave) is a sibling project that provides a REST API for project + environment-scoped secret storage with AES-256-GCM at rest, per-project DEKs, key rotation, and a built-in promotion endpoint that maps cleanly onto Cooker's promote-secrets feature.

## Decision

Introduce `secrets.Manager` (ADR-0001) with two adapters today:

1. **`database`** — wraps the existing AES-GCM + JSONB code path. Default. Behavior unchanged for existing installs.
2. **`keepsave`** — delegates to KeepSave over HTTP. KeepSave is **system of record**: Cooker neither stores nor caches plaintext locally. The local AES `Codec` only loads when backend = `database`.

Tenancy: a single KeepSave project owns all of one Cooker install's secrets. Cooker's environment **name** (e.g. `prod`, `uat`) maps to KeepSave's `environment` query parameter. Per-env API keys provide isolation without forcing N projects.

Alternatives considered:

| Option | Rejected because |
|---|---|
| One KeepSave project per Cooker env | Loses cross-env promotion (KeepSave's `/promote` is intra-project), N× operator burden. |
| Per-env KeepSave project + schema change | Most flexible, costs a schema migration; defer until a customer asks. |
| Write-through with DB fallback | Doubles the write surface, complicates rotation, and KeepSave already owns durability. |
| Direct import of KeepSave Go SDK | SDK directory has no `go.mod` yet; using it requires module-publishing on the KeepSave side. Stub with an internal HTTP client until then. |

## Consequences

+ Operators can swap to KeepSave with one env var (`COOKER_SECRETS_BACKEND=keepsave`) plus three config values; no schema change on the Cooker side.
+ KeepSave handles encryption, rotation, and audit, freeing Cooker from custody of the master key.
+ Cooker's promote-secrets feature can later wrap KeepSave's `/promote` endpoint cheaply.

− Switching backends doesn't auto-migrate; an operator who flips the env var on a populated install will see empty secret lists until they copy data over. Documented in README §Secrets backends.
− KeepSave outage = Cooker's secret API is down for that install. No DB fallback (intentional, per system-of-record decision). Mitigated by KeepSave's own HA story.
− Round-trip latency on secret read/write is bounded by HTTP to KeepSave (~few ms in-cluster); the SDK-level cache is a follow-up once we swap from the internal client.
