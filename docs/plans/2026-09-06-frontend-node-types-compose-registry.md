# Frontend: readable node types and a Compose registry

**Status: READY FOR UAT — acceptance pending.** Marked for UAT at Santapong's
request on 6 September 2026. This status covers the local frontend candidate;
it does not mark the shared registry or live cloud deployment as accepted.

Source: Santapong's 6 September 2026 frontend review. Baseline inspected:
clean `develop` at `fb6faaa`, matching the local origin ref; no fresh fetch.
Keep the existing dark Porthole theme and amber/semantic status palette.

## Problem

Every pipeline type renders the same six-pixel dot. Type is only available in
a hover title or inspector, while the subtitle often shows config or timing.
Compose provides one filename input with no saved stack inventory. Its heading
follows the input even before that file has loaded, and failed parses are hidden
when an older graph exists. The existing Registry page manages image repositories
and registry connections; it does not catalogue Compose stacks.

## First delivery: frontend improvements

1. **Distinct stages.** Build = cube, Test = flask, Push = upload, Deploy = rocket,
   Approval = diamond/check, Custom = terminal. Preserve GitOps' branch symbol on
   existing graphs. Put a visible type label under each node name. Use the same
   symbols in the tray and inspectors; editor, run and deployment views share the
   renderer. Execution status retains its colour and dot. Keep the 48px node
   footprint so saved coordinates, connections and React Flow measurement survive.
2. **Compose stack library.** Add a sidebar with named saved stacks, search,
   filename, saved service/network/volume counts, open and forget actions. Save
   metadata in browser storage scoped to the API origin. Never persist the graph,
   environment values or Compose YAML. Reopening always parses the current file.
   Saving the same filename updates its entry and name. The UI explicitly says
   "this browser" and links to the existing image registries page.
3. **Reliable stack switching.** Bind the map title and service updates to the
   successfully loaded filename. Show parse errors beside the retained graph;
   clear selection on a successful switch, remount the canvas for the new file,
   and ignore outdated requests. File opening is disabled during a service save.
   Refit the map when the canvas resizes and provide a keyboard-accessible Fit map
   control to recover from manual zoom/pan.
4. **Verification.** Build, lint and unit checks; browser acceptance of all stage
   types, save/reload/search/reopen, wrong-file prevention, storage errors,
   accessibility and the existing motion/Compose editing regressions. Inspect
   desktop and narrow layouts with representative sample data.

Interpretation: the user's registry wording was incomplete. A saved file library
is the bounded first step chosen while requesting clarification. It is not a
shared registry, remote file importer or Docker Hub integration. The backend only
accepts bare filenames within its configured Compose directory; preserve that
boundary. No new dependency, backend migration or deployment is part of this step.

## Next delivery: GitHub discovery and deployment preview

Santapong's follow-up clarifies the next experience: connect GitHub, pick a Compose
file, render it and its Dockerfile builds, assign a prefix, and select compute and
external database destinations. The
[capability audit and implementation plan](2026-09-06-github-compose-deployment.md)
records the baseline gaps, implementation status and acceptance criteria. This
GitHub-first flow is now implemented locally, including a server-backed GitHub
stacks catalogue. Human and live cloud acceptance remain pending; OCI distribution
and immutable revision history are still future work.

## Later delivery: a shared, versioned Compose registry

Docker supports Compose distribution as OCI artifacts using `docker compose
publish` and `oci://` references (Compose 2.34.0+ according to
[Docker's official documentation](https://docs.docker.com/compose/how-tos/oci-artifact/),
checked 6 September 2026). Build Cooker on that format for interoperable packages.

1. Add a server-backed stack resource with ownership, name, description, source
   kind (local/Git/OCI), source reference, immutable revision/digest, parsed summary
   and validation status. Reuse Cooker's authentication, tenancy and registry
   credentials. Preserve authorization when resolving every source and revision.
2. Add registry browse/search, version history and a map preview for a selected
   revision. Keep imported data separate from an editable draft; expose the source
   and revision next to the map. An import/preview must not execute the stack.
3. Add explicit import/publish workflows, backend reference resolution and package
   validation. Handle variables/secrets, unavailable sources and unsupported local
   resources. Docker's documented publishing limitations include bind mounts and
   services with only build definitions; surface these during validation.
4. Connect a reviewed revision to Cooker's existing deployment workflow, with
   target/environment selection and its existing approvals and run evidence.
5. Optionally add curated stack templates once versioned import and preview work.
   Catalog entries should carry provenance, required inputs and supported versions.

Acceptance for the shared registry: another authorized browser can find the same
stack and version; a digest resolves reproducibly; denied sources stay denied;
preview has no execution effects; imports and deployments report actual outcomes.

## UAT acceptance checklist

Owner: Santapong. All items below are pending human UAT; automated evidence is
recorded separately. Use disposable Compose files with the real local backend
for persistence checks.

- [ ] Identify every pipeline stage type at a glance in editor and run views.
- [ ] Add, rename, move and connect stages; save/reopen preserves the graph.
- [ ] Open two real Compose files, name and save both, search, reload the page,
      and reopen each saved stack.
- [ ] Editing image/ports/environment updates only the selected service in the
      loaded file; reopen confirms persistence and stable service positions.
- [ ] A missing file shows an error while the previous map/title remain accurate.
- [ ] Rename/forget a library entry; forgetting does not modify the source file.
- [ ] Check desktop and narrow screens, keyboard selection, Fit map and Calm mode.
- [ ] Accept the library's current scope: file references saved in this browser;
      GitHub discovery now has a separate implementation/UAT guide; live cloud
      acceptance and OCI distribution remain separate work.

UAT result: **pending**. Release/deployment status: **not performed**.

## Evidence

Implemented on 6 September 2026, on top of `fb6faaa`, without deploying.
First-delivery items above are implemented and checked.
The current GitHub stack catalogue is implemented. OCI distribution and immutable
revision history remain proposals.

- `npm test`: **178 passed**, including six library persistence/error checks and
  three file-identity/out-of-order response checks.
- `npm run test:e2e -- --workers=2`: **26 passed** after the responsive library
  fix, including existing reduced motion, Calm mode, focus, accessibility and
  service-edit regression checks.
- After the final Compose-only canvas sizing/Fit map changes,
  `npm run test:e2e -- --workers=2 e2e/compose.spec.ts e2e/node-types-library.spec.ts e2e/a11y.spec.ts`:
  **12 passed**. The runner built the final production bundle before testing.
- `npm run lint`: **0 errors**, the same five existing refresh warnings in
  OIDCProvider and PipelinesPage. `git diff --check`: clean.
- Visual review: all six node symbols on the editor and run views; populated
  library and error recovery at 1440px; narrow layout and fitted nodes at 760px;
  visible Brave browser inspection with sample data on localhost:4621.

Mocked UI checks prove frontend behaviour only; the local Cooker backend was not
running. No live Compose file was modified. Browser previews are explicitly
labelled sample data. Re-run the existing Compose edit acceptance against the
real backend during UAT before making a production release claim.

## Review lessons for the next frontend pass

Observed and corrected on 6 September 2026, scoped to this checkout:

- A passing `toBeVisible()` assertion did not catch a shrinking flex container
  clipping the saved cards and overlapping the map header. At the narrow breakpoint,
  the library/list need intrinsic height (`flex: none`). Keep bounding-box
  containment and sibling non-overlap checks alongside screenshot review.
- Direct Compose navigation did not load RunView's stylesheet. A shared class
  name (`run-canvas`) therefore had zero layout height despite rendering a nested
  absolute canvas. Compose now defines its own containing block; the regression
  checks its height after a direct visit.
- For registry switching, the successfully loaded filename is the source of truth;
  the editable filename input is only a request. Retain that identity after errors,
  and reject late read/write responses after a new file request.
- In this managed environment, browser servers/tests failed with loopback `EPERM`
  inside the sandbox. Re-running with approved network access succeeded; this was
  an environment failure, not evidence of a frontend defect. The standard test
  runner owns port 4620 and uses mocked APIs. Use another port for a review preview.

These are repository-local review notes, not new global policy or authorization.
