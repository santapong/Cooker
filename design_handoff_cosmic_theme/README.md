# Handoff: Cooker — Cosmic / Galaxy Theme

## Overview
This package re-skins the **Cooker** CI/CD platform (the app in `frontend/`) into a **cosmic / galaxy** visual identity. The organizing metaphor — "a planet linked to another planet" — maps directly onto Cooker's product: **every pipeline stage is a planet, every DAG edge is a trajectory between planets.** Status maps to cosmic phenomena, and Cooker's existing warm **ember/gold** is kept as the "running / live" star-fire so brand continuity holds.

It ships **two modes**:
- **Deep Field** (dark) — deep-space navy, nebula bloom, a real starfield, violet→cyan glow accents. This is the primary mode.
- **Daybreak** (light) — a dawn-atmosphere variant (peach horizon → sky blue) that reuses the same token contract.

Scope covered: the full app (Pipeline editor "Star Map", Run "Mission Control", Pipelines, Apps, App detail, New-app wizard, Registry, Environments, Clusters, Hosts, Docker, Compose, Templates, Schedules, Notifications, Settings) **and** a public marketing site (Home, Pricing, Docs, Sign in, Sign up).

## About the Design Files
The files in `design-references/` are **design references created in HTML/React-via-Babel** — prototypes that show the intended look and behavior. **They are not production code to copy directly.** The task is to **recreate this visual system inside Cooker's existing frontend** (React 18 + TypeScript + Vite + Zustand + React Flow), reusing its established patterns:

- The app already has a token system at `frontend/src/theme/tokens.ts` (`COOKER_TOKENS`, `cookerTheme(mode)`, `CookerTheme` interface), a `ThemeProvider` (`frontend/src/theme/ThemeProvider.tsx`), and a `themeMode` in `frontend/src/stores/uiStore.ts`. **The cosmic theme should be implemented as new return values from `cookerTheme()` plus a few additive token fields — not a rewrite.**
- UI atoms live in `frontend/src/components/ui/atoms.tsx` (`Pill`, `Btn`, `Card`, `StatusDot`, `KindBadge`, `PageHeader`, etc.). Re-skin these; their component APIs do not need to change.
- Pipeline nodes/edges are React Flow: `frontend/src/components/pipeline/nodes/StageNode.tsx` (+ Build/Test/Push/Deploy/Approval/Custom wrappers) and `frontend/src/components/pipeline/edges/ConditionalEdge.tsx`.
- Layout chrome: `frontend/src/components/layout/{Sidebar,TopBar}.tsx`.
- Global CSS / keyframes: `frontend/src/index.css`.

If implementing the **marketing site** (Home/Pricing/Docs/Sign in/Sign up) and no public-site app exists yet, build it as its own route group or a small static site using the same tokens; those reference files are plain HTML + one shared CSS file and can be ported to whatever the team prefers.

## Fidelity
**High-fidelity (hifi).** Exact colors, typography, spacing, radii, shadows, and interaction states are specified below. Recreate pixel-faithfully using the codebase's existing libraries and component structure.

---

## Design Tokens

### Fonts (Google Fonts)
```
Display / headings : "Space Grotesk", 400/500/600/700   ← NEW (replaces Fraunces for display)
Body / UI          : "Inter Tight", 400/500/600/700     ← already in app
Mono / data        : "JetBrains Mono", 400/500/600      ← already in app
```
Import: `https://fonts.googleapis.com/css2?family=Space+Grotesk:wght@400;500;600;700&family=Inter+Tight:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500;600&display=swap`

> In the existing `CookerTheme`, the `serif` slot is used for display headings (Fraunces). For cosmic, point display usage at **Space Grotesk**. Recommend renaming/adding a `display` field, or repurposing `serif` → Space Grotesk for this theme.

### DEEP FIELD (dark) — primary
| Token (suggested name) | Hex | Usage |
|---|---|---|
| `void` | `#06061A` | page/root background, letterbox |
| `bg` | `#08081F` | app background |
| `canvasTop` → `canvasBot` | `#0C0B28` → `#070617` | DAG canvas radial gradient |
| `surface` | `rgba(20,21,52,0.72)` | glass panels (with `backdrop-filter: blur(10px)`) |
| `surfaceSolid` | `#12132F` | opaque cards/rows |
| `surfaceAlt` | `#0D0E25` | sidebar, recessed |
| `panelGlass` | `rgba(14,15,40,0.66)` | topbar/palette/inspector glass |
| `line` | `rgba(138,126,230,0.16)` | hairline borders (violet-tinted) |
| `lineStrong` | `rgba(138,126,230,0.32)` | stronger dividers |
| `text` | `#ECEBFF` | primary text (starlight) |
| `textSoft` | `#ADB0E4` | secondary |
| `textMute` | `#6E70A6` | tertiary / mono labels |
| `textFaint` | `#4A4C7A` | disabled / separators |
| `violet` (accent) | `#8B6DFF` | primary accent |
| `violetGlow` | `#7C5CFF` | glows, focus rings, primary-btn gradient end |
| `cyan` | `#33E1F2` | secondary accent |
| `ember` | `#FFB454` | **running / live** (Cooker continuity) |
| `emberDeep` | `#F59E0B` | ember gradient end |
| `good` | `#4FE6B0` | success / deployed |
| `bad` | `#FF6B8A` | failed / error |
| `warn` | `#FFC861` | pending / awaiting |
| `cool` | `#5BB6FF` | info |

### DAYBREAK (light)
| Token | Hex |
|---|---|
| `void` | `#EEF1FB` |
| `bg` | `#F3EEF0` |
| `canvasTop` → `canvasBot` | `#E9EEFC` → `#FDEEE4` (sky → peach horizon) |
| `surface` | `rgba(255,255,255,0.74)` |
| `surfaceSolid` | `#FFFFFF` |
| `surfaceAlt` | `#F4F1FB` |
| `panelGlass` | `rgba(255,255,255,0.70)` |
| `line` | `rgba(108,96,196,0.18)` |
| `lineStrong` | `rgba(108,96,196,0.34)` |
| `text` | `#241F3C` |
| `textSoft` | `#5A5680` |
| `textMute` | `#928FB4` |
| `textFaint` | `#BDBAD6` |
| `violet` | `#6D52E0` |
| `cyan` | `#1597B6` |
| `ember` | `#E8902F` |  `emberDeep` `#C9740F` |
| `good` | `#1FA97A` |  `bad` `#E05574` |  `warn` `#D99411` |  `cool` `#3B82C4` |

### Planet identity (per stage type)
Each stage `kind` renders as a planet orb with a radial-gradient body (`from`→`to`), a halo (`glow`), and a unicode glyph. Used in nodes, palette, inspector, legend.
| kind | from | to | glow | glyph |
|---|---|---|---|---|
| `source`   | `#7FD0FF` | `#3B82C4` | `#5BB6FF` | `◐` |
| `build`    | `#FFC36B` | `#E8702A` | `#FF9D45` | `✦` |
| `test`     | `#7CF2FF` | `#1AA7C4` | `#33E1F2` | `◉` |
| `push`     | `#C0A8FF` | `#7C5CFF` | `#A78BFA` | `▲` |
| `deploy`   | `#7DF4C6` | `#19A878` | `#4FE6B0` | `⬢` |
| `approval` | `#FFE08A` | `#E0A93A` | `#FFD166` | `◇` (Saturn ring) |
| `custom`   | `#D7B9FF` | `#9B6BE8` | `#C49BFF` | `✺` |

Orb body CSS: `radial-gradient(circle at 33% 28%, <from> 0%, <to> 72%, <to@0.7> 100%)`, plus `box-shadow: inset -3px -4px 8px rgba(0,0,0,0.35), inset 2px 2px 5px rgba(255,255,255,0.4), 0 0 <0.4*size>px <glow@0.5>`. Halo = absolutely-positioned `radial-gradient(circle, <glow@0.34>, transparent 70%)` inset by ~34% of size. `approval` adds a rotated elliptical ring overlay.

### Status → color
`success/deployed → good` · `failed/error → bad` · `running/building → ember` · `pending/queued → textMute` · `awaiting → warn`. (Maps onto the existing `statusTone()` in `atoms.tsx`.)

### Spacing / radius / shadow
- Radius: chips/ports `999px`; buttons/inputs `8–11px`; cards/panels `12–18px`.
- Card shadow (dark): `0 12px 30px rgba(0,0,0,0.42)`; live node adds `0 0 26px rgba(255,180,84,0.30)`.
- Glass: `backdrop-filter: blur(10–14px)` on surfaces with semi-transparent bg.
- Primary button: `linear-gradient(135deg, violet, violetGlow)`, `color:#fff`, `box-shadow: 0 6px 18px rgba(124,92,255,0.4)`; lift `translateY(-1px)` on hover.
- Focus ring (inputs): `border-color: violet; box-shadow: 0 0 0 3px rgba(139,109,255,0.22)`.

### Keyframes (port to `index.css`)
- `ccTwinkle` — star opacity 0.5↔0.12.
- `ccHalo` — planet halo scale 1↔1.12 (live only).
- `ccSpin` — orbit ring rotation (live only).
- `ccPulse` — expanding box-shadow ring on live status dots (replaces existing `cookerPulse`, ember-tinted).
- `ccShimmer` — progress sweep on running nodes.
- `ccDash` — `stroke-dashoffset` march on active trajectories.
- `ccBlink` — log cursor.

---

## Screens / Views

> All app screens share the shell: **Sidebar (232px)** + **TopBar (54px glass)** + content area on a `radial-gradient(canvasTop→canvasBot)` backdrop with a sparse drifting starfield. Re-skin `Sidebar.tsx`/`TopBar.tsx` once; every page inherits it.

### Shell — Sidebar
- 232px, `surfaceAlt` bg, sparse non-drifting starfield behind, `line` right border.
- Brand: a **ringed-planet mark** (cyan→violet body, ember orbit ring) + "Cooker" in Space Grotesk 21/600, tagline `build · ship · orbit` in mono 9/uppercase/letter-spacing 2.
- Workspace chip: CK tile with `linear-gradient(135deg, violet, cyan)`.
- Nav items: glyph + label; active item = `violet@0.16` bg, `violet@0.4` border, `inset 2px 0 0 violet` + soft glow, trailing glowing dot. Inactive `textSoft`, hover lighten.
- "Constellation" (recent) list with status dots; ember items pulse.
- Footer: OP avatar (ember gradient) + "Operator / signed in".

### Shell — TopBar
- 54px, `panelGlass` + blur, `line` bottom border.
- Left: breadcrumbs (mono for IDs). Right: search field (`⌕ Find an app, run, image…` + `⌘K`), simple/pro segmented toggle (pro = violet), theme toggle (`☾`/`☀`), bell with ember dot.

### Pipeline editor — "Star Map" (hero) — `PipelineEditorPage`
- Header strip (glass): breadcrumbs, "Star Map", `N planets` (violet pill) + `N links` (cyan pill), a live `run #… · live` ember chip, "updated …".
- Left **palette** (212px glass): "Launch a planet" — six draggable stage cards, each a planet orb + label + desc; bottom "Validate orbit" + gradient "▶ Launch run".
- **Canvas**: `radial-gradient(canvasTop→canvasBot)` + drifting starfield + faint dotted grid (`radial-gradient(violet@0.1 1px, transparent 1px)` 34px). React Flow nodes are **planet nodes**; edges are **trajectories** (see below). Zoom controls (top-right glass stack), minimap (bottom-left glass showing planets as glowing dots + viewport rect), hint "drag a planet to launch · scroll to warp →".
- Right **inspector** (296px glass): planet header (orb + name + kind/status), config fields (image/command/timeout/retries/fan-out), a **live telemetry** mini log tail with blinking ember cursor, footer "Logs" / "Abort".

#### Planet node (re-skin `StageNode.tsx`) — 214px wide
- Glass card: `surface` + blur, `1px` border (`line`; selected→`violet`; live→`ember@0.5`), radius 14, padding `11px 13px`.
- Left "atmosphere" glow strip: `radial-gradient(ellipse at left center, <glow|ember>@0.30, transparent 75%)`.
- Connection **ports**: 9px glowing dots centered on left/right edges (`violetGlow`, `0 0 8px` glow) — these are the React Flow `<Handle>`s, restyled.
- Row 1: 34px planet orb + label (Inter Tight 13/600) + detail (mono 10/textMute).
- Row 2: status dot (pulses + ember when live) + duration/status (mono 10/uppercase; ember when live) + running shimmer bar; optional env pill (cool) right-aligned.
- Pending/queued nodes render at `opacity: 0.62`.

#### Trajectory edge (re-skin `ConditionalEdge.tsx`)
- Cubic-bezier path between source/target ports. State-driven:
  - `idle` → faint dotted (`stroke-dasharray: 1 7`, `line`).
  - `done` → solid green light-trail (`good`), with a soft blurred underlay glow.
  - `active` (downstream of a running node) → ember/violet gradient stroke 2.4px, `stroke-dasharray: 8 6` marching via `ccDash`, a blurred ember underlay, **and a travelling comet** (a white `<circle r=3.6>` with `drop-shadow(ember)` animated along the path via SVG `animateMotion`).

### Run view — "Mission Control" — `RunPage`
3 columns: **flight-log rail** (left 296) + **log stream** (center) + **promotion trajectory** (right 312).
- Rail: "Flight log · run #…", pipeline name (display), running/time pills, a vertical timeline whose connector is a `good→ember→line` gradient; each stage row = a "step planet" (✓ for done, pulsing dot for live, hollow for pending) + name + kind + duration; bottom "✕ Abort run".
- Log stream: **near-black** panel (`#05050F`) with its own faint starfield. Header: test planet orb + stage name + line count + `all/info/warn/error` filter (violet active) + green "tail · live". Body: monospace lines `timestamp(#5A5C8A) · LEVEL(ok=good/info=soft/warn=warn/error=bad) · message(#D6D8FF)`, ending in a pulsing ember dot + blinking block cursor. Footer: line/warn/error counts.
- Promotion rail: dev/staging/prod as planet cards along a dotted vertical trajectory; current target shows running orb + "awaiting" → "✓ Approve production" gradient button. Below: "Live telemetry" cpu/memory/net bars (cyan/violet/ember, glowing fills).

### Pipelines list — "Constellation gallery" — `PipelinesPage`
Header ("N pipelines · 1 live", "Pipelines", subtitle, Templates + New pipeline). Responsive grid (`minmax(340px,1fr)`) of glass cards: name (display 19/600), status dot + status + `N planets` pill, a **mini constellation SVG thumbnail** (planet dots wired by faint dashed lines; live node ember), description, footer "updated …" + "open map →". Nebula corner glow per card.

### Apps — "Mission roster" — `AppsPage`
Greeting header ("Good evening." / "N apps in orbit, all healthy."). Four glass stat tiles (left accent bar). Two columns: **services table** (StatusDot, service/team, env pill + image mono, health bar + %, runs, last deploy, owner avatar gradient, →) with all/yours/broken tabs; **Telemetry feed** (timeline with connector line, ember dot on newest).

### App detail — "Planet profile" — `AppDetailPage`
Hero: 56px deploy planet (running, ringed) + name + status/env pills + URL/repo + Redeploy/Deploy. Four stat tiles (health/replicas/runs/p95). Deploy history table (version, env pill, status, time). Right: deploy-target config (`KV` grid) + GitHub webhook (masked hmac + Rotate).

### New-app wizard — "Launch sequence" — `NewAppWizard`
Centered (max 880). Header "New app". 4-step stepper (done=green ✓, active=violet gradient glow, future=hollow; connectors solid/dotted). Active step card: build planet + "Configure the build", mono-styled fields (name/dockerfile/context/port), Back / Continue.

### Registry — "Orbiting artifacts" — `RegistryPage`
Two columns. Repos list (ringed push-planet + repo mono + tags/pushed/size; selected row violet-tinted + left border). Tag inspector: manifest planet header, tags as cards (tag + latest pill + size + digest + arch), **Referrers** chips (✓ cosign sig=good, SBOM=cyan, provenance=violet).

### Environments — "Planetary systems" — `EnvironmentsPage`
Three env cards as planets along a promotion trajectory (dotted SVG arrows between them): dev (source orb, auto), staging (test orb, running, auto), production (deploy orb, ringed, manual gate). Each: orb + name (display) + namespace + status pill + strategy/vars/secrets pills + "deployed …" + "manage →". Below: production **secrets** table (masked values, rotation age, vault-backend pill).

### Clusters — `KubernetesPage`
Header (kubernetes · client-go watch). Four stat tiles. Workloads table: pod (status dot + mono name), namespace, status pill, CPU health bar + %, restarts (bad if >3). Includes Running/Pending/Ember-rollout/CrashLoop states.

### Hosts — `HostsPage`
Header (ssh-docker · tofu pinned). Grid of host cards: source planet + name + addr + status pill; host-key fingerprint (`KV`) + container count; a "key changed" host shows red border + "⚠ Re-verify host key".

### Docker — `DockerPage`
Header (local engine · docker.sock, "dev-only"). Tabs (Containers/Images/Volumes/Networks). Container table: status dot, name, image, ports (cyan), status (up/exited).

### Compose — "Service constellation" — `ComposePage`
Split: left canvas with 3 service planets (web→db, web→cache dashed trajectories) on starfield+grid; right `docker-compose.yml` panel (line numbers, syntax-tinted: keys cyan, values soft, comments faint) with "valid" pill + "▶ Up".

### Templates — "Constellation catalog" — `TemplatesGalleryPage`
Grid of glass cards: name (display) + tag pill, **constellation thumbnail**, description, "N deploys" + "use template →", nebula corner glow.

### Schedules — `SchedulesPage`
Header (cron · leader-elected). Table: pipeline (custom planet, ember when imminent) + timezone, cron (cyan mono), cadence (human), next run (ember), enable toggle. Disabled rows dimmed.

### Notifications — `NotificationsPage`
Header (multi-channel dispatcher). Grid of target cards: brand tile (Slack #4A154B / Discord #5865F2 / Email cyan / Webhook ember) + name + kind + enable toggle; event-type filter chips (violet). Disabled cards dimmed.

### Settings — `SettingsPage`
Centered (max 920). Header (workspace · config). Tabs (General/Builders/Secrets/Auth/Observability). Sections (Builder / Secrets backend / Observability) as glass cards with label-row layout + status pills; footer Reset / Save.

### Marketing site (standalone — `design-references/cosmic-*.html`)
Shared `cosmic-site.css` (CSS variables mirror the dark tokens) + `starfield.js` (canvas drifting stars, density ∝ area, ~18% ember/30% cyan tinted). Sticky glass nav with ringed-planet logo; gradient footer.
- **Home** (`cosmic-home.html`): hero with **animated star-map SVG** (planets + trajectories + comet on the active link) and headline "Ship code across the galaxy." (gradient word); meta stats; logo strip; "Build · ship · orbit" pillars; pipeline showcase with mock editor; 6-card feature grid; deploy-targets row; gradient CTA band; footer.
- **Pricing** (`cosmic-pricing.html`): three planet tiers (Explorer/Crew featured/Constellation), full comparison table (section rows + yes/no/value cells), FAQ 2-col.
- **Docs** (`cosmic-docs.html`): 3-col (left section nav with colored dots, content with code blocks/callouts/numbered steps, right "on this page" TOC).
- **Sign in** (`cosmic-signin.html`): split — left brand panel with big ringed planet, right OIDC buttons (Keycloak/Okta/Google/GitHub) + email/password + "Launch console". OIDC + PKCE note.
- **Sign up** (`cosmic-signup.html`): mirror — form (name/email/workspace/password) + free-tier perks panel with ember planet.

---

## Interactions & Behavior
- **Sidebar nav**: route-active item highlighted (violet glow + inset bar); hover lightens row.
- **Planet node**: hover/selected → violet ring + lift shadow; live → ember outer glow + halo pulse + orbit-ring spin + shimmer progress bar; pending → dimmed (0.62).
- **Trajectory**: state machine `idle → active → done` (driven by upstream node status); active marches dashes + runs a comet (`animateMotion`).
- **Run logs**: live tail appends lines; blinking cursor while `status==='running'`; filter chips subset by level.
- **Buttons**: `translateY(-1px)` + stronger shadow on hover; primary uses violet gradient.
- **Inputs**: violet focus ring.
- **Toggles** (schedules/notifications): knob slides; track `good` when on, `line` when off.
- **Theme toggle**: swaps Deep Field ↔ Daybreak via the existing `themeMode` store; all tokens flip.
- **Reduced motion**: gate starfield/comet/halo/shimmer behind `@media (prefers-reduced-motion: no-preference)`; the visible end-state must be the base style (important for any SSR/print).
- **Marketing**: smooth-scroll anchors; sticky nav; cross-links between Home/Pricing/Docs/Sign in/Sign up.

## State Management (reuse existing)
- `uiStore.themeMode`: extend to support the cosmic palettes (e.g. `'dark' → Deep Field`, `'light' → Daybreak`, or add a `'cosmic'` mode). `ThemeProvider` already memoizes `cookerTheme(themeMode)`.
- `uiStore.mode`: existing simple/pro toggle (drives palette + inspector/rail visibility) — unchanged.
- Pipeline run/stage status already streams via `useStageLogs` / `useWebSocket`; the planet/trajectory states are **derived from existing run+stage status** — no new state needed. Map `statusTone()` → planet glow/edge state.
- Edge `state` (idle/active/done) is computed from the source node's run status (done if upstream succeeded, active if upstream running, else idle).

## Assets
- **No raster assets.** All planets, rings, comets, starfields, and the brand mark are CSS/SVG generated — port them as components. (Brand mark = layered divs/SVG: glow + gradient body + ember ring.)
- Fonts via Google Fonts (above). If self-hosting, include Space Grotesk + Inter Tight + JetBrains Mono.
- Glyphs are unicode (`◐ ✦ ◉ ▲ ⬢ ◇ ✺` etc.) — safe to keep, or swap for the app's existing icon set.

## Files (in `design-references/`)
**App canvas (React-via-Babel prototype):**
- `Cosmic Cooker.html` — entry; loads the scripts below into a design canvas
- `cosmic-theme.js` — **all tokens** (Deep Field + Daybreak), `PLANET_KIND`, `statusColor`, `hexA` ← start here
- `cosmic-parts.jsx` — Starfield, PlanetOrb, **PlanetNode**, **Trajectory**, CosmicGraph (React Flow analogues)
- `cosmic-chrome.jsx` — CookerMark, Sidebar, EditorHeader, Palette
- `cosmic-ui.jsx` — TopBar, AppShell, atoms (CPill, CBtn, GlassCard, Stat, KV, Tabs, HealthBar, CPageHeader)
- `cosmic-screens.jsx` — Inspector, canvas chrome, **PipelineEditor**
- `cosmic-pages1.jsx` — Pipelines list, Apps (+ ConstellationThumb)
- `cosmic-pages2.jsx` — Run/Mission Control, Registry, Environments
- `cosmic-pages3.jsx` — App detail, New-app wizard, Clusters, Hosts, Docker
- `cosmic-pages4.jsx` — Compose, Templates, Schedules, Notifications, Settings
- `design-canvas.jsx` — the presentation wrapper only (NOT part of the product; ignore for implementation)

**Marketing site (plain HTML/CSS/JS):**
- `cosmic-site.css`, `starfield.js`
- `cosmic-home.html`, `cosmic-pricing.html`, `cosmic-docs.html`, `cosmic-signin.html`, `cosmic-signup.html`

## Suggested implementation order
1. **Tokens** — add Deep Field + Daybreak palettes to `tokens.ts` (extend `CookerTheme` with `violet`, `cyan`, `violetGlow`, `panelGlass`, `display`, planet map). Wire Space Grotesk + the new keyframes into `index.css`.
2. **Atoms** — re-skin `atoms.tsx` (Pill→glass/glow, Btn primary→violet gradient, Card→glass, StatusDot→ember pulse, KindBadge→planet orb).
3. **Chrome** — `Sidebar.tsx` + `TopBar.tsx` (glass, ringed-planet mark, violet-active nav).
4. **Pipeline** — `StageNode.tsx` (planet node) + `ConditionalEdge.tsx` (trajectory + comet) + canvas backdrop (starfield + grid) in `PipelineCanvas.tsx`. This is the signature surface.
5. **Pages** — apply tokens/atoms across the remaining pages (most need only re-skinned atoms + a starfield backdrop).
6. **Marketing** — port the five HTML pages as a route group / static site if in scope.
7. **Daybreak** — verify the light palette across everything; gate motion behind reduced-motion.
