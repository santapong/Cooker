# Automation setup and recovery

This is a development/UAT candidate. Read the [remaining release gates](../plans/2026-09-28-backend-implementation.md)
before enabling unattended deployment. The existing default demonstration stack
uses no-op execution backends.

## Prepare an installation

1. Back up PostgreSQL and the existing `COOKER_SECRET_KEY` separately. Retain the
   exact existing key; replacing it does not rotate stored ciphertext.
2. Apply normal Cooker migrations, including 028–031: queue recovery, durable
   stage logs, trigger inbox, and encrypted source connections. Start from a
   restored disposable backup when rehearsing this upgrade.
3. Configure PostgreSQL and `COOKER_JOBQUEUE_ENABLED=true`. App automation
   requires the durable queue. Manual deployment without this flag retains the
   older process-bound path and does not gain the new recovery guarantees.
4. Install Git 2.43 or newer in the source worker. Configure Docker CLI/Compose
   v2 for local Compose or the supported Kubernetes/cloud runtime independently.
   The combined Cooker image now includes Alpine's
   [Compose CLI plugin](https://pkgs.alpinelinux.org/packages?branch=v3.19&name=docker-cli-compose);
   the earlier image included Docker CLI but could not run `docker compose`.
5. For source builds, use the documented [BuildKit/Crane pair](buildkit-handoff.md).
   Remote production BuildKit requires mutual TLS. Keep stage outputs enabled.
6. Retain authentication, administrator/MFA boundaries and deployment policy.
   Provide an operator-controlled HTTPS webhook address. An App URL and the
   Cooker control-plane URL are different addresses.

This upgrade does not create a public domain, tunnel, Git account or cloud
project. It does not automatically enable existing Apps for deployment rules.

## Connect a repository and enable a rule

1. Open **Settings → Git connections → Connect source**. Choose GitHub, GitLab
   or Gitea and allow only the intended repository paths. A saved connection is
   configuration, not proof that the credentials can read that repository.
2. Use a configured GitHub App installation or a repository-scoped read token.
   GitLab/Gitea support an explicit HTTPS server origin. For a private server,
   configure its private-network policy and trusted CA; verification stays on.
3. Copy the displayed **`/webhooks/connections/<connection-id>`** URL into the
   repository's push webhook. Use the matching webhook secret/signing token.
   GitLab's signing and older secret-token modes are explicit; missing a
   signature never switches a signing connection into the weaker mode.
4. Import/review the Compose App using the connection. Select files, a target,
   a linked environment and the deployment prefix. Save remains separate from
   deployment. A full `refs/tags/...` selects a tag; a bare name selects a branch.
5. In **App → Git deployment rules**, save the environment keys each service
   may receive. Omitted services receive no linked keys. An empty map explicitly
   grants none. Put non-secret image/port interpolation inputs in BuildPlan
   variables; keep passwords in the environment secret store.
6. Choose branch or tag, exact names or patterns, exclusions and approval mode.
   `*` matches one slash-separated segment. `release/*` matches `release/v1`;
   `**` is not supported. Exclusions win.
7. **Preview rule**, inspect the services/files/commit, then **Approve
   configuration & save rule**. The server derives hashes and machine identity;
   browser state cannot grant authority. With governance enabled, the machine
   deployment identity also needs the appropriate existing delegation.
8. Send a test delivery and inspect **Recent Git changes**. A receipt links to
   the deployment. **Check for missed changes** uses the same deduplication
   contract. An HTTP 202 means durable acceptance, not a healthy application.

Ordinary source-code changes may proceed inside the approved configuration.
Changes to reviewed deployment files, Dockerfiles, App/environment/connection
versions or authority require reinspection. A delayed branch event never swaps
its source to a newer commit. Moved/re-annotated release tags need review.

## Migration from older webhooks

The previous per-App webhook URLs cannot authorize a reviewed machine
deployment. Eligible legacy requests now return **409
`source_connection_required`** and do not start a run. Some previously ignored
events still return an ignored response. Create a source connection and review
a rule explicitly, then replace the provider webhook URL. The old `autoDeploy`
boolean and encrypted per-App secret remain stored for compatibility; neither
silently grants the new rule authority. Bitbucket Server/DC has no complete new
source adapter yet; Bitbucket Cloud is a separate future provider.

## Configuration, logs and health

- Normal configuration reads show ordinary values and secret names. Resolved
  values do not enter immutable queue payloads or deployment-view graphs.
- Explicit service scope rejects build-time use of linked environment inputs.
  The first slice does not add a general build-secret feature.
- Stage logs are redacted before persistence/broadcast. PostgreSQL replay has
  retention/size limits and a visible gap when history is unavailable or expired.
- Docker runtime logs remain runtime streams. The executor stamps a run label;
  Cooker verifies it and follows the exact container ID rather than a service
  name that a rollout can reuse. Their redaction configuration comes from
  the recorded durable deployment, not an edited App. If that database
  environment generation was rotated or removed, Cooker refuses the old replay
  until a new reviewed deployment rather than treating new credentials as the
  complete old secret set. Legacy deployments without that record cannot use
  this verified log path. Missing retained queue input also disables replay.
  Supported external managers resolve the deployment's captured immutable
  version references, never their latest values. A missing pinned value refuses
  replay. Legacy external deployments need captured plain-only service scope;
  an empty current inventory cannot prove historical secret absence. See
  [immutable external-secret references](external-secret-versions.md).
  Database-backed secrets must match the recorded environment generation;
  storage-key rewrapping preserves that generation, unlike replacing a value.
- Kubernetes service log sessions are unavailable until the log adapter can
  bind a request to immutable pod identity. The capability endpoint reports
  this limitation; a pod-name/selector request alone is insufficient.
- Runtime reads accept only services in the App's recorded deployment. Docker
  requires an actual container health check for a healthy verdict; running
  without one remains unverified. Kubernetes checks desired/ready/updated
  replicas, the observed controller generation, and the recorded run label.
  Each health probe uses one deployment snapshot. Periodic local probes use
  `COOKER_APP_HEALTH_INTERVAL`; unsupported targets remain Unknown.

Masking is defense in depth for known resolved values. It cannot identify every
transformed, encoded, application-generated or externally rotated secret.

## Interrupted execution

Workers renew their durable claims. A lost lease, unexpected shutdown or
uncertain external mutation becomes **Needs attention** and retains the App's
deployment lock. It is not blindly retried.

An administrator inspects the actual runtime and records the verified result
with `POST /api/v1/admin/jobs/<job-id>/resolve`:

```json
{"outcome":"failed","evidence":"Verified the target has no new healthy workload; retained the previous deployment."}
```

Use the outcome and evidence that actually occurred. Allowed terminal outcomes
are `succeeded`, `failed`, and `cancelled`. This records an observed resolution;
it does not rerun the deployment. Then a new manual deployment can acquire the
App lock. The administrative job-read API intentionally omits the payload.

Stop new triggers by disabling their rules while investigating. Preserve the
database and run evidence. Do not downgrade the database while workers are
using the newer schema. Terminal notifications are best effort; the durable run
record remains the source of truth.

## Encryption-key recovery and rotation boundary

The database codec uses one configured AES-GCM key. Keep its backup separate
from the database, restrict access, and test restoration in an isolated
installation using a non-sensitive probe secret. A wrong/missing key must fail
decryption; never generate a replacement over unreadable existing ciphertext.

The candidate now includes an explicitly offline [key-rotation tool](offline-key-rotation.md).
It inventories all five managed ciphertext families, creates an authenticated
private before/after backup, rewraps in one locked transaction and can restore
exact original ciphertext if the complete expected inventory still matches.
Logical configuration/credential generations and notification sequence values
remain unchanged. Normal writes stay legacy until tagged-v1 is explicitly
selected; compatible readers must be activated before any tagged records.

Changing the configured key alone still makes existing ciphertext unreadable.
Keep the original key and full database backup separately. The tool does not
provide online multi-key rotation or prove owner installation/process-loss
recovery. Its previous-build rollback boundary requires original ciphertext and
key recovery; earlier fallback tests do not establish tagged-reader compatibility.
If the original key is lost, recover its secure backup or re-enter credentials
from their authoritative owners; run history cannot reconstruct them.

The [disposable backup/restore drill](backup-restore.md) has verified a
same-version PostgreSQL logical restore with fake encrypted environment and Git
credentials, unchanged ciphertext after wrong-key reads, and retained job/run/
log evidence. This is a repeatable fixture check; a production upgrade and
multi-key rotation still require their own installation-specific rehearsals.


## Vercel is the first provider integration

See [account connection and deployment synchronization](vercel-sync.md). Migration
032, PostgreSQL and an active installation encryption key are required. This
read-only integration is independent of queue and AWS/GCP inventory switches.


For a local-auth frontend, also build with `VITE_LOCAL_AUTH_ENABLED=true`.
Keep `VITE_OIDC_ENABLED=false` when no OIDC provider is configured. The
candidate production Compose stack pairs this frontend flag with its existing
local-auth backend; UAT supports opting into both flags. Omitting the frontend
flag with an authenticated local backend previously initialized a development
user and caused a sign-in redirect loop. Pure development mode with both
interactive auth flags disabled keeps its existing behavior.
