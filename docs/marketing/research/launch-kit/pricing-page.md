<!-- DRAFT pricing page — for review -->

# Build pipelines. Not bills.

Cooker is a self-hosted CI/CD tool with a visual graph editor. One binary, unlimited seats, no per-developer tax.

> **Explorer is free forever. Crew starts at $49/month — for your whole team, not per head.**

---

## Plans

### Explorer — $0, forever

No credit card. No expiry. Self-hosted, single binary.

Everything you need to build, push, and deploy from a single machine:

- Unlimited pipelines and runs
- Unlimited seats
- 1 replica, 1 environment (Dev)
- K8s, Fly, Render, and SSH deploy targets
- Basic OIDC login (sign in with your identity provider — free)
- Postgres-backed secrets (AES-GCM)
- 7-day run retention
- API tokens and YAML pipeline export
- Community support

> No paywall on OIDC. Authentication is a security baseline, not a premium feature.
>
> _Note: OIDC-tier placement is pending maintainer sign-off. Team recommendation: basic OIDC remains free on Explorer. SSO group-to-role mapping and MFA step-up gate at Crew._

---

### Crew — $49 per replica / month

For teams running Cooker in production. One price covers your entire organisation — no seat counting, no per-developer fees, no surprise invoices.

Everything in Explorer, plus:

- Unlimited replicas (high-availability)
- 3 environments (Dev / Staging / Production)
- SSO group-to-role mapping
- MFA step-up enforcement
- ECS and Cloud Run deploy targets (in addition to K8s, Fly, Render, SSH)
- Vault, AWS Secrets Manager, and GCP Secret Manager backends
- Cron-triggered pipeline runs
- 90-day run retention
- Basic audit log
- Priority email support
- 14-day free trial — no credit card required; degrades gracefully to Explorer on expiry, never bricks

**Cloud pricing:** {{CLOUD BASE: $49, or $39 with 1,000 included build-minutes — maintainer decision pending}}

---

### Constellation — Custom

For enterprises with air-gapped environments, strict compliance requirements, or multi-team isolation needs.

Everything in Crew, plus:

- SSO group-to-role map (advanced, multi-IdP)
- Full audit log with append-only export and OTLP
- KeepSave multi-tenant secrets backend
- Air-gapped licensing
- Unlimited environments
- All deploy targets, including air-gapped clusters
- Configurable run retention with export
- SLA-backed support with a dedicated customer success manager

Contact us to start a conversation.

---

## Full tier comparison

| | Explorer | Crew | Constellation |
|---|---|---|---|
| **Price** | $0 forever | $49 / replica / mo | Custom annual |
| **Seats** | Unlimited | Unlimited | Unlimited |
| **Replicas (self-hosted)** | 1 | Unlimited | Unlimited |
| **Environments** | 1 (Dev) | 3 (Dev / Staging / Prod) | Unlimited |
| **Pipelines and runs** | Unlimited | Unlimited | Unlimited |
| **Concurrent builds** | 1 | 3 included | Negotiated |
| **Run retention** | 7 days | 90 days | Configurable / export |
| **Deploy targets** | K8s, Fly, Render, SSH | + ECS, Cloud Run | All + air-gapped |
| **Secrets backends** | Postgres AES-GCM | + Vault, AWS, GCP | + KeepSave multi-tenant |
| **Basic OIDC login** | Yes* | Yes | Yes |
| **SSO group-to-role map** | No | No | Yes |
| **MFA step-up** | No | Yes | Yes |
| **Cron triggers** | No | Yes | Yes |
| **Audit log / OTLP** | No | Basic | Full + append-only export |
| **API tokens / YAML export** | Yes | Yes | Yes |
| **14-day trial** | n/a | Yes (no card) | Sales-assisted |
| **Support** | Community | Priority email | SLA + dedicated CSM |

*Basic OIDC is free on Explorer. _Pending maintainer sign-off (team recommendation: basic OIDC free)._

---

## Pricing for the team buyer

### The no-seat-tax comparison

Most CI/CD tools charge per developer. That tax compounds fast.

| Tool | 30-person team cost / mo | Notes |
|---|---|---|
| Buildkite Pro | ~$450 (30 × $15/user) | Before you've run a single build — compute is separate |
| CircleCI Performance | ~$450 (30 × $15/user) | Plus per-minute compute costs on top |
| GitLab Premium | ~$870 (30 × $29/user) | Includes SCM, but you're paying for the whole platform |
| **Cooker Crew** | **$49** | One replica covers the team. Add a second for HA: $98 total. |

A 30-person team on Buildkite Pro pays approximately $450 per month before touching compute. Cooker Crew is $49 for unlimited seats.

If your team grows from 10 to 30 people, your Cooker bill does not change. Your Buildkite bill triples.

---

## Pricing for the solo developer

Explorer is free forever. One binary, one command, one environment, unlimited pipelines.

You get:

- Full CI/CD: build OCI images, push to any registry, deploy to K8s or SSH targets
- The visual graph editor, no limitations
- Basic OIDC — sign in with GitHub, Google, or any OIDC-compatible IdP
- Unlimited pipelines and runs, API tokens, YAML import/export
- No trial period, no feature expiry, no card required

When you're ready for Staging, or when your side project becomes a team project, the Crew trial is one click — 14 days, no card, degrades to Explorer at the end.

---

## Cooker vs. Coolify

**"Coolify is free. Why pay $49?"**

Coolify deploys your app. It does not build your images or run your CI.

Coolify is a PaaS: you give it a Docker image or a Git repo and it handles the hosting, reverse proxy, SSL, and restarts. That is genuinely useful, and Cooker is not a replacement for it.

Cooker is a CI/CD tool: it takes your source code, runs your pipeline, builds the OCI image, pushes it to your registry, and then deploys it — to Kubernetes, ECS, Cloud Run, Fly, Render, or an SSH target. The build step, the registry push, and the deployment orchestration happen inside Cooker's DAG.

If you are already using Coolify to host your apps, Cooker can build and push the images that Coolify then deploys. They are complementary, not alternatives.

The right comparison for Cooker's price is not Coolify's $5/server. It is Buildkite's $15/user, CircleCI's $15/user, or the engineering time your team spends stitching together a build pipeline from shell scripts and GitHub Actions.

---

## Pricing FAQ

### What does "per replica" mean?

A replica is one running instance of the Cooker binary. Most self-hosted users run one replica. Running two or more replicas adds high availability (HA): if one instance goes down, the others keep serving.

Crew is priced by replica because multi-replica HA is a production signal — it means you are running Cooker for a team with uptime requirements, not a side project. Explorer gives you one replica for free.

You pay $49 per month per replica you run. A two-replica HA setup costs $98/month. Seats are unlimited at any replica count.

### Are seats really unlimited?

Yes. Unlimited seats is a binding promise, not a footnote. Every tier — Explorer ($0), Crew ($49/replica), and Constellation (custom) — has no seat limit. You are never billed per user, per developer, or per active committer.

### Can I run unlimited pipelines?

Yes. Unlimited pipelines and runs is also a binding promise. Cooker does not gate the number of pipelines you define or the number of times you trigger them. The only limits are concurrent builds (1 on Explorer, 3 included on Crew) and run retention (7 days on Explorer, 90 days on Crew).

### What happens when the 14-day Crew trial ends?

Your instance degrades to Explorer (Free) limits. It never bricks. Running pipelines and your existing infrastructure keep working. Crew-only features — multi-environment, extended retention, MFA, cron triggers — lock until you subscribe. Your data is retained.

No credit card is required to start the trial.

### What is the difference between self-hosted and Cloud?

Self-hosted means you run the Cooker binary on your own infrastructure. You bring the server; Cooker runs on it. Self-hosted Crew is priced at $49 per replica per month.

Cooker Cloud is a hosted version where Cooker runs the infrastructure for you, including the build farm. Cloud pricing includes a base subscription plus metered build-minutes.

Cloud pricing: {{CLOUD BASE: $49, or $39 with 1,000 included build-minutes — maintainer decision pending}}

Cooker Cloud is in active development and not yet available for public signups. Join the waitlist to be notified when it opens.

### Does Explorer include OIDC?

Yes. Basic OIDC login — connecting Cooker to your existing identity provider (GitHub, Google, Okta, any OIDC-compatible IdP) — is included on Explorer at no cost.

What is gated at Crew is SSO group-to-role mapping (automatically assigning Cooker roles based on IdP group membership) and MFA step-up enforcement. Those are enterprise security-policy features, not basic authentication.

_Note: OIDC-tier placement is pending maintainer sign-off. Team recommendation: basic OIDC free on Explorer._

### Is there an annual discount?

Yes. Annual billing saves 20%: pay for 10 months, get 12. Self-hosted Crew billed annually is $470 per replica per year instead of $588.

### Can I use Cooker for free forever?

Yes. Explorer is not a trial. It has no expiry, no feature degradation over time, and no credit card requirement. If one replica and one environment cover your needs, you never pay.

### How does self-hosted licensing work?

Self-hosted Crew and Constellation licenses are offline-verified Ed25519-signed license files. The license is verified by the Cooker binary without calling home. This means Cooker works in air-gapped environments. On license expiry, Cooker degrades to Explorer limits — it does not brick or stop serving your existing pipelines.

### Where can I run Crew?

Any Linux server where you can run a Docker container or a Go binary. Crew is tested on standard Debian/Ubuntu VMs, managed Kubernetes (EKS, GKE, AKS), and bare-metal. The Helm chart and raw Kubernetes manifests are included. No cloud-specific lock-in.

### Do you offer support for open-source projects?

Explorer is free forever, which covers most open-source and personal projects. If you are working on a significant open-source project and need Crew features, contact us — we evaluate sponsorship arrangements case by case.

---

## Still have questions?

Open a [GitHub Discussion](https://github.com/your-org/cooker/discussions) or email the team. We read everything.
