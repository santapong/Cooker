# W11 follow-up — findings cross-referenced against backlog (2026-05)

Read-only audit. Cross-references every gap in `docs/audits/W11-user-journeys.md` against `backlog.md` § "Discovered via user-journey W11" to confirm coverage and flag tier drift.

## How to read this doc

`W11 §X step Y` cites the persona walkthrough in `docs/audits/W11-user-journeys.md`. `backlog §...` cites the "Discovered via user-journey W11" section of `backlog.md`. Every W11 finding is accounted for in one of the three tables below. The most interesting result is in §1 (the one P1→P2 demotion) and §3 (nothing new worth promoting from P3 today).

---

## 1. W11 P1 items present in backlog

W11 surfaces five P1-tagged gaps (the doc's own "Verdict" line says four, but the per-persona Gaps sections enumerate five — minor doc drift). The backlog carries four at P1 and one demoted to P2.

| W11 § | Finding | backlog.md anchor | Backlog tier |
|---|---|---|---|
| SaaS §6 + Enterprise §6 | In-product audit-log viewer | "In-product audit-log viewer" | **P1** ✓ |
| Enterprise §4 | Tenant scoping (data-scoped or namespace-scoped) | "Tenant scoping — design-doc gate first" | **P1** ✓ |
| ML §5 | Per-Pipeline / per-App `runDeadline` override | "Per-Pipeline / per-App `runDeadline` override" | **P1** ✓ |
| ML §4 + ML §9 | Build-cache plumbing (`--cache-from` / `--cache-to`, Kaniko `--cache-repo`) | "Build-cache plumbing" | **P1** ✓ |
| ML §6 | Kaniko / Buildah Job `nodeSelector` + `tolerations` | "Kaniko / Buildah Job `nodeSelector` + `tolerations`" | **P2** (demoted) |

**Tier-drift call-out.** W11 ML §6 ships at P1 (engineer cannot pin builds away from GPU nodes; without it, a Kaniko build can starve the GPU pool it is supposed to feed). The backlog lists it at P2. Either (a) the demotion is deliberate because the chart's existing `builder.kaniko.{namespace, serviceAccount}` gives operators a workaround via dedicated namespaces, or (b) it's a transcription slip. **Recommendation:** keep it at P2 for now but mention the workaround inline in the backlog entry, otherwise the next planner round will see "P1 in audit, P2 in backlog" and re-litigate it.

---

## 2. W11 P1 items missing from backlog

**None.** All five W11 P1-tagged gaps are in `backlog.md` (four at P1, one demoted to P2 as flagged above). No new entries to add to this table.

---

## 3. W11 P2 / P3 items missing from backlog and worth adding

A full diff of W11's 13 P2 + 14 P3 gaps against the backlog shows every one is already represented under `## Discovered via user-journey W11`. There are no missing P2 or P3 items to add.

Items deliberately not re-listed (already in backlog):

- P2: empty-state CTAs, build-recipe auto-detect, webhook URL surfacing, deployed URL surfacing, GitHub org bulk import, secret diff view, MFA approver pre-warning, production-readiness checklist, per-team RBAC, secrets-backend connectivity test, deployed-cluster surfacing, append-only audit-log adapter, deploy-target `NodeSelector` / `Tolerations`.
- P3: bundled k3s easy-button, PR-preview environments, bulk webhook-secret rotation, `groupRoleMap` schema validation, "make-pipeline-for-app" button, SAML, `/me/admins` dashboard, `/health/ready` rate-limit doc note, ML stage type, GitHub→PVC staging doc.

**Promotion candidate worth a re-look (not an add).** W11's "Cross-persona patterns" section flags **first-run / empty-state onboarding** as one of the five highest-leverage candidates because it touches three personas (Indie, SaaS, Enterprise). The backlog currently has the two pieces of it (empty-state CTAs; production-readiness checklist) sitting at P2 each. Both are single-persona-tagged in their backlog entries even though the audit's pattern section pitches them as cross-persona. Consider grouping them under a single P1 "First-run experience" rollup in the next planning round, or at minimum cross-link them.

---

## Verdict

Coverage is good. Backlog represents 31/31 W11 gaps. The two artefacts worth a one-line touch in `backlog.md` on the next planning round:

1. Note the ML §6 P1→P2 rationale (workaround via dedicated namespace) so future planners don't re-open it.
2. Cross-link the two "first-run experience" P2 items (empty-state CTAs + production-readiness checklist) and consider rolling them up.

No code change implied. No new backlog entries required.

---

## Cross-references

- Source audit: `docs/audits/W11-user-journeys.md`
- Backlog: `backlog.md` § "Discovered via user-journey W11"
- Format precedent: `docs/audits/launch-readiness.md`, `docs/audits/chain-recheck.md`
