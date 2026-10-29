# Delivery evidence — 29 October 2026

The kitchen landing is deployed to the existing production project. The isolated health/log example is also deployed. Existing protection settings are unchanged; both production domains are anonymously accessible. The 79-task plan is published on the feature branch; the Cooker-native example remains blocked.

## Published artifacts

| Slice | Actual evidence |
|---|---|
| Landing | [Production](https://cooker-landing-cbf38579.vercel.app), deployment `dpl_6K1cD8yRt7dN8ksZ39KfBVDBVf8F`, READY / production, source `c1a59a95cfff377b72c17f11995f2debc5817a1d` |
| Public export |46 content/config files, 7,575,085 bytes plus manifest. SHA256 `447c5f6916b548c2d457d0c035511e6e1b7d8145b6abf22e903c08f535731654`; local/Pi/preview/production manifests match; corrected hosted JS/CSS bytes matched pins |
| Example | [Deployment](https://cooker-example-service-20261029.vercel.app), `dpl_HvRpQzk9ptLCEywtD32B4hjZR8ET`, READY / production, source `7ac91132cb07916f8d1a65267ee64bc6a84f70b1` |
| Example runtime | Authenticated `/api/health` returned 200 JSON/no-store; structured `health_check` event observed in iad1/Node24, request `h2lfb-1793288613925-605b4932c399`. One handler test passed |
| Git | Signed-off source/plan/example commits pushed to `feature/cooker-release-plan-20261029`; final evidence maintained on that branch. No main/develop merge or tag |

Exact safe receipts: [landing](evidence/vercel-landing.json), [artifact](evidence/landing-artifact.json), [verification](evidence/landing-verification.json), [example](evidence/vercel-example.json).

## Actual landing verification

All **37 distinct checks passed on the corrected source across preserved bounded runs**: real moving pixels, registration, pause/resume, offscreen stopping, reduced motion, both branching examples/keyboard selection, six widths320–1920, mobile navigation/Escape/focus, FAQ, cold reduced-motion, no-WebGL, missing-library and no-JavaScript fallbacks. This was not one uninterrupted full-suite pass.

The first source failed registration after 23 passing assertions. Diagnosis observed stale pixel CSS dimensions after resize; OGL setSize writes these dimensions. The correction restores100% canvas CSS after framebuffer resize, handles window resize and follows the image's native content box. Scheduling of delayed resize callbacks was not isolated.

The corrected full run passed 26 assertions, then Chromium full-page capture failed. Smaller hero/story/workflow/pass captures at 390/1440 succeeded and were visually reviewed. A subsequent run passed the remaining 1920 geometry assertions, then its menu locator failed because the accessible name changes after opening. A stable selector allowed all remaining 9 cases to pass. Both incomplete wrappers and their errors remain retained.

Hosted preview review independently checked six widths, menu focus, n8n prerequisites, FAQ and an empty warning/error log. Production readback verified heading, motion and matching artifact. Production screenshot initially timed out; capture succeeded after pausing motion. Other engines, physical devices, performance and final owner approval of reconstructed B3 outlines remain separate.

## Test ownership and cleanup

All 60 source pins matched. Actual Pi worker controls: CPU 80%, RAM 1GiB, swap0, PIDs 256, deadline 240s. Largest sampled memory peak 839,991,296 bytes. Controller had its own 10%CPU/64MiB/32PID ceiling. Disk stopping threshold 150MiB was soft, not a filesystem quota.

Reservation conflicts executed no browser cases. Executed attempts verified owned cleanup. Final independent cleanup found no active owned units/processes, empty/absent cgroups, port14637 closed, exact staging removed and lane released with verified cleanup. Docker/nginx/tailscaled remained active; n8n readiness returned 200. No production Pi service restart/change or test waiter remains.

## Remaining boundaries

- Protection `all_except_custom_domains` is preserved. Fresh anonymous requests returned 200 with actual landing content and example health JSON; the preview redirected to Vercel sign-in (final 200 login page, no application marker). The earlier setting question was withdrawn: its premise was an overbroad reading of the protection label. No protection or DNS change was needed. See [public-access evidence](evidence/public-access.json).
- Previous production `dpl_8VfFfEC9wnK6WwyXNqBVKSvEYbMX` is retained for rollback; no rollback executed.
- Native Cooker readiness at `127.0.0.1:4644` was refused; retained containers are stopped. No owner database/key/credential changed.
- Source has a dedicated create-only embedded landing publisher, unfinished adopted-project submission and no completed generic Vercel stage. **Direct connector deployments are not Cooker-managed runs.** EXM-02 retains the missing qualified runtime, own permitted connection/binding, durable dispatch and provider/run receipt.
- The 700-path backend candidate was preserved. None of 48 retained full product requirements is completed by this landing/example delivery.

Private controllers, failures, source pins and original screenshots: `/mnt/data/cooker-release-20261029/`. Only reviewed portable summaries/public hashes are committed.
