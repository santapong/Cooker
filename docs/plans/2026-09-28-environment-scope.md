# Per-service runtime environment — 28 September 2026

Compose Apps can now explicitly select which linked environment keys each
service receives. The default remains the earlier global injection behavior
for existing manual configurations. Automated Compose deployment requires the
explicit mode in the automation policy.

```json
{
  "kind": "compose",
  "files": ["compose.yaml"],
  "environmentScope": "service",
  "serviceEnvironment": {
    "web": ["DB_NAME", "DB_PASSWORD", "N8N_ENCRYPTION_KEY"],
    "worker": ["DB_NAME", "DB_PASSWORD", "N8N_ENCRYPTION_KEY"],
    "postgres": ["POSTGRES_DB", "POSTGRES_PASSWORD"],
    "redis": []
  }
}
```

These are references to keys, never copied credentials. Omitted services and
empty lists receive no linked keys. An empty map still means deny all because
`environmentScope` survives JSON serialization independently. Compose-literal
environment entries retain their normal override precedence.

The resolver rejects a key configured as both plain and secret in explicit
mode. Inspection rejects unknown services/keys, duplicate keys, a map without
its mode, and collisions between linked keys and public Compose interpolation
inputs. External database bindings also require each source key in every
consumer's allowlist; aliases cannot grant a service additional access.

Linked environment interpolation is allowed only in the selected service's
runtime `environment` section. It cannot supply a build argument, image,
command, path, label or global resource. Bare inherited environment keys and
bare build arguments are checked too. Non-secret build/interpolation inputs
can be supplied separately in `variables`. Known secret values are checked
after Compose interpolation, including literal copies and renamed variables.
The checks do not recognize arbitrary encoded or transformed values.

Explicit scope currently rejects `env_file` and linked variables in shared
top-level extension templates; declare reviewed runtime environment keys in
the service instead. This conservative boundary avoids the loader's global
interpolation environment bypassing service isolation. Single-workload plans
do not yet support this mode. Existing manual plans with no scope retain their
earlier behavior, including secret-over-plain precedence.

The same selection feeds both generated per-service deployment stages and the
private native Compose runtime file. Runtime values do not become build args
or build environment. Persistent deployment graph views continue to mask
runtime values. Linked environment resolution for inspection registers known
secret values in the attempt's ephemeral protection context.

Tests exercise web/worker/database isolation, deny-all after serialization,
the native Compose file after a second Docker-compatible interpolation pass,
build isolation, alias/literal/bare-variable bypasses, collisions, and legacy
behavior. These are local backend tests, not proof of a production migration
or cloud deployment.
