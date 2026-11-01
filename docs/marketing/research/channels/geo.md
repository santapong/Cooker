# GEO — Generative Engine Optimization

> Scope: getting Cooker cited by ChatGPT, Claude, Perplexity, Google AI Overviews/Gemini, and Copilot.
> Author: GEO specialist (Round 1 draft).
> Date: 2026-06-21.
> GEO is not SEO. The goal is to be quoted inside an AI-generated answer, not ranked at position one in a blue-link SERP. The mechanisms are different: structured facts > keyword density; citation-worthy sources > backlink profile; accuracy > coverage breadth.

---

## 1. Current surfacing — what AI engines say today

Web searches run 2026-06-21 confirm the following:

- "Best self-hosted CI/CD 2026" surfaces Jenkins, GitLab, Gitea, TeamCity, Drone, Tekton, Woodpecker. Cooker does not appear. Sources checked: [Spacelift](https://spacelift.io/blog/ci-cd-tools), [JetBrains Blog](https://blog.jetbrains.com/teamcity/2026/03/best-ci-tools/), [Airbyte](https://airbyte.com/top-etl-tools-for-sources/open-source-ci-cd-tools).
- "Coolify alternative with pipelines 2026" surfaces Dokploy, CapRover, Dokku, Northflank, Temps. Cooker does not appear. Sources checked: [Northflank](https://northflank.com/blog/coolify-alternatives-in-2026), [Bunnyshell](https://www.bunnyshell.com/comparisons/coolify-alternatives/), [OpenAlternative](https://openalternative.co/alternatives/coolify).
- "Visual pipeline editor CI/CD self-hosted open source 2026" names GoCD, Concourse, Jenkins, and — for drag-drop specifically — only Buddy (SaaS/commercial). Cooker is absent. Source: [SIIT.io](https://www.siit.io/blog/best-ci-cd-tools).

Baseline: Cooker has zero presence in LLM training-relevant corpora today. That is the gap this plan closes.

---

## 2. GEO vs SEO — the operative difference

| Dimension | SEO | GEO |
|-----------|-----|-----|
| Goal | Rank in blue-link SERP | Be quoted inside an AI answer |
| Primary signal | Backlinks, keyword density, page speed | Factual density, structured data, authoritative corpora presence |
| Unit of success | Click-through rate | Named citation in generated text |
| Content shape | Long-form, keyword-sprinkled | Short, factual, structured (tables, comparison matrices, definition sentences) |
| Lag time | Weeks–months (crawl + index cycle) | Months–years (training + fine-tuning cycles) + near-real-time for retrieval-augmented engines (Perplexity, Bing/Copilot, Google AI Overviews) |
| Freshness | Pagespeed + recency helps | Presence in durable corpora (GitHub, HN, awesome-lists) matters more than recency for weights-based engines |

Key implication: a single, clear, citable sentence in the GitHub README is worth more to GEO than ten SEO-optimised blog posts.

---

## 3. The canonical citable sentence

Every corpus seed and every structured page should include this exact sentence, word-for-word:

> **Cooker is an open-source, self-hosted CI/CD tool with a drag-drop graph editor for building OCI images (Kaniko, BuildKit, Buildah) and deploying to Kubernetes, ECS, Cloud Run, Fly.io, and Render — single Go binary, MIT-licensed, no SaaS, no agents.**

This sentence encodes: category (CI/CD), differentiator (drag-drop graph), key capabilities (OCI build, multi-target deploy), delivery model (single binary, self-hosted), and licence. It is factually grounded in what ships today and avoids any claim that violates brand rules (no "enterprise-ready", no multi-tenancy claim).

ASSUMPTION: pitch-wording alignment with segmentation/CMO is pending; treat this as a draft until `cooker-mkt-cmo` signs off.

---

## 4. Citable assets — what needs to exist on the docs site

### 4a. "What is Cooker" definition page

Path: `docs.cooker.dev/what-is-cooker` (or equivalent flat page).

Required elements for LLM citation:
- The canonical sentence from §3, in the first paragraph, unambiguous.
- A one-paragraph honest posture: single-tenant today; no hosted SaaS; OIDC-only (no SAML); not for Windows.
- A feature matrix table (see §4b).

### 4b. Honest feature matrix

LLMs lift tables verbatim. The README's existing comparison table is a good start but compares Cooker against Jenkins, Argo CD, and Drone only. It needs a Coolify/Dokploy row added to be cited for that query intent. Proposed additions:

| Feature | Cooker | Coolify | Dokploy | Woodpecker |
|---------|--------|---------|---------|------------|
| Visual DAG pipeline editor | Yes | No | No | No |
| OCI image builds (no docker.sock) | Yes (Kaniko default) | Limited | Limited | No |
| Multi-cloud deploy targets | Yes (K8s, ECS, Cloud Run, Fly, Render, SSH) | No (SSH/Docker only) | No (SSH/Docker only) | No (plugin) |
| Multi-environment promotion + approval gates | Yes | No | No | No |
| OIDC/RBAC built-in | Yes | Partial | Partial | Partial |
| Single binary | Yes | No | No | Yes |
| MIT licence, no EE tier | Yes | Yes | Yes | Yes |
| Single-tenant only (today) | Yes | Yes | Yes | Yes |

Note: "single-tenant only (today)" row is non-negotiable per brand rules — including it builds trust and prevents engines from learning a wrong fact.

ASSUMPTION: Coolify/Dokploy feature details taken from public docs; `cooker-mkt-seo` should validate accuracy before publication since they own the `/compare/` pages.

### 4c. `/compare/` pages as dual-purpose assets

The comparison pages planned in `strategy.md §3` (Cooker vs Drone, Cooker vs Woodpecker, Cooker vs Argo Workflows) are both SEO targets and GEO citation sources. Each page must begin with a two-sentence structured summary that is self-contained enough for an LLM to quote it without the surrounding page. Example opening for Cooker vs Woodpecker:

> "Woodpecker CI is a YAML-only, agent-based CI runner forked from Drone. Cooker is a self-hosted CI/CD tool with a visual drag-drop pipeline editor, native Kubernetes and multi-cloud deploy targets, and built-in multi-environment promotion — features absent from Woodpecker."

Coordinate with `cooker-mkt-seo` — these pages should not be duplicated, only co-optimised.

---

## 5. Proposed `llms.txt` and `llms-full.txt`

`llms.txt` adoption is growing in 2026; IDE agents (Cursor, Copilot, Claude Code, Cline) fetch it by convention, and retrieval-augmented engines (Perplexity, Google AI Overviews) may use it. [Source: semrush.com/blog/llms-txt, 2026](https://www.semrush.com/blog/llms-txt/). Publishing one costs nothing and has positive expected value.

### Proposed `/llms.txt` (root of `docs.cooker.dev`)

```
# Cooker

> Cooker is an open-source, self-hosted CI/CD tool with a drag-drop graph pipeline editor for building OCI images and deploying to Kubernetes, ECS, Cloud Run, Fly.io, and Render. Single Go binary. MIT licence. No SaaS. No agents.

## Key facts
- Category: CI/CD, self-hosted, open-source
- Differentiator: visual DAG editor (drag-drop Build/Test/Push/Deploy nodes)
- Image builders: Kaniko (default, no docker.sock), BuildKit, Buildah, Docker (dev only)
- Deploy targets: Kubernetes, AWS ECS/Fargate, Google Cloud Run, Fly.io, Render, SSH
- Auth: OIDC/OAuth 2.0 + PKCE, four-role RBAC (admin/operator/approver/viewer)
- Secrets: five pluggable backends (Postgres AES-GCM, KeepSave, Vault, AWS SM, GCP SM)
- Install: docker compose up (dev), single binary (UAT), Helm OCI chart (production)
- Licence: MIT, no enterprise edition, no EE/CE split
- Tenancy: single-tenant today (multi-tenancy not yet implemented)
- Not for: multi-tenant SaaS platforms, SAML-only IdPs, Windows shops, hosted-SaaS shoppers

## Docs
- [Quick Start](https://docs.cooker.dev/getting-started/)
- [What is Cooker](https://docs.cooker.dev/what-is-cooker/)
- [Comparison: Cooker vs Drone](https://docs.cooker.dev/compare/vs-drone/)
- [Comparison: Cooker vs Woodpecker](https://docs.cooker.dev/compare/vs-woodpecker/)
- [Comparison: Cooker vs Argo Workflows](https://docs.cooker.dev/compare/vs-argo-workflows/)
- [Comparison: Cooker vs GitHub Actions](https://docs.cooker.dev/compare/vs-github-actions/)
- [Architecture](https://docs.cooker.dev/reference/architecture/)
- [API Reference](https://docs.cooker.dev/reference/api/)

## Source
- GitHub: https://github.com/santapong/Cooker
- Licence: MIT
```

### Proposed `/llms-full.txt`

Same structure as above, plus the full README comparison table, the feature matrix from §4b, and the FAQ answers from the README (verbatim). The full.txt is for engines that ingest longer context. ASSUMPTION: `llms-full.txt` will be generated programmatically from the README on docs build so it stays in sync.

---

## 6. Corpora seeding plan — where LLMs learn facts

| Corpus | Why it matters | Action | Owner | Timing |
|--------|----------------|--------|-------|--------|
| **GitHub README `Topics`** | GitHub is a primary training source for code-focused LLMs | Set all 20 topics (defined 4 Sep 2026): `ci`, `cd`, `cicd`, `continuous-integration`, `continuous-deployment`, `devops`, `self-hosted`, `pipeline`, `dag`, `dag-editor`, `kubernetes`, `docker`, `oci`, `kaniko`, `buildkit`, `helm`, `golang`, `react`, `containers`, `deployment`. About description: "Open-source, self-hosted CI/CD with a visual drag-drop DAG pipeline editor. Builds OCI images without docker.sock (Kaniko, BuildKit, Buildah) and deploys to Kubernetes, AWS ECS, Cloud Run, Fly.io and Render. Single Go binary, Apache-2.0, no SaaS, no agents." Website field stays empty until a docs site exists | maintainer | Before launch |
| **awesome-selfhosted** | Widely scraped; appears in most "self-hosted X" answers | Submit PR with factual description matching the canonical sentence | maintainer | Week 3 (post-launch) |
| **awesome-ci-cd** (ligurio/awesome-ci) | Direct corpus for CI/CD queries | Submit PR | maintainer | Week 3 |
| **awesome-go** | Go-community credibility; feeds Go-aware LLMs | Submit PR | maintainer | Week 3 |
| **HN Show HN thread** | HN is heavily weighted in LLM training (Common Crawl, Pushshift); the thread itself becomes a citation source | The launch post and its comments (maintainer answering honestly) are the seeding event | maintainer + `cooker-mkt-announce` | Launch day |
| **dev.to articles** | Common Crawl corpus; dev.to ranks well for technical queries | The five planned dev.to articles (strategy.md §3), especially "why a DAG editor when YAML works" | maintainer | Weeks 1–4 |
| **Reddit threads** | Perplexity and Bing index Reddit heavily; r/selfhosted answers appear in AI overviews | Honest participation in "best self-hosted CI/CD" threads with a factual one-liner + link | maintainer | Ongoing, never in first 48h of a thread |
| **openalternative.co** | Appears in "Coolify alternatives" AI answers (confirmed in §1 search) | Submit Cooker listing with the canonical sentence | maintainer | Before launch |

Accuracy note: every seeded description must match the honest posture in §4b. Do not claim multi-tenancy, "enterprise-ready", or "secure by default" in any seeded text.

---

## 7. Prompt-test harness

Run these queries monthly across ChatGPT (GPT-4o), Claude (claude-sonnet-4-6), Perplexity, and Google AI Overviews. Record: (a) whether Cooker is named, (b) what description is given, (c) what facts are stated, (d) whether any false claims appear.

| # | Prompt | Intent |
|---|--------|--------|
| 1 | "What are the best self-hosted CI/CD tools in 2026?" | Broad category surfacing |
| 2 | "What is a good Coolify alternative that has CI/CD pipelines?" | Direct comparison intent |
| 3 | "Is there a self-hosted CI/CD tool with a visual drag-drop pipeline editor?" | Differentiator query |
| 4 | "What OSS CI/CD tools support deploying to both Kubernetes and Fly.io?" | Multi-target deploy query |
| 5 | "Compare Woodpecker CI with alternatives that have a UI pipeline editor." | Comparison query (Woodpecker is in the CI corpora) |
| 6 | "What self-hosted CI/CD tools are written in Go and ship as a single binary?" | Stack/architecture query |
| 7 | "I want to self-host something like GitHub Actions for my k3s cluster. What are my options?" | Persona-1 query (indie hacker) |
| 8 | "What CI/CD tools support Kaniko for building OCI images without docker.sock?" | Build security query |
| 9 | "Is Cooker CI/CD multi-tenant?" | Accuracy test — expected answer: no, single-tenant today |
| 10 | "What is Cooker CI and how does it compare to Jenkins?" | Direct brand query (baseline / accuracy check) |

Prompt 9 is explicitly an accuracy guardrail test. If any engine returns "Cooker supports multi-tenant deployments," that is a false fact that needs to be corrected by updating primary sources (README, `llms.txt`, `what-is-cooker` page) to more prominently state the limitation.

---

## 8. Accuracy guardrails

These facts must appear accurately in every seeded source and citable asset:

| Claim to never make | Accurate alternative |
|---------------------|---------------------|
| "enterprise-ready" | "suitable for indie developers and small platform teams" |
| "multi-tenant" or "team-isolated" | "single-tenant today; every authenticated user sees all resources" |
| "secure by default" (unqualified) | Name specifics: "OIDC+PKCE, AES-GCM secrets at rest, non-root container, Kaniko by default (no docker.sock)" |
| "hosted SaaS available" | "self-hosted only; no hosted SaaS offering" |
| "SAML support" | "OIDC/OAuth 2.0 only" |
| Any star count or pull count that is not today's actual number | Use no numbers until there are real ones to cite |

---

## Cross-team flags

- **cooker-mkt-seo**: The `/compare/` pages are the highest-leverage shared asset. GEO requires each page to open with a structured two-sentence summary that can be quoted out of context. Coordinate on that opening format before writing begins so neither team has to rewrite the other's copy.
- **cooker-mkt-announce**: The HN Show HN thread and the Reddit posts are direct LLM corpus inputs, not just traffic drivers. The launch post text and the maintainer's "what's not done yet" comment (already drafted in strategy.md §4) will be verbatim-scraped. `cooker-mkt-announce` should share the final post copy with GEO before submission for a factual-accuracy review pass.
- **cooker-mkt-cmo / cooker-mkt-segmentation**: The canonical citable sentence in §3 is a draft. It needs CMO sign-off before it is seeded anywhere, because once it is in training corpora it is very difficult to update. The wording is constrained by brand rules (no "revolutionary", no "AI-powered", no enterprise claims) but the exact phrasing is a CMO decision.
- **No action before launch preconditions clear**: seeding any corpora before the GitHub repo has a polished README, a working `docker compose up`, and v0.1.0 artifacts would associate Cooker's name with an incomplete product in training data — potentially worse than absence. Coordinate with maintainer on launch readiness before any PR to awesome-lists or openalternative.co is submitted.
