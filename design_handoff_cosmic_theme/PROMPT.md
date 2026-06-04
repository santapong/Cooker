# Claude Code — kickoff prompt

Copy everything in the block below into Claude Code, run from the root of the **Cooker** repo
(with this `design_handoff_cosmic_theme/` folder placed inside the repo, or its path adjusted).

---

```
You are implementing a new visual theme for Cooker — a CI/CD platform with a visual DAG
editor (frontend at `frontend/`, React 18 + TypeScript + Vite + Zustand + React Flow).

Read `design_handoff_cosmic_theme/README.md` in full before writing any code. It is the
source of truth: exact tokens, typography, planet/trajectory specs, per-screen breakdowns,
and a suggested implementation order. The HTML/JSX files in
`design_handoff_cosmic_theme/design-references/` are DESIGN REFERENCES (prototypes), not
code to copy — recreate the system inside Cooker's existing components and patterns.

The theme is "Cosmic / Galaxy". Core metaphor: each pipeline STAGE is a planet, each DAG
EDGE is a trajectory between planets; "running" keeps Cooker's existing ember/gold as
star-fire. Two modes: "Deep Field" (dark, primary) and "Daybreak" (light).

Work in this order and STOP for my review after each step:

1. TOKENS — In `frontend/src/theme/tokens.ts`, add the Deep Field + Daybreak palettes as
   new return values from `cookerTheme(mode)`. Extend the `CookerTheme` interface with the
   additive fields the design needs (violet, cyan, violetGlow, panelGlass, display, plus the
   planet color map). Wire Space Grotesk (display) and the new keyframes (ccTwinkle, ccHalo,
   ccSpin, ccPulse, ccShimmer, ccDash, ccBlink) into `frontend/src/index.css` and the Google
   Fonts import. Don't break the existing `themeMode` contract in `stores/uiStore.ts`.

2. ATOMS — Re-skin `frontend/src/components/ui/atoms.tsx` (Pill, Btn, Card, StatusDot,
   KindBadge→planet orb, PageHeader, inputs) per the spec. Keep component APIs stable.

3. CHROME — Re-skin `frontend/src/components/layout/Sidebar.tsx` and `TopBar.tsx`
   (glass surfaces, ringed-planet brand mark, violet-active nav).

4. PIPELINE (signature surface) — Turn `components/pipeline/nodes/StageNode.tsx` into the
   planet node (orb + atmosphere glow + glowing ports + live ember halo/shimmer) and
   `components/pipeline/edges/ConditionalEdge.tsx` into the trajectory (idle dotted / done
   green / active marching gradient + travelling comet). Add the starfield + dotted-grid
   backdrop to `components/pipeline/PipelineCanvas.tsx`. Edge state derives from the source
   node's run status — no new state.

5. PAGES — Apply the re-skinned tokens/atoms across the remaining pages; most only need the
   atoms + a starfield backdrop. Match the per-screen notes in the README.

6. DAYBREAK + MOTION — Verify the light palette everywhere; gate starfield/comet/halo/shimmer
   behind `@media (prefers-reduced-motion: no-preference)` with the visible end-state as base.

Constraints:
- Reuse existing structure, hooks, and stores — this is a re-skin, not a rewrite.
- No raster assets; all planets/rings/comets/starfield/brand mark are CSS/SVG.
- TypeScript strict; keep `npm run build`, `npm run lint`, and tests green after each step.
- Don't touch backend (`backend/`) or behavior — visuals only.

Start with step 1 and show me the diff before moving on.
```
