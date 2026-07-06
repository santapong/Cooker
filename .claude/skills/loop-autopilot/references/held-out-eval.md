# Held-Out Eval

`verifier-integrity.md` establishes that AP6 (a fooled verifier) is invisible from
inside the loop — the judge cannot report that it was gamed. Guards 1–3 there make
gaming *hard*; this file is the **detector** that catches it when it happens anyway. It
is the one measurement in this plugin taken entirely from outside the loop, against
ground truth the loop never sees.

## What "meta-overfit" means for an LLM loop (not an RL one)

The self-improvement literature describes loops that overfit their own training traces —
weights drift to score well on the loop's accumulated data while degrading on held-out
evaluation. That framing assumes gradient updates to a model. **This loop has none.**
The model (Claude) is fixed from round to round. So what drifts?

The loop's *accumulated configuration*: the rubric phrasing the judge reads, the credit
ledger's trust weights (`credit-horizon.md`), and any "learnings" carried forward. That
is the analogue of a training set here — and it is exactly what can overfit. **Meta-
overfit in this loop = the accumulated config passing its own judgments while diverging
from ground truth.** The generator learns which framings the *current* rubric rewards;
the ledger learns to trust the kinds that *currently* get merged; both improve on their
own recent history and can drift away from what's actually correct. Same failure,
different substrate: not weights over gradient steps, but config over loop-rounds.

This distinction is load-bearing because it tells you *what to freeze*. You are not
snapshotting model weights. You are holding a fixed set of tasks with hidden ground
truth constant, and re-running the **current** verify configuration against them over
the loop's life, watching whether the current config's verdicts still match the frozen
oracle.

## The measurement: two verdicts per task, and their gap

The held-out suite is a frozen set of tasks. Each task carries a **hidden oracle** — a
deterministic test command that returns the true pass/fail, and that the loop's Act and
Verify stages never see. For each task, run the loop's *current* verify path to get its
verdict, and run the oracle to get the truth:

| Loop verdict (`safeToPropose`) | Oracle (hidden test) | Meaning |
|---|---|---|
| pass | pass | true accept — judge and truth agree |
| **pass** | **fail** | **false accept — the judge was fooled (the AP6 signal)** |
| fail | pass | false reject — over-cautious; wastes work, not dangerous |
| fail | fail | true reject — judge caught a real problem |

The number that matters is the **false-accept rate**: tasks the loop would have proposed
that the oracle says are wrong. A single number in isolation tells you the judge's
current accuracy. The *trend of that number across runs* tells you whether the loop is
meta-overfitting.

## The alarm: rising divergence, not a fixed threshold

Capture a **baseline** false-accept rate the day the loop is deployed — the judge's
honest starting accuracy against ground truth. Persist it (the pinned-issue pattern from
`credit-ledger.workflow.js`). On each held-out run, compare the current false-accept
rate to the baseline:

- **Flat or falling** → the loop's config is holding or improving against truth. Healthy.
- **Rising across consecutive runs** → the config is drifting: the generator is learning
  the rubric's blind spots, or the ledger is steering toward gameable kinds, faster than
  the work is actually improving. **This is the self-improvement-reversal signature.**
  Freeze config changes, escalate, and diff what changed in the rubric/ledger since the
  baseline.

The signal is the **divergence between the loop's own pass rate and the oracle's pass
rate over time**. If in-loop `safeToPropose` climbs while the held-out oracle rate stays
flat or drops, the loop is getting better at pleasing itself, not at the work. That
crossing — self-score up, held-out flat — is the whole thing this file exists to make
visible, the same way `comprehension-rot.md` makes the human's unread-merge gap visible.
Two silent failures, two external detectors: comprehension-rot watches the *human* stop
reading; held-out-eval watches the *judge* stop being right.

## Building the suite — three rules

1. **Ground truth must be a deterministic oracle, not a judgment.** If the "correct"
   answer is itself an LLM's opinion, you have not left the loop — you've added a second
   judge that can be gamed too. Each task's oracle is a test command with a binary exit
   code: a hidden regression test, a property check, a reference-output diff. The point
   of held-out is that the answer is *known* and *external*.
2. **The suite is never visible to Act or Verify.** This is a protected path in
   `verifier-integrity.md` Guard 2, and it is the entire discipline: the moment the
   generator can read the held-out tasks or their oracles, they measure nothing. Store
   them where the loop's tooling is not pointed — a separate location, a Routine-only
   secret path, or a private companion repo — never in the working tree the Act stage
   checks out.
3. **Freeze it, and version it.** A suite you edit while the loop runs is not a baseline.
   Snapshot it, record the baseline false-accept rate against that snapshot, and only
   cut a *new* versioned suite (with a fresh baseline) deliberately — never patch the
   live one to "add a case the loop missed," which contaminates the trend.

## Sizing and cadence

Held-out eval is expensive per task (it runs the full verify path plus the oracle), so
it does not run every loop round. Deploy it as its own scheduled Routine — the same
shape as `credit-ledger.workflow.js` (a periodic reconcile against a pinned baseline),
weekly or every N proposals rather than per-round. A suite of 20–40 tasks spanning the
kinds the loop actually proposes (bug fix, feature, tech-debt, doc) is enough to move
the false-accept rate meaningfully; fewer than ~15 and single-task noise swamps the
trend. Weight the suite toward the kinds the credit ledger reports highest trust for —
those are where an undetected gamed judge does the most damage, because the loop proposes
them most and the human waves them through fastest.

## Why this is the first SUSTAIN build, and where it stops

Held-out eval is the detector the other five guards need to stay honest — without it, AP6
is unfalsifiable and every "the loop is improving" claim is the loop grading its own
homework. It is necessary for SUSTAIN and it is a **precondition for SCALE**: you cannot
responsibly promote `safeToPropose` to a merge decision (removing the human) without a
standing, external measurement of how often that verdict is wrong against ground truth.
But it is a detector, not a cure — it tells you the judge drifted, after it drifted. The
cure at SCALE is not "wait for a green held-out run and then trust the gate forever"; it
is merge-behind-canary + agent-driven rollback (the SCALE mechanism, drafted in
`deployment.md`, off by default),
with held-out eval as the standing tripwire that pulls autonomy back to propose-only the
moment divergence rises.
