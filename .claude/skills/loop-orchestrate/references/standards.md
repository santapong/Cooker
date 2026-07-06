# Project-Orchestration Standards — the authoritative frameworks

The named, established frameworks the PM layer leans on. `task-decomposition.md` tells you *how* to build the typed DAG and `model-routing.md` *which model* runs each node; this file names the **prior-art standard behind each judgment call** — how to order the backlog, how to schedule a dependency graph, and the two laws that cap what parallelism and extra agents can buy you. Cite the standard by name and edition when you justify a plan decision; do not reinvent a scoring or scheduling method that one of these already fixes.

Pin the edition you map to (below) and re-check it on the cadence in the closing note — prioritization and PM standards get revised, and a stale formula quietly skews every ledger.

## Prioritization — turning a backlog into ordered nodes

Before the DAG has edges it has a **backlog**: a set of candidate deliverables competing for a bounded `--budget`. These four frameworks turn that set into a defensible order — which nodes become phase-1 roots, which get deferred past a gate, which get cut. Pick one per project and record it in the plan; mixing them silently makes the ordering unauditable.

| Framework | Issuing body | Edition (2026) | Formula / axes | Maps to this skill |
|---|---|---|---|---|
| **WSJF** (Weighted Shortest Job First) | Scaled Agile, Inc. (**SAFe**); originally Reinertsen, *Product Development Flow* | **SAFe 6.0** (2023, current) | `Cost of Delay ÷ Job Size`; CoD = business value + time criticality + risk/opportunity | Rank sibling roots when budget forces a sequence; "Job Size" ≈ a node's `estTokens`, so WSJF orders the ledger's phase-1 slice by value-per-token. |
| **RICE** | Intercom (Sean McBride) | Scoring model, current form | `(Reach × Impact × Confidence) ÷ Effort` | Score discovery/`analyze` nodes whose value is uncertain; low **Confidence** is the signal to insert a cheap `scout` before committing an `implement`. |
| **MoSCoW** | Agile Business Consortium (**DSDM / AgilePF**) | Agile Project Framework, current handbook | Must / Should / Could / Won't-this-time | Tag each node's `phase` scope at a gate: **Must** ships this phase, **Could** is provisional DAG deferred to re-plan (step 6), **Won't** is cut and logged. |
| **Kano** | Noriaki Kano (1984 model) | Original model, stable | Must-be / Performance / Attractive / Indifferent / Reverse | Separate table-stakes deliverables (**Must-be** — never cut, even under budget pressure) from **Attractive** delighters that a tight `--budget` may defer. |

**Choosing among them.** WSJF when a hard `--budget` forces a strict sequence and you can estimate job size (the default for a PM run). RICE when value is speculative and you need a confidence dial. MoSCoW when the real question is *scope at a gate*, not fine-grained order. Kano when the risk is cutting something users treat as mandatory. They compose: MoSCoW to draw the phase line, WSJF to order what's inside it.

## Decomposition — the Work Breakdown Structure (WBS)

The **Work Breakdown Structure** is the canonical decomposition standard — the deliverable-oriented hierarchical breakdown that `task-decomposition.md` §1 ("identifying units of work") is a task-DAG dialect of.

- **Standard.** PMI *Practice Standard for Work Breakdown Structures* (current edition), codified in the **PMBOK Guide, 7th Edition (2021)**. PMI has since shifted to continuous digital updates via PMIstandards+, so treat "7th Edition" as the print baseline and re-check for a successor.
- **The 100% Rule** — the WBS's core discipline: the children of any node sum to **exactly** 100% of the parent's scope, no more (no gold-plating) and no less (no gaps). This is the check behind the decomposition smell tests: a node whose siblings don't cover the parent's deliverable is an underspecified DAG.
- **Work package = leaf node.** A WBS decomposes to **work packages** — the smallest independently assignable, estimable unit. That is exactly a DAG **node**: one deliverable, one `taskType`, one acceptance criterion (§1). Stop decomposing at the same point: when a node is estimable and one agent (or one uniform fan-out) can finish it.
- **WBS is nouns, not verbs.** The standard mandates *deliverable*-oriented naming — mirrored in the rule that a node's `description` names the output ("returns …"), not the activity.

## Scheduling the DAG — CPM & PERT

Once nodes have `dependsOn` edges, two network-analysis standards govern *timing* — which the workflow engine realizes, but which the PM must reason about to size phases and estimate the ledger.

| Standard | Origin | What it computes | Maps to this skill |
|---|---|---|---|
| **CPM** (Critical Path Method) | Kelley & Walker, DuPont/Remington Rand, 1957; codified in PMBOK | The **longest dependency chain** through the DAG — the sequence with zero slack that sets the minimum project duration | The critical path is the chain of `dependsOn` edges no fan-out can shorten. It tells you where extra agents *cannot* help (Amdahl, below) and which barriers (`task-decomposition.md` §4) sit on the true bottleneck vs. off it. |
| **PERT** (Program Evaluation and Review Technique) | US Navy Special Projects Office, 1958 | A **three-point estimate** per task: `(Optimistic + 4×Most-likely + Pessimistic) ÷ 6`, with variance from `(P − O) ÷ 6` | Use it to compute a node's `estTokens` for the ledger (step 8) when cost is uncertain — a single point estimate hides the tail; the PERT weighting and variance flag which nodes to watch for overrun. |

**Slack** (a task's freedom to slip without moving the critical path) is why off-critical-path nodes tolerate the cheaper tier in `model-routing.md`: a node with slack that runs slow doesn't delay the gate, so it rarely justifies a tier bump.

## Hard limits on parallelism — Amdahl's & Brooks's Laws

Two results bound the entire fan-out-and-route strategy. They are the reason `task-decomposition.md` §5 caps fan-out and `model-routing.md` modifier A drops a tier under width — not conservatism, but arithmetic.

- **Amdahl's Law** (Gene Amdahl, AFIPS 1967) — a theorem, not a versioned standard. Speedup from `N` parallel agents is `1 / ((1 − p) + p/N)`, where `p` is the parallelizable fraction. The **serial fraction is the ceiling**: as `N → ∞`, speedup → `1 / (1 − p)`. Every barrier, every `synthesize`/`critic` reconvergence, every gate is serial `(1 − p)` work. Consequence: a DAG that is one wide `scout` fan-out feeding one Opus `synthesize` is capped by that synthesis no matter how wide you fan the scouts — widen the fan-out only up to the point the serial merge dominates, then stop (§5's "don't fan out work you can't reconverge").
- **Brooks's Law** (Fred Brooks, *The Mythical Man-Month*, 1975; Anniversary Ed. 1995) — *"Adding manpower to a late software project makes it later."* Communication and reconvergence overhead grow super-linearly with agent count (∝ n(n−1)/2 pairwise). Consequence: throwing more agents at a **behind** node usually loses, because each added agent adds merge/verify/worktree cost (`isolation: 'worktree'` is exactly this tax, §6). The routing move under width is therefore *down a tier to protect budget* (modifier A), **not** *more agents at the same tier*.

Together they set the fan-out discipline: parallelize the genuinely independent `p` (disjoint scouts, per-item implements), keep the serial `(1 − p)` sharp and few (one decisive `synthesize`, fewer/higher-effort verifiers per H4), and never buy width you then pay to reconverge.

## Edition discipline

Standards drift; a plan that cites a superseded formula reads as sloppy and skews the ledger. Rules:

- **Pin, don't blend.** SAFe **6.0** and PMBOK **7th Edition** are the mapping baseline here. When SAFe 7 or a PMBOK successor lands, update the WSJF axes and the WBS/CPM references **together** — do not order one phase by SAFe 6 WSJF and the next by a newer definition.
- **Laws don't version, methods do.** Amdahl's and Brooks's Laws are mathematical/empirical results and need no edition tracking. WSJF, RICE inputs, MoSCoW, and the PMI standards **do** — re-check them on a yearly cadence, the same discipline `../../loop-review/references/owasp-cwe.md` applies to the OWASP/CWE editions.
- **Prefer the specific standard over a generic instinct.** If you're ranking a backlog, name WSJF or RICE and show the arithmetic; if you're decomposing, invoke the WBS 100% Rule; if you're sizing fan-out, cite Amdahl. A named standard with its formula beats "it felt about right."
