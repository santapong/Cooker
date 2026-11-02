# Cooker login and workspace integration

Implemented locally on 1 October 2026 after the owner requested implementing
the existing drafts. Split is the default implementation because it was the
currently reviewed login; the alternative prototypes remain references. This
does not record an exclusive owner selection of a login variant.

The candidate is `Cooker-backend`, on `feature/backend-automation-20260928`.
The original `Cooker` checkout remains clean at `949d4a9` on develop. Existing
backend roadmap changes in the candidate were preserved. Nothing was committed,
merged, pushed, or released by this integration.

## Delivered behavior

- Real sign-in, sign-up, and callback screens use the Split family: bracket
  wordmark, charcoal/ember, illustrative Build → Review → Deploy story, password
  visibility, honest help, server method discovery, and retry. Embedded app-import
  wizards keep their existing layout and behavior. Local auth/OIDC/session/MFA
  contracts remain; no provider buttons or password-reset service were invented.
- Labelled navigation, mobile drawer, keyboard page search, skip link, Calm
  control, persisted charcoal/Paper themes, role-aware management navigation,
  and common panel/form styling are integrated in the real shell.
- Overview reads app and pipeline records. App cards retain direct app,
  deployment, and editor links, with search and recorded-health filters. Unknown
  health is not called healthy or undeployed. Legacy autoDeploy is not used as
  the summary of independent trigger rules.
- Runs explicitly selects a pipeline and reads its existing run endpoint. Both
  lists have a stated 100-record scope. Errors are visible and retryable, including
  a missing deep-linked pipeline during a failed list request.
- Documentation provides seven task-focused guides and optional developer
  details. Selecting a topic moves keyboard focus to its article heading.
  Provider availability, readiness, logs, publication, and domain limitations
  are described separately from illustrative examples.
- The editable DAG has rectangular stage cards, separate arrow endpoints,
  numbered selectable explanations, explicit edge-condition editing, join
  explanations, a Step list, Find a step, and fit that includes numbered hit
  areas. Real IDs, save/version behavior, validation, and run endpoints remain.
  Admin/operator editing is preserved; viewers can inspect. Existing Run/Compose
  graph rendering remains in its original mode.
- The SPA favicon uses a Vite-managed asset under the backend's real `/assets/`
  route. The former root favicon URL returned the SPA HTML fallback. The current
  asset keeps the C and brackets in Cooker charcoal/ember.

## Verification and local runtime

231 unit tests in 27 files passed. TypeScript and production builds passed;
scoped lint and whitespace checks passed. Browser fixtures were added/updated,
but the Playwright CLI suite was not executed. Visible CUA checks separately
exercised synthetic login failures/discovery retry, disabled signup, viewer and
operator access, app search, dependency selection/condition save with retained
IDs, run-history retry, mobile navigation/search/Escape, documentation and themes.
No real deployment was created by these browser fixtures.

Login and n8n checks covered 320,390,768,1024,1440,1920 widths. Separate 12-step/
8-edge and 24-step/20-edge verification fixtures covered 320 and1440. Measured
viewports had no whole-page/main overflow; numbered dependency targets remained
44px and inside the fitted map. Fit is for overview; Step list and Find a step
are reading alternatives at large graph sizes. These checks are not common-user
comprehension evidence, full Axe certification, real cloud UAT, or cross-browser
certification.

The frontend was activated on the existing localhost preview by copying hashed
assets first and replacing index.html last, retaining old assets for open tabs.
All65 final frontend files matched their active HTTP bytes; no source maps ship.
The later local runtime update activated the previously tested static seven-file
landing-only binary. Active API: `cooker-vercel-preview-api-integrated-20261001`
at127.0.0.1:4644. Exact auth environment, database container, network, and private
keys were preserved. No active jobs/publications existed; aggregate counts were
unchanged afterward, and the actual signed-in account still opened Settings/Cloud.
The previous API container and frontend backup remain available for rollback.

Cloud now reports the exact7-file,1,364,907-byte landing artifact. All7 public
files match, excluded preview/private paths404, and unauthenticated publication
history401. No Vercel account is connected and no publication has been requested.
Connecting the intended Vercel account requires human secure credential entry;
the public deployment remains pending. The full31-file design preview at4646
and its original source remain separate and unchanged.

Detailed evidence, screenshots, rollback, and validation logs:
`/mnt/data/cooker-design-integration-20261001/README.md`.
