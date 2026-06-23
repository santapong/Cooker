# SEO plan — Cooker v1

> Author: SEO specialist. Round 1, 2026-06-21.
> Inputs: strategy.md §1/§3, README.md competitor table, 00-brief.md, WebSearch (all searches run 2026-06-21).
> This doc does not touch product code. All difficulty ratings are estimates; label them ASSUMPTION where unchecked by data.

---

## 1. Keyword map

### Cluster A — Head terms (high volume, high difficulty)

| Keyword | Intent | Difficulty est. | Notes |
|---|---|---|---|
| self-hosted CI/CD | Informational / commercial | High (70+/100) | Dominated by Jenkins, GitLab, Drone roundups. We appear in zero of them today. Realistically a 12-month organic target, not a launch-week win. |
| open source CI/CD | Informational | High | Same roundup trap. Good for docs-site authority; poor for near-term ranking. |
| CI/CD for Kubernetes | Informational / commercial | High | ArgoCD, Tekton, and Flux own this. Worth a long-form guide but not a quick win. |

### Cluster B — Visual/graph differentiator (medium volume, medium difficulty — our real opportunity)

| Keyword | Intent | Difficulty est. | Notes |
|---|---|---|---|
| visual CI/CD pipeline | Commercial investigation | Medium (40–55) | ASSUMPTION: medium-difficulty; Atmosly and Devtron rank here but neither dominates organically. Cooker's visual-graph differentiator is a direct match. Checked 2026-06-21: search results surface Harness and Atmosly, confirming real demand and a gap for a purpose-built open-source entrant. |
| drag drop pipeline editor | Commercial investigation | Low-medium | Very few pages target this exact phrase. Exact-match opportunity in a thin field. |
| graph-based CI/CD | Informational | Low | Niche but intent-aligned; the kind of query a developer types after reading about Concourse and wanting something more modern. |
| visual pipeline Kubernetes | Commercial investigation | Medium | Atmosly's "Pipeline Builder" page ranks here. We can out-qualify it with a single-binary OSS story. |

### Cluster C — Competitor "vs" and "alternative" long-tails (low-medium volume, low difficulty — fastest to rank)

| Keyword | Intent | Difficulty est. | Notes |
|---|---|---|---|
| Woodpecker CI alternative | Commercial investigation | Low | Checked 2026-06-21: high search activity in r/selfhosted and dev.to; weak dedicated comparison pages in SERPs. Opportunity window. |
| Drone CI alternative | Commercial investigation | Low | Similar to Woodpecker; license angst post-Harness acquisition drives query volume. |
| Coolify alternative with pipelines | Commercial investigation | Low | "Coolify alternative" has strong dev.to coverage but no result addresses the "no pipeline DAG" gap specifically. |
| Argo Workflows alternative | Commercial investigation | Low-medium | Checked 2026-06-21: Atmosly has a dedicated alternative page; this niche is contested but addressable. |
| GitHub Actions self-hosted alternative | Commercial investigation | Medium | High volume; but also high competition from Buildkite, GitLab, etc. Target only the "open source + visual" sub-intent, not the generic term. |
| Drone vs Woodpecker | Informational | Low | Checked 2026-06-21: several posts rank here; inserting a "vs both + Cooker" page can capture searchers undecided between the two. |
| Coolify vs Dokploy | Informational | Low | Checked 2026-06-21: dev.to post ranks first; a Cooker comparison page (we sit adjacent to both tools) can capture comparison intent. |

### Cluster D — Self-hosted PaaS crossover

| Keyword | Intent | Difficulty est. | Notes |
|---|---|---|---|
| self-hosted PaaS with CI/CD | Commercial investigation | Low | ASSUMPTION: low-difficulty; no strong incumbent page. Owned by strategy.md's core positioning. |
| Coolify with pipeline editor | Commercial investigation | Very low | ASSUMPTION: near-zero competition; a Cooker docs page is the only serious answer to this query today. |
| single binary CI/CD | Informational | Low | Targets the sysadmin persona; "single binary" is a known IndieHacker/r/selfhosted trigger word. |

---

## 2. Content plan

All pages live under `docs.cooker.dev`. MkDocs Material renders `docs/`. Pages in `docs/compare/` satisfy the strategy.md §3 mandate.

### 2a. /compare/ pages (comparison content)

| Page | Target keyword | Priority | Outline |
|---|---|---|---|
| `/compare/cooker-vs-github-actions/` | GitHub Actions self-hosted alternative | Month 1 | Intro framing self-hosting decision; head-to-head feature table (visual editor, deploy story, self-host cost); Where GitHub Actions wins (marketplace, free public repos); Where Cooker wins (deploy DAG, no YAML required, single binary); Migration sketch. |
| `/compare/cooker-vs-drone/` | Drone CI alternative | Month 2 | License history (Harness/BSL); feature table; Cooker's visual editor vs Drone's YAML; honest nod to Drone's maturity; single-CTA at bottom. |
| `/compare/cooker-vs-woodpecker/` | Woodpecker CI alternative | Month 2 | Fork history (OSS credential); YAML-first vs graph-first; identical where-they-win section; contributor community comparison. |
| `/compare/cooker-vs-argo-workflows/` | Argo Workflows alternative | Month 3 | Acknowledge Argo's K8s-native depth; CRD overhead vs single binary; visual editor gap in Argo; Cooker's deploy targets beyond K8s (ECS, Fly, Render); coexistence framing (Argo CD + Cooker can live together). |
| `/compare/cooker-vs-coolify/` | Coolify alternative with pipelines | Month 2 | Shared self-hosted PaaS positioning; Coolify's no-pipeline-DAG gap; Cooker's graph editor differentiator; where Coolify wins (app marketplace, Heroku-style ease for non-K8s users); honest about Cooker's maturity vs Coolify's community size. |

Each page: 800–1200 words. Open with the user's real question ("I'm looking at X, should I switch?"). Feature table in the first third. Explicit "Where X wins" section — this is non-negotiable for brand credibility and for winning featured snippets (Google favours balanced comparison pages for "vs" queries). CTA: link to quickstart, not to a signup page.

### 2b. Docs-as-marketing landing pages

| Page | Target keyword | Notes |
|---|---|---|
| `docs/index.md` (docs landing) | visual CI/CD Kubernetes | Embed the graph editor screenshot above the fold. Link to quickstart and /compare/ section. |
| `docs/user-guide/getting-started/quickstart.md` | self-hosted CI/CD quickstart | "From `docker compose up` to first green run" framing. Targets bottom-funnel install intent. |
| `docs/user-guide/concepts/pipeline-editor.md` | drag drop pipeline editor | In-depth explanation of the React Flow graph editor; the page a searcher lands on after Googling this exact phrase. |
| `docs/compare/` (section index) | Cooker vs [competitors] | Aggregator page linking to all /compare/ sub-pages; structured as a decision guide. |
| `docs/user-guide/guides/kubernetes-deploy.md` | CI/CD deploy to Kubernetes | Targets K8s-native audience; positions Cooker next to Argo/Tekton without picking a fight. |
| `docs/user-guide/guides/self-hosted-paas.md` | self-hosted PaaS with CI/CD | Cross-niche content targeting Coolify/Dokploy defectors; show Cooker sitting between PaaS and pure CI. |

---

## 3. Technical SEO

### Sitemap
MkDocs Material auto-generates `sitemap.xml` at `docs.cooker.dev/sitemap.xml`. Verify the following are included and not blocked by `robots.txt`:
- All `/compare/` pages
- The docs index and all concept/guide pages
- The quickstart page (highest-converting URL)

ASSUMPTION: MkDocs Material plugin `sitemap` is enabled in `mkdocs.yml` — confirm with the docs-site owner (docs engineer or maintainer).

### SoftwareApplication structured data
Add JSON-LD to the docs site's `<head>` (or the main landing page `cooker.dev`):

```json
{
  "@context": "https://schema.org",
  "@type": "SoftwareApplication",
  "name": "Cooker",
  "applicationCategory": "DeveloperApplication",
  "operatingSystem": "Linux, macOS",
  "offers": { "@type": "Offer", "price": "0", "priceCurrency": "USD" },
  "url": "https://cooker.dev",
  "description": "Open-source CI/CD tool with a graph-based visual pipeline editor. Single Go binary. Builds OCI images, deploys to Kubernetes, ECS, Cloud Run, Fly, Render.",
  "license": "https://opensource.org/licenses/MIT",
  "codeRepository": "https://github.com/santapong/Cooker"
}
```

This schema is eligible for a rich result on branded queries and is the most low-effort structured data win available.

### Meta and Open Graph
Every `/compare/` page needs a unique `<title>` and `<meta description>`:
- Title pattern: `Cooker vs [Competitor]: [one-line differentiator] | Cooker Docs`
- Description: 145–155 characters; include the primary keyword naturally.
- OG image: a static 1200x630px card with the graph editor screenshot + "Cooker vs [Competitor]" text. One image per compare page drives click-through on Reddit/HN link shares (these are our main backlink sources).

MkDocs Material supports per-page `meta` in front-matter. Document the pattern in a contributor note so it stays consistent.

### MkDocs crawlability
- Set `use_directory_urls: true` (MkDocs default) so URLs are `/compare/cooker-vs-drone/` not `/compare/cooker-vs-drone.html`.
- Do not add `noindex` to any docs page unless it is genuinely boilerplate (changelog pages can stay indexed; thin auto-generated API reference pages can be `noindex` if they have no prose).
- Canonical tags: MkDocs Material generates `<link rel="canonical">` by default. Confirm the `site_url` in `mkdocs.yml` matches the production domain exactly (no trailing slash mismatch).
- If dev.to cross-posts use the canonical tag pointing back to `docs.cooker.dev`, the link equity flows home (see §4 below).

---

## 4. Backlinks and authority

### Awesome-list targets (prioritised)

| List | URL | Category to target | Priority |
|---|---|---|---|
| awesome-selfhosted | github.com/awesome-selfhosted/awesome-selfhosted | Software Development > CI/CD | P1 — PR after v0.1.0 artifacts exist (launch gate) |
| awesome-go | github.com/avelino/awesome-go | DevOps Tools | P1 |
| awesome-kubernetes (ramitsurana) | github.com/ramitsurana/awesome-kubernetes | CI/CD | P2 |
| awesome-k8s-resources | github.com/tomhuang12/awesome-k8s-resources | CI/CD | P2 |
| awesome-selfhost-docker | github.com/hotheadhacker/awesome-selfhost-docker | — | P3 |
| awesome-github-actions-runners | github.com/neysofu/awesome-github-actions-runners | alternatives | P3 — angle: Cooker as a full-stack alternative, not just a runner replacement |

Each PR needs a one-line description that matches the list's convention. Don't submit until `v0.1.0` has published release artifacts (docs/shipping-go.md precondition). ASSUMPTION: strategy.md §3 already scoped `awesome-selfhosted`, `awesome-go`, `awesome-ci-cd` — this list extends that.

### Dev.to canonical cross-posts
Strategy.md §3 plans five dev.to articles. For SEO:
- Set the dev.to "canonical URL" field to the equivalent docs-site URL (or the GitHub README for the launch post). This passes link equity to Cooker's owned domain rather than letting dev.to accrue it.
- Articles with the most backlink potential: the "Why we built a DAG editor" essay and the HN debrief — pitch these for republication on The Changelog blog or Hacker Noon after initial publication.

### HN / Reddit citation flywheel
The Show HN post and the r/selfhosted post are the highest-leverage organic backlink sources at launch:
- Every external blog that covers the HN launch typically links directly to the GitHub repo. That's fine — GitHub Pages passes some equity to linked domains.
- Over weeks 2–8, monitor who links from Reddit/HN comments and reach out to bloggers who wrote "tools I tried this week" posts to ask for a docs-site link instead of (or in addition to) the GitHub link.
- The comparison pages are specifically designed to be cited in Reddit threads where someone asks "Drone vs Woodpecker — any others worth looking at?" A /compare/ page is a credible, helpful link to drop in that context without appearing self-promotional.

---

## 5. Roadmap summary

| Horizon | Action | Owner |
|---|---|---|
| Pre-launch (week 0) | Add SoftwareApplication JSON-LD to landing page; confirm MkDocs sitemap is live | Maintainer |
| Launch week | `/compare/cooker-vs-github-actions/` published; PR to awesome-selfhosted and awesome-go | Maintainer |
| Month 2 | Drone, Woodpecker, and Coolify compare pages; dev.to canonical back-links set | Content author |
| Month 3 | Argo Workflows compare page; outreach to bloggers who covered the HN launch | Content author |
| Ongoing | Monitor Search Console (add property to `docs.cooker.dev`); flag any `/compare/` page ranking in positions 11–20 for a refresh | SEO |

ASSUMPTION: Google Search Console is set up on launch day. If not, this is a day-one action — without it there is no click/impression data to act on.

---

## Cross-team flags

- **cooker-mkt-geo**: The keyword clusters in §1 (particularly "visual CI/CD pipeline" and "Coolify alternative with pipelines") overlap directly with GEO's entity-disambiguation needs. Share the competitor name list and feature table so GEO schema and AI-overview targeting use the same factual framing. The SoftwareApplication JSON-LD in §3 is a shared dependency — coordinate on the final `description` field text so it serves both organic SERPs and AI-generated overviews.

- **cooker-mkt-announce**: The Show HN post and Reddit posts (strategy.md §3) are the primary launch-week backlink source for SEO. The announce specialist should know that every external post citing the HN thread ideally links to `docs.cooker.dev`, not just the GitHub repo. Coordinate the CTA copy in launch posts to drive traffic to the docs site. The five dev.to articles need canonical URLs set before publish — announce needs to know this is not optional.

- **cooker-mkt-segmentation**: The keyword map splits along persona lines: Cluster A/B targets the SMB platform-team persona (higher intent, longer evaluation cycle); Cluster C/D targets the indie-hacker persona (fast decision, high traffic). Segmentation's persona definitions should validate that "Coolify alternative" intent really belongs to persona-1 (indie hacker) rather than persona-2 (SMB team), because the content tone differs materially. Flag any persona re-scoping back to SEO so compare-page tone can be adjusted before publishing.

- **cooker-mkt-sem**: Cluster C ("vs" and "alternative" terms) are the keywords most likely to appear on a SEM list because they are high commercial-intent and relatively cheap to bid on. SEO's recommendation: do not bid on these until organic /compare/ pages have been live for at least 60 days. If we rank organically for "Drone CI alternative" or "Woodpecker CI alternative," paying to also appear in paid search on the same query is wasted budget. Flag overlap to SEM so they hold the "vs/alternative" terms in reserve and focus paid spend on head terms (self-hosted CI/CD, CI/CD Kubernetes) where organic ranking is a 12-month project, not a 60-day one. The no-paid-ads-before-day-180 rule in brand guidelines (strategy.md §7) pre-empts this conflict for the launch period, but it should be documented now.
