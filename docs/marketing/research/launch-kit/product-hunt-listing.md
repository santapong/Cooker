<!-- DRAFT — for maintainer review before the Product Hunt listing is submitted. Do not publish. -->

# Product Hunt — Launch Kit

> Extends announce.md §3 (Product Hunt section).
> Launch day: Wednesday of launch week, 12:01 AM Pacific.
> Listing must be in "upcoming" mode for at least 24 hours before 12:01 AM Wednesday.
> Maker account must be aged 30+ days before launch day — set up the account ~5 weeks ahead.
> Date produced: 2026-06-21.

---

## 1. Tagline

```
CI/CD you can see. Self-hosted. Single binary. Apache-2.0.
```

Character count: 57 (Product Hunt limit is 60). No changes needed.

---

## 2. Listing description

> Cooker is an open-source CI/CD tool built around a visual pipeline editor. Instead of writing YAML,
> you drag Build, Push, and Deploy nodes onto a canvas, draw arrows between them, and click Run.
>
> **What it does**
> - Builds OCI-compliant Docker images using your choice of builder: docker (dev), Kaniko, buildah,
>   or BuildKit (production).
> - Pushes images to any OCI-compliant registry — Docker Hub, GHCR, ECR, GCR, self-hosted.
> - Deploys to Kubernetes across Dev, Staging, and Production environments with one pipeline
>   configuration.
> - Streams live build and deploy logs directly in the graph editor panel.
>
> **How it runs**
> A single Go binary serves the HTTP API, the React front-end, and the WebSocket log stream on
> port 8080. No agents, no SaaS, no asterisks. `docker compose up` is the quickstart.
>
> **What's not done yet (honest list)**
> - Single-tenant: every authenticated user can read every pipeline. Multi-tenancy is on the roadmap.
> - No in-product audit-log viewer. The log is written; the UI isn't built yet.
> - No PR-preview environments (first-class per-PR ephemeral envs planned, not shipped).
> - The docker builder mounts the host socket and is dev-only. Use Kaniko or buildah for production.
>
> **License and source**
> Apache-2.0. Full source at
> github.com/santapong/cooker. No EE tier, no CE/EE split, no "open core." Everything is in
> the public repo.

Editorial notes:
- Product Hunt limits the description to roughly 260 words in the rendered listing. The text above
  is approximately 230 words; it is within limits.
- The "What's not done yet" block is non-negotiable. It mirrors the HN first comment and the
  README section. Readers who find the PH listing via HN will check for consistency; a PH listing
  that hides the gaps while the HN thread is open about them damages trust.
- Do not add superlatives, awards, or comparisons to specific competitors in this description.
  Factual statements only. (strategy.md §7 brand rules apply on Product Hunt.)
- The first sentence must not start with "I" — Product Hunt's SEO heuristics penalise
  first-person-singular openers in descriptions.

---

## 3. Maker's first comment

Post this within 5 minutes of the listing going live (12:01 AM Pacific, Wednesday).

> Hi Product Hunt. I'm the maintainer of Cooker — happy to answer questions here.
>
> The 60-second demo cast is the fastest way to see what the graph editor feels like:
> [link to cast / README cast section]. The quickstart is `docker compose up` and takes about
> 30 seconds on a machine with Docker installed.
>
> A few things worth knowing upfront, because people ask:
>
> The graph is not just a viewer — it is the pipeline definition. The JSON the editor writes is
> what the executor runs. There's no YAML export or "sync back to code" step; the graph IS the
> config.
>
> The tool is single-tenant today. If you need team isolation, wait for the multi-tenancy
> milestone (tracked at github.com/santapong/cooker/issues). I'm not going to oversell where
> things are.
>
> I built this because I kept rewriting the same CI config on every side project and never had a
> clear picture of what was actually running. If that resonates, try it. If you hit something broken
> or missing, open an issue — I respond to every one.

Editorial notes:
- This comment must be posted from the maker account that claimed the product. An unverified
  "maker" comment tag reduces trust.
- The cast link is a placeholder: fill in the actual URL before launch night. If the cast is
  embedded in the README at github.com/santapong/cooker, the link can point there; if it is
  hosted separately (asciinema, YouTube), use that URL instead.
- "I respond to every one" is a commitment. Do not write it unless it is true and will remain
  true for at least 30 days post-launch.
- Do not say "thanks for the upvote" in any reply. (strategy.md §7.)
- Respond to every PH comment within 2 hours during the 24-hour launch window. After 24 hours,
  a daily check for the first week is sufficient.

---

## 4. Gallery / carousel captions

Product Hunt allows up to 5 gallery images. Recommended set of 3 (minimum viable); a 4th and
5th are optional and noted below.

### Image 1 — Graph editor with a completed run (the hero screenshot)

Caption:
> The pipeline is the graph. Drag Build, Push, and Deploy nodes; draw edges; click Run. The
> node status updates live as each step completes.

Asset spec:
- 1270 x 760 px, PNG or WebP.
- Show a pipeline with at least 3 nodes (Build, Push, Deploy) and at least 2 of them in a
  "completed / green" state. The third can be in-progress (spinner) for motion.
- The log panel should be visible on the right side, showing real output lines — not placeholder
  text.
- No personal data, no real credentials, no internal hostnames in the screenshot.

### Image 2 — Live log stream

Caption:
> Build logs stream directly in the editor. No page reload, no polling. A failed step turns
> red immediately; click it to see the last 500 lines.

Asset spec:
- 1270 x 760 px.
- Show the log panel in full-width or expanded mode with realistic-looking (but synthetic)
  build output. Something like `Step 3/8 : RUN go build ./...` followed by a few lines.
- A red "failed" node state is acceptable here — showing failure handling honestly is a
  trust signal, not a negative.

### Image 3 — Deploy diff / environment promotion

Caption:
> Deploy to Dev, Staging, or Production from the same pipeline. The environment selector shows
> what's running where before you promote.

Asset spec:
- 1270 x 760 px.
- Show the environment selector or a deploy-diff panel. If the "which cluster" UI is not yet
  prominent (per the honest notes in the HN comment), show the pipeline with the deploy node
  selected and its configuration panel open instead — that is an honest representation of
  current state.

### Image 4 (optional) — Docker compose quickstart terminal

Caption:
> `docker compose up`. That's the install. Port 8080.

Asset spec:
- 1270 x 760 px, dark terminal background.
- Shows the compose output with the final "ready at http://localhost:8080" line.
- Synthetic output is fine; real output preferred if reproducible.

### Image 5 (optional) — The 60-second cast as an animated GIF

Caption:
> 60 seconds from install to first green run.

Asset spec:
- Product Hunt accepts GIFs as gallery items. Export a 60-second asciinema or screen recording
  as GIF, capped at 10 MB. If the GIF exceeds 10 MB, trim to 30 seconds showing the
  graph-editor interaction only (not the install step).

---

## 5. Asset checklist

Check each item before the listing goes live Sunday night (the night before Wednesday).

| Asset | Spec | Status |
|---|---|---|
| Product icon | 240 x 240 px, PNG, transparent background. The wordmark glyph from strategy.md §2 (IBM Plex Mono / JetBrains Mono, with steam-curl glyph). Greyscale with amber accent (#E07A1F). | [ ] |
| Banner image | 1270 x 760 px, PNG/WebP. Graph editor screenshot with a completed run. Same as gallery image 1. | [ ] |
| Gallery image 1 | 1270 x 760 px. Graph editor — hero screenshot. Caption written above. | [ ] |
| Gallery image 2 | 1270 x 760 px. Live log stream screenshot. Caption written above. | [ ] |
| Gallery image 3 | 1270 x 760 px. Deploy/environment view. Caption written above. | [ ] |
| Gallery image 4 (opt.) | 1270 x 760 px. Terminal quickstart. | [ ] |
| Gallery image 5 (opt.) | GIF, ≤10 MB. 60-second cast clip. | [ ] |
| Demo video / cast | The 60-second hero cast embedded or linked. Product Hunt shows this as the primary media if provided. Prefer the YouTube URL if the YouTube video is live by Wednesday 12:01 AM; otherwise use asciinema or a direct MP4. | [ ] |
| Tagline | 50 chars. Written above. Fill in license before submitting. | [ ] |
| Listing description | ~230 words. Written above. Fill in license and repo URL. | [ ] |
| Maker account | Aged 30+ days. Verified email. Linked to the maintainer's GitHub. | [ ] |
| "Upcoming" listing | Submitted to PH "upcoming" section before Sunday 22:00 ET (=Sunday night, ≥26 hours before 12:01 AM Wednesday Pacific). | [ ] |
| Topics / categories | Set to: Developer Tools, Open Source, DevOps, Kubernetes. (Verify these exist as PH categories at submission time; category names change.) | [ ] |
| Launch URL | Points to the docs site (docs.cooker.dev) or the GitHub repo. Confirm the docs site is live before setting this. | [ ] |
| Backup operator | One person other than the maintainer has the PH maker account credentials and knows the comment-watch SLA (2-hour response during the 24-hour window). | [ ] |

---

## 6. Launch-day timing note

**Target: Wednesday of launch week, 12:01 AM Pacific.**

Rationale (from announce.md §3): Tuesday through Thursday are the highest-traffic days on
Product Hunt for developer tools. Wednesday avoids competing with high-profile Monday drops
(large consumer products tend to go Monday) and with the HN Show HN spike (Monday) that the
team is managing separately. Wednesday 12:01 AM Pacific = 03:01 AM Eastern = 08:01 AM UTC,
which gives European users a full day to discover the listing before it ages off the front page.

The 24-hour Product Hunt day runs midnight-to-midnight Pacific. The listing that goes live at
12:01 AM has the longest possible runway; listings that go live at 09:00 AM have already lost
9 hours of the competition window.

**Pre-launch checklist on Sunday night (night before launch week begins):**
1. Submit the listing in "upcoming" mode — the Sunday before launch week. This gives a full
   72 hours of "upcoming" discoverability before the Wednesday launch.
2. Verify the cast link resolves. Click it from a private/incognito window.
3. Confirm the backup operator has the credentials and has read this document.
4. Set a phone alarm for 11:55 PM Tuesday Pacific (5 minutes before launch).

**Realistic outcome:** developer tools without an existing audience typically land #3–#8 of the
day on Product Hunt. That outcome is fine — the value of the PH listing for Cooker is the
permanent indexed page and the backlink to docs.cooker.dev, not the #1 badge. Do not measure
success by PH rank alone.

**If the listing is #1 at any point:** do not announce this on social media during the 24-hour
window. Calls to "go upvote us on PH" are a brand-protection violation (strategy.md §7) and
Product Hunt actively watches for coordinated voting. Mention it factually after the window
closes if the outcome is positive.
