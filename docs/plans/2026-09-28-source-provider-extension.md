# Source provider extension — bounded implementation proposal

Inspected 28 September 2026 in `feature/backend-automation-20260928`. This is a proposed M2/M3 extension boundary, not implemented GitLab/Gitea source support. The new queued deployment path is being implemented separately; existing provider webhook parsing is not evidence of a working source checkout.

## Current coupling and first correction

- `backend/internal/model/app.go` identifies source through `GitHubRepo`, `Branch` and `BuildPlan.InstallationID`.
- `backend/internal/source/github/app.go` owns repository browsing, revision inspection, installation tokens and GitHub source authorization.
- `backend/internal/source/github/clone.go` derives an HTTPS URL on `github.com`, permits an `owner/name` path, and binds an in-memory token to that host. It supports exact-SHA checkout but is not a generic GitLab/Gitea client.
- The current `AppDeployQueue.ResolveCommit` function takes a GitHub installation ID, repository and ref. The queue payload records the resolved SHA, but does not yet identify a provider connection.
- `source/gitlab` and `source/gitea` contain payload decoding and webhook verification only. GitLab already exposes `group/subgroup/repo` as an event path, which the GitHub clone validator cannot accept.

Keep capability reporting explicit: GitHub has source inspection/checkout; other existing webhook receivers have verification only until a complete connection adapter passes acceptance. Do not infer the clone origin from an event's repository URL.

## Minimal data and service boundary

Introduce one `SourceConnection` record with an immutable ID, provider (`github`, `gitlab`, `gitea`), canonical HTTPS origin, API base, credential reference, optional GitHub installation ID, version and disabled state. Store secret references only. Restrict connection creation/network policy to instance administrators; normal deployments consume an authorized connection.

An App gains a source reference containing connection ID, stable provider repository ID, canonical display path, and a typed ref (`branch` or `tag`, with its full ref name). A resolved deployment plan records those identities, connection version, requested ref, full commit and source configuration version. A repository rename may update display data; it must not silently substitute a different repository. The same `team/app` on two origins is two distinct sources.

Use a provider interface shared by inspection, manual planning and triggers:

- `Repository`: fetch and verify the selected repository's stable identity and read access.
- `ListRevisions`: list branches/tags with pagination and explicit kind.
- `ResolveRevision`: turn a typed branch/tag or expected event commit into a verified full commit.
- `Checkout`: fetch the exact recorded commit into a private, bounded work directory and verify `HEAD` before returning.
- `Capabilities`: report supported operations and configured constraints without claiming that a network probe or deployment already passed.

Preserve GitHub App token minting inside its adapter. Reuse a small Git transport helper for process isolation and exact-object checkout; do not move GitHub-specific installation semantics into the other providers. The GitLab/Gitea adapters can start with narrowly scoped read credentials from the existing secret manager. OAuth account linking and automatic webhook registration are later convenience work, not prerequisites for a manually configured source connection.

## Identity, refs and credential rules

The configured connection defines origin and repository scope. A signed webhook provides event facts, not authority to choose an arbitrary clone URL. New webhook routes should identify the connection before verification; normalize a verified event to connection ID, stable repository ID, ref kind/name, before/after object, deletion state and delivery identity. Keep the existing provider signature algorithms at the boundary.

Branch and tag names can be identical. Preserve their type throughout inspection and trigger rules, resolve annotated tags to commits, and record both event identity and resolved commit when they differ. A queued deployment never re-resolves a moving branch. Deleted refs produce an ignored decision, not a new deployment. A force push cannot change the already accepted plan.

GitLab supports a project ID or encoded project path and a branch/tag/commit in its [commit endpoint](https://docs.gitlab.com/api/commits/). Prefer the stable project ID after connection. GitHub's [commit endpoint](https://docs.github.com/en/rest/commits/commits) accepts qualified branch/tag refs and full commit IDs. Gitea exposes [typed Git refs](https://docs.gitea.com/api/1.26/operations/repo-list-git-refs/); pin and test the supported server version, including annotated-tag peeling, before advertising equivalence.

Bind credentials to the exact configured origin and do not place them in clone URLs, command arguments, repository configuration, logs or queued payloads. Disable redirects across origins. Use a minimal subprocess environment, disabled interactive prompts and credential helpers, disabled hooks, and explicit protocol restrictions. Do not inherit the control-plane database/cloud environment. Submodules and Git LFS should initially return a clear unsupported/review-needed result unless their additional origins and credentials are explicitly configured.

Self-hosted GitLab/Gitea needs private-network access. Make approved origins and optional private-network egress an explicit operator policy; do not globally allow metadata/link-local addresses or arbitrary event-controlled hosts. Validate both API and Git transport destinations, certificate trust, redirects and resolved addresses. Custom CA support must not become a global TLS-verification bypass.

## Compatibility and execution order

1. Add the source interface and a GitHub adapter around the existing client. Keep legacy `GitHubRepo`/installation fields readable through a compatibility resolver. Existing Apps retain behavior; no silent database-wide rewrite or credential movement.
2. Add source connection persistence and API validation with encrypted references, authorization, versioning and redacted reads. Reject conflicting legacy/new source fields on writes. Populate the immutable plan's source identity; do not mutate accepted plans when a connection changes.
3. Move inspection and queued checkout through the shared interface, retaining the current GitHub integration tests. Add a conformance suite with exact-commit and credential-boundary tests.
4. Complete the GitHub trigger decision/inbox work against that interface: one verified event may match multiple Apps, while one event/rule/target intent has one stable receipt. Record matched/ignored/error reasons and keep enqueue acceptance durable.
5. Implement GitLab then Gitea API/checkout adapters, with their provider-specific repository paths, credential model and typed-ref resolution. A real disposable self-hosted source instance is suitable acceptance for each; GitHub.com and hosted GitLab account UAT remain separately labeled.

## Required acceptance cases

- Manual and webhook deployments inspect and build the same exact commit even when a branch advances or is deleted after queueing.
- A branch and a tag with the same spelling select different objects; lightweight and annotated tags both resolve correctly.
- Same repository path on two connections cannot cross-match events or credentials; a renamed repository retains stable identity.
- GitLab nested group paths survive API encoding and checkout; traversal, leading-option refs and malformed refs are rejected.
- Invalid signatures, disabled connections, missing credentials, unauthorized repositories, duplicate deliveries and deletion events produce explicit results without deploying.
- Redirects, alternate origins, blocked network destinations, hooks and uncontrolled submodule fetches cannot bypass source policy.
- Provider contract tests and a real checkout/build fixture are reported separately. A receiver test alone never enables `sourceCheckout: available`.

The first implementation slice should be the compatibility-preserving source interface plus the GitHub adapter. It provides a stable boundary for M2 now and prevents GitLab/Gitea from inheriting GitHub-only assumptions later. No provider implementation or credentials were changed by this proposal.
