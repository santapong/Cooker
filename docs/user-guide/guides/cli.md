# CLI (`cookerctl`)

`cookerctl` is a single-binary command-line client for a Cooker server. It
talks to the same REST API the web UI uses, authenticated with an
[API token](#1-create-an-api-token), and covers the core automation loop:
list, export, import, and run pipelines, and follow a run to completion.

It is the scripting and CI-of-CI entry point — anything you can do by hand
in the Pipelines UI, you can wire into a shell script or a CI job.

## Install

`cookerctl` is a second command in the same Go module as the server (the
module is rooted at `backend/`). Build it from a checkout:

```bash
make build-cli          # writes ./bin/cookerctl
# or, directly:
cd backend && go build -o cookerctl ./cmd/cookerctl
```

You can also install it with the Go toolchain. The module path maps to the
`backend/` directory, so the package import path is
`github.com/santapong/cooker/cmd/cookerctl`:

```bash
go install github.com/santapong/cooker/cmd/cookerctl@latest
```

That drops a `cookerctl` binary in `$(go env GOPATH)/bin` — put that on your
`PATH`.

Check it works (this also prints the server's build version once it can
reach one):

```bash
cookerctl version
```

## 1. Create an API token

The CLI authenticates with a `ck_…` API token, not your browser session.
Create one in the UI under **Settings → API tokens**:

1. Click **New token**, give it a name (e.g. `ci-deploy`), and pick a role.
   The role is capped at your own — a token can never out-rank the user
   who minted it. For CI, prefer the **operator** role and a short expiry.
2. Copy the plaintext **once** — it is shown exactly once and is
   unrecoverable afterward. Cooker stores only its hash.

> Tokens that authenticate the API cannot themselves mint or delete tokens
> (no self-replication). Manage tokens from the UI or an interactive login.

## 2. Point the CLI at your server

Two settings: the server URL and the token. Both can come from flags or
environment variables; **the environment is preferred for the token** so it
never lands in your shell history or process list.

```bash
export COOKER_URL=https://cooker.example.com
export COOKER_TOKEN=ck_xxxxxxxxxxxxxxxxxxxx   # the plaintext from step 1
```

| Setting | Env var | Flag | Default |
|---|---|---|---|
| Server base URL | `COOKER_URL` | `--server` | `http://localhost:8080` |
| API token | `COOKER_TOKEN` | `--token` | _(none)_ |

The token value is never printed, logged, or included in any error message.
If a request comes back `401`, the CLI prints a hint pointing you back to
**Settings → API tokens**.

## 3. The core loop

### List pipelines

```bash
cookerctl pipelines list
```

```
ID                                    NAME     STAGES  VERSION  UPDATED
6f1c…                                 release  4       7        2026-06-11T09:12:03Z
```

Add `--json` to any list/get command for machine-readable output:

```bash
cookerctl pipelines list --json | jq -r '.[].id'
```

### Export a pipeline to YAML

Export is read-level, the same access as viewing a pipeline. Write it to a
file you can commit to git:

```bash
cookerctl pipelines export 6f1c… -o release.cooker.yaml
# or stream to stdout:
cookerctl pipelines export 6f1c… > release.cooker.yaml
```

The document is portable: server-assigned fields (id, timestamps, version)
are stripped, and secret **values** never appear — only `secretRefs`
(names). See [Pipelines as code](pipelines-as-code.md) for the envelope.

### Import a pipeline from YAML

Import creates a **new** pipeline (fresh id) and prints it:

```bash
cookerctl pipelines import -f release.cooker.yaml
# Imported pipeline "release"
# ID: 9a2e…
```

Use `-f -` to read the document from stdin. Import runs the same DAG
validation the editor does — an invalid graph fails with the same error.

### Run a pipeline and follow it

Trigger a run. By default the CLI returns immediately with the run id:

```bash
cookerctl pipelines run 9a2e…
# Run 1b3d… started (status: pending)
```

Add `--follow` to poll the run to completion, printing each stage
transition and the stage's logs as it finishes. **The exit code mirrors the
run result** — `0` on success, `1` on failure or cancellation — so it drops
straight into a CI step:

```bash
cookerctl pipelines run 9a2e… --follow
# Run 1b3d… started (status: pending)
#   [09:14:01] Build image → running
#   [09:14:09] Build image → success
#     --- logs: Build image ---
#     #1 building with kaniko
#     …
#   [09:14:09] Deploy to prod → running
#   [09:14:18] Deploy to prod → success
#     --- logs: Deploy to prod ---
#     deployment.apps/app configured
# Run 1b3d… finished: success
echo $?   # 0
```

`--follow` uses REST polling (~2s interval), not WebSocket — no ticket
exchange, nothing to keep open. It is intentionally simple; for live
streaming in a browser, use the UI.

#### Idempotent triggers

Every `run` sends an `Idempotency-Key` header. By default the CLI generates
a fresh key per invocation, so retrying a `run` after a transient network
error replays the original run instead of starting a duplicate. Pin the key
yourself when a CI job may re-run the same logical step:

```bash
cookerctl pipelines run 9a2e… --idempotency-key "build-${GIT_SHA}" --follow
```

A replayed trigger returns the original run unchanged.

### Inspect run history and logs

```bash
cookerctl runs list 9a2e…
# RUN ID     STATUS   STAGES  CREATED
# 1b3d…      success  2       2026-06-11T09:14:00Z

# All stage logs for a run (pipeline id is required to locate the run):
cookerctl runs logs 1b3d… --pipeline 9a2e…

# Just one stage:
cookerctl runs logs 1b3d… --pipeline 9a2e… --stage deploy
```

Both accept `--json`.

## A full CI example

Import a version-controlled pipeline, run it, and gate the job on the
result — the whole loop in four lines:

```bash
export COOKER_URL=https://cooker.example.com
export COOKER_TOKEN=$CI_COOKER_TOKEN          # injected as a CI secret

PID=$(cookerctl pipelines import -f release.cooker.yaml --json | jq -r .id)
cookerctl pipelines run "$PID" --follow         # exits non-zero if the run fails
```

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success (including a followed run that succeeded) |
| `1` | Runtime error: API error (non-2xx), a `401`, or a followed run that failed/was cancelled |
| `2` | Usage error: unknown command or bad flags |

## See also

- [Pipelines as code](pipelines-as-code.md) — the YAML envelope `export`/`import` use.
- [Your first pipeline](first-pipeline.md) — build one in the editor first.
- [API reference](../reference/api.md) — the full endpoint list the CLI is built on.
