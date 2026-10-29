# Delivery evidence — 29 October 2026

This file is updated from actual receipts. Preparation and attempted execution are not successful publication.

## Current observations

- Existing Vercel landing project identified and team access renewed by the owner. Existing project/deployment details read back successfully.
- Earlier production deployment: `dpl_8VfFfEC9wnK6WwyXNqBVKSvEYbMX`. Keep as a rollback candidate until the new result is verified.
- Existing project uses Vercel sign-in protection (`all_except_custom_domains`). This release preserves it; public access/domain assignment requires its explicit final decision.
- Native Cooker readiness attempt at`http://127.0.0.1:4644/health/ready` returned connection refused. Retained local Cooker containers are stopped. No credentials or owner database were changed.
- Source inspection: dedicated landing creation exists; adopted-project update review has no submission/job path, and generic Vercel pipeline deployment remains an open requirement. A native Cooker-managed example is not claimed.
- The first bounded Pi browser run passed23 of24 assertions, including actual OGL pixels, pause/resume, reduced motion, offscreen stopping and workflow checks. It failed registration at1024px. Actual1GiB RAM/80%CPU/swap0/256PID caps were read back; sampled peak~640MiB. Owned process/cgroup/port cleanup verified. A diagnostic attempt did not execute because another project held the shared lane; no competing job was started. Registration and remaining browser checks are still open.
- Signed-off plan/example commit`7ac91132cb07916f8d1a65267ee64bc6a84f70b1` was pushed to`feature/cooker-release-plan-20261029`.
- Direct Vercel example deployment`dpl_HvRpQzk9ptLCEywtD32B4hjZR8ET` reachedREADY from that exact commit, on project`prj_7l0MbrL1FmW3rw7xKFi3WtXUWAGt`. Authenticated GET`/api/health` returned200 JSON and`Cache-Control:no-store`; the exact request produced a structured`health_check` runtime event in`iad1`/Node24. This is direct provider acceptance, not a Cooker-managed deployment. Existing sign-in protection remains.
- Landing source, responsive registration correction and new landing deployment remain in progress.

Private run logs, screenshots, input pins and resource/cleanup receipts are retained locally under the29October delivery evidence directory. Only reviewed portable summaries and public artifacts are committed here.
