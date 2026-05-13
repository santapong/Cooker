# P#3 stage outputs — schema migration sketch (W5)

> Status: **research sketch, not approved.** Written 2026-05 W5.
> Companion to `docs/audits/2026-05-p3-jsonb-cap-design.md` (PR #58).
> Target implementation window: weeks 11–14 of the 20-week DAG plan
> (`docs/dag-adaptation-2026.md` §7.3).

---

## 1. Current shape of `pipeline_runs.stage_runs`

`pipeline_runs.stage_runs` is a `JSONB NOT NULL DEFAULT '[]'` column established
by `001_initial.up.sql`. It carries an array of `model.StageRun` objects serialised
by `encoding/json`. The Go struct as of `main` (commit `31a3712`):

```go
// internal/model/run.go
type StageRun struct {
    StageID    string     `json:"stageId"`
    Status     RunStatus  `json:"status"`
    StartedAt  *time.Time `json:"startedAt"`
    FinishedAt *time.Time `json:"finishedAt"`
    Logs       string     `json:"logs,omitempty"`
    Error      string     `json:"error,omitempty"`
    Artifacts  []Artifact `json:"artifacts,omitempty"`
}
```

The `logs` field is capped at 1 MiB by `cappedBuffer` (`executor.go:26`,
`stageLogCap = 1 << 20`). There is no existing JSONB-level CHECK constraint on
`stage_runs` or `pipeline_runs`.

---

## 2. P#3 addition — `Outputs map[string]string`

P#3 (Primitive #3, "inter-stage outputs") adds a key-value output map to each
`StageRun`. Subsequent stages read prior outputs via a DSL variable reference
(e.g. `${stages.build.image}`), described in `dag-adaptation-2026.md §7.3` as
Primitive #5 for weeks 18–20.

Proposed struct addition (not yet in `main`):

```go
type StageRun struct {
    StageID    string            `json:"stageId"`
    Status     RunStatus         `json:"status"`
    StartedAt  *time.Time        `json:"startedAt"`
    FinishedAt *time.Time        `json:"finishedAt"`
    Logs       string            `json:"logs,omitempty"`
    Error      string            `json:"error,omitempty"`
    Artifacts  []Artifact        `json:"artifacts,omitempty"`
    Outputs    map[string]string `json:"outputs,omitempty"`    // NEW
    Truncated  map[string]bool   `json:"_truncated,omitempty"` // NEW
}
```

The `omitempty` tag on both new fields is mandatory: existing rows decode
`Outputs` as `nil` (not an empty map), and the field is omitted from the wire
response when not present, keeping the payload backward-compatible for old
clients that ignore unknown fields.

Cap parameters (from PR #58 `2026-05-p3-jsonb-cap-design.md §1`):
- Per-key value cap: **4 096 bytes** (4 KiB)
- Total outputs cap: **32 768 bytes** (32 KiB)

Enforcement lives in `internal/buildplan/outputcap.go` (service layer, not store
layer), following the same placement as `cappedBuffer` for logs. The function
signature from the design doc:

```go
func ApplyOutputCap(
    outputs map[string]string,
    keyMax, totalMax int,
) (capped map[string]string, truncatedKeys []string)
```

---

## 3. Migration #010 — `010_stage_outputs.up.sql`

Because `stage_runs` is JSONB, adding `Outputs` to the Go struct requires no DDL
column addition — the field appears transparently once the Go binary is deployed.
The migration's role is:

1. Serve as an append-only history marker that P#3 landed and the schema is
   considered stable from this point.
2. Add a CHECK constraint that asserts the total column size stays within the cap
   established by `ApplyOutputCap`, providing a database-layer defence against
   any future write path that bypasses the service-layer cap.

```sql
-- 010_stage_outputs.up.sql
-- P#3 inter-stage outputs marker migration.
-- No DDL column change: stage_runs JSONB accepts the new "outputs" /
-- "_truncated" fields transparently via Go marshal. This migration adds a
-- CHECK constraint matching ApplyOutputCap's totalMax=32768, scoped to the
-- outputs sub-field only (avoids conflicting with the separate log cap).

BEGIN;

ALTER TABLE pipeline_runs
    ADD CONSTRAINT chk_stage_runs_outputs_cap
    CHECK (
        (
            SELECT COALESCE(sum(pg_column_size(elem->'outputs')), 0)
            FROM jsonb_array_elements(stage_runs) AS elem
            WHERE elem->'outputs' IS NOT NULL
              AND elem->'outputs' != 'null'::jsonb
        ) <= 32768
    );

COMMIT;
```

**Idempotency note.** `ADD CONSTRAINT` errors if the name already exists. The final
migration file must use the DO-block guard shown inline in the up-migration above.

Cap value audit: PR #58 §1 states `totalMax=32 KiB = 32 768 bytes`. The migration
literal `32768` must stay in lock-step with `ApplyOutputCap`'s constant. If that
constant changes, add a new numbered migration — never edit a shipped one.

---

## 4. Down migration — `010_stage_outputs.down.sql`

```sql
-- 010_stage_outputs.down.sql
-- Rolls back the outputs CHECK constraint added by the up migration.
-- The Outputs/Truncated fields in the Go struct become invisible on decode
-- (JSONB ignores unknown fields during Go unmarshal) so no data surgery is
-- needed on the column itself.

ALTER TABLE pipeline_runs
    DROP CONSTRAINT IF EXISTS chk_stage_runs_outputs_cap;
```

`DROP CONSTRAINT IF EXISTS` is idempotent. No data is lost; existing rows with
`outputs` populated remain in the column and will be read back by any binary that
still carries the `Outputs` struct field.

---

## 5. Backward compatibility

**Existing rows without `Outputs`.** The `omitempty` tag means:
- JSON marshal: field absent from wire when `Outputs == nil`.
- JSON unmarshal: field left as `nil` (zero value for `map[string]string`) on
  existing rows. Callers must use `len(sr.Outputs) > 0` or nil-check; a
  direct `sr.Outputs["key"]` on a nil map returns `""` without panic in Go,
  but `ok` will be false.

**Old binaries reading new rows.** Old binaries decode `StageRun` without the
`Outputs` field definition; `encoding/json` silently drops unknown fields. No
decode error; data is invisible to the old binary. Acceptable for a forward-only
deployment model (rolling updates replace old pods before traffic shifts).

**New binaries reading old rows.** `Outputs` decodes as `nil`. The DSL variable
resolver for `${stages.build.image}` must handle nil `Outputs` gracefully and
return an error (stage failed / output not set), not a panic.

---

## 6. Open questions for W6 (when P#3 actually ships)

**Q1 — Per-key size cap in addition to total cap.**
The design caps total `outputs` at 32 KiB with individual keys truncated to 4 KiB.
Should the CHECK constraint also enforce the per-key limit? A generated constraint
of the form `pg_column_size(elem->key) <= 4096` for every key is not feasible in
a CHECK expression without PL/pgSQL. Options: (a) trust `ApplyOutputCap` as the
sole enforcer; (b) add a trigger (expensive on every write); (c) promote to the
separate `stage_outputs` table (see PR #58 §2) where a column-level constraint
`CHECK (length(value) <= 4096)` is trivial. Recommendation: trust the service cap
for now; revisit at the stage_outputs extraction milestone (week 16).

**Q2 — Queryable outputs via generated column.**
If operators need queries like "all runs where `build.image` starts with
`sha256:abc`", a generated column
`outputs jsonb GENERATED ALWAYS AS (stage_runs->0->'outputs') STORED` is one
approach, but it only covers a fixed array index. A full-text GIN index on
`stage_runs` (`GIN (stage_runs jsonb_path_ops)`) supports
`stage_runs @? '$[*].outputs.image ? (@ starts with "sha256:")'` without a
generated column. Assess query patterns before adding either; neither is needed
for P#3's core delivery.

**Q3 — Read-after-write consistency in the executor (T5 drain).**
T5's `drainDone` channel (`executor.go:354`) ensures all `persistProgress` calls
complete before the run reaches terminal status, so a reader seeing `status=success`
also sees `outputs` for that stage. Risk: cherry-picking P#3 before T5 reintroduces
the concurrent last-write-wins hazard (PR #58 §5 Risk 2). The P#3 PR template must
include a `T5-required` prerequisite gate.
