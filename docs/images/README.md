# Documentation visuals

These illustrations describe the implementation at `6d6ca01`; they are not
screenshots of a live deployment or proof of cloud acceptance. Each SVG is
self-contained, has an accessible title/description, and can be embedded directly
in GitHub Markdown without an external image service.

| Asset | Purpose | Editable source |
|---|---|---|
| [stage-types.svg](stage-types.svg) | Actual six stage symbols, using the product's SVG path geometry | [Python generator](sources/render-ui-diagrams.py) reads [StageSymbol.tsx](../../frontend/src/components/pipeline/StageSymbol.tsx) |
| [compose-workflow.svg](compose-workflow.svg) | The five import/review steps | [Python generator](sources/render-ui-diagrams.py) |
| [github-compose.svg](github-compose.svg) | Compact AWS/GCP architecture for README readability | [Architecture JSON](sources/github-compose.architecture.json) + [Python generator](sources/render-ui-diagrams.py) |
| [github-compose.html](github-compose.html) | Interactive architecture with source links, light/dark modes and exports | [Architecture JSON](sources/github-compose.architecture.json), rendered with Archify 2.17 |

The static SVGs use Cooker's hull/ember palette. The interactive viewer uses
Archify's semantic component palette. Both architecture layouts use the same six
components and five directed relationships. The HTML can be downloaded and
opened locally; GitHub's file viewer does not execute interactive HTML.

## Regenerate the SVGs

From the repository root, using Python 3 and its standard library:

```sh
python3 docs/images/sources/render-ui-diagrams.py
```

Stage geometry comes from the application component rather than a separate icon
copy. The architecture SVG reads the JSON topology and arranges it for Markdown;
it is a separate layout from the interactive viewer's native SVG export.

## Regenerate the interactive architecture

With Archify 2.17 available, set `ARCHIFY_DIR` to its installed directory. Review
all referenced source files and the JSON's pinned repository revision before
regenerating. From the repository root:

```sh
node "$ARCHIFY_DIR/bin/archify.mjs" validate architecture \
  docs/images/sources/github-compose.architecture.json \
  --repo-root "$PWD" --quality showcase --json

node "$ARCHIFY_DIR/bin/archify.mjs" deliver architecture \
  docs/images/sources/github-compose.architecture.json \
  docs/images/github-compose.html \
  --repo-root "$PWD" --quality showcase --json

node "$ARCHIFY_DIR/bin/archify.mjs" visual-check \
  docs/images/github-compose.html --json
```

If Chromium is not found, set `ARCHIFY_CHROME` to its executable path and rerun the
visual check. Do not equate the deterministic validator with browser inspection.
After any source change, regenerate the affected assets, inspect the rendered
output and refresh the verification record. Keep generated screenshot sidecars
in a local review directory unless they are intentionally part of a docs change.

## Verification

[verification.json](verification.json) records specification/artifact SHA-256,
SVG hashes and the checks performed on 7 September 2026:

- Architecture: **9/9** showcase checks, no composition errors or warnings;
  repository references verified at the pinned implementation revision.
- Browser: containment passed at **1440×900, 1600×1000, 1920×1080 and 2048×1320**;
  light/dark captures completed.
- Visual review: light desktop and dark large-display HTML reviewed; all three
  standalone SVGs rendered and inspected. SVG text bounds and XML parsing passed.

These are documentation checks. Application test results and pending account/cloud
acceptance are recorded in the [implementation plan](../plans/2026-09-06-github-compose-deployment.md).

The interactive viewer includes the MIT-licensed Archify runtime by tt-a1i, based
on Cocoon AI. Its notice is preserved in [LICENSE.archify](LICENSE.archify).
