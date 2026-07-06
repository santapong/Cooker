# Backend

Data modeling, consistency, and the machinery that makes writes reliable. The engine here is a single bias: **keep one strongly-consistent relational store as the default, and add a second store, a cache, or a queue only when a named access pattern proves the default can't serve it.** Every addition buys a capability and bills you a consistency problem — a dual write, a stale read, a redelivered message. Know the bill before you sign, and pay it deliberately, per domain, not globally.

Read this when you are on step 3 of the workflow (data & consistency), or when the user asks "how should I model this?", "SQL or NoSQL?", "do I need a queue?", "how do I cache this?", or "how do I make this reliable?". The persistent thread: **decide consistency per domain, not for the whole system.** One system can hold a strongly-consistent ledger in Postgres and an eventually-consistent activity feed at the same time — and should.

## Data modeling: normalize first, denormalize for a proven read

**Default to a normalized schema (roughly 3NF): one fact, one place.** Normalization is the safe default because it makes writes correct by construction — there is a single row to update, so no copy can drift out of sync, and referential integrity is the database's job, not yours. Model the entities and their relationships as the domain actually has them; let foreign keys and joins do the assembly at read time.

**Denormalize deliberately, for a specific hot read path that a join can't serve fast enough** — a feed that must render in one round trip, a dashboard aggregate too expensive to compute live, a fan-out read that would otherwise join five tables at p99. When you do, you are trading write cost and a drift risk for read latency. Own that trade explicitly: name the read it serves, and name how the copies stay coherent (recompute on write, a materialized view, a background projection).

- **Trade-off**: normalized keeps writes simple and correct but pays a join at read time; denormalized makes the hot read trivial but fans a single logical write out to N places that can now disagree.
- **Failure mode**: denormalizing *speculatively*, before a read has proven slow — now every write updates several copies, some update path is missed, and the copies silently diverge. The normalized version would have been correct and probably fast enough. Measure the read first; denormalize the one that's actually slow.
- **Smell test**: if you're denormalizing to avoid a join on a table with thousands of rows, you have an indexing problem, not a modeling problem. Fix the index first.

## Polyglot persistence: one store until a pattern forces another

**Start with a single relational store (Postgres as the exemplar) and make it do everything it can** — which is far more than people assume: JSONB for semi-structured data, full-text search, geospatial, arrays, range types, materialized views, LISTEN/NOTIFY for lightweight eventing. A great many systems that reached for a document store or a search cluster on day one needed only a column type they didn't know existed.

**Reach for a specialized store only when a specific access pattern is genuinely a bad fit for the relational one**, and add exactly that store for exactly that pattern:

- **Search / relevance ranking** over large text corpora → a search engine (Elasticsearch/OpenSearch). Postgres full-text handles a lot; escalate when you need relevance scoring, faceting, and typo tolerance at scale.
- **Hot key-value reads, sessions, rate counters, ephemeral state** → an in-memory store (Redis). See caching, below.
- **Large binary objects** (images, video, exports) → object storage (S3), never BLOBs in the transactional DB.
- **High-ingest append-only metrics/events** → a time-series store; **deeply connected graph traversals** → a graph DB; **massive write-heavy denormalized tables** → a wide-column store (Cassandra).

**The tax you take on with the second store is the dual-write problem**: a single logical change must now land in two systems that don't share a transaction. Do not solve it with two writes in a row in application code — a crash between them corrupts state silently. Solve it with the **outbox pattern** (below) or change-data-capture. If you can serve the pattern from the primary store, you avoid this tax entirely; that's why "one store" is the default.

## Sharding & partitioning: the last resort, not the first reach

**Partition a table when one machine can no longer hold it or serve it — and not one moment sooner.** There are two axes, and they are not the same decision:

- **Vertical partitioning** splits a table by *columns*: move the rarely-read blob or a wide audit column to its own table (or store) so the hot rows stay narrow and cache-dense. Cheap, low-risk, and often all you need.
- **Horizontal partitioning (sharding)** splits a table by *rows*: each shard holds a disjoint slice of the keyspace on its own primary. This is what buys write throughput and dataset size past a single node — and it is the expensive one that most of this section is warning you about.

**The shard key is the entire decision, and it must match your dominant access pattern.** Choose the key that your hot queries already filter on (`tenant_id`, `user_id`) so a request routes to exactly one shard. A key that doesn't match your reads forces **scatter-gather**: every query fans out to all shards and waits on the slowest, which is *worse* than the single node you were fleeing. And the key is nearly irreversible — the data is already physically distributed by it, so a wrong choice is a migration, not a config change.

**What sharding costs you the moment you adopt it**:

- **Cross-shard transactions and joins vanish.** An operation spanning two shards becomes a distributed transaction (a saga — below) or an application-side join. Referential integrity across shards is now your job, not the database's.
- **Rebalancing is real operational pain.** Adding a shard, or fixing one that grew lopsided into a hotspot, means moving live data while still serving traffic. Consistent hashing and range-based schemes each trade differently here, and none of them is free.

**House default: exhaust the single-node ladder first.** Before you shard, climb: a bigger primary, then **read replicas** to offload reads (most systems are read-heavy, and this rung alone buys years), then vertical partitioning, then a caching tier. Reach for horizontal sharding only when a single primary plus read replicas **provably can't keep up** on write throughput or dataset size — reach for it last, not first. Plenty of systems sharded early and bought years of distributed-systems tax to solve a problem one read replica would have absorbed.

## Indexing: the cheapest 10x, and the silent write tax

An index turns a full-table scan (O(n)) into a lookup (O(log n) for a B-tree). It is usually the single highest-leverage fix for a slow read — and the most over-applied one.

- **Index the columns your hot queries filter, join, and sort on** — the ones in `WHERE`, `JOIN ... ON`, and `ORDER BY`. Start from the slow query, not the schema.
- **Composite index column order is load-bearing.** A B-tree on `(tenant_id, created_at)` serves `WHERE tenant_id = ? ORDER BY created_at` and `WHERE tenant_id = ?`, but **not** a query on `created_at` alone — only a leftmost prefix is usable. Order columns most-selective-and-equality-filtered first, range/sort last.
- **A covering index** (one that includes every column a query reads) lets the DB answer from the index alone, skipping the table fetch. Worth it for a critical read; not worth it everywhere.
- **Every index is a write tax.** Each `INSERT`/`UPDATE`/`DELETE` must maintain every index on the table, and each index costs storage and memory. Indexes are read optimizations paid for out of write throughput.

**Failure modes**: indexing every column "to be safe" (writes crawl, none of the indexes are the right shape); a low-cardinality index (a boolean, a status with three values — the DB scans anyway); and unused indexes lingering after the query that needed them was deleted. **Read the query plan** (`EXPLAIN ANALYZE`) rather than guessing — the plan tells you whether your index is used and where the time actually goes. An index you added on a hunch and never verified is as likely to hurt as help.

## Caching: cache-aside by default, and the tiers above it

Caching trades staleness and a coherence problem for latency and load relief. The two hard parts are, famously, invalidation and naming — plan for both before you add a cache.

**Default: cache-aside (lazy loading).** The application checks the cache; on a miss it reads the source of truth, populates the cache with a TTL, and returns. The store (Redis as the exemplar) holds only what's been asked for, and a cache outage degrades to slower reads rather than errors. This is the right default because it's simple, resilient to cache failure, and caches exactly the working set.

Escalate to another strategy only when the access pattern demands it:

- **Write-through** — write to cache and source of truth together, synchronously. Keeps the cache fresh (no first-read miss) at the cost of write latency and caching data that may never be read. Use when reads must never see stale data *and* nearly everything written is read soon.
- **Write-back (write-behind)** — write to cache, flush to the store asynchronously. Fastest writes, absorbs bursts — but a cache-node loss before flush is **data loss**. Reserve for tolerant, high-write workloads (counters, metrics), never for a system of record.
- **Read-through** — the cache library, not your app, loads on miss. Cleaner call sites; couples you to the cache as a hard dependency.

**Tier the caches** and let each absorb what it can before the next: browser/CDN at the edge, an in-process/local cache, then the shared distributed cache (Redis), then the database's own buffer cache. A request should be satisfied by the outermost tier that can honestly answer it.

**Failure modes to design against up front**: **stale reads** — every cached value needs a TTL *and* an invalidation-on-write story, or readers see the past indefinitely; **cache stampede** — a hot key expires and a thousand requests hit the DB at once (mitigate with request coalescing, a slightly randomized TTL, or serving-stale-while-revalidating); and **treating the cache as a datastore** — if losing the cache loses data or takes the system down, it stopped being a cache. The source of truth must survive a cold cache.

## CAP and PACELC: choose consistency per domain

**CAP**: when the network **partitions** (nodes can't talk), a distributed store must sacrifice either **C**onsistency (every read sees the latest write) or **A**vailability (every request gets a non-error response). You don't get both during a partition — the only choice is which one you give up. **PACELC** completes the honest picture: **P**artition → **A**vailability or **C**onsistency; **E**lse (normal operation) → **L**atency or **C**onsistency. Even with no partition, synchronous strong consistency costs latency (a write must reach a quorum before it returns). Every distributed store sits somewhere on this map — Postgres synchronous replication is PC/EC (it favors consistency, paying latency); a Dynamo-style store is PA/EL (it favors availability and latency, accepting eventual consistency).

**Turn this into a per-domain policy**, not a system-wide religion:

- **Strong consistency** for domains where a stale or lost write is a correctness bug the user pays for: money, inventory/stock levels, auth and permissions, unique-constraint enforcement (usernames, idempotency keys). Keep these in one transactional store and read them from the primary (or a synchronous replica). The latency and availability cost is the price of correctness, and it's non-negotiable here.
- **Eventual consistency** for domains where a few seconds of staleness is invisible or acceptable: activity feeds, view/like counts, search indexes, recommendations, analytics, notifications. Here you buy availability and latency by tolerating lag, and it's a good trade.

**Smell test**: if you can't tell a stakeholder how stale a read is allowed to be, you haven't finished designing the domain. "How wrong, for how long, is acceptable here?" is the question that assigns each domain to a column. The default answer for anything touching money or identity is *not at all*; for most everything else, *a little, briefly.*

## Message queues and delivery semantics

A broker (Kafka as the streaming exemplar; a classic queue like RabbitMQ/SQS for work distribution) decouples producers from consumers, absorbs bursts, and lets you retry work that failed. It also hands you delivery semantics you must design around — most of the reliability work is on the consumer side, not the broker.

- **At-least-once is the honest default, and the one to design for.** The broker guarantees a message is delivered, but a consumer crash between processing and acknowledging means it's redelivered. You *will* see duplicates. Build for it.
- **At-most-once** — fire and forget, no redelivery — is only acceptable when a lost message doesn't matter (some metrics, some telemetry). Rarely what you want.
- **"Exactly-once delivery" is an illusion** across a network with crashes: the two generals problem guarantees you can't have it end-to-end. What real systems achieve is **exactly-once *effect*** = at-least-once delivery **+ an idempotent consumer**. Kafka's "exactly-once" is exactly this within its own boundaries (idempotent producer + transactional offset commits); the moment a side effect leaves that boundary (charging a card, sending an email), idempotency is back on you.

**So make consumers idempotent.** Give each message a stable business key; before acting, record that key in a processed-messages table (or check for the effect's existence) inside the same transaction as the effect, and drop the message if you've seen it. Processing the same message twice must land the system in the same state as processing it once.

**Also design**: **ordering** — global order is expensive; most systems only need per-key order (Kafka gives per-partition order, so partition by the entity whose order matters). **Dead-letter queues** — a poison message that fails forever must move aside after N attempts, or it blocks the partition and you retry it until the heat death of the universe. **Backpressure** — a consumer slower than the producer needs the queue to buffer and the producer to feel it, not an unbounded memory climb.

## Transactional patterns: outbox, saga, CQRS

Three patterns for the moment a single local transaction no longer covers the change. Each solves a real problem and each adds real cost — reach for them only when the simpler thing (one ACID transaction in one store) genuinely can't do the job.

### Outbox — reliable events without a distributed transaction

**The problem**: you must update your DB *and* publish an event (or write a second store), atomically, but they don't share a transaction. Writing to the DB and then publishing is a bug: crash in between and the event is lost or phantom.

**The pattern**: in the *same* local transaction as the state change, insert the event into an `outbox` table. A separate relay process reads the outbox and publishes to the broker, marking rows sent (retrying safely, since consumers are idempotent). The DB transaction makes state-change-and-event atomic; the relay makes delivery reliable.

- **When**: any time a write to the primary store must reliably produce an event or a second-store write — the standard fix for the dual-write problem, and the mechanism to break a shared table when extracting a service (see `references/architecture-patterns.md`).
- **Cost**: a relay to run and monitor, at-least-once publishing (so consumers must be idempotent anyway), and a little publish latency. Cheap relative to the corruption it prevents — usually the right call.

### Saga — a long-lived transaction across services

**The problem**: a business operation spans multiple services/stores (place order → reserve inventory → charge payment → schedule shipping), each with its own database, so a single ACID transaction is impossible.

**The pattern**: model it as a sequence of local transactions, each publishing an event that triggers the next. Failure at any step runs **compensating transactions** that semantically undo the prior steps (refund the charge, release the reservation). Orchestrated (a coordinator directs each step) or choreographed (services react to each other's events); orchestration is easier to reason about and debug at more than a couple of steps.

- **When**: a genuinely cross-service transaction you can't collapse into one store — including the cross-shard write that sharding just created. If the whole operation fits in one database, use a real transaction — a saga would be strictly worse.
- **Cost**: you trade atomicity and isolation for availability. There is no rollback, only compensation, so you must design a semantic undo for *every* step and accept intermediate states other readers can observe (an order briefly "placed but unpaid"). It is markedly harder to build, test, and debug than a local transaction — the price of a boundary you had a requirement to cross.

### CQRS — separate the write model from the read model

**The problem**: one model can't serve both a complex write side (rich invariants, normalized) and a demanding read side (many query shapes, denormalized, high volume) without one crippling the other.

**The pattern**: split them. Commands mutate the write model; a separate, denormalized read model (often a different store, updated via events/projections) serves queries. Pairs naturally with event sourcing and with the outbox that feeds the projections.

- **When**: a genuine, large asymmetry between reads and writes — vastly more reads than writes, or query shapes the write schema can't serve efficiently. Often applied to *one* hot aggregate, not the whole system.
- **Cost**: two models to keep coherent, and the read model is now **eventually consistent** with the write model — a user may not immediately see their own write, which you must design for (read-your-writes on the write side, or a UI that expects lag). Applying CQRS to a plain CRUD domain is textbook premature complexity: you've doubled the moving parts to serve a symmetry that never existed. Most domains want a single model.

## The 12-factor app

A checklist for services that deploy and scale cleanly on modern platforms. Not law, but each factor you violate is a future operational bruise. Compressed, all twelve:

1. **Codebase** — one repo per app, tracked in version control, deployed to many environments. Many apps sharing a codebase is a distribution problem.
2. **Dependencies** — declare and isolate them explicitly (a manifest + lockfile); never rely on system-wide packages leaking in.
3. **Config** — everything that varies between environments (creds, endpoints, toggles) lives in the **environment**, never in code. The strict test: could you open-source the repo this second without leaking a secret?
4. **Backing services** — treat the DB, cache, broker, mail as attached resources addressed by URL/config; swapping a local Postgres for a managed one is a config change, no code edit.
5. **Build, release, run** — three strictly separate stages. A release is an immutable build + config; you can't change code at runtime.
6. **Processes** — run as **stateless**, share-nothing processes. Any state that must persist goes in a backing service; never lean on local disk or in-memory session stickiness.
7. **Port binding** — the app is self-contained and exports its service by binding a port, not by living inside an injected webserver.
8. **Concurrency** — scale **out** by running more processes (the process model), not only up by growing one.
9. **Disposability** — fast startup and graceful shutdown; a process can be killed at any moment (drain in-flight work, release the message) so the platform can move it freely.
10. **Dev/prod parity** — keep environments as alike as possible — same backing-service *types* everywhere — to kill "works on my machine."
11. **Logs** — treat logs as an event **stream** to stdout; the platform captures and routes them. The app doesn't manage log files.
12. **Admin processes** — run migrations and one-off tasks as one-off processes against the same release and config as the app, not by hand on a box.

The through-line — statelessness (6), config in the environment (3), disposability (9) — is what lets a platform scale, restart, and reschedule your process without ceremony. Break these and horizontal scaling and zero-downtime deploys fight you at every step.

## Resilience: pools, timeouts, retries, breakers

A backend that calls a database or another service **will** face slowness and failure; the only question is whether it degrades or amplifies. These four defaults turn a dependency's bad minute into a blip instead of a cascade. Apply them to every out-of-process call — DB, cache, broker, third-party API.

- **Connection pooling** — never open a connection per request; databases cap connections and the handshake is expensive. Pool and reuse, and **size the pool deliberately**: too small starves throughput, too large lets a traffic spike open more connections than the DB can serve and topples it. In serverless/high-fan-out fronts, put a pooler (e.g. PgBouncer) in front of Postgres so thousands of function instances don't each hold connections.
- **Timeouts on everything** — an unbounded call is a latent hang. A caller with no timeout, waiting on a dependency that's stalled, holds its connection and thread until it too runs out — one slow dependency exhausts the caller's pool and the stall propagates upstream. Set connect *and* read timeouts on every network call. Every retry loop needs an overall deadline, and the timeout must be shorter than the caller's own.
- **Retries with exponential backoff and jitter** — retry only **idempotent** or idempotency-keyed operations (blind retries on a non-idempotent write double-charge). Back off exponentially and **add jitter** — synchronized retries without jitter reconverge into a thundering herd that DDoSes the recovering dependency. Cap attempts and the total deadline; a retry storm against a struggling service prevents its recovery.
- **Circuit breaker** — when a dependency is failing, stop hammering it. After a failure threshold the breaker trips **open** and calls fail fast (serving a fallback or cached value) instead of piling onto a downed service; after a cooldown it goes **half-open**, lets a probe through, and closes on success. This is what stops one failed service from cascading into a system-wide outage as every caller's threads block on it. Pair with **bulkheads** — isolate resource pools per dependency so one saturated dependency can't consume the connections/threads the others need.

**The failure this section prevents is the cascade**: dependency B slows, A's calls to B pile up with no timeout, A's pool exhausts, A stops serving C, and a localized problem becomes total. Timeout + breaker + bounded pool + jittered backoff each break one link in that chain. For turning these into SLO/error-budget targets and mapping them to capacity, see `references/nfr.md`.

## Decision tables

**Storage & modeling**

| Situation | Default |
|---|---|
| New system, no proven exotic access pattern | Single relational store (Postgres), normalized |
| A read path is join-heavy and provably too slow | Denormalize *that* read; keep the source normalized |
| Full-text / relevance search at scale | Add a search engine for that pattern only |
| Hot KV, sessions, counters, rate limits | Add an in-memory store (Redis) |
| Large binary objects | Object storage (S3), not DB blobs |
| Must write DB **and** publish an event atomically | Outbox pattern |
| A query is slow on a modest table | Add/fix the index before touching the model |
| Read load outgrows one primary | Read replicas — before any sharding |
| A single primary + replicas provably can't keep up | Shard horizontally on the dominant access key — last resort |

**Consistency & transactions**

| Situation | Choice |
|---|---|
| Money, inventory, auth, uniqueness | Strong consistency, one transactional store |
| Feeds, counts, search, recommendations, analytics | Eventual consistency |
| Transaction spans multiple services/stores/shards | Saga with compensating transactions |
| Whole operation fits one database | One ACID transaction — not a saga |
| Huge read/write asymmetry on one aggregate | CQRS for that aggregate |
| Plain symmetric CRUD | Single model — not CQRS |
| Messaging delivery guarantee | At-least-once + idempotent consumers |

## Exemplars (illustrations only)

Named to make a pattern concrete, **not** as defaults to adopt — each fits a scale and access pattern that is probably not yours:

- **Postgres** — the relational default that stretches far past where people abandon it: JSONB, full-text, geo, materialized views, logical replication, read replicas, and native table partitioning before you ever reach for cross-node sharding. Reach past it only for a pattern it genuinely can't serve.
- **Redis** — the in-memory exemplar for cache-aside, sessions, rate limiters, and ephemeral state. Fast because it's memory-first; treat it as a cache, not a system of record, unless you've explicitly configured and accepted its durability trade-offs.
- **Kafka** — the streaming/event-log exemplar for high-fan-out ingest, per-partition ordering, and replayable consumption. Its "exactly-once" holds inside its boundary; the moment an effect leaves it, idempotency is yours. Overkill for a few thousand events a day — a plain queue (SQS/RabbitMQ) or the outbox off your existing DB is simpler and enough.

Read each as "here is a tool whose access pattern was genuinely present," then check whether *your* requirements contain the same pattern. If they don't, the exemplar argues against adopting it, not for.
