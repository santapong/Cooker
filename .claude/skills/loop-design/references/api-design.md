# API Design

The contract is the most expensive thing to change, because every consumer is coupled to it. Design it first, make the defaults boring, and put every non-default behind a stated reason. **The failure mode this reference exists to prevent is a contract that can't evolve** — a versioning scheme that forces a big-bang migration, pagination that breaks under concurrent writes, or an error shape every client special-cases. Get these right once and additive change stays cheap forever.

Defaults below are prescriptive; each has an escape hatch that names the requirement that justifies leaving the default. When a protocol choice, versioning scheme, or auth model is load-bearing, record it as an ADR (`../templates/adr-template.md`) — name the alternative you rejected.

## 1. Protocol: REST vs GraphQL vs gRPC

**Default: REST over HTTP/JSON.** It is resource-oriented, cacheable through standard HTTP semantics, debuggable with `curl`, and understood by every client and proxy. Reach past it only when a named consumer need forces it.

| Dimension | REST | GraphQL | gRPC |
|---|---|---|---|
| Best consumer | Public/partner APIs, CRUD resources | Many client types with wildly varying read shapes | Internal service-to-service |
| Read shape | Fixed per endpoint | Client-selected per query | Fixed, strongly typed |
| Over/under-fetching | Common (mitigate with sparse fields/expansion) | Eliminated by design | N/A (contract-shaped) |
| Caching | Free via HTTP (CDN, ETags) | Hard (POST, per-query) | None built in |
| Latency/throughput | Good | Good; risk of N+1 resolvers | Best (HTTP/2, binary Protobuf) |
| Streaming | Awkward (SSE/WebSocket bolt-on) | Subscriptions | First-class (bidi streams) |
| Browser reach | Native | Native | Needs grpc-web + proxy |
| Tooling/skills | Universal | Moderate, growing | Codegen-heavy, less human-readable |

**Selection rubric — walk it in order:**

1. **Is the consumer another internal service, latency/throughput-critical, and not a browser?** → gRPC. You get codegen, tight contracts, and HTTP/2 streaming. Cost: put a gateway in front for any browser/public traffic.
2. **Do many client types (web, iOS, Android, partners) each need very different slices of the same graph, and is over/under-fetching a real cost (mobile bandwidth, screen-specific aggregation)?** → GraphQL, one flexible endpoint. Cost: you now own query-cost rate limiting (§9), N+1 resolver batching, and lost HTTP caching.
3. **Otherwise** → REST. This covers the large majority of public and partner APIs.

Don't run all three. A common healthy topology is gRPC between internal services, fronted by a REST or GraphQL edge for external consumers (§10). See `architecture-patterns.md` for where these service boundaries fall.

## 2. Resource modeling (REST)

Model **nouns, not verbs**, around the domain aggregates from `backend.md` — one aggregate root maps to one top-level resource.

- Collections are plural; items live under them: `GET /orders`, `GET /orders/{id}`, `GET /orders/{id}/items`.
- Use opaque, stable IDs (UUID/ULID). **Never expose DB auto-increment keys** — they leak volume and invite enumeration.
- Nest sub-resources one level deep at most. Deeper nesting (`/customers/{c}/orders/{o}/items/{i}`) couples URLs to a hierarchy that will change; prefer `/orders/{o}/items/{i}` and let the order carry its customer.
- **State transitions that aren't CRUD get explicit action sub-resources**, not verbs smeared into the path: `POST /orders/{id}/cancel`, `POST /payments/{id}/capture`. Escape hatch: when the transition is itself a durable thing worth listing/auditing, model it as a resource (`POST /orders/{id}/refunds`).
- Relationships default to **link, with opt-in expansion** (`?expand=customer,items`) rather than always embedding — keeps payloads small and avoids fetching graphs nobody asked for.

## 3. Versioning — path-based (default)

**Default: major version in the path, `/v1/...`.** It is visible in every log and cache key, trivially routable at the gateway, and lets you run `/v1` and `/v2` side by side during migration.

- **Only breaking changes bump the version.** Additive changes — new optional fields, new endpoints, new enum values clients are told to tolerate — ship into the current version without a bump. Design responses so clients ignore unknown fields.
- Never make the version a query param (`?version=2`) as the primary scheme — it pollutes caches and is easy to drop.
- **Deprecation is a process, not an event**: announce with a `Deprecation` and `Sunset` header on responses, run N and N−1 concurrently for a stated window, and give partners a migration guide before you remove anything.

**Escape hatch — header/media-type versioning** (`Accept: application/vnd.acme.v2+json`): use only when you must version *individual resources independently* or keep URLs permanently stable (deep-linked, bookmarked). Cost: versioning becomes invisible in URLs and logs, and every cache must vary on `Accept`.

## 4. Pagination — cursor / keyset (default)

**Default: opaque cursor (keyset) pagination.** It is stable under concurrent inserts and deletes — offset pagination silently skips or repeats rows when the underlying set shifts between pages, which corrupts exports and infinite scroll.

Request: `GET /orders?limit=50&cursor=eyJjIjoi...`
Response:

```json
{
  "data": [ { "id": "ord_01H...", "created_at": "2026-06-30T10:00:00Z" } ],
  "page": { "next_cursor": "eyJjIjoi...", "has_more": true }
}
```

- The cursor is **opaque** to clients — base64 of the last row's sort key(s), e.g. `(created_at, id)`. Clients treat it as a token, never parse it.
- Mechanics: `WHERE (created_at, id) < (:c, :id) ORDER BY created_at DESC, id DESC LIMIT :n+1`. The composite tiebreaker guarantees total ordering; every sortable column combination must be index-backed (`backend.md`).
- **Cap `limit`** (default 50, max ~100–200) and always return `has_more` — never make clients infer "done" from a short page.

**Escape hatch — offset pagination** (`?page=3&per_size=25`): acceptable only for small, static, human-browsed datasets where users need "jump to page N." Never use offset for large, mutating, or infinite-scroll data.

## 5. Filtering & sorting conventions

- **Filter** via whitelisted query params: `?status=open&created_after=2026-01-01`. Expose only fields you've explicitly opted in — arbitrary column filtering invites injection and unindexed table scans. Structured operators go in bracket form when needed: `?amount[gte]=100`.
- **Sort** via a single param with `-` for descending: `?sort=-created_at,name`. Every sortable field must be index-backed (§4).
- **Sparse fieldsets** (`?fields=id,status,total`) and **expansion** (`?expand=customer`) let clients trim or grow payloads without a schema change.
- When a query needs more than a handful of params or boolean logic, stop cramming the URL — offer `POST /orders/search` with a JSON body. It sacrifices GET cacheability for expressiveness; make that trade deliberately, not by accident.

## 6. Idempotency keys on mutations

Retries are inevitable (timeouts, proxies, mobile radios). **Require an `Idempotency-Key` header on every non-idempotent mutation** — `POST` creates, `PATCH`, and any action endpoint. Non-negotiable for payments and any at-least-once caller.

- The server stores `key → (status, response)` scoped to endpoint + auth principal for a window (24h is typical) and **replays the stored response** on any retry with the same key.
- Same key, different request body → `409 Conflict` (or `422`): the caller is misusing the key.
- `PUT` and `DELETE` are idempotent by definition; `GET`/`HEAD` are safe. You still key `PATCH` because it usually isn't idempotent.

```http
POST /v1/payments
Idempotency-Key: 9f1c2b3a-...
Content-Type: application/json

{ "amount": 4200, "currency": "usd", "source": "card_..." }
```

A retry with the same key returns the identical `201` and payment object — the charge happens once. See `backend.md` for the outbox pattern that makes the write-plus-side-effect atomic.

## 7. Error envelope & status codes

Return **one error shape everywhere** so clients write one handler. Machine-readable `code` (stable string, safe to branch on), human `message`, optional per-field `details`, and a `request_id` for support/trace correlation.

```json
{
  "error": {
    "code": "validation_failed",
    "message": "The order could not be created.",
    "details": [ { "field": "items", "issue": "must not be empty" } ],
    "request_id": "req_01H..."
  }
}
```

RFC 9457 `application/problem+json` is a fine standard alternative — pick one and hold it.

**Status codes — mean them:**

| Code | Use for |
|---|---|
| 200 / 201 / 204 | OK / created / no content |
| 202 | Accepted for async processing (return a job resource to poll) |
| 400 | Malformed request (unparseable, missing required) |
| 401 | Not authenticated (no/invalid credentials) |
| 403 | Authenticated but not authorized |
| 404 | Resource not found (or hidden for authz reasons) |
| 409 | Conflict (idempotency-key reuse, version/edit conflict) |
| 422 | Well-formed but semantically invalid (validation) |
| 429 | Rate limited (with `Retry-After`) |
| 5xx | Server fault — never leak stack traces |

**Never return `200` with an error body.** It defeats every proxy, retry policy, and monitor. `4xx` = the client must change the request; `5xx` = the server failed and a retry may help. Always attach `request_id` and wire it to your traces (`nfr.md` observability).

## 8. Bulk endpoints

When callers need many writes, a bulk endpoint cuts round-trips instead of hammering the single-item route. Offer `POST /orders/batch` with an array, and **decide partial-success semantics explicitly**:

```json
{
  "results": [
    { "index": 0, "status": 201, "id": "ord_01H..." },
    { "index": 1, "status": 422, "error": { "code": "validation_failed" } }
  ]
}
```

- Return **per-item status** (a multi-status body) unless the batch is genuinely transactional and all-or-nothing — say which it is.
- Cap batch size and carry one `Idempotency-Key` for the whole batch (§6).
- **Escape hatch — large or slow bulk goes async**: accept the job, return `202` plus a job resource the client polls (`GET /jobs/{id}`). Don't hold an HTTP connection open for a 10-minute import.

## 9. Rate limiting

**Default: token bucket per principal** (API key → user → IP as fallback), enforced at the gateway (§10) so services stay clean. Communicate limits so good clients can back off:

```http
HTTP/1.1 429 Too Many Requests
Retry-After: 30
RateLimit-Limit: 1000
RateLimit-Remaining: 0
RateLimit-Reset: 30
```

- Tier limits by auth tier (anonymous < keyed < paid partner).
- **GraphQL is rate-limited by query cost/complexity, not request count** — one request can ask for the world. Assign field costs and budget per window.
- Pair with a global concurrency/circuit breaker so one abusive caller can't starve the rest (`nfr.md` resilience).

## 10. Gateway & BFF

**API gateway at the edge** owns the cross-cutting concerns — TLS termination, authentication, rate limiting, routing, request logging — keeping them out of every service. It routes to REST/GraphQL/gRPC backends behind it. **Keep business logic out of the gateway**; it's a policy layer, not an application (`architecture-patterns.md`).

**BFF (backend-for-frontend)**: a per-client-type aggregation layer (a web BFF, a mobile BFF) that composes several downstream services and shapes the response for exactly that client. Reach for it when multiple clients need very different response shapes and you've decided against GraphQL. Escape hatch: **one client → no BFF**, and don't spawn a BFF per micro-frontend — that's just distribution tax. GraphQL and a BFF solve overlapping problems; pick one for a given surface.

## 11. HATEOAS — when

Full hypermedia (`_links` everywhere, clients navigating purely by relations) is **over-engineering for the common case**: a known client against a documented API is better served by stable URLs and good docs. Include links only where the server genuinely drives client behavior:

- **State-dependent actions**: return the actions currently valid for a resource's state, so clients don't hard-code the state machine.
- **Pagination** `next` (§4) — the one hypermedia link everyone should ship.

```json
{
  "id": "ord_01H...",
  "status": "pending",
  "_links": {
    "self":   { "href": "/v1/orders/ord_01H..." },
    "cancel": { "href": "/v1/orders/ord_01H.../cancel", "method": "POST" }
  }
}
```

When the order ships, the `cancel` link disappears and a `return` link appears — the client renders available actions without owning the transition rules. Adopt this **only** for genuine workflow/state-machine resources (checkout, approvals, fulfillment); skip it elsewhere.

## 12. Auth — OAuth2/OIDC, API keys, mTLS

Always over TLS, always least-privilege scopes. Match the mechanism to the caller:

- **User-facing / third-party → OAuth2 + OIDC (default).** Authorization Code + **PKCE** for user apps; **Client Credentials** for service-to-service; **OIDC** when you also need identity/login. Issue **short-lived access tokens** (JWT or opaque) plus refresh tokens; scopes carry coarse authorization.
- **First-party server-to-server / simple partner access → API keys.** Treat them as secrets: scope them, support rotation, and **never put them in URLs** (they leak into logs and history) — use `Authorization: Bearer` or a dedicated header. API keys authenticate a *client*, not a *user*.
- **Internal service-to-service / zero-trust / high-assurance → mTLS.** Both ends present certs; pair with workload identity (e.g. SPIFFE) so service identity is cryptographic, not a shared secret. Warranted when the traffic moves money or crosses a trust boundary.

**Validate tokens at the gateway (§10)** and propagate the verified identity downstream (signed header or mTLS-asserted). Scopes gate coarse access at the edge; **resource-level authorization ("can *this* user touch *this* order?") lives in the service** — the gateway can't know it. Security review lives in the `loop-review` skill; NFR security posture in `nfr.md`.

## Record it

Emit an ADR (`../templates/adr-template.md`) when the choice is hard to reverse: **protocol** (REST/GraphQL/gRPC), **versioning scheme**, and **auth model** are the usual load-bearing three. Context → decision → alternatives rejected → consequences. The rejected alternative is the part your successor will thank you for.
