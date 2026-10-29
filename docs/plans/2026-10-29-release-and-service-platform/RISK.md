# Scoped release risk record

29 October2026. Scope: the static kitchen site and isolated Node health/log example, not the unmerged Cooker control plane.

| Area | Evidence / response | Remaining boundary |
|---|---|---|
| Public artifact | Explicit exporter:46 content/config files,7,574,862bytes on the first tested input; no login studies/private notes/controller/backups/source maps. Linked docs/workspace are labelled local examples | Recompute after fixes and verify final deployment; manifest alone is not browser acceptance |
| Third-party code | Same-origin OGL1.0.11 import closure, exact16 source hashes and Unlicense retained.29October OSV version lookup returned0 advisory matches | Version lookup is not comprehensive source/OS/runtime analysis or a security certification |
| Browser trust | Self-origin CSP, no object content or form submission, nosniff/referrer/permissions headers | Existing Vercel sign-in protection remains. Public access requires its explicit final decision |
| Example API | Dependency-free5s health Function; handler test passed, actual200/no-store and deployment-scoped structured log observed | Direct Vercel deployment is not a Cooker-managed run; no durable data or sandbox provisioned |
| Motion/layout | Actual OGL pixels, pause/resume/reduced/offscreen and workflow checks passed in the first run |1024px registration failed; diagnose before production promotion. No performance/hidden-tab/comprehension claim from frame counters |
| Test isolation | Actual work caps80%CPU,1GiB RAM,swap0,256PIDs read back; peak~640MiB and cleanup verified | Diagnostic execution waits for the shared lane; no competing workload or production-service changes |
| Source publication | Signed-off feature commits, curated frontend/example/plan paths; unrelated700-path backend candidate untouched | Narrow staged private-key/token pattern scan is not a complete secret scanner |

The vendored upstream Renderer source has two whitespace-only blank lines; preserve upstream bytes/hashes rather than silently modifying the pinned library. Whitespace checks apply to owned source; that vendor-only exception is not a behavioral or security test exception.

Recommendation: keep landing production promotion pending the layout check and intended access decision. The small example has verified direct-provider health/log behavior. Full Cooker product/release acceptance remains open in the task register.
