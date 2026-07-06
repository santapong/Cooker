---
name: loop-scout
description: Find existing frameworks, libraries, tools, or standards that already solve a problem before building it from scratch — a prior-art and build-vs-buy check that prevents reinventing the wheel and over-engineering. Use when the user is about to build a component, feature, or system, asks whether to build or adopt an existing solution, wants to avoid reinventing something, asks whether a library or framework exists for a need, or is scoping work that likely has established solutions.
---

# Finding Frameworks

You are about to stop someone from building what already exists. Before any non-trivial thing gets built, this skill runs the reflex a good engineer runs automatically: **does a mature, maintained solution already solve this — and if so, is there a real reason not to use it?** The default answer to "should we build this from scratch?" is *no*. Your job is to find the prior art, evaluate it honestly, and only bless a from-scratch build when reuse genuinely loses.

**The failure mode this skill prevents is over-engineering: building a bespoke solution to a solved problem.** A from-scratch build is a liability you maintain forever; a well-chosen dependency is leverage. Bias toward reuse, and make anyone building from scratch earn it.

## 1. State the need in solution-neutral terms

Describe the *capability* required, not the implementation you already imagined. "I need to schedule recurring jobs" — not "I need to build a cron loop." Over-specified needs are how people rationalize reinvention: every extra bespoke requirement is a reason no existing tool "fits". Separate **must-haves** from **nice-to-haves**, and note hard constraints (language/runtime, license, deployment target, data residency, scale). Keep the nice-to-haves out of the fit test until a candidate has cleared the must-haves.

## 2. Check the boring options first

Search in this order — cheapest, most durable reuse first. Full source list in **`references/where-to-look.md`**.

1. **The standard library and platform you already run on.** The best dependency is no new dependency. Many "we need a library for this" needs are one stdlib call.
2. **Established frameworks/libraries** in your ecosystem's package registry (npm, PyPI, crates.io, Maven, Go modules, …).
3. **Managed / cloud services** for the capability (queues, auth, search, email) — buying the operational burden is often the real win.
4. **Standards and specs** (IETF/W3C/ISO/OpenAPI, file formats, protocols) — implement the standard, don't invent a dialect.
5. **Existing internal code** — the wheel may already be in your own monorepo.

Use the **`loop-research`** skill to run the actual search and verify claims about candidates (adoption, maintenance, security) against real sources rather than memory.

## 3. Evaluate candidates honestly

Score each real candidate against the need using **`references/evaluation-criteria.md`**: fit, maturity (maintenance cadence, releases, community, adoption), license compatibility, security/supply-chain risk, lock-in and exit cost, total cost of ownership, and learning curve. **A candidate that fits the must-haves at acceptable risk beats a hypothetical perfect build.** Do not disqualify a strong option over a nice-to-have it misses — wrap or adapt instead.

## 4. Decide: reuse, adapt, or build

Apply the build-vs-buy decision in **`references/build-vs-buy.md`**. Default to **reuse**; the ladder is *reuse → adapt/wrap → build*, and you only step down a rung with a stated reason:

- **Reuse** when a candidate clears the must-haves at acceptable risk. This is the expected outcome.
- **Adapt / wrap** when a candidate is close but needs a thin integration or extension layer.
- **Build** only when one of these holds: no candidate clears the must-haves; the capability is your product's **core differentiation** (don't outsource your moat); every option carries unacceptable license/security/lock-in cost; or the need is genuinely trivial and a dependency would cost more than the ten lines it replaces.

## 5. Apply the over-engineering guardrails

Even when building is justified, keep it small. From **`references/build-vs-buy.md`**:

- **YAGNI** — build for the requirement in front of you, not the imagined future. Speculative generality is the most common self-inflicted complexity.
- **Prefer boring technology** — proven, widely-used, well-understood beats novel and clever.
- **Never build these yourself**: authentication/session management, cryptography, TLS, date/time handling, and standard-format parsers. The DIY versions are a security and correctness minefield with excellent existing solutions.

## 6. Recommend

Deliver a short, decisive recommendation, not a survey: the pick (reuse X / adapt X / build), the rationale against the must-haves, the **strongest counter-argument named**, and the runner-up so the decision is auditable. When the choice is architecturally significant, record it as an ADR using the **`loop-design`** skill's `templates/adr-template.md`. For a wide search across many candidates, run **`templates/prior-art-search.workflow.js`** to fan out discovery and evaluation in parallel, then synthesize the decision.

## Reference files

- `references/where-to-look.md` — where to find prior art (stdlib/platform first, registries, managed services, standards, internal code) and how to search each
- `references/evaluation-criteria.md` — the candidate scoring rubric: fit, maturity, license, security, lock-in, TCO
- `references/build-vs-buy.md` — the reuse → adapt → build decision, YAGNI, boring-tech, and what never to build yourself
- `references/standards.md` — the authoritative standards this skill applies — named, version-pinned, and mapped to its workflow
- `templates/prior-art-search.workflow.js` — parallel discovery → per-candidate evaluation → build-vs-buy recommendation workflow script
