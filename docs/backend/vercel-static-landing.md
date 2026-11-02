# Publish the Cooker landing page through Cooker

This bounded Vercel publishing feature creates a **new dedicated static project**
and publishes the prepared Cooker landing as its **first production release**.
It extends the separate [read-only Vercel inventory](vercel-sync.md) feature.
It does not replace a connected project's existing deployment.

The artifact contains exactly seven public files: the landing HTML, its CSS
and JavaScript, shared tokens/theme CSS, the favicon and the selected Open
Workbench illustration. The existing Cooker logo is preserved. Login,
documentation, workspace and example files are excluded. The published landing
links to public GitHub documentation and has no Sign in or Workspace preview
navigation. Publishing does not deploy the Cooker backend or start n8n.
Custom domains, arbitrary repositories/files, subsequent releases
to an existing project, promotion, rollback, cancellation and deletion are
outside this publishing slice.

## Operator setup

Use a build containing migration **033** (`vercel_publications`) and the
embedded landing files, alongside migration **032** (`vercel_connections`).
The normal PostgreSQL migration path applies these migrations.

| Setting | Requirement |
| --- | --- |
| `DATABASE_URL` | Persistent PostgreSQL used by the Cooker installation |
| `COOKER_SECRET_KEY` | The installation's existing base64-encoded 32-byte encryption key |
| `COOKER_JOBQUEUE_ENABLED=true` | Required for durable publication execution and recovery |
| Authenticated administrator | Required for connection setup, review, publication and provider checks; configured MFA also applies |
| Outbound HTTPS | Access to the fixed `https://api.vercel.com` API origin |

For local email/password sign-in, pair backend
`COOKER_LOCAL_AUTH_ENABLED=true` and a persistent
`COOKER_LOCAL_AUTH_JWT_SIGNING_KEY` with a frontend **build** using
`VITE_LOCAL_AUTH_ENABLED=true`. Keep `VITE_OIDC_ENABLED=false` when no OIDC
provider is configured. A frontend runtime environment change alone does not
rebuild these flags. Use the existing authenticated installation or its first
account setup; the local preview has no seeded administrator credentials.
See the [operator guide](operator-guide.md) for the surrounding configuration.

In **Settings → Vercel connections**, connect the intended personal account or
exact team ID. Enter a Vercel access token in Cooker's secure form, never in a
chat, URL or source file. It needs access to verify the selected identity, list
its projects, create a project, and create/read deployments in that scope.
Cooker verifies the identity and encrypts the token server-side. Managed
connector credentials are not copied into Cooker. Project selection for
read-only inventory is independent of this new dedicated publication project.

Preserve the PostgreSQL data and the encryption key together, with protected
backups; preserve the local-auth signing key separately as well. Replacing or
losing the encryption key makes saved provider credentials unreadable. Deleting
the database loses publication receipts and their recovery evidence. Neither
action removes resources already created in Vercel. Follow the existing
[backup/restore guidance](backup-restore.md) instead of resetting state to retry.

## Review and publish

1. Open **Cloud → Vercel deployments → Publish Cooker landing** as an
   administrator. **Preview landing** opens the exact embedded public artifact
   under `/landing-preview/` on the Cooker server.
2. Choose an enabled Vercel connection and select **Review deployment**.
   Review performs provider reads and saves a receipt; it does not create a
   Vercel project or deployment.
3. Check the account/team, generated `cooker-landing-…` project name, file
   count, artifact SHA-256, and **Production · first release** target. The
   review expires after ten minutes. Changing the artifact or connection
   requires a fresh review before submission.
4. Select **Create project & publish landing**. The administrator who created
   the review must submit it. Cooker durably queues that operation, creates
   its dedicated static project, then submits the reviewed files once.
5. Follow the retained operation, project and deployment IDs. A successful
   receipt requires matching provider metadata, production target, `READY`
   state and a validated HTTPS URL. **Open published landing** opens that URL.
   Provider readiness does not itself prove application-level behavior or
   unrestricted public access under the account's Vercel protection settings.

The project uses the documented static configuration: no framework, empty
install/build commands, and `.` as its output directory. Deployment uses
inline base64 files and an explicit production target. Cooker adds no custom
domain and makes no deployment-time changes to an existing project's settings.

## Refresh, uncertainty and recovery

| Action | What it does |
| --- | --- |
| **Refresh status / Refresh publication history** | Reads Cooker's saved publication receipt; does not submit or query a new deployment |
| **Check provider** | Reads Vercel for an operation marked **Needs attention**, verifies its identity/artifact, and records the observed result; never submits a deployment |
| Inventory **Refresh** | Refreshes the separate snapshot of selected projects and recent deployments; does not resolve a publication receipt |

The reviewed operation ID is Cooker's durable idempotency key. Repeated or
concurrent submissions of that same review create at most one queue job.
A different review is a different operation and a different proposed project;
do not create one merely because an earlier response was lost.

Each queued publication has one execution attempt. Before either provider
write, Cooker persists its intent and rechecks the job claim plus the original
connection version, enabled state, account and team. Known provider IDs are
retained when a response is incomplete, including when the request deadline
ends, provided the worker still owns its claim. Disabling a connection prevents
later admitted writes; it cannot retract a request already submitted to Vercel.

A lost response, interrupted worker or expired worker claim produces
**Needs attention** rather than automatically replaying an external write.
That job keeps the publication lock for its account. Cooker does not claim a
provider-side exactly-once guarantee or send a fabricated idempotency header.
The static client never automatically retries a mutation.

**Check provider** reads the retained deployment ID. When only a project ID is
known, it can inspect up to the latest 100 deployments and accept exactly one
matching operation ID and artifact hash. A discovered ID is saved even while
the deployment is building. Missing or ambiguous history remains inconclusive;
it never authorizes replay. If project creation itself is uncertain and no
project ID was received, an administrator must inspect Vercel and record an
evidence-backed operator resolution before another publication proceeds.
Cooker does not automatically delete a project left by a partial operation.

For read-only reconciliation, a replacement token on the same connection may
be used after exact account, scope and team verification. This also works when
inventory synchronization is paused. Replacement credentials do not authorize
replaying the original writes; those retain the original connection-version
pin. Disconnecting removes the credential needed for these checks, so resolve
uncertain publications before disconnecting where possible.

## Artifact and acceptance boundary

The dedicated `Cooker-landing/website/build_landing.py` build produces
`website/dist-landing` from an explicit seven-file allowlist. It adjusts
navigation only in the generated deployment index; the full local design
preview and its original source pages are preserved. Do not copy the broader
`website/dist` package into this deployment artifact.

Only `backend/internal/provider/vercel/landing/files` is embedded and published;
the adjacent `SOURCE.json` records source file sizes/hashes and is not included
in the public artifact. The artifact digest hashes lexically ordered paths,
each followed by a NUL byte and the fixed-size SHA-256 of that file's bytes.
The worker compares the reviewed digest with its embedded artifact before any
provider mutation. This is not a general server-directory upload endpoint.

The client bounds the artifact to 100 files, 4 MiB per file and 8 MiB total,
requires an `index.html`, validates relative paths, and rejects executable
provider/build configuration. HTTP response sizes and timeouts are bounded,
redirects are disabled, and errors never include raw provider response bodies
or tokens. Deployment metadata records `cookerOperationId` and
`cookerArtifactSha256` for scoped recovery.

Implementation includes synthetic provider transport tests and PostgreSQL
publication/recovery tests, with their broader results recorded separately.
The 1 October 2026 landing-only package passed 135 scoped race cases/subtests,
including exact allowlist, excluded-route and provider-client checks; its
static backend build also passed. Evidence is under
`/mnt/data/cooker-landing-only-20261001/runtime`. These package checks did not
perform an external deployment or repeat the PostgreSQL campaign.

**Hosted publishing acceptance is a separate gate.** Use the chosen account
connection in Cooker, review this seven-file artifact and new-project
destination, publish once, then verify the returned website and durable receipt.
Neither preparing the artifact nor restarting the local preview establishes
that hosted outcome. Credentials belong only in the secure connection form.

## Official provider references

Checked 1 October 2026:

- [Create a project — POST /v11/projects](https://vercel.com/docs/rest-api/projects/create-a-new-project)
- [Skip the build step for static files](https://vercel.com/docs/builds/configure-a-build#skip-build-step)
- [Project configuration and install command](https://vercel.com/docs/project-configuration/vercel-json)
- [Create a deployment — POST /v13/deployments](https://vercel.com/docs/rest-api/deployments/create-a-new-deployment)
- [Read a deployment and production/preview target](https://vercel.com/docs/rest-api/deployments/get-a-deployment-by-id-or-url)
- [Recent deployment listing](https://vercel.com/docs/rest-api/deployments/list-deployments)
- [Current OpenAPI specification](https://openapi.vercel.sh/)
