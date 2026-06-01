# Cooker custom protocols — design proposal

> Status: **proposal, not approved.** Three candidate Cooker-native protocols, scoped against the current REST + WebSocket + JSON baseline. Written 2026-05.
> Cross-references `docs/audits/2026-05-perf-and-optimization.md` (Wave 1 perf audit) by ID. Pairs with the in-flight `docs/roadmap-2026.md` "Extend it" theme — this doc owns the transport/protocol design surface only.
> No code changes. No commitments.

---

## 0. Premise — where the standards are already right

Cooker today uses three transport idioms; for each, the question is whether a Cooker-native replacement actually wins.

| Layer | Today | Verdict |
|---|---|---|
| Public API (CRUD on pipelines, runs, envs, secrets, apps) | HTTP/1.1 + JSON, OIDC bearer, OpenAPI 3 at `docs/openapi.yaml`. Handlers at `backend/internal/handler/*.go`. | **Keep.** Browsers, `curl`, Postman, Terraform providers, every CI tool, every SDK generator targets this. The cost of swapping is enormous; the benefit (per-request latency under a millisecond in the kernel-to-handler path) is unmeasurable to a user. See §8 for the explicit "won't do gRPC". |
| Real-time browser updates (run status, build logs, K8s watch) | WebSocket, text frames, JSON-or-raw envelope. `backend/internal/server/websocket.go:153` → `Broadcast(channel, data []byte)`. Frontend `frontend/src/hooks/useWebSocket.ts`. | **Replace selectively.** This is where the audit findings live (`P26-05-02`, `P26-05-03`, `P26-05-16`, `P26-05-27`, `P26-05-30`). A binary, framed, resumable WS sub-protocol meaningfully wins. See §3 (CKR-LOG/1). |
| Pipeline definition | JSON document in Postgres JSONB (`pipelines.stages`, `pipelines.edges`). Authored exclusively through the React Flow canvas. | **Supplement, don't replace.** A text DSL doesn't *remove* the canvas — it gives operators a GitOps-able source of truth and lets external tools generate pipelines. See §4 (CKR-DSL). |
| Pipeline execution location | In-process inside the API binary. `internal/service/executor.go` runs the DAG; `internal/builder/*.go` calls Docker / Kaniko / etc. *inside the API pod*. | **New surface.** No remote runner protocol exists. Every competitor has one (GH Actions, GitLab, Drone, Buildkite, Woodpecker). Without one, customers in air-gapped or compliance-bound networks cannot use Cooker at all. See §5 (CKR-RUNNER/1). |

The rest of this doc designs the three new protocols and ranks them.

---

## 1. Method — how we evaluate "build vs. buy"

A Cooker-native protocol earns its existence only if **all four** hold:

1. **Measurable problem.** A specific number from production or the perf audit — not "JSON is slow in general."
2. **No standard fits.** We did the comparison; the standards either don't solve our problem, solve it at unjustifiable cost, or carry an ecosystem tax (gRPC in browsers, NATS for one tenant, etc.).
3. **Migration is gradual.** Old clients keep working through one full release. No flag day.
4. **Security posture is no worse.** Auth, MITM resistance, downgrade attacks, replay — all explicitly considered.

If any one fails, we stick with standards. The three proposals below pass all four.

---

## 2. Naming convention

- `CKR-LOG/1` — log-streaming WebSocket sub-protocol. `1` is the major version; bumped only on a wire-incompatible change.
- `CKR-DSL` — pipeline definition language. Versioned via `apiVersion: cooker.dev/v1alpha1` inline, Kubernetes-style.
- `CKR-RUNNER/1` — remote runner agent protocol. Major version in the path.

All three negotiate version via the transport's native mechanism (`Sec-WebSocket-Protocol`, `apiVersion` field, gRPC service version respectively). No magic-byte sniffing.

---

## 3. CKR-LOG/1 — binary log-streaming sub-protocol

### 3.1 Problem we're solving

Every per-line log message from the executor pays this allocation chain today:

1. `service.lineWriter.Write` allocates a fresh `[]byte` per emitted line — `internal/service/logbroadcast.go:85-87`. Audit: **P26-05-03**.
2. `WebSocketHub.Broadcast` copies it into a `BroadcastMessage{Channel, Data}` struct — `backend/internal/server/websocket.go:153`. Audit: **P26-05-02**.
3. In multi-replica mode, `encodeBroadcast` allocates again for the Redis wire format, and `decodeBroadcast` copies once more on receive — `wshub_backend.go:265, 284`. Audit: **P26-05-16**.
4. Each connected browser tab gets its own `client.send <- msg.Data` channel push.
5. On the wire: one Gorilla `TextMessage` per line, no compression, no framing of which run/stage the line belongs to (channel name does that, but it's the full string repeated on every Redis hop).
6. On the browser: `useWebSocket.onmessage` runs `JSON.parse(event.data)` and try/catches the failure for the raw-text case — audit **P26-05-30**.
7. First paint of a stage's log requires **three** round-trips: REST backfill (`/pipelines/:id/runs/:runId/logs/:stageId`) → WS ticket fetch (`POST /api/v1/ws-tickets`) → WS upgrade with `?ticket=`. Audit: **P26-05-27**.

The audit estimates 2-3 allocations per log line in single-replica mode, ~5-6 in multi-replica. At a Kaniko build emitting 1k lines, that's ~5k allocations purely on the log fan-out path before we even count downstream GC pressure.

Standards-track text-frame WebSocket plus per-line JSON cannot meaningfully fix this without a custom wire format on top.

### 3.2 Why standard tools don't fit

Fair comparison — each alternative was considered:

| Alternative | Verdict for Cooker |
|---|---|
| **GitHub Actions log API** (chunked HTTP POST/GET of log spans, polled by the UI) | Works at GitHub's scale because of CDN edge caching. Cooker has no CDN. Polling kills the "tail" UX users expect from a live build. |
| **Buildkite WebSocket logs** | Text frames, JSON envelopes, no multiplex, no resume. Same shape as our current code; copying it changes nothing. |
| **gRPC server-streaming** | First-class for binary, but browsers cannot speak gRPC natively — requires gRPC-Web (HTTP/1.1 framing of base64) or Connect. Both introduce a proxy/transcoder. Adds ops surface for one feature. Also: we don't ship a `.proto` toolchain today; that's a separate adoption decision. |
| **Server-Sent Events (SSE)** | One-way (server→client), reconnect via `Last-Event-ID` is built-in, text-only. Reconnect is the *one* thing it does better than raw WS. But: text-only forces base64 or escape-encoding for any binary payload, no per-message framing, browsers cap connections to 6 per origin. Disqualifying for the "10 stages tailing simultaneously" UX. |
| **NATS JetStream** | Brilliant for fan-out, persistent, has replay-from-offset. But: forces every Cooker deployment to run NATS *in addition to* Redis. Two queueing systems is too many. Also: no browser client without a WSS gateway, which puts us back at "WebSocket on top of NATS" — same shape, more moving parts. |
| **WebTransport / HTTP/3 streams** | Future-correct answer. Cloudflare and Chromium ship it; Safari does not as of 2026. Revisit in 18 months. |

The right move is a **WebSocket sub-protocol**. We keep the transport (one TLS connection, one upgrade, browser-native, ticket-auth already shipped) and replace the *payload* with something dense and framed.

### 3.3 Wire format

Binary, little-endian, fixed-size headers, variable-size payload. Inspired by HTTP/2 framing minus the bits HTTP/2 needs that we don't (header compression, flow-control windows, priority).

#### 3.3.1 Negotiation

The browser opens the WS with:

```
Sec-WebSocket-Protocol: ckr-log/1, json
```

The server responds with the chosen one. Legacy clients send only `json` (or no header), get the existing text+JSON behaviour. Servers that don't speak `ckr-log/1` ignore it and pick `json`. Zero-cost coexistence.

After upgrade, the server immediately sends a `HELLO` frame so the client knows it's connected to a `ckr-log/1`-speaking peer. Receipt of any non-binary frame after that is a protocol error → close 1002.

#### 3.3.2 Frame layout

Every frame is one Gorilla `BinaryMessage`. We do **not** stream half-frames across WS message boundaries; one logical frame per WS message keeps the receiver state machine trivial.

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+---------------+---------------+-------------------------------+
|  Type (u8)    |   Flags (u8)  |       Stream-ID (u16)         |
+---------------+---------------+-------------------------------+
|                       Sequence (u64)                          |
+                                                               +
|                                                               |
+---------------------------------------------------------------+
|                        Length (u32)                           |
+---------------------------------------------------------------+
|                       Payload (Length bytes)                  |
+---------------------------------------------------------------+
```

- **Type (1 byte):**
  - `0x01 HELLO` — sent by server on connect; payload is a CBOR map `{server_ver, max_streams, supported_codecs[]}`.
  - `0x02 SUBSCRIBE` — client → server; payload is CBOR `{stream_id, channel, from_seq?}`. `from_seq` enables resume.
  - `0x03 UNSUBSCRIBE` — client → server; payload empty.
  - `0x10 LOG_LINE` — server → client; payload is the raw log bytes (no terminator, no envelope).
  - `0x11 LOG_CHUNK_ZSTD` — server → client; payload is zstd-compressed concatenation of N `\n`-separated lines. Used when the server has buffered ≥ 4 KiB of pending output (see §3.3.4).
  - `0x20 STATUS` — server → client; payload is CBOR `{stage_id, state, started_at?, finished_at?, exit_code?}`.
  - `0x30 PING` / `0x31 PONG` — control. Replaces Gorilla's PING/PONG so we can attach a Sequence to PONG and measure RTT.
  - `0xFE END` — server → client; payload is CBOR `{stream_id, reason}`. Final frame for the stream.
  - `0xFF ERROR` — either direction; payload is CBOR `{code, message}`.
- **Flags (1 byte):** bit 0 `BACKPRESSURE` (server is throttling; client should slow its acks if any), bit 1 `RESUMABLE` (set on every `LOG_LINE` / `LOG_CHUNK_ZSTD` if the server has retained the bytes long enough to satisfy a resume request).
- **Stream-ID (2 bytes):** assigned by the client in `SUBSCRIBE`; lets one WS connection carry multiple stage-log streams simultaneously (the multiplex point).
- **Sequence (8 bytes):** monotonic per-stream; lets the client resume after a reconnect via `SUBSCRIBE { from_seq: last_seen + 1 }`.
- **Length (4 bytes):** payload length. Capped at 16 MiB per frame; lines longer than that are split across frames with `0x10` repeated (sequence still increments).
- **Payload:** Type-specific. Control frames carry CBOR; `LOG_LINE` carries raw bytes.

**Why CBOR for control, raw bytes for log?** Raw bytes for logs because that's what saves the allocations (the whole point). CBOR (RFC 8949) for control because it's strictly smaller than JSON for our shapes, has typed integers, and has stable Go implementations (`fxamacker/cbor`). Could also use Protobuf; CBOR wins on "no codegen step."

#### 3.3.3 Multiplex

Today a stage-log tab opens its own WebSocket. With 10 stages, 10 WS connections, each going through 10 ticket fetches and 10 TLS handshakes. CKR-LOG/1 collapses this: one connection, one ticket fetch, one TLS handshake, N `SUBSCRIBE` frames over the same connection. Each stream is independently flow-controlled by the `BACKPRESSURE` flag.

Server-side cap: 64 streams per connection (`max_streams` in `HELLO`). Beyond that, open a second connection.

#### 3.3.4 Compression

Two paths:

- **Per-frame `permessage-deflate`** (RFC 7692) — already negotiable on the WebSocket layer. Browsers support it. Cheap, but each frame's compression context is independent so short log lines compress poorly.
- **Server-batched `LOG_CHUNK_ZSTD`** — when the broadcaster has ≥ 4 KiB of pending bytes OR the client's send buffer is more than half full, the executor emits a single `0x11` frame containing a zstd-compressed batch instead of N `0x10` frames. Typical compression ratio on Kaniko output is 5-7×.

Two-track choice because the batched path adds latency (we wait to accumulate 4 KiB). Tail-mode UX wants per-line; bulk-replay-mode UX (history backfill, see §3.3.5) wants batched. The flag bit picks the mode.

#### 3.3.5 Resume semantics and replacing the REST backfill

The current "REST backfill then WS attach" model (audit P26-05-27) collapses into one CKR-LOG/1 connection:

1. Client opens WS, sends `HELLO`, sends `SUBSCRIBE { stream_id, channel, from_seq: 0 }`.
2. Server replays everything it has for that channel as one or more `0x11 LOG_CHUNK_ZSTD` frames with `RESUMABLE=1`, ending the historical replay with a `LOG_CHUNK_ZSTD` whose final sequence number matches the live tail.
3. Server transitions the stream to live mode and starts emitting `0x10 LOG_LINE` frames as the executor produces them.

The server retains the log buffer in its existing `cappedBuffer` (`internal/service/executor.go:301`) until the stage finishes, then on the next persistence write the bytes plus per-line sequence numbers go to Postgres. Resume after stage finish reads from Postgres; resume during stage execution reads from RAM. Same `from_seq` interface.

Resume window: bounded by `stageLogCap` (1 MiB today). If `from_seq` falls outside the retained window, the server emits `0xFF ERROR { code: "out_of_window" }` and the client falls back to a fresh `from_seq: 0` (full replay).

### 3.4 Migration & rollout

Three phases over three releases, no flag day:

| Phase | Server | Frontend | Old clients |
|---|---|---|---|
| **A** (release N) | Ships `ckr-log/1` AS WELL AS the existing JSON-text behaviour. `Sec-WebSocket-Protocol` negotiates. | Continues sending JSON. | Unchanged. |
| **B** (release N+1) | Default on; deprecation warning in the JSON path. | Switches to `ckr-log/1`. Vite chunk size for the new client code is ~6 KiB ungzipped. | Continue working via JSON. |
| **C** (release N+2) | JSON path removed. | — | Must upgrade. Documented in `docs/UPGRADING.md`. |

REST backfill endpoint `/pipelines/:id/runs/:runId/logs/:stageId` stays — it's also used by the "download full log as text" button and by the API export path. Only the *initial-paint* call goes away.

### 3.5 Security

- **Auth.** Inherits the existing WS ticket flow (`POST /api/v1/ws-tickets` → 60s single-use ticket → `?ticket=`). The sub-protocol doesn't change how the connection is authenticated.
- **Downgrade attack.** A MITM could strip `ckr-log/1` from the `Sec-WebSocket-Protocol` header to force the JSON path. Mitigation: in phase C, the client refuses to accept anything but `ckr-log/1` and closes 1002 if the server picks JSON. (Phases A/B accept either; that's the tradeoff for compatibility.)
- **Resource exhaustion.** `max_streams: 64` per connection caps fan-out. Per-frame `Length: 16 MiB` cap prevents a malicious server from sending a payload that exhausts the browser's memory. (Server-side, the executor is the producer so a malicious "server" is the impossible threat; documenting the cap is still good hygiene.)
- **Replay.** Sequence numbers are per-stream and not cryptographic — they protect against missed-frame UX bugs, not adversarial replay. The auth ticket and TLS are the actual replay defenses.
- **No JS evaluation.** Logs are rendered as `<pre>` text. CBOR decode is bounded by the Length field and refuses unknown major types. No risk of payload-driven code execution.

### 3.6 Success criteria

If we ship CKR-LOG/1 and don't move these numbers, we wasted the work:

| Metric | Today (estimate) | Target |
|---|---|---|
| Allocations per log line, single-replica | 2-3 | ≤ 1 (one pooled buffer per line) |
| Allocations per log line, multi-replica | 5-6 | ≤ 2 |
| First-paint latency for a stage log tab | ~3 RTTs (REST + ticket + WS) | 1 RTT after the already-cached ticket |
| WS connections for a 10-stage run | 10 | 1 |
| Bytes on the wire for a typical Kaniko build (~1 MiB raw log) | ~1 MiB (no compression) | ~150-200 KiB (zstd batched) |
| p99 log-line server-to-screen latency at 10k lines/sec/run | unmeasured | < 50 ms |

### 3.7 Effort

**~3 engineering weeks.**

- Week 1: backend frame codec, hub-side wiring of the new sub-protocol selection, per-stream sequence numbers, `cappedBuffer` retention for resume.
- Week 2: frontend hook (`useStageLogsV2`) that speaks CKR-LOG/1, multiplex of multiple stream IDs across one WS, zstd decoder (`@oneidentity/zstd-js` or the WASM zstd build).
- Week 3: zstd batching trigger, dual-stack deprecation path, perf regression tests in CI, docs, `docs/UPGRADING.md` entry.

### 3.8 Risks & open questions

- **zstd in the browser is a WASM blob (~70 KiB).** Adds to the bundle. Mitigation: lazy-load only on stages-log routes. Acceptable for the bandwidth win on hot reloads.
- **CBOR adds a Go dependency.** Small (~30 KiB binary increase), well-maintained. Acceptable.
- **Multiplex changes the lifecycle of per-stage subscriptions.** Today the WS connection's lifecycle IS the subscription's lifecycle. After CKR-LOG/1, a tab can `UNSUBSCRIBE` from one stream while keeping the connection open. The hub's `clients map[channel]map[*Client]bool` needs to grow `*Client` → set-of-streams. Not hard; just a real refactor in `websocket.go`.
- **Question for the user:** do we want to extend CKR-LOG/1 to *all* WS channels (pipeline-run status, kube-watch) or keep it scoped to logs? My recommendation is logs-only for v1, generalize in v2 once we've lived with it.

---

## 4. CKR-DSL — pipeline-as-code spec

### 4.1 Problem we're solving

The pipeline graph today exists only as JSONB in Postgres. To get a pipeline into Cooker you must:

1. Use the React Flow canvas in a browser, or
2. POST to `/api/v1/pipelines` with hand-written JSON matching the internal `model.Pipeline` shape.

Both fail the GitOps test. There is no way to:

- Diff a pipeline change in a code review.
- Roll back a pipeline by reverting a commit.
- Template a pipeline (envs, branches).
- Generate pipelines from an external tool.
- Define pipelines in the same repo as the code they build (the single most-requested feature in every CI tool).

The graph UI is great for exploration. It is wrong as the sole source of truth.

### 4.2 Why standard tools don't fit

Every major CI tool ships its own format. We evaluated five:

| Format | Strengths | Why not for Cooker |
|---|---|---|
| **GitHub Actions YAML** | Familiar, well-known. | Built around the linear "jobs / steps" model. Translating the Cooker DAG (arbitrary stage graph with conditional edges and env swimlanes) needs heavy `needs:` lists and loses the visual structure. |
| **Drone / Woodpecker YAML** | Tight, simple, container-first. | Step ordering is implicit by file order; we need explicit edges. Conditional logic via `when:` is per-step, not per-edge. |
| **Concourse YAML** | Real resource/job/trigger model — closest in spirit to Cooker's DAG. | Steep learning curve; Concourse-style "inputs/outputs as resources" is a paradigm shift our users haven't asked for. |
| **Tekton CRDs (YAML)** | K8s-native, very structured. | Forces a K8s control plane assumption. Cooker isn't K8s-only (`internal/deploytarget/` has Cloud Run, ECS, Fly.io). |
| **Dagger CUE / Earthly Earthfile / BuildKit LLB** | Programmable. | Each is its own runtime; we'd be embedding a second execution engine. Out of scope for "describe a Cooker pipeline." |

### 4.3 Format choice: **YAML**

The candidates we weighed:

- **YAML.** Universal in CI. Operators already know it. Loss of type strictness vs. JSON is mitigated by a published JSON Schema we use for `cooker pipeline validate`.
- **HCL.** Beloved in the Terraform world, but ours is a CI-tool audience, not an IaC audience. Adds a parser, learning curve, and one more language for operators to know.
- **Starlark.** Great for templating (Bazel, Tilt, Buildkite). But: a Turing-complete config language is a tax on every code review. Bazel teams have written entire essays about this. Skip.
- **CUE.** Schema + values in one. Powerful, niche. Hugo and Dagger users love it; everyone else finds it foreign.
- **KCL.** Newer, K8s-flavoured CUE. Same problem with niche audience.

**Pick YAML.** Universal, has a schema story (JSON Schema 2020-12 generated from our Go types via `invopop/jsonschema`), every IDE has completion + lint. Ergonomic loss vs. CUE/HCL is real but localised; ergonomic loss for *operators* of any alternative is much bigger.

### 4.4 Schema sketch

```yaml
apiVersion: cooker.dev/v1alpha1
kind: Pipeline
metadata:
  name: api-build-and-deploy
  description: |
    Build the API image, run tests, push to GHCR, deploy to dev,
    promote to staging on success, manual gate to prod.
spec:
  params:
    - name: image_tag
      default: "{{ git.sha }}"
      description: Tag for the built image.
  secrets:
    - name: GHCR_TOKEN
      from: { env: production, key: ghcr-write-token }
  stages:
    - id: build
      type: build
      runs_on: { labels: [linux, amd64] }     # see CKR-RUNNER/1
      env: dev
      with:
        builder: kaniko
        context: ./api
        dockerfile: ./api/Dockerfile
        platforms: [linux/amd64, linux/arm64]
        cache: { type: registry, ref: "{{ params.image_tag }}-cache" }
    - id: test
      type: test
      env: dev
      needs: [build]
      with:
        image: "{{ stages.build.image }}"
        command: ["go", "test", "./..."]
    - id: push
      type: push
      env: dev
      needs: [test]
      with:
        registry: "ghcr.io/santapong/api"
        tags: ["{{ params.image_tag }}", "latest-dev"]
    - id: deploy-dev
      type: deploy
      env: dev
      needs: [push]
      with:
        target: { kind: kubernetes, cluster: dev, namespace: api }
        manifests: ./k8s/api/dev
    - id: deploy-staging
      type: deploy
      env: staging
      needs: [deploy-dev]
      promotion: { strategy: auto }
      with:
        target: { kind: kubernetes, cluster: staging, namespace: api }
        manifests: ./k8s/api/staging
    - id: deploy-prod
      type: deploy
      env: production
      needs: [deploy-staging]
      promotion:
        strategy: manual
        required_approvers: 1
        groups: [cooker-approvers, sre-oncall]
      with:
        target: { kind: kubernetes, cluster: prod, namespace: api }
        manifests: ./k8s/api/prod
  on:
    branch: [main, "release/*"]
    pull_request: false
    schedule: []
```

Top-level keys:

- `apiVersion` — `cooker.dev/v1alpha1` (alpha = breaking changes allowed); graduates to `v1beta1`, then `v1`. Stability policy in §4.7.
- `kind` — currently only `Pipeline`. Reserved future kinds: `Environment`, `App`.
- `metadata.name` — globally unique; the on-disk filename SHOULD match.
- `spec.params` — typed parameters with defaults.
- `spec.secrets` — declarative references to the existing secrets backends. The value never appears in the file; only the reference does.
- `spec.stages[]` — the DAG. Each has `id` (unique), `type` (one of `build|test|push|deploy|approval|custom|gitops`), `env` (matches an Environment), `needs[]` (the DAG edges), `with` (type-specific config), optional `runs_on` (runner labels, see §5), optional `promotion` (for env-boundary stages).
- `spec.on` — trigger configuration. Initially branch + schedule; PR triggers in v1beta1.

**Edge labels (success/failure/always)** map to a `needs` enrichment:

```yaml
needs:
  - { stage: build, on: success }
  - { stage: notify, on: failure }
```

Short form `needs: [build]` is sugar for `needs: [{stage: build, on: success}]`.

### 4.5 Round-trip with the graph UI

**Lossless graph ↔ DSL** is a goal, with one exception: visual node coordinates.

- DSL → graph: the layout engine (existing in the canvas) auto-arranges. The user can save manual positions back to the DSL under `metadata.layout.positions[<stage_id>]` if they want, but it's optional. Authoring in the DSL doesn't require the user to do any layout work.
- Graph → DSL: the canvas can export its current state to a DSL document. Positions go to `metadata.layout` if non-default.

**The rule:** any DSL document that round-trips through the canvas must produce a byte-equivalent DSL on export, modulo:

- Key order (we canonicalise on write).
- Comments — the graph cannot represent them, so re-export drops them. Document this in `docs/dsl.md`.

This is the same tradeoff `kubectl get -o yaml` makes. Acceptable.

### 4.6 GitOps reconciler

A new optional service: `cooker-gitops-reconciler`. Polls a Git repo, walks a configured path (`pipelines/**.cooker.yaml`), parses each file, diffs against the Cooker store, and applies create/update/delete.

- Runs as a sidecar to the main Cooker binary, or as a separate Deployment.
- Auth: a Cooker API token scoped to `operator` role.
- Conflict resolution: **Git wins.** If a user edits a pipeline in the UI after it was reconciled from Git, the next reconcile loop overwrites the UI change. The UI shows a "managed by GitOps — edits will be reverted" banner. (This is the ArgoCD model.)
- An override field `metadata.annotations.cooker.dev/gitops-mode: "freeze"` lets users opt a single pipeline out of reconciliation.

The reconciler is a follow-on, not part of v1alpha1. The DSL ships first; the reconciler is built once people are writing DSL files in their repos.

### 4.7 Versioning

| Track | When stable | Breaking changes allowed? |
|---|---|---|
| `v1alpha1` | First release | Yes, with one-release deprecation notice. |
| `v1beta1` | After ≥ 6 months of v1alpha1 usage with no breaking changes for two consecutive releases | Compat for two releases on each breaking change. |
| `v1` | After ≥ 6 months of v1beta1 | Never within v1.x. Breaking changes go to `v2alpha1`. |

The version is in `apiVersion` per document. The server can support multiple versions simultaneously and translate on read. Old documents continue to work after a graduation.

### 4.8 Compatibility & rollout

The DSL is purely additive. The UI stays the default. Existing pipelines in the DB are untouched. Adoption is opt-in per pipeline.

New endpoints:

- `POST /api/v1/pipelines/from-dsl` — parses a DSL document, creates a pipeline.
- `GET /api/v1/pipelines/:id/dsl` — exports an existing pipeline as DSL.
- `POST /api/v1/pipelines/:id/validate-dsl` — dry-run validation of an updated DSL against the existing pipeline.

A new CLI subcommand `cooker pipeline {validate,apply,export}` mirrors these for local use. Useful in pre-commit hooks.

### 4.9 Security

- **No code execution in the DSL.** Templating is `{{ ... }}` with a strict allowlist of accessors: `params.X`, `secrets.X.name`, `stages.X.<output>`, `git.{sha,branch,tag}`, `env.X`. No `exec`, no file reads, no network. Compile-time check rejects unknown accessors.
- **Secrets never appear inline.** The `secrets:` block carries *references*, never values. A linter pass refuses to apply any DSL that contains a string matching common secret patterns (long base64, JWT shape, AWS key shape) in `with:` fields. Belt-and-braces against an operator pasting a token in for testing and forgetting.
- **Schema validation runs server-side, always.** The CLI's local validation is for fast feedback; the API re-validates. Trust boundary stays at the backend.
- **Path traversal in `with: { manifests: ... }`** is bounded to the source repo by the reconciler; for the API path, the field is a string the deploy adapter passes to its target unchanged. The deploy adapter is responsible for sandboxing (it already is today).

### 4.10 Success criteria

| Metric | Target |
|---|---|
| Author a new pipeline in DSL, validate, apply, run — total wall-clock time | < 5 minutes for a returning user |
| Round-trip canonicalisation: DSL → graph → DSL == identity (modulo comments) | 100% on a fuzz test of valid documents |
| Schema-rejected DSLs with a useful error message (file + line + key path) | 100% of invalid documents |
| Existing pipelines exported to DSL and re-imported without behaviour change | 100% of the integration-test pipeline corpus |

### 4.11 Effort

**~4 engineering weeks** for v1alpha1.

- Week 1: Go types, JSON Schema generation, parser with line-number-preserving errors.
- Week 2: graph ↔ DSL converters; round-trip test corpus.
- Week 3: HTTP endpoints, CLI subcommand, validation pipeline.
- Week 4: docs (`docs/dsl.md`), examples, the schema published at `docs/schemas/cooker-pipeline-v1alpha1.json`.

GitOps reconciler is a separate ~2-week follow-on.

### 4.12 Risks & open questions

- **Bus factor on the parser.** YAML libraries in Go are middling. `gopkg.in/yaml.v3` has known maintenance issues; `goccy/go-yaml` is newer and faster but less battle-tested. Pick `goccy/go-yaml` (better line-number support for errors) with an upgrade plan if it ever stalls.
- **Templating engine.** Don't use `text/template` — it's too powerful (function maps, conditionals). Use a 50-line custom evaluator over the strict accessor set. Slower to extend; safer.
- **Question for the user:** do we want to support multi-document YAML (`---`-separated multiple `Pipeline` resources in one file) on day one? My recommendation: no, keep it one-pipeline-per-file in v1alpha1. Add `kind: List` later if asked.

---

## 5. CKR-RUNNER/1 — remote runner agent protocol

### 5.1 Problem we're solving

Today every build, test, push, and deploy runs **inside the Cooker API pod**. That means:

- The Cooker pod's container is the security boundary for every customer workload. A pipeline that runs `npm install` runs with the same SA token as Cooker itself.
- Cooker cannot deploy into networks it can't reach. A customer with a `cooker-prod` cluster behind a corporate firewall cannot let Cooker punch in.
- Horizontal scaling Cooker scales the *control plane and the data plane together*. A team running 50 builds doesn't need 50 Cookers; they need 50 runners and 1 Cooker.
- Compliance: SOC-2 customers cannot let a shared-tenant tool deploy into their VPC. They need a runner they own and run in-network.

This is the single biggest gap in the architecture relative to every competitor. GH Actions, GitLab, Drone, Buildkite, Woodpecker — every one of them has a runner agent. Cooker not having one caps adoption at "single small cluster" use cases.

### 5.2 Why standard tools don't fit

For once, the obvious answer is the right one — but with a specific transport choice.

| Alternative | Verdict |
|---|---|
| **GitHub Actions self-hosted runners** (HTTP long-poll) | Solid model. The runner polls; the server never initiates. Firewall-friendly. Costs latency: poll interval is the floor on job-pickup time. ~1-2s in practice. |
| **GitLab runners** (HTTP register + WSS for log streaming) | Similar shape. Two protocols (REST control + WSS logs); awkward but works. |
| **Drone / Woodpecker agents** (gRPC) | Tightly coupled, good throughput. Same pull-based model. |
| **Buildkite agents** (HTTPS poll) | Mature, the de-facto reference for "self-hosted runner that just works." Same model as GH Actions. |
| **Tailscale control protocol** | Reference for "agent dials home, server never dials in." Not directly applicable but the security property is what we want. |
| **Native socket per job** | Server initiates connections to agents. Requires inbound connectivity from server to agent. **Disqualifying** for the firewall-bound customer. |

The shape is settled: **agent pulls work; server never initiates a connection to the agent.** That's the model every successful self-hosted runner uses. The only real design choice is the transport.

### 5.3 Transport choice: **gRPC over HTTP/2 (TLS), with a long-poll fallback over HTTPS**

**Primary path: gRPC.**

- Bi-directional streams handle log shipping (`StreamLogs`) and job dispatch (`RequestJob` returns a stream of one or more job-detail messages) cleanly.
- Schema-first via Protobuf. Versioning is straightforward; tooling is mature.
- Server-streaming for log uploads avoids the "agent posts a HTTP chunk every N ms" anti-pattern.
- Connection multiplex via HTTP/2 — one TCP connection per agent, regardless of concurrent jobs.

**Fallback path: HTTPS long-poll + chunked POST.**

- For customers whose egress firewall only allows plain HTTPS (not full HTTP/2, not gRPC framing).
- Same RPC surface, JSON over HTTPS. Higher per-RPC overhead but ships.
- Auto-negotiated at agent enrolment: agent tries gRPC, falls back to HTTPS on `UNAVAILABLE` or TLS-ALPN failure.

**Why not pure HTTPS like GH Actions?** Agent log shipping is the hot path. GH Actions ships logs in chunked POSTs every N seconds — visible UX latency. gRPC server-streaming gets us sub-second tail. The HTTPS fallback exists for the 5% of customers who need it; the default is gRPC.

**QUIC?** Considered, rejected for v1. The mTLS-over-QUIC story is fine; the load-balancer and ops story is not yet. Revisit when nginx/HAProxy/Envoy have boring QUIC support in operator hands. ~2027.

### 5.4 RPC surface

```protobuf
syntax = "proto3";
package cooker.runner.v1;

service Runner {
  // One-time call during enrolment; returns long-lived runner identity.
  rpc Register (RegisterRequest) returns (RegisterResponse);

  // Periodic; agent → server every 10s. Server uses it to detect dead agents.
  rpc Heartbeat (HeartbeatRequest) returns (HeartbeatResponse);

  // Long-running stream; server pushes job assignments. One concurrent
  // call per agent. Closes on graceful agent shutdown or context cancel.
  rpc RequestJob (RequestJobRequest) returns (stream JobAssignment);

  // Bi-di stream for logs. Agent opens once per job; sends frames as
  // they're produced. Frames carry the CKR-LOG/1 sequence number so the
  // server can resume after agent reconnects mid-job.
  rpc StreamLogs (stream LogFrame) returns (LogAck);

  // Final result; agent sends once per job.
  rpc ReportResult (ReportResultRequest) returns (ReportResultResponse);

  // Agent → server; declines an in-flight job (e.g. resource exhausted).
  // Server requeues. Stops the agent from being marked dead.
  rpc DeclineJob (DeclineJobRequest) returns (DeclineJobResponse);
}

message RegisterRequest {
  string enrolment_token = 1;       // one-use; minted via Cooker UI
  string hostname = 2;
  string version = 3;               // runner agent version
  repeated string labels = 4;       // e.g. ["linux", "amd64", "gpu"]
  Capacity capacity = 5;            // CPU, memory, disk available
  bytes csr = 6;                    // PKCS#10 CSR for mTLS cert
}
message RegisterResponse {
  string runner_id = 1;
  bytes cert = 2;                   // signed by Cooker's runner CA
  bytes ca_chain = 3;
  google.protobuf.Duration cert_ttl = 4;
}

message JobAssignment {
  string job_id = 1;
  string run_id = 2;
  string stage_id = 3;
  string pipeline_name = 4;
  StageSpec spec = 5;               // what to do — DSL-derived
  map<string, string> params = 6;
  repeated SecretMaterial secrets = 7;  // pre-resolved secrets, scoped to this job
  google.protobuf.Timestamp deadline = 8;
  string lease_token = 9;           // proof-of-assignment; required on ReportResult
}

message LogFrame {
  string job_id = 1;
  uint64 sequence = 2;              // monotonic per job; survives reconnects
  bytes payload = 3;                // raw bytes; identical semantics to CKR-LOG/1 LOG_LINE
  bool is_final = 4;
}

message ReportResultRequest {
  string job_id = 1;
  string lease_token = 2;
  Result result = 3;                // success | failed | cancelled
  int32 exit_code = 4;
  repeated Artifact artifacts = 5;
  string error = 6;
}
```

### 5.5 Authentication

Three-stage:

1. **Enrolment.** Operator creates a Runner in the Cooker UI, gets a one-use enrolment token (TTL 15 min). Token is bound to `{org, environment, labels[]}`. Operator pastes it into the agent's config and starts the agent.
2. **mTLS certificate issuance.** Agent's first call is `Register` with the enrolment token and a CSR. Cooker's internal CA signs a client cert with the runner's identity baked into the CN and SANs. Cert TTL is 24 hours; auto-rotated by the agent.
3. **Per-call mTLS.** Every subsequent RPC uses the issued cert. Server validates: cert chain, expiry, SAN matches the claimed `runner_id`. Enrolment token is single-use and is rejected on replay.

No long-lived secrets after the 15-minute enrolment window. Lost agent = revoke the cert in Cooker's UI = next heartbeat fails = job assignments stop.

**Why mTLS not bearer tokens?** Bearer tokens leak through misconfigured proxies, log lines, env dumps. mTLS keys are easier to scope and revoke and don't end up in `kubectl describe pod` output.

### 5.6 Security boundary properties

The runner is the **outbound** half of the connection. The Cooker server has no inbound path to the runner. This gives us:

- **Customer firewall stays closed.** Egress on 443 is enough. SOC-2 customers can ship without opening any inbound port.
- **No agent discovery problem.** Agents introduce themselves; the server doesn't go looking. Eliminates the "how do I find runners in three different VPCs" question.
- **Compromise blast radius.** A compromised agent can leak the secrets of the jobs it's been assigned, no more. It cannot pivot to other agents. It cannot reach the Cooker DB. It can submit forged log frames for *its own* jobs — but those land in the job's log stream and an observant operator can detect anomalies.

### 5.7 Failure modes

| Failure | Behaviour |
|---|---|
| Agent crashes mid-job | Server's `StreamLogs` stream closes. Heartbeat misses (>3 × 10s) → server marks job `LOST`. Server reschedules the job to another agent with matching labels. Lease token from the dead agent is invalidated; if it ever comes back online and tries to `ReportResult`, the server rejects it. |
| Network partition (agent alive, can't reach server) | Agent buffers log frames locally, retries with exponential backoff. After `max_offline_duration` (default 10 min), agent gives up, kills the in-flight job, marks itself "evicted." On reconnect, registers as a new instance. |
| Server crash | Agents reconnect with backoff. In-flight jobs continue executing locally; logs buffer. When the server returns, agents resume the `StreamLogs` stream with the last successful `LogAck` sequence. |
| Job deadline exceeded | Server cancels the job in the assignment stream. Agent SIGTERMs the user process, gives it `grace_period` (default 30s), then SIGKILL. Reports `cancelled`. |
| Two agents both pick up the same job (server bug or replay) | `lease_token` is server-issued and unique per assignment. `ReportResult` carrying a stale lease is rejected — the second-finisher's result is dropped. Idempotent by construction. |
| Agent claims a label it doesn't have (e.g. `gpu` on a non-GPU box) | Verified at job acceptance, not enrolment. Agent runs the job, presumably fails. Operator fix; no protocol concern. |

### 5.8 Migration & compatibility

- The **in-process executor stays the default.** Cooker continues to ship as a single binary that can build and deploy locally. The remote runner is opt-in per stage (`runs_on: { kind: runner, labels: [...] }`).
- No breaking changes to existing pipelines. Stages without `runs_on` run in-process as today.
- The runner agent is a **separate binary** built from the same repo (`cmd/cooker-runner/`). Shares the strategy adapters (`internal/builder/`, `internal/deployer/`) but skips the HTTP server and DB layers.
- Distribution: same GoReleaser pipeline (per `docs/shipping-go.md`), separate artifact, separate Docker image (`ghcr.io/santapong/cooker-runner`).

### 5.9 Success criteria

| Metric | Target |
|---|---|
| Cold enrolment to first job assigned | < 60 s |
| p50 job-pickup latency (server has a job → agent starts it) | < 500 ms over gRPC; < 2 s over HTTPS fallback |
| Per-agent steady-state CPU at idle | < 1% of one core |
| Per-agent RSS at idle | < 50 MiB |
| Survives a 30-second server outage with in-flight jobs | 100% |
| Survives an agent reconnect mid-build with no log loss | 100% (CKR-LOG/1 sequence numbers + agent-side buffer) |

### 5.10 Effort

**~6 engineering weeks** for the v1 of the protocol, the agent, and the server-side scheduler.

- Weeks 1-2: protobuf surface, server-side gRPC service, enrolment / CA / mTLS issuance, runner registry.
- Weeks 3-4: `cmd/cooker-runner` binary; reuse `internal/builder/*` and `internal/deployer/*`; local job execution.
- Week 5: log streaming with `CKR-LOG/1`-compatible sequence semantics, lease / heartbeat / dead-detection scheduler.
- Week 6: HTTPS fallback, Helm chart for runner, docs (`docs/runners.md`), end-to-end test suite (Cooker + 3 runners + a representative pipeline).

This is the largest of the three by a wide margin and unlocks the most adoption.

### 5.11 Risks & open questions

- **Internal CA management.** Issuing certs means we own a CA. Cooker becomes a small PKI. Risk: key compromise = trust collapse for every agent. Mitigation: CA key in a Kubernetes Secret encrypted with `COOKER_SECRET_KEY` at rest; rotation procedure documented; CA cert TTL is 5 years, runner certs are 24 hours.
- **gRPC ALB compatibility.** Some cloud load balancers (older AWS NLB configurations, Azure App Gateway) handle HTTP/2 oddly. Mitigation: document the tested load-balancer configurations; the HTTPS fallback is for the broken cases.
- **The "what runs where" mental model.** Users will be confused about which stages run in-process vs. on a runner. Mitigation: the UI shows a runner badge on each stage; `cooker pipeline validate` warns about pipelines that mix locations in ways that won't work (e.g. a `deploy` stage labelled for a runner that can't reach the target cluster).
- **Question for the user:** do we want to support **runner pools** (a managed set of ephemeral runners spun up from a pool template per job, AWS-CodeBuild-style) on day one, or only **standing runners**? My recommendation: standing only for v1; pools are a v2 feature once standing runners have shaken out.

---

## 6. Ranking — which to build first

| Order | Protocol | Why | Effort |
|---|---|---|---|
| **1st** | **CKR-LOG/1** | Smallest scope, biggest immediate UX win, directly closes 4 named perf audit findings (P26-05-02, -03, -16, -27). No new operator concept. Pure transport optimisation. Lays groundwork (sequence numbers, resume) reused by the runner protocol. | ~3 weeks |
| **2nd** | **CKR-DSL** | Unblocks GitOps workflows, code-review of pipelines, external generators. Largest addressable user base. No new infrastructure. Independent of #1 and #3. | ~4 weeks (+ ~2 wks reconciler later) |
| **3rd** | **CKR-RUNNER/1** | Biggest strategic unlock — without it, large customers cannot adopt Cooker. But: largest effort, hardest to get right (PKI, scheduler, failure modes), and benefits least from existing Cooker code. Land it after the DSL exists so runner stages can be declared cleanly. | ~6 weeks |

**Why this order, not strict "build the most impactful first":** runner-first means designing the DSL surface for `runs_on` without having shipped the DSL, then re-doing both. Logs-first means the runner can ship using CKR-LOG/1's sequence semantics for its log shipping (§5.4 RPC `LogFrame` mirrors CKR-LOG/1's frame on purpose). Each protocol makes the next cheaper.

---

## 7. What we deliberately do **not** build

### 7.1 Don't replace REST with gRPC for the public API

Tempting. Wrong.

- Every Cooker integration point that exists today — `curl`, the browser frontend, future Terraform provider, future GitHub Action, every SaaS user's "I just want to call the API from a script" — expects HTTP/JSON. The cost of moving is multi-quarter; the benefit is sub-millisecond per request that nobody measures.
- gRPC in the browser needs gRPC-Web or Connect. Both add a proxy layer or a transcoder. Both fragment the documentation surface.
- We already have OpenAPI 3 generated from doc-comments (`docs/openapi.yaml`). Schema-first works for us; codegen tools work fine off OpenAPI; the SDK story is solved without protobuf.

**The right rule:** REST for the public API surface. gRPC only inside Cooker (server↔runner, server↔server in the future).

### 7.2 Don't build a CKR-DAG bytecode

Compile pipelines to a stack-machine "execution plan" instead of the in-memory DAG? Two competitors have done this (Buildkite's bk graph, Buildkit's LLB). Neither shipped a user-visible win for it. The DAG runner (`pkg/dagrunner/`) is already fast, simple, and tested. Adding a bytecode IR is engineering pleasure, not user value.

### 7.3 Don't build a Cooker plugin RPC (Hashicorp `go-plugin` style)

Already covered in `docs/shipping-go.md` §5: the strategy-pattern adapter shape we have is the right answer. RPC plugins are wrong for the per-request-hot-path adapter pattern. The runner protocol is *not* this — it's a remote *worker*, not an inline *adapter*.

### 7.4 Don't replace WebSocket with WebTransport yet

WebTransport over HTTP/3 is the future-correct answer for what CKR-LOG/1 does. But: Safari support is still patchy, and the Cooker user is often a corporate machine where the firewall hasn't been told about UDP yet. Revisit in 2027.

---

## 8. One-page summary

| | CKR-LOG/1 | CKR-DSL | CKR-RUNNER/1 |
|---|---|---|---|
| **Replaces** | Per-line JSON-text WS frames + REST log backfill | Nothing (supplements graph UI) | Nothing (new surface) |
| **Transport** | WebSocket binary sub-protocol | YAML over REST | gRPC over HTTP/2 mTLS (HTTPS fallback) |
| **Audit findings closed** | P26-05-02, -03, -16, -27, -30 | — | — |
| **Strategic unlock** | UX polish, allocation pressure | GitOps, code-review, external generators | Air-gapped deploys, horizontal scale, SOC-2 |
| **User-visible impact** | Smoother log tailing, faster first paint | Pipelines in Git | Customer adoption gates |
| **Effort** | ~3 weeks | ~4 weeks (+2 for GitOps reconciler) | ~6 weeks |
| **Risk** | Medium (zstd + CBOR deps; lifecycle refactor in hub) | Medium (YAML lib choice; templating engine boundaries) | High (PKI, scheduler, failure modes) |
| **Reversibility** | High — dual-stack for 2 releases | High — opt-in per pipeline | Medium — agent installs are persistent |
| **Order** | **1st** | **2nd** | **3rd** |

---

## 9. Open questions for the user

Before any of this becomes a real PR:

1. **CKR-LOG/1 scope.** Logs only, or all WS channels (status, kube-watch) in one go? (Recommendation: logs only for v1.)
2. **CKR-DSL multi-doc YAML.** One pipeline per file, or allow `---`-separated multi-doc? (Recommendation: single-doc, add `kind: List` later.)
3. **CKR-RUNNER/1 pools.** Standing runners only in v1, or pools (ephemeral, spun up per job)? (Recommendation: standing only; pools in v2.)
4. **Naming.** `CKR-*` is functional; `cooker.dev/v1alpha1` matches Kubernetes. Happy with either; not happy with both spellings in the same surface. Pick one for the user-visible labels.
5. **Ordering bias.** This doc ranks CKR-LOG/1 first by ROI. If the business need is "large-customer adoption," reorder to CKR-RUNNER/1 first; accept the 2x cost from re-doing the DSL surface around it.

---

## 10. Cross-references

- `docs/audits/2026-05-perf-and-optimization.md` — Wave 1 findings; P26-05-02, P26-05-03, P26-05-16, P26-05-27, P26-05-30 are the named log-path bottlenecks.
- `docs/architecture.md` — system map; the WS hub lives in `internal/server/websocket.go`.
- `docs/design.md` §6 — current WS authentication model (ticket exchange).
- `docs/shipping-go.md` — why we don't adopt `hashicorp/go-plugin` for adapters; same reasoning informs §7.3.
- `backlog.md` — open items; none of the three protocols are currently scoped there. This doc is the proposal.
- `backend/internal/server/websocket.go` — current `Broadcast` path.
- `backend/internal/server/wshub_backend.go` — Redis backend, `encodeBroadcast` / `decodeBroadcast`.
- `backend/internal/service/logbroadcast.go` — `lineWriter` and `StageLogChannel`.
- `frontend/src/hooks/useWebSocket.ts` — current WS client.

*End of proposal.*
