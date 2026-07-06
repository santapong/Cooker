# Where to Look for Prior Art

Search these sources in order — cheapest and most durable reuse first. The goal is to find what already solves the need before a line of bespoke code is written.

## 0. The standard library and platform (always first)

Before any dependency, check what the language, runtime, and platform you already run on give you for free. The best dependency is no new dependency — it adds zero supply-chain, upgrade, or lock-in cost.

- Language stdlib: string/date/collection/crypto/HTTP utilities are often already there.
- Runtime/platform features: the OS, browser, database, or cloud you already use frequently covers the need (e.g. Postgres for a job queue or full-text search before adding Kafka/Elasticsearch; the browser's `Intl`, `fetch`, `crypto.subtle` before a polyfill).
- Rule: if a stdlib call plus ten lines does it, that beats a dependency.

## 1. Ecosystem package registries

Search your language's registry and its metadata (downloads, last release, dependents):

| Ecosystem | Registry | Signal to read |
|---|---|---|
| JavaScript / TypeScript | npm | weekly downloads, last publish, dependents |
| Python | PyPI | releases, downloads (pypistats), maintained status |
| Rust | crates.io | downloads, recent version, reverse deps |
| Java / Kotlin | Maven Central | latest version, usages |
| Go | pkg.go.dev / Go modules | imported-by, versions |
| Ruby | RubyGems | downloads, last release |
| .NET | NuGet | downloads, latest version |
| PHP | Packagist | installs, maintenance |

Curated "awesome-<topic>" lists and the ecosystem's well-known meta-libraries are fast ways to find the field's standard options.

## 2. Managed / cloud services

For a capability, ask whether to buy the *operational burden* rather than run it: managed queues, auth/identity (OAuth/OIDC providers), search, email/SMS, feature flags, payments, object storage, observability. A managed service often wins not on code but on not paging someone at 3am. Weigh cost and lock-in (see `evaluation-criteria.md`).

## 3. Standards and specifications

If the problem is "represent/exchange/parse X", there is probably a standard: IETF RFCs (protocols, auth, dates — RFC 3339), W3C (web), ISO, OpenAPI/JSON Schema, established file formats, and protocol specs (gRPC, GraphQL). Implement the standard or use a library that implements it — never invent a private dialect of a solved format.

## 4. Existing internal code

The wheel may already exist in your own repo, monorepo, or organization's shared packages. Search the codebase and internal registries before adding anything external; an internal utility that already fits beats a new third-party dependency.

## How to search effectively

- Query by **capability and synonyms**, not your imagined API ("rate limiting", "token bucket", "throttle").
- Read real signals, not vanity metrics: last release date, open/closed issue ratio, release cadence, number of maintainers, and *dependents* (who relies on it) over raw star counts.
- Delegate the search and fact-checking to the `loop-research` skill so adoption/maintenance/security claims are verified against primary sources (the repo, the registry, advisories) rather than asserted from memory.
