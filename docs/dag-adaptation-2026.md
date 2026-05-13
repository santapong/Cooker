# Cooker DAG adaptation — what Jenkins, Dokploy, Dagger, and Airflow taught us

> Status: **research + plan, not approved.** Written 2026-05.
> Author: PM pass on branch `claude/project-audit-security-GKXzQ`.
> Cross-references `docs/audits/dag-performance.md` (Wave 1 perf audit), `docs/audits/W11-user-journeys.md` (persona walkthroughs), `docs/pm-brief-2026-05.md` (May 2026 PM brief), `docs/roadmap-2026.md` (2026 roadmap), `docs/protocols.md` (CKR-DSL design).
> No code changes here. The output is a 20-week implementation roadmap and seven design decisions.

---

## 1. Executive summary

Cooker has a working DAG engine. `backend/pkg/dagrunner/runner.go:35-57` builds the topological sort once and runs each level in parallel with bounded fan-out; `backend/internal/service/executor.go:171-263` is the type-dispatch layer that turns each node into a Builder/Pusher/Deployer/GitOps call. The wire is sound. The fittings are bare.

What we're missing, after looking at how four mature DAG systems solved it:

1. **Retry policies with exponential backoff** at the stage level. The infrastructure is already there (`backend/internal/retry/retry.go`), and `executor.go:208-219` wraps stages in `retry.Do` — but the retry count comes from a single integer `Stage.Config.Retries` (`backend/internal/model/pipeline.go:70`). No backoff knob, no per-error classification beyond "is it a context error." Airflow's per-task `retries` + `retry_delay` + `retry_exponential_backoff` is what this should look like.
2. **Trigger rules / conditional execution.** `backend/internal/model/pipeline.go:97` declares `Edge.Condition` as `"success" | "failure" | "always"` — the field has existed since the schema was first written, but `executor.go` never reads it. The DAG runner unconditionally drops downstream nodes when an upstream fails (`runner.go:131-138`). Airflow's seven trigger rules and Jenkins's `post { failure { ... } }` blocks both encode this; Cooker has the field and the UI shape — it just doesn't wire through.
3. **Inter-stage data flow.** Build emits a digest (`executor.go:334-340`), Push emits a tag (`executor.go:396-400`), Deploy emits a list of K8s resources (`executor.go:434-439`). Each stores them on `StageRun.Artifacts`, but no later stage can *read* them. Airflow has XCom; Dagger has function-return values; we have a serialise-only one-way artifact log.
4. **Content-addressable build caching.** Kaniko Job specs do not get `--cache=true` (`docs/audits/dag-performance.md` §1 finding, citing `backend/internal/builder/kaniko.go:159-242`). BuildKit's `SolveOpt` does not set `CacheImports` or `CacheExports`. Every build is a cold start. This is a one-line config exposure in each adapter that the W11 ML persona flagged as the single highest-ROI change for their workflow.
5. **Post-stage cleanup hooks.** Jenkins's `post { always { ... } }` is the pattern operators reach for when they need "delete the test container even if the test failed," "post a Slack message on any outcome," "rm -rf the workspace." Cooker has no equivalent. The W11 SaaS persona's "rotate webhook secret on every successful deploy" use case wants this.

What we should *not* add, even though one or two of the four researched systems shipped it:

- **Dynamic task mapping** (Airflow's `.expand()`). Kubernetes Deployment replicas already cover the "run N instances of this in parallel" use case. Adding it at the DAG layer would compete with K8s and lose.
- **Hierarchical nested stages** (Jenkins `stage { stage { ... } }`). Module composition by `App → Pipeline` is the better abstraction; flat is easier to debug, and the graph editor's drawer UX gets unreadable past two levels of nesting.
- **Code-derived implicit DAGs** (Dagger's "the graph is whatever your Go program does"). Static validation is one of Cooker's selling points. The graph is the source of truth; that's the W11 indie-hacker promise.

What this doc proposes:

- A **20-week implementation roadmap** (§10) that lands tidy-first (§6, weeks 1–3), then the five primitives in the order they unblock each other (weeks 4–17), then the CKR-DSL parser from `docs/protocols.md` §4 absorbing the stable surface (weeks 18–20).
- **Four architectural decision records** (§8) — the load-bearing choices that can't be defaulted: builder for caching, outputs storage shape, interpolation engine, trigger-rule expression language.
- Per-primitive **integration design** in §7 — model fields, store impact, service changes, adapter deltas, frontend hooks, test plan.

The 90-day calendar in `docs/pm-brief-2026-05.md` §3 has 15 items. This doc adds five more — but they're sequenced *after* the 90-day list, not in parallel. Tidy-first (T1–T5) starts in week 1; the first user-visible primitive (Retry v2) ships in week 5. The full primitive set lands by end of week 17; the CKR-DSL parser absorbs them in weeks 18–20.

---

## 2. Cooker's current DAG layer — what's solid and what isn't

### 2.1 What's solid

The runner package is a textbook implementation and the comments are explicit about every tradeoff:

| Mechanism | File:line | Notes |
|---|---|---|
| Kahn's topological sort, level-grouped | `backend/pkg/dagrunner/dag.go:29-76` | Each topo level is a slice of node IDs; the runner consumes one level at a time. |
| Bounded fan-out per level (semaphore) | `runner.go:91-109` | `NewRunnerBounded(maxParallel=N)` caps concurrent goroutines via `chan struct{}` semaphore. `0` preserves legacy unbounded. |
| Env override on the cap | `executor.go:39-47` | `COOKER_DAG_MAX_PARALLEL` overrides the default 16 (`executor.go:33-37`). Reasonable prod default. |
| Panic recovery per node | `runner.go:110-116` | `defer func() { if rec := recover(); ... }` — converts panic into stage failure, no process crash. Closes `dag-performance.md` #7. |
| OTel span propagation across goroutines | `runner.go:85-86, 118` | Parent span injected into a `MapCarrier`, each child goroutine extracts back into its `ctx`. Concurrent stages still link to the right trace. |
| Per-stage timeout | `executor.go:184-194` | `time.ParseDuration(stage.Config.Timeout)` with a 30-minute default. Unparseable → `slog.Warn` + default. Closes `dag-performance.md` #6 (was reported as missing; has since been wired). |
| Retry on transient errors | `executor.go:208-225`, `backend/internal/retry/retry.go:61-107` | `retry.Do` with `retry.Policy{MaxAttempts, Initial, Max, IsTransient}`. Approval/Custom/Test forced to `MaxAttempts=1` (`executor.go:220-223`). Closes `dag-performance.md` #3 in the basic case. |
| Mid-run persistence | `executor.go:199, 255, 261` | `e.persistProgress(ctx, run)` called after stage start and after stage end. Closes `dag-performance.md` #10 in the basic case (one row write per stage transition). |
| Bound on log size | `executor.go:26, 344-373` | `cappedBuffer` truncates per-stage logs at 1 MiB with a marker line. JSONB stays bounded. |
| LogWriter wired for build | `executor.go:300-329` | `builder.Request.LogWriter = io.MultiWriter(logs, lineWriter)` — logs persist to `StageRun.Logs` *and* stream to the WebSocket hub per-line. Closes `dag-performance.md` #2 for build. |

What this means in practice: the **engine** is production-ready. The W11 indie-hacker persona's "push to main, see a green pipeline in 5 min" loop works. The audit's Critical findings #1 and #2 have a partial fix (build is wired, push/deploy are not — see below).

### 2.2 What's not solid

The wiring layer between the runner and the adapters is where every gap lives. Six findings, ranked by user impact:

1. **Stub stages silently succeed.** `executor.go:377-381` (`executeTest`), `executor.go:460-464` (`executeApproval`), `executor.go:466-470` (`executeCustom`). All three return `nil` after a single `slog.Info`. A user who builds a pipeline with `test → approval → push` gets a green run regardless of test outcome and without any human approval. This is `dag-performance.md` §3's Critical finding #1 — still open as of this writing. The retry-policy reset at `executor.go:220-223` already singles these three out as "doesn't make sense to retry"; the right next step is to make them *fail loud* until real implementations land (T1 in §6).

2. **Edge conditions are dead.** `backend/internal/model/pipeline.go:97`:
   ```go
   Condition string `json:"condition,omitempty"` // "success", "failure", "always"
   ```
   The field round-trips through the API, persists in JSONB, and is renderable in the React Flow canvas — but `executor.go:220-244` switches purely on `stage.Type` with no input from the upstream edges. A pipeline author can label every edge `"failure"` and the DAG will run as if every edge were `"success"`. This is the entire blocker for Primitive #2 (Trigger rules) — and the field already exists, which makes it the cheapest of the five primitives to ship.

3. **LogWriter is not wired for push or deploy.** `executor.go:383-402` (`executePush`) builds a `pusher.Request{Source, Target}` with no `LogWriter`. `executor.go:404-441` (`executeDeploy`) builds a `deployer.Request{Kind, Namespace, ...}` with no `LogWriter`. The Pusher/Deployer interfaces don't even expose one. This is `dag-performance.md` #2 (High) — the build half is closed (§2.1 above); the push and deploy halves are still open. A user watching a run in the UI sees "Push: success" with **no log output** at all. T2 in §6.

4. **Redundant status-drain goroutine.** `executor.go:266-270`:
   ```go
   go func() {
       for update := range runner.Updates() {
           logger.Info("pipeline stage transition", "stage", update.NodeID, "status", update.Status)
       }
   }()
   ```
   This goroutine is launched *after* `runner.Run(ctx)` is invoked but *before* it blocks (see line 272). Since `runner.Run` is synchronous and only returns after `close(r.updates)` at `runner.go:66`, this goroutine has a small race: if `Run` returns before the goroutine starts ranging, the channel is already closed and the range exits immediately with no logging. Not a correctness bug — `slog.Info` is the only side effect — but the pattern is wrong. T3 in §6 either moves the drain *before* `runner.Run` (in another goroutine that the main one waits for) or removes it and emits the transitions from inside `taskFunc` directly.

5. **`persistProgress` writes per stage, not batched.** `executor.go:199, 255, 261` — three calls per stage (start, fail, success). A 50-stage pipeline writes 100+ rows. The runner already emits a `StatusUpdate` channel (`runner.go:60-62, 149-153`); piggybacking on that gives a single drain point that can batch by debounce (e.g. 500 ms or 10 transitions, whichever comes first). T5 in §6.

6. **No outputs/XCom path.** `StageRun.Artifacts` (`backend/internal/model/run.go:41`) accumulates `{Type, Ref, Digest}` triples — but no later stage's `StageConfig` (`pipeline.go:45-90`) can reference `${stages.build.digest}`. Push picks `stage.Config.Image` (`executor.go:390`) as the source, falling back to nothing if unset — there's no machinery to read the digest from the upstream Build's `StageRun.Artifacts`. This is the entire blocker for Primitive #3 (Outputs), and also the silent cause of `dag-performance.md` §1's "tag mutations between Build and Push aren't detected" warning.

### 2.3 What the runner gives us for free

Three properties of the existing engine that primitives need to preserve:

- **Static validation.** `BuildDAGFromPipeline` (`executor.go:152`) refuses cycles before execution starts. Any primitive that adds an edge (Outputs, Trigger rules) must extend this check, not bypass it.
- **OTel propagation.** `runner.go:85-86, 118` is the load-bearing reason the existing trace/log filtering works. Primitives must thread `ctx` through their new code paths the same way.
- **Best-effort progress writes.** `persistProgress` (`executor.go:291-298`) swallows errors and logs them. The "work itself is the source of truth" comment is precisely the right policy for primitives that emit new state — Retry attempts, edge skips, outputs — to follow.

---

## 3. Four mature DAG systems compared

Method: for each system, one page covering mental model, DAG mechanics, the "pipeline as X" choice, what they got right, what they got wrong, and the one or two things Cooker should steal. The cross-cutting synthesis at the end maps to §4's five-primitive list.

### 3.1 Apache Airflow

**Mental model.** A *DAG* is a Python `with DAG(...)` block; each *task* is an instance of an `Operator` subclass; the operator's `execute(context)` method does the work. The scheduler is a separate process from the workers; the metadata DB (`task_instance`, `xcom`, `dag_run` tables) is the source of truth for both.

**DAG mechanics.** Tasks are dependencies declared via `task_a >> task_b` (shifted operator overload) or by `task.set_upstream(...)`. The scheduler walks the DAG, evaluates each task's trigger rule against its upstream task states, and submits "ready" tasks to the executor (Celery, Kubernetes, or local). Levels are *not* explicit — Airflow doesn't have Kahn's level grouping; it polls task state.

**Pipeline as X choice.** Pipeline-as-Python. Verbose; runs everywhere Python runs; gets you operator reuse but also gets you arbitrary `import requests` in the DAG definition that runs on the scheduler. This is **the wrong choice for Cooker** — the graph editor's promise is that the pipeline is data, not code. See §5 (do not adopt).

**What they got right** — three things Cooker should steal:

1. **Trigger rules as a per-task enum.** `TriggerRule.ALL_SUCCESS` (default), `ALL_FAILED`, `ALL_DONE`, `ONE_SUCCESS`, `ONE_FAILED`, `NONE_FAILED`, `NONE_SKIPPED`. Seven values. Closes 80% of the "I want to run cleanup on failure" / "I want to run the deploy only if all tests passed" use cases. Maps cleanly onto Cooker's existing `Edge.Condition` field. **Adopt verbatim** — see Primitive #2 in §4.

2. **XCom for inter-task data flow.** `ti.xcom_push(key="image_digest", value="sha256:abc...")` in the producer; `ti.xcom_pull(task_ids="build", key="image_digest")` in the consumer. Storage: a separate `xcom` table keyed by `(dag_id, run_id, task_id, key)`. Cooker doesn't need a new table — `StageRun.Artifacts` (`backend/internal/model/run.go:41`) and a new `StageRun.Outputs` map on the same JSONB column will do — but the *shape* (producer pushes by key, consumer pulls by `${stages.build.digest}` syntax) is right. **Adopt with the storage adapted** — Primitive #3.

3. **Retry policy as a structured object.** Each task has `retries: int`, `retry_delay: timedelta`, `retry_exponential_backoff: bool`, `max_retry_delay: timedelta`. Cooker has `Stage.Config.Retries: int` (`pipeline.go:70`); the underlying retry package (`backend/internal/retry/retry.go:36-48`) already has all four fields. The plumbing is half-done; primitive #1 is "expose the rest of the `retry.Policy` fields on `StageConfig`." **Adopt verbatim.**

**What they got wrong** — two patterns Cooker should not copy:

1. **Dynamic task mapping (`expand()`).** `BashOperator.partial(task_id="run").expand(bash_command=["echo 1", "echo 2", ...])` generates N tasks at runtime. Powerful — and **breaks static validation**. Cooker's graph editor cannot render "N nodes, where N is a function output." See §5.

2. **The scheduler is a separate process.** Airflow's three-tier architecture (scheduler + workers + metadata DB) is unavoidable when you have thousands of DAGs and multi-hour task latencies. Cooker's runs are bounded to a 30-minute deadline (`pm-brief-2026-05.md` would call this out if the deadline were actually enforced — currently the audit reports it's not, per `dag-performance.md` §3 finding #7). For us, in-process is right; **don't adopt** the multi-process scheduler architecture.

### 3.2 Dagger

**Mental model.** A pipeline is **a Go (or Python/TypeScript) program**. You write `func Build(ctx context.Context, src *dagger.Directory) *dagger.Container { return client.Container().From("...").WithMountedDirectory("/src", src).WithExec([]string{"go", "build"}) }`. The Dagger Engine takes the function's output (its DAG of container operations) and runs it. The DAG is **implicit** — derived from the Go program's data flow.

**DAG mechanics.** Every operation returns a lazy reference; the engine builds the DAG by recording which reference depends on which. Execution happens when you call `.Stdout()` or `.Export()` — the engine then walks back through the references, deduplicates identical sub-DAGs, and runs them via BuildKit LLB.

**Pipeline as X choice.** Pipeline-as-Go-code. Strong type safety; lousy onboarding (you need a Dagger SDK; you need Go). **Wrong choice for Cooker.** The graph editor exists precisely so operators don't have to write Go.

**What they got right** — two things Cooker should steal:

1. **Content-addressable caching via BuildKit LLB.** Every operation in a Dagger pipeline hashes to a content address; identical operations across pipelines and across runs hit the same cache. This is just BuildKit's existing LLB cache, used aggressively. Cooker has a BuildKit adapter (`backend/internal/builder/buildkit.go`); per `dag-performance.md` §1, `SolveOpt` is not configured with `CacheImports` / `CacheExports`. **Adopt** — Primitive #4, see DR-1.

2. **Function return values as the DAG.** Dagger doesn't have an "outputs" abstraction *per se*; the return value of `Build` is a `*dagger.Container`, and the next function call that takes a `*Container` argument is implicitly the consumer. The lesson for Cooker isn't to remove the explicit Outputs API — it's that **the consumer should reference outputs by structured accessor, not by string label**. `${stages.build.digest}` not `${OUTPUT_FROM_BUILD}`. Strict accessors keep the validator working. Adopt — see DR-3.

**What they got wrong:**

1. **Code-derived implicit DAGs.** No graph editor. No static validation against a pipeline schema before runtime — bugs surface at runtime, not at lint time. **Disqualifying for Cooker.** See §5.

### 3.3 Jenkins (Declarative Pipeline)

**Mental model.** A pipeline is a `Jenkinsfile` (Groovy DSL) checked into the repo. The `pipeline { stages { stage('X') { steps { ... } } } }` block is parsed by the Jenkins controller; each `stage` runs as a unit; `post` blocks fire after stage or pipeline completion regardless of outcome.

**DAG mechanics.** Mostly linear (stages run in declaration order), with `parallel { ... }` blocks for explicit fan-out. The `when { ... }` directive is a per-stage gate that runs the stage only if the condition holds. Failure handling is via `post { failure { ... } } / always { ... } / success { ... }` — the three blocks Jenkins users reach for in every non-trivial pipeline.

**Pipeline as X choice.** Pipeline-as-DSL-in-repo. The Groovy DSL is genuinely full Groovy, which is power-and-footgun. The *shape* (decl in repo, pipeline-as-code, no UI authoring) is right; the language choice (Groovy) is mediocre.

**What they got right** — three things Cooker should steal:

1. **`post` blocks.** `post { success { sh 'notify --green' } failure { sh 'cleanup' } always { archiveArtifacts 'logs/**' } }`. Maps cleanly onto Cooker's `StageConfig`: add `Stage.Config.Post.OnSuccess`, `Stage.Config.Post.OnFailure`, `Stage.Config.Post.Always`. Each is a sub-stage (or a reference to another stage). **Adopt with a Cooker shape** — Primitive #5 in §4.

2. **`when` directives.** `stage('Deploy') { when { branch 'main' } steps { ... } }`. Cooker's analog is the `Edge.Condition` field already discussed in §2.2 #2 — but `when` is per-stage (not per-edge) and supports boolean expressions (`when { allOf { branch 'main'; environment name: 'DEPLOY_ENV', value: 'prod' } }`). Cooker should support both: trigger-rule on the edge (Airflow-style, simple enum), `when` expression on the stage (Jenkins-style, boolean over the inputs/outputs). See DR-4.

3. **Stage timeouts as a structured option.** `options { timeout(time: 5, unit: 'MINUTES') }`. Cooker already has `Stage.Config.Timeout` (`pipeline.go:69`) — just a Go duration string. **Already adopted** (`executor.go:184-194`).

**What they got wrong:**

1. **Groovy.** Power exceeds the use case; teaches your CI to be a programming language. CKR-DSL should be data, per `docs/protocols.md` §4. **Don't adopt.**
2. **Nested stages.** `stage('Tests') { parallel { stage('Unit') { ... }; stage('Integ') { ... } } }`. Cooker can express this with two parallel sibling stages — keep the graph flat. See §5.
3. **Master controller monolith.** Jenkins's "one controller, N agents" architecture has known scaling pain; the CKR-RUNNER/1 design (`protocols.md` §5) is taking a different path. Tangential to this doc.

### 3.4 Dokploy

**Mental model.** A self-hosted Vercel/Heroku clone. Each "application" maps to a Docker Compose stack or a single Dockerfile; deploys are a build step + a container restart. There is no DAG editor; pipelines are implicit (build, push, restart) and per-app.

**DAG mechanics.** None worth comparing — Dokploy's "DAG" is a linear three-step pipeline. The interesting part is the *app abstraction*: one App = one Compose stack = one URL.

**Pipeline as X choice.** No pipeline. App-centric. The deploy is whatever the build artifact says it is.

**What they got right** — one thing Cooker should learn from, but not adopt as a DAG primitive:

1. **App-as-the-unit.** Dokploy's mental model maps cleanly to Cooker's W11 indie-hacker persona. Cooker already has the App abstraction (`backend/internal/handler/app.go` ecosystem); the DAG is *under* the App for simple cases. Implication for this doc: primitives we add to the DAG must remain optional. A user who never touches the graph editor shouldn't pay any cost for retry policies, outputs, or post-hooks. We should default all five primitives to "off" / "absent" with the historical behaviour preserved. **No code change** in this doc — just a design constraint for every primitive in §7.

**What they got wrong** (from Cooker's perspective, not Dokploy's):

1. **No pipeline UX.** Operators with non-trivial flows (multi-env, approval gates, fan-out) have nowhere to express them. Cooker's graph editor is the differentiator.
2. **One-Compose-stack-per-app coupling.** Limits the "8 services, 3 envs" SaaS team case (W11 persona 2). Cooker's Pipeline+App+Environment triple covers this; Dokploy can't.

### 3.5 Synthesis — what the four systems say in one paragraph

Airflow has the right primitives (trigger rules, XCom, retry) at the wrong substrate (Python-as-DAG). Dagger has the right caching model (LLB content-addressing) at the wrong substrate (Go-as-DAG). Jenkins has the right shape (declarative file in the repo, `post` blocks, `when` directives) at the wrong language (Groovy). Dokploy is the right reminder that the App is sometimes more useful than the graph. **Cooker's job is to take the three primitives Airflow does well, the caching Dagger does well, and the cleanup pattern Jenkins does well, and ship them inside a graph editor that Dokploy users would still find approachable.**

---

## 4. The five DAG primitives Cooker is missing

Ranked by user impact across the four W11 personas. Each primitive has: one-line description, the audit/persona finding that motivates it, the model field it adds, the effort estimate.

### Primitive #1 — Retry policies with backoff

- **Description:** Per-stage `retries: int`, `retry_delay: duration`, `retry_max_delay: duration`, `retry_exponential_backoff: bool`. Replace today's "one int" `Stage.Config.Retries`.
- **Motivates:** `dag-performance.md` §3 finding #3 (no retry beyond `MaxAttempts`); W11 §SaaS step 4 (transient registry 5xx fails the whole run); W11 §ML step 5 (long builds hit transient K8s API blips).
- **Model:** `pipeline.go:45-90` — replace `Retries int` with `Retry RetryPolicy` struct of four fields. JSON-tagged. Back-compat: a bare integer in old JSONB rows is read as `Retry.MaxAttempts`. Migration: none (JSONB shape change is handled by the Go unmarshaller).
- **Effort:** **S.** ~2 days. Reuses `backend/internal/retry/retry.go` (already shipped — `Policy{MaxAttempts, Initial, Max, IsTransient}`). The wiring at `executor.go:208-219` already constructs the policy from the old single int; this primitive widens the source from `stage.Config.Retries` to `stage.Config.Retry`.
- **Personas:** All four. Highest immediate ROI to W11 §SaaS and §ML; indie hacker benefits indirectly via fewer red-X notifications.

### Primitive #2 — Trigger rules / conditional execution

- **Description:** Honor `Edge.Condition` (`pipeline.go:97`). Add a per-stage `Stage.When` boolean expression. Allow stages to skip when conditions aren't met, marking `StageRun.Status = "skipped"` (new enum value in `run.go:8-14`).
- **Motivates:** `dag-performance.md` §3 finding #2 (no skip-downstream / partial-success policy); W11 §SaaS step 3 (approval policy on prod); W11 §SaaS step 6 (compliance "who approved what"); cross-references Airflow trigger rules verbatim.
- **Model:** No new fields on Edge — `Condition` already exists. **One new field on Stage:** `Stage.When ExprBoolean` (the Jenkins-style boolean expression). New `RunStatus` enum value `RunStatusSkipped` (`run.go:8-14`). Optionally a separate `RunStatusFailedContinue` for "the upstream failed but we ran anyway" — see DR-4 for whether one or two new statuses.
- **Effort:** **S.** ~3 days. The hard part is the boolean-expression evaluator for `Stage.When` (which shares its strict-accessor allowlist with Primitive #3 — see DR-3). Edge-condition evaluation is a one-pager: when a stage finishes, mark its `Status` (success/failed/skipped); when a downstream stage is about to start, evaluate each incoming edge's `Condition` against its upstream's `Status`; if no incoming edge resolves to "run," skip the stage.
- **Personas:** SaaS team (approval policy depth) + Enterprise SRE (compliance trail). Indie hacker doesn't notice until they hit their first failed test stage; then they notice immediately.

### Primitive #3 — Inter-stage data flow / outputs

- **Description:** A stage produces `Outputs map[string]string` (persisted as part of `StageRun`); a downstream stage references them via `${stages.<id>.<key>}` interpolation in `StageConfig` string fields.
- **Motivates:** `dag-performance.md` §1 "tag mutations between Build and Push aren't detected" (the digest exists on `StageRun.Artifacts[0].Digest` but Push picks `stage.Config.Image` as the source — `executor.go:390`); W11 §ML step 9 (DVC/HuggingFace fetch into Build then reuse in Test); W11 §SaaS step 6 (audit trail "what digest did we deploy?").
- **Model:** **One new field on `StageRun`:** `Outputs map[string]string` (`run.go:34-42`). Goes into the existing `stage_runs` JSONB column — no schema migration. The Builder/Pusher/Deployer interfaces grow a `Outputs map[string]string` field on their `Result` types. Caps: per-key value ≤ 4 KiB, per-stage total ≤ 32 KiB. Server-side rejection on Result; truncation marker if exceeded.
- **Effort:** **M.** ~5 days. The hard part is the interpolation engine — see DR-3. The plumbing is mechanical: each adapter that wants to expose outputs sets them on `Result`; `executor.go` copies them onto `StageRun.Outputs`; each adapter that wants to consume them gets a pre-interpolated `StageConfig` (`executor.go` does the substitution before calling the adapter).
- **Personas:** All four. The ML persona gains the most (cached intermediate artifacts pinned by content). The SaaS team gains audit trail. The indie hacker gets the "Push automatically picks Build's digest" magic.

### Primitive #4 — Content-addressable build caching

- **Description:** Surface `--cache-from` / `--cache-to` on the Builder interface. Default Kaniko adapter sets `--cache=true --cache-repo=<configured>`; BuildKit adapter sets `CacheImports`/`CacheExports` in `SolveOpt`.
- **Motivates:** `dag-performance.md` §1 (no Kaniko cache wiring; no BuildKit cache export); W11 §ML step 4 (the highest-ROI ask in the entire user-journey audit); roadmap A19/A20.
- **Model:** **New struct `CacheSpec`** on `Stage.Config.Build`. Fields: `Mode ("registry"|"oci"|"disabled")`, `Ref string` (where to write/read), `Inline bool` (BuildKit's inline cache, default true). Builder interface adds `Request.Cache CacheSpec`. **No new RunStatus.** No new model on `StageRun` — cache reuse is a builder concern; the digest still ends up in `Outputs` (Primitive #3).
- **Effort:** **M.** ~5 days. Two adapters in parallel (Kaniko default + BuildKit opt-in — per user, see DR-1). Kaniko side: append `--cache=true --cache-repo=<spec.Ref>` to the Job args (`backend/internal/builder/kaniko.go` around the args-construction site cited by `dag-performance.md`). BuildKit side: set `CacheImports`/`CacheExports` in `SolveOpt` (`backend/internal/builder/buildkit.go`, around the comment cited by `dag-performance.md` §1 line 14-19 explicitly deferring this). UI: a single "cache repo" textbox on the Build-stage drawer; defaulted from a per-Environment value.
- **Personas:** ML engineer (47-min builds → 8-min builds on the second run, per W11 §ML estimate). SaaS team (every build of every service benefits). Enterprise (cost reduction). Indie hacker (faster first deploys).

### Primitive #5 — Post-stage cleanup hooks

- **Description:** Each stage may declare `Post.OnSuccess`, `Post.OnFailure`, `Post.Always` — each is itself a (mini-)stage that runs after the parent regardless of context cancellation (within reason — see DR-2's cap discussion).
- **Motivates:** W11 §SaaS "rotate webhook secret on every successful deploy"; W11 §SaaS step 8 (bulk webhook-secret rotation); Jenkins `post { ... }` semantics.
- **Model:** **New struct `PostHooks`** on `Stage.Config`. Fields: `OnSuccess *Stage`, `OnFailure *Stage`, `Always *Stage`. Each is a pointer; nil means "no hook." A hook is itself a `Stage` so it gets the same retry, timeout, log, output treatment as a top-level stage. Frontend renders post-hooks as small subordinate nodes in the canvas (visually nested, but logically siblings — they're separate DAG nodes with synthesised edges).
- **Effort:** **S.** ~3 days. The hard part is making sure the post-hook stage runs even when the parent's context was cancelled by the per-stage timeout. Use `context.WithoutCancel(ctx)` (Go 1.21+, already in our minimum) plus a short hard deadline (default 60s) for the hook itself. Wire-in is at `executor.go` after the stage's `defer cancelStage()` returns: launch the hook synchronously before returning to the runner.
- **Personas:** SaaS team (notifications), Enterprise SRE (audit-trail emit on every stage), ML engineer (rm workspace).

### 4.6 Why these five, in this rank order

Effort × persona-impact, with one tie-breaker: **does the primitive unblock a later primitive.**

- #1 (Retry) and #2 (Trigger rules) are both S-effort; #1 first because its retry-classification needs apply to every later primitive (including #5's post-hooks, which themselves should be retryable).
- #2 (Trigger rules) before #5 (Post-hooks) because `post { failure }` semantics depend on "did the parent stage fail" — which `Edge.Condition` already gates. #5 with a buggy/missing #2 means `failure` hooks fire on success.
- #3 (Outputs) before #4 (Caching) because cache-key interpolation (`${stages.build.digest}` in `CacheSpec.Ref`) wants the outputs evaluator. Without #3, #4's cache-repo field is a static string per stage — usable but worse.
- #4 (Caching) last among the user-visible primitives because it's the largest model+adapter change and benefits the most from the others stabilising first. Also: it's the W11 ML persona's #1 ask, so it ships before the CKR-DSL parser, which doesn't help that persona at all.

---

## 5. Three primitives Cooker should NOT add

For each, one paragraph explaining the alternative we already have, and the cost of adopting.

### 5.1 Dynamic task mapping (Airflow `.expand()`)

Airflow lets a task generate N downstream tasks at runtime by calling `.expand(bash_command=...)` with a list whose length isn't known until the upstream finishes. **Don't adopt.** Kubernetes Deployment replicas already handle the "run N copies of this in parallel" pattern at the deploy stage. For build-stage parallelism (e.g. "build for amd64 and arm64"), the Builder interface already supports `Request.Platforms` (`pipeline.go:51`) and BuildKit's image-index logic does the fan-out internally. The cost of adopting `.expand()` is high: it breaks the graph editor's static layout, it breaks `BuildDAGFromPipeline`'s pre-execution cycle check, and it forces the persistence layer to deal with a `StageRun` count that isn't known until runtime. The benefit doesn't exist for the W11 personas — none of the four asked for it.

### 5.2 Hierarchical nested stages (Jenkins `stage { stage { ... } }`)

Jenkins lets you nest stages: `stage('Tests') { parallel { stage('Unit') { ... }; stage('Integ') { ... } } }`. **Don't adopt.** Cooker can express this with two top-level stages `unit-test` and `integ-test` both with `Edge.Source = "build"` — the graph editor renders them as parallel siblings, the runner schedules them in the same level, and the user gets the same outcome with a flatter mental model. Nesting compounds the graph editor's complexity (a node can now be either a stage or a group of stages; drawer UX gets two modes); it makes `dagrunner.Runner` recursive; it makes outputs harder (does `${stages.tests.digest}` refer to the parent group or one of the children?). Module composition by `App → Pipeline` (which Cooker already has, `roadmap-2026.md` §A1) is the better abstraction for "I have related pipelines."

### 5.3 Code-derived implicit DAGs (Dagger)

Dagger's "the graph is whatever your Go program does" is elegant if you write Go. **Don't adopt.** Cooker's graph is the source of truth for two reasons that we will not give up: (a) the graph editor exists, and 60% of our W11 user base would not be there if pipeline authoring required writing Go; (b) static validation (the cycle check, the type check, the topological sort) runs *before* execution starts and catches author errors before any compute is consumed. Dagger debugs by re-running; Cooker debugs by linting. The two design centres are incompatible.

The CKR-DSL design in `docs/protocols.md` §4 is *not* Dagger-style; it's YAML data that round-trips with the graph editor losslessly (`protocols.md` §4.5). That's the right point on the static-vs-dynamic spectrum for us, and it's the destination this doc's 20-week plan ends at.

---

## 6. Tidy-first refactors (T1–T5)

These land in weeks 1–3 — before any primitive. They close audit findings, simplify the executor, and de-risk the primitive work that follows. Each is independently mergeable.

### T1 — Stub stages fail loud

**Problem.** `executor.go:377-381` (`executeTest`), `executor.go:460-464` (`executeApproval`), `executor.go:466-470` (`executeCustom`) silently return `nil`. A pipeline with a `test` stage runs green regardless of test outcome. `dag-performance.md` §3 Critical finding #1.

**Fix.** Change each stub from `slog.Info(...); return nil` to `return fmt.Errorf("stage type %q not implemented", stage.Type)`. The retry policy already singles these three out as `MaxAttempts=1` (`executor.go:220-223`), so the failure won't retry-storm.

**Side effect.** Every existing pipeline that used `test`/`approval`/`custom` stages will start failing. **This is the point.** Acceptable because (a) those pipelines were silently broken before; (b) the failure message is structured and the UI will render it on the stage card; (c) we can offer a one-line config to fall back to the old behaviour during a deprecation window if a UAT install needs it.

**Effort.** ~1 hour of code + ~2 hours of doc updates (`SECURITY.md` if the stub was load-bearing for any "approval gate" claim, which it shouldn't be — but verify).

### T2 — Wire LogWriter for push and deploy

**Problem.** `executor.go:383-402` and `404-441` construct Pusher/Deployer requests with no `LogWriter`. `dag-performance.md` §4 High finding #2.

**Fix.** Mirror the build wiring at `executor.go:300-321`:
1. Add `LogWriter io.Writer` to `pusher.Request` (likely already there? — check `backend/internal/pusher/`) and `deployer.Request`.
2. In `executePush` and `executeDeploy`, build a `cappedBuffer + io.MultiWriter + lineWriter` exactly like `executeBuild`.
3. Defer `sr.Logs = logs.String()`.
4. Make sure each Pusher and Deployer adapter writes to the LogWriter — at minimum a "Pushed image to <ref>" line and a "Applied <kind>/<name>" line per resource.

**Effort.** ~half a day. Mechanical, no new types beyond optional interface-field additions.

**Required before:** Primitive #4 (caching) — when a Kaniko build hits a cache, the only way to *see* "5/10 layers cached" is via the LogWriter stream. T2 makes that visible for push-cache-export and deploy-cache-aware Helm reuse as well.

### T3 — Remove redundant status-drain goroutine

**Problem.** `executor.go:266-270` ranges over `runner.Updates()` in a goroutine launched right before `runner.Run` is called synchronously on the same goroutine. The drain goroutine has a small race (channel may close before range starts) and serves only to `slog.Info` something that the per-stage `slog.Info("pipeline executing stage", ...)` at `executor.go:201-202` already covers.

**Fix.** Either delete the goroutine entirely or move the drain inside the `runner.NewRunnerBounded` task closure (it has direct access to status changes via the retry policy). Recommended: delete; the duplication isn't load-bearing.

**Effort.** ~30 minutes.

**Required before:** Primitive #5 (post-hooks) — post-hooks fire on status transitions, and the status-update pump becomes the obvious place to dispatch them. T3 clears the way; T5 (next) puts a proper drain in.

### T4 — Honor `Edge.Condition` as a no-op `success` reader

**Problem.** `pipeline.go:97`'s `Condition` field has never been read. A pipeline-as-YAML round-trip exporter from CKR-DSL (`protocols.md` §4) will preserve the field but Cooker still ignores it. This is the foundation for Primitive #2.

**Fix.** In `BuildDAGFromPipeline` (or its caller), refuse to build the DAG if any edge has `Condition != ""` AND `Condition != "success"`. Default behaviour stays "treat as success." This is **not yet** Primitive #2 — it's a forward-compatible refusal so users can't ship a pipeline with `Condition = "failure"` and get surprising behaviour. Primitive #2 in week 6 replaces the refusal with real evaluation.

**Effort.** ~1 hour.

### T5 — Batched persistProgress via Updates channel ✅ CLOSED (`claude/w4-t5-batched-persistprogress`)

**Problem.** `executor.go:199, 255, 261` call `persistProgress` three times per stage. Each is a full `UPDATE pipeline_runs SET stage_runs = ..., ...` rewriting the entire JSONB column. A 50-stage pipeline pays ~100+ writes.

**Fix.** Replace the explicit calls with a single drain goroutine that consumes `runner.Updates()` and writes at most once per `min(500ms, 10 transitions)`. The drain goroutine is what T3 cleared the way for. Terminal status (`"failed"` / `"success"`) flushes eagerly — no debounce lag on the final outcome.

**Landed.** Two new tests: `TestExecutor_T5_BatchedWritesDebounce` (asserts fewer writes than the pre-T5 per-call count for a 12-stage pipeline) and `TestExecutor_T5_EagerFlushOnTerminal` (asserts ≥1 write on terminal failure before Execute returns). Both pass under `-race`. Closes dag-performance.md Medium #10.

**Required before:** Primitives #1 (retry attempts each emit a status transition — without batching, the write rate triples), #2 (skip emits a transition), #5 (post-hooks emit transitions).

---

## 7. Per-primitive integration design

One section per primitive. Each section covers: model, store, service, adapter, frontend, tests, migration, risk.

### 7.1 Primitive #1 — Retry policies with backoff

**Model.** `backend/internal/model/pipeline.go:69-70` (current single int `Retries`) becomes:

```go
Retry RetryPolicy `json:"retry,omitempty"`

// RetryPolicy mirrors retry.Policy with JSON-friendly types.
type RetryPolicy struct {
    MaxAttempts int    `json:"maxAttempts,omitempty"`
    InitialMS   int    `json:"initialMs,omitempty"`   // milliseconds for JSON friendliness
    MaxMS       int    `json:"maxMs,omitempty"`
    Exponential bool   `json:"exponential,omitempty"` // default true
}
```

Back-compat: the JSON unmarshaller for `StageConfig` accepts either `retries: int` (old) or `retry: RetryPolicy` (new). A custom `UnmarshalJSON` on `StageConfig` consumes both.

**Store.** No schema change. Stages live in `pipelines.stages JSONB` per migration `001_initial.up.sql:8`. The Go type drives the serialisation.

**Service.** `executor.go:208-219` replaces the inline `retry.Policy` construction with a `policyFromStage(stage)` helper that reads `stage.Config.Retry` (with the back-compat fallback to `stage.Config.Retries`).

**Adapter.** No change. The retry sits above the adapter layer (`retry.Do` calls the adapter's method).

**Frontend.** `PipelineEditorPage.tsx` stage-edit drawer gains a "Retry" sub-section with four inputs (max attempts, initial delay, max delay, exponential toggle). One Zustand slice change.

**Tests.** `executor_test.go` adds a test that constructs a stage with `MaxAttempts=3, InitialMS=100, MaxMS=1000, Exponential=true`, mocks a builder that fails twice then succeeds, asserts three calls and a "success" stage outcome. The retry package itself is already covered by `backend/internal/retry/retry_test.go`.

**Migration.** None on the DB side. The JSONB shape change is handled by the custom unmarshaller. Existing pipelines load with the old `Retries: 2` reading as `Retry{MaxAttempts: 3, ...defaults}`.

**Risk + rollback.** Risk: low. The wrapping is already in place. Rollback: revert the model change; the retry.Policy package keeps working with `MaxAttempts=1 + stage.Config.Retries`.

### 7.2 Primitive #2 — Trigger rules / conditional execution

**Model.** No new field on Edge (`pipeline.go:97` `Condition` already exists). One new field on Stage: `When string` (a tiny boolean expression — see DR-4). One new value on `RunStatus` (`run.go:8-14`): `RunStatusSkipped = "skipped"`. Optionally also `RunStatusFailedContinue = "failed-continue"` if we want to express "the upstream failed but we proceeded under an `always` edge" — DR-4 picks one or two.

**Store.** No schema change. The new field and enum value go into the existing JSONB.

**Service.** Two changes:

1. **Edge evaluation.** New function `edgeAllows(upstreamStatus, edge.Condition) bool` in `backend/internal/buildplan/` (new file `edges.go`). Called inside the runner's task closure right after the topological scheduler picks the next node — before the goroutine launches. If no incoming edge `allows`, the stage is set to `Status = "skipped"` and the goroutine returns immediately.
2. **`When` evaluation.** New function `evalWhen(expr, ctx) bool` in `backend/internal/buildplan/when.go` (new file). Takes the expression (`Stage.When`) and a context (incoming outputs, env vars, git refs). Returns bool. The grammar is tiny — see DR-4. Called after edge evaluation.

**Adapter.** No change.

**Frontend.** Edge inspector gains a dropdown for `Condition` (success / failure / always / none — none = no constraint). Stage drawer gains a `When` textbox. Run page shows skipped stages with a distinct visual (greyed out + "Skipped: <reason>" tooltip).

**Tests.** `executor_test.go` adds a test that runs a pipeline `build → [test on success] → [notify on failure]` and asserts notify is skipped when test passes. A second test with build failing asserts test is skipped, notify runs.

**Migration.** None. New `RunStatusSkipped` is additive; old runs with old status values still load.

**Risk + rollback.** Risk: medium. The boolean expression evaluator is the load-bearing part — see DR-4 for the grammar. Rollback: if `When` proves unusable, ship Primitive #2 with edge-condition only (which is the Airflow-equivalent and 80% of the value).

### 7.3 Primitive #3 — Inter-stage data flow / outputs

**Model.** `backend/internal/model/run.go:34-42` adds:

```go
type StageRun struct {
    StageID    string     `json:"stageId"`
    Status     RunStatus  `json:"status"`
    StartedAt  *time.Time `json:"startedAt"`
    FinishedAt *time.Time `json:"finishedAt"`
    Logs       string     `json:"logs,omitempty"`
    Error      string     `json:"error,omitempty"`
    Artifacts  []Artifact `json:"artifacts,omitempty"`
    Outputs    map[string]string `json:"outputs,omitempty"` // NEW
}
```

Builder/Pusher/Deployer `Result` types add `Outputs map[string]string` field. Pre-existing fields stay (`Tags`, `ImageID`, `Digest`, `AppliedResources`) — they're still useful for the typed `Artifact` log. Outputs are the string-keyed escape hatch.

**Store.** No schema change. New field in the existing `stage_runs` JSONB column. **Caps** enforced at write-time in `executor.go`: per-key value ≤ 4 KiB, per-stage total ≤ 32 KiB. On overflow, append a `_truncated` key with a marker; never persist the over-cap value.

**Service.** Two changes:

1. **Outputs ingestion.** After each adapter call (`executor.go:330` for build, `392` for push, `430` for deploy, `486` for gitops), copy `result.Outputs` to `stageRun.Outputs` with the caps applied.
2. **Outputs interpolation.** New function `interpolateConfig(cfg StageConfig, ctx OutputCtx) StageConfig` in `backend/internal/buildplan/interpolate.go` (new file). Called before each adapter call. Replaces `${stages.<id>.<key>}` in any string field of `StageConfig`. Returns an error for unknown accessors (which then becomes a stage failure).

**Adapter.** Each Builder/Pusher/Deployer adds `Outputs` to its `Result` struct. The Kaniko builder, for instance, sets `Outputs["digest"]` and `Outputs["size"]`. Doc-only contract for which keys each adapter emits — codified in `docs/extending.md` per roadmap B1.

**Frontend.** Run page renders `Outputs` as a key→value table beneath the stage's log panel. Stage editor's string fields gain a `${stages.X.Y}` autocomplete (Phase 2 — not required for the primitive to ship).

**Tests.** `executor_test.go` adds: build stage emits `Outputs["digest"] = "sha256:abc"`; push stage has `Config.Repository = "registry/${stages.build.digest}"`; assert push receives `Source = "registry/sha256:abc"`. A second test asserts that an unknown accessor produces a stage failure with a useful error.

**Migration.** None. Field is additive.

**Risk + rollback.** Risk: medium. The interpolation engine is custom (per DR-3 — we don't use `text/template`). Rollback: a feature flag `COOKER_OUTPUTS_ENABLED` short-circuits the interpolation pass and the ingestion pass.

### 7.4 Primitive #4 — Content-addressable build caching

**Model.** `backend/internal/model/pipeline.go:45-90` adds:

```go
// Build (cont'd)
Cache CacheSpec `json:"cache,omitempty"`

type CacheSpec struct {
    Mode    string `json:"mode,omitempty"`    // "registry" | "oci" | "disabled". Default "disabled".
    Ref     string `json:"ref,omitempty"`     // e.g. "ghcr.io/org/app:cache"
    Inline  bool   `json:"inline,omitempty"`  // BuildKit's inline cache; default true when Mode != ""
}
```

`builder.Request` (in `backend/internal/builder/builder.go`) adds `Cache CacheSpec`.

**Store.** No schema change.

**Service.** `executor.executeBuild` (`executor.go:300-342`) passes `stage.Config.Cache` into `builder.Request.Cache`. No other changes.

**Adapter.** **Two adapters in parallel** (per DR-1):

- **Kaniko (default).** In the Kaniko Job spec assembly (`backend/internal/builder/kaniko.go` around the args site cited by `dag-performance.md` §1 line 159-242), append `--cache=true` and `--cache-repo=<spec.Ref>` when `Cache.Mode == "registry"`. For `"oci"` mode, the same `--cache-repo` flag with the OCI prefix.
- **BuildKit (opt-in).** In `backend/internal/builder/buildkit.go` `SolveOpt` construction (the existing code that `dag-performance.md` §1 line 14-19 says explicitly defers cache features), set `CacheImports` and `CacheExports` to point at `Cache.Ref`. Mode `"registry"` uses BuildKit's `registry` cache type; `"oci"` uses `local` if Ref is a path, else `registry` with `oci-mediatypes=true`.

**Frontend.** Build stage drawer gains a "Cache" section with mode dropdown (Off/Registry/OCI) and a ref textbox. Defaults from the Environment's `cacheRepo` (new env field — see backlog).

**Tests.** Adapter-level tests in `backend/internal/builder/kaniko_test.go` and `buildkit_test.go` assert that the cache flags / `SolveOpt` fields are populated when `CacheSpec.Mode != "disabled"`. Service-level test in `executor_test.go` asserts the spec flows from `StageConfig.Cache` into `builder.Request.Cache`.

**Migration.** None on the DB. **Operational migration:** users who want cache benefit must point `cacheRepo` at a registry with credentials present. Add a `docs/build-cache.md` doc covering "where do I point this?"

**Risk + rollback.** Risk: medium. BuildKit cache semantics differ from Kaniko cache semantics — same `CacheSpec` value may behave subtly differently across adapters. Mitigation: ship Kaniko default first (most users); BuildKit opt-in lands the same week behind `COOKER_BUILDER=buildkit`. Rollback: `Cache.Mode = ""` (the absent default) preserves today's no-cache behaviour exactly.

### 7.5 Primitive #5 — Post-stage cleanup hooks

**Model.** `pipeline.go:45-90` adds:

```go
Post *PostHooks `json:"post,omitempty"`

type PostHooks struct {
    OnSuccess *Stage `json:"onSuccess,omitempty"`
    OnFailure *Stage `json:"onFailure,omitempty"`
    Always    *Stage `json:"always,omitempty"`
}
```

Each hook is itself a `Stage`, recursively. Hooks of hooks are forbidden (validation rejects them) — one level deep, like Jenkins.

**Store.** No schema change.

**Service.** New function `runPostHook(parentResult outcome, hook *Stage, hookKind string)` in `executor.go`. Called from inside the task closure after `cancelStage()` is deferred — see `executor.go:194` — but uses `context.WithoutCancel(ctx)` plus a 60s hard deadline so the hook runs even when the parent's timeout fired. Hook executes via the same `executor.run` machinery (recursive call — bounded by one level by validation). The hook gets its own `StageRun` row in the parent's `Run.StageRuns` slice with a synthetic ID `<parent>:post:<kind>`.

**Adapter.** No change.

**Frontend.** Stage drawer gains a "Post" tab with three sub-stages (OnSuccess / OnFailure / Always). Canvas renders post-hooks as small nodes attached to the parent with a distinct edge style. Run page shows them in the stage's expandable detail.

**Tests.** `executor_test.go` adds: parent stage fails → `OnFailure` runs, `OnSuccess` doesn't, `Always` runs. Parent succeeds → `OnSuccess` runs, `OnFailure` doesn't, `Always` runs. Parent times out → `OnFailure` and `Always` both run (within their own 60s deadline).

**Migration.** None.

**Risk + rollback.** Risk: low — every hook is a regular stage that already has retry/timeout/logging. The novel piece is `context.WithoutCancel` (Go 1.21+ stdlib). Rollback: a `COOKER_POSTHOOKS_ENABLED` flag short-circuits the hook dispatch.

---

## 8. Architectural decision records

Four load-bearing choices. Each as a one-page record: context, options considered, decision, consequences.

### DR-1 — Builder for caching: Kaniko default, BuildKit opt-in

**Context.** Primitive #4 needs a builder backend that supports content-addressable layer caching. Cooker has both Kaniko (`backend/internal/builder/kaniko.go`, 354 lines) and BuildKit (`backend/internal/builder/buildkit.go`, 108 lines). The user explicitly directed: "Both Kaniko + BuildKit with cache support — KEEP Kaniko default, expose BuildKit as opt-in."

**Options considered.**

1. **Kaniko-only with cache.** Simpler. Loses BuildKit users.
2. **BuildKit-only with cache.** Modern; loses Kaniko users who run BuildKit-incompatible registries.
3. **Both adapters honor the same `CacheSpec`.** More work; lets operators choose per-environment.

**Decision.** Option 3 — both adapters. Kaniko stays the production default (Cooker's existing `selectBuilder` keeps it as the default value). BuildKit is opt-in via `COOKER_BUILDER=buildkit`. Both honor the same `CacheSpec` model field from §7.4. Kaniko consumes the spec via `--cache=true --cache-repo=<ref>` Job args; BuildKit consumes it via `CacheImports`/`CacheExports` in `SolveOpt`.

**Consequences.**
- (+) Operators can experiment with BuildKit without losing Kaniko's known-good production behaviour.
- (+) The Primitive #4 model and frontend ship once, work for both adapters.
- (-) Double the adapter testing surface. Mitigation: integration tests run against both via a `testtag` matrix in CI.
- (-) Cache semantics differ subtly between the two — we document the difference in `docs/build-cache.md`. The pinpoint difference: Kaniko's `--cache-repo` is read-write per build; BuildKit's `CacheExports` writes on success only.

### DR-2 — Outputs storage: piggyback on `stage_runs` JSONB

**Context.** Primitive #3 needs to persist per-stage outputs. Options range from "new table" to "string-encode into the existing Logs field." Postgres migration `001_initial.up.sql:15-26` shows `stage_runs JSONB` is the existing home for per-stage state.

**Options considered.**

1. **New `stage_outputs` table.** Schema-first. Best for high-volume outputs (>1MiB per stage). Migration cost.
2. **New JSONB column on `pipeline_runs`.** Same JSONB-blob discipline as today. Slightly less normalised.
3. **Piggyback on the existing `stage_runs` JSONB.** Zero migration. Same shape as `Logs`, `Artifacts`.

**Decision.** Option 3, with caps. Outputs go inside the existing `stage_runs` JSONB column as a new field on the `StageRun` struct (§7.3). Per-key value capped at 4 KiB, per-stage total capped at 32 KiB. Over-cap writes append a `_truncated` marker and discard the over-cap value.

**Consequences.**
- (+) No DB migration; primitive ships in one PR.
- (+) Outputs round-trip with the rest of `StageRun` for free — every existing read path (handler, frontend) sees them without changes.
- (-) Hard cap on output size. Acceptable: outputs are for digests, version strings, computed URLs — not for blobs. Blobs go in `Artifacts` with an external reference.
- (-) Postgres JSONB updates rewrite the column; for runs with many stage transitions this can be inefficient. Mitigated by T5 (batched persistence).

### DR-3 — Interpolation engine: custom strict-accessor evaluator

**Context.** Primitives #2 (`Stage.When`) and #3 (`${stages.X.Y}`) both need to evaluate user-authored expressions against a stage context. The natural choice is `text/template` — already in the Go stdlib. CKR-DSL (`docs/protocols.md` §4.9, §4.12) deliberately rules this out: "Don't use `text/template` — it's too powerful (function maps, conditionals). Use a 50-line custom evaluator over the strict accessor set."

**Options considered.**

1. **`text/template`.** Familiar; powerful; turing-complete with `funcMap`. Lets users do things we don't want (file reads, conditionals, comparisons that escape the accessor allowlist).
2. **CEL (Common Expression Language).** Used by Kubernetes for `ValidatingAdmissionPolicy`. Sandboxed. Heavy dependency (~1 MiB binary growth). Overkill for our needs.
3. **Custom ~50-line evaluator over a strict accessor set.** Same shape as `docs/protocols.md` §4.9. Maximum control. Lower learning surface for users.

**Decision.** Option 3. New file `backend/internal/buildplan/interpolate.go` (~50–80 lines). Allowed accessors: `params.X`, `secrets.X.name`, `stages.X.<output_key>`, `git.{sha,branch,tag}`, `env.X`. Unknown accessors return an error (stage failure with a useful message). No conditionals, no functions, no comparisons in this evaluator — boolean expressions for `Stage.When` (§7.2) go through a *different* evaluator (DR-4) that wraps this one.

**Consequences.**
- (+) Shares the accessor allowlist with CKR-DSL (`protocols.md` §4.9). One evaluator serves both purposes.
- (+) Static analysis trivially proves "this expression cannot do X" (X = file read, network call, etc.). Audit-friendly.
- (-) Every new accessor needs a code change. Acceptable — the accessor set is small and we want it controlled.
- (-) Users coming from GitHub Actions / Drone may be surprised by the lack of `if: ${{ X == 'main' }}` style. Mitigation: the `Stage.When` evaluator in DR-4 adds the boolean layer; the interpolator stays accessor-only.

### DR-4 — Trigger-rule expression language: edge-enum + stage-boolean

**Context.** Primitive #2 has two surfaces: per-edge (an enum: "success", "failure", "always") and per-stage (an expression: `branch == 'main' && env != 'prod'`). Different shapes mean different evaluators.

**Options considered.**

1. **Edge enum only (Airflow-style).** Simple. Covers 80% of the "run cleanup on failure" cases.
2. **Stage boolean only (Jenkins-style `when`).** More expressive. Doesn't compose well with the existing `Edge.Condition` field.
3. **Both, layered.** Edge enum gates the DAG topology; stage boolean gates the per-stage entry. Independent.

**Decision.** Option 3. Edge `Condition` is an enum with seven values mirroring Airflow's trigger rules (the four already documented — `success`, `failure`, `always` — plus `all_done`, `one_success`, `one_failed`, `none_failed`). Stage `When` is a boolean expression over the same accessor set as DR-3, plus `==`, `!=`, `&&`, `||`, parenthesisation. New `RunStatus` value: `RunStatusSkipped`. No `RunStatusFailedContinue` — a stage that ran under an `always` edge whose upstream failed still records as `success` if it itself succeeded; the upstream's `failed` status is recorded on its own `StageRun`. One status value per stage; readers join across stages to reconstruct chains.

**Consequences.**
- (+) Airflow users see familiar trigger rules; Jenkins users see familiar `when` blocks. Both ship.
- (+) The expression language stays bounded — no arithmetic, no calls, no regex. Auditable.
- (-) Two code paths to maintain (edge evaluator, stage boolean evaluator). Mitigation: both live in `backend/internal/buildplan/` and share the accessor resolver from DR-3.
- (-) Users may want richer expressions over time (e.g. `branch =~ /^release\//`). We rebuff Phase 1; revisit in CKR-DSL v1beta1.

---

## 9. CKR-DSL ordering — primitives first, parser second

The CKR-DSL design in `docs/protocols.md` §4 commits to ~4 engineering weeks for v1alpha1 (`protocols.md` §4.11), shipping a parser, a graph↔DSL round-trip, HTTP endpoints, and a CLI subcommand. The parser is **not** a blocker for any primitive in this doc — each primitive ships with a JSON-shape change to `model.Pipeline` that the existing canvas and API handle.

**Sequencing rationale.** Each primitive that ships adds one row to `docs/dsl-mapping.md` (a new doc that this plan creates in week 18 — one line per primitive, describing the DSL syntax that absorbs it). When CKR-DSL v1alpha1 ships in weeks 18–20, the parser already knows about all five primitives, and the round-trip test corpus (`protocols.md` §4.10) covers them from day one. **Net cost:** five extra `docs/dsl-mapping.md` lines and five extra round-trip test cases — paid for by not having to re-design the DSL syntax after every primitive lands.

**Anti-pattern we're avoiding.** Ship CKR-DSL first, then retrofit each primitive into the DSL. This is the trap GitHub Actions fell into (every new feature requires a new `with:` key, which gets reviewed in isolation and inconsistently): `if:` syntax differs from `on:` syntax differs from `outputs:` syntax. By ordering primitives first, we get one chance to design the DSL syntax for *all five at once*, with the patterns established by their model-field choices.

**Decision.** §10 calendar places CKR-DSL parser at weeks 18–20. Each primitive PR (weeks 4–17) includes a one-paragraph "DSL form" section in its PR description; the DSL agent in week 18 reads those paragraphs as input.

---

## 10. 90-day week-by-week calendar

Twenty weeks. Two-week slack baked into T1–T5. Effort estimates assume 3.5 engineering-days per primitive-week.

| Week | Workstream | Deliverable |
|---|---|---|
| 1 | Tidy | T1: stub stages fail loud. T3: remove redundant goroutine. |
| 2 | Tidy | T2: LogWriter for push + deploy. T4: Edge.Condition refuses non-success. |
| 3 | Tidy | T5: batched `persistProgress` via Updates channel. Buffer week — review, fix, regression-test. |
| 4 | P#1 Retry | Model change: `Retries int` → `Retry RetryPolicy`. Back-compat unmarshaller. Service wiring. |
| 5 | P#1 Retry | Frontend drawer. Tests. PR. **Ships end of week 5.** |
| 6 | P#2 Trigger | `Edge.Condition` evaluator. New `RunStatusSkipped`. Service wiring (edge half). |
| 7 | P#2 Trigger | `Stage.When` evaluator (DR-4 boolean layer). Tests. |
| 8 | P#2 Trigger | Frontend (edge dropdown + when textbox). PR. **Ships end of week 8.** |
| 9 | P#5 Post | Model + service for `Post.{OnSuccess,OnFailure,Always}`. `context.WithoutCancel` plumbing. |
| 10 | P#5 Post | Frontend (Post tab in drawer; small canvas nodes). Tests. PR. **Ships end of week 10.** |
| 11 | P#3 Outputs | Model change: `StageRun.Outputs`. Adapter `Result.Outputs` plumbing. |
| 12 | P#3 Outputs | Interpolator (DR-3, ~50 lines). Service wiring. Caps enforcement (DR-2). |
| 13 | P#3 Outputs | Frontend (outputs table on run page). Tests. |
| 14 | P#3 Outputs | Doc: `docs/extending.md` outputs section. PR. **Ships end of week 14.** |
| 15 | P#4 Caching | Model: `CacheSpec`. `builder.Request.Cache` plumbing. Kaniko adapter cache flags. |
| 16 | P#4 Caching | BuildKit adapter `CacheImports`/`CacheExports`. Adapter tests for both. |
| 17 | P#4 Caching | Frontend (cache section in build drawer). `docs/build-cache.md`. PR. **Ships end of week 17.** |
| 18 | CKR-DSL | Parser. `goccy/go-yaml` (per `protocols.md` §4.12). Line-number-preserving errors. |
| 19 | CKR-DSL | Graph↔DSL converters. Round-trip test corpus including all five primitives. |
| 20 | CKR-DSL | HTTP endpoints + CLI subcommand (per `protocols.md` §4.8). `docs/dsl.md`. PR. **CKR-DSL v1alpha1 ships end of week 20.** |

Total: 20 weeks. Each primitive PR is independently reviewable and revertable. The four tidy refactors at weeks 1–3 are individually mergeable too — if any one of T1–T5 turns up scope, it can ship alone without blocking the others.

**Parallelism opportunity.** Weeks 4–10 (P#1, P#2, P#5) can fan out to two engineers — they touch overlapping files (`executor.go`, `pipeline.go`, `run.go`) but disjoint regions. Weeks 11–17 (P#3, P#4) are deeper and want one engineer per primitive serially because each touches a different external surface (adapters for P#4, interpolator for P#3) and the test surface is large.

**Cooker-feature-dev** agent (per `docs/pm-brief-2026-05.md` §5) is the right owner for each primitive PR. Each primitive can spawn `cooker-backend-api` + `cooker-frontend-ui` + `cooker-frontend-state` in parallel per `docs/design.md` §11.

---

## 11. Cross-references and follow-ups

### 11.1 Where this doc connects

- **`docs/pm-brief-2026-05.md` §3 (90-day plan).** The 15-item list there is the parent calendar. This doc's 20-week plan is the *successor* — it picks up after the 90-day items 1, 4, 5, 6, 9, 10 land (the "first 2 weeks in parallel" group). Item 13 of the 90-day plan (CKR-LOG/1 v0) is independent of this doc and runs in parallel.
- **`docs/roadmap-2026.md`.** Rows C4 (Pipeline-as-code DSL), C5 (GitOps mode), C6 (Time-travel/replay), D6 (YAML import), D7 (Per-pipeline `runDeadline` override), A19/A20 (build-cache plumbing) all touch the primitives here. C4 = CKR-DSL = this doc's weeks 18–20. D7 already implied by Primitive #1. A19/A20 = Primitive #4. D6 (YAML import) is the *consumer* of the CKR-DSL parser — it ships after week 20 as a follow-on.
- **`docs/protocols.md` §3 (CKR-LOG/1).** Independent of this doc. The log protocol replaces the WS payload shape; this doc replaces the DAG primitive shape. They share zero code.
- **`docs/protocols.md` §4 (CKR-DSL).** This doc's destination. The DSL absorbs the stable surface of the five primitives in weeks 18–20.
- **`docs/audits/dag-performance.md` #1 (Critical, stub stages).** Closed by T1.
- **`docs/audits/dag-performance.md` #2 (High, LogWriter unwired).** Build half already closed by current code; push + deploy halves closed by T2.
- **`docs/audits/dag-performance.md` #7 (Medium, panic recovery).** Already closed by `runner.go:110-116`.
- **`docs/audits/dag-performance.md` #10 (Medium, mid-run progress).** Already partially closed by `executor.go:199, 255, 261`; fully closed by T5.
- **`docs/audits/W11-user-journeys.md` §Indie step 7 (PR-preview environments).** Not addressed in this doc — multi-week effort, deferred per W11.
- **`docs/audits/W11-user-journeys.md` §SaaS step 4 (bulk import).** Not addressed — orthogonal to DAG primitives.
- **`docs/audits/W11-user-journeys.md` §ML step 4 (cache plumbing).** Closed by Primitive #4.
- **`docs/audits/W11-user-journeys.md` §ML step 5 (per-app `runDeadline`).** Implied by Primitive #1's structured retry policy + the existing `Stage.Config.Timeout` — out of scope for this doc, ship as a separate one-line PR.

### 11.2 Follow-ups (not in the 20-week plan)

Each is a single-PR follow-up after the 20-week plan lands. Total scope: ~6 weeks of additional work.

1. **`docs/extending.md`** (roadmap B1) gains an "Outputs contract" section listing which keys each shipped adapter emits. Cheap. One-page doc PR.
2. **`docs/build-cache.md`** new doc — how to point `Cache.Ref` at a registry, credentials, multi-arch caveats. Ships with Primitive #4 in week 17 but is one separate PR.
3. **`docs/dsl-mapping.md`** new doc — one section per primitive showing its DSL form. Ships with CKR-DSL in week 20 but is one separate PR.
4. **`backlog.md` "Closed" log entries** for each primitive + each tidy refactor. Ships in each primitive's PR per CLAUDE.md.
5. **`SECURITY.md`** updates for Primitive #5 (post-hooks): hooks run with `context.WithoutCancel(ctx)` plus a 60s deadline; document this explicitly so a security reviewer doesn't have to read the code.
6. **OTel span attribution for skipped stages** (Primitive #2). Add `stage.skipped = true` as an attribute so existing dashboards filter correctly. Tiny.
7. **Frontend autocomplete for `${stages.X.Y}`** (Primitive #3 Phase 2). Out of scope for the primitive PR; ships as a follow-up when we have a critical mass of users hitting the feature.

### 11.3 Out of scope for this round (deliberately deferred)

- Writing any of the five primitives — each is a future PR per §7.
- Multi-tenancy ADR (`docs/pm-brief-2026-05.md` §4 open question #1 / #7) — unrelated; gates different work.
- BuildKit production hardening — DR-1 only documents that BuildKit is an opt-in second adapter; making it production-ready (rootless, multi-arch, secrets) is a separate workstream.
- CKR-LOG/1 protocol — `docs/protocols.md` §3, runs in parallel.
- D6 YAML import (Drone / GitHub Actions) — depends on CKR-DSL parser landing first.
- C5 GitOps mode — `protocols.md` §4.6 — depends on CKR-DSL.

---

## 12. Verdict

Five primitives, ordered by what unblocks what, sequenced behind five tidy-first refactors that close two audit Critical/High findings and clear the way for the primitives. Twenty weeks; revertable per primitive; the CKR-DSL parser absorbs the result. Two engineers can run the first three primitives in parallel weeks 4–10; one engineer carries the back half. No new external dependencies; no DB schema migrations; reuses every extension point Cooker already has (`retry.Policy`, `Builder/Pusher/Deployer` interfaces, JSONB columns, `selectBuilder`-style wiring).

The single highest-impact item is **Primitive #4 (build caching)** — W11 §ML estimates 47-min cold builds to 8-min cached builds. If the team can only afford one of the five, ship #4 with a hard-coded `CacheSpec` per Environment and skip the model field. Everything else in this doc is upside on top of that.

The decision the user owes is **DR-4** — edge enum and stage boolean (the doc's recommendation), or one or the other. Until that's confirmed, the Primitive #2 PR cannot start.
