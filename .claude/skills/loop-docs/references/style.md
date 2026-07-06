# Technical Writing Style

The prose rules behind the `loop-docs` skill: how to render verified code behavior into documentation a specific reader can act on. This is the drafting reference — open it once you know the doc's type (`doc-types.md`) and before writing a substantial doc. Everything here is subordinate to accuracy: §5 of the SKILL is the non-negotiable core, and none of these style moves excuses an unverified claim. When a workflow drafts many docs at once, these rules govern the *draft* stage of the extract → draft → verify pipeline (`loop-engine` skill, harness policy H1 pipeline-default).

## 1. Write for the reader's task, not your knowledge

**Documentation is a tool the reader picks up to do one thing — shape every sentence around that thing, not around what you know.** The author knows the whole system; the reader knows one task and arrived mid-problem. Order the doc by what they need next, not by how the code is structured internally. Cut the architecture tour a task-doer will skip; cut the "how it works" a looker-up doesn't want. Ask of every paragraph: *does the named reader (§2 of the SKILL) need this to get unstuck?* If not, it belongs in a different doc type or nowhere. Jargon the reader hasn't met yet is a task-blocker — see §5.

## 2. Accuracy over completeness

**A correct small doc beats a comprehensive wrong one — when you must choose, ship less and true.** Coverage tempts you into claims you can't verify ("it also supports X", "you can probably Y"); each unchecked claim is a future support ticket and a crack in the reader's trust. Document the paths you have confirmed against the source, exhaustively and correctly, and mark the rest as an explicit gap rather than a confident guess. "This covers the common case; edge behavior for Z is untested" is a *stronger* doc than one that silently over-promises. This is the prose corollary of the SKILL's anti-aspirational rule: breadth never buys its way past verification (§5 of the SKILL).

## 3. Lead with a real, runnable example

**Show a working example before you explain it — readers copy first and read second.** Put the common-case example high, above the parameter tables and the theory, because that snippet is what most readers came for. Make it *real*: a command or call that runs as written, with realistic values, not `foo`/`bar` placeholders that hide the actual shape of an argument. Prefer an example you have actually executed or lifted from a passing test — a snippet that errors on paste is worse than no snippet, and hand-invented examples rot silently. Then build outward from it: the minimal case, then the parameters and variations. One concrete, correct example teaches more than three paragraphs of description.

## 4. Active voice, present tense, second person

**Instructions read as commands to the reader: "call `save()`", not "the data may be persisted".** Three defaults, applied uniformly:

- **Active voice** — name who acts. "The scheduler retries the job" beats "the job is retried"; passive hides the actor the reader needs to know about.
- **Present tense** — describe behavior as a standing fact. "`parse()` returns a `Token`", not "will return".
- **Second person, imperative** — address the reader directly in how-tos and tutorials. "Run the migration, then restart the server."

Reserve first person plural ("we recommend") for genuine authorial judgment in explanation docs; keep it out of reference and steps.

## 5. Scannable structure, terms defined on first use

**Readers scan before they read — build the page so the skimmer and the reader both succeed.** Short sentences over long ones; one idea per paragraph. Use headings that name the task, bulleted and numbered lists for sequences and options, and tables for anything with parallel structure (params, flags, config keys, return values). Front-load each section with its point so a skim of the headings and first lines conveys the shape.

Define every term the first time it appears — spell out an acronym on first use (`SSR` → "server-side rendering (SSR)"), and gloss any domain word the named reader might not hold. A term the reader hasn't met is exactly the block §1 warns about. Once defined, use it consistently: one name per concept, never two words for the same thing.

## 6. Inverted pyramid: most important thing first

**Put the answer at the top; let detail deepen as the reader descends.** Borrowed from journalism: the reader who stops after the first paragraph should still have the essential fact. A README opens with what the thing is and the fastest path to running it, not the contribution guidelines. An API entry opens with what the call does and its signature, then the parameters, then the edge cases and caveats. A how-to opens with the goal and the result, then the steps. Never bury the load-bearing sentence under setup, history, or hedging — the reader who needs it most is the one least willing to dig for it.

## 7. Write against doc rot

**A hand-copied fact is a future lie — point at the source of truth instead of duplicating it.** Docs rot because prose and code drift apart and nothing flags the break. Minimize the surface that can drift:

- **Co-locate docs with code.** Put the contract where the reader (and the next editor) will see it against the implementation — docstrings at the call site, module docs beside the module, endpoint docs next to the handler. Distance between a claim and the code it describes is how long the claim lives after the code changes.
- **Prefer generated or verified snippets.** Pull signatures, default values, CLI `--help` output, and version numbers from the source (or a doc build that reads the source) rather than transcribing them by hand. A generated fact can't silently disagree with the code; a typed-in one will.
- **Document behavior that exists, not behavior that's planned.** Describe what the code provably does today. A feature "coming soon" or a flag "to be added" either exists and is verified, or it stays out until it does. This is §5 of the SKILL again, stated for prose.
- **Mark version and as-of where it matters.** When a claim is true only for a version or a date — an API shape, a pricing number, a dependency requirement — stamp it ("as of v3.2", "checked 2026-07"). An unstamped time-bound fact reads as eternal and misleads the moment it lapses. This mirrors the recency-stamping the `loop-research` skill applies to claims.
- **Leave a sync signal.** Where a doc necessarily tracks a specific symbol, flag the coupling both ways — a `// keep in sync with docs/api.md#auth` near the code and a pointer from the doc back to the source — so a future change surfaces the doc it invalidates (SKILL §6).

## 8. Before / after: vague and passive → precise and active

Rewriting is where the rules above become concrete. The pattern: name the actor, state the exact behavior, replace hedging with a checked fact, lead with the point.

**Before** (vague, passive, no example, buried point):

> The configuration can be adjusted in various ways to change how the system behaves. Timeouts may be configured, and it is generally recommended that reasonable values are used. Requests that take too long will eventually be handled appropriately.

**After** (precise, active, present tense, example first, term defined):

> Set `requestTimeout` (milliseconds) to bound how long the client waits for a response. It defaults to `30000`.
>
> ```js
> client.configure({ requestTimeout: 5000 }); // fail fast after 5s
> ```
>
> When the timeout elapses, the client aborts the request and throws `TimeoutError`. Set it below your upstream's own limit so the client, not the proxy, controls the deadline.

What changed, rule by rule: the actor is named ("the client aborts", not "will be handled"); the behavior is exact (`TimeoutError`, `30000`) and verified against the source rather than described as "appropriate"; a runnable example leads (§3); voice is active and tense present (§4); the operative instruction sits first, not after the throat-clearing (§6); and the default is stated once, ideally pulled from source rather than retyped (§7). If you cannot confirm the default or the error type at the source, you do not write them — you verify or you mark the gap (§2, §5 of the SKILL).
