# Build vs. Buy — and Not Over-Building

The decision after the search: reuse what exists, adapt something close, or build from scratch. The default is reuse, and building must be *earned*. This file is the anti-over-engineering core of the skill.

## The decision ladder

Walk it top-down; take the first rung that holds, and state why you stopped there.

1. **Reuse** — a candidate clears the must-haves at acceptable risk (maturity, license, security, lock-in, TCO). **This is the expected outcome.** Adopt it, wire it in, move on.
2. **Adapt / wrap** — a strong candidate is close but not exact. Wrap it behind a thin interface, extend it, or contribute the missing piece upstream. Still far cheaper than owning a full implementation.
3. **Build** — justified only when a rung above genuinely fails (next section).

## When building from scratch is actually justified

Only when one of these is true:

- **No candidate clears the must-haves.** You searched the stdlib, registries, services, and standards (see `where-to-look.md`) and nothing real fits.
- **It is your core differentiation.** The capability *is* your product's value — the thing customers pay for. Don't outsource your moat. (But be honest: your auth system, your job queue, and your logging are almost never your moat.)
- **Every option carries unacceptable cost** — a license you can't ship, a security/supply-chain risk you can't accept, or lock-in you can't tolerate, with no viable mitigation.
- **The need is genuinely trivial.** Ten obvious lines beat pulling in a transitive dependency tree. (The classic cautionary tale: a one-line left-pad becoming a critical dependency.)

If none of these holds, you are about to over-build. Go back to the ladder.

## Over-engineering guardrails (even when you do build)

- **YAGNI — "You Aren't Gonna Need It."** Build for the requirement in front of you, not the imagined future. Speculative generality — the config option no one asked for, the plugin system for a single use case, the abstraction over one implementation — is the most common self-inflicted complexity.
- **Prefer boring technology.** Proven, widely-used, well-understood beats novel and clever. Boring tech has known failure modes, real documentation, and people who've hit your problem before. Novelty is a cost you pay in surprises.
- **Rule of three before abstracting.** Don't extract a framework from one or two cases; wait until a real third case shows you the actual shape. Premature abstraction is harder to undo than duplication.
- **Smallest thing that works.** Ship the simplest correct solution; add structure only when a concrete requirement demands it.

## Never build these yourself

These are solved problems where the DIY version is a security or correctness hazard with excellent, battle-tested existing solutions. Reusing here is not laziness — it is correctness:

- **Authentication, authorization, session management** — use an established identity provider or library (OAuth2/OIDC).
- **Cryptography and TLS** — use vetted libraries; never hand-roll crypto or a protocol implementation.
- **Date, time, and timezone handling** — use the platform/standard library; the edge cases will defeat a bespoke version.
- **Standard-format parsers and serializers** (JSON, CSV, XML, URLs, email addresses) — the corner cases are endless; use a maintained parser.
- **Password hashing** — use a purpose-built algorithm/library (argon2/bcrypt/scrypt), never a raw hash.

## Recording the decision

State the pick, the rationale against the must-haves, the **strongest counter-argument named**, and the runner-up. When the choice is architecturally significant or hard to reverse, capture it as an ADR (see the `loop-design` skill's `templates/adr-template.md`) so the reasoning survives.
