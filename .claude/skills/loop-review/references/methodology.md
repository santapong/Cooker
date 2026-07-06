# Review Methodology

The three-phase method every code-review finder follows. Phase 1 builds the map, Phase 2 finds the suspects, Phase 3 proves which suspects are exploitable. The phases are a funnel: context scopes the search space, deviation analysis nominates candidates cheaply, and data-flow assessment is the expensive step you spend only on candidates that survived Phase 2.

A finding leaves this pipeline **only** if it is both exploitable and high-confidence (see [The reporting bar](#the-reporting-bar)). Everything else is dropped, not downgraded to a "nit" — the router (`../SKILL.md`) is for correctness and security defects, not style.

## Phase 0 — Scope: diff vs full repo

Decide the review surface before spawning any finder. The surface determines what each agent reads and what it is allowed to flag.

- **Diff review (default).** The user is reviewing a change (PR, branch, staged hunks). The **changed lines are the only place a finding may be anchored** — a bug you notice in untouched code is out of scope unless the diff makes it newly reachable. Fetch the diff with `git diff <base>...HEAD` (three-dot: changes on the branch, not churn from the base moving). Each changed hunk becomes a finder's seed; its neighborhood (below) is context, not review surface.
- **Full-repo review.** The user asks to audit a module, a package, or "the whole thing." No base to diff against; the review surface is every file matching the scope. Enumerate the file list up front and pipeline over it (this is known work — do not loop; see `../../loop-engine/references/loop-policy.md` L6). Anchor findings anywhere in scope.

**Smell test for diff scope:** if a finder wants to report a bug and cannot point to a changed line that introduces or exposes it, it is reviewing the wrong surface. Either the finding is out of scope, or the review should have been a full-repo audit and wasn't.

Mixed case — a diff that is large enough to be a de-facto rewrite (new module, wholesale replacement): treat the new files as full-repo surface and the edited files as diff surface. Do not make one finder straddle both.

## Phase 1 — Repo / Context

Build the mental model the later phases index against. One scout pass, cheap effort, before any vulnerability hunting. Capture:

- **Languages and frameworks** — what runtime, what web/ORM/serialization libraries. This decides which sink shapes matter (a Python f-string in a `cursor.execute` is a sink; the same f-string in a log line is not).
- **Entry points** — where untrusted input crosses into the code: HTTP route handlers, message-queue consumers, CLI arg parsers, deserializers, webhook receivers, file uploads. These are the origins of every Phase-3 trace.
- **Trust boundaries** — which values arrive from outside the trust zone (network, user, third-party API, another tenant) versus values the code itself produced. A finding requires taint that crosses a boundary; without a boundary there is no vulnerability, only a bug.
- **Auth model** — how the code decides who may do what: middleware, decorators, per-handler checks, row-level tenancy. You need this to judge reachability ("is this route authenticated?") and to spot missing checks.
- **Data stores and external effects** — databases, caches, shells, template engines, HTTP clients, file systems. These are the sink inventory. Note the *safe* API for each (parameterized query, `execFile` over `exec`, autoescaping template) — Phase 2 measures deviation against exactly these.

Output of Phase 1 is a short context note the finders share, not prose for a human. Keep it to the facts a finder needs to classify a source, a sink, and a boundary. If the repo is large, scope the context pass to the subtree the diff touches plus its shared libs — you do not need the whole monorepo's map to review one service.

## Phase 2 — Comparative deviation analysis

Vulnerabilities cluster where changed code stops resembling the code around it. This phase is cheap pattern-matching that nominates candidates for Phase 3; it does not confirm anything on its own.

For each changed hunk, compare against two baselines:

1. **Surrounding convention** — how the *rest of this codebase* does the same operation. If every other query in the repo uses the parameterized helper and this hunk hand-builds a string, that is a deviation. If every handler is wrapped in the auth decorator and this new one isn't, that is a deviation. The codebase's own norm is the strongest baseline because it encodes decisions the reviewer may not know.
2. **Secure baseline** — the framework's documented safe API for the operation (parameterized queries, autoescaping output, constant-time comparison, `path.resolve` + prefix check for file access, CSRF tokens on state-changing routes). Deviation from the secure baseline matters even when the whole codebase deviates the same way — a repo-wide antipattern is a repo-wide finding, reported once against the newly-changed instance.

**What counts as a deviation worth escalating:** a changed line that reaches a Phase-1 sink via an API that is not the safe one, OR drops a check (auth, validation, escaping, bounds) that comparable code retains. Rename it, reformat it, refactor it — none of that is a deviation. Behavior change at a sink or a boundary is.

Feed each nominated deviation to Phase 3. A deviation with no path from an untrusted source to a real sink dies here.

## Phase 3 — Data-flow vulnerability assessment

The confirmation step. Take a Phase-2 candidate and trace the value: does untrusted data reach a dangerous sink without an adequate sanitizer in between? This is the only phase that can promote a candidate to a reported finding.

Trace three elements:

- **SOURCE** — an untrusted value entering at a Phase-1 entry point: `req.query`/`req.body`/`req.params`/headers/cookies, message payloads, CLI args, env only if attacker-influenced, file contents from an untrusted upload, another tenant's row.
- **SINK** — a place where that value causes an effect if malicious: SQL/NoSQL execution, shell/`exec`, HTML/template output (XSS), file path resolution (traversal), redirect target (open redirect), deserializer, reflection/`eval`, an outbound URL (SSRF), a response that leaks another user's data (authz).
- **SANITIZER** — the control that neutralizes the taint *for that sink*: parameterization, allowlist validation, contextual escaping, canonicalization + prefix check, type coercion that discards the dangerous shape. A sanitizer counts only if it is on **every** path from source to sink and is correct **for that specific sink** — HTML-escaping does nothing for SQL; escaping for SQL does nothing for a shell.

The finding exists only if the path is **unbroken** (source actually flows to sink; no intervening reassignment to a constant) and **reachable** (the entry point is live, and if it is behind auth, note that the attacker must be an authenticated user — that lowers, not eliminates, severity). Break either and there is no finding.

### Worked example: source → sink trace

```js
// routes/reports.js
app.get('/reports', (req, res) => {          // entry point, unauthenticated
  const rows = buildReport(req.query.sort)   // SOURCE: req.query.sort
  res.json(rows)
})

// db/reports.js
function buildReport(sortColumn) {
  return db.query(                           // SINK: raw SQL execution
    `SELECT * FROM reports ORDER BY ${sortColumn}`  // taint interpolated, no sanitizer
  )
}
```

Trace:

- **Source:** `req.query.sort` — fully attacker-controlled, crosses the network trust boundary at an unauthenticated route.
- **Path:** passed as-is into `buildReport`, interpolated directly into the query string. No reassignment, no validation on any path. Unbroken.
- **Sink:** `db.query` with a template-literal-built statement. `ORDER BY` **cannot** be parameterized, so the only correct control is an allowlist of column names.
- **Sanitizer:** none. → **SQL injection, high confidence.** Reachable without auth; taint is unbroken; the safe baseline (allowlist) is absent. Report it.

Now the same code with one line added:

```js
function buildReport(sortColumn) {
  const col = ALLOWED_SORTS.has(sortColumn) ? sortColumn : 'created_at'  // SANITIZER
  return db.query(`SELECT * FROM reports ORDER BY ${col}`)
}
```

The allowlist sits on every path and is correct for this sink (it constrains the value to a known-safe set before it reaches SQL). The taint is broken. **No finding** — do not report it as "still looks risky." A correct sanitizer for the sink ends the trace.

## Sizing the neighborhood (token budget)

Each finder gets the changed hunk plus a bounded neighborhood of surrounding code. Too little and it can't see the source or the sanitizer; too much and you burn budget re-reading the repo per finder. Default sizing:

- **1 hop by default.** Give the finder the changed function in full, its **direct callers** (so it can see where arguments come from — the source) and its **direct callees that receive the tainted value** (so it can see the sink). One level up, one level down.
- **Signatures, not bodies, for provenance-only neighbors.** If you only need to know *what a caller passes*, feed its signature and the call site, not its whole body. Full bodies are for the function under review and any callee that is itself a candidate sink.
- **Pull in the definitions the trace names** — the type/schema of the tainted value, the sink's own wrapper (is `db.query` the raw driver or a safe helper?), and the sanitizer's implementation if one is claimed. These are what let Phase 3 decide "sanitizer correct for this sink" without guessing.
- **Escalate to 2 hops only to reach a boundary.** If the trust boundary (the actual entry point) is not visible within 1 hop, walk up one more level — but stop as soon as you can classify the source as trusted or untrusted. Do not expand the neighborhood to "understand the feature"; expand it to *close the trace*.
- **Cap and log.** Set a per-finder token ceiling (a function plus ~1 hop is typically a few KB, not tens). If a hunk's neighborhood blows the cap, split the hunk across finders rather than truncating silently — silent truncation makes a finder miss the sanitizer and report a false positive. Log what was dropped (see `../../loop-engine/references/harness-policy.md` H6).

**Smell test:** a finder that needed the whole file to find the source was under-scoped in Phase 1 (the entry point should already be in the shared context note) or the neighborhood was sized by file, not by data flow. Size by the trace, not by the file.

## The reporting bar

A finding is reported only when both hold:

- **Exploitable** — there is a concrete path from an untrusted source to a real sink with no adequate sanitizer, and the entry point is reachable. "Theoretically unsafe pattern" with no live source is not exploitable. State the exploit as inputs → effect (e.g. `?sort=id);DROP...` → arbitrary SQL), not as a category label.
- **High-confidence** — the trace is unbroken and you have read every intervening step, not assumed it. If you had to guess whether a sanitizer exists on some path, the confidence is not high — resolve the guess (pull the neighbor in) or drop the finding.

Default to dropping. A review that reports three real, exploitable defects is worth more than one that reports thirty maybes — the maybes train the reader to ignore the tool. When in doubt between reporting and dropping, drop, and let the verification pass (`../../loop-engine/references/harness-policy.md` H4) be the place uncertain findings die, not the reader's inbox.
