# Notifications

> **Partial — read this first.** Cooker does NOT yet ship first-party notification sinks (Slack, Discord, Teams, email, PagerDuty). The roadmap tracks them as `A7` and `A8`. What works today is the **status surface** — anything that wants to know about a run can poll the API or subscribe to the WebSocket. This page documents both the gap and the workaround.

## What exists today

- **Run status over WebSocket.** `/ws/pipeline-run/:runId` streams status changes; `/ws/runs/:runId/stages/:stageId/logs` streams per-stage logs.
- **REST polling.** `GET /api/v1/pipelines/:id/runs` and `GET .../runs/:runId` return current status.
- **Audit log.** Every authenticated mutating call produces a structured event (see [Audit logging](../../../SECURITY.md#audit-logging)). This is for compliance, not user-facing notifications.

## What doesn't yet

- No outbound notifier interface (`internal/notifier/` doesn't exist).
- No Slack / Discord / Teams / email / SMS / PagerDuty / webhook sink.
- No "notify when run fails" config on a pipeline.

If you need these, the workaround is a **Custom stage** at the end of your pipeline (or a `failure`-edge cleanup) that `curl`s your chosen sink.

## Workaround: Slack

Add a Custom stage to the pipeline. Connect from the last business stage with both `success` and `failure` edge conditions, or two separate Custom stages.

```text
   Deploy-Prod ──(success)──► Notify-Success
              ──(failure)──► Notify-Failure
```

Notify-Success config:

```yaml
type: custom
config:
  image: curlimages/curl:latest
  command:
    - sh
    - -c
    - |
      curl -X POST $SLACK_WEBHOOK_URL \
        -H 'Content-Type: application/json' \
        -d "{\"text\":\":white_check_mark: Deploy of $REPO@$COMMIT_SHA to prod succeeded\"}"
  secretRefs:
    - SLACK_WEBHOOK_URL
```

Store `SLACK_WEBHOOK_URL` as an [Environment secret](secrets.md). Reference it in `secretRefs` so the executor injects it as an env var into the Custom stage.

## Workaround: Discord

Same pattern; Discord's webhook URL accepts the same Slack-shaped payload via the `/slack` suffix:

```bash
curl -X POST $DISCORD_WEBHOOK_URL/slack \
  -H 'Content-Type: application/json' \
  -d "{\"text\":\":fire: Deploy failed: $REPO@$COMMIT_SHA\"}"
```

## Workaround: Email

There's no SMTP code in Cooker. Use a service:

- **SendGrid / Postmark / Mailgun / SES**: `curl` their API from a Custom stage.
- **Internal SMTP relay**: a small image like `bytemark/smtp` runs the relay; your Custom stage `curl`s an HTTP-to-SMTP shim or runs `mailx` against it.

The Custom stage idiom is the same — POST to an HTTP endpoint with the run context in the body.

## Workaround: PagerDuty

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
        -d "{
          \"routing_key\": \"$PAGERDUTY_INTEGRATION_KEY\",
          \"event_action\": \"trigger\",
          \"payload\": {
            \"summary\": \"Cooker deploy failed: $REPO@$COMMIT_SHA\",
            \"severity\": \"error\",
            \"source\": \"cooker\"
          }
        }"
  secretRefs:
    - PAGERDUTY_INTEGRATION_KEY
```

## What variables are available to Notify stages

The executor injects these env vars into every stage's runtime:

| Variable | Source |
|---|---|
| `${COMMIT_SHA}` | The git revision the run was built from. |
| `${BRANCH}` | The branch the run was built from. |
| `${REPO}` | `owner/name` of the GitHub repo. |
| `${RUN_ID}` | Run UUID. |
| `${PIPELINE_ID}` | Pipeline ID. |
| `${STAGE_NAME}` | Current stage name. |
| Pipeline / Environment variables | Anything you defined on Pipeline.Variables or Environment.PlainVars. |
| Secrets referenced via `secretRefs` | The plaintext value. |

> **TODO: verify** the exact list of executor-injected variables — the code lives under `internal/service/executor.go` but the documented contract is not yet in `docs/design.md`. Treat this list as best-effort. <!-- TODO: verify -->

## Status checks back to GitHub

Cooker can write the run's success / failure as a GitHub Commit Status, so PRs in GitHub see the green check or red X.

> **Partial.** Roadmap `A1` flags this as "needs flesh-out — branch/path filters, status checks back to GitHub." Today the webhook-receive path works (see [GitHub webhooks](github-webhooks.md)); writing status back is not yet implemented.

## When notifications land

When roadmap `A7` ships, expect a new `internal/notifier/` package with a `Notifier` interface, a `selectNotifier`-style registration in `server.go`, and chart values like `notifier.slack.webhookUrlSecret`. The workaround Custom stages will continue to work; the notifier framework will reduce the boilerplate.

## Cross-references

- **[Stages: Custom](../concepts/stages.md#custom-stagetypecustom)** — the workaround mechanic.
- **[Secrets](secrets.md)** — storing the webhook URL.
- **[Roadmap A7](https://github.com/cooker-ci/cooker/blob/main/docs/roadmap-2026.md#a-integrations)** — when proper notifiers will land.
