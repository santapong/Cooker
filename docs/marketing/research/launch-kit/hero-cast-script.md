<!-- DRAFT hero-cast script -->

# Cooker — 60-second hero cast: script and storyboard

> Status: DRAFT. The maintainer records this; this document is the pre-production brief.
> Tagline in use: "CI/CD you can see."
> Honest scope: single-tenant, self-hosted, `docker compose up` quickstart. No multi-tenant or enterprise claims.
> This asset is launch precondition P3 (strategy.md §4). Nothing in announce.md is actionable until this exists.

---

## 1. Goal and specs

**Goal.** Demonstrate Cooker's core value proposition — drag a pipeline, run it, watch it go green — in the time it takes to read a tweet thread. The cast must answer "what does this thing do?" for a visitor who arrived from HN or a Reddit link and has zero prior context.

**Format.** Prefer a hybrid recording: a short terminal segment (`docker compose up`) leading into a full-screen browser recording of the UI. Pure asciinema cannot capture the React Flow graph editor, which is the differentiator. Use OBS, QuickTime screen recording, or `wf-recorder` on Wayland — export as MP4 (H.264, no audio track). An asciinema cast of only the terminal portion is acceptable as a fallback embed for docs pages where video is not supported, but the primary asset must show the UI.

**Duration.** 60 seconds nominal. Acceptable range: 55–65 seconds. Do not shorten beats to the point the text captions cannot be read at normal pace.

**No voiceover.** Captions only. Captions carry all narrative weight.

**Resolution.** 1280x720 minimum. 1920x1080 preferred for YouTube upload. The README embed can be a 1280x720 GIF extracted from the first 8 seconds for above-the-fold bandwidth; the full MP4 links to the YouTube/docs page.

**Beats.** 10 beats, as specified below.

**Embed targets.**
- README.md: first viewport, above the install commands. GIF of the first 8 seconds (the `docker compose up` beat) autoplays; MP4 is linked.
- Docs site landing page: full MP4 embed via `<video autoplay muted loop>`.
- Product Hunt listing: the same MP4, optionally trimmed to 90 seconds with a closing title card.
- Bluesky/X native video: the 30-second cut-down (section 5 below).

---

## 2. Beat-by-beat shot list

All timestamps are cumulative from 00:00. Aim for the listed endpoint; adjust pacing in post if needed by stretching or tightening the terminal/UI wait animations rather than cutting beats.

| Timestamp | On-screen action | Terminal command / UI action | Caption text |
|---|---|---|---|
| 00:00 – 00:06 | Terminal: black background, cursor blinks, user types and presses Enter | `git clone https://github.com/santapong/Cooker.git && cd Cooker` | **Cooker.** CI/CD you can see. |
| 00:06 – 00:16 | Terminal: `docker compose up` output scrolls — Postgres starts, Redis starts, backend starts, frontend starts. Final line: `cooker ready on :8080` | `docker compose up` | One command. No agents. No SaaS. |
| 00:16 – 00:20 | Browser opens at `http://localhost:8080`. The pipeline canvas is empty, centered, with a faint grid. A "New Pipeline" button is visible. | Open browser, navigate to `http://localhost:8080` | Open the canvas. |
| 00:20 – 00:28 | User opens the node palette (sidebar or right-click menu). Drags four nodes onto the canvas in sequence: Build, Test, Push, Deploy. Each node snaps to a horizontal row with a short drop animation. | Drag Build node → canvas. Drag Test node → canvas. Drag Push node → canvas. Drag Deploy node → canvas. | Drag the stages you need. |
| 00:28 – 00:34 | User draws three edges: Build→Test, Test→Push, Push→Deploy. Each edge appears as a curved arrow. The DAG is now a complete left-to-right chain. | Click Build node output handle, drag to Test node input handle. Repeat for Test→Push and Push→Deploy. | Wire them together. |
| 00:34 – 00:38 | User clicks the green "Run" button (top-right of canvas or toolbar). A confirmation is not required — it fires immediately. The pipeline status badge changes from grey "Idle" to blue "Running". | Click "Run" button. | Click Run. |
| 00:38 – 00:46 | Right panel (or bottom panel) opens showing a live log stream. Lines scroll in real-time: build output first (layer pulls, compile lines), then test output (PASS lines), then push digest lines. Text scrolls fast enough to look live but slow enough that the viewer can read two or three lines. | (No manual action — logs stream automatically via WebSocket) | Logs stream in real time. No polling. |
| 00:46 – 00:52 | The Push node on the canvas turns green. The Deploy node starts blinking (in-progress). A single line in the log panel reads: `Deploying to Kubernetes...` then `Rollout complete.` The Deploy node turns green. | (No manual action — status updates via WebSocket) | Build pushed. Deploying now. |
| 00:52 – 00:57 | Camera holds on the fully-green four-node pipeline. All four nodes show a green check mark. The status badge reads "Success". | (No manual action — hold shot) | Pipeline complete. Image pushed. Deployment rolled out. |
| 00:57 – 01:00 | Fade to black. White text on black: `cooker` in monospace, then below it `CI/CD you can see.` and below that `github.com/santapong/Cooker MIT` | (Title card, no UI action) | (No caption — the title card is the caption) |

---

## 3. Exact commands and actions, in recording order

This is the rehearsal checklist for the maintainer. Run through it on a clean machine before recording.

**Pre-recording setup (not recorded).**
- Ensure Docker Desktop or Docker Engine is running.
- Ensure ports 8080 and 5173 are free.
- Clear any previous Cooker volumes: `docker compose down -v`.
- Pre-pull images so the `docker compose up` output does not show slow pull progress during the demo. Image-pull progress is not interesting; the "services started" output is. Pre-pull: `docker compose pull`.
- Set terminal font size to 16pt+ so it is legible at 1280x720.
- Set terminal window to 120 columns x 36 rows. This is the sweet spot for readability after video compression.
- Close all other browser tabs. Use a clean browser profile with no extensions that add UI chrome.
- In the browser, zoom to 100%. The React Flow canvas should fill the viewport.

**Recorded sequence.**

1. Terminal is in focus, cursor at prompt in the home directory (or any clean directory — not already inside the Cooker folder).

2. Type (do not paste — the key-by-key rhythm reads as human):
   ```
   git clone https://github.com/santapong/Cooker.git && cd Cooker
   ```
   Press Enter. The clone output scrolls. Since it is pre-pulled, this is fast (< 2 seconds).

3. Type:
   ```
   docker compose up
   ```
   Press Enter. Wait for the `cooker ready on :8080` line (or equivalent readiness log line from the backend). This should take 8–10 seconds with pre-pulled images. Do not cut this short — the viewer needs to see that it actually starts.

4. Switch to the browser. Navigate to `http://localhost:8080`. The app loads. The empty canvas is visible.

5. Open the node palette. The exact UI interaction depends on the current frontend implementation: right-click on the canvas opens a context menu, or a sidebar panel lists stage types. Either path is acceptable. The recording must show the palette opening — do not drag from off-screen.

6. Drag the Build node onto the canvas. Pause 0.5 seconds. Drag the Test node. Pause 0.5 seconds. Drag Push. Pause 0.5 seconds. Drag Deploy. The four-beat pause rhythm prevents the recording from looking artificially accelerated.

7. Draw the edges. Click the output handle of Build, hold, drag to the input handle of Test, release. Repeat for Test→Push and Push→Deploy. Each edge should snap cleanly. Pause 0.3 seconds between edges.

8. Click the Run button. Do not move the mouse while the status badge changes — let the animation play.

9. The log panel opens automatically (or click the "Logs" tab if it is not auto-open). Let the logs scroll. Do not fast-forward. The scroll speed is what makes this feel real.

10. Hold on the all-green canvas for 5 seconds. This is the payoff shot; do not rush it.

11. Fade to black. Show the title card for 3 seconds.

---

## 4. Recording tips

**Terminal.**
- Font: JetBrains Mono, IBM Plex Mono, or Fira Code. All are monospaced and legible after H.264 compression. Avoid thin weights.
- Font size: 16pt at 1280x720, 18pt at 1920x1080.
- Color scheme: dark background, high-contrast foreground. The exact scheme does not matter; avoid solarized-dark at small sizes (the yellow on brown is hard to read in video).
- Window size: 120 columns x 36 rows.
- Shell prompt: keep it short. If your prompt is multiline or shows git branch info, simplify it for the recording. A bare `$ ` is fine.
- Do not show a real hostname or username if you prefer not to; using a prompt like `cooker@demo:~$` is fine and adds a light branding touch.

**Browser.**
- Use a Chromium-based browser at 100% zoom. Firefox renders React Flow correctly but Chromium produces sharper screen recordings on most platforms.
- Hide the bookmarks bar and any pinned extensions for a clean chrome (ironic phrasing noted).
- The browser tab title should read "Cooker" — confirm this before recording.

**Asciinema (terminal portion only, optional fallback).**
- Record with: `asciinema rec --cols 120 --rows 36 --stdin cooker-terminal.cast`
- Stop with Ctrl-D when you switch to the browser. This gives you a standalone terminal cast for embedding in docs.
- Upload to asciinema.org or self-host; embed the player in the docs quickstart page.
- The asciinema cast covers beats 1–2 only (the `git clone` and `docker compose up`). The full browser recording covers beats 3–11.

**Screen recording.**
- macOS: QuickTime Player "New Screen Recording." Select the browser window for beats 3–10, the terminal for beats 1–2. Stitch in a video editor.
- Linux: OBS Studio (scene-switch between terminal and browser windows) or `wf-recorder -g "$(slurp)"` for a region capture.
- Windows: OBS Studio. Note: Cooker does not run natively on Windows, so the recording environment should be a Linux VM or WSL2 with X forwarding, or a Linux host.

**Captions.**
- Burn captions into the video (not as a sidecar) for maximum compatibility. Use a 24pt+ sans-serif font (Inter, Helvetica Neue, or system default). White text, 2px black outline, positioned at 10% from the bottom of the frame.
- Each caption appears 0.3 seconds after the beat begins and disappears 0.3 seconds before the next beat. This prevents the "caption flash" effect.
- Exact caption text is in the table above. Do not ad-lib; these phrases were chosen to be short enough to read in the allotted time.

**Post-production.**
- Export the full cut at H.264, CRF 18, 30fps. File size should land under 15MB at 1280x720.
- Extract the GIF: `ffmpeg -i cooker-hero.mp4 -vf "fps=15,scale=1280:-1:flags=lanczos" -loop 0 cooker-hero.gif`
- The GIF covers 00:00–00:08 (the terminal beats). Keep it under 4MB for README embed performance. If it exceeds 4MB, reduce fps to 12 or scale to 960px wide.
- Do not add background music. No audio track at all. The cast is silent.

---

## 5. 30-second cut-down (Bluesky / X native video)

The 30-second variant covers the same story with beats compressed. Use this as the native video upload on Bluesky and X/Twitter on launch Monday at 17:00 ET (per the announce.md calendar).

| Timestamp | On-screen action | Caption text |
|---|---|---|
| 00:00 – 00:04 | Terminal: `docker compose up` output scrolling. Show the last 6 lines finishing and the ready log line. (Skip the `git clone` beat.) | Cooker. One command to start. |
| 00:04 – 00:08 | Browser opens at `http://localhost:8080`. Empty canvas visible. | Self-hosted CI/CD — open in a browser. |
| 00:08 – 00:16 | Drag Build → Test → Push → Deploy nodes onto canvas. Draw the three edges. Slightly faster pace than the 60s version — acceptable because this audience expects short-form content. | Drag the pipeline you need. |
| 00:16 – 00:19 | Click Run. Status badge turns blue. | Click Run. |
| 00:19 – 00:25 | Log panel scrolls. Build → Test → Push lines visible. Deploy node turns green. | Logs live. Node turns green. |
| 00:25 – 00:28 | Hold on the all-green canvas. | Pipeline complete. Image pushed. Deployed. |
| 00:28 – 00:30 | Title card: `cooker — CI/CD you can see. github.com/santapong/Cooker` | (Title card) |

**Export spec for Bluesky / X.** MP4, H.264, 1280x720, 30fps, no audio, under 50MB (Bluesky's current limit for video uploads is 50MB at 60s; the 30s cut will land well under this). Aspect ratio 16:9; both platforms support this natively.

**Caption note.** Bluesky and X both support uploaded caption (SRT) files for accessibility. Provide the SRT alongside the video. The SRT for the 30-second cut is a 7-line file following the timestamps above; generate with any SRT editor or by hand.

---

## 6. Launch-kit integration notes

- The full MP4 (`cooker-hero.mp4`) and GIF (`cooker-hero.gif`) belong in `docs/images/` once recorded. They are not committed until final (file size).
- The README draft at `docs/marketing/research/launch-kit/cooker-README.draft.md` has a placeholder for the hero cast (`{{HERO_CAST}}`). Replace it with the GIF embed + MP4 link once the recording exists.
- The Product Hunt listing at `docs/marketing/research/launch-kit/product-hunt-listing.md` also holds a placeholder for the video asset — use the same MP4 (Product Hunt accepts H.264 MP4 under 50MB).
- The launch-readiness tracker (`launch-readiness-tracker.md`) marks the hero cast as precondition P3. Record this before any launch-week calendar action begins.
- Feed the MP4 URL (once hosted) to `cooker-mkt-seo` so it can add a `VideoObject` structured-data block to the docs site head. The canonical YouTube URL is the preferred target for structured data.

---

## Cross-team flags

- **`cooker-mkt-seo`**: once the MP4 is on YouTube and the GIF is in the README, provide the YouTube URL for `VideoObject` JSON-LD schema. The transcript of the captions above is the `description` field.
- **`cooker-mkt-geo`**: the hero cast is the primary citable demonstration of the product. Ensure the YouTube title and description use the canonical one-line product description from strategy.md §1 verbatim, so AI citation tools index the right sentence alongside the video URL.
- **`cooker-mkt-segmentation`**: beat ordering (Build → Test → Push → Deploy) assumes persona 1 (indie hacker on k3s). If segmentation concludes that the launch audience is more heavily persona 2 (SMB platform team), consider adding a brief Deploy-to-Kubernetes beat showing the cluster target selection UI before the log stream. This would extend the runtime to ~70 seconds; acceptable for the docs embed, too long for the social cut.
- **Maintainer (product-readiness gate)**: this script assumes the `docker compose up` quickstart works end-to-end on a clean machine and that the live log WebSocket is stable. If either is broken, the cast cannot be recorded honestly. Confirm both against launch precondition P4 before scheduling the recording session.
