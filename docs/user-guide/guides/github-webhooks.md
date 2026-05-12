# GitHub webhooks

Cooker can auto-deploy an [App](../concepts/apps.md) on every push to a configured branch. Push events arrive at `/webhooks/github`, Cooker verifies the HMAC signature, looks up the App by `owner/name + branch`, and kicks off a deploy.

This page covers wiring a single App. For org-wide bulk wiring, see roadmap `D12` (bulk import — not shipped yet).

## What it does

```text
   GitHub push ─► /webhooks/github ─► HMAC verify ─► Lookup App by repo+branch
                                                       │
                                                       ▼
                                                   AutoDeploy?
                                                       │
                                                       ├─ true ─► Spawn run
                                                       └─ false ─► 202 Accepted (no-op)
```

## Step 1 — Set the App's webhook secret

Pick a strong random string and seal it to the App:

```bash
curl -X PUT https://cooker.example.com/api/v1/apps/<APP_ID>/webhook \
     -H 'Authorization: Bearer <jwt>' \
     -H 'Content-Type: application/json' \
     -d '{"secret":"REPLACE_ME_WITH_A_LONG_RANDOM_STRING"}'
```

Endpoint:

- Path: `PUT /api/v1/apps/:id/webhook`
- Role: `admin` (with step-up MFA if `COOKER_OIDC_MFA_ACR_VALUES` is configured)
- Stores: `App.WebhookSecret` (AES-GCM sealed via `Codec` before persisting)

The plaintext value is what you'll paste into GitHub. Cooker never returns it back to you — keep your own copy until you've configured GitHub.

## Step 2 — Enable AutoDeploy on the App

```bash
curl -X PUT https://cooker.example.com/api/v1/apps/<APP_ID> \
     -H 'Authorization: Bearer <jwt>' \
     -H 'Content-Type: application/json' \
     -d '{"autoDeploy":true}'
```

Or toggle it in the UI's App detail page (the toggle exists; the **webhook URL** is not yet surfaced next to it — see W11 indie gap and roadmap `D9`).

## Step 3 — Configure the GitHub webhook

GitHub repo -> **Settings -> Webhooks -> Add webhook**.

| Field | Value |
|---|---|
| Payload URL | `https://cooker.example.com/webhooks/github` |
| Content type | `application/json` |
| Secret | The exact value you `PUT` in step 1 |
| Which events | "Just the push event" |
| Active | Yes |

GitHub will send a `ping` immediately. Cooker responds `202 Accepted` to pings and any push event; you can confirm in the GitHub UI's "Recent Deliveries" list.

> **TLS required.** `Payload URL` must be HTTPS in production. GitHub will deliver to HTTP for self-signed certs only if you tick "Disable SSL verification" — never do that for Cooker.

## Step 4 — Test it

Push a commit to the configured branch. Within a few seconds, the Cooker UI shows a new run on the App detail page.

To test without an actual git push, forge a request locally:

```bash
BODY='{"ref":"refs/heads/main","after":"abc1234","repository":{"full_name":"your/repo"}}'
SECRET='REPLACE_ME_WITH_A_LONG_RANDOM_STRING'
SIG="sha256=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac "$SECRET" | awk '{print $2}')"

curl -X POST https://cooker.example.com/webhooks/github \
     -H 'X-GitHub-Event: push' \
     -H "X-Hub-Signature-256: $SIG" \
     -H 'Content-Type: application/json' \
     -d "$BODY"
```

Expected response: `202 Accepted` with `{"status":"deploy queued"}`.

A wrong signature gets `401 Unauthorized`.

## What gets verified

Per `backend/internal/source/github/webhook.go:38`:

- **Signature.** `X-Hub-Signature-256` is constant-time-compared (`hmac.Equal`) against `HMAC-SHA-256(body, App.WebhookSecret)`.
- **Branch match.** Only push events to `App.Branch` trigger a deploy. Pushes to other branches return 202 without doing anything.
- **Deleted-branch detection.** `deleted=true` and zero-SHA `after` values are rejected (per `handler/app.go:300-306`).

## Idempotency

`/webhooks/github` is wrapped in the idempotency middleware. The `X-GitHub-Delivery` header is captured as the idempotency key, so when GitHub retries (which it does on a 5xx), Cooker replays the original response rather than spawning a second deploy.

## Rate limiting

The `/webhooks/github` endpoint is **not** rate-limited at the application layer today. The HMAC check is constant-time, so brute-force isn't a real vector — but an unauthenticated flood of large bodies is. The 10 MiB body limit applies (`handler/app.go:268-280`), but rate limiting at the ingress / WAF is the right defense for public exposure. Tracked as `S26-05-22` in the [security review](../../audits/2026-05-security-review.md).

## Webhook secret rotation

Replace the secret atomically:

1. Generate a new value.
2. `PUT /api/v1/apps/:id/webhook` with the new value (this re-seals it).
3. Paste the new value into GitHub's webhook config.

There is no overlap window — once you `PUT`, the old value is rejected immediately. For a high-traffic webhook you may want to update GitHub first (where the rate limit makes mismatches recoverable) and Cooker second.

> **No bulk rotation.** For N apps, you rotate N times. Roadmap `D14` tracks a bulk endpoint.

## Troubleshooting

| Symptom | Cause |
|---|---|
| `401 Unauthorized` | Signature mismatch — wrong secret, or you forgot to set one. |
| `404 Not Found` | No App matches `owner/name + branch`. Check `GET /api/v1/apps`. |
| `202 Accepted` but no run | `App.AutoDeploy=false` — the webhook arrived, Cooker just didn't trigger anything. |
| `503 Service Unavailable` | The codec isn't initialised — `COOKER_SECRET_KEY` is missing. See [Login loop](../troubleshooting/login-loop.md) for the boot-time symptom set. |

## Cross-references

- **[Apps](../concepts/apps.md)** — the App model.
- **[Reference: Webhooks](../reference/webhooks.md)** — payload schema and supported events.
- **[Security: HMAC verification](../../../SECURITY.md#network-security)** — the trust boundary.
