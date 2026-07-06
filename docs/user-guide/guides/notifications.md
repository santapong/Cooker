# Notifications

Cooker sends outbound notifications when a run, deploy, or canary
finishes. Configure destinations ("targets") under **Settings →
Notification targets** (admin only), or via the
`/api/v1/admin/notification-targets` API.

Notifications are **always on** — they work whether or not the async
job queue (`COOKER_JOBQUEUE_ENABLED`) is enabled. An install with no
targets configured simply sends nothing.

## Supported channels

| Channel | `kind` | Config shape |
|---|---|---|
| **Email (SMTP)** | `email` | `{"host":"smtp.example.com","port":587,"username":"…","password":"…","from":"cooker@example.com","to":"oncall@example.com"}` |
| **Generic webhook** | `webhook` | `{"url":"https://api.example.com/notify","bearerToken":"…"}` |
| Slack | `slack` | `{"webhookUrl":"https://hooks.slack.com/services/…"}` |
| Discord | `discord` | `{"webhookUrl":"https://discord.com/api/webhooks/…"}` |

**Email and generic webhook are the supported, documented channels.**
Slack and Discord adapters ship and work, but are less exercised — treat
them as available rather than first-class for now.

## Events

| Event | Fires when |
|---|---|
| `run.failed` / `run.succeeded` / `run.cancelled` | a pipeline run reaches that terminal state |
| `deploy.failed` / `deploy.succeeded` | an app deploy finishes |
| `build.failed` | a deploy's build stage fails |
| `canary.promoted` / `canary.aborted` / `canary.failed` | a canary rollout transitions |

### Default filter avoids alert fatigue

A new target with **no** event filter subscribes to the **failure /
state-change set** — `run.failed`, `deploy.failed`, `build.failed`,
`canary.failed` — not every event. Firing on every green run is how
teams end up muting the channel and then missing the failure it existed
for. To receive success events too, tick them explicitly when creating
the target (or pass them in `eventTypes`).

## Credentials at rest

Target configs carry secrets (the SMTP password; a Slack/Discord/webhook
URL is itself a bearer credential). When `COOKER_SECRET_KEY` is set,
Cooker encrypts the config column at rest with AES-GCM — the same codec
as the database secrets backend. Set `COOKER_SECRET_KEY` in any
install that stores real credentials. Without it, configs are stored in
plaintext and the server warns at boot.

## Example: create a webhook target via the API

```bash
curl -X POST "$COOKER_URL/api/v1/admin/notification-targets" \
  -H "Authorization: Bearer $COOKER_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "ops-webhook",
    "kind": "webhook",
    "config": {"url": "https://ops.example.com/hook"}
  }'
# eventTypes omitted → subscribes to the failure set.
```

## Status checks back to GitHub

Writing the run's success / failure back to the commit as a GitHub
Commit Status is **planned** (the highest-voted item in this area) but
not yet shipped. The inbound webhook-receive path works today (see
[GitHub webhooks](github-webhooks.md)).

## Fallback: notify an unsupported sink from a pipeline

For a destination Cooker doesn't have an adapter for (PagerDuty, Teams,
SMS, …), add a **Custom stage** at the end of the pipeline that `curl`s
it, connected with `success` / `failure` edge conditions:

```yaml
type: custom
config:
  image: curlimages/curl:latest
  command:
    - sh
    - -c
    - |
      curl -X POST https://events.pagerduty.com/v2/enqueue \
        -H 'Content-Type: application/json' \
        -d "{\"routing_key\":\"$PD_KEY\",\"event_action\":\"trigger\",
             \"payload\":{\"summary\":\"Cooker: $REPO@$COMMIT_SHA failed\",
             \"severity\":\"error\",\"source\":\"cooker\"}}"
  secretRefs:
    - PD_KEY
```

Store the key as an [Environment secret](secrets.md) and reference it in
`secretRefs`.

## Cross-references

- **[Stages: Custom](../concepts/stages.md#custom-stagetypecustom)** — the fallback mechanic.
- **[Secrets](secrets.md)** — storing credentials.
- **[Security: notification-target configs](../../../SECURITY.md#credential-handling--notification-target-configs)** — how configs are encrypted at rest.
