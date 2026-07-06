# Frontend & Performance

The engine here is a single bias: **render as much as you can on the server, and ship the least JavaScript that satisfies the interaction the page actually needs.** Most pages are content with islands of interactivity, not applications; treating every page as a client app is the default mistake this section exists to prevent. Every rendering strategy below is scored against that bias, and every performance decision is validated against three numbers — not against a framework's marketing.

Read this when you are on step 5 of the workflow (frontend & performance), or when the user asks "SSR or CSR?", "why is the page slow?", "should we use React Server Components?", or "what's our bundle budget?".

The stack names below — Next.js above all — are **illustrations of a principle**, never a mandate. "ISR in Next.js" means "revalidate-on-a-schedule with whatever your framework calls it" (Nuxt, SvelteKit, Astro, Remix all have the equivalents). Match the incumbent framework first; reach for a named exemplar only when there is no incumbent.

## Core Web Vitals: the three numbers you validate against

Everything downstream is validated against these, and only these three are the ones Google ships as the pass/fail set. A metric is **"good"** when the **75th percentile** across real users (field data, not a lab run on your laptop) is at or below the threshold:

| Metric | Good (p75) | Needs work | Poor | What it measures |
|---|---|---|---|---|
| **LCP** — Largest Contentful Paint | **≤ 2.5 s** | ≤ 4.0 s | > 4.0 s | Time until the largest above-the-fold element (usually the hero image or headline) is painted. Proxy for *loading* speed. |
| **INP** — Interaction to Next Paint | **≤ 200 ms** | ≤ 500 ms | > 500 ms | Worst-case latency from a user interaction (tap/click/keypress) to the next frame, across the whole visit. Proxy for *responsiveness*. |
| **CLS** — Cumulative Layout Shift | **≤ 0.1** | ≤ 0.25 | > 0.25 | Sum of unexpected layout shifts (unitless score). Proxy for *visual stability*. |

**INP replaced FID (First Input Delay) as a Core Web Vital in March 2024.** If a source, audit, or older config still targets FID, it is stale — FID only measured the delay before processing the *first* interaction and almost everything passed it. INP measures the full interaction (input delay + processing + presentation) across *every* interaction in the session and is far harder to pass; it is where slow hydration and heavy main-thread work now show up.

Two diagnostic (not pass/fail) metrics you use to *explain* a bad score:

- **TTFB** — Time To First Byte. A slow LCP that is mostly TTFB is a server/CDN problem (see caching), not a front-end one.
- **FCP** — First Contentful Paint. The gap between FCP and LCP tells you whether the hero asset is the bottleneck.

**Field over lab.** Lab tools (Lighthouse, WebPageTest) are for debugging with a repeatable trace; the numbers that count are **field data** from real devices — Chrome UX Report (CrUX), or your own Real User Monitoring. A green Lighthouse score on a fast laptop over wifi routinely hides a red field INP on a mid-tier Android phone. Design and test against a throttled mid-tier mobile profile, because that is the p75 user.

## Rendering strategy: match the page to the content

The choice is decided by four properties of the page's content, in priority order: **how fresh must it be** (per-request, per-interval, build-time), **how personalized** (per-user vs. same-for-everyone), **how interactive** (static content vs. app shell), and **how SEO/first-paint-critical**. Pick the cheapest strategy that satisfies those — do not server-render a logged-in dashboard for SEO it will never get, and do not client-render a marketing page that lives or dies on LCP and crawlability.

Next.js is the exemplar because it exposes all of these in one framework and lets you choose **per route**, which is the correct granularity — a single app mixes strategies (static marketing pages, ISR product pages, an SSR'd account area, a CSR'd editor).

### CSR — Client-Side Rendering

Server sends a near-empty HTML shell plus a JS bundle; the browser fetches data and renders everything.

- **When it fits**: highly interactive app *behind auth* where SEO and first paint are irrelevant — dashboards, editors, internal tools, canvases, anything that is unmistakably an "application," not a "page."
- **Trade-offs**: cheapest server (static files on a CDN), richest client interactivity. Cost: worst LCP and worst crawlability — the user stares at a spinner while the bundle loads, parses, and fetches. Every byte of JS is on the critical path.
- **Failure modes**: using CSR for content that needs SEO or fast first paint (a blog, a storefront, a landing page); unbounded bundle growth because "it's just an SPA"; the loading-spinner waterfall (shell → JS → API → render).

### SSR — Server-Side Rendering

Server renders full HTML per request, then the client **hydrates** it into an interactive app.

- **When it fits**: content that is **per-request fresh and/or personalized** and must still paint fast and be crawlable — a logged-in feed, search results, a price that depends on the user's region, a checkout page.
- **Trade-offs**: fast, crawlable first paint with live data. Cost: TTFB now includes render time (a slow data call blocks the whole page), the server does work on every request (cache carefully), and you still pay hydration cost on the client.
- **Failure modes**: SSR-ing content that never changes (that's SSG's job — you're paying per-request cost for a constant); a slow upstream call in the render path spiking TTFB and therefore LCP; shipping a huge bundle so the page *looks* ready but isn't interactive (bad INP) until hydration finishes.

### SSG — Static Site Generation

Pages are rendered to HTML **at build time** and served as static files from the CDN.

- **When it fits**: content that is the **same for everyone and changes rarely** — marketing pages, docs, blog posts, changelogs, legal pages. The gold standard for LCP and cost.
- **Trade-offs**: best possible TTFB/LCP (a file from the edge), trivially cacheable, cheapest to serve, most resilient (no server in the hot path). Cost: content is only as fresh as the last build, and **build time grows with page count** — a million-page catalog rebuilt on every content change is a non-starter (that's ISR's cue).
- **Failure modes**: SSG for genuinely dynamic or personalized data (stale prices, wrong user); rebuilding the entire site to fix one typo; build times ballooning past usefulness at scale.

### ISR — Incremental Static Regeneration

SSG's freshness fix: serve the static page from cache, but **revalidate it in the background** on a time interval or on-demand, regenerating just that page.

- **When it fits**: large catalogs of mostly-static-but-occasionally-changing pages — e-commerce product pages, news articles, listings — where a few seconds/minutes of staleness is acceptable and full rebuilds don't scale.
- **Trade-offs**: SSG's edge performance and cost with bounded, per-page freshness; no full rebuild. Cost: staleness window is real (a user can see an old page until revalidation fires), and on-demand invalidation adds a moving part.
- **Failure modes**: using the staleness window for data that must be exact *now* (inventory hitting zero, a live price) — that needs SSR or a client fetch; treating ISR as "SSG but I never have to think about freshness."

### RSC — React Server Components

Components that **run only on the server** and stream serialized UI to the client, sending **zero JavaScript for those components**. Client Components (marked `"use client"`) are the interactive islands that do ship JS. This is the model behind the Next.js App Router.

- **When it fits**: the default for new React apps that mix static-ish content with interactive islands — most real pages. Move data-fetching and heavy, non-interactive rendering to the server; keep only genuinely interactive leaves on the client. Directly attacks bundle size and therefore INP.
- **Trade-offs**: dramatically less client JS (server components add nothing to the bundle), data-fetching co-located on the server (no client waterfall, secrets stay server-side), streaming so the shell paints before slow data resolves. Cost: a genuinely new mental model (the server/client boundary, what can cross it, serialization limits), and a `"use client"` boundary drawn too high pulls a whole subtree back onto the client.
- **Failure modes**: marking everything `"use client"` and getting a CSR app with extra steps; putting server-only secrets or heavy deps in a Client Component; misplacing the boundary so an interactive leaf drags its static ancestors into the bundle.

### PPR — Partial Pre-Rendering

The newest synthesis (Next.js): a single page serves an **instantly-static shell from the edge** while **dynamic holes stream in** within the same response — no separate route-level choice between static and dynamic.

- **When it fits**: pages that are mostly static but have a few per-request/personalized slots — a product page (static description, dynamic price/stock/"recommended for you"), a dashboard with a static frame and live widgets. You get SSG's instant shell *and* SSR's live data on one page.
- **Trade-offs**: best of static + dynamic on the same page — fast LCP from the pre-rendered shell, live data streamed into the holes, no all-or-nothing per route. Cost: it is the **newest and least battle-tested** of these strategies (experimental/stabilizing at the time of writing) — adopt when it's stable in your framework version, not on a bet.
- **Failure modes**: adopting a bleeding-edge feature for a critical path before it's stable; wrapping the whole page in a dynamic boundary so there's no static shell left to pre-render (you've re-invented SSR).

## Decision table: content type → rendering strategy

| Content type | Default strategy | Why |
|---|---|---|
| Marketing / landing / docs / blog (same for all, rarely changes) | **SSG** | Best LCP and cost; a file from the edge. Rebuild on publish. |
| Large catalog, mostly static, occasional updates (products, articles) | **ISR** | Static speed at scale without full rebuilds; bounded staleness. |
| Product/detail page: static frame + live price/stock/personalized slot | **PPR** (else SSG shell + client-fetched slot) | Instant static shell, dynamic holes streamed in. Fall back if PPR isn't stable. |
| Per-request fresh and/or personalized, needs SEO + fast paint (feed, search, checkout) | **SSR** | Live, crawlable, fast first paint. Watch the upstream call in the render path. |
| New React app mixing content with interactive islands | **RSC** (server by default, `"use client"` islands) | Least client JS; server data-fetching; best INP posture. |
| Highly interactive app behind auth, no SEO (dashboard, editor, internal tool) | **CSR** | SEO/first-paint don't matter; ship the app, hold a hard bundle budget. |
| Truly real-time (live prices, collaborative cursors, chat) | SSR/RSC shell + **client subscription** (WebSocket/SSE) | No pre-render is "fresh" enough; stream the live layer on the client. |

The dominant real-world answer for a new project is **server-first with interactive islands** — RSC/SSR for the shell and data, small Client Components for the interactivity, SSG/ISR for the pages that never needed a server. Reach for full CSR only when the page is unambiguously an app behind auth.

## Performance discipline: the budget you hold the line on

Rendering strategy sets your ceiling; discipline is how you avoid giving it back. **The dominant lever on both LCP and INP is the amount of JavaScript on the critical path** — every kilobyte is downloaded, parsed, and executed on the main thread, and on a mid-tier phone that is where your INP goes to die. Hold a budget explicitly; a budget nobody measures is a wish.

**Bundle budget (sane default, adjust to your users):** cap **first-load JS on the critical path at ≈ 130–170 KB compressed** (≈ the amount a mid-tier mobile CPU can parse/execute inside the LCP window). Set the number in CI and **fail the build when a route exceeds it** — a size-limit / bundle-analyzer check on every PR. Regressions arrive one innocent `import` at a time; the gate is the only thing that holds.

The disciplines, each a lever on the numbers above:

- **Code-splitting.** Split by route (and by heavy feature) so a page ships only the JS it needs, not the whole app. The single highest-leverage move for first-load JS. Route-level splitting is automatic in Next.js/most frameworks; split *within* a route for heavy, below-the-fold, or rarely-used features.
- **Tree-shaking.** Rely on ES-module `import`/`export` and side-effect-free packages so bundlers drop dead code. Killers: `import * as X`, non-tree-shakeable libraries (import one icon, ship the whole set), and packages that don't mark `"sideEffects": false`. Import the leaf (`lodash-es/debounce`), never the barrel.
- **Lazy loading.** Defer what isn't needed for first paint: dynamic-import below-the-fold and interaction-triggered components (modals, editors, charts), and `loading="lazy"` off-screen images/iframes. Don't lazy-load the LCP element itself — that delays the metric you're optimizing.
- **Image optimization.** Images are usually the LCP element and the heaviest bytes. Serve modern formats (AVIF/WebP), size to the device with `srcset`/responsive sizes, set explicit `width`/`height` (or `aspect-ratio`) to reserve space and kill CLS, `priority`/preload the LCP image, and lazy-load the rest. Use the framework's image component (Next.js `<Image>`) or a CDN image pipeline rather than hand-rolling.
- **Critical CSS.** Inline the small amount of CSS needed for above-the-fold content and defer the rest, so first paint isn't blocked on the full stylesheet. Watch for render-blocking CSS and unused framework CSS; purge dead classes. Avoid injecting fonts/CSS that cause a late reflow (a CLS source).
- **Hydration cost.** Hydration re-runs component code on the client to attach interactivity — pure overhead that a page has already "shown." Slow or excessive hydration is a leading INP cause. Minimize it: fewer/smaller Client Components (RSC), stream and hydrate progressively, and don't hydrate what is never interactive. A page that *looks* ready but can't respond is failing INP even with a great LCP.
- **Caching & CDN.** Serve static and ISR output from the **edge**; set correct `Cache-Control`/`stale-while-revalidate` so repeat and nearby visits skip the origin. This is the biggest lever on TTFB (and therefore the server-side portion of LCP). Version/fingerprint asset filenames for immutable long-lived caching. See `references/nfr.md` for the caching hierarchy and cache-aside defaults on the server side.
- **Third-party script control.** Analytics, tag managers, chat widgets, and A/B tools are the most common silent LCP/INP wreckers — third-party JS runs on *your* main thread on *your* budget. Audit every tag, load them `async`/`defer` or off the main thread (partytown-style / web worker), lazy-load on interaction, and set a **third-party budget** the same way you set the JS budget. "Marketing added a tag" is a frequent, unattributed regression — put third-party weight behind the same CI gate.

**Smell test** — if first paint is fine but the page feels janky or unresponsive to taps, you have an INP/hydration/third-party-JS problem, not a rendering-strategy problem. If the page is slow to *appear at all*, look at TTFB (server/CDN) and the LCP image before touching JS.

## Data-fetching contract with the API

The frontend's performance depends on the API designed in step 4. Close the loop:

- **Fetch on the server where you can** (SSR/RSC/route handlers) to avoid the client waterfall (shell → JS → API → render) and to keep credentials server-side.
- **Batch and shape** so a page isn't making a dozen sequential client calls; this is a case where GraphQL or a purpose-built BFF endpoint earns its keep (see `references/api-design.md`).
- **Cursor-paginate** long lists (step 4's default) so infinite scroll stays stable under concurrent writes.
- **Cache at the fetch layer** with clear revalidation, and reflect the API's cache headers rather than fighting them.

A rendering strategy chosen without its data-fetching contract is half a decision — SSR in front of a slow, chatty API just moves the waterfall to the server and spikes TTFB.

## Exemplars (illustrations only)

Named to make a choice concrete, **not** as targets to copy — each fits a specific content profile, which may not be yours:

- **Next.js** — the exemplar throughout because it exposes SSG, ISR, SSR, RSC, and PPR and lets you pick **per route**; the App Router makes RSC the default. Read its strategy names as the vocabulary, then map to your framework's equivalents (Nuxt, SvelteKit, Astro, Remix).
- **Astro** — "islands architecture" taken to its conclusion: static HTML by default, ship JS only for explicitly-hydrated islands. The reference point for content-heavy sites that want near-zero client JS.
- **CrUX / PageSpeed Insights** — the field-data source that decides whether your Core Web Vitals actually pass at p75; the arbiter your Lighthouse score is only a proxy for.

Read each as "here is where this strategy's content profile was genuinely present," then check whether *your* page has the same profile. If it doesn't, the exemplar argues against copying it.
