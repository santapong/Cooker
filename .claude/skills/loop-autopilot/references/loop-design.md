# Loop Design

The autonomous improvement loop in full. It reuses the `loop-engine` skill's loop and harness policies — this file is how those rules apply to a self-improving project, not a new set of rules.

## The round

```
intake → (fresh?) → triage → act → verify → propose
             │
             └─ (empty) → research / tech-debt scan → propose ideas
```

Each round pulls a batch of work, does it, and proposes it. Feedback work and idle-research work both end at the same **propose** gate (a draft PR or a proposal issue). The loop repeats until a stop condition fires.

## Guards (all three, per loop-policy)

- **Budget floor (L2)** — `while (budget.total && budget.remaining() > FLOOR ...)`. `FLOOR` = the cost of one full round plus verification headroom (start ~60k tokens). The `budget.total &&` guard is mandatory: without a token target `remaining()` is `Infinity` and the loop would run to the agent-cap. With no target, do a **single** bounded round.
- **Dry counter (L1)** — track consecutive rounds with no fresh feedback. After **K** idle rounds (default 2), stop — the project has no pending feedback and you've done a research pass; don't spin.
- **Hard round cap (L4)** — `round < MAX_ROUNDS` (default ~6) as a backstop. Hitting the 1000-agent limit means the loop was mis-designed.

Combine them: `while (budget.total && budget.remaining() > FLOOR && round < MAX_ROUNDS && dry < K)`. The loop condition tests only cheap facts (budget, round, dry count) — **never** an agent's opinion on whether to continue (L7).

## Convergence — never re-propose the same thing (L3)

Keep a `seen` set keyed on a stable identity (issue/PR number, or `kind:title` for an idea), and **before acting**, drop any candidate that is:

1. Already in `seen` this run, **or**
2. Already tracked by an **open issue or open PR** on the repo (query first).

Dedup only against *seen + open work*, not against what you've already merged — and add to `seen` *before* acting, so a dropped-in-verification item doesn't reappear next round. Dedup is plain logic in the loop, never an agent call.

## Prioritize

Not all feedback is equal. Within a round, order the batch by impact × urgency: red CI and reported bugs first (they block others), then requested changes on open PRs, then issues, then proactive tech-debt and research ideas. Use `loop-orchestrate` to turn a large batch into an ordered task DAG and to pick the model/effort per item (cheap triage on a small model, careful fixes and verification on a strong one).

## Act as an AIDLC Construction pass

Each accepted item runs design → implement → test on a `claude/` branch, routed to the owning skill (`loop-debug`, `loop-design`, `loop-test`, `loop-docs`), with `loop-scout` as the pre-build "don't reinvent it" gate. Keep changes small and single-purpose — one item, one focused PR.

## Verify before proposing

Adversarially self-review with `loop-review` and package the impact with `loop-audit`. A change that fails review or whose risk memo flags it as unsafe/unclear is **not proposed** — it's dropped or turned into an issue that explains the blocker. Better to propose nothing than to propose noise; the loop's credibility is its signal-to-noise.

## Propose — the single human gate (H11)

The loop ends every unit of work at a draft PR (or a proposal issue) and a summary comment. It **never merges**. This is harness policy H11 — one workflow per human gate — applied continuously: the human reviews and merges, the loop keeps finding the next thing. When deployed as a Cloud Routine, this gate is the *only* thing standing between "autonomous" and "unsupervised writes to main," so it is enforced structurally (draft PR + `claude/` branch + never-merge), not by an approval prompt.
