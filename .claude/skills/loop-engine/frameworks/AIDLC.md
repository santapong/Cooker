---
name: AIDLC
summary: AI-Driven Development Life Cycle — three gated phases (Inception → Construction → Operation) that take a software task from intent to verified, documented delivery.
when-to-use: Feature builds, refactors, migrations, and any software change big enough to benefit from explicit design, review, and verification gates. Default framework for this skill.
---

# AIDLC — AI-Driven Development Life Cycle

AIDLC structures a software task as three phases. Each phase ends at a **human gate**: the workflow returns the phase's deliverable, the user approves (or redirects), and the next phase runs as a fresh workflow (harness policy H11). Small tasks may skip straight to Construction; pure investigations may end at Inception.

## Phase: Inception

- **Purpose**: Turn the user's intent into an approved, decomposed plan — the *units of work*.
- **Entry criteria**: A task statement from the user; access to the codebase or problem domain.
- **Agent activities**:
  - Parallel readers map the relevant subsystems, existing patterns, and constraints (one agent per area — requirements, current implementation, tests, dependencies).
  - A synthesis step (plain script logic or one synthesizer agent) merges the maps and decomposes the task into independent units of work, each with scope, touched files, and acceptance criteria.
- **Orchestration hint**: `parallel()` fan-out of readers with a barrier — synthesis genuinely needs all maps at once (harness policy H2). Use `pipeline()` instead if areas are independent and don't need cross-referencing.
- **Exit gate (human)**: Present the unit-of-work breakdown and the plan. User approves scope before any code changes.

## Phase: Construction

- **Purpose**: Design, implement, and test each approved unit of work.
- **Entry criteria**: Approved unit-of-work list from Inception.
- **Agent activities**:
  - Per unit: design the change → implement it → run/write tests. This is a per-item chain.
  - After implementation: adversarial review — finders sweep the diff per lens (correctness, security, regressions), verifiers try to refute each finding (harness policy H4), and confirmed findings loop back into fixes.
- **Orchestration hint**: `pipeline(units, design, implement, test)` — no barrier; unit A can be in test while unit B is in design. Use `isolation: 'worktree'` **only** if units mutate overlapping files concurrently (harness policy H7). The review sweep is a `parallel()` fan-out with a dedup barrier.
- **Exit gate (human)**: Present the diff, test results, and confirmed-then-fixed review findings. User approves before delivery work.

## Phase: Operation

- **Purpose**: Prove the change works end-to-end and make it deliverable.
- **Entry criteria**: Approved implementation from Construction.
- **Agent activities**:
  - End-to-end verification: drive the affected flows for real, not just unit tests.
  - A verification loop until dry (loop policy L1): finders hunt integration gaps and regressions until two consecutive clean rounds.
  - Produce docs, changelog/release notes, and a completeness-critic pass ("what's missing?" — harness policy H12).
- **Orchestration hint**: Loop-until-dry for the verification hunt; a small `pipeline()` for docs/notes artifacts.
- **Exit gate (human)**: Present verification evidence, docs, and known gaps. User approves ship/merge.
