# JSONB-cap enforcement design — Primitive #3 inter-stage outputs

> Status: **design audit, not approved.** Written 2026-05 W3.
> Feeds the P#3 implementation PR sequenced for weeks 11–14 of the 20-week DAG plan.
> Cross-references `docs/dag-adaptation-2026.md` §DR-2 (storage decision), §7.3 (P#3 design),
> §6 T5 (batched-persistProgress interaction).

---

## 1. Where to enforce the cap

**Recommendation: service layer (`executor.go`), not store layer.**

### Options

**Service layer (executor.go, at ingestion time).** After each adapter call, the executor
copies `result.Outputs` to `stageRun.Outputs` before calling `persistProgress`. The cap
check runs at that copy point — per-key value truncated to 4 KiB, total rejected past 32 KiB,
`_truncated[key]=true` marker appended for each dropped key.

**Store layer (at JSONB-marshal time).** The postgres and memory stores intercept
`StageRun.Outputs` during serialisation and enforce the cap there.

### Trade-off analysis

| Criterion | Service layer | Store layer |
|---|---|---|
| Testability | Cap is a pure-Go function over `map[string]string`. Unit-testable without DB or store setup. | Requires marshalling the full `StageRun` through the store to observe truncation. |
| Single choke point | No — memory store could accept uncapped data in tests if the executor is bypassed. | Yes — every write path (executor, future batch writer, admin tooling) is capped. |
| Error visibility | Truncation happens where the outputs are produced; easy to log with stage/run context. | Marshal-time truncation is invisible to the caller; the store cannot return a structured warning. |
| Alignment with existing pattern | `cappedBuffer` for logs is also enforced in `executor.go:26,344-373`. Consistent. | New pattern with no precedent in the codebase. |
| T5 batched-write compatibility | Cap fires before the batch queue; batch writer sees already-capped data. | Cap fires inside the batch writer's marshal path; ordering is entangled. |

**Rationale.** The existing log-cap precedent (`cappedBuffer`, `executor.go:344-373`) lives in
the service layer — the store is unaware of the 1 MiB log limit. Outputs follow the same
pattern. Testability is the decisive factor: a pure function
`ApplyOutputCap(outputs map[string]string, keyMax, totalMax int) (map[string]string, []string)`
is table-driven without any store setup; the returned truncated-key list feeds structured log
output at the call site. The store's role is dumb persistence — enforcing domain limits there
violates the layering contract in `CLAUDE.md`.

The single-choke-point concern is addressed by placing the cap function in its own package
(`backend/internal/buildplan/outputcap.go`) called from the one write path (`executor`).
A store-layer defensive assertion can be added later if a second write path emerges.

---

## 2. Migration path to a separate `stage_outputs` table

### Trigger conditions

Switch when any of: (a) a real-world stage output exceeds 32 KiB; (b) indexed lookups over
output keys are needed (e.g. "all runs where `stages.build.digest` starts with `sha256:abc`");
(c) JSONB column rewrite cost under T5 batching becomes measurable at scale (>500 stages/run).

### Target schema

```sql
CREATE TABLE IF NOT EXISTS stage_outputs (
    id          TEXT PRIMARY KEY,
    run_id      TEXT NOT NULL REFERENCES pipeline_runs(id) ON DELETE CASCADE,
    stage_id    TEXT NOT NULL,
    key         TEXT NOT NULL,
    value       TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (run_id, stage_id, key)
);

CREATE INDEX IF NOT EXISTS idx_stage_outputs_run_stage
    ON stage_outputs(run_id, stage_id);
```

### Back-fill query

```sql
INSERT INTO stage_outputs (id, run_id, stage_id, key, value, created_at)
SELECT
    gen_random_uuid()::text,
    pr.id,
    sr->>'stageId',
    kv.key,
    kv.value,
    pr.created_at
FROM pipeline_runs pr,
     jsonb_array_elements(pr.stage_runs) AS sr,
     jsonb_each_text(sr->'outputs') AS kv
WHERE sr->'outputs' IS NOT NULL
  AND sr->'outputs' != 'null'::jsonb
ON CONFLICT (run_id, stage_id, key) DO NOTHING;
```

Safe to re-run (`ON CONFLICT DO NOTHING`). No locks on `pipeline_runs` needed; only inserts.

### Dual-read window

Service reads from both sources and merges (new table wins on conflict); executor writes new
table only. After one release cycle, the JSONB `outputs` field is no longer written. After a
second cycle (data verified), the Go struct field is removed; no column-drop migration needed
(JSONB fields are schemaless).

### Estimated cost

~1.5 engineer-days (schema migration + back-fill + store changes for both impls + dual-read
window + conformance test update). Earliest: week 16, after P#3 stabilises.

---

## 3. Test corpus

Five round-trip cases. All assert memory and postgres impls behave identically (conformance
test pattern per `CLAUDE.md`).

**Case 1 — One key, 1 KiB value → stored intact.**
Input: `{"result": "<1024 bytes>"}`. Expected: identical map, no `_truncated` marker.

**Case 2 — One key, 5 KiB value → truncated; marker present.**
Input: `{"digest": "<5120 bytes>"}`. Expected: first 4096 bytes stored;
`_truncated["digest"]=true` appended.

**Case 3 — Five keys, each 4 KiB → 20 KiB total, all stored.**
Input: five keys each with a 4096-byte value. Total = 20 480 bytes — under the 32 KiB cap.
Expected: all five stored intact, no markers.

**Case 4 — Twenty keys, each 4 KiB → cap hits at key #8; remainder discarded.**
Keys #1–#8 = 32 KiB (at cap); #9–#20 discarded. `_truncated` map lists each discarded key.
Keys must be sorted lexicographically before accumulating totals — Go map-iteration is
non-deterministic; truncation boundaries must be key-name-stable for reproducible assertions.

**Case 5 — Empty string value → distinct from absent key.**
Input: `{"tag": ""}`. Expected: map present with `ok=true` on lookup. A dry-run stage may
emit an empty tag; `${stages.build.tag}` must resolve to `""` not an error. Assertion:
`len(stageRun.Outputs) == 1` and `stageRun.Outputs["tag"] == ""` after round-trip.

---

## 4. API surface impact

### What clients see

`GET /api/v1/pipelines/:id/runs/:runId` returns the `PipelineRun`, which embeds `StageRun[]`.
Each `StageRun` gains an optional `"outputs"` field (key→value map) and `"_truncated"`
(key→bool, present only when at least one key was truncated or shortened).

Contract: if `_truncated[key]` is absent, the value is byte-for-byte exact. If present, the
value is the first 4 096 bytes of the original. The combined output payload on the wire
never exceeds 32 KiB plus negligible marker overhead.

### OpenAPI sketch update (`docs/openapi.yaml`)

Required additions when P#3 ships:

```yaml
StageRun:
  type: object
  properties:
    outputs:
      type: object
      description: >
        Key-value map emitted by the stage adapter. Per-key cap 4 096 bytes; total cap
        32 768 bytes. Keys prefixed with `_` are reserved (e.g. `_truncated`).
      additionalProperties: { type: string }
    _truncated:
      type: object
      description: Present when values were truncated. Each key maps to true.
      additionalProperties: { type: boolean }
```

Additive — old clients ignoring unknown fields are unaffected.

---

## 5. Migration risk register

**Risk 1 — Pre-cap rows that already exceed 32 KiB.**
Likelihood: none — `outputs` is a new field; no existing row contains it. Safety check in
the P#3 PR template: `SELECT count(*) FROM pipeline_runs WHERE stage_runs @> '[{"outputs":{}}]'`
must return 0 before merging. Reviewer runs this against the UAT DB.

**Risk 2 — Concurrent writes to the same `stage_runs` JSONB.**
T5's single drain goroutine serialises all writes; no concurrent race after T5 lands. P#3
is sequenced weeks 11–14, after T5 (weeks 1–3), so this risk is closed before P#3 code is
written. If cherry-picked before T5, add `TODO(T5-required)` at the ingestion call site —
concurrent `persistProgress` calls produce last-write-wins semantics that can silently drop
outputs from a racing stage.

**Risk 3 — `_truncated` marker collides with a user-supplied output key.**
An adapter emitting `result.Outputs["_truncated"] = "v"` would be silently overwritten.
Mitigation: **reserve the `_` prefix** for system keys. `ApplyOutputCap` returns a structured
error (stage failure, not silent discard) for any input key beginning with `_`. Document in
`docs/extending.md` "Output keys" section. Unit test must assert `{"_anything": "v"}` is
rejected before any truncation logic runs.

**Risk 4 — Lexicographic sort stability across Go versions.**
`sort.Strings` is stable and version-independent; the sort is on key strings, not map
iteration order. Conformance tests must assert truncated-key lists by name, not position.

**Risk 5 — `_truncated` map grows large with many discarded keys.**
Worst case: 200 keys discarded → ~192 `_truncated` entries ≈ ~3 KiB overhead — negligible
against the 32 KiB cap. Document in `ApplyOutputCap` godoc.
