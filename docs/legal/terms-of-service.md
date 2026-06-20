> ⚠️ **TEMPLATE — requires review by qualified legal counsel before use.**
> This is a non-binding draft, not legal advice. Do not publish or rely on it until a
> qualified attorney has reviewed and adapted it to your jurisdiction and business.
> Fill in every `[BRACKET]` placeholder.

# Cooker — Terms of Service (Template)

**Last updated:** [DATE]
**Provider:** [LEGAL ENTITY NAME], [ENTITY TYPE], [ADDRESS] ("**Cooker**", "**we**", "**us**").

These Terms of Service ("**Terms**") govern your use of the Cooker software and, where
offered, the Cooker hosted service (together, the "**Service**"). By installing, accessing,
or using the Service, you ("**you**", "**Customer**", "**User**") agree to these Terms. If
you use the Service on behalf of an organization, you represent that you have authority to
bind that organization.

> **Two deployment models, two scopes.** Cooker is offered (a) as **self-hosted software**
> you run on your own infrastructure, and (b) potentially as a **hosted service** we
> operate. Sections marked **[Self-hosted]** or **[Hosted]** apply only to that model.
> Sections without a marker apply to both.

---

## 1. The Service

Cooker is a CI/CD management tool with a graph-based UI for building OCI-compliant
container images, pushing them to registries, and deploying workloads to Kubernetes across
environments. **The Service executes code and configuration that you provide** — build
plans, test stages, custom shell scripts, Dockerfiles, and deployment definitions.

**You are solely responsible for the code, commands, images, and configuration you run
through the Service**, and for the systems, registries, clusters, and credentials they
target. See the [Acceptable Use Policy](acceptable-use-policy.md) ("**AUP**"), which is
incorporated into these Terms by reference.

## 2. License

### 2.1 [Self-hosted] Software license
The Cooker source code is licensed under the terms in the repository `LICENSE` file. These
Terms do not replace that license; they govern **your use of the Service as a product**
and any commercial-use terms layered on top. In the self-hosted model **you operate the
software and you are the data controller** for all data it processes (see the
[Privacy Policy](privacy-policy.md)).

### 2.2 [Hosted] Right to use
Subject to these Terms and timely payment of fees, we grant you a non-exclusive,
non-transferable, revocable right to access and use the hosted Service during your
subscription term.

### 2.3 Restrictions
You may not, except as expressly permitted by the applicable software license: (a) resell
or sublicense the hosted Service; (b) reverse engineer the hosted Service except where
permitted by law; (c) remove proprietary notices; or (d) use the Service in violation of
the AUP or applicable law.

## 3. Accounts, access, and security

- You must keep authentication credentials, API tokens, and OIDC sessions confidential.
- You are responsible for all activity under your account or tokens.
- You must configure least-privilege RBAC and rotate credentials per good practice.
- **[Self-hosted]** Securing the deployment (OIDC, TLS, secrets backend, network policy,
  container hardening) is your responsibility; we publish guidance (`SECURITY.md`, the
  install and runbook guides) but do not operate your instance.

## 4. Customer data and content

- **"Customer Data"** means data you submit to or generate through the Service, including
  pipeline/app/environment definitions, secrets and deploy credentials you store, run
  history, and build/stage logs.
- **[Hosted]** As between the parties, you own your Customer Data. You grant us a limited
  license to process it solely to provide, secure, and support the Service. Our handling of
  personal data is described in the [Privacy Policy](privacy-policy.md) and, where
  applicable, a Data Processing Addendum ("**DPA**").
- **Secrets and credentials.** The Service stores secrets you provide (registry creds,
  deploy creds, SSH keys) encrypted at rest. **You are responsible for the validity, scope,
  and revocation of any credentials you store**, and for the consequences of granting the
  Service access to your registries and clusters.
- **Logs may contain whatever your build prints.** Build and stage logs can capture
  secrets your code emits. You are responsible for not printing sensitive data into build
  output.

## 5. Fees and payment **[Hosted]**

- Fees, billing cycle, and plan limits are as described at checkout or in your order.
- Payments are processed by **[Stripe]**; card data is handled by the payment processor and
  never stored by us (see the [Privacy Policy](privacy-policy.md) sub-processor list).
- Fees are non-refundable except as required by law or expressly stated.
- We may change pricing on prospective notice of at least **[30] days**.

## 6. Acceptable use

Your use of the Service is governed by the [Acceptable Use Policy](acceptable-use-policy.md).
Because the Service runs code you supply, the AUP is a material term. **We may suspend,
throttle, or terminate any build, deployment, or account that violates the AUP**, including
immediately and without notice where necessary to protect the Service, other users, or
third parties.

## 7. Third-party services

The Service integrates with third parties you choose (container registries, Kubernetes
clusters, identity providers, secrets backends, source hosts, notification channels). Your
use of those services is governed by their terms. We are not responsible for third-party
services or for actions the Service takes against systems you direct it to.

## 8. Service levels **[Hosted]**

Any service-level commitment is set out in the [SLA](sla.md) or your order. **[Self-hosted]
deployments carry no service-level commitment** — availability depends entirely on how you
operate the software.

## 9. Warranty disclaimer

**THE SERVICE IS PROVIDED "AS IS" AND "AS AVAILABLE", WITHOUT WARRANTIES OF ANY KIND,
EXPRESS OR IMPLIED, INCLUDING IMPLIED WARRANTIES OF MERCHANTABILITY, FITNESS FOR A
PARTICULAR PURPOSE, TITLE, AND NON-INFRINGEMENT.** We do not warrant that the Service will
be uninterrupted, error-free, or secure, or that it will meet your requirements. **You
acknowledge that the Service executes code you provide and deploys to infrastructure you
control, and that the outcomes of those operations are your responsibility.**

Some jurisdictions do not allow the exclusion of implied warranties; in those
jurisdictions the above exclusions apply to the maximum extent permitted by law.

## 10. Limitation of liability

**TO THE MAXIMUM EXTENT PERMITTED BY LAW, NEITHER PARTY WILL BE LIABLE FOR ANY INDIRECT,
INCIDENTAL, SPECIAL, CONSEQUENTIAL, OR PUNITIVE DAMAGES, OR FOR LOST PROFITS, REVENUE,
DATA, OR GOODWILL, ARISING OUT OF OR RELATED TO THESE TERMS OR THE SERVICE, EVEN IF
ADVISED OF THE POSSIBILITY.**

**OUR TOTAL AGGREGATE LIABILITY ARISING OUT OF OR RELATED TO THESE TERMS WILL NOT EXCEED
THE GREATER OF (A) THE FEES YOU PAID US IN THE [12] MONTHS BEFORE THE CLAIM, OR (B)
[USD 100].** For **[Self-hosted]** use where no fees are paid, our aggregate liability is
limited to **[USD 100]** to the extent permitted by law.

These limits do not apply to liability that cannot be excluded by law (e.g. death or
personal injury caused by negligence, or fraud).

## 11. Indemnification

You will defend and indemnify us against third-party claims arising from (a) your Customer
Data or content, (b) your use of the Service in violation of these Terms or the AUP, or
(c) code, images, or deployments you run through the Service that harm a third party.

## 12. Suspension and termination

- You may stop using the Service at any time. **[Hosted]** you may cancel a subscription
  per your plan.
- We may suspend or terminate your access for (a) breach of these Terms or the AUP,
  (b) non-payment **[Hosted]**, or (c) to comply with law or protect the Service or others.
- **[Hosted]** On termination, we will make Customer Data available for export for **[30]
  days**, after which it may be deleted per the [Privacy Policy](privacy-policy.md)
  retention schedule.

## 13. Changes to these Terms

We may update these Terms. For material changes we will provide reasonable notice
(e.g. in-product notice or email **[Hosted]**, or a changelog entry **[Self-hosted]**).
Continued use after changes take effect constitutes acceptance.

## 14. Governing law and disputes

These Terms are governed by the laws of **[GOVERNING LAW JURISDICTION]**, excluding its
conflict-of-laws rules. The parties submit to the exclusive jurisdiction of the courts of
**[VENUE]**, except that either party may seek injunctive relief in any court of competent
jurisdiction. **[Optional: arbitration / class-action-waiver clause — counsel to advise.]**

## 15. Miscellaneous

Entire agreement; severability; no waiver; assignment (we may assign in a merger or sale of
assets; you may not assign without consent); force majeure; notices to **[NOTICE ADDRESS /
EMAIL]**.

---

**Contact:** [LEGAL CONTACT EMAIL]
**Related:** [Privacy Policy](privacy-policy.md) · [Acceptable Use Policy](acceptable-use-policy.md) · [SLA](sla.md)
