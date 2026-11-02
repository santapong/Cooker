# Docker service log snapshots

This local backend candidate adds bounded runtime log browsing to native Docker
service cards in Apps and Services. It complements deployment-step history and
the existing live runtime stream. It does not retrieve Vercel logs or implement
Kubernetes pod sessions.

## Workspace usage

1. Sign in as an operator or administrator and open a native app's Services
   section or the workspace Services page.
2. Choose **Browse current Docker logs**, then a replica. Multiple replicas
   require an explicit choice; Cooker does not silently combine their output.
3. Choose a time range, tail limit and optional literal search. Custom windows
   span at most 24 hours. Search runs after known secret values are redacted.
4. Capture the snapshot. Expand its deployment/container details to check the
   exact run, immutable container ID, window and expiry.
5. Page through that same snapshot or download its redacted text. Capture again
   for newer output. Close the browser to release the session.

Snapshot entries remain fixed while paging. The control does not follow newer
output, a recreated container or a later deployment. Empty or unavailable logs
are not an application health verdict. Imported Vercel services keep their
verified provider inspector links.

## API contract

All paths begin with `/api/v1/apps/:appId/services/:service` and require the
existing authenticated operator/admin role. Encode individual path segments.

| Method and suffix | Result |
| --- | --- |
| `GET /log-replicas` | Recorded run and inspected immutable replica identities |
| `POST /log-sessions` | New redacted snapshot and first page; HTTP 201 |
| `GET /log-sessions/:sessionId?offset=0&limit=200` | Fixed snapshot page |
| `GET /log-sessions/:sessionId/download` | Redacted UTF-8 text; fixed filename |
| `DELETE /log-sessions/:sessionId` | Release that session; HTTP 204 |

Creation accepts `containerId`, `expectedRunId`, `maxLines`, optional literal
`search`, and optional RFC 3339 `since`/`until`. The workspace always sends the
chosen container and recorded run. Without a window, the server uses its last
hour. Replies carry the app, service, run, container, window, expiry, coverage,
ordered entries, offset and optional next offset.

Pages default to 200 entries and accept at most 500. Search is limited to 256
UTF-8 bytes. The default tail is 2,000 lines; the maximum is 10,000. A capture
allows at most 1 MiB of redacted output. Oversized captures fail rather than
silently claiming complete coverage. A reached tail can omit earlier lines.

Request bodies reject unknown fields, trailing values and bodies over 16 KiB.
Public failures use fixed messages: invalid requests are 400, expired/missing
sessions 404, capacity 429 and unavailable observations 503. Lookup failures
remain unavailable rather than pretending the app is absent. Responses disable
HTTP caching and content sniffing. Provider/CLI stderr and database errors are
not returned to the browser.

## Identity, secrecy and retention

The backend captures the recorded deployment before inspecting replicas.
Compose replicas must match its exact run, project, service and generated
replica name; one-off containers are rejected. Standalone Docker identities
must match the recorded runtime name and run label. Reads address a full
immutable container ID, never a short ID or mutable name. Replica enumeration
is capped at 128. CLI operations have deadlines and bounded metadata output.

The accepted secret generation is checked before and after capture and on
subsequent page/download reads. Known values are redacted before search,
retention and download. This is value-based masking, not proof that arbitrary
unregistered secrets or transformed values are safe to log.

Sessions retain only redacted entries and deployment metadata in process
memory. Resolved credentials are not stored with them. Limits are 128 sessions,
16 MiB of estimated retained memory and four concurrent captures per process.
A session expires after ten minutes; expiry is enforced on access and expired
entries are purged on cache operations. Closing releases it; a restart loses
all sessions. This is not a durable log archive. Docker's own log rotation may
have already removed earlier output.

## Verification and rollout

The affected Go service/handler/router suites cover identity fences, generation
changes, redaction, paging, capacity, expiry, request limits and roles.
Frontend checks cover immutable identity, safe errors, authenticated download,
close handling, keyboard access and both accepted themes at narrow widths.
Actual Docker/official-server acceptance is recorded separately from fixture
tests under the local roadmap evidence directory. Consult its final manifest
and verification before claiming a live acceptance result.

These source changes do not activate the owner's localhost server, publish
Cooker, modify hosted projects or complete the full roadmap. A coherent release
build and owner acceptance remain separate rollout gates.
