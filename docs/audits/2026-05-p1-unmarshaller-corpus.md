# P#1 Retry-policy back-compat unmarshaller corpus (2026-05)

> Status: research deliverable. Read-only. No code shipped here.
> Gated on T5 landing. Targets W5 of the 20-week calendar in `docs/dag-adaptation-2026.md` §10.
> References: `dag-adaptation-2026.md` §7.1 (model + migration) and §4 (primitive rationale).

---

## 1. Semantic baseline (read before test cases)

`executor.go:209` today: `MaxAttempts: 1 + stage.Config.Retries`

Old `Retries int` counts **extra retries beyond the first attempt**. A value of `3` means the
stage runs at most four times (1 initial + 3 retries). `MaxAttempts = 1` means no retry.

The executor also hardcodes `Initial: 1s, Max: 15s` (`executor.go:208-219`); these become the
implicit defaults the new struct must document explicitly.

New `RetryPolicy` `Exponential bool` is tagged `omitempty`. Its zero value (`false`) is therefore
indistinguishable from "not set" after a JSON round-trip. **See Decision #1 below.**

---

## 2. Five round-trip test cases

### TC-1 — Old shape, non-zero retries

Input JSON fragment inside `StageConfig`:
```json
{"retries": 3}
```

Expected Go value after `UnmarshalJSON`:
```go
StageConfig{
    Retry: RetryPolicy{
        MaxAttempts: 4,   // 1 + 3 (executor.go:209 semantics preserved)
        InitialMS:   1000, // executor default: 1 s
        MaxMS:       15000, // executor default: 15 s
        Exponential: false, // zero-value; see Decision #1
    },
}
```

Re-marshal produces `{"retry":{"maxAttempts":4,"initialMs":1000,"maxMs":15000}}`.
`retries` key is gone; the old field is consumed and promoted.

---

### TC-2 — New shape, fully specified

Input:
```json
{"retry": {"maxAttempts": 3, "initialMs": 100, "maxMs": 1000, "exponential": true}}
```

Expected:
```go
StageConfig{
    Retry: RetryPolicy{MaxAttempts: 3, InitialMS: 100, MaxMS: 1000, Exponential: true},
}
```

Re-marshal reproduces the input verbatim (all fields non-zero, none elided by `omitempty`).

---

### TC-3 — Old shape, zero retries (no retry)

Input:
```json
{"retries": 0}
```

`Retries` zero-value with `omitempty` means this key is absent in new saved docs, but old
stored rows may contain `"retries": 0` explicitly.

Expected:
```go
StageConfig{
    Retry: RetryPolicy{MaxAttempts: 1}, // 1+0 = 1, no retry
}
```

`InitialMS` and `MaxMS` default to executor values at call time; they are omitted from the
persisted struct because they are zero (the executor applies its own floor via `retry.Do`).

---

### TC-4 — New shape, empty object (no retry)

Input:
```json
{"retry": {}}
```

Expected:
```go
StageConfig{
    Retry: RetryPolicy{MaxAttempts: 1}, // treat absent/zero as "run once"
}
```

The unmarshaller must clamp `MaxAttempts` to a minimum of 1 (matching `retry.Do:62-63`).
An empty `{}` value for `retry` is equivalent to "no retry" and must not panic or leave
`MaxAttempts` at its zero value `0`.

---

### TC-5 — Both keys present (conflict)

Input:
```json
{"retries": 2, "retry": {"maxAttempts": 3}}
```

Per `dag-adaptation-2026.md` §7.1 back-compat rule: **the new shape wins.**

Expected:
```go
StageConfig{
    Retry: RetryPolicy{MaxAttempts: 3},
}
```

The `retries: 2` value is silently discarded. The unmarshaller reads `retry` first; if
the key is present, it short-circuits without reading `retries`. A `slog.Warn` on the
discarded field is recommended so operators know old JSONB rows have conflicting data.

---

## 3. Custom `UnmarshalJSON` skeleton

```go
// executorDefaultInitialMS and executorDefaultMaxMS mirror executor.go:208-219.
// They become the fill-in defaults when converting old Retries int → RetryPolicy.
const (
    executorDefaultInitialMS = 1000  // 1 s
    executorDefaultMaxMS     = 15000 // 15 s
)

func (sc *StageConfig) UnmarshalJSON(data []byte) error {
    // Use a shadow type to avoid infinite recursion.
    type plain StageConfig
    var raw struct {
        plain
        // Old field — accepted on ingest, never written on marshal.
        Retries *int `json:"retries,omitempty"`
    }
    if err := json.Unmarshal(data, &raw); err != nil {
        return err
    }
    *sc = StageConfig(raw.plain)

    // New shape wins (TC-5). Only fall back to old Retries if retry is absent.
    if sc.Retry == (RetryPolicy{}) && raw.Retries != nil {
        extra := *raw.Retries
        if extra < 0 {
            extra = 0
        }
        sc.Retry = RetryPolicy{
            MaxAttempts: 1 + extra,
            InitialMS:   executorDefaultInitialMS,
            MaxMS:       executorDefaultMaxMS,
        }
    }
    // Clamp MaxAttempts floor to 1 (TC-4).
    if sc.Retry.MaxAttempts < 1 {
        sc.Retry.MaxAttempts = 1
    }
    return nil
}
```

`RetryPolicy` itself needs no custom unmarshaller — standard `encoding/json` handles it.
The `Retries` field on `StageConfig` is **removed** from the struct definition; the shadow
type in `UnmarshalJSON` is the only consumer of the old key.

---

## 4. Migration risk register

### R-1 — `omitempty` on `Exponential bool` hides intent

**Risk.** The design (§7.1) annotates `Exponential bool` with `// default true`. But `omitempty`
on a `bool` elides `false`. A row with `{"retry":{"maxAttempts":3}}` is ambiguous: did the
author mean "no exponential backoff" or "use the default (true)"?

**Impact.** If the executor interprets absent `Exponential` as `false` (linear delay), pipelines
that relied on the implicit "default true" will behave differently after the migration.

**Decision needed before P#1 starts (Decision #1).** Pick one:
- (a) Change the struct tag to `json:"exponential"` (no `omitempty`). Adds noise to every
  serialised RetryPolicy but makes intent explicit. Recommended.
- (b) Keep `omitempty`; define "absent = false = linear". Update the §7.1 comment to drop
  the "default true" claim. Executor sets `Exponential: true` only when the user explicitly
  sets the field.
- (c) Use a pointer `*bool` so nil = "use default (true)". More idiomatic for tri-state but
  adds a dereference at every call site.

### R-2 — `StageConfig` fields nested inside other stage-type-specific structs

**Risk.** `dag-adaptation-2026.md` §7.1 assumes `Retries` lives at the top level of a single
`StageConfig` flat struct. Confirmed by `backend/internal/model/pipeline.go:45-90`: `StageConfig`
is flat; there is no nesting. However, any stage type that embeds or wraps `StageConfig` in a
sub-struct (e.g. a future `BuildConfig struct { StageConfig; CacheSpec }`) would bypass the
custom unmarshaller unless the embedding propagates the custom method. **Not a current risk**
(no wrapping exists today) but must be reviewed before any future struct-splitting refactor of
`StageConfig`.

### R-3 — `executor_test.go` fixtures do not use `Retries` today

**Risk.** A grep of `backend/internal/service/executor_test.go` finds zero occurrences of
`Retries` in test fixtures. No hard-coded `Retries: 2` exists at this time. **No test breakage
expected** from renaming the field — but confirm with `go test ./internal/service/... -run .`
before merging the P#1 PR.

### R-4 — Old JSONB rows with both `retries` and `retry` keys (TC-5)

**Risk.** If any pipeline was hand-edited in the DB or via a migration script to contain both
keys simultaneously, TC-5's "new wins, old discarded" rule silently drops the `retries` value.
A one-time `SELECT id, stages FROM pipelines WHERE stages::text LIKE '%"retries"%'` query
before deploying P#1 lets the operator audit the scope. The unmarshaller should emit a
`slog.Warn("StageConfig: both retries and retry present; retries discarded", ...)` to aid
post-deploy log inspection.

### R-5 — `retry.Do` floor vs. unmarshaller floor

**Risk.** `retry.Do` clamps `MaxAttempts < 1` to `1` at call time (`retry.go:62-63`). The
unmarshaller does the same clamp (TC-4 skeleton above). Having two clamp sites is not a bug
(defence-in-depth), but it means a stored `{"retry":{"maxAttempts":0}}` is silently corrected
twice. Prefer the unmarshaller clamp as the canonical correction point and document this so
a future refactor does not remove the `retry.Do` floor under the assumption "the model enforces
it."
