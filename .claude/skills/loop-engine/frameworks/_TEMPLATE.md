---
name: <FrameworkName>
summary: <One sentence: what lifecycle this framework imposes and its phase sequence.>
when-to-use: <What kinds of tasks this framework fits; when to prefer it over AIDLC.>
---

# <FrameworkName> — <Full Name>

<Short intro: the phase sequence, whether phases are gated, and which tasks may skip phases.>

## Phase: <Phase 1 Name>

- **Purpose**: <What this phase produces and why.>
- **Entry criteria**: <What must exist/be approved before this phase starts.>
- **Agent activities**: <What to fan out — the kinds of agents, what each reads/produces, what schema-shaped output they return.>
- **Orchestration hint**: <pipeline / parallel-with-barrier / loop-until-dry / loop-until-budget, and why — cite the harness/loop policy rule that justifies any barrier or loop.>
- **Exit gate**: <"(human)" if the user must approve before the next phase, plus what deliverable is presented; otherwise the automatic exit condition.>

## Phase: <Phase 2 Name>

- **Purpose**:
- **Entry criteria**:
- **Agent activities**:
- **Orchestration hint**:
- **Exit gate**:

<!-- Add as many phases as the framework needs. Keep the exact section headings
     above ("Purpose", "Entry criteria", "Agent activities", "Orchestration hint",
     "Exit gate") — the skill reads these fields when mapping a task onto phases. -->
