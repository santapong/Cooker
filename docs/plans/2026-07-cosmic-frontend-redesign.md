# Cosmic Frontend Redesign — "Porthole" Design & Motion Spec

Status: DESIGN — approved surface: all 22 pages. Implementation phased (see §8).
Decision 4 Sep 2026: **brand-recolored + dark-only.** Palette below replaces the
original navy/nebula/violet set to match the brand rule in
`docs/marketing/strategy.md` §2 (greyscale + one warm amber accent, no decorative
gradients, lowercase mono wordmark). Layout, motion budgets, substitution table and
phases are unchanged. Light mode is deferred (`uiStore.themeMode` keeps its type;
no light toggle is wired). P1 landed the same day; P2 (porthole editor) landed 5 Sep 2026 —
`components/porthole/` (Porthole, Starfield, StarNode, ConstellationEdge) + `components/pipeline/`
(PipelineCanvas, StageTray, StageInspector). The list→porthole shared-element transition
(rung 3) waits for the P4 list thumbnails; P2 opens the porthole with a 320 ms CSS scale/fade.
Authored 28 Jul 2026 against the post-reset stubs (PR #150 / Phase 2 reset).
Stack it lands on: React 18 + Vite, react-router 6, zustand, `@xyflow/react` 12,
plain CSS in `frontend/src/index.css` (no CSS framework, no motion library — by design, see §4).

Visual companion: `cosmic-porthole-mockup.html` (this directory) — a
self-contained mockup of the RunPage porthole; open in a browser for the live
draw-in, comet, twinkle and drift (append `?static` for the resting state).

## 1. Concept

The app is a spacecraft flight deck. Chrome is matte hull; instruments are
minimal — hairlines, small-caps labels, generous negative space. Every DAG
view is a **porthole**: a window into space where pipeline stages are **stars**,
edges are **constellation lines**, and a run is **light traveling star to star**.
Opening a pipeline feels like stepping up to the window: the UI recedes,
the void comes forward.

Minimalism rule: prefer spacing and 1px hairlines over boxes and borders.
If a container can be removed without losing scanability, remove it.

## 2. Design tokens (`index.css` custom properties)

```css
:root {
  /* surfaces */
  --void:    #050506;   /* porthole space */
  --hull-0:  #0b0b0c;   /* app background */
  --hull-1:  #141416;   /* raised panel / card */
  --line:    rgba(255,255,255,.10);  /* hairlines */
  /* ink */
  --ink-1:   #f2f2f0;   --ink-2: #a3a39c;   --ink-3: #66665f;
  /* star states */
  --star-idle:    #d6d6d0;
  --star-running: #E07A1F;  /* ember — pulses; same hue as the accent */
  --star-ok:      #7ee2a8;  /* semantic only */
  --star-fail:    #ff6b7a;  /* semantic only */
  --star-queued:  #66665f;
  /* accent — the ONLY hue outside greyscale + semantic status */
  --ember:   #E07A1F;   /* links, focus ring, active rail dot, running star, comet */
}
```

Focus ring: `outline: 2px solid var(--ember); outline-offset: 2px` — never removed.

Gradient rule: the brand forbids decorative gradients. The one exception is the
pre-rendered radial-gradient **halo sprite** behind a star (a lighting effect,
animated by opacity/scale only). Nothing else — no gradient buttons, headers or text.

**Type.** Self-hosted woff2 (no CDN): Space Grotesk (headings, nav, star labels;
OFL), Inter var (body; OFL), JetBrains Mono (IDs, durations, logs; OFL).
Modular scale ×1.25 off 15px body: `12 / 15 / 19 / 24 / 30 / 38`.
Small-caps instrument labels (`STAGES`, `TELEMETRY`): 12px, `letter-spacing: .08em`,
`text-transform: uppercase`, color `--ink-3`.

**Space.** 4px grid. Page padding 32px, section gap 24px, row height 48px.
Density low: hairline row separators, no zebra, no cell borders.

## 3. Motion system — budgets first

| Motion | Budget | Curve | Rung |
|---|---|---|---|
| Hover / focus / press / toggle | 80–120 ms | `cubic-bezier(.2,0,0,1)` | 1 (transition) |
| Panel / inspector / dialog | 200 ms in / 160 ms out | decel in, accel out | 1 |
| Route → porthole open | **≤ 400 ms total** | 320 ms decel in / 240 ms accel out | 3 (View Transition) |
| Constellation draw-in | scene ≤ 600 ms **settled**: 60 ms lead, stars pop 280 ms on a `min(30ms, 240/N)` stagger, edges draw 280 ms from 120 ms (+30 ms each, last start ≤ 320 ms) | decel | 2 (`@keyframes`) |
| Run comet (edge traversal) | 1200 ms / edge, loops while running | ease-in-out | 2 (`offset-path`) |
| Active-star halo pulse | 1600 ms cycle, opacity .6↔1, halo scale 1↔1.15 | sine-ish | 2 |
| Ambient starfield drift | 240 s / 360 s loops, 2 parallax layers | linear | 2 |
| Star twinkle | sparse subset, **period ≥ 7 s each**, opacity .35↔.8 | ease-in-out | 2 |
| Node drag settle | ≤ 160 ms decel (drag itself is 1:1 pointer, unanimated) | decel | 5 (WAAPI) |
| Skeleton star chart | appears at 120 ms if pending; matches final layout | — | 1 |

Asymmetry rule everywhere: decelerate on entry, accelerate on exit.
Stagger is a *scene* budget, not per-item — 20 stages must still settle by 600 ms
(P2 reconciled the numbers: the original `360/N` span plus a 320 ms pop overran the budget).
(100 ms direct-response and 400 ms Doherty ceilings are research-backed
heuristics, not W3C criteria.)

**Compositor discipline.** Animate `transform` and `opacity` only. Glows are
pre-rendered radial-gradient sprite elements animated by opacity/scale — never
animated `filter`/`box-shadow`. Layout changes (inspector open, console
expand) ride View Transitions or FLIP. `will-change` applied just before
porthole open, removed on `transitionend`/`finish`.

### Reduced motion — every branch substitutes, never just deletes

```css
@media (prefers-reduced-motion: reduce) { /* + [data-calm="true"] override */ }
```

| Full motion | Reduced substitute |
|---|---|
| Porthole shared-element flight | 160 ms opacity cross-fade |
| Constellation dash draw-in | 160 ms opacity fade of edges |
| Run comet traveling dot | static brighter stroke on active edge + active-star **opacity-only** pulse (2 s) |
| Starfield drift + twinkle | static starfield (stars remain, motion off) |
| Drag settle | instant snap |

Ambient/interaction-triggered motion (drift, parallax) is **off by default**
under reduced motion. **Calm mode**: a porthole-toolbar toggle that applies the
same substitutions plus pauses the comet — satisfies WCAG 2.2.2 pause/stop/hide
for the looping run indicator.

**Flash safety (SC 2.3.1, non-negotiable):** twinkle periods ≥ 7 s, phase-shifted,
small opacity delta — nothing on any screen may flash > 3×/s. No strobe, no
"warp flash" transition, ever.

## 4. Escalation-ladder decision — stop at rung 5, zero bytes

- Rungs 1–2 (CSS transitions/keyframes) cover chrome, constellation draw-in
  (SVG `stroke-dashoffset`), comet (`offset-path: path()`), halo pulse, starfield.
- Rung 3 (`document.startViewTransition`) covers list→porthole shared-element
  continuity (thumbnail constellation → full canvas). Feature-detect; fallback
  is the reduced cross-fade.
- Rung 5 (WAAPI) covers the drag-settle tween only.
- **No rung-6 library.** None of the five triggers apply: no mid-flight timeline
  seeking, no true spring physics required (drag is 1:1 pointer-follow), no SVG
  path morphing, no scroll-scrubbed scene, no gesture physics. Motion payload: 0 KB.

## 5. Screen designs (all 22 pages, five families + shell)

**Shell** (`App.tsx` layout wrapper — the comment at App.tsx:35 asks for exactly this):
64px icon-only left instrument rail (Pipelines, Apps, Docker, Compose, K8s, Cloud,
Environments, Hosts, Registry, Templates, Schedules, Notifications, Audit,
Analytics, Settings — grouped with hairline gaps); active item = lit ember dot + glow
sprite. Templates/special items get `--ink-2` + an outline, never a second hue. 48px top strip: breadcrumb, capability badges, user. All on `--hull-0`.

**A. Star-chart lists** — PipelinesPage, AppsPage, EnvironmentsPage, HostsPage,
RegistryPage, TemplatesGalleryPage, SchedulesPage, NotificationTargetsPage,
AuditLogPage: hairline rows; left cell is a **mini-constellation thumbnail**
(72×40 SVG drawn from the pipeline's real stages/edges — doubles as the
shared element for the porthole transition); status star dot; name in Space
Grotesk; meta (id, updated, duration) in mono `--ink-3`. Hover: background
lightens to `--hull-1`, thumbnail stars brighten (120 ms).

**B. Porthole views** — PipelineEditorPage, RunPage, DeploymentPage:
full-bleed `--void` canvas in a hull frame — 24px rounded window, 1px inner
rim light, HUD corner brackets. React Flow with custom node/edge types:
`StarNode` = core dot (6px) + halo sprite (radial-gradient, 48px) + small-caps
label beneath; `ConstellationEdge` = 1.5px slightly-curved path in
`rgba(255,255,255,.22)`. Editor: bottom-center capsule tool tray of stage types
(drag onto canvas). RunPage: same canvas + collapsible bottom telemetry console
(mono, 40% height) + right stage inspector panel (200 ms slide+fade).
Stage states color the star and its halo per tokens; running star pulses,
comet travels the active edge.

**C. Instrument panels** — DockerPage, ComposePage, KubernetesPage, CloudPage:
two-column instrument cards on `--hull-1`, gauge-style counters, mono values,
small-caps captions.

**D. Airlock** — SignInPage, SignUpPage: centered card floating over a full
static-drift starfield; OIDC primary button. NewAppWizard: 3-step airlock
sequence, step transition = 200 ms cross-fade + 8px decel rise.

**E. Analytics** — spark/line charts in `--ember` on `--hull-0`, faint hairline
axes only (chart discipline finalized at implementation).

SettingsPage: single 640px column, small-caps section headers, hairlines.
Empty states: faint constellation sketch + one sentence + one primary action.
Loading: skeleton star chart matching final layout (no spinners).

## 6. Runtime checks (this spec defines; `loop-test` authors)

**CI (Playwright, a11y — must-pass):**
1. Reduced-motion substitution: emulate `prefers-reduced-motion: reduce`;
   porthole open still becomes visible via opacity change; no running
   animation animates `transform` (filter `document.getAnimations()`).
2. Flash ceiling: every animation on the porthole has computed
   `animation-duration ≥ 7 s` for twinkle-class elements; no element's opacity
   cycles > 3×/s (SC 2.3.1).
3. Focus after view transition: list → porthole navigation lands focus on the
   canvas heading (`document.activeElement` assertion).

**Non-CI browser checks:** CLS < 0.02 on porthole open; open completes ≤ 400 ms
(transition timing); Calm mode halts comet + drift; `will-change` absent on
idle elements (computed-style audit).

## 7. What a green suite proves

That the motion is not broken or harmful — substitution branches exist, no
flash risk, focus lands, frames stay on the compositor. It does **not** prove
the motion feels good; that judgment stays human, via the mockup and review.

## 8. Implementation phases (each lands on `develop`, UI reviewed via screenshot)

1. **P1 Foundation** — tokens + fonts + shell (rail, strip, route wrapper),
   reduced-motion plumbing, Calm mode store flag.
2. **P2 Porthole** — starfield, StarNode/ConstellationEdge, PipelineEditorPage,
   view-transition from list, draw-in choreography.
3. **P3 Run telemetry** — RunPage comet + console + inspector, DeploymentPage.
4. **P4 Charts & lists** — star-chart list family + thumbnails.
5. **P5 Panels & airlock** — instrument panels, sign-in/up, wizard, settings,
   analytics, empty/skeleton states.
6. **P6 Verification** — the §6 checks authored and wired into CI.
