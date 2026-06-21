<!-- DRAFT — for maintainer review before any post goes live. Do not submit. -->

# Show HN — Launch Kit

> Extends strategy.md §4 and announce.md §1.
> All preconditions from strategy.md §4 must be met before anything here is used.
> Date produced: 2026-06-21.

---

## 1. Title options

### Option A (recommended)
**Show HN: Cooker — open-source CI/CD with a graph UI, single Go binary**

Rationale: matches the primary HN reader's scan pattern (tool name, differentiator, implementation
signal). "Graph UI" is screenshot-worthy and specific. "Single Go binary" is a signal to the Go
community and to anyone who has felt deployment-friction. Short enough to read at a glance.

### Option B
**Show HN: Cooker — drag, drop, deploy. Self-hosted CI/CD for Kubernetes**

Rationale: the alliterative hook is readable; "Self-hosted CI/CD for Kubernetes" names the use case.
Risk: "drag, drop, deploy" reads slightly product-marketingish for HN; option A is safer.

### Option C
**Show HN: Cooker — what if your CI pipeline looked like the diagram you drew on a whiteboard**

Rationale: conceptual hook that earns curiosity clicks. Risk: longer than HN titles that perform
well; the framing is soft where HN rewards specificity. Use only if options A and B have been tested
against the team and feel too plain.

**Recommended: Option A.**

---

## 2. Post body (ship-ready — paste verbatim)

> Hi HN. I built Cooker because every time I started a side-project I'd lose the first hour to a CI
> config that I'd already written four times. I wanted to see my pipeline, not type it.
>
> Cooker is a single Go binary that serves both an HTTP API and a React front-end. You drag Build /
> Test / Push / Deploy nodes onto a canvas, draw arrows between them, and click Run. It builds OCI
> images (your choice of docker / Kaniko / buildah / BuildKit), pushes them to any OCI-compliant
> registry, and deploys to Kubernetes. There's no SaaS, no agent, no asterisks — `docker compose up`
> and you've got a working install. Demo cast on the README. Source at github.com/santapong/cooker,
> Apache-2.0.

Editorial notes (remove before posting):
- The second paragraph ends with the license placeholder. Fill in the actual license before posting.
  Leaving a placeholder live on HN will look like a production mistake.
- The repo URL (github.com/santapong/cooker) should be confirmed against the actual canonical URL the
  night before launch; if it moved, update it.
- Do not add a third paragraph. HN Show HN posts perform best when the body is short and the detail
  lives in the first comment. The "what's not done yet" comment below is the third paragraph.

---

## 3. "What's not done yet" first comment (verbatim — post within 5 minutes of the Show HN going live)

> What's not done yet, since this is HN and someone will ask:
>
> - **Single-tenant.** Every authenticated user can read every pipeline / app / environment. This is
>   documented; multi-tenancy is on the roadmap but I'm not going to pretend it ships today.
> - **No in-product audit-log viewer.** The audit middleware is in place and writes structured JSON; you'd
>   have to query it from your log stack today.
> - **No PR-preview environments.** Possible to assemble manually via the pipeline editor; no first-class
>   per-PR ephemeral env yet.
> - **Builder choice matters.** The `docker` builder mounts the host docker socket and is dev-only. Kaniko
>   / buildah / BuildKit are the production options. The security audit names the docker.sock risk as S26-05-04.
> - **Bus factor of one.** That's me. PRs welcome — there's a CONTRIBUTING.md and a labelled list of "good
>   first issues."
>
> Happy to answer anything.

Editorial notes (remove before posting):
- Post this comment with a logged-in account that is the same account that posted the Show HN. HN
  gives maintainer-authored first comments high visibility.
- The comment references security audit issue S26-05-04. Confirm the audit document is publicly visible
  on the repo before launch. If it is behind a branch that isn't merged, this reference will 404.
- Do not edit this comment after posting. HN readers notice edits and they read as evasion.

---

## 4. Full objection-handling table

Source: strategy.md §4 entries merged with the 8 additions from announce.md. All entries in one table.
Order: strategy.md originals first, announce.md additions after a visual separator. No duplicate rows.

| Objection | Prepared response |
|---|---|
| "Why not Argo Workflows / Argo CD?" | Argo is K8s-native for ML/data pipelines; we're focused on the visual-editor + deploy-app shape. They coexist; we don't compete. Linked comparison post. |
| "Why not Drone / Woodpecker?" | Both are YAML-first; the differentiator is the visual graph. Woodpecker is excellent and OSS — we'd recommend it for YAML-loving teams. |
| "Single tenant means I can't use this." | Correct — we are not pitching this to multi-tenant SaaS platforms today. Roadmap item, documented in README. |
| "Another CI tool, why?" | Because every existing OSS CI is either YAML-only or has a UI we wouldn't show to anyone. Specific opinion, willing to defend. |
| "How is the visual editor different from Concourse?" | Concourse's UI is read-only and famously hard to learn; ours is the editor. Drag-drop, not "look at the YAML you already wrote." |
| "License?" | Apache-2.0, full repo, no EE/CE split. |
| "What about Windows?" | Not supported. Shells to `kubectl` and `docker`. Not on the roadmap. |
| "Why React Flow / why TypeScript?" | We needed a graph editor that didn't take 6 months to write. React Flow ships. The whole frontend is replaceable; nothing in the backend cares. |
| "Why Go?" | Native Docker / K8s / OCI SDKs. No FFI. Single binary. Standard answer. |
| "Security audit?" | Yes — `docs/audits/2026-05-security-review.md` on the repo. Four open HIGHs, all named, all tracked. We did this audit ourselves and published it. |
| "Looks abandoned / one-person project?" | Currently one maintainer. Bus factor of one. Documented. Looking for contributors; "good first issue" labels active. |
| "Show me a multi-cluster deploy" | Honest answer: works at the deploy-target level; UI doesn't surface "which cluster" prominently yet. Tracked. |
| "Is there a hosted version?" | No. May be later. Today is OSS-only. |
| — announce.md additions below — | — |
| "The graph editor is just React Flow with a backend — that's not a differentiator." | Correct that we use React Flow. The differentiator is not the library choice; it is that the graph is the authoritative data model — the DAG you draw is what executes. Other CI tools use YAML as the model and add a UI on top as a viewer. We inverted that. |
| "OCI compliance is table stakes. Every registry supports it." | Agreed — it's not a differentiator, it's a baseline. We name it because there are builders (notably the docker socket path) that produce non-OCI images; we default to compliant builders. |
| "I already use Coolify / Dokploy. Why switch?" | You don't have to. Coolify and Dokploy are better than us today for PaaS-style deploy-from-git with zero config. We're for teams who need a build step that isn't "build pack magic" — OCI images built from a Dockerfile with a pipeline you can inspect, fail, and retry visually. |
| "Why Apache-2.0 and not AGPLv3? You'll get forked into oblivion." | Apache-2.0 is a deliberate choice — permissive, with an explicit patent grant. If someone forks Cooker and sells it, it validates the idea. The moat is not the license; it is contributor momentum, docs quality, and being the best-maintained implementation. AGPL would scare off the enterprise users we do eventually want. |
| "No Windows, no Mac native — you're excluding most developers from trying it." | The server runs on Linux (and Docker Desktop on Mac). The `docker compose up` quickstart works on Mac with Docker Desktop. We don't run natively on Windows; the target environment is a Linux server, not a dev laptop. |
| "Your 'What's not done yet' list is long. Should I wait for v1.0?" | That list is a promise, not an excuse. If any item on it is a blocker for you, wait. If it isn't, `docker compose up` takes 30 seconds and you can decide yourself. |
| "Single Go binary sounds great but what happens when it crashes?" | It restarts. The state is in Postgres (or the in-memory store for dev mode). The WebSocket tickets are 60-second TTL; a crash during a run is recoverable because the runner status is persisted before the run starts. |
| "How do you handle secrets in the pipeline?" | Env-var injection from the configured secrets backend (Vault, KeepSave, or env-file). Secrets are never written to the run log. This is documented; the implementation is in `internal/secrets/`. |

Usage notes:
- Every response here is a starting point, not a script. Read the actual comment before replying;
  adapt to what the person actually wrote.
- If an objection comes up that is not on this table: "Good question — I don't have a satisfying
  answer right now. Filed at github.com/.../issues/NN." Never bluff.
- If the license question comes up, the answer is: "Apache-2.0 — full source on the repo, no EE split,
  and an explicit patent grant."

---

## 5. Comment-watch SLA and flagging/second-chance rules

### SLA

- **09:00–21:00 ET on launch day (Monday)**: every comment gets a response within 30 minutes.
  "Response" means an actual answer, not "thanks for the comment." If the answer takes research,
  post "Looking into this — back in 30 min" to acknowledge, then deliver.
- **21:00–09:00 ET (overnight)**: every comment gets a response within 2 hours. Set a phone alarm;
  the overnight window on launch night can still deliver significant ranking.
- **Backup person**: must have the HN login credential before 08:30 ET Monday. One-hour shift
  handoff during the day is fine; the backup owns the overnight window if the maintainer needs sleep.
- **Days 2–7**: check the thread once per morning and once per evening. The tail of an HN thread
  can generate useful contributor interest days later.

### What counts as a response

- Direct, specific answer to the comment.
- No defensive tone. No dismissal. No "as I said in the post."
- If the criticism is valid, say so. "You're right, that's a real gap — filed at issue #N."
- If the criticism is wrong, correct it politely with a specific technical fact.
- Upvoting a critical comment is fine; it signals confidence. Downvoting anything in the thread is
  a brand-protection violation (strategy.md §7 is clear on no manipulation).

### Flagging signal

- If the post is flagged by HN or stalls below rank 60 by 11:00 ET Monday, do not attempt rescue
  via social amplification. Social traffic that looks coordinated (spike in referrals from Twitter,
  Bluesky, Discord, all within minutes) triggers further flagging. Let the post run.
- If the score is below 20 points at 11:00 ET, shift energy to the Reddit posts (Tuesday) and
  do not mention the HN result publicly until there is something positive to say.

### Second-chance path

- If the first Show HN lands below the top 30 positions for fewer than 3 hours total, wait 6
  months before a second Show HN attempt. HN's guidelines explicitly permit a second Show HN for
  a significantly updated project; use that window when v0.2.0 ships.
- A comment in the original thread welcoming future contributors ("if you'd like to help build
  the audit-log viewer, there's a good-first-issue label") is always acceptable and does not
  count as re-promotion.
- A "Who is hiring?" or "Who wants to be hired?" comment referencing Cooker is separately
  acceptable if a maintainer role ever opens, regardless of the Show HN outcome.

### Post-HN actions that are always off the table (strategy.md §7)

- No DMs to community members asking for upvotes.
- No posting in private groups asking friends to vote.
- No upvote-ring services.
- No "smash the vote button" language anywhere.
- No deleting critical comments on GitHub or any other platform.
