# Evaluating a Candidate

Score each real candidate against the need on these axes. The point is an honest, comparable read — not a spreadsheet for its own sake. A candidate that clears the must-haves at acceptable risk wins; do not reject a strong option over a nice-to-have it misses (wrap or adapt instead).

## The axes

1. **Fit** — does it cover the *must-haves* (not the nice-to-haves)? How much glue code or configuration is needed to make it fit? A candidate needing a thin adapter still beats a from-scratch build.

2. **Maturity** — is it alive and trusted? Read real signals:
   - Last release / commit recency and cadence (steady beats a burst two years ago).
   - Open-to-closed issue ratio and whether maintainers respond.
   - Number of maintainers / contributors (bus factor) and any corporate or foundation sponsor.
   - Adoption: *dependents* (who builds on it) and download trends over raw star count.
   - Documentation quality and a changelog that signals stability.

3. **License compatibility** — is the license compatible with your use and distribution? Permissive (MIT, Apache-2.0, BSD) is low-friction; copyleft (GPL/AGPL) has obligations that may be fine for internal use but not for shipped/proprietary products. AGPL for a network service is a common trap. Confirm, don't assume.

4. **Security & supply chain** — CVE/advisory history and *how fast* issues were patched; transitive dependency count and depth (each is attack surface and upgrade cost); provenance and signing; whether it's a single-maintainer package handling sensitive work. Prefer well-audited options for anything security-sensitive.

5. **Lock-in & exit cost** — how hard is it to leave later? Does it own your data model, config, or deployment? Is there a standard interface or a second implementation, or is it a one-way door? Managed services trade operational relief for lock-in — price that in.

6. **Total cost of ownership (TCO)** — integration effort + ongoing operation + upgrade/maintenance burden + learning curve, over the project's life. A "free" library with heavy upgrade churn can cost more than a paid service. Compare against the honest, fully-loaded cost of building and maintaining it yourself (which is almost always underestimated).

7. **Performance & scale fit** — does it meet the actual throughput/latency/footprint the need requires? Verify against the requirement, not against benchmarks for a different workload.

## A quick scoring rubric

For each candidate, rate the axes and reduce to a verdict:

- **Reuse** — clears all must-haves; maturity, license, and security acceptable; lock-in/TCO tolerable.
- **Adapt / wrap** — close fit but needs a thin integration or extension layer.
- **Reject** — fails a must-have, or carries an unacceptable license/security/lock-in cost with no mitigation.

Keep the top two or three candidates with their verdicts and the one line that decides each. That comparison *is* the evidence for the build-vs-buy call in `build-vs-buy.md`. When claims about maturity, adoption, or security need to be trustworthy, verify them with the `loop-research` skill against primary sources (the repository, the registry, security advisories).
