---
name: cooker-improve
description: Quality / refactor / known-issue-fix workflow for the Cooker codebase. Trigger on "improve X", "refactor Y", "fix theme T<n>", "tighten X", "clean up Y", "implement the remediation for …", or "close audit finding [A<n>-<m>]". Pairs with cooker-find — find first, then improve.
---

# Cooker — improve workflow

Cooker has a published remediation plan and ~60 catalogued findings. Use this skill to land an improvement that fits the existing pattern instead of inventing a new one.

## Layering (do not break)

```
handler (HTTP only) ──► service (business logic) ──► store / strategy adapter
```

- **No** business logic in handlers.
- **No** HTTP types in services.
- **No** `panic` outside startup.
- **Errors** wrapped with package prefix: `fmt.Errorf("oidc: discover: %w", err)`.
- **Typed errors** via `errors.Is`: `store.ErrNotFound`, `store.ErrConflict`.

## Pre-flight: check the audit

Before touching code:

1. Read the relevant section of `docs/audits/remediation-plan.md` for the theme — file:line + fix sketch + effort estimate.
2. Cross-check `docs/audits/chain-recheck.md` to confirm the finding isn't already closed.
3. If a chain has moved since the audit, the line number in the citation may be ±a few; the file path is correct.

## Per-PR rhythm (one theme = one commit)

1. **Branch** from main: `claude/<area>-<topic>` or `fix/T<n>-<slug>`.
2. **Implement** the change. Keep it scoped — resist the urge to also "tidy up nearby."
3. **Run the focused check** before committing:
   ```bash
   .claude/skills/cooker-improve/check-pkg.sh internal/<changed-pkg>
   ```
   That's `gofmt -l`, `go vet`, and `go test -race` on the package — same gates CI uses, but scoped, so the loop is fast.
4. **Cross-reference docs.** If the change closes a chain in `chain-recheck.md`, flip its row from "Open" to "Closed by …" and cite the new commit SHA.
5. **Commit message** must reference what it closes:
   ```
   <type>(<pkg>): T<n> — <short title>
   
   <why + how, ~5–15 lines>
   
   Closes T<n> (and the related [A<n>-<m>]) in
   docs/audits/remediation-plan.md (Phase <p>).
   ```
6. **Push**, open as **draft PR**. CI gates merge.

## Where to put new code

| Adding | Lands in |
|---|---|
| Cross-cutting helper (one-purpose, used by ≥2 callers) | New `internal/<thing>/` package — see `internal/{retry,validate,idempotency}` as templates |
| Schema change | New migration `backend/internal/store/postgres/migrations/<NNN>_<topic>.up.sql` + matching `.down.sql`. NNN strictly monotonic. |
| New stage type | `model/pipeline.go` const + `service/executor.go` switch arm + `validate/validate.go` enum + tests in all three |
| New external adapter (builder / pusher / deployer / secrets) | Implement the interface; add a constructor case to `selectXxx` in `server/server.go`; document the env-var value in `.env.uat.example` and `docs/UAT.md` |
| New HTTP route | Handler in `internal/handler/<domain>.go`; register in `internal/server/router.go` with the appropriate role + middleware (writeRole / adminRole / mfa) |

## Anti-patterns to refuse

- Don't add a feature flag where a config knob already exists.
- Don't add backwards-compat shims for fields that don't have any callers yet.
- Don't `panic` in non-init code; return an error.
- Don't pin alpine apk versions without verifying they exist (T24 lesson — fixed in `d4c2d64`).
- Don't add a finding to an audit doc without `file:line`.
- Don't catch a context error (`context.Canceled` / `DeadlineExceeded`) and retry — `internal/retry.IsContextErr` is the classifier, treat ctx errors as terminal.
- Don't extend the `dagrunner.Runner` to do stage-level work — it's the level-scheduler; stage logic lives in the executor's per-stage callbacks.

## Quick checks

After any change:

| Question | Command |
|---|---|
| Does this race? | `go test -race ./<pkg>/... -count=10` |
| Is the migration reversible? | `psql ... < up.sql && psql ... < down.sql && psql ... < up.sql` |
| Does CI gofmt match? | `gofmt -l backend` |
| Does the new error implement `errors.Is`? | `errors.Is(err, store.ErrConflict)` should compile |
| Is the new endpoint authenticated? | Find the route in `router.go`; confirm it's not before `oidcMW.Verify` |
