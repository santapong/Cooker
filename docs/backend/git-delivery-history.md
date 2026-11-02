# Git delivery decision history

The App’s **Git deployment rules → Git delivery decisions** panel browses the durable authenticated inbox. Administrators can select this App or all incoming deliveries, search public source/receipt identities, filter status/provider/exact ref, and page through decisions. One delivery can have multiple rule decisions. The old “recent 100 intents” section is replaced; review approval remains on the same administrator API. Expand **Incoming webhook attempts** to inspect separate authenticated request attempts, including exact retries and rejected requests which never entered the inbox.

These reads inherit existing administrator and configured MFA requirements:

- `GET /api/v1/admin/trigger-deliveries`
- `GET /api/v1/admin/trigger-deliveries/:id/history`
- `GET /api/v1/admin/webhook-attempts`

List parameters are `appId`, `provider` (`github`, `gitlab`, `gitea`), `connectionId`, `status`, `ref` (exact full ref), `q`, `deliveryId`, `limit` (default 25, maximum 100), and `offset` (0–1,000,000). Search is a literal case-insensitive substring of public delivery/repository/ref/commit/rule/App/intent/job/run identities, capped at 256 characters. SQL applies all filters before pagination. Count and rows share a repeatable-read snapshot. Totals count **decisions**, not HTTP requests, Apps, successful deployments or unique deliveries.

The response contains explicitly selected identity fields, source ref/commit, safe reason codes/messages, timestamps, current intent/job/run status, and available receipt IDs. It excludes raw payloads, event/spec JSON, actor authority, private environments, authorization headers, plan/envelope digests, receipt digests, run variables/logs/errors, job payloads and private provider/preparation errors. Unknown stored reason text is replaced with a safe actionable explanation. Search does not inspect those private fields.

| Status | Meaning |
|---|---|
| accepted | Intent recorded pending validation, or durably queued; this is not rollout success |
| ignored | No matching active rule, exclusion, disabled rule, deletion or another recorded non-triggering event |
| duplicate | This delivery’s rule decision reused an existing logical intent; its original intent/job/run is inspectable |
| stale | The original intent was superseded when its source ref changed |
| review_required | Review is required before admission, or the queued job needs external outcome reconciliation |
| failed | Preparation failed, or the current linked job/run failed |
| rejected | A recorded rejected decision; current pre-admission HTTP rejections are outside this inbox |
| archived | A compacted delivery identity remains without reconstructable decisions |
| unknown | An unsupported historical lifecycle value, without guessing success/failure |

Current intent lifecycle is joined instead of replaying the inbox’s original pending state. Duplicate and ignored decisions preserve their independent meaning. A succeeded queue handler can still link to a failed rollout, so the run status participates in failure filtering. Available receipt identities are inspected in a disclosure; run links open the existing deployment/run page. Approval controls remain administrator-only. Imported Vercel flows remain read-only and are not authored Git-trigger pipelines.

## Prospective safe journal (migration 036)

New deliveries atomically persist minimal normalized decisions with their inbox/intents. The store derives App/environment/target identity from the exact locked rule generation, overriding caller-provided target claims. These records contain original rule/version, immutable target, initial outcome and fixed safe reason code, and the original logical intent reference. They survive rich inbox/spec compaction. Current intent/job/run lifecycle is still joined for inspection; a retained initial accepted decision is not a success verdict. Historical data without a normalized decision remains labelled legacy.

The new request journal begins **after signature/token authentication**. Every authenticated request receives an independent attempt ID before inbox lookup, including exact retries, supported validation failures and unsupported event types. Fixed reason/outcome codes record processing results. Exact retries reuse the original delivery and never re-admit deployment intents. Invalid transport identities have their invalid value omitted. The journal stores no raw body/header, private repository from a rejected payload, provider errors, credentials or body digest.

`GET /admin/webhook-attempts` supports `appId`, `provider`, `connectionId`, `status`, `q`, `deliveryId`, `limit` and `offset`, with the same pagination/search bounds as decisions. App filtering uses immutable retained decision targets through an existence check, so a delivery with several decisions does not multiply attempt totals. Rejected/unassigned requests appear under all incoming requests. Totals count **attempts**, independently from decision totals. The page exposes the persisted audit-schema start time; this is not proof of continuous handler coverage.

| Request status | Meaning |
|---|---|
| received | Processing began without a confirmed terminal audit result; a crash or interrupted completion is possible |
| accepted | The authenticated request entered the inbox; its rule decisions may all be ignored |
| ignored | Its HTTP event type does not trigger deployment |
| duplicate | An authenticated exact retry reused the original delivery |
| rejected | A fixed validation/identity/repository/install/tag check rejected processing |
| failed | Inbox lookup or tag verification was unavailable; this is request processing failure, not a rollout verdict |
| uncertain | Inbox admission could not be confirmed; no automatic replay or external outcome is inferred |

Beginning the journal is required before processing proceeds. Final audit completion has an independent three-second deadline so cancellation cannot silently erase the outcome. If completion cannot be persisted, HTTP503 reports an unconfirmed result and the durable received row remains. A later original-identity retry adds a new attempt and reuses any already committed inbox record. Terminal attempt outcomes are immutable.

## Coverage limits

1. Requests before this upgraded handler, requests handled during a legacy-version rollback, and legacy webhook routes are outside prospective coverage. No historical attempts or immutable targets are backfilled from current rule configuration.
2. Authentication failures, unavailable/disabled connections, and oversized requests rejected before authentication are not journaled as authenticated attempts. Their security telemetry is separate; no raw credentials are retained to manufacture a history entry.
3. Before cutover, retention may already have removed ignored decisions or logical duplicate references. Surviving legacy intents/receipts are inspected where available, but missing decisions remain unknown.
4. Legacy ignored decisions without immutable targets use the current rule’s App association, labelled current rule. Prospective decisions retain their original App/environment even when rules are edited or rich records compacted.
5. An interrupted attempt can remain received indefinitely. This is visibly unconfirmed rather than guessed failed/succeeded. A request processing result never proves external deployment success.

The prospective implementation fills safe authenticated request/retry visibility and minimal retained decision gaps. Broad historical completeness remains impossible for missing pre-cutover records. The candidate also retains explicit authored pipeline targets under GIT-03. `targetKind=pipeline&targetId=<id>` filters their decisions independently from App IDs; the native editor links the target pipeline and its run. Candidate UI/native source integration is verified separately from this prospective journal; actual hosted source-to-runtime acceptance remains open.

Verification for this slice is recorded separately from deployment acceptance in `/mnt/data/cooker-roadmap-20261002/delivery-browser`. The initial read/UI freeze is preserved independently from `journal-036`. Fixture/browser checks do not contact a real Git provider or create a deployment.
