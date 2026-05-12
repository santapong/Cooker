# Webhooks

Cooker today supports **one inbound webhook**: GitHub's `push` event for App auto-deploy. Outbound notifier webhooks are not yet implemented — see [Notifications](../guides/notifications.md) and roadmap `A7`.

## Endpoint

```text
POST /webhooks/github
```

**Authentication:** HMAC-SHA-256 signature in `X-Hub-Signature-256`. Computed against the request body using the per-App webhook secret.

**Idempotency:** the `X-GitHub-Delivery` header is captured by the idempotency middleware. A retry from GitHub replays the original response (no duplicate deploy).

**Rate limit:** none at the app layer today (`S26-05-22`). Enforce at the ingress.

## Headers Cooker reads

| Header | Required | Purpose |
|---|---|---|
| `X-GitHub-Event` | yes | Must be `push` (other events return 202 with no-op). |
| `X-Hub-Signature-256` | yes | `sha256=<hex>` HMAC-SHA-256 of the body. |
| `X-GitHub-Delivery` | yes | Idempotency key. |
| `Content-Type` | yes | `application/json`. |

## Payload schema (subset Cooker reads)

```json
{
  "ref": "refs/heads/main",
  "after": "abc1234567890abcdef1234567890abcdef12345",
  "before": "f00ba12...",
  "deleted": false,
  "repository": {
    "full_name": "owner/repo"
  }
}
```

Cooker reads:

- **`ref`** — extracts the branch name (`refs/heads/<branch>`). Must match `App.Branch`.
- **`after`** — the new commit SHA. Used as `${COMMIT_SHA}` in build / push tags. Rejected if zero (`0000...0000`).
- **`deleted`** — rejected if true (branch deletions don't trigger deploys).
- **`repository.full_name`** — `owner/repo`. Looked up in `apps.github_repo`.

All other fields in GitHub's push payload are ignored.

## Verification flow

```text
   POST /webhooks/github
        │
        ▼
   Check X-Hub-Signature-256 present ──── no ──► 401
        │
        ▼
   Lookup App by repo.full_name + ref's branch ── no match ── 404
        │
        ▼
   Open App.WebhookSecret via Codec ──── codec error ──► 503
        │
        ▼
   hmac.Equal(SHA-256(body, secret), provided signature)
        │
        ├── mismatch ──► 401 Unauthorized
        │
        └── match
              │
              ▼
        deleted=true OR after=zero ──► 400 (reject)
              │
              ▼
        App.AutoDeploy
              │
              ├── false ──► 202 (no-op, "autoDeploy off")
              │
              └── true
                    │
                    ▼
              Spawn deploy run ──► 202 {"status":"deploy queued","runId":"..."}
```

Source: `backend/internal/handler/app.go:260-340` and `backend/internal/source/github/webhook.go`.

## Setting the secret

```bash
curl -X PUT https://cooker.example.com/api/v1/apps/<APP_ID>/webhook \
     -H 'Authorization: Bearer <jwt>' \
     -H 'Content-Type: application/json' \
     -d '{"secret":"REPLACE_ME"}'
```

Endpoint: admin only (with MFA gate if configured). The secret is sealed via `Codec` before persisting; the plaintext is never returned.

Walkthrough in [GitHub webhooks](../guides/github-webhooks.md).

## Testing without GitHub

Forge a delivery locally:

```bash
BODY='{"ref":"refs/heads/main","after":"abc1234","repository":{"full_name":"your/repo"},"deleted":false}'
SECRET='your-webhook-secret'
SIG="sha256=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac "$SECRET" | awk '{print $2}')"

curl -X POST https://cooker.example.com/webhooks/github \
     -H 'X-GitHub-Event: push' \
     -H "X-Hub-Signature-256: $SIG" \
     -H "X-GitHub-Delivery: $(uuidgen)" \
     -H 'Content-Type: application/json' \
     -d "$BODY"
```

Expected: `202 Accepted` with `{"status":"deploy queued"}`.

## What Cooker does NOT do today

- **No `pull_request` event.** Only `push`. PR-preview environments are roadmap (W11 indie persona).
- **No status check write-back to GitHub.** Cooker's run status doesn't post a Commit Status to GitHub — green check / red X on the commit. Roadmap `A1`.
- **No GitLab / Bitbucket / Gitea webhook adapters.** Roadmap `A2` / `A3`.
- **No outbound webhooks.** No "POST to this URL when a run fails" config. Use the [notifications workaround](../guides/notifications.md).

## Body size limit

10 MiB. Bigger bodies are rejected with `413 Payload Too Large` (per `handler/app.go:268-280`).

## What's NOT verified

- **Source IP.** Cooker doesn't compare against GitHub's published webhook IP ranges (it relies on HMAC). If you want to enforce IP allowlists, do it at the ingress controller.
- **Replay window.** A captured request with a valid signature can be replayed forever. The idempotency middleware catches repeated `X-GitHub-Delivery` values within 5 minutes, but a request with a fresh delivery ID and a stale `after` SHA will re-deploy the same commit. Mitigation: rotate `App.WebhookSecret` periodically; the old signature stops verifying immediately.

## Cross-references

- **[GitHub webhooks guide](../guides/github-webhooks.md)** — end-to-end walkthrough.
- **[Apps](../concepts/apps.md)** — App.AutoDeploy and the synthesised run.
- **[`backend/internal/source/github/webhook.go`](https://github.com/cooker-ci/cooker/blob/main/backend/internal/source/github/webhook.go)** — the verification source.
