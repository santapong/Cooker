# Cooker — Marketing & Monetization Research Team

A team of Claude Code subagents that researches Cooker's go-to-market and monetization, then
**discusses, critiques, and refines** until the output converges. Created 2026-06-21.

> The agents themselves live in `.claude/agents/cooker-mkt-*.md`. This folder holds the team's
> **outputs** (created when the team runs) plus this guide.

## Org chart (16 agents)

```
                          cooker-mkt-cmo                         (CMO — top orchestrator)
              ┌───────────────────┴─────────────────────────┐
        channel specialists (4)                      cooker-mkt-monetization-lead
   seo · sem · geo · announce                ┌──────────── 10 analysts ────────────┐
                                       pricing · market-sizing · competitor ·
                                       business-model · segmentation · unit-economics ·
                                       forecast · growth · partnerships · risk
```

| Agent | Role |
|---|---|
| `cooker-mkt-cmo` | Chief Marketing Officer — owns the brief, runs the discussion rounds, writes `PLAN.md`. |
| `cooker-mkt-seo` | Organic search (keywords, `/compare/` pages, technical SEO, backlinks). |
| `cooker-mkt-sem` | Paid search / PPC (a deferred lever, per strategy.md). |
| `cooker-mkt-geo` | Generative Engine Optimization (cited by ChatGPT/Claude/Perplexity/AI Overviews). |
| `cooker-mkt-announce` | Launch & outreach (Show HN, Reddit, Product Hunt, dev.to, podcasts, communities). |
| `cooker-mkt-monetization-lead` | Runs the 10-analyst monetization team; writes `monetization/monetization-plan.md`. |
| `cooker-mkt-pricing` | Tiers, price points, value metric, packaging. |
| `cooker-mkt-market-sizing` | TAM / SAM / SOM. |
| `cooker-mkt-competitor` | How rivals monetize + their price points. |
| `cooker-mkt-business-model` | Open-core vs licensing vs SaaS vs services mix + sequencing. |
| `cooker-mkt-segmentation` | ICP, personas, willingness-to-pay. |
| `cooker-mkt-unit-economics` | LTV / CAC / payback / margin / COGS. |
| `cooker-mkt-forecast` | Bear/base/bull revenue projections. |
| `cooker-mkt-growth` | Funnel (AARRR), free→paid, expansion/NRR. |
| `cooker-mkt-partnerships` | Marketplaces, sponsorships, channels. |
| `cooker-mkt-risk` | Monetization risk, licensing/compliance; red-teams everyone. |

## How to run it

Invoke the CMO:

> Use the `cooker-mkt-cmo` agent to build Cooker's full marketing + monetization plan.

The CMO runs a **4-round discussion loop** (this is the "teamwork" / agents talking to each other):

1. **Brief** — CMO writes `00-brief.md` (shared facts/constraints from the existing docs).
2. **Drafts** — every specialist writes a v1 (in parallel).
3. **Cross-critique** — each specialist reads peers' drafts and writes a `*-critique.md` (conflicts, overlaps, dependencies). *This is where the agents discuss each other's work.*
4. **Refine → Synthesis** — specialists revise to v2; the CMO writes `PLAN.md` with a decision log.

The monetization lead runs the same 4 rounds inside its 10-analyst sub-team and reports up as one voice.

### Quick mode
For a cheaper run, tell the CMO "drafts-only, single round" — it skips the critique/refine rounds.

### Nested-spawning caveat
The two-tier design needs subagents to spawn subagents (CMO → monetization-lead → analysts). If your
environment forbids that, the CMO spawns all 10 analysts **directly** (flat) and the monetization-lead
runs **synthesis-only** over their files. Both coordinator agents document this fallback.

## What it builds on (does NOT reinvent)

- `docs/marketing/strategy.md` — the existing 90-day adoption strategy.
- `docs/launch/01-billing-monetization.md` + `docs/launch/README.md` — billing/licensing/pricing + the tenancy gate.
- `docs/product-plan.md` §7 — the adoption-first monetization ladder + anti-goals.
- `docs/audits/W11-user-journeys.md` — the canonical ICP personas.

## Output map (created when the team runs)

```
docs/marketing/research/
├── 00-brief.md                      (CMO)
├── PLAN.md                          (CMO synthesis + decision log)
├── channels/   seo.md sem.md geo.md announce.md   (+ *-critique.md)
└── monetization/   monetization-plan.md + 10 analyst files   (+ *-critique.md)
```

## Ground rules (inherited from strategy.md)

Honest posture only — no astroturfing, no inflated numbers, no overselling multi-tenancy, keep the
core OSS, no solo-operated paid SaaS before the fix-first + pen-test gate. The agents never edit
product code; they write only under `docs/marketing/research/`.
