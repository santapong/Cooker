# Cooker Launch — Orchestration Protocol (PM playbook)

This is how the PM (me) runs the Opus delivery team to execute `docs/launch/README.md`.
It encodes **loop engineering** (work as bounded loops with terminal conditions) and
**harness engineering** (externalised state, context hygiene, structured handoffs, log
rotation) so the effort survives context compaction and is fully resumable.

---

## 1. Roles

- **PM (orchestrator, Opus):** owns `docs/launch/` plan, `.agent/state.md`, sequencing,
  delegation, **review of every change**, the audit, the research pass, git/PRs. Writes
  almost no product code — only `.agent/**` and trivial glue.
- **Delivery agents (Opus subagents):** the specialised `cooker-*` agents (their domain
  ownership + built-in gates make them the right tools) and `general-purpose` for greenfield
  (billing/SLA). **Every Agent call passes `model: "opus"`** per D3.

### Agent → domain map
| Domain | Agent |
|--------|-------|
| Helm / manifests / Dockerfile / observability artifacts | `cooker-infra-deploy` |
| CI / Makefile / workflow gates | `cooker-infra-ci` |
| Auth / secrets / security hardening / SECURITY.md | `cooker-security` |
| HTTP handlers / services / routing | `cooker-backend-api` |
| Store / schema / migrations | `cooker-backend-data` |
| Builder/pusher/deployer adapters | `cooker-backend-adapters` |
| React pages/components | `cooker-frontend-ui` |
| Zustand/api-client/hooks/auth | `cooker-frontend-state` |
| Cross-stack feature coordination | `cooker-feature-dev` |
| Greenfield design+build (billing, licensing, SLA tooling) | `general-purpose` |
| Read-only investigation / audit fan-out | `Explore` |

---

## 2. The milestone loop (loop engineering)

Each milestone runs the **same bounded loop** with an explicit terminal condition. The PM
never declares a milestone done until its DoD is green; on failure it **re-diagnoses and
re-kicks**, it does not give up after one round.

```
PLAN ──► DELEGATE ──► REVIEW ──► VERIFY ──► AUDIT ──► RESEARCH ──► RECORD ──► ADVANCE
  ▲                                  │ fail                          │
  └──────────────── re-kick ◄────────┴───────────────────────────────┘
```

1. **PLAN** — break the milestone into non-overlapping tasks (distinct file sets → no write
   conflicts). Write the task table into `state.md` (Active tasks).
2. **DELEGATE** — launch Opus agents in parallel (one message, multiple Agent calls) when
   tasks are independent. Each prompt is a **task contract** (§3).
3. **REVIEW** — PM reads every returned diff (`git diff`), checks against the plan + CLAUDE.md
   conventions (layering, no localStorage outside auth/, secretKeyRef, etc.). Reject → re-kick
   the same agent via `SendMessage` (keeps its context) with precise corrections.
4. **VERIFY** — PM runs the gates itself (§5). Red → re-kick with the failing output.
5. **AUDIT** — after the milestone's code is green, run the audit pass (§6). Findings →
   `state.md` Risk register.
6. **RESEARCH** — for each 🟡/🔴 finding or notable design choice, run the research pass (§7).
   → `state.md` Research log.
7. **RECORD** — update `state.md` board + ledgers; append a log entry (§4); rotate if needed.
8. **ADVANCE** — commit, push, open/update a **draft PR** (one PR per milestone = one logical
   change), move board to next milestone. Respect gates (M3+ blocked on D4 confirmation).

**Terminal condition per milestone (DoD):** plan tasks complete · PM review passed ·
`go vet` + `go test -race` + frontend `lint`/`build`/`test` + `helm lint`/`kubeconform` green ·
audit findings triaged · research recorded · draft PR open · `state.md` updated.

---

## 3. Task contract (how the PM talks to an agent)

Every delegation prompt contains, in order:
1. **Context** — milestone, the one-paragraph why, and the relevant `docs/launch/` section.
2. **Scope (do exactly this)** — the precise change, bounded to a **named file set** that does
   not overlap any sibling agent running in parallel.
3. **Reuse these anchors** — concrete paths/patterns to mirror (from the exploration anchors),
   so agents extend existing code, not invent.
4. **Constraints** — CLAUDE.md rules that apply; **never run `go mod tidy`**; don't touch files
   outside scope; Opus.
5. **Definition of done** — what to produce + which gates to run before returning.
6. **Return** — a tight summary: files changed, gates run + results, risks/assumptions, TODOs.

**Resuming an agent:** use `SendMessage` to the agent's id to continue with its context intact
(corrections, follow-ups). A fresh `Agent` call = fresh context; use only for new tasks.

**Parallelism rule:** agents run in parallel **only** when their file sets are disjoint.
Overlapping work is serialised, or merged into one agent.

---

## 4. Logging & rotation (harness engineering)

- `state.md` — **snapshot only** (board + ledgers + next actions). Never narrative. Stays small.
- `.agent/log/current.md` — append-only event log. One entry per loop step / delegation /
  review / gate-run / decision. Format:
  ```
  ## [YYYY-MM-DD HH:MM] <Mn> <STEP> — <one-line>
  - detail bullets (agent id, files, gate results, verdict)
  ```
- **Rotation:** before appending, the PM runs `bash .agent/rotate.sh`. When `current.md`
  exceeds `MAX_LINES` (default 400), it is moved to `.agent/log/archive-<NNN>-<date>.md`, a
  line is added to `.agent/log/index.md`, and a fresh `current.md` is started with a back-link.
  This caps any single file so context reads stay cheap.
- **Context hygiene:** the PM keeps its own context lean — delegate broad reads to `Explore`,
  never tail subagent transcript files, and rely on `state.md` (not memory) as the source of
  truth after compaction.

---

## 5. Verification gates (PM runs these, not the agents' word for it)
- Backend: `cd backend && go vet ./... && go test ./... -race`
- Frontend: `cd frontend && npm run lint && npm run build && npm test`
- Deploy YAML: `helm lint deploy/helm/cooker` + `kubeconform` (once M0-T3 lands the gate)
- Docker: `docker build -f deploy/docker/Dockerfile .` for Dockerfile-touching milestones
- PM spot-reads the actual diff for every task — green gates are necessary, not sufficient.

---

## 6. Audit pass (after each milestone implementation)
Run the `cooker-audit` skill / `Explore` fan-out over the milestone diff with this checklist:
1. **Correctness/bugs** introduced by the change.
2. **Technical debt** — shortcuts, missing tests, TODOs, duplicated logic.
3. **Side-effects** — did it touch/alter behaviour of *other* functions or callers? (grep usages)
4. **Convention breaches** — layering, error wrapping, store parity, security rules.
5. **Regressions** — does it weaken anything #115/earlier audits fixed?
Each finding → Risk register row: severity · what introduced it · recommended change.

## 7. Research pass (after audit)
For each notable design choice or 🟡/🔴 finding, ask: **is there a better way?**
- Use `Explore`/`general-purpose` (+ web research where allowed) to compare ≥1 alternative.
- Record: current approach · alternative · trade-offs · verdict (keep/change) → Research log.
- If "change" wins and it's cheap+safe, re-kick the owning agent; else log as follow-up.

---

## 8. Honest constraints
- The full 3-lane plan is **months** of work; a session executes milestone-by-milestone and
  **persists state** so the next session resumes from `state.md`. The PM never claims a lane
  done that isn't merged.
- M3+ stay **blocked** until D4 (Cooker Cloud go/no-go) is confirmed by the user.
- Per CLAUDE.md: branch from `main`, draft PRs, squash-merge, never push to `main` directly,
  update `SECURITY.md`/`docs/guides/UAT.md` when relevant, **never `go mod tidy` in backend/**.
