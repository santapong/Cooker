---
name: cooker-new-feature
description: Routine for shipping a new user-facing feature in Cooker. Trigger on "add a new <thing>", "implement <feature>", "we need to support <X>", "build a <stage type|builder|deploy target>", or any new-capability ask. Bias toward respecting the existing layering and reusing extension points (selectXxx, model.StageType<N>, internal/<thing>) before inventing new ones.
---

# Cooker — new-feature routine

## Inputs the user provides

One of:
- A user-facing requirement ("we need to support pushing to GHCR with OIDC auth").
- A JIRA / issue ticket with acceptance criteria.
- A paragraph of design intent.

If acceptance criteria are missing, ask **one** clarifying question to surface them and stop. Don't guess at semantics.

## Steps

### 1. Sanity check: does this already exist?

Use **cooker-find** first. Half of "new feature" requests are actually "existing feature with a different name."

```bash
.claude/skills/cooker-find/where-is.sh <noun>
rg -i -n "<keyword from request>" backend/internal docs
```

If you find prior art:
- A built-in adapter that does the same job → "this already exists at `<path>`; do you want to extend it or are you asking for a parallel implementation?"
- A roadmap entry in `launch-readiness.md` → "this is R<n> on the post-launch roadmap; do you want to promote it now?"

Don't proceed with the build until you've confirmed no existing thing serves the request. For *external* prior art — "should we build this at all, or adopt a library/service?" — run `/loop-scout`; for architecture-level design decisions that deserve an ADR, use `/loop-design`.

### 2. Read the design checklist

`docs/reference/design.md` § 11 has the canonical "adding a feature" checklist. Read it. Map your feature into its layers:

| Layer | Question |
|---|---|
| **Model** | Is there a new struct field, a new enum value, or a new entity? |
| **Store** | Does it need a migration? A new query? An index? |
| **Service** | What's the business-logic shape? Does it reuse `Executor` / `AppDeployer` / `Promoter`, or is it a new helper? |
| **Adapter** | Is it a new builder / pusher / deployer / secrets backend? Then it implements the existing interface and slots into `selectXxx`. |
| **Handler** | What's the HTTP shape? Path, method, request/response struct, role required, rate-limited or not? |
| **Frontend** | New page? New API client method? New Zustand store? |
| **Helm** | Does it need a values knob? A new template? RBAC? |

If any answer is "I don't know yet," pause and design. Don't ship a partial vertical slice.

### 3. Find an existing template

For most feature shapes, Cooker already has one or two precedents:

| New shape | Template |
|---|---|
| New stage type | `model.StageTypeGitOpsCommit` + `service/executor.go` switch arm + `validate/validate.go` enum |
| New builder | `internal/builder/kaniko.go` (full impl), wired via `selectBuilder` in `server/server.go` |
| New pusher | `internal/pusher/crane.go` |
| New deployer | `internal/deployer/clientgo.go` (or, for cloud targets, `internal/deploytarget/render/render.go`) |
| New secrets backend | `internal/secrets/keepsave/` |
| New cross-cutting helper (idempotent ops, rate ctrl) | `internal/{retry,validate,idempotency}/` |
| New HTTP route | The handlers in `internal/handler/<domain>.go`, registered in `internal/server/router.go` |
| New Postgres column | Migration via `.claude/skills/cooker-improve/new-migration.sh <slug>` |

**Read the template before designing.** Match its style; don't invent a new pattern when an old one fits.

### 4. Plan the commits

Target one commit per layer, in dependency order so each one stands alone:

1. **Schema migration** (if needed). New `NNN_<slug>.up.sql` + `.down.sql` via `new-migration.sh`. Make NOT-NULL columns have a DEFAULT so it's compatible with rolling deploys (chain B.7.2).
2. **Model + store** changes. New / updated struct in `internal/model/`, new methods on the store interface, postgres + memory implementations together (don't ship one without the other — `internal/store/store.go` interface gates both).
3. **Validate + business logic**. New constants and validators in `internal/validate/`, new business logic in `internal/service/`. If you're adding a new stage type, this is where the executor switch arm and the retry-policy decision land.
4. **Adapter** (if needed). New file in `internal/<area>/<adapter>.go` implementing the interface; constructor case in `selectXxx`.
5. **Handler + router**. HTTP wrapper in `internal/handler/<domain>.go`, route + middleware in `internal/server/router.go`. Pick the right role: `writeRole` for mutations, `adminRole` for destructive, `mfa` for security-sensitive.
6. **Frontend** (if user-facing). API client method in `frontend/src/api/`, Zustand store update, page in `frontend/src/pages/`.
7. **Helm** (if it needs deployment changes). Values block, template additions, RBAC.
8. **Tests** (interleaved — write each layer's test in the same commit as the layer, not all at the end).
9. **Docs** (`docs/reference/architecture.md` for the system map, `docs/reference/design.md` § 11 if you added a new template, `.env.uat.example` for new env vars, `RUNBOOK.md` if there are new failure modes).

### 5. Implement, layer by layer

For each commit:

- Use `cooker-improve`'s rules: layering, error wrapping, no panic outside startup.
- Run the scoped check:
  ```bash
  .claude/skills/cooker-improve/check-pkg.sh internal/<changed-pkg>
  ```
- Reference the requirement / issue in the commit message.

If a layer needs more than ~150 lines, split it. Reviewers can't load 500-line commits.

### 6. Wire it in

For new pluggable backends (builder / pusher / deployer / secrets):

- Add the constructor case to `selectXxx` in `backend/internal/server/server.go`.
- Document the env-var value in `.env.uat.example` and `docs/guides/UAT.md`.
- Add a Helm values block for it.

For new stage types:

- Add to `model.StageType*` constants.
- Add to `validate.StageType` enum.
- Add the executor switch arm.
- Add a test that runs the new stage end-to-end.

### 7. Verify end-to-end

Before opening the PR:

- `cd backend && go test -race ./...`
- `cd backend && gofmt -l .` (must be empty)
- `cd backend && go vet ./...`
- `cd frontend && npm run lint && npm run build && npm test`
- `helm lint deploy/helm/cooker && helm template ...` (mirror the CI helm job for whichever values you exercise)

### 8. Branch + draft PR

- Branch: `feature/<area>-<short-name>` (e.g. `feature/builder-buildkit-cache`).
- One PR per feature, even if it spans many commits.
- PR title: `feat(<area>): <one-line summary>`.
- PR body must include:
  - The user-facing requirement / issue number.
  - The acceptance criteria as a test plan checklist.
  - The new env vars, Helm values, and migration numbers (if any).
  - A "rollout notes" section if it's not backwards-compatible — operators need to know whether they can roll back.

## Anti-patterns to refuse

- **Business logic in handlers.** If the handler does anything beyond parse → validate → call service → render, it's wrong.
- **HTTP types in services.** Services take and return models, not `*gin.Context`.
- **Inventing a new package when an existing one fits.** Look at `internal/{retry,validate,idempotency}` — if your helper looks like one of those, extend it instead.
- **Skipping the migration `.down.sql`.** T15 ships down for everything 002+. Yours must too.
- **NOT NULL without DEFAULT in a migration.** Breaks rolling deploys (B.7.2).
- **Adding a new env var without a `Validate()` gate.** Dev defaults reaching production is the most common configuration bug; T19's Validate is the catch.
- **Frontend store changes without API client + zustand updates in the same commit.** Splitting them creates a window where the SPA is broken.
- **Skipping tests "because it's a thin wrapper."** The bug surface is exactly the same as a thick one; the test is what catches the wiring mistake.
- **Marking the PR ready before CI is green.** Always start as draft; flip to ready after CI passes.

## When to stop and ask the user

- The acceptance criteria don't specify error / edge-case behaviour ("what should happen if X is null?").
- Two existing features could serve the request and you can't pick one ("do you want this in the existing builder package or as a parallel adapter?").
- The feature would change a public API (path, env var, schema column) in a way an operator can't roll back trivially.
- The feature touches auth, secrets, or the Dockerfile — those need a `SECURITY.md` review pass.
- Implementing it cleanly requires a roadmap item to land first (e.g., "this depends on R6 Redis idempotency").

## Output expected at the end

- A draft PR with one commit per layer (or close to it).
- All CI jobs green.
- Updated docs: at minimum `.env.uat.example` if you added an env var, `docs/reference/architecture.md` if you added a major component, `docs/reference/design.md` § 11 if you added a new template-worthy pattern.
- Test plan in the PR body, exercised manually if needed.

## Checklist before declaring done

- [ ] Acceptance criteria mapped to test cases
- [ ] One commit per layer (model → store → service → adapter → handler → frontend → helm)
- [ ] Migration includes `.up.sql` AND `.down.sql`; NOT NULL columns have DEFAULTs
- [ ] New env vars in `.env.uat.example` + `Validate()` gate
- [ ] All four CI jobs green (backend, frontend, helm, docker)
- [ ] PR title follows `feat(<area>): <summary>`; PR is draft
- [ ] Rollout-notes section in PR body if not backwards-compatible
