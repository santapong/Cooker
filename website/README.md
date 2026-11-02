# Cooker website

The selected B3/K1 kitchen identity, product story, two interactive workflow examples and optional pinned OGL path light. The source also retains separately labelled login/design studies for local review.

## Public build

From the repository root:

```sh
python3 website/export_public.py
python3 -m http.server 4637 --bind 127.0.0.1 --directory website/public-dist
```

The public exporter explicitly allowlists the landing, docs, illustrative workspace, three story images, shared assets and pinned OGL import closure. It excludes login studies, private notes, test/controllers, backups and source maps. It rejects artifacts over100files,4MiB/file or8MiB total and emits a file/artifact hash manifest. The current Vercel root is`website`, build`python3 export_public.py`, output`public-dist`. Run service/browser checks only after resource admission.

`build.py` separately packages the broader local design-review site into`dist`; it is not the public publication command. Both output directories are ignored by Git. Docs and workspace previews use illustrative in-memory state and do not execute builds/deployments or call a Cooker backend.

## Graphics and identity

`assets/cooker-b3.svg` is a manual vector reconstruction of the selected Soft Toque; source reference selection remains separate from final outline acceptance. The kitchen is a fixed1536×1024 plate with three registered visible-route sections, not a reconstructed3D room. `kitchen-motion.js` optionally loads the same-origin OGL1.0.11 closure and animates96 point samples at a requested30fps/DPR1.5 ceiling. Pause, reduced motion and offscreen/failure handling preserve native content. Actual runtime evidence is required before marking the graphics accepted.

Keep the shared charcoal/ember/cream tokens unchanged. Build dependencies, approval, startup readiness and ongoing service health remain separate concepts; the kitchen scenery and light loop are explanatory artwork, not a real deployment status.

## Workflows and delivery

The release example has two check branches and an all-prerequisites join before Push. The n8n example shows four services and five startup requirements; Worker waits for PostgreSQL, Redis and main. Current task/verification/source/deployment state is in the[release plan](../docs/plans/2026-10-29-release-and-service-platform/PLAN.md) and[delivery record](../docs/plans/2026-10-29-release-and-service-platform/DELIVERY.md). No production API, credentials or databases are bundled.
