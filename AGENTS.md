# Cooker workspace instructions

- Canonical project checkout: this `company/apps/Cooker` directory. The owner requested consolidation on 2 November 2026. Continue ordinary frontend/backend/docs work here; do not create sibling Cooker copies or Git worktrees unless the owner explicitly requests one.
- Use `develop` for integration and `main` for releases. Use plain Git and signed-off commits. Never use `gh`. A develop source integration does not establish release or live-provider acceptance.
- Preserve the kitchen website and the application's accepted auth/DAG behavior. Keep the remaining roadmap gates explicit; source, fixture tests, live acceptance and release are separate states.
- Read the relevant Cooker maintenance skill and current project checkpoint before complex work. Ignore bundled `.claude` agents, skills and workflows; do not delete them.
- Put local consolidation/recovery receipts under `.git/local-recovery/`; do not commit credentials, environment files, generated reports or dependency trees.
- DATA can be mounted `noexec`. Native toolchains/dependency caches may live under the user's cache directory and be referenced from this checkout; they are build support, not another source checkout. Keep test execution within the current resource budget.
