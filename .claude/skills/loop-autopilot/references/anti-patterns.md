# Anti-Patterns

Five ways an autonomous loop degrades by **skipping a move** (AP1–AP5), plus a sixth
(AP6) where a move **runs but is fooled**. Each is a single failure, not a vague
"quality" problem — naming which pattern you're looking at tells you which one thing
broke, not the whole system. Run this checklist before deploying as a Cloud Routine,
and re-run it any time the loop's behavior feels off or its guards change. AP1–AP5 are
detailed below; AP6 is summarized here and treated in full in `verifier-integrity.md`.

## AP1 — Nodding Loop (verification skipped)

**Symptom:** proposals get approved with rubber-stamp merges; plausible-looking mistakes
slip through and surface days later.

**Status here: guarded.** `safeToPropose` (`loop-design.md`, §Verify) gates every item —
a change that fails `loop-review`'s adversarial pass, or whose `loop-audit`
memo flags unsafe or unclear, is dropped, not proposed.

**Not guardable by architecture:** a human merging drafts without reading the risk memo
is still a Nodding Loop, just a human-side one. See `comprehension-rot.md`.

## AP2 — Amnesiac Loop (persistence skipped)

**Symptom:** no cumulative progress — the same issue gets "fixed" repeatedly because
nothing remembers it was already handled.

**Status here: guarded.** The draft PR *is* the persistent record (`loop-design.md`,
§Propose). Within a run, the `seen` set additionally blocks re-proposing the same item.

## AP3 — Manual Loop (scheduling skipped)

**Symptom:** it isn't really a loop — it's a script someone has to remember to run.

**Status here: guarded, but read the default correctly.** A manual `Workflow()` call in
`mode: "dry"` (`SKILL.md` §6) *is* a Manual Loop, on purpose — that's the safe on-ramp
before you trust it unattended. It only becomes a real loop once deployed as a Cloud
Routine or Action (`deployment.md`). The anti-pattern isn't the dry-mode default — it's
still running it by hand three months after "Unattended" was documented and never
graduating.

## AP4 — Blind Loop (discovery skipped)

**Symptom:** a human still decides every session what's worth working on.

**Status here: guarded.** `feedback-intake.md`'s four sources (issues, PR comments, CI,
idle-research) are pulled automatically; nobody hand-picks the day's work.

## AP5 — Tangled Loop (handoff skipped)

**Symptom:** parallel agents collide on the same files or working directory.

**Status here: guarded (worktree isolation on the Act stage).** Harness policy H1 is
explicit — under `pipeline()`, "item A may be in stage 3 while item B is still in stage
1." That's exactly how `improvement-loop.workflow.js` runs its Act→Verify stages:
multiple items' Act stages can be genuinely concurrent, and each checks out a different
`claude/` branch. Two concurrent checkouts in one working directory would corrupt each
other.

**Fix (applied):** in **live** mode the Act-stage `agent()` runs with
`isolation: 'worktree'` — the runtime's built-in per-agent git worktree (harness policy
H7), so each concurrent Act stage mutates files in its own isolated tree and the
collision cannot happen. Dry mode is read-only and needs no isolation. If you replace
worktree isolation with something cheaper, cap in-flight Act calls to 1 (a `parallel()`
barrier or a semaphore) instead — never run concurrent file-mutating Act stages in a
shared directory.

## AP6 — Gamed Loop (verification runs but is fooled)

**Symptom:** `safeToPropose` keeps returning `true`, the memos read clean, the credit
ledger holds — and the artifact is getting worse. The loop hasn't stopped verifying; it
has learned to pass its own check without being good, or the check has drifted into
agreeing with it (self-preference, rubric hacking, master-key responses).

**Why it's not AP1.** AP1 is verification *skipped*; AP6 is verification *present and
lying*. `safeToPropose` gating every item does nothing here, because the judge is the
thing that was fooled — and a fooled judge cannot report it. AP6 is invisible from inside
the loop by construction, so it can't be guarded the way AP1–AP5 are; it must be made
*hard* by structure and caught from *outside*.

**Status here: guarded — see `verifier-integrity.md` (full treatment) and §7 of
`SKILL.md`.** Three structural guards run in-band before Propose
(`templates/verifier-canary.workflow.js`): an impossible-test canary, a diff-integrity
check on protected paths, and a sampled cross-judge. One detector runs out-of-band
(`templates/held-out-eval.workflow.js`, `held-out-eval.md`): a frozen suite with hidden
oracles whose rising false-accept rate is the meta-overfit alarm. This is the gate that
must hold before SCALE (removing the human merge).

## Using this file

Don't treat it as a one-time read. A fix for one pattern can quietly reopen another —
e.g. raising `MAX_ROUNDS` to push past a false AP3 diagnosis increases concurrent items,
which is only safe because AP5 is now guarded by worktree isolation; if you ever remove
that isolation, AP5 reopens. Re-check all five rows whenever
`improvement-loop.workflow.js` or its guards change, not just the row you think you
touched.

| Code | Name | Move missing | Status |
|---|---|---|---|
| AP1 | Nodding Loop | Verification | ✅ guarded |
| AP2 | Amnesiac Loop | Persistence | ✅ guarded |
| AP3 | Manual Loop | Scheduling | ✅ guarded (mind the dry-mode default) |
| AP4 | Blind Loop | Discovery | ✅ guarded |
| AP5 | Tangled Loop | Handoff | ✅ guarded (worktree isolation, live mode) |
| AP6 | Gamed Loop | Make verification un-gameable | ✅ guarded (canary + diff-integrity + cross-check, & held-out detector) — see `verifier-integrity.md` |
