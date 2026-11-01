# Compose library and registries

Cooker uses three different kinds of registry or catalogue. Choose the one that
matches what you want to save.

| Surface | Saved content | Persistence and sharing |
|---|---|---|
| **Compose registry: saved stacks** | File path, display name, service/network/volume counts and saved time | Browser storage, scoped to the frontend/API origin |
| **Compose registry: GitHub stacks** | Current server-stored Compose Apps, including reviewed SHA, selected files, prefix and bindings | Shared through the Cooker backend; use PostgreSQL for durable storage |
| **Image registries** | OCI/container images and registry access configuration | The configured image registry and Cooker settings |

Cooker does not currently publish/import Compose bundles as OCI artifacts. The
GitHub stack catalogue is not an immutable history of every reviewed revision.

## Save a local Compose file reference

1. Open the **Compose** page and enter a file path readable by the Cooker backend
   under `COOKER_COMPOSE_DIR`.
2. Inspect the parsed service map. Select a service to see its details.
3. Enter a **Stack name**, then choose **Save stack**.
4. Search by name or filename. Select a saved stack to read that file again.

“Local file” means local to the backend, including its mounted directories. This
is not an upload from your browser's filesystem. The backend allowlist applies
when opening or editing a file.

To rename a reference, open it, change **Stack name** and choose **Update saved
stack**. **Forget** removes the browser entry; the source file remains unchanged.
Clearing browser storage also removes these entries. Saved counts are a snapshot;
reopening reads the current file, and saving updates the stored counts.

The library stores no YAML, environment values or graph payloads. A browser
storage failure is reported without preventing you from opening a file. A failed
file open keeps the previous successful graph/path together.

## Edit service configuration

The service inspector can update image, ports and environment for the opened
file. A successful save writes that service through the backend and adopts the
returned graph. This changes the source Compose file; saving a library reference
only bookmarks it. The backend must have permission to write the mounted file.

## Reopen a GitHub stack

Import through **Apps → New app**, inspect the Compose preview, then **Save app**.
The **GitHub stacks** section of the Compose registry lists saved Compose Apps.
Open one to revisit the reviewed configuration. Another browser using the same
backend can see the same server-backed catalogue, subject to access permissions.

A saved App retains its source SHA. It does not follow the branch automatically;
use **Inspect latest branch revision** and review before saving an update. No
build or deployment occurs simply by reopening the stack.

## Choose an image registry

The import destination step accepts a registry host and optional repository path,
for example `registry.example.com/team`. This identifies where built images go;
it does not create a registry or grant push/pull credentials. Use the **Image
registries** link for the separate registry surface.

The Docker build/push host must have push access, and the selected runtime must
have pull access. Private cloud registries may require pre-created repositories
and workload roles. Follow [GitHub Compose deployment](../../guides/GITHUB-COMPOSE-DEPLOYMENT.md)
for target prerequisites and the [registry guide](registries.md) for configuration.
