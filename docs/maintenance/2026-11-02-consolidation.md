# Cooker source consolidation — 2 November 2026

The owner requested one project checkout in `company/apps/Cooker`, integration into `develop`, remote publication and removal of the extra Cooker worktrees. This is a development integration, not a main release or deployment.

The combined source retains the published kitchen website, the backend automation/service workspace candidate, the committed security/dependency changes and the n8n queue Compose/DAG example. Original branch tips, source manifests, source archives and indexes are retained locally under `.git/local-recovery/20261102-consolidation/`. Topic branches remain available after checkout removal.

## Integration corrections

- Wrap the two Vercel connection test tables so each callback receives the connection array.
- Use the supported React Router 7 StaticRouter export in the authenticated-attempts test.
- Preserve scoped BuildKit registry authentication, TLS and OCI handoff while adopting the updated BuildKit filesystem API. Restore the separate Docker/BuildKit tests after their automatic merge overlapped, and update the OCI fixture to provide a real temporary context.
- Extend the current-production Vercel fixture with the new metadata-read endpoint and its actual array response. Production identity/Git-source assertions are retained.
- Serialize shared-database CI tests to preserve the legacy queue table boundary.
- Retain only exact Gitleaks fingerprints for three prose-only acceptance-document matches; no broad new secret rule is disabled.

## Validation

The first combined backend run compiled and recorded 3,356 passing test/subtest events, 28 skips and one failing Vercel fixture. The Vercel package passed after the fixture correction; the affected final fixture recheck also passed. Go vet passed. Database/live Docker/provider opt-ins were not enabled.

Frontend TypeScript passed after the Router import correction. ESLint passed with 11 existing warnings; Vite production bundling passed. The first unit run passed 53 files / 518 tests and could not load the Router-dependent file; the corrected file then passed all five tests. No browser campaign is implied by these unit/build checks. Seven security-script tests, workflow validation, and redacted working-tree/history secret scans passed. Detailed logs and bounded-executor receipts are retained locally.

Full SQL16/HTTP6, combined authenticated Git/provider/runtime acceptance, live Vercel/Neon authority and release acceptance remain open. Historical evidence remains limited to its original inputs. The existing 79-task roadmap retains those requirements.

The normal `npm run build` passed after the final TypeScript correction. No new platform acceptance is inferred from this integration.

## Workspace convention

Continue in this Cooker folder. Keep backend, frontend, website and roadmap together. Local recovery data stays inside Git administration rather than in additional visible project folders. Native caches may be outside DATA when its noexec mount prevents running tool binaries. No production services, keys, domains, main branch or deployed artifacts are changed by this consolidation.
