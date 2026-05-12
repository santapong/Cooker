# Cooker — Marketing strategy (open-source adoption)

> Goal: open-source adoption. Not SaaS. Not dual-license. Not "open core."
> Horizon: 90 days post-launch.
> Status: draft. Operational, not aspirational.
> Author: marketing lead, 2026-05.

This plan is constrained by the two audits on this branch:

- `docs/shipping-go.md` — we **do not ship binaries today**. No GoReleaser, no signed releases, no published Helm chart, no `cooker --version`. Marketing cannot launch until that lands.
- `docs/audits/2026-05-security-review.md` — no CRITICAL findings, but four open HIGH issues including IDOR on every per-resource read (`S26-05-09`). The marketing posture cannot claim multi-tenant safety.

Everything below assumes both audits' Wave-1 work is closed before launch day.

---

## 1. Positioning

### What Cooker is

**Cooker is an open-source CI/CD tool with a graph-based UI for building Docker images and deploying to Kubernetes — single Go binary, no agents, no SaaS, no asterisks.**

That sentence is the one we test on a stranger. If they don't get "visual pipeline + builds containers + deploys to K8s + self-hosted Go binary" in five seconds, we rewrite.

### Who it's for (ICP)

From `docs/audits/W11-user-journeys.md`, three personas matter:

1. **Indie hacker / solo dev on a single-node k3s.** Wants `git push → live URL` in five minutes. Will not read a wiki. Pays $0–$10/month for everything. **Primary persona for launch.**
2. **SaaS platform team (50-person company), one OIDC provider, three environments.** Wants Dev → Staging → Prod with one approval click. Will read the docs once. **Secondary persona for launch.**
3. **Enterprise SRE / platform engineer.** Compliance-first, multi-cluster, Vault. **Not** the launch audience — Cooker is single-tenant today (`S26-05-09`) and we will not pretend otherwise.

The fourth W11 persona — the ML engineer — is interesting but blocked on build-cache plumbing and `nodeSelector` exposure. We do not market to them yet.

### Who we're explicitly NOT for

- Anyone who needs multi-tenancy or hard team isolation. State this in the README.
- Anyone whose CI lives in YAML and likes it there. We will not win the "YAML-as-code" argument; don't fight it.
- Windows shops. Cooker shells `kubectl` and `docker`; Windows is a stretch.
- Anyone needing SAML. OIDC only.
- Hosted-SaaS shoppers. We don't run one. There may be one later. Don't promise it.

### Against — competitive landscape

| Tool | Where they win | Where we win |
|---|---|---|
| **GitHub Actions** | Free for public repos, runs everywhere, infinite ecosystem | We're self-hosted, visual, and the deploy story is first-class — Actions makes you build deploy yourself |
| **Drone** | Mature, fast, container-native | We're OSS-with-no-EE-asterisk; Drone has a commercial license tier and Harness now owns it |
| **Woodpecker** | Drone-compatible OSS fork, clean Go codebase | We have a graph UI for pipelines, native K8s deploy, and multi-env promotion built in — Woodpecker is YAML-only |
| **Concourse** | Powerful pipeline semantics, strong for monorepos | Concourse's UX is famously a "you'll get used to it" — we look like a flowchart and onboard in minutes |
| **Argo Workflows** | K8s-native, scales for ML/data pipelines | We're a single binary; Argo needs three controllers, a UI install, and CRD plumbing before "hello world" |
| **Jenkins X** | Mature K8s GitOps story | We don't require GitOps, no `jx` CLI to install, no Lighthouse, no Tekton underneath |
| **Dagger** | Programmable pipelines in Go/Python/TS | Dagger is a SDK, not a server — different shape. We compete only on "I want a UI to look at my last 20 runs" |
| **Tekton** | The K8s-native primitives layer | Tekton is plumbing; we're the product on top. We don't compete; we coexist |
| **CircleCI / Buildkite** | Polished SaaS, deep features | We're free, self-hosted, OSS |

### The one thing we lean on

**Graph-first UX for self-hosted CI/CD.**

The differentiator is not "OSS" (Woodpecker is). Not "single binary" (Caddy got there first; ours just happens to be too). Not "K8s-native" (Argo / Jenkins X). It is the visual graph editor combined with a real deploy story — drag a Build node, drag a Push node, drag a Deploy node, draw arrows, click Run. Other graph CIs exist (Concourse, GoCD); none of them feel like React Flow in 2026.

Why we picked this over "single-binary simplicity":
- Single-binary is a feature people *notice* after they're already trying us, not a hook that gets them to try.
- The visual graph is screenshot-ready. It's what wins HN front-page real estate.
- It's the one thing we genuinely do that no major competitor does.

Risk: if the demo video is bad, the differentiator collapses. The 60-second cast is the single highest-leverage marketing asset we will produce.

---

## 2. Naming, voice, visuals

### Tagline — 3 options, pick one

1. **"CI/CD you can see."** — short, declarative, names the differentiator. Picked.
2. "Drag, drop, deploy." — alliterative, fun, slightly too cute for an infra-tool audience.
3. "Pipelines without the YAML wall." — picks a fight with YAML CI; we said we wouldn't.

**Chosen: "CI/CD you can see."** Defence: it earns the screenshot. Every visual asset we produce reinforces the tagline. It also signals the OSS / self-host posture by what it doesn't say ("from your terminal," "in your cloud," etc. — none of those).

### Voice & tone

**Dos**
- Speak like a senior engineer explaining a tool to a peer. No exclamation marks. No "delightful." No "magic."
- Lead with what's true today. If something is on the roadmap, say "planned" or "not yet."
- Use concrete numbers (`8080`, `UID 65532`, `60s ticket TTL`) when they earn attention.

**Don'ts**
- Don't say "enterprise-ready" — we have an open IDOR (`S26-05-09`).
- Don't say "secure by default" without naming what — the security review reads back.
- Don't use the word "revolutionary." Or "game-changing." Or "next-gen."

### Logo / wordmark direction

- Lowercase **"cooker"** wordmark, monospaced. Suggested face: **IBM Plex Mono** or **JetBrains Mono**.
- Glyph: a stylised pot with a single steam-curl coming off it — abstract enough to read as a chevron or a wave at small sizes.
- README hero: ASCII rendering of the wordmark — `cooker` in mono, three steam-lines above the second `o`. Black-on-white, no gradients.
- Colour: one accent (warm amber, `#E07A1F`-ish). Everything else greyscale. No purple-to-pink gradients.

Do not commission a logo before launch. The wordmark on its own is sufficient. A logo procurement decision can wait until 90 days post-launch.

### Hero terminal cast on the landing page

A 60-second asciinema (or equivalent) showing:

```
$ docker compose up        # ~10s
$ open http://localhost:8080
[user drags Build → Test → Push → Deploy nodes; draws edges; clicks Run]
[live log stream fills the right panel]
[final node turns green, image pushed, deployment rolled out]
```

No voiceover. Captions only. Embed on the landing page above the fold. This **is** the hero. If we don't have this cast, we don't launch.

---

## 3. Channels & content plan

### GitHub repo itself

- **Audience:** anyone who clicks a link to the repo.
- **Frequency:** continuous; the README is the front door.
- **Format:** README with hero cast, GIF in the first 200px, badges (build, license, latest release, OpenSSF Scorecard once it ships), `Topics` set (`ci`, `cd`, `kubernetes`, `docker`, `oci`, `golang`, `react`, `pipeline`, `self-hosted`).
- **Success metric:** stars per week; repo → docs-site click-through.
- **Owner:** maintainer.
- **Inclusions to chase:** `awesome-selfhosted`, `awesome-go`, `awesome-ci-cd`, `awesome-kubernetes-tools`. PR to each as soon as v0.1.0 has artifacts.

### Hacker News — the launch post

- **Audience:** the one channel that delivers a 4-figure star spike on day one. Also the most ruthless.
- **Frequency:** one launch post. One.
- **Format:** Show HN. Posted Monday or Tuesday, 09:00 ET.
- **Owner:** maintainer.

Three candidate titles (pick on launch morning):

1. **Show HN: Cooker — open-source CI/CD with a graph UI, single Go binary**
2. **Show HN: Cooker — drag, drop, deploy. Self-hosted CI/CD for Kubernetes**
3. **Show HN: Cooker — what if your CI pipeline looked like the diagram you drew on a whiteboard**

Opening 200 words drafted in §4.

### Reddit

Posted on different days to avoid cross-subreddit spam reports. Each post is rewritten for the subreddit; the same copy reposted across subs gets flagged.

| Subreddit | Why | Angle | Post day |
|---|---|---|---|
| **r/selfhosted** (1.1M) | Indie-hacker home base — our persona-1 lives here | "Self-hosted CI/CD on a single VPS — show me what you build" | Tue |
| **r/devops** (700k) | Persona-2 audience; high signal on tool flame-wars | "Open-source graph-based CI/CD — feedback wanted from people who hate YAML" | Wed |
| **r/kubernetes** (250k) | K8s deploy story is core | "Deploying to Kubernetes from a visual pipeline editor — built in Go" | Wed (alt day) |
| **r/golang** (250k) | Go-community recognition; recruits contributors | Technical post: "How we built a DAG executor and a visual editor that share the same data model" | Thu |
| **r/programming** | Long shot; high reward if it lands | Re-share of the dev.to deep-dive article | Fri |

Cadence: one post per sub max in launch week, then nothing for a month. **No reposting**.

### Dev.to / Hashnode

Five launch articles, one per week starting from launch week. Cross-post on Hashnode and the docs site.

1. **"Show HN debrief: what shipping a graph-based CI tool taught us about cold launches"** — the recap. Honest numbers.
2. **"Why we built a CI/CD tool with a DAG editor when YAML works fine"** — the differentiator essay. The piece you link to forever.
3. **"From `docker compose up` to your first green pipeline run in 60 seconds"** — embed the hero cast; literal walk-through.
4. **"The single-binary deploy story: a tour of Cooker's Helm chart"** — operator-flavoured.
5. **"OCI compliance for people who haven't read the spec"** — the unique-to-us angle; references `internal/oci/` and the OCI conformance workflow.

Each article has a concrete CTA at the bottom: "Star the repo," "Try the demo," "Open an issue."

### Twitter/X & Bluesky & Mastodon (Fosstodon)

- **Audience:** maintainer-network amplification, not customer discovery.
- **Frequency:** 2–3 posts per week through launch month; 1/week thereafter.
- **Format:** one of:
  - A 30-second screen-cap of a feature.
  - A "before / after" of a config (cribbed from a real user issue).
  - A milestone tweet (first external PR, 100th star, etc.).
- **Success metric:** retweets/boosts from infra-Twitter regulars. Followers are a vanity number.
- **Posting cadence rule:** if there's nothing to show, don't post.

Mastodon (Fosstodon specifically) is the right pick — it's where the OSS / Go / K8s crowd actually engages. Bluesky is upside; Twitter/X is unfortunate but unavoidable.

### YouTube

- **Audience:** mid-funnel — people who clicked the README, are interested, want to see motion before they `git clone`.
- **Frequency:** one video for launch. One follow-up per month.
- **Format (launch):** 8-minute demo. Voice-over, no face-cam. Recorded screen of: install via Helm, create a pipeline, run a pipeline, fail a pipeline, deploy.
- **Success metric:** average view duration > 4 minutes (= half the video). Sub count is vanity.
- **Owner:** maintainer + a friend who edits.

Long-form (45-minute architecture deep-dives, conference talks) — defer to month 3+ if there's audience demand.

### Conference / meetup

- **Realistic talks for 2026:**
  - **KubeCon EU 2026** (CFP closed for May; aim Nov 2026). Talk title: "Drag-drop-deploy: a visual editor for K8s pipelines, in Go."
  - **FOSDEM 2027** (CFP opens autumn 2026). Devroom: Continuous Integration & Continuous Delivery, or the Go devroom. Talk: "OSS CI without the YAML."
  - **GopherCon 2026** (US). Talk: "Building a graph-based DAG runner in Go."
  - **SREcon 2026 EMEA/Americas**. Talk: only after we have an in-product audit-log viewer (otherwise we'll get torn apart on the panel).
  - **Local Go meetups** — Bangkok Gophers, London Gophers, Berlin Go meetup, NYC Golang — easier accept rate; smaller stakes. Apply rolling.
- **What we are not doing in 2026:** booth / sponsorship spend. Zero budget.

### Podcast outreach

Five podcasts that fit, with the hook:

1. **The Changelog** — "We built an OSS CI/CD tool because we got tired of YAML." Pitch the visual-editor angle.
2. **Go Time** — "The DAG runner pattern: how Cooker's `pkg/dagrunner` works." Pure-Go technical angle.
3. **The Kubernetes Podcast (Google)** — "A single-binary deploy story for K8s without operators." Single-binary angle.
4. **PodCTL / Kubernetes Bytes** — "Graph-based CI/CD: where it fits next to Argo CD." Coexistence angle.
5. **Software Engineering Daily** — "OSS sustainability: building an alternative to commercial CI." Business-model angle.

Pitch by email, one paragraph, three bullet points each. Track in a spreadsheet. Do not chase if no response in two weeks.

### Comparison content (SEO long-tail)

These articles capture search intent. They take a week each to write well.

- "Cooker vs Drone: an honest comparison" — published month 2.
- "Cooker vs Woodpecker CI: which OSS pipeline?" — month 2.
- "Cooker vs Argo Workflows for app deploys" — month 3.
- "Cooker vs Jenkins X" — month 3, lower priority.
- "Cooker vs GitHub Actions for self-hosters" — month 1; this is the one most people will search for.

Hosted on the docs site under `/compare/`. Each piece is honest — we list where the competitor wins. Linking out is fine; it builds trust.

### Docs as marketing

The user-guide (separate agent) is also a marketing surface. Its landing pages should:

- Have a "60-second quickstart" as the first page after the index.
- Show a screenshot of the graph editor on the landing page itself, not behind a click.
- Include "From scratch to deployed in 5 minutes" as a documented path with literal copy-paste commands.
- Include a "What's not done yet" page that mirrors `backlog.md`'s priority list. Honesty earns trust.
- Have a `/compare/` section as above.

---

## 4. The launch

### Preconditions — we do NOT launch until all of these are true

1. **GoReleaser pipeline shipping v0.1.0 binaries.** From `docs/shipping-go.md` 0–30d plan: tag → linux/darwin × amd64/arm64 tarballs, multi-arch Docker image at `ghcr.io/santapong/cooker`, checksums. **Cosign signing is desirable but not strictly required for launch day.**
2. **Helm OCI chart published** at `oci://ghcr.io/santapong/charts/cooker`. Documented in README.
3. **60-second hero cast.** Recorded, captioned, embedded on README and docs landing page.
4. **Working `docker compose up` quickstart.** Tested end-to-end on a clean machine by a person who isn't the maintainer.
5. **README.md rewritten** to lead with the visual-editor differentiator. Hero cast or GIF in the first viewport. Demo URL above the install commands.
6. **"What's not done yet" section** in the README, honest about: single-tenant only (`S26-05-09`); no audit-log viewer; no PR-preview envs; no bulk import. Don't oversell.
7. **`docs/audits/2026-05-security-review.md` Quick-Wins 1–6 landed.** Particularly `S26-05-04` (raw-manifest docker.sock removal), `S26-05-10` (sslmode enforcement), `S26-05-13` (default Postgres password). If a HN reader reads the security audit on the same branch as the launch post, the open HIGHs must be a smaller list than today.
8. **A docs site** at `docs.cooker.dev` (MkDocs Material, free Cloudflare Pages or GitHub Pages). Even if it's just rendering the existing `docs/` directory.

If any of these are missing, the launch is delayed by a week. There is no "we'll fix it after launch" path here — the cold-launch traffic happens once.

### Launch-week schedule (day by day)

All times America/Eastern. Pick a week with no major US holidays and no AWS re:Invent / KubeCon clash.

| Day | What | Owner | Why this day |
|---|---|---|---|
| **Mon 09:00** | Show HN post goes live | maintainer | HN's Monday morning is the highest-traffic window |
| **Mon 09:00–21:00** | HN comment-watch shift (see below) | maintainer + 1 backup | First 12 hours decide the trajectory |
| **Mon 17:00** | Mastodon (Fosstodon) post with cast + link | maintainer | After HN traction is visible |
| **Tue 10:00** | r/selfhosted post | maintainer | Indie-hacker audience |
| **Tue evening** | Dev.to article #1 (Show HN debrief, honest numbers) published | maintainer | Captures HN recap traffic |
| **Wed 10:00** | r/devops + r/kubernetes posts (different angles) | maintainer | Mid-week is highest /r/devops engagement |
| **Wed 18:00** | YouTube 8-minute demo goes public | maintainer | Now ready to handle inbound traffic with motion |
| **Thu 10:00** | r/golang post (technical angle) | maintainer | Recruits Go contributors |
| **Thu evening** | Dev.to article #2 ("why a DAG editor when YAML works") | maintainer | Steady drip |
| **Fri 11:00** | Twitter/X + Bluesky launch-recap thread; tag conference / podcast outreach starts | maintainer | Friday afternoon = light news cycle |
| **Fri** | First retrospective: stars, traffic, top objections, what to fix in week 2 | maintainer | Decisions made cold while data is fresh |

If HN performs (top-30 for 6+ hours), shift weeks 2–4 toward inbound issue triage. If it doesn't, switch the order: skip r/programming, double down on r/selfhosted with a quickstart-focused angle.

### The HN post — actual draft

**Title (pick one Monday morning):**
- **Show HN: Cooker — open-source CI/CD with a graph UI, single Go binary**

**Opening two paragraphs:**

> Hi HN. I built Cooker because every time I started a side-project I'd lose the first hour to a CI config that I'd already written four times. I wanted to see my pipeline, not type it.
>
> Cooker is a single Go binary that serves both an HTTP API and a React front-end. You drag Build / Test / Push / Deploy nodes onto a canvas, draw arrows between them, and click Run. It builds OCI images (your choice of docker / Kaniko / buildah / BuildKit), pushes them to any OCI-compliant registry, and deploys to Kubernetes. There's no SaaS, no agent, no asterisks — `docker compose up` and you've got a working install. Demo cast on the README. Source at github.com/santapong/cooker, MIT.

**"What's not done yet" section (must be in the comments by minute five):**

> What's not done yet, since this is HN and someone will ask:
>
> - **Single-tenant.** Every authenticated user can read every pipeline / app / environment. This is documented; multi-tenancy is on the roadmap but I'm not going to pretend it ships today.
> - **No in-product audit-log viewer.** The audit middleware is in place and writes structured JSON; you'd have to query it from your log stack today.
> - **No PR-preview environments.** Possible to assemble manually via the pipeline editor; no first-class per-PR ephemeral env yet.
> - **Builder choice matters.** The `docker` builder mounts the host docker socket and is dev-only. Kaniko / buildah / BuildKit are the production options.
> - **Bus factor of one.** That's me. PRs welcome — there's a CONTRIBUTING.md and a labelled list of "good first issues."
>
> Happy to answer anything.

This pre-empts the harshest comments. The HN reader who scrolls comments first sees we're not pretending.

### Comment-watching plan

**First 12 hours after submission**: maintainer on-duty. One trusted friend on standby for the second 12 hours. Goal: every comment gets a response within 30 minutes during the launch window; within 2 hours overnight.

**Objections to expect** (and prepared answers):

| Objection | Prepared response |
|---|---|
| "Why not Argo Workflows / Argo CD?" | Argo is K8s-native for ML/data pipelines; we're focused on the visual-editor + deploy-app shape. They coexist; we don't compete. Linked comparison post. |
| "Why not Drone / Woodpecker?" | Both are YAML-first; the differentiator is the visual graph. Woodpecker is excellent and OSS — we'd recommend it for YAML-loving teams. |
| "Single tenant means I can't use this." | Correct — we are not pitching this to multi-tenant SaaS platforms today. Roadmap item, documented in README. |
| "Another CI tool, why?" | Because every existing OSS CI is either YAML-only or has a UI we wouldn't show to anyone. Specific opinion, willing to defend. |
| "How is the visual editor different from Concourse?" | Concourse's UI is read-only and famously hard to learn; ours is the *editor*. Drag-drop, not "look at the YAML you already wrote." |
| "License?" | MIT, full repo, no EE/CE split. |
| "What about Windows?" | Not supported. Shells to `kubectl` and `docker`. Not on the roadmap. |
| "Why React Flow / why TypeScript?" | We needed a graph editor that didn't take 6 months to write. React Flow ships. The whole frontend is replaceable; nothing in the backend cares. |
| "Why Go?" | Native Docker / K8s / OCI SDKs. No FFI. Single binary. Standard answer. |
| "Security audit?" | Yes — `docs/audits/2026-05-security-review.md` on the repo. Four open HIGHs, all named, all tracked. We did this audit ourselves and published it. |
| "Looks abandoned / one-person project?" | Currently one maintainer. Bus factor of one. Documented. Looking for contributors; "good first issue" labels active. |
| "Show me a multi-cluster deploy" | Honest answer: works at the deploy-target level; UI doesn't surface "which cluster" prominently yet (`W11` persona-3 gap). Tracked. |
| "Is there a hosted version?" | No. May be later. Today is OSS-only. |

Anything we don't have an answer to: "Good question — I don't have a satisfying answer right now. Filed at github.com/.../issues/NN." Never bluff.

---

## 5. 30 / 60 / 90 day plan

Each week: 1 content piece, 1 product polish, 1 community action.

### Days 0–30 — "Survive the launch, ship v0.1.1"

| Week | Content | Product polish | Community |
|---|---|---|---|
| 1 (launch) | HN post + dev.to #1 + YouTube demo | Hotfix v0.1.1 from launch-week bug reports | Reply to every comment & issue within 24h |
| 2 | Dev.to #2 ("why DAG editor over YAML") | Land one P2 from `W11` (e.g. empty-state CTAs, build-recipe auto-detect) | First "good first issue" labelled; recruit one contributor |
| 3 | Dev.to #3 (60s quickstart walkthrough) | Webhook URL surfaced on AppDetailPage | Add `awesome-selfhosted` PR; reach out to first podcast |
| 4 | Comparison: Cooker vs GitHub Actions | Ship `cooker --version`, embedded migrations sanity test | First external PR merged — celebrate publicly |

**Day 30 milestone:** 200+ stars, 1+ external contributor, v0.1.1 released, awesome-list inclusion live.

### Days 30–60 — "Reduce the persona-2 friction"

| Week | Content | Product polish | Community |
|---|---|---|---|
| 5 | Dev.to #4 (Helm chart tour) | Audit-log viewer scaffolding (the W11 P1) | Open a Discord OR Matrix server — commit to one. Pin the link. |
| 6 | Comparison: Cooker vs Drone | OIDC quickstart docs (Keycloak in Docker compose) | Office hours: 1 hour live on YouTube, Q&A |
| 7 | Dev.to #5 (OCI compliance for the uninitiated) | Per-environment secret diff view (W11 P2) | Submit KubeCon Nov 2026 CFP |
| 8 | Comparison: Cooker vs Woodpecker | First small UX polish PR from external contributor merged | Second podcast pitch round |

**Day 60 milestone:** 400+ stars, 3+ external contributors, audit-log viewer GA, first podcast appearance booked or aired.

### Days 60–90 — "Earn the second look"

| Week | Content | Product polish | Community |
|---|---|---|---|
| 9 | Architecture deep-dive (DAG runner internals) | Build-cache plumbing for Kaniko (W11 P1) | Conference talk submitted to a regional Go meetup |
| 10 | Operator's blog: "running Cooker for 60 days, what we learned" | First SOC2-shaped feature: append-only audit log adapter | Discord/Matrix office hours |
| 11 | Comparison: Cooker vs Argo Workflows | `cooker config print` + `cooker config validate` (shipping-go 30-90 item) | Recruit a second maintainer (formal CONTRIBUTING handoff) |
| 12 | 90-day retrospective | First v0.2.0 release with the cumulative quarter's work | Decide: is the bet working? See §6 metrics |

**Day 90 milestone:** 500+ stars, 5+ external contributors, v0.2.0 shipped, the differentiator-validation decision made.

---

## 6. Metrics — what we're measuring

### Targets

| Metric | Day 7 | Day 30 | Day 60 | Day 90 | Notes |
|---|---|---|---|---|---|
| GitHub stars | 200 | 350 | 600 | 1000 | HN front page is +400 on a good day |
| Docker image pulls (GHCR) | 200 | 1000 | 2500 | 5000 | Reflects "did they try it" |
| Helm chart pulls | 40 | 200 | 500 | 1200 | Lower numbers but higher-intent users |
| Unique docs-site visitors | 1500 | 4000 | 8000 | 15000 | Cloudflare Analytics or Plausible |
| Discord / Matrix members | n/a | 25 | 75 | 200 | Commit by day 30 |
| External contributors with merged PR | 0 | 1 | 3 | 5 | The single most important number |
| GitHub issues opened (any) | 30 | 100 | 200 | 350 | Engagement signal; not all positive |
| "Time to first run" survey median (minutes) | n/a | 12 | 8 | 6 | Survey on the docs site |

### What success looks like

- 1000+ stars by day 90.
- 5+ external contributors with merged PRs.
- Two podcast appearances or one conference talk accepted.
- The visual-editor screenshot is the lede image when someone writes about us.

### Honest definition of failure

If at day 90 we are:

- Below 300 stars, AND
- Zero external contributors, AND
- No coverage outside our own posts

…then the **visual-editor differentiator is wrong for this audience**. The contingency plan: pivot the pitch to "single-binary K8s deploy story" and rerun the launch in month 4 with a different headline. The product doesn't change; the pitch does.

If at day 90 we have great star velocity but zero contributors, the problem is **CONTRIBUTING.md, issue labels, or response time** — not the product. Fix that, not the pitch.

---

## 7. Brand-protection list — what we do NOT do

- **No astroturfing.** No upvote rings. No friends posting "wow great project" on HN. The HN moderators are good and the community sniffs it out.
- **No inflated numbers.** If we have 200 stars, we say 200 stars. We do not say "thousands of developers."
- **No overselling auth/multi-tenancy.** Cross-ref `S26-05-09`. Every claim about "secure" or "team-ready" or "enterprise" must survive a read of the security audit on the same branch.
- **No "we use AI to…" framing.** Cooker has zero LLM features in the product. If it ever does, it'll be optional and named. The brand voice is engineer-to-engineer.
- **No begging for stars.** No "if you found this useful, please star." No README "smash that star button." It cheapens the project.
- **No picking fights** with Drone or Woodpecker. Both are good projects with respectful authors. Where we win, we name it factually. Where they win, we say so.
- **No deleting unflattering issues or comments.** If a user reports a bug, the bug exists. The fix is to fix it, not to delete the report.
- **No private "early access" lists.** Everything is OSS, all at once, for everyone.
- **No press-release tone.** Ever. The voice is a senior engineer talking to peers. If a sentence sounds like a marketing department wrote it, rewrite it.
- **No mentioning hosted Cooker** until we've decided that's the business model. Today there is no hosted offering.

---

## 8. Budget & resources

### Human-hours per week, sustained 90 days

| Activity | Hours / week |
|---|---|
| Content (one piece per week, average) | 4 |
| Community: issue triage, PR review, Discord/Matrix presence | 5 |
| Conference / podcast outreach + prep | 1 (avg; spiky) |
| Social posts (low-volume cadence) | 1 |
| Demo / cast / video production (avg over month) | 2 |
| Product polish that the marketing plan needs (empty-state CTAs, hero cast updates, etc.) | 3 |
| **Total** | **~16 hours / week** |

That's about two days a week. Sustainable for one maintainer if it's their main focus; not sustainable as a side project alongside the actual code. Honest assessment: if maintainer time is split, we hit ~10 hours and the 90-day metrics shift downward by ~30%.

The plan **assumes** we land one external contributor by day 30 who absorbs ~3 hours/week of triage and content review. If that doesn't happen, the metrics slip; that's fine, but we name it.

### Money

**Free**

- GitHub repo, GitHub Releases, GHCR (image + chart registry)
- Cloudflare Pages or GitHub Pages for docs hosting
- MkDocs Material (open-source)
- Discord (free tier) or Matrix (self-hosted on existing infra)
- Asciinema / Plausible (free tier) / OpenSSF Scorecard
- Domain registration (~$15/year for `cooker.dev` or similar — counted as $0 because trivial)

**Has a cost**

| Item | Cost | When |
|---|---|---|
| Domain `cooker.dev` (or alt) | ~$15/year | Now |
| Domain `cooker.io` defensive registration | ~$40/year | Optional, before launch |
| YouTube video editing (if not in-house) | $200–500 / video | Month 1, ad hoc thereafter |
| Newsletter platform (Buttondown / Beehiiv free tier OK for <1000 subscribers) | $0–$10/month | Month 2+ if we start a newsletter |
| Conference travel (if a talk accepted) | $1000–3000 per event | Month 6+ |

**Not yet**

- Conference booth / sponsorship: zero budget for 2026.
- Paid ads (Google / Twitter / Reddit): not in the plan. Open-source adoption doesn't buy. If we ever do paid, it's not before day 180.
- Hosted-docs services (Mintlify, ReadtheDocs commercial): no — MkDocs + Cloudflare Pages is free and good enough.
- Logo design firm: defer until month 6+.

**90-day total spend: under $100** assuming maintainer does video editing or asks a friend. Under $1000 if we outsource one video per month. This is a labour problem, not a money problem.

---

## Cross-references

- Personas — `docs/audits/W11-user-journeys.md`.
- Why we can't launch yet — `docs/shipping-go.md` (0–30 day plan).
- What we can honestly claim today — `docs/audits/2026-05-security-review.md`.
- Open work — `backlog.md`.
- Architecture map for content writers — `docs/architecture.md`.
- Threat model that this strategy is constrained by — `SECURITY.md`.

*End of document.*
