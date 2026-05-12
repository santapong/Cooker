# Primitive #1 sub-delegation context-pack (2026-05)

Forward-looking prep for the cross-stack PR that ships **Primitive #1 — Retry
policies with backoff** (`docs/dag-adaptation-2026.md` §7.1). Context-pack,
not implementation: file:line anchors so the future cooker-feature spawn can
hand each sub-agent a focused brief without re-discovering the codebase.

**Gate.** P#1 is **blocked on PR-T5** (`docs/dag-adaptation-2026.md` §6 T5,
"Required before: Primitives #1, #2, #5"). PR-T5 is sequenced for W4 of the
30-day plan; P#1 fires in W5. PR-T5 is **not yet open** as of 2026-05.

**Sanity check — does T5's batched persistProgress conflict with P#1's
retry-status emit?** No. T5 introduces a debounced drain goroutine over
`runner.Updates()` (write at most once per `min(500ms, 10 transitions)`, with
eager flush on terminal `failed`/`success`). P#1 increases the transition
rate (each retry attempt emits a transition); T5 is precisely the
prerequisite that absorbs the increase. The terminal-flush rule means the
last attempt's outcome still surfaces without debounce lag. Complementary,
not conflicting.

---

## 1. `cooker-backend-api` brief

**Model — today's `Retries int`.**
`backend/internal/model/pipeline.go:70` (inside the `// Custom` block of
`StageConfig`). §7.1 cites `pipeline.go:69-70` — actual is line 70 alone (69
is `Timeout`). Minor doc drift, not a blocker.

**Model — new shape per §7.1.** Add `Retry RetryPolicy` sibling on
`StageConfig`. Define `RetryPolicy` in the same file. Keep `Retries int`
deprecated for back-compat unmarshalling.

**Back-compat `UnmarshalJSON`.** `StageConfig` is at
`backend/internal/model/pipeline.go:45`. No `UnmarshalJSON` exists today
(grep confirmed). Add one on `*StageConfig` that:
1. Unmarshals into a private alias to avoid recursion.
2. If JSON had `retry`, take it.
3. Else if JSON had `retries: N`, synthesise
   `Retry{MaxAttempts: 1+N, InitialMS: 1000, MaxMS: 15000, Exponential: true}`
   — mirroring the inline defaults at `executor.go:208-219`.

**Executor wiring — inline `retry.Policy` to replace.**
`backend/internal/service/executor.go:208-219` (verified: line 208 opens
`retry.Policy{`, line 219 closes; line 209 is
`MaxAttempts: 1 + stage.Config.Retries`). Lines 220–223 hold the stage-type
override forcing `MaxAttempts = 1` for `Approval`, `Custom`, `Test` — move
that rule inside the helper.

**`policyFromStage` helper signature (does not exist yet).**

```go
// policyFromStage builds a retry.Policy from a stage's config, preferring
// the new RetryPolicy shape and falling back to the legacy Retries int.
// Honors the stage-type rule that approval/custom/test stages do not retry.
func policyFromStage(stage model.Stage) retry.Policy
```

Place in `executor.go` near top, or in a new
`backend/internal/service/retry_policy.go` if executor.go is too long
(check before deciding).

**Call site.** Replace block at `executor.go:208-223` with
`retryPolicy := policyFromStage(stage)`.

**Test file.** `backend/internal/service/executor_test.go`.

**Existing retry test in `executor_test.go`.** None — grepped
`Retries|retry|Retry|MaxAttempts`, zero hits. §7.1's phrasing is correct
that this is net-new. The `retry` package itself is covered by
`backend/internal/retry/retry_test.go` (`TestDo_SucceedsImmediately`,
`TestDo_RetriesUntilSuccess`, `TestDo_StopsOnNonTransient`,
`TestDo_RespectsContextCancel`, `TestDo_ExhaustsMaxAttempts`,
`TestIsContextErr`).

**Where to sit the new test.** Alongside
`TestExecutor_BuildStage_DispatchesToBuilder` (line 284) and
`TestExecutor_BuildStage_BuilderErrorFailsStage` (line 343). Reuse the
failing-builder mock shape with a fail-twice-then-succeed variant. Name:
`TestExecutor_BuildStage_RetriesOnTransientError`. Assert three builder
calls and stage outcome `success` with
`MaxAttempts=3, InitialMS=100, MaxMS=1000, Exponential=true`.

**Hard rules.** No handler-request fields change → no migration. JSONB
storage stays per `001_initial.up.sql:8`. `policyFromStage` belongs in
service, never handler.

---

## 2. `cooker-frontend-ui` brief

**Drawer host.** `frontend/src/pages/PipelineEditorPage.tsx:99` —
`{selectedNodeId && <NodeConfigPanel />}`.

**Drawer component.**
`frontend/src/components/pipeline/panels/NodeConfigPanel.tsx` (179 lines).
Per-stage-type conditional input blocks: build 78–97, test 99–108, push
110–129, deploy 131–150, custom 152–161. "Promotion" `SectionLabel` at
163–169.

**Current "Retries" input — honest finding: it does not exist.** Searched
`NodeConfigPanel.tsx` for `retries|Retries|retry|Retry` — zero hits.
`StageConfig.retries` exists in the API contract
(`frontend/src/types/pipeline.ts:45`) but has no editor surface. §7.1's
"gains a Retry sub-section" wording holds — wholly new sub-section, not a
replacement. Drop it unconditionally before the "Promotion" `SectionLabel`
at line 163, so it shows for every stage type.

**Four inputs per §7.1.**

| Field         | Atom    | JSON path                    | Default |
|---------------|---------|------------------------------|---------|
| Max attempts  | `Input` | `config.retry.maxAttempts`   | 1       |
| Initial delay | `Input` | `config.retry.initialMs`     | 1000    |
| Max delay     | `Input` | `config.retry.maxMs`         | 15000   |
| Exponential   | `Toggle`| `config.retry.exponential`   | true    |

`Input` (`frontend/src/components/ui/atoms.tsx:350`) accepts `type="number"`
via spread props. `Toggle` is at `atoms.tsx:263`, signature
`{ on, label, onClick }`.

**Style — patterns to match.**
- `SectionLabel` (`atoms.tsx:186`) for the header — see usage at
  `NodeConfigPanel.tsx:164`.
- `Label` + `Input` pair in a plain `<div>` — see build/push/deploy blocks
  at `NodeConfigPanel.tsx:81-86, 113-118, 134-139`.
- Column flex gap 14px is set on the container at
  `NodeConfigPanel.tsx:65-75`; no per-field overrides.
- Use the in-bundle `Toggle`, not a hand-rolled checkbox.

**Stage types that don't retry.** Approval / custom / test (per backend
override at `executor.go:221-223`). Render disabled with a
`<Pill tone="neutral">stage type does not retry</Pill>` so the constraint
is discoverable.

---

## 3. `cooker-frontend-state` brief

**Slice file.** `frontend/src/stores/pipelineStore.ts` (182 lines) — only
slice holding stage config; verified by grepping `StageConfig` across
`frontend/src/stores/`.

**Type file.** `frontend/src/types/pipeline.ts:24-46` — `StageConfig`
interface; current `retries?: number` at line 45.

**Single function to update.** `updateStageConfig` at
`pipelineStore.ts:119-134`. Today: shallow `{ ...s.config, ...config }`
merge (line 126). The new `retry` is nested — shallow merge clobbers
sibling retry keys when the UI writes only one.

Two viable shapes:
1. **Type-level only.** Add `retry?: RetryPolicy`; UI always passes the
   full `retry` object on every change. Zero store-logic change.
2. **Deep-merge `retry` only.** Special-case the `retry` key in
   `updateStageConfig` so it does
   `retry: { ...s.config.retry, ...config.retry }` when both sides have it.

§7.1 says "one Zustand slice change" — option 2 is that one change.
Recommend it: keeps UI components dumb (each input writes only its own
key).

**Type additions** (mirror the forthcoming Go shape):

```ts
export interface RetryPolicy {
  maxAttempts?: number;
  initialMs?: number;
  maxMs?: number;
  exponential?: boolean;
}

export interface StageConfig {
  // ...
  /** @deprecated kept for back-compat unmarshalling on the server */
  retries?: number;
  retry?: RetryPolicy;
}
```

Place in `frontend/src/types/pipeline.ts` adjacent to the existing
`StageConfig` at line 24.

---

## Cross-cutting

- **No migration.** §7.1: pipelines stay in JSONB
  (`internal/store/postgres/migrations/001_initial.up.sql:8`); back-compat
  `UnmarshalJSON` on `StageConfig` handles the old shape on read.
- **No handler change.** Pipelines flow through generic CRUD handlers that
  marshal `model.Pipeline` whole; no new request fields per the CLAUDE.md
  rule.
- **Spawn order on the day.** `cooker-backend-api` first (model + helper +
  test, green); then `cooker-frontend-state` (types + slice); then
  `cooker-frontend-ui` (drawer). Backend lands only after PR-T5.
- **`backlog.md` hygiene.** When the P#1 PR merges, move its line item to
  "Closed" with the PR number in the same PR.
- **Doc drift.** §7.1 cites `pipeline.go:69-70`; actual is line 70 alone.
  One-line correction in `dag-adaptation-2026.md` in the implementation PR.
