# ADR-0003 — JSONB columns for pipeline graphs and environment secrets
Date: 2026-05-02
Status: accepted

## Context

A pipeline is a directed acyclic graph of stages (build/test/push/deploy/approval/custom). The shape of each stage's config differs per type, edges carry conditional labels, and the editor lets users invent custom stages. Storing this in a fully-normalised relational schema (`pipelines`, `stages`, `edges`, `stage_config_<type>`) gives strong typing per row but turns every read into a multi-table join and every shape change into a migration.

The same shape problem appears for environment-scoped configuration: `PlainVars` (a `map[string]string`) and `Secrets` (a `map[string][]byte`) both need to grow new keys without DDL.

## Decision

Use `JSONB` columns for both:

- `pipelines.graph JSONB` — the entire React-Flow node + edge document. Server-side validation runs on read/write via `internal/oci`-style strict types but the column is loose.
- `environments.plain_vars JSONB`, `environments.secrets JSONB` — maps. Secrets values are the AES-GCM-sealed blob, base64-encoded so the JSON layer doesn't choke on binary.

Queries that need to filter or rank inside the graph (e.g. "pipelines that target environment X") use the JSONB indexing operators (`@>`, `->`, GIN indexes when needed).

Alternatives considered:

| Option | Rejected because |
|---|---|
| Fully normalised relational schema | Joins multiply per node count; every UI feature needs a migration. |
| Flat JSON column (`TEXT`) | Loses indexing, type-safety on reads, and the JSON containment operators that make filters ergonomic. |
| Document store (Mongo) | New dependency for the one place we need flexible shapes; Postgres JSONB is the lowest-cost path. |
| Separate microservice per stage type | Premature splitting; the data is co-owned by one transactional boundary anyway. |

## Consequences

+ Schema migrations are rare — most editor changes don't touch DDL.
+ Single transactional read for a pipeline + its full graph; same for environments + all their config.
+ The OCI image-spec types and the Pipeline graph types both live behind validation functions, so the looseness of JSONB doesn't leak into handler code.

− No referential integrity from JSONB into other tables. We rely on application-level checks to keep `target.environmentId` references valid.
− Postgres-specific. If we ever want SQLite or MySQL support, JSONB has to be re-mapped; today it's not on the roadmap.
− Aggregate queries over thousands of pipelines are slower than a flat schema would be. Acceptable: the dataset is small (low-thousands of pipelines per install) and the hot reads are by ID.
