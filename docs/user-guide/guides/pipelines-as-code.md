# Pipelines as code (YAML import/export)

Every pipeline you build in the visual editor round-trips losslessly to a
YAML document. That lets you keep pipelines in git, code-review changes,
share examples, and recreate a pipeline on another Cooker instance with a
single request.

There is no separate DSL: the YAML is just the pipeline's JSON shape
wrapped in a small envelope, so the field names match the API exactly.

## The envelope

```yaml
apiVersion: cooker.dev/v1
kind: Pipeline
metadata:
  name: my-pipeline
  description: optional human description
spec:
  stages: []
  edges: []
```

- `apiVersion` / `kind` are fixed at `cooker.dev/v1` / `Pipeline`. Import
  rejects anything else with a 400 so a future format can't be
  mis-parsed.
- `metadata` carries the only authored identity fields (`name`,
  `description`).
- `spec` is the graph: `stages`, `edges`, and the run-time policy
  (`variables`, `runDeadline`).

Server-assigned fields — `id`, `createdAt`, `updatedAt`, and the
optimistic-concurrency `version` — are **never** exported. A re-import
mints fresh values, so the same document is portable across instances and
the export is deterministic (export → import → export is byte-identical).

> **Secrets are safe to commit.** A stage references secrets by name
> (`secretRefs: [DOCKERHUB_TOKEN]`), never by value. Secret material lives
> in an Environment and is resolved at run time, so it never appears in an
> exported document. You still commit *which* secrets a stage needs, not
> the secrets themselves.

## Export

In the Pipelines list, each pipeline card has an **Export** button that
downloads `<name>.cooker.yaml`.

Over the API (read-level access, the same as viewing a pipeline):

```bash
curl -fsSL \
  -H "Authorization: Bearer $COOKER_TOKEN" \
  http://localhost:8080/api/v1/pipelines/$PIPELINE_ID/export \
  -o my-pipeline.cooker.yaml
```

## Import

In the Pipelines list, click **Import YAML**, then paste a document or
choose a `.yaml` file and click **Import pipeline**. Cooker creates a new
pipeline and opens it in the editor.

Over the API (requires the `operator` or `admin` role, the same as
creating a pipeline). Send the raw YAML as the body with
`Content-Type: application/yaml`:

```bash
curl -fsSL -X POST \
  -H "Authorization: Bearer $COOKER_TOKEN" \
  -H "Content-Type: application/yaml" \
  --data-binary @my-pipeline.cooker.yaml \
  http://localhost:8080/api/v1/pipelines/import
```

Import always **creates a new pipeline** with a fresh ID. There is no
name-collision handling: importing the same document twice gives you two
pipelines (the editor's "New pipeline" has no unique-name rule either). To
update an existing pipeline from a file, import it and delete the old one,
or edit in place.

Validation on import is identical to the editor's: an unknown
`apiVersion`/`kind`, malformed YAML, an empty body, an invalid stage
type, or a graph that fails DAG validation (cycles, dangling edges) all
return a 400. A DAG failure returns the same
`{ "valid": false, "errors": [...] }` envelope you'd see from the
**Validate DAG** button.

Documents larger than 1 MiB are rejected with a 413 — a pipeline is a few
KB even with many stages.

## A full example

A build → push → deploy pipeline with an approval gate before production,
a structured retry on the build, a registry layer cache, and per-stage
env and secret references:

```yaml
apiVersion: cooker.dev/v1
kind: Pipeline
metadata:
  name: release
  description: Build, push, approve, deploy to prod
spec:
  variables:
    REGISTRY: registry.example.com/acme
  runDeadline: 45m
  stages:
    - id: build
      name: Build image
      type: build
      position: { x: 0, y: 0 }
      config:
        dockerfile: Dockerfile
        context: .
        tags:
          - ${REGISTRY}/app:latest
        cache:
          mode: registry
          ref: registry.example.com/acme/app:buildcache
        retry:
          maxAttempts: 3
          initialMs: 500
          maxMs: 5000
          exponential: true
        env:
          CGO_ENABLED: "0"
        secretRefs:
          - DOCKERHUB_TOKEN
    - id: push
      name: Push to registry
      type: push
      position: { x: 220, y: 0 }
      config:
        registry: ${REGISTRY}
        repository: app
    - id: approve-prod
      name: Approve production
      type: approval
      position: { x: 440, y: 0 }
      config:
        requiredApprovers: 1
    - id: deploy
      name: Deploy to prod
      type: deploy
      position: { x: 660, y: 0 }
      environmentId: prod
      config:
        namespace: prod
        manifestPath: k8s/
  edges:
    - id: e1
      source: build
      target: push
      condition: success
    - id: e2
      source: push
      target: approve-prod
      condition: success
    - id: e3
      source: approve-prod
      target: deploy
      condition: success
```

> **Environment IDs are instance-specific.** `environmentId` and any
> `secretRefs` point at Environments and secrets that must already exist
> on the target instance. Importing a document that references an
> environment that doesn't exist yet still creates the pipeline — wire the
> environment up before you run it.

## See also

- [Your first pipeline](first-pipeline.md) — build one in the editor first.
- [Secrets](secrets.md) — how `secretRefs` resolve at run time.
- [Promotions](promotions.md) — environment promotion and approval gates.
- [API reference](../reference/api.md) — the full endpoint list.
