# Prior art behind the governance layer

The harness and loop policies in this skill are **homegrown by design** — deliberately light, tuned to the Workflow tool's actual primitives (`pipeline`, `parallel`, `agent`, `budget`), not lifted from any external spec. But they are not invented from nothing: every rule formalizes a pattern that distributed-systems and multi-agent research settled decades ago. This file names that prior art, pins the canonical reference, and maps each source to the specific policy rule it stands behind — so a reviewer can defend "why a barrier must be earned" or "why loops dedup against *seen*" by pointing at established theory, not taste.

This is not a compliance checklist. Unlike a versioned standard (see `../../loop-review/references/owasp-cwe.md` for that shape), these are **foundational papers and named patterns**: the canonical citation is stable, and what actually moves is the implementation ecosystem and the fast-churning LLM-orchestration literature. Read `harness-policy.md` and `loop-policy.md` first; this file is the "why it's shaped that way" layer beneath them.

## The lineage at a glance

| Workflow primitive | Prior art it formalizes | Canonical source (issuing body) | Policy rule |
|---|---|---|---|
| `parallel()` fan-out + barrier | **Scatter–Gather / fan-out–fan-in** | Hohpe & Woolf, *Enterprise Integration Patterns* (Addison-Wesley, 2003) | H2, H5 |
| `parallel()` as reduce / merge | **MapReduce** | Dean & Ghemawat, OSDI 2004 (Google) | H1, H2 |
| `pipeline()` stage dependencies | **DAG execution / topological scheduling** | Kahn, *CACM* 1962; dataflow (Dryad, EuroSys 2007) | H1, H9 |
| `agent()` isolation, no shared memory | **Actor model** | Hewitt 1973; Agha, *Actors* (MIT, 1986) | H3, H7 |
| loop-until-dry shared result set | **Blackboard architecture** | Hearsay-II, *ACM Computing Surveys* 1980; Nii, *AI Magazine* 1986 | L1, L3 |
| orchestrator + judge/verify panels | **Contract Net / multi-agent orchestration** | Smith, *IEEE Trans. Computers* 1980; FIPA specs (2002) | H4, H12 |

## Fan-out / fan-in (Scatter–Gather)

**What it is.** The concurrency pattern where one unit of work is split across N workers and their results are collected back at a join point. Its authoritative naming is the **Scatter–Gather** and **Splitter / Aggregator** patterns from Hohpe & Woolf's *Enterprise Integration Patterns* (2003, still the reference edition); cloud workflow engines re-expose it verbatim (AWS Step Functions **Map** state, Azure Durable Functions "fan-out/fan-in").

**How the skill applies it.** `parallel(thunks)` *is* the scatter; consuming its array *is* the gather. The pattern's hard lesson — a gather must tolerate a worker that never reports — is exactly **H5** (`parallel()` never rejects; `.filter(Boolean)` before consuming). Use it when the join genuinely needs all results at once; otherwise the barrier is unearned (**H2**).

## MapReduce

**What it is.** Dean & Ghemawat, "MapReduce: Simplified Data Processing on Large Clusters," **OSDI 2004** (Google; revised *CACM* 2008) — the canonical citation, unversioned. A **map** phase transforms items independently and a **reduce** phase folds them behind a barrier.

**How the skill applies it.** A per-item `pipeline()` stage is the map; a `parallel()` barrier that dedups/merges the full result set is the reduce. The key insight the policy inherits: **map needs no barrier, reduce does** — which is precisely **H1** (default to `pipeline()`, no barrier between stages) and **H2** (a barrier is earned only by a true cross-item reduce, not by a per-item transform that belongs inside a stage).

## DAG execution & topological scheduling

**What it is.** Modeling a job as a directed acyclic graph and running nodes in **topological order** so every task starts only after its dependencies finish. Rooted in **Kahn's algorithm** (A.B. Kahn, *CACM* 1962) and the dataflow-execution line (Dryad, Isard et al., EuroSys 2007); the modern living implementation is **Apache Airflow's** DAG scheduler (current major line 3.x as of 2026).

**How the skill applies it.** `pipeline(items, s1, s2, …)` is a linear DAG per item; `meta.phases` plus `phase()` ordering is the coarse-grained DAG across the run. **H9** (phase-title discipline, per-agent `opts.phase` inside fan-outs) is topological-scheduling hygiene: it keeps the dependency graph legible and race-free. When you sketch a task's shape before authoring, you are drawing this DAG.

## Actor model

**What it is.** Hewitt's 1973 formalism, made rigorous by **Gul Agha's *Actors*** (MIT Press, 1986): independent units that hold no shared state and communicate only by messages. Production lineage runs through Erlang/OTP and Akka.

**How the skill applies it.** Every `agent()` is an actor — fresh, memoryless, isolated, communicating only through its structured return. **H3** (schema on every consumed result; agents return data, not prose) is the actor message contract. **H7** (worktree isolation only for concurrent mutation) and **L8** (rounds vary, agents don't remember) fall straight out of the no-shared-state premise: because actors share nothing, isolation is only needed when they'd collide on the filesystem, and cross-round continuity must be re-injected via the prompt.

## Blackboard architecture

**What it is.** A shared workspace ("blackboard") that multiple specialist knowledge sources read from and write to until a solution emerges. Named by the **Hearsay-II** speech system (Erman, Hayes-Roth, Lesser & Reddy, *ACM Computing Surveys* 1980) and surveyed by **H. Penny Nii** (*AI Magazine* 1986).

**How the skill applies it.** The loop-until-dry `seen` set and accumulating `confirmed` list are a blackboard: each finder round posts to it, and the run converges when the board stops changing. **L1** (stop after K dry rounds) is the blackboard's termination condition, and **L3** (dedup against everything *seen*, not just what's confirmed) is the rule that keeps the board monotonic so it can actually go quiet — dedup against only accepted items and rejected findings re-post forever.

## Contract Net & multi-agent orchestration

**What it is.** **Reid G. Smith's Contract Net Protocol** (*IEEE Transactions on Computers*, 1980) — a manager decomposes a task, distributes it, and integrates bids/results — is the seminal orchestration pattern; the **FIPA** agent specifications (Foundation for Intelligent Physical Agents, 2002, the last ratified set) standardized the messaging. This is the fastest-moving layer: contemporary LLM multi-agent-orchestration literature (2023–2026) is where the ideas are being re-derived, and it has **no settled edition**.

**How the skill applies it.** The orchestrating session is the manager; `agent()` fan-outs are the contractors. **H4** (adversarial verify, perspective-diverse lenses, judge panels) and **H12** (completeness critic) are orchestration-quality controls: independent skeptics and diverse judges are the multi-agent answer to a single contractor being wrong. Because this layer is unsettled, prefer the durable primitive (independent verification, majority refute) over any specific framework's vocabulary.

## Map-reduce vs. pipeline: the trade-off the policy encodes

The central design tension the harness policy resolves is **barrier vs. stream**:

| | Barrier (reduce / `parallel()`) | Stream (pipeline / `pipeline()`) |
|---|---|---|
| Wall-clock cost | Slowest item *per stage*, summed | Slowest single *item chain* |
| Cross-item context | Available — all results in hand | Not available within a stage |
| When correct | A stage truly needs the whole set (dedup, zero-count early-exit, "compare to the other findings") | Every stage is per-item independent |

**H1/H2 pick streaming as the default and force barriers to be earned** precisely because the pipeline wins on wall-clock whenever stages are independent, and the only thing a barrier buys is cross-item context. Reach for a barrier only when a stage genuinely consumes the full set; otherwise the "reduce" is a per-item transform hiding in the wrong shape.

## Edition discipline

These are **foundational sources, not reissued standards** — there is no "2025 edition" of MapReduce or the actor model, and the canonical citations above are stable to cite indefinitely. What *does* move is two layers, and those are where to re-check on a cadence:

- **Living implementations** — Airflow's scheduler, Akka, Step Functions, Durable Functions. Their versions advance; cite them as "current" and verify the major line before leaning on a specific behavior.
- **LLM multi-agent orchestration** — the Contract Net descendants being re-derived for language agents. This is genuinely unsettled and fast-churning; treat any named framework or vocabulary here as provisional, and re-scan roughly **every 6–12 months**. When it consolidates into something citable, add it as its own section rather than retrofitting a paper's meaning.

Rule of thumb, mirroring the loop-review skill's edition handling: **cite the stable seminal source for the concept, and pin the moving implementation separately** — never blur the timeless pattern with the version of the tool that happens to implement it this year. When you update this file, update `harness-policy.md` / `loop-policy.md` cross-references in lockstep so a policy rule and its prior-art anchor never drift apart.
