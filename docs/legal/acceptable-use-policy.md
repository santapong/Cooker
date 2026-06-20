> ⚠️ **TEMPLATE — requires review by qualified legal counsel before use.**
> This is a non-binding draft, not legal advice. The right to suspend/terminate abusive
> builds must be drafted so it is enforceable without creating "we monitor everything"
> liability. Do not publish until counsel has reviewed it. Fill in every `[BRACKET]`.

# Cooker — Acceptable Use Policy (Template)

**Last updated:** [DATE]

This Acceptable Use Policy ("**AUP**") governs your use of the Cooker software and hosted
service (together, the "**Service**") and is incorporated into the
[Terms of Service](terms-of-service.md). It applies to everyone who uses the Service.

> **Why this policy is load-bearing.** Cooker runs **arbitrary build and deploy code you
> supply** — test stages, custom shell scripts, Dockerfile `RUN` steps, and deployment
> actions against infrastructure you direct it at. That makes the Service a powerful
> general-purpose code-execution and network-egress surface. This AUP is the rulebook for
> not abusing it, and the legal backstop to the technical controls described in
> `docs/launch/04-security-compliance-legal.md` §3.

---

## 1. You are responsible for what you run

You are solely responsible for all code, commands, container images, configuration, and
deployments you execute through the Service, and for ensuring you have the right to run them
against every system, registry, cluster, and account they touch.

## 2. Prohibited uses

You may **not** use the Service to:

### 2.1 Abuse the execution surface
- Mine or generate cryptocurrency, or run proof-of-work / proof-of-stake workloads
  ("**crypto-mining**"), or run any workload whose primary purpose is to consume compute for
  value extraction.
- Run distributed computing, password/hash cracking, or brute-force workloads.
- Deliberately consume excessive CPU, memory, storage, build minutes, network egress, or
  job-queue capacity ("**resource abuse**"), including fork bombs, infinite build loops, or
  intentionally pathological builds.
- Circumvent or attempt to circumvent quotas, rate limits, sandboxes, or isolation
  boundaries.

### 2.2 Attack or harm third parties
- Launch denial-of-service (DoS/DDoS), flooding, or amplification attacks.
- Scan, probe, penetrate, or attempt to gain unauthorized access to any network, system, or
  account you are not authorized to test.
- Build, host, or distribute **malware**, ransomware, exploit kits, botnet command-and-
  control, phishing kits, or spam infrastructure.
- Exfiltrate data from, or pivot into, systems you are not authorized to access — including
  using the Service's network egress as a relay/proxy for unauthorized traffic.

### 2.3 Misuse credentials and data
- Use credentials, tokens, registries, or clusters you are not authorized to use.
- Store or process data you are not legally permitted to (e.g. another party's secrets).
- Attempt to access, read, or interfere with **other customers'** data, secrets, builds,
  artifacts, or workloads, or with the Service's control plane.

### 2.4 Illegal or infringing content
- Violate any applicable law or regulation, including export-control and sanctions law.
- Infringe intellectual-property rights or build/distribute infringing artifacts.
- Produce or distribute unlawful content (e.g. CSAM, content that incites violence).

### 2.5 Operational abuse
- Interfere with, degrade, or disrupt the Service or its infrastructure.
- Falsify identity, evade suspension/termination, or create accounts to circumvent limits.
- Resell or sublicense the hosted Service except as expressly permitted.

## 3. Resource and fair-use limits

The Service enforces technical limits (per-user/per-tenant rate limits, and — where
configured — build/deploy quotas) to keep it available for everyone. **[State concrete
limits per plan: max concurrent builds, build-minute cap, max build duration, egress cap,
storage cap.]** We may adjust limits to protect the Service. Sustained or deliberate
attempts to exceed limits are resource abuse under [§2.1](#21-abuse-the-execution-surface).

## 4. Security research

We welcome good-faith security research **on your own resources and account only**. Do not
test against other customers, the shared control plane, or third parties without
authorization. Report vulnerabilities per `SECURITY.md`. Good-faith research that follows
the disclosure policy will not be treated as an AUP violation.

## 5. Enforcement

We may, at our discretion and to the extent permitted by law:

- **Throttle, suspend, or kill** any build, deployment, job, or account that we reasonably
  believe violates this AUP — **immediately and without prior notice** where necessary to
  protect the Service, other users, or third parties (e.g. an in-progress mining job, a
  detected attack, or runaway resource consumption).
- Remove or disable access to violating content or artifacts.
- Suspend or terminate the account per the [Terms of Service](terms-of-service.md) §12.
- Cooperate with law enforcement and report illegal activity as required.

We will use reasonable efforts to notify you of enforcement actions and, where appropriate,
give you an opportunity to cure — but immediate protective action takes priority over
notice.

> **Monitoring note (counsel to review).** We monitor aggregate resource usage and
> security signals to enforce this AUP. We do **not** routinely inspect the contents of
> your builds beyond what is necessary for operation, security, and abuse prevention. The
> exact monitoring representations must be aligned with the [Privacy Policy](privacy-policy.md)
> and your jurisdiction's law.

## 6. Reporting abuse

Report suspected abuse, attacks, or illegal use to **[ABUSE EMAIL]**.

## 7. Changes

We may update this AUP; material changes are notified per the
[Terms of Service](terms-of-service.md) §13. Continued use after changes take effect
constitutes acceptance.

---

**Related:** [Terms of Service](terms-of-service.md) · [Privacy Policy](privacy-policy.md) · [SLA](sla.md) · `SECURITY.md`
