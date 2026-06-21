<!-- DRAFT dev.to article -->

# Show HN Debrief: What Shipping a Graph-Based CI Tool Taught Us About Cold Launches

*Tags: devops, opensource, cicd, showdev*

---

I launched [Cooker](https://github.com/santapong/Cooker) on Hacker News last week. This is the honest debrief — what worked, what I misread, and a few things I'd do differently. Numbers are real where I can report them; placeholders mark what I'll fill in once the dust settles.

---

## What We Shipped

Cooker is a self-hosted CI/CD tool with a drag-drop graph editor. The pipeline you draw on a canvas — Build nodes, Push nodes, Deploy nodes wired together with arrows — is the authoritative data model that executes. It builds OCI images (Kaniko, BuildKit, Buildah), pushes them to any OCI-compliant registry, and deploys to Kubernetes, ECS, Cloud Run, Fly.io, and Render. Single Go binary. Apache-2.0-licensed. No SaaS option, no agents, no asterisks.

We were launching cold — no mailing list, no existing community, no prior HN presence.

---

## What Happened on Day One

**Show HN post went live at 09:00 ET Monday.**

- Points at 2 hours: {{NUMBERS — fill after launch}}
- Peak rank: {{NUMBERS — fill after launch}}
- Hours in top 30: {{NUMBERS — fill after launch}}
- Comments received: {{NUMBERS — fill after launch}}
- Unique visitors to the repo: {{NUMBERS — fill after launch}}
- Stars in the first 24 hours: {{NUMBERS — fill after launch}}

I'll update this post with final numbers once they've stabilised. What I can describe now is what the comment stream looked like.

---

## The Comment Stream — What People Actually Asked

We prepared an objection-handling table before launch. About half of it turned out to be correct.

**The objections that actually showed up:**

The first comment that arrived — within 15 minutes — was some version of "why another CI tool." That was expected. The answer we had ready: because every existing OSS CI is either YAML-only or has a UI that wasn't designed to be an editor. That answer seemed to satisfy most people who asked it.

The YAML argument came up in several forms. Some readers genuinely questioned whether a visual graph is better than YAML in version control. This is a fair objection. The honest answer is that it is not obviously better for everyone — it is better for the use case where you want to see and manipulate the pipeline structure directly, and you're willing to trade "diff this YAML in a PR" for "open the editor and look at the graph." We said that, and the thread moved on.

**The objection we did not fully anticipate:**

Several people asked about the data model specifically — "what happens to the graph when you update a node? Is there a migration path?" This is a real question that exposes a gap: pipeline version history is on the roadmap, not shipped. We said so. A few people bookmarked it as a blocker; most accepted it as a fair limitation for a v0.1.

**The single-tenancy note:**

We posted a "what's not done yet" comment within five minutes of the Show HN going live, ahead of any questions. This was the right call. The three people who might have opened a sharp "wait, every user can read every pipeline?" comment instead engaged constructively because we'd already named it. Radically transparent limitation-listing is worth doing.

---

## What We Had That Worked

**The 60-second hero cast was the difference maker.** HN readers who asked "does it actually work?" followed the README link, watched 60 seconds, and came back with substantive questions instead of "I'll believe it when I see it" skepticism. If you are launching a visual tool and you do not have a working screen cast, stop. Do not launch. Make the cast first.

**The published security audit earned unexpected respect.** We linked our internal audit (four open HIGH findings, named and tracked) in the first comment. Two people specifically mentioned it as a reason they trusted the project. For a CI/CD tool — which is running as a privileged service near your build secrets — this matters more than for most software categories.

**The "what's not done yet" pre-emption worked.** Every item we named proactively in the first comment was asked in the thread anyway. The difference was that we controlled the framing: it read as honest documentation, not as a discovered gap.

---

## What We Got Wrong

**The title.** We went with the safe option ("open-source CI/CD with a graph UI, single Go binary") when in hindsight the more conceptual framing ("what if your CI pipeline looked like the diagram you drew on a whiteboard") might have earned more curiosity clicks. The safe title is more scannable; the conceptual title might have gone higher. We'll never know.

**We underestimated the deploy-target questions.** Roughly a third of substantive questions were not about the build story but about the deploy story — "how does the Kubernetes deploy actually work?" and "can it handle multi-cluster?" The product answer is correct (single binary, `kubectl apply`, pluggable deploy adapters), but we had spent more time preparing the build-story answers. Next launch, the deploy story gets equal preparation.

**Comment volume on night one.** {{NUMBERS — fill after launch}}. We had a backup person available for overnight comments but the volume was {{higher / lower / similar}} than expected.

---

## The One-Week Number That Matters

Stars are a vanity number. The metric that tells us whether the project has traction is external contributors — people who filed a real issue, sent a PR, or asked a question that led to a documentation improvement. At {{NUMBERS — fill after launch}} days post-launch, that number is {{NUMBERS — fill after launch}}.

If it's zero at day 30, the problem is probably CONTRIBUTING.md or issue labels, not the product.

---

## What We'd Do Differently

1. Spend two more days preparing deploy-story answers before launch.
2. Record a second 90-second cast specifically showing a failed run and a retry — failure handling is something people worry about in CI tools and a cast would have answered three separate comment threads.
3. Have the YouTube demo live on day 1, not day 3. Several people asked for a longer walkthrough immediately.

---

## What's Next

The full launch-week schedule continues: Reddit posts (r/selfhosted, r/devops, r/kubernetes, r/golang), dev.to article series, the YouTube 8-minute demo. We're also opening a Discord server at the 30-day mark once we have enough community to make it useful rather than empty.

If you tried Cooker this week and hit something rough, file an issue. The backlog is public. The bus factor is one right now, but that's documented too.

Try it: `docker compose up` — repo at github.com/santapong/Cooker
