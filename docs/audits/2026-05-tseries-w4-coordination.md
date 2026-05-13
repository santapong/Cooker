# T-series W4 coordination dry-run (2026-05 W3)

**Status.** Coordinator dry-run, read-only. W4 T-series (T2, T4, T5)
sequencing. T1 + T3 ship in W3 via `claude/w3-t1-t3-handler-f1`. T5 is
prerequisite for Primitive #1 in W5.

**Source of truth.** `docs/dag-adaptation-2026.md` §6 (T1–T5 definitions),
§10 weeks 1–3. Cross-refs: `docs/audits/2026-05-p1-context-pack.md` (on
`main`); `docs/audits/2026-05-deploytarget-walk.md` (W3 in flight on
`claude/w3-research-deploytarget-walk`).

**Line numbers** cited against `main` at `da8cb83`. §6 cited slightly stale
lines for `executePush`/`executeDeploy` (+2 after T1's stub replacement
landed); corrected anchors below.

---

## 1. T2 — Wire LogWriter for push + deploy

**Anchors on `main`.**
- Build reference wiring: `backend/internal/service/executor.go:300-342`
  (`cappedBuffer`+`io.MultiWriter`+`lineWriter` 301-321; `builder.Request{
  ... LogWriter: writer}` 322-329; `defer sr.Logs = logs.String()` 316-321).
- Push (no LogWriter today): `backend/internal/service/executor.go:385-404`.
- Deploy (no LogWriter today): `backend/internal/service/executor.go:406-443`.
- `pusher.Request`: `backend/internal/pusher/pusher.go:19-26` — add
  `LogWriter io.Writer`.
- `deployer.Request`: `backend/internal/deployer/deployer.go:26-41` — add
  `LogWriter io.Writer`.
- Pusher adapters: `backend/internal/pusher/{crane,docker,noop}.go` +
  `conformance_test.go` (extend contract test: ≥1 line written when set).
- Deployer adapters: `backend/internal/deployer/{clientgo,kubectl,noop}.go`.
  `clientgo_helpers.go` enumerates applied resources — natural write site.
- Frontend: `frontend/src/pages/RunPage.tsx:144-147` already wires
  `LogsPanel` from `useStageLogs(selectedStageId)` keyed by stage ID — the
  panel is stage-agnostic. **Frontend delta is near-zero**; behaviour
  change is "push/deploy stages now show log lines."

**Sub-agent assignment per `docs/design.md` §11 (interface-extension).**
- `cooker-backend-adapters` — extend both Request types with
  `LogWriter io.Writer` (nil-safe); every Pusher impl writes
  `Pushed image to <Target>@<Digest>\n`; every Deployer impl writes
  `Applied <kind>/<ns>/<name>\n` per resource (Helm: one line per release
  with revision).
- `cooker-backend-api` — mirror `executor.go:300-321` build wiring into
  `executePush` (385-404) and `executeDeploy` (406-443); add
  `defer sr.Logs = logs.String()`; thread `runID` into both signatures so
  `StageLogChannel(runID, stage.ID)` matches build.
- `cooker-frontend-ui` — visual fixture asserting push + deploy stages
  render log panels after run.

**Cross-reference.** T2 CONSUMES per-adapter findings from
`docs/audits/2026-05-deploytarget-walk.md` (W3 in flight). **Do not start
T2 coding until that audit lands** — its adapter list is the test matrix.

**Risk.** Medium. Per CLAUDE.md "new pluggable backend": extend interface,
update every impl, extend `backend/internal/pusher/conformance_test.go`.
No store migration needed (`StageRun.Logs` already persists).

**PR title.** `feat(executor,adapters): T2 — wire LogWriter through push + deploy`

**Verification.** `cd backend && go vet ./... && go test ./... -race` and
`cd frontend && npx tsc --noEmit && npm run lint && npm run build`. Manual:
UAT pipeline, click push stage, see "Pushed image to …".

**Closed line.** `backlog.md`: T2 → Closed with PR #.
`docs/dag-adaptation-2026.md` §6 T2: `**Closed:** PR #NN
(claude/w4-t2-logwriter-push-deploy).`

---

## 2. T4 — Refuse non-success Edge.Condition

**Anchors on `main`.**
- `Edge.Condition`: `backend/internal/model/pipeline.go:93-98` (shipped;
  JSON-tag comment lists `"success"`, `"failure"`, `"always"`).
- DAG builder: `backend/internal/service/pipeline.go:29-46`
  (`BuildDAGFromPipeline`).
- Validation hook: `backend/internal/service/pipeline.go:12-26`
  (`ValidatePipelineDAG` — natural place to append the condition check).
- Caller: `backend/internal/service/executor.go:152`.

**Behavior.** In `ValidatePipelineDAG`, after the existing edge loop, for
each edge where `e.Condition != "" && e.Condition != "success"`, emit
`"edge %s→%s: condition %q is not supported yet (W6 Primitive #2)"`.
Default `""` remains "treat as success." Forward-compat refusal — not yet
Primitive #2.

**Sub-agent assignment per `docs/design.md` §11.**
`cooker-backend-api` only. One file: `internal/service/pipeline.go`. One
test: extend `pipeline_test.go:98-114` `TestBuildDAGFromPipeline_Invalid`
with `Condition="failure"`.

**Cross-reference.** Primitive #2 (W6) replaces this refusal with real
evaluation (`docs/dag-adaptation-2026.md` §7.2).

**Risk.** Low. ~1h. Additive validation; default-empty edges keep existing
pipelines green.

**PR title.** `feat(service): T4 — refuse non-success Edge.Condition`

**Verification.** `cd backend && go vet ./... && go test ./... -race`.
Tests pin: `""` accepted, `"success"` accepted, `"failure"`/`"always"`
rejected.

**Closed line.** `backlog.md`: T4 → Closed with PR #.
`docs/dag-adaptation-2026.md` §6 T4: `**Closed:** PR #NN.`

---

## 3. T5 — Batched persistProgress via Updates channel

**Anchors on `main`.**
- `persistProgress` calls to remove:
  `backend/internal/service/executor.go:199` (Running), `:255` (failed
  terminal), `:261` (success terminal).
- Persist impl: `backend/internal/service/executor.go:291-298` (keep as
  private method; drain calls it).
- Drain slot: T3 already cleared `executor.go:265-272` (now a comment
  block); T5 reintroduces a debounced drain there.
- Synchronous Run: `backend/internal/service/executor.go:272`
  (`err = runner.Run(ctx)`). Drain must terminate cleanly when
  `runner.Updates()` closes (on `Run` return).

**Behavior.** Replace the three explicit calls with one goroutine started
just before `runner.Run` that: (1) ranges `runner.Updates()`; (2) persists
when `time.Since(last) >= 500ms` OR `transitions_since_last >= 10`;
(3) **eager-flushes immediately** on `Status == RunStatusFailed` /
`RunStatusSuccess` (terminal — final outcome must surface without
debounce lag); (4) on channel close, flushes pending and exits; (5) uses
`done chan struct{}` so the caller `<-done` after `Run` returns and
before the run-level terminal `persistProgress` (final write not racy).

**Sub-agent assignment per `docs/design.md` §11.**
`cooker-backend-api` only. File: `internal/service/executor.go`. Tests in
`executor_test.go`: (a) 1 write after 10 fast transitions <500ms (count
cap); (b) 1 write after 500ms with <10 transitions (time cap);
(c) **eager flush <5ms** on failed transition (pin); (d) **eager flush
<5ms** on success transition (pin); (e) no goroutine leak after
`runner.Run` returns (`goleak.VerifyNone` or sentinel `done` check).

**Cross-reference.** `docs/audits/2026-05-p1-context-pack.md` lines 8–18
confirms T5's debounce + eager-flush absorbs P#1's per-attempt
status-emit volume. **P#1 (W5) is blocked on T5 landing.**

**Risk.** Medium. Debounce timing is subtle. Two failure modes:
(a) final terminal write lost because drain exited before `Run` returned —
mitigated by `done` + flush-on-close; (b) eager flush coalesces with
debounced flush and double-writes — mitigated by `pending bool` reset on
every flush path.

**PR title.** `refactor(executor): T5 — batched persistProgress via Updates drain`

**Verification.** `cd backend && go vet ./... && go test ./... -race -run Persist`
then `go test ./internal/service/... -race -count=20` (flush timing flake).

**Closed line.** `backlog.md`: T5 → Closed with PR #.
`docs/dag-adaptation-2026.md` §6 T5: `**Closed:** PR #NN. Unblocks P#1 (W5).`
`docs/audits/2026-05-p1-context-pack.md` line 10: flip "PR-T5 is not yet
open" → `**PR-T5 merged in #NN.**`.

---

## 4. Sequence + parallelism

**Recommended order: T4 → T2 → T5.**

1. **T4 first** (~1h, one file). Landing first ensures W5/W6 work doesn't
   risk a YAML-imported pipeline shipping a stale `Condition` value.
2. **T2 second** (~½ day). Mechanical, gated on
   `docs/audits/2026-05-deploytarget-walk.md`. Sequencing T2 here gives
   that audit one more day of buffer.
3. **T5 last** (~1 day). Largest, timing-sensitive. Last slot gives W4's
   regression-test window to catch any debounce surprise before P#1
   starts W5.

**Parallelism.** T2 and T5 are file-disjoint (T2 = `executor.go:385-443`
+ adapter files; T5 = `executor.go:199/255/261/265` + new drain). Two
engineers: run in parallel. T4 slots into slack. **Do NOT parallelize
T2 + T5 on the same engineer** — merge conflict at `executor.go:255-272`
is mechanical, but holding LogWriter contracts and debounce semantics
simultaneously slows each more than sequential saves.

**One engineer.** Strict serial: T4 Mon AM → T2 Mon PM + Tue → T5 Wed +
Thu → regression buffer Fri. Matches §10 weeks 2–3.

**Branch names.** `claude/w4-t4-edge-condition-refuse`,
`claude/w4-t2-logwriter-push-deploy`, `claude/w4-t5-batched-persist-progress`.
All branch from `main`. No code dependencies between them
(`executor.go` overlap is at unrelated regions). Open as draft PRs in
sequence; rebase forward as each parent merges.

---

## 5. Out of scope

- Primitive #2 evaluator (W6, §7.2). Retry-policy model change (W5, P#1;
  see `2026-05-p1-context-pack.md`). Per-adapter LogWriter line-content
  audit — W3 deliverable on `claude/w3-research-deploytarget-walk`,
  consumed by T2. T1 + T3 — shipping W3 via `claude/w3-t1-t3-handler-f1`.
