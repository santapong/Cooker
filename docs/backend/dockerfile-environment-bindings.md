# Single-Dockerfile runtime environment bindings

A Dockerfile App can select which keys from its linked Environment reach its
single runtime component. The existing component ID is `deploy`; it is not a
user-created service name. The API uses the existing BuildPlan fields:

```json
{
  "kind": "dockerfile",
  "path": "Dockerfile",
  "environmentScope": "service",
  "serviceEnvironment": {
    "deploy": ["DB_NAME", "TOKEN"]
  }
}
```

`DB_NAME` may be readable configuration and `TOKEN` may be a protected key.
Selecting names does not reveal or store protected values in the App, queue
payload, deployment graph, history, or API response. Protected values are resolved
in memory for supported admission checks, runtime configuration and log handling.

With explicit `environmentScope: "service"`, an empty `deploy` list, an empty
map, or an omitted map injects no linked values. It records an explicit empty
secret snapshot when an Environment is linked and does not list or retrieve
protected values. Unknown keys, duplicate keys, other component names, selected
plain/protected collisions and private registry environments are rejected.
Without a linked Environment only an empty selection is valid.

An omitted `environmentScope` retains the previous all-Environment runtime
injection behavior. The interface must label that behavior as legacy; changing
an existing App to explicit scope must be a deliberate saved configuration
change. Buildpack plans do not support this binding. Compose keeps its existing
named-service selection contract; this addition does not narrow Compose's
admission-time secret inventory reads.

For queued App deployment, admission captures the App configuration, linked
Environment generation, selected protected key names, and immutable external
secret version references where supported. Workers validate that evidence
against the exact accepted `deploy` selection before retrieving values. They
do not discover new keys or repin latest external versions. An explicit missing
or inconsistent accepted snapshot fails closed. Database-backed secrets retain
the existing Environment-generation fence. A changed generation requires a
new reviewed deployment. Receipt recovery preserves the original selection.

The captured subset is used for historical log redaction as well. An inspection
must use the deployment's accepted App and secret snapshot; current App key
selections do not silently replace them. A changed Environment generation
refuses inspection rather than guessing which protected values to redact.

Runtime keys grant no build authority. They are passed only to the synthesized
`deploy` stage; they are not copied into BuildArgs, build/push stage environments,
or Compose interpolation inputs. In explicit mode, BuildArgs cannot use known
linked key names and cannot contain resolved selected protected values as
literal aliases. `BuildPlan.Variables` is rejected for an explicitly scoped
Dockerfile plan. Invalid metadata and accepted-reference mismatches fail before
source checkout; checking a protected literal build argument can require reading
the selected immutable protected value before checkout. Arbitrary caller-supplied
public BuildArgs remain separate inputs and are not evidence of runtime-key
authorization.

The focused backend verification uses owned in-memory stores, actual App queue
admission and restored JSON payloads. The workspace selector has separate
browser checks for empty defaults, legacy labeling, roles, conflicts and session
drafts. New Dockerfile apps select no linked values by default; existing omitted
scopes require an explicit saved conversion to change their behavior.

A separate real PostgreSQL campaign admits selected and empty Dockerfile Apps
through the production queue hooks, with actual local immutable Git preflight.
Fresh helper processes resolve the exact accepted subset before/after storage
rewrap, database restart and restore, alongside native and registry pins. It
does not claim hosted Git or start a worker, image build or deployment.
A separate official-server/PostgreSQL campaign passed 40 HTTP checks and three
unmocked synthetic-role UI journeys for create/save/read, legacy/empty/subset
bindings, stale-edit recovery and refusal controls. New Apps now return the
server-owned initial version one, matching the stored record and its first edit.
The configuration fixture contains an actual Docker CLI with an unreachable
private endpoint; it does not provide daemon or deployment evidence.

Actual Docker worker/runtime and compatible-fallback acceptance remain open.
Existing Compose/native and previous accepted runtime evidence retain their exact
earlier frozen build inputs; the older843/846 workers reject this newly supported
Dockerfile binding contract.
