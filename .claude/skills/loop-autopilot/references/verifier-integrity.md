# Verifier Integrity

`anti-patterns.md` catalogs five ways the loop degrades by **skipping a move** —
verify, persist, schedule, discover, hand off. Every one is guarded. But all five
guards assume the verifier *tells the truth*. AP1 (Nodding Loop) fires when
verification is **skipped**; it says nothing about verification that **runs and is
fooled**. That is a distinct failure, and none of AP1–AP5 covers it.

## AP6 — Gamed Loop (verification runs but is fooled)

**Symptom:** `safeToPropose` keeps returning `true`, the risk memos read clean, the
credit ledger's trust weights hold or climb — and the artifact is getting *worse*. The
loop hasn't stopped verifying. It has learned to produce diffs that pass its own check
without being good, or the check itself has drifted into agreeing with them.

**Why the other guards can't see it.** AP1's fix is `safeToPropose` gating every item.
But `safeToPropose` is the output of `loop-review` + `loop-audit` — the same
model family that produced the diff in the Act stage (harness policy leaves the judge
un-blinded by default). A judge scoring its own family's work is subject to three
documented pathologies from the LLM-as-judge literature:

- **Self-preference** — a judge rates outputs from its own model family higher than a
  neutral oracle would, independent of quality.
- **Rubric hacking** — the generator satisfies the *letter* of the rubric
  (`loop-audit` checks a memo exists, tests were added, files listed) while
  missing its intent; the check passes on form.
- **Master-key responses** — a small set of near-universal strings ("looks correct,
  well-tested, low risk") that push an LLM judge toward a high score regardless of the
  diff under review.

None of these trips a guard, because from the loop's point of view *the verification
step succeeded*. AP6 is invisible to everything inside the loop by construction — the
judge cannot report that it was fooled. It has to be caught from **outside** the loop
(`held-out-eval.md`, the detector) and made **hard to do** by structure (this file, the
three guards).

**Status here: guarded by three structural checks below + the held-out detector.**

## The load-bearing principle

You cannot make an LLM judge un-gameable by writing a better rubric — a better rubric is
just a longer surface to hack. The defenses that work are **structural**: checks the
generator cannot satisfy by writing more convincing prose, because they don't grade
prose. Each of the three below has a binary, generator-independent answer.

## Guard 1 — Impossible-test canary

Plant a test that **must fail** — it asserts something false about the codebase (a known
bug is absent, `1 == 2`, a function returns a value it provably cannot). A correct diff
leaves it failing. Run the candidate's full test command with the canary injected.

- Canary still **fails** → the test harness is honest. Proceed.
- Canary **passes** → the loop has done something that makes failing tests report
  success: edited the runner, mocked the assertion, wrapped the suite in a
  `try/except: pass`, special-cased the canary's name, or set `continue-on-error`. Any
  of these is an integrity violation. **Drop the candidate, do not propose, escalate.**

This is the single cheapest AP6 guard and catches the most damaging class — a generator
that has learned to make the *verifier's own signal* meaningless. It does not grade the
diff; it grades whether "a test failed" still means what it should. `loop-test`
already verifies each real test "fails before, passes after"; the canary is the
adversarial inverse — a test that must *never* pass, watched for the moment it does.

## Guard 2 — Diff-integrity guard (protected paths)

AP5's worktree isolation stops concurrent Act stages from colliding. It does **not** stop
a single Act stage from editing the things that judge it. The loop's incentive, once it
is optimizing to pass, is to edit the ruler rather than the work. Refuse any diff that
touches a **protected path**:

- the test files the change is verified against (weakening a test to pass it is the
  oldest form of rubric hacking);
- the rubric / verify prompt the judge reads;
- the held-out suite and its oracle (`held-out-eval.md`) — if the loop can see or edit
  the held-out set, it is no longer held out;
- the canary from Guard 1;
- CI config that could set `continue-on-error`, skip a job, or relax required checks.

Compute the diff's touched paths and intersect with the protected set **before** the
verify stage runs. A non-empty intersection is a hard stop, logged as an integrity
violation — not a low score to be argued down. This extends the immutable-tests
intuition to *everything the loop could edit to make its own judgment easier*.

## Guard 3 — Self-preference cross-check (sampled)

Guards 1 and 2 are binary and run every item. Self-preference is statistical, so check
it on a **sample**, not every diff (it costs a second judge call). On a random subset of
diffs the primary judge marked `safeToPropose: true`, re-judge with a **different model
family** (a cross-judge) against the same rubric.

- Cross-judge agrees → evidence the pass was about the diff, not the judge's affinity
  for its own family's style.
- Cross-judge disagrees, and the rate of disagreement is **rising across runs** → the
  primary judge is drifting toward self-preference. This is the same rising-divergence
  signal `held-out-eval.md` watches for, measured a cheaper way when a hidden oracle
  isn't available for that item.

Sampling here is deliberate, and the reasoning is the mirror image of
`comprehension-rot.md`: there, sampling is *uniform-at-random over time* so a
three-week-old regression can surface; here, sampling is over *items* so the second
judge's cost stays bounded while still producing a trend. A single disagreement means
little; the **rate over time** is the measurement.

## How AP6 relates to the human gate and the ledger

The loop is propose-only — a human still merges (`SKILL.md` §5). So why harden a verifier
that only gates *proposals*, not merges? Two reasons.

1. **The ledger trusts the merge signal, and the merge signal trusts the human.**
   `credit-horizon.md`'s trust weights are learned from what gets merged. If the human
   is a Nodding reviewer (`comprehension-rot.md`), "merged" stops meaning "good," and a
   gamed verifier now has a gamed teacher — the ledger learns to trust exactly the kinds
   the loop games best. AP6 + comprehension-rot compound: a fooled judge feeding a
   fooled ledger, both reading clean.
2. **AP6 is the gate that must hold before SCALE.** Removing the human merge step
   (SCALE) means `safeToPropose` *becomes* the merge decision. Every reason a human
   still merges today is a reason to trust the verifier less than that promotion
   requires. The three guards here plus the held-out detector are the evidence you would
   need before that promotion is anything but reckless — and even then, the safe path is
   not a perfect gate but merge-behind-canary + agent-driven rollback (the SCALE
   mechanism, drafted in `deployment.md` §"Advanced: autonomous delivery" as an
   off-by-default mode), because no gate catches everything.

## Using this file

Run the three guards as a pre-flight before the Propose gate, wired via
`templates/verifier-canary.workflow.js`. Guards 1 and 2 are hard stops on every item;
Guard 3 is a sampled trend. Re-check them whenever the verify stage, the rubric, or the
credit ledger changes — a change that raises pass rates is exactly when to ask whether
the work got better or the judge got easier.

| Code | Name | Move missing | Status |
|---|---|---|---|
| AP1 | Nodding Loop | Run verification | ✅ guarded (`safeToPropose`) |
| AP6 | Gamed Loop | Make verification un-gameable | ✅ guarded (canary + diff-integrity + cross-check, & held-out detector) |

AP6 sits directly under AP1 in the table on purpose: same stage of the loop (Verify),
opposite failure. AP1 is verification absent; AP6 is verification present and lying.
