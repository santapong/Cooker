# Hypothesis Testing

The parallel-hypothesis method for finding a root cause. You have a localized fault region (`methodology.md` got you there) and a reproduction that fails on demand (`../SKILL.md` §1). Now you turn the evidence into a set of concrete, falsifiable hypotheses about *why* the bug happens, and test each one until exactly one survives. The engine is elimination, not confirmation: **a hypothesis you cannot prove wrong is not a hypothesis, it is a hunch** — and a debugging session that only looks for evidence it is right will find it whether or not it is true. Every reported root cause here has a test that could have killed it and didn't.

## 1. Generate a *set*, not a favorite

Do not latch onto the first plausible cause. From the localized region and the evidence (the stack, the logs, the diff since last-good, the shape of the failure), enumerate **every** cause that could produce the observed symptom — the boring ones (a null slipping through, an off-by-one, a stale cache, a wrong config value) alongside the interesting ones. **List at least three before you test any of them.** A single-hypothesis investigation is confirmation bias with a plan: you will spend an hour proving the first idea instead of five minutes eliminating the other two.

Each hypothesis must be a **specific causal claim about a named location**, not a category. "Concurrency issue" is not a hypothesis; "two requests race on `session.user` because the write in `login()` is not awaited before the read in `authorize()`" is — it names the mechanism, the site, and predicts a symptom you can go check.

## 2. Make each hypothesis falsifiable

Write every hypothesis in the form **"if it is X, then doing Y will show Z."** The `Y` is a concrete action (add a log line, pass this input, pin this clock, revert this commit); the `Z` is an observation that must appear if `X` is true and must *not* appear if `X` is false. If you cannot fill in a `Y` and `Z` that would distinguish the world where the hypothesis holds from the world where it doesn't, the hypothesis is not yet testable — sharpen it until it is.

The discriminating power is the whole point. **A test whose result is the same whether or not the hypothesis is true tells you nothing** — it feels like progress and is not. Before running any test, ask: "which outcome kills this hypothesis?" If neither outcome would, redesign the test. Prefer a single test that splits the hypothesis set — an observation that confirms one candidate *and* eliminates others at the same time is worth more than three that each touch one.

## 3. Rank by likelihood × cheapness, test cheap-and-likely first

You have a set; you need an order. Score each hypothesis on two axes and multiply:

- **Prior likelihood** — how well it fits the evidence you already have. A cause that explains *all* the symptoms (including the weird detail) outranks one that explains only the headline. Recent changes in the fault region are higher-prior than untouched code (see `methodology.md` on `git bisect`).
- **Cheapness to test** — how fast and cheap the discriminating test is. Reading a variable at a breakpoint or adding one log line is cheap; standing up a load test to reproduce a race is expensive.

**Test in descending likelihood × cheapness order.** A cheap test that eliminates a likely cause is the best move on the board even if some less-likely cause would have been more satisfying to chase — you are buying the most certainty per minute. Run expensive tests only after the cheap ones failed to explain the failure.

## 4. Eliminating is progress

The goal of a test is not to crown a winner; it is to **shrink the set**. Every hypothesis you eliminate is real progress even though the bug is still unfixed — you have narrowed the space of possible causes, and the survivor is closer. Treat a confirmed *elimination* as a win and log it, so no one re-investigates that branch later.

Three verdicts per test:

- **CONFIRMED** — the predicted `Z` appeared and no rival hypothesis predicts the same `Z`. This is your root cause (validate the causal chain end-to-end before fixing — §5 of `../SKILL.md`).
- **ELIMINATED** — the predicted `Z` did not appear, so `X` is false. Cross it off. Progress.
- **INCONCLUSIVE** — the test didn't actually discriminate (the observation is consistent with both worlds), or the setup was wrong. This is a **failed test, not a surviving hypothesis** — redesign the test (§2) rather than treating "couldn't eliminate it" as "confirmed it."

**Default a hypothesis to not-the-cause until a test earns it CONFIRMED.** Do not promote the last-one-standing to root cause by elimination alone unless your set was genuinely exhaustive — a bug caused by something you never hypothesized will masquerade as "well, it must be the only one left." Confirm the survivor positively.

## 5. When a result surprises you, chase it

A test outcome you did not predict is the highest-value event in the whole method: it means a hidden assumption you were treating as ground truth is false. **Do not explain a surprise away — chase it.** The value held when you were sure it wouldn't; the branch executed that you thought was dead; the log line never printed. That gap between expectation and observation is pointing straight at a wrong belief, and the wrong belief is often the bug (or the reason you couldn't find it). Widen the investigation to the assumption that just broke before returning to your ranked list.

## 6. Avoid confirmation bias: seek the disconfirming test

The failure mode this reference exists to prevent is finding a cause that *fits* and stopping. A hypothesis that fits the evidence is not confirmed — evidence is usually consistent with several causes at once. **Actively design the test that would prove your leading hypothesis wrong**, and only trust it once it survives that test. Concretely:

- For your favored hypothesis, ask "what would I expect to see if this were **false**?" and go look for exactly that. If you find it, the hypothesis is dead no matter how good it felt.
- Keep at least one rival hypothesis alive until a test discriminates between them. A set of one is not being tested, it is being defended.
- Beware the test you *want* to pass. If you are hoping for a green result, you have stopped debugging and started rooting for a fix.

## 7. Mapping to the workflow: fan out one agent per hypothesis

When the hypotheses are **independent and do not share a cheap discriminating test** — so testing them serially is slow and no single observation splits the set — run them in parallel. This is the `../SKILL.md` §8 orchestration path, built on the `../../loop-engine` skill's `templates/parallel.workflow.js`:

1. **Fan out** — one agent per hypothesis (parallel — the earned-barrier rule H2: these are genuinely independent, so a barrier is justified, not reflexive). Each agent is handed **one** hypothesis in the "if X then Y shows Z" form, the reproduction, and the fault-region context.
2. **Structured verdict** — every agent returns the same schema so the orchestrator can converge them mechanically (harness policy H3):

   ```json
   {
     "hypothesis": "session.user race: login() write not awaited before authorize() read",
     "test": "pin two concurrent requests, log session.user between write and read",
     "verdict": "confirmed | eliminated | inconclusive",
     "evidence": "observed session.user === undefined on the second request at authorize()"
   }
   ```

3. **Converge** — the orchestrator keeps the `confirmed` hypothesis as the root cause and **eliminates the rest**, logging each elimination (no silent drops — harness policy H5: an agent that returned null/inconclusive is recorded as inconclusive, never dropped as if it had eliminated its cause). If zero come back `confirmed`, the set was not exhaustive — generate more hypotheses (§1) and fan out again. If two come back `confirmed`, at least one test failed to discriminate — the observations overlapped — so re-test to split them.

Each hypothesis agent is prompted to **eliminate** its assigned cause and to default to `eliminated`/`inconclusive` when the evidence doesn't clearly confirm — the debugging analogue of the adversarial verifier (harness policy H4: verification is adversarial; an agent trying to *kill* its hypothesis is worth more than one trying to bless it, and diverse hypotheses beat redundant ones testing the same idea). The orchestrator trusts a `confirmed` only when its `evidence` shows a discriminating observation, not a restatement of the claim.

Do not fan out for a bug with one or two candidate causes, or when the hypotheses share a single cheap test that splits them all at once — test that inline and skip the agents. The parallel path pays off only when serial testing is genuinely the bottleneck.

## Reference files

| File | What it covers |
|---|---|
| `../SKILL.md` | The reproduce → localize → hypothesize → fix router; §4 points here, and §8 is the orchestration path this file's §7 maps to |
| `methodology.md` | Localization that hands this step its fault region: `git bisect`, binary-searching the code path, instrumentation, delta-debugging |
| `../templates/bug-diagnosis.workflow.js` | The parallel hypotheses → eliminate/converge → fix workflow script |
| `../../loop-engine/references/harness-policy.md` | H2 earned barrier, H3 structured output, H4 adversarial/diverse verify, H5 null handling — cited throughout §7 |
</content>
</invoke>
