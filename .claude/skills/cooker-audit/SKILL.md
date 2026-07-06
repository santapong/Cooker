---
name: cooker-audit
description: Find bugs, SPOFs, vulnerabilities, and chain-error interactions in the Cooker codebase using the patterns learned from the existing four-doc audit series. Trigger on "find bugs", "audit X", "look for SPOFs", "vulnerabilities in Y", "what could break Z", "is this safe", or any open-ended investigation that doesn't have a specific file in mind. Pairs with cooker-find (which is for known-target lookups) — use cooker-audit when the target is "wherever the bug is."
---

# Cooker — bug / SPOF / vulnerability hunter

This skill encodes the anti-patterns that actually showed up in Cooker over the four-audit, ten-then-twenty-agent fan-out series. Use it when you're asked to *look for* problems rather than *fix* a known one.

## When to use this skill (vs the others)

| Question | Skill |
|---|---|
| "Where is X?" — known target | `cooker-find` |
| "Fix theme T<n>" — known target | `cooker-improve` |
| "Pick this week's fix" — known target list | `cooker-weekly` |
| "Why's CI red?" — known signal | `cooker-ci-debug` |
| Generic security/quality review with no Cooker audit corpus (another repo, vendored code) | `loop-review` |
| Impact/risk report for a diff, PR, or release | `loop-audit` |
| **"Find bugs in X" / "audit Y" / "is this safe?" — in Cooker** | **this skill** |

If the user wants a finding, this skill. If they want a known answer, one of the others. The methodology here matches `loop-review`; what this skill adds is the Cooker corpus — the known-false-positives table, the heat-map, and the don't-re-flag routing into `docs/audits/`.

## Read these first (don't re-discover what's already audited)

- `docs/audits/chain-recheck.md` — every chain we already know about, with current Open / Closed status.
- `docs/audits/launch-readiness.md` § "Known issues that *are* visible in UAT but won't break it" — items already documented as expected.
- `docs/audits/remediation-plan.md` — themes T1–T24 + W1–W5 already landed.

If a finding has a `[B.X.Y]` or `[A<n>-<m>]` citation in those docs, **don't re-flag it** — instead, point at the existing reference and ask whether the work to close it should be promoted out of the roadmap.

## Anti-patterns I keep seeing in Cooker

These are the patterns that produced real findings, in priority order. When sweeping a new area, run through this list before composing a generic "find bugs" prompt.

### 1. Comments promise behaviour the code doesn't do

The single highest-yield pattern. Search for declarative comments and check the code under them:

```bash
rg -B2 -A8 'extended with a [0-9]+|deadline|timeout|will retry|cleans up' backend
```

Confirmed examples:
- `runs.go:Spawn` said "extended with a 30-minute deadline" — never called `WithTimeout`. Fixed by W2 `runDeadline`.
- `executor.go:281` (Custom stage) "logged but timeout never enforced" — fixed by T10.
- `kaniko.go:179` TTL=300s "Job cleanup" comment was right but the orphan-sweep collision wasn't documented.

When you see "should X" or "matches Y" in a comment, grep the code for the verb in the next 50 lines. If it's not there, that's a finding.

### 2. Per-process in-memory state in a "supports multi-replica" feature

Every map / channel / sync.Map in `backend/internal/server/` is a candidate. The pattern:

```bash
rg -n 'make\(map\[' backend/internal/server
rg -n 'sync\.Map' backend/internal/server
```

Confirmed: `wsticket.go`, `ratelimit.go`, `idempotency/idempotency.go` all have per-process state. Cooker exposes Redis backends for the first two; idempotency is single-replica until R6 lands.

When you find one, ask: "what happens with replicaCount=3 + no sticky sessions?" If the answer is "broken", that's a finding even if there's a documented mitigation — the mitigation needs to be in the operator's checklist (`launch-readiness.md`).

### 3. Fail-stop on first stage error, no retry, no backoff

`backend/pkg/dagrunner/runner.go` returns the first error from `errCh`. `backend/internal/service/executor.go` used to halt on any stage error. Fixed by T10 (retry + per-stage timeout) — but the *pattern* shows up wherever a function does `for ... { if err { return err } }` over external calls.

```bash
rg -n -A2 'for [^{]*\{[^}]*\.(Push|Build|Deploy|Apply|Get)' backend
```

When you find one with no retry / classifier wrapping, check whether the underlying call is plausibly transient (network, registry, k8s API). If yes, it's a finding.

### 4. No per-call timeout on external I/O

```bash
rg -n 'PushContext|client\.Solve|Pods\(\)\.Get|Jobs\(\)\.Create' backend \
  | rg -v '_test.go'
```

Each of those calls accepts a `context.Context`. If the caller passed an unbounded ctx, the call can hang forever. Cooker's pattern: per-stage timeout (T10) bounds each invocation. Anywhere else doing network I/O without a timeout is a finding.

### 5. Dev defaults reaching production

`backend/internal/config/config.go`'s `Validate()` is the gate. Anything fetched via `getEnv("X", "<default>")` that isn't checked in `Validate()` for `Env.IsProduction()` can leak into prod silently.

```bash
rg -n 'getEnv\("[A-Z_]+",\s*"' backend/internal/config
```

Pull each default and ask: "is this default safe in production?" Confirmed examples: `DATABASE_URL` was `cooker:cooker@localhost`, `AllowedOrigins` could be `["*"]`. Fixed by T19. Any new env var added without a Validate gate is a finding.

### 6. Stub handlers that silently succeed

`backend/internal/service/executor.go`'s `executeTest`, `executeApproval`, `executeCustom` originally just `slog.Info`'d and returned nil — meaning approval gates auto-passed. The audit category is "false confidence" because a green pipeline didn't mean what the user thought it meant.

```bash
rg -n -B1 -A5 'TODO:' backend/internal/service backend/internal/handler \
  | rg -B5 'return nil$'
```

If a handler's only body is a slog + return, ask whether it's documented as a stub. If not, that's a finding.

### 7. Missing optimistic concurrency on hot rows

T11 added `version` columns. The pattern that needs it: any row a user can edit while another goroutine holds a copy of the same row.

```bash
rg -n 'UPDATE [a-z_]+ SET' backend/internal/store/postgres
```

Each non-version-checked Update is a candidate. After T11, only `pipeline_runs` updates aren't version-checked — by design, because there's only one writer (the executor for that run).

### 8. Cleanup races: row deleted while a goroutine holds it

`backend/internal/handler/{pipeline,app,environment,host}.go` Delete handlers don't check whether anything's in flight. The chain `[B.5.1]` (pipeline deleted while run in flight) is a typical case.

When auditing a Delete handler, ask: "what's currently using this row?" — runs, deploys, promotions. If the answer's "nothing checked," it's a finding.

### 9. Shell-string interpolation with `fmt.Sprintf`

The Buildah injection (T1) was the highest-impact one. Pattern:

```bash
rg -n 'fmt\.Sprintf\([^)]*[^a-zA-Z_]%s[^a-zA-Z_]' backend
```

If the result is fed to `/bin/sh -c` or `exec.Command("sh", "-c", ...)`, that's a finding regardless of input source — operators sometimes paste arbitrary args via env or pipeline definitions.

### 10. Unbounded resource growth

```bash
rg -n 'io\.ReadAll\(c\.Request\.Body\)' backend
rg -n 'append\([^,]+, [^,]+\.\.\.' backend  # slice-of-slice append
rg -n 'make\(chan [^,]+, [0-9]+\)' backend  # check buffer sizes
```

Confirmed: `handler/app.go:242` was unbounded `io.ReadAll` on the GitHub webhook (T8 fixed). WebSocket per-client send buffer is 256 messages — mostly fine, but worth flagging if a build emits a burst.

## Where bugs hide in Cooker (heat-map)

| File / package | What I look for here |
|---|---|
| `backend/internal/server/server.go` | Boot-time resource leaks; lifecycle ordering of redis / wsHub / store; cleanup-stack on early return |
| `backend/internal/server/runs.go` | Goroutine join races; documented-but-missing timeouts; orphan-sweep edge cases |
| `backend/internal/server/websocket.go` | Channel close-twice; map mutation under RLock; missing read deadlines |
| `backend/internal/service/executor.go` | Stub stages; nil-deref on stageMap mismatch; per-stage timeout enforcement |
| `backend/internal/builder/buildah.go` | Shell-string composition; Args field with user input |
| `backend/internal/builder/kaniko.go` | Job lifecycle (poll vs delete vs orphan); resource Requests-without-Limits |
| `backend/internal/handler/*.go` | IDOR on path params; missing input validation; raw err.Error() in 5xx bodies |
| `backend/internal/store/postgres/*.go` | Missing version checks; `LIKE`/`ORDER BY` injection candidates; UPDATE-without-WHERE-version |
| `backend/internal/config/config.go` | Dev defaults reaching prod; Validate() coverage gaps |
| `backend/internal/auth/oidc.go` | Issuer / JWKS staleness; raw err.Error() to clients |
| `deploy/helm/cooker/templates/*.yaml` | `optional: true` on required Secrets; missing `required` guards |
| `deploy/kubernetes/rbac.yaml` | Cluster-wide ClusterRole when namespaced Role would do |

## Known false positives — don't re-flag these

Subagents got these wrong; I had to verify and correct. If you see a finding making one of these claims, treat it as a false positive **unless** the underlying code has changed since the audit.

| Claim | Why it's wrong |
|---|---|
| "Environment / webhook secrets are stored as plaintext" | They're AES-GCM-sealed via `crypto/codec.go:Seal` (verified at lines 57-66). Base64 just wraps the sealed bytes for JSONB round-tripping. The migration comment at `002_env_secrets.up.sql:1-4` confirms. |
| "GitHub webhook accepts pushes from any GitHub owner with the same repo name" | `app.go:267` matches by `ev.Repository.FullName` which is `owner/name` (globally unique on GitHub). The chain is conditional on `App.GitHubRepo` being misconfigured without an owner — see [A11-2]. |
| "`go wsHub.Run()` is launched before `registerRoutes()`" | Verified: `server.go:215` runs `s.registerRoutes()` first, line 216 launches `go wsHub.Run()`. The audit found prior was wrong. |
| "SIGTERM during `server.New()` panics on `srv.Close()` (srv is nil)" | Verified: `main.go:41-43` exits non-zero before reaching `Close()` if `New` fails. The path is unreachable. |
| "kubectl SHA verification format mismatch" | k8s.io still publishes `kubectl.sha256` as a bare hex hash (verified by webfetch). The Dockerfile's `echo "$(cat …)  /usr/local/bin/kubectl" \| sha256sum -c -` construction is correct. |

## The right tools, with token-cost notes

| Tool | Use when | Cost notes |
|---|---|---|
| **`audit-greps.sh`** (this skill) | Initial sweep over the codebase | Cheap; one Bash call returns the canonical greps formatted |
| **Explore subagent (one)** | Bounded question, e.g. "audit handler/app.go" | Moderate; ~3-5 K tokens for a focused report |
| **Explore subagent fan-out (10×)** | Comprehensive audit across multiple categories | Heavy; ~50 K tokens. Worth it for a once-per-quarter sweep, not for routine work |
| **Explore subagent fan-out (20×)** | Deep + chain-error analysis | Very heavy; ~120 K tokens. Reserve for a major refactor or pre-launch readiness |
| **`mcp__github__pull_request_read`** | Confirming a CI failure is still red, fetching review comments | Cheap |
| **WebFetch on GitHub Actions logs** | Don't try this | The pages are auth-walled. Use `get_check_runs` instead and ask the user to paste logs if needed. |

## Workflow

1. **Read the existing audit docs first.** Don't re-discover known findings.
2. **Run `.claude/skills/cooker-audit/audit-greps.sh`** — gets you the canonical pattern matches in one Bash call.
3. **Check `cooker-find/where-is.sh`** to navigate to the relevant area.
4. **For each candidate**:
   - Read the file (don't trust grep alone — context matters).
   - Check the false-positive list above.
   - Verify the claim against the actual code.
5. **Format the finding** with `file:line` citation, severity (Critical = exploitable RCE / data loss; High = correctness / availability; Medium = degraded behaviour; Low = hardening), and a one-line fix sketch.
6. **Cross-reference** to the existing audit doc IDs if it's a known item. If novel, name it (e.g. "Bug-2026-05-14-X") and add it to `chain-recheck.md` if it's a chain-shaped interaction.
7. **Don't write a finding without reading the cited file.** Subagents that summarise without reading often misattribute behaviour.

## Output format

Match the format used in `vulnerabilities-and-chains.md`:

```
| # | Severity | Finding | File |
|---|---|---|---|
| <n> | **Critical** | <one-line description> | `path:line` |
```

For chain-error findings, use the *Trigger / Sequence / Effect / Citation / Mitigation* format from `chain-recheck.md` Part B.

## Anti-patterns to refuse

- Don't run a 10/20-agent fan-out for a single-file investigation. One Explore is enough.
- Don't claim a finding without reading the cited line directly.
- Don't re-flag anything in the false-positive list without code-level evidence the underlying code has changed.
- Don't fabricate severity. If you're unsure, default to Medium and flag uncertainty.
- Don't write the fix at the same time as the finding — put fixes in `cooker-improve`. Mixing the two pollutes the audit-doc reading flow.
