# Cooker — Announcement & Outreach Plan (v1)

> Extends strategy.md §3–§4. Does not rewrite them.
> Date: 2026-06-21. Author: announcement & outreach strategist.
> All times America/Eastern unless noted.

---

## Preconditions reminder (strategy.md §4)

Do not read anything below as actionable until all eight preconditions in §4 are met: GoReleaser v0.1.0 binaries, Helm OCI chart, 60-second hero cast, clean `docker compose up` on a fresh machine, rewritten README, "what's not done yet" section, Quick-Wins 1–6 from the security audit, and a live docs site. The hero cast is the single hard gate — if it does not exist, no channel listed here can compensate.

---

## Channel-by-channel plan

### 1. Hacker News — Show HN

strategy.md §3–§4 covers the title candidates, draft, and comment-watch plan. Additions only:

- **Comment-watch SLA**: every comment within 30 min during 09:00–21:00 ET on Monday; within 2 hours overnight. The backup person needs the HN login.
- **Flagging signal**: if the post is flagged or stalls below rank 60 by 11:00, do not try to rescue it with social amplification — that triggers more flagging. Let it run.
- **Second-chance path**: if the first Show HN lands below top 30 for fewer than 3 hours, wait 6 months before a second attempt. A "who is hiring?"-style comment in the thread for future contributors is always acceptable.

### 2. Reddit

strategy.md §3 sets the subreddit list and cadence. One addition per sub:

| Subreddit | Extra guidance |
|---|---|
| **r/selfhosted** | Lead with the `docker compose up` angle; link directly to the 60-second cast. The sub upvotes things it can run tonight. |
| **r/devops** | The YAML objection is this sub's currency. The title "feedback wanted from people who hate YAML" (§3) earns engagement; be ready to defend the graph-editor claim with the cast. |
| **r/kubernetes** | Frame around the deploy story, not the CI story. K8s readers care about what happens after the image is built. |
| **r/golang** | Technical post, not a product post. Lead with a real architectural decision — the DAG executor sharing a data model with the React graph is the hook. |
| **r/programming** | Only post here if r/selfhosted and r/devops both break the top 20 in their respective hot feeds. If they don't, skip. |

No cross-posting the same text. No upvote solicitation in any post. No DM campaigns to get upvotes. (Brand rule.)

### 3. Product Hunt

Not covered in strategy.md §3–§4. Adding it here.

- **When**: Wednesday of launch week (day 3), 12:01 AM Pacific. Tuesday–Thursday are peak-traffic days; Wednesday avoids competing with major Monday launches and is the recommended slot for open-source developer tools per current Product Hunt playbooks (see sources). ASSUMPTION: Product Hunt listing will be claimed and maker account aged 30+ days before launch; this requires setup ~5 weeks before launch day.
- **Angle**: "Open-source CI/CD with a visual pipeline editor — single Go binary, self-hosted." Not "the Coolify of CI/CD" — that risks a brand comparison we don't control.
- **Assets needed**: a 240×240 product icon (the wordmark glyph from strategy.md §2), a 1270×760 banner showing the graph editor with a completed run, the 60-second cast or a 90-second screen-recorded edit of it, and three carousel screenshots (graph editor, live log stream, deploy diff view).
- **Tagline** (60 chars max): `CI/CD you can see. Self-hosted. Single binary. MIT.`
- **Owner**: maintainer, with one friend owning comment responses on PH for the 24-hour window.
- **Comment discipline**: respond to every comment within 2 hours. No "thanks for the upvote." No hunter kickbacks. (Brand rule.)
- **Realistic outcome**: developer tools without an existing audience typically land #3–#8 of the day. That is fine — PH's value for this product is the permanent listing and the SEO backlink, not the #1 badge.

### 4. Dev.to / Hashnode

strategy.md §3 lists five articles with titles. Operational additions:

- Cross-post all five to Hashnode with canonical URL pointing to dev.to.
- Add a `cooker` tag and the `oci`, `kubernetes`, `go`, `selfhosted` tags on both platforms.
- Article #1 (Show HN debrief) should go up Tuesday evening of launch week while the HN thread is still warm — it captures the search traffic from people who missed the HN post.
- Each article's CTA: "Try it: `docker compose up` — readme at github.com/santapong/cooker." Do not add "please star." (Brand rule.)

### 5. X / Bluesky / Mastodon (Fosstodon)

strategy.md §3 covers cadence and format. Three additions:

- **Bluesky starter pack**: on launch Monday, post the hero cast as a native video upload (not a YouTube link). Bluesky's algorithm currently favours native video significantly over external links. ASSUMPTION: this is an algorithm behaviour observed in mid-2025; verify before launch.
- **Mastodon timing**: post to Fosstodon at 17:00 ET Monday (after HN trajectory is readable). Fosstodon's peak engagement window is 14:00–20:00 UTC; 17:00 ET = 21:00 UTC, which is slightly late — post at 16:00 ET instead.
- **X/Twitter**: post a 4-tweet thread at launch (cast, graph-editor screenshot, "what's not done yet" bullet, repo link). Do not post "smash that star" language anywhere. (Brand rule.) Use `#DevOps #Kubernetes #Golang #selfhosted` — no more than four tags.

### 6. YouTube

strategy.md §3 covers the 8-minute demo format. Addition: publish the YouTube video Wednesday 18:00 ET (strategy.md §4 already has this). Use chapters in the description: 0:00 Install, 1:30 Create pipeline, 3:45 Run and watch logs, 5:30 Deploy to Kubernetes, 7:00 What's not done yet. The "what's not done yet" chapter is required — it follows the HN post's honest tone and gives the viewer the same caveat the technical community expects.

### 7. Newsletters

strategy.md §3 does not name specific newsletters. These are the ones worth targeting, in priority order:

| Newsletter | Audience | Pitch approach | Timing |
|---|---|---|---|
| **DevOps Weekly** (Gareth Rushgrove, ~25k subscribers, devopsweekly.com) | Practitioner DevOps engineers | Submit via the site's submission form. One sentence: "Open-source graph-based CI/CD, single Go binary, self-hosted — link to Show HN thread." No pitch email needed; Gareth curates from submissions. | Submit on Monday of launch week, after the HN post is live. |
| **Go Weekly** (Cooper Press, ~30k subscribers, golangweekly.com) | Go developers | Submit via golangweekly.com/issues (sponsor/submit form). One sentence on the DAG executor + graph editor. | Submit Thursday of launch week (after r/golang post). |
| **TLDR DevOps** (tldr.tech, ~200k+ total subscribers across editions) | Broad tech/DevOps audience | Submit via tldr.tech/tech/contribute or email contribute@tldr.tech. Lead with the OSS + visual-editor angle. Large enough list that even a brief mention is high-value. | Submit Wednesday of launch week. |
| **Bret Fisher's Cloud Native DevOps Newsletter** (bretfisher.com, Docker/K8s practitioner audience) | Container/K8s practitioners | Email Bret directly; he curates personally. One paragraph, include the cast link. Bret specifically covers CI/CD and self-hosted tooling. | Week 2 post-launch. |
| **The New Stack** (thenewstack.io) | Cloud-native engineers and architects | Pitch an op-ed or tool spotlight via their editorial contact form. "Visual graph CI/CD for self-hosted K8s" is on-brand for their audience. Longer lead time; aim for week 3–4. | Week 3 post-launch. |

ASSUMPTION: subscriber counts sourced from publicly cited figures as of 2025; verify before committing outreach budget.

### 8. Discord vs Matrix — recommendation

strategy.md §5 defers this to day 30 ("commit to one"). Recommendation here: **Discord**.

Rationale: the primary launch persona (indie hacker / solo dev on k3s) already uses Discord daily. The real-time help-seeking behaviour that matters most for adoption — "my pipeline failed, what do I do?" — maps directly to Discord's channel model. Matrix has an ideological fit (OSS community, federation, self-hostable) but imposes setup friction: Element Web or a third-party client, account creation on a homeserver, no push notifications by default. For a project at zero-to-one contributor stage, Discord's lower join friction outweighs the ideological trade-off.

The one caveat: Discord's March 2026 age-verification rollout caused a measurable user backlash (search spike for "Discord alternatives"). If that sentiment has meaningfully shifted the OSS infrastructure community to Matrix by launch day, revisit. ASSUMPTION: the Fosstodon/Matrix community has not displaced Discord as the default for tool-level support by June 2026.

First-week community-response SLA: every support question answered within 24 hours. Every bug report acknowledged with "filed at issue #N" within 4 hours. No unanswered messages older than 48 hours. One person owns this; not a shared responsibility in week 1.

### 9. Podcasts

strategy.md §3 lists five shows. Go Time has ended (finale episode after 340 episodes; source: changelog.com). The list below replaces it and extends the original five:

| Show | Active in 2026 | Angle | Pitch hook |
|---|---|---|---|
| **The Changelog** (changelog.com) | Yes | OSS sustainability + "why we built another CI tool" | "We published our own security audit with four open HIGHs on launch day. That's the story — not the feature list." |
| **Kubernetes Bytes** (biweekly, Season 6 as of Feb 2026) | Yes | K8s deploy story + how a single binary deploys to multiple environments | "No operator, no CRD, no webhook — a Go binary that calls `kubectl apply` and knows which cluster is which." |
| **Ship It! Weekly** (Changelog network) | Yes | What happens after `git push` — real incident/deploy story | "We built the entire deploy pipeline in Go because we wanted to read the source when it broke." |
| **Arrested DevOps** (arresteddevops.com) | Yes, in 2026 roundups | Culture + tool-choice angle | "Why we chose a graph UI instead of YAML: the argument from first principles." |
| **Software Defined Talk** (softwaredefinedtalk.com) | Yes, in 2026 roundups | OSS business model + competitive landscape | "How do you build an OSS CI tool without an EE tier and still pay the bills?" |
| **DevOps and Docker Talk** (Bret Fisher) | Yes | Container build story + Kaniko vs docker.sock | "We documented the docker.sock RCE gap in our own audit and then built around it — here's the Kaniko path." |
| **The Platform Engineering Podcast** | Yes (active per 2026 lists) | Internal developer platform angle | "Single-binary CI/CD as a primitive for internal platforms — what Cooker gives you that Backstage plugins don't." |
| **Kubernetes Podcast from Google** (kubernetespodcast.com) | Yes, celebrating GKE 10th anniversary in 2025 | K8s-native deploy without K8s-native complexity | "Most K8s CI tooling requires three controllers. We ship one binary." |
| **The Cron Job** (Omer Hamerman + Mark Serdze) | Yes | Cost optimisation + scaling | "How we structured the pipeline executor so teams don't pay per-seat." |
| **CloudCast** (700+ episodes, cloud/AI/Kubernetes) | Yes | Single-binary architecture for operators | "What it takes to serve an API, a React frontend, a WebSocket hub, and a rate limiter from one Go binary on port 8080." |
| **Software Engineering Daily** | ASSUMPTION: still active | OSS business model | "Adoption-first monetisation: why we refuse to add a feature gate before 1k stars." |
| **GopherCon Go talks** (conference, not podcast) | CFP 2026 open | Pure Go — DAG executor design | "Building a DAG runner that shares a data model with a React Flow graph: the constraints that made it work." |

**Pitch format**: one email per show. Subject: "Guest pitch: [angle]." Body: one paragraph, three bullet-point talking points, cast link, repo link. Do not follow up more than once. Track in a spreadsheet. If no response in two weeks, move on.

### 10. Conferences

strategy.md §3 already lists KubeCon EU 2026, FOSDEM 2027, GopherCon 2026, SREcon, local Go meetups. No changes needed. One addition: **Platform Engineering Conf 2026** (ASSUMPTION: separate event, verify CFP dates). The "single-binary CI/CD as IDP primitive" angle is well-suited to that audience.

---

## Launch-week calendar (extended)

Extends strategy.md §4. New rows in **bold**. Preconditions gate: all eight §4 preconditions must be true before this calendar starts.

| Day / Time (ET) | Action | Owner | Notes |
|---|---|---|---|
| **Sun 22:00 (night before)** | **Product Hunt listing published in "upcoming" mode; schedule for Wed 12:01 AM Pacific** | maintainer | PH listing must be live in upcoming for ≥24h before launch for discoverability. |
| Mon 09:00 | Show HN post | maintainer | Title chosen morning-of from §4 candidates. |
| Mon 09:00–21:00 | HN comment watch (30-min SLA) | maintainer + backup | Backup has HN login. |
| Mon 16:00 | Mastodon (Fosstodon) post with native cast | maintainer | 16:00 ET = 20:00 UTC, near Fosstodon peak. |
| **Mon 17:00** | **X 4-tweet thread + Bluesky native video post** | maintainer | After HN trajectory is visible. No star-begging. |
| **Mon 20:00** | **Submit link to DevOps Weekly submission form** | maintainer | Link to HN thread, not bare repo. |
| Tue 10:00 | r/selfhosted post | maintainer | docker-compose angle; cast link. |
| **Tue 12:00** | **Submit to TLDR DevOps (contribute@tldr.tech)** | maintainer | One sentence, OSS + visual editor. |
| Tue evening | Dev.to article #1 published | maintainer | Show HN debrief, honest numbers. |
| **Wed 00:01 PT** | **Product Hunt listing goes live** | maintainer | Set to auto-launch; maintainer on call for comment responses. |
| **Wed 09:00** | **Submit to Go Weekly (golangweekly.com submit form)** | maintainer | DAG executor angle; links to r/golang post later Thu. |
| Wed 10:00 | r/devops + r/kubernetes posts | maintainer | Different angles per strategy.md §3. |
| Wed 18:00 | YouTube 8-min demo public | maintainer | Chapters in description; "what's not done" chapter included. |
| **Wed evening** | **Product Hunt comment-watch (2-hour SLA)** | backup | Maintainer focused on YT comments; backup owns PH thread. |
| Thu 10:00 | r/golang post (technical) | maintainer | DAG executor + graph data model angle. |
| Thu evening | Dev.to article #2 published | maintainer | "Why a DAG editor when YAML works" |
| Fri 11:00 | X/Bluesky recap thread; podcast/newsletter outreach begins | maintainer | First contact emails to shows listed above. |
| **Fri 14:00** | **Email Bret Fisher (Cloud Native DevOps Newsletter + DevOps and Docker Talk)** | maintainer | Personal email; one paragraph + cast link. |
| Fri EOD | First retrospective: stars, traffic, PH rank, top objections | maintainer | Cold data while fresh. Decide week-2 pivot if needed. |
| **Week 2, Day 30** | **Open Discord server; pin invite link in README + docs** | maintainer | strategy.md §5 sets day-30 as the commit date. Discord recommended above. |

---

## Extended HN objection table

Adds to strategy.md §4. Only new objections listed here; existing entries unchanged.

| New objection | Prepared response |
|---|---|
| "The graph editor is just React Flow with a backend — that's not a differentiator." | Correct that we use React Flow. The differentiator is not the library choice; it is that the graph is the authoritative data model — the DAG you draw is what executes. Other CI tools use YAML as the model and add a UI on top as a viewer. We inverted that. |
| "OCI compliance is table stakes. Every registry supports it." | Agreed — it's not a differentiator, it's a baseline. We name it because there are builders (notably the docker socket path) that produce non-OCI images; we default to compliant builders. |
| "I already use Coolify / Dokploy. Why switch?" | You don't have to. Coolify and Dokploy are better than us today for PaaS-style deploy-from-git with zero config. We're for teams who need a build step that isn't "build pack magic" — OCI images built from a Dockerfile with a pipeline you can inspect, fail, and retry visually. |
| "Why MIT and not AGPLv3? You'll get forked into oblivion." | MIT is a deliberate choice. If someone forks Cooker and sells it, it validates the idea. The moat is not the license; it is contributor momentum, docs quality, and being the best-maintained implementation. AGPL would scare off the enterprise users we do eventually want. |
| "No Windows, no Mac native — you're excluding most developers from trying it." | The server runs on Linux (and Docker Desktop on Mac). The `docker compose up` quickstart works on Mac with Docker Desktop. We don't run natively on Windows; the target environment is a Linux server, not a dev laptop. |
| "Your 'What's not done yet' list is long. Should I wait for v1.0?" | That list is a promise, not an excuse. If any item on it is a blocker for you, wait. If it isn't, `docker compose up` takes 30 seconds and you can decide yourself. |
| "Single Go binary sounds great but what happens when it crashes?" | It restarts. The state is in Postgres (or the in-memory store for dev mode). The WebSocket tickets are 60-second TTL; a crash during a run is recoverable because the runner status is persisted before the run starts. |
| "How do you handle secrets in the pipeline?" | Env-var injection from the configured secrets backend (Vault, KeepSave, or env-file). Secrets are never written to the run log. This is documented; the implementation is in `internal/secrets/`. |

---

## Outreach — one-paragraph pitches

### Podcasts (top 4 to pitch first)

**The Changelog**: Cooker launched with a published security audit that named four open HIGH findings on the same day as the Show HN post. That is the story — a small OSS project choosing radical transparency about what it can't do yet, in a space (CI/CD) where vendors routinely oversell security posture. Happy to discuss the build-versus-buy decision, the OSS sustainability path (no EE tier, adoption-first monetisation), and what "CI/CD you can see" actually means for a solo maintainer running a k3s node.

**Kubernetes Bytes**: Cooker deploys to Kubernetes from a Go binary that shells `kubectl` — no operator, no CRD, no webhook admission controller. The deploy target model is pluggable: same pipeline, different deploy adapter, whether the target is K8s, ECS, Fly, or SSH. We can talk about the trade-offs of that approach versus the CRD-native path, and what it costs you in observability to stay out of etcd.

**Arrested DevOps**: We made a deliberate choice to use a visual graph editor instead of YAML for pipeline configuration. This is not obviously correct — YAML CI is in version control, diffable, and well-understood. The graph editor is editable in a UI, which means it's harder to code-review and easier to mess with at 3am. We can defend that choice, argue against it, and explain what we would build differently if we started over.

**DevOps and Docker Talk**: We documented the docker socket RCE risk in our own published security audit (issue S26-05-04) and then built the Kaniko adapter to close it. That is the episode hook — not "here is a feature," but "here is a security decision we made in public and the implementation that followed."

### Newsletter submission copy

**DevOps Weekly submission** (one sentence): "Cooker — open-source CI/CD with a drag-drop graph pipeline editor, single Go binary, self-hosted Kubernetes deploy, MIT — just launched on HN: [link to HN thread]."

**Go Weekly submission** (one sentence): "Cooker uses a shared data model between its React Flow graph editor and its Go DAG executor — the JSON the frontend writes is what the backend runs. Source at github.com/santapong/cooker."

**TLDR DevOps submission** (two sentences): "Cooker is a self-hosted CI/CD tool with a visual pipeline editor — drag Build, Push, and Deploy nodes onto a canvas, draw edges, click Run. Single Go binary, OCI images, Kubernetes deploy, MIT-licensed."

### Communities (awesome-lists and directories)

**awesome-selfhosted PR**: Submit within 48 hours of the Helm chart and docker compose quickstart being verified clean on a third-party machine. Category: "Software Development — Continuous Integration." Entry: `[Cooker](https://github.com/santapong/cooker) - Graph-based CI/CD pipeline editor for building OCI images and deploying to Kubernetes. ([Source Code](https://github.com/santapong/cooker)) MIT Go`.

**awesome-go PR**: Category "DevOps Tools." Submit after Go Weekly mention lands (cross-link helps the maintainers verify the project is real).

**awesome-ci-cd (if the repo exists and is maintained)**: ASSUMPTION: verify this list is actively maintained before spending time on the PR.

---

## Cross-team flags

- **SEO (`cooker-mkt-seo`)**: Every channel listed here generates a potential backlink. Priority backlinks: DevOps Weekly (high-authority, practitioner audience), Go Weekly (direct Go community), The New Stack op-ed (high DA), awesome-selfhosted (community-maintained but frequently crawled). All outreach should coordinate timing so that anchor text and URLs are consistent before the SEO agent indexes them.

- **GEO / citable sources (`cooker-mkt-geo`)**: The HN post, Product Hunt listing, Dev.to articles, and any podcast appearances all become citable sources. Ensure every article and podcast pitch uses the canonical product description from strategy.md §1 verbatim — inconsistent descriptions fragment citation signals.

- **Segmentation (`cooker-mkt-segmentation`)**: The channel-to-persona mapping implied here is: HN + r/selfhosted + Discord = persona 1 (indie hacker); r/devops + r/kubernetes + Product Hunt + newsletters = persona 2 (SMB platform team); podcast appearances + conference talks = longer-term persona 2 + early persona 3 awareness. If segmentation produces different audience-to-channel conclusions, the launch-week calendar ordering may need to change.

- **Product-readiness gate**: The Product Hunt listing (Wednesday, day 3) and YouTube video (Wednesday 18:00) are both downstream of the Show HN (Monday). If HN reveals a critical usability bug, Wednesday actions should be paused. The comment-watch plan (strategy.md §4, extended above) is the signal for this decision. No calendar item after Monday is unconditional.

- **Go Time is no longer active** (concluded after episode 340; source: changelog.com). Strategy.md §3's podcast list should be updated to remove it. The replacements above (Ship It! Weekly, Kubernetes Bytes, Arrested DevOps) cover the same Go-practitioner audience.
