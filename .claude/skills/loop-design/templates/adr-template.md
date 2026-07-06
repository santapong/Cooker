# ADR-NNNN: <short decision title, present tense — "Use Postgres as the primary datastore">

One ADR records **one** significant, hard-to-reverse decision. Copy the blank template below into `docs/adr/NNNN-slug.md`, fill every section, and leave the filled-in example at the bottom of this file for reference. Number ADRs sequentially and never renumber — a superseded ADR stays in the log with its status flipped, so the history of *why* survives.

The load-bearing section is **Alternatives Considered**: an ADR that doesn't name the option it rejected, and why, is just a changelog entry. If you can't name a credible alternative, the decision probably wasn't significant enough to need an ADR.

---

## Status

*One of:* **Proposed** · **Accepted** · **Deprecated** · **Superseded by [ADR-NNNN](NNNN-slug.md)**

Start at Proposed while under review; flip to Accepted when the decision is made. Never edit an Accepted ADR's decision later — write a new ADR that supersedes it, and set this one to *Superseded by ...*. Use Deprecated when the decision no longer applies but nothing replaced it.

Date: YYYY-MM-DD · Deciders: <names/roles>

## Context

The forces in play: the requirement, constraint, or problem that forces a choice now. Cite the concrete numbers this decision answers to — scale, latency/availability target, consistency need, team size, compliance — so a future reader can tell whether those forces still hold. State facts and constraints, not the decision; the reader should feel the tension before they see the resolution. Keep it to what's decision-relevant.

## Decision

"We will ..." — state the choice in one or two sentences, active voice, present tense. Be specific enough that someone could act on it without asking a follow-up (name the tool/pattern/boundary, not just the category). If the decision has a scope limit or an explicit trigger for revisiting it, say so here.

## Consequences

What becomes true once this is in effect — the good and the bad, because every real decision has both. If a consequence has a mitigation, name it inline.

**Positive**

- <what this makes easier, safer, faster, or cheaper>
- <capability or option this preserves>

**Negative**

- <new cost, risk, or constraint this imposes — and its mitigation, if any>
- <the next bottleneck: where this design breaks first, and at what scale>

## Alternatives Considered

For each real option you rejected, name it and give the one reason it lost. This is the section future-you will actually reread.

- **<Alternative A>** — <why rejected: which requirement it failed, or what cost it added>
- **<Alternative B>** — <why rejected>
- **Do nothing / defer** — <why acting now beats waiting, or why it doesn't>

---

# Example (filled in)

Below is a complete ADR using the template above, so the shape is concrete. Delete this section from your real ADRs.

## ADR-0007: Use cursor-based pagination for the public list API

### Status

**Accepted**

Date: 2026-06-18 · Deciders: API guild, Platform lead

### Context

The `/v1/orders` and `/v1/events` list endpoints back an infinite-scroll UI and several third-party integrations that page through the full result set. Both collections receive concurrent writes at up to ~200 inserts/sec during peak. Our current offset pagination (`?page=3&size=50`) silently skips or repeats rows when items are inserted or deleted between requests, and integrators have filed three data-integrity bugs traced to exactly this. p99 latency on deep pages (`offset > 50k`) has crept past our 150 ms target because the database still scans and discards the skipped rows.

### Decision

We will use **opaque cursor-based pagination** on all public collection endpoints: each response returns a `next_cursor` encoding the sort key of the last row, and clients pass it back as `?cursor=...`. Offset/page parameters are removed from `/v1` list endpoints. The cursor is opaque (base64 of the keyset) so we can change its internals without breaking clients.

### Consequences

**Positive**

- Stable under concurrent writes — no skipped or duplicated rows, which closes the integrator bug class directly.
- Deep pages stay fast: the query becomes an indexed `WHERE (sort_key) > (cursor)` range scan, so p99 no longer degrades with page depth. Keeps us under the 150 ms target.

**Negative**

- No random access to page N and no total-count-driven "jump to last page"; the UI must switch to infinite scroll or next/prev only. Mitigation: product already uses infinite scroll on both surfaces.
- Requires a stable, unique sort key with a matching index on every paginated collection. Mitigation: add a composite `(created_at, id)` index in the same migration; `id` breaks ties so the keyset is total.
- Next bottleneck: cursors are tied to a single sort order. Adding user-selectable sort columns later means one cursor scheme per sort key — revisit if/when multi-sort ships.

### Alternatives Considered

- **Keep offset pagination** — fails the concurrent-write correctness requirement outright and degrades past our latency target on deep pages; it's the status quo we're fixing.
- **Offset pagination + a snapshot/consistent-read window** — would fix correctness but requires holding a server-side snapshot per client session, adding state and memory cost we don't want for anonymous third-party callers.
- **Seek pagination with an exposed (timestamp, id) tuple instead of an opaque cursor** — functionally equivalent, but leaking the tuple couples clients to our sort internals and blocks changing them later; opaqueness costs nothing and buys reversibility.
- **Do nothing / defer** — rejected: the bugs are active and customer-visible, and offset latency worsens as the tables grow, so waiting only raises the migration cost.
