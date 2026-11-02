# Source adapter foundation — 28 September 2026

This records the bounded M3 source foundation in `feature/backend-automation-20260928`. It is not a claim that the full M3 trigger/recovery exit gate or hosted-provider UAT has passed. The root implementation integrates these adapters separately into App inspection, accepted plans, webhooks and jobs.

## Implemented boundary

`internal/source/gittransport.Adapter` has these operations:

- `Repository(ctx, pathOrID)` returns stable numeric provider ID, canonical path, approved origin and default branch.
- `ResolveRevision(ctx, repository, Ref{Kind,Name})` returns a typed full ref, immutable commit and original object SHA. A branch and tag of the same spelling remain distinct.
- `VerifyCommit(ctx, repository, sha)` checks an already accepted commit without resolving a moving branch again.
- `ListRevisions(ctx, repository, kind, page)` returns at most 50 entries and an explicit next page. Gitea resolves each listed typed ref to avoid ambiguous tag/list SHA semantics.
- `Checkout(ctx, repository, commit, logWriter)` returns a private directory. Its caller owns `os.RemoveAll(dir)`. It fetches the full accepted commit, verifies a commit object and exact HEAD, and never substitutes the current branch tip.

GitLab and Gitea constructors take approved `gittransport.Config` and in-memory `Credentials`. GitLab supports nested group paths and v4 project/branch/tag/commit APIs. Gitea uses v1 stable repository lookup and Git refs, verifies an exact match from the prefix-filter refs endpoint, and peels annotated tags with a bounded, cycle-checked chain. API clone URLs, object URLs and pagination URLs never choose a destination. A changed repository path requires identity refresh; it cannot silently select a replacement at the old path.

Connection records retain compatibility with `Create(ctx,connection,webhookSecret)` and `Update(ctx,connection,*webhookSecret)`. `CreateWithCredentials`, `UpdateWithCredentials` (pointer patch fields), and `OpenCredentials` add encrypted token storage and explicit rotation without discarding an unchanged webhook key. JSON reads contain `hasToken`/`hasWebhookSecret`, never decrypted tokens. A connection cannot change provider or origin in place. Token username changes require explicit rotation. GitHub App installation and token modes cannot be combined.

The 3 October candidate adds [repository token replacement and recovery](source-token-replacement.md): administrator Settings perform a bounded metadata check for every allowed repository before a version-fenced save. This retains the webhook/configuration and distinguishes an uncertain save from a confirmed failure. The check does not certify checkout or complete the GIT-05 missed-event recovery gate.

The separate [manual missed-ref recovery contract](missed-ref-recovery.md) checks current reviewed authority before App source access and retains the shared webhook inbox's matching-rule and deduplication semantics. Automatic sweeps and hosted/official-server acceptance remain separate.

GitLab webhook mode is explicitly `signing` or `legacy_token`; there is no automatic downgrade. New signing connections require the documented `whsec_` prefix and a base64-encoded 32-byte key. GitHub and Gitea retain signing mode. Secret verification itself is implemented by the webhook boundary.

## Network, process and credential constraints

Only canonical HTTPS origins are accepted. GitLab/Gitea API and Git origins must match; fixed API prefixes are `/api/v4` and `/api/v1`. Paths, embedded credentials and URL queries are rejected in origin configuration. Instance administrators must authorize creation and the explicit private-network flag; a verified event supplies facts, not new network authority.

Private addresses and loopback require explicit approval. Metadata, link-local, multicast, reserved and supported address-translation ranges remain denied even with that flag. Each HTTP dial validates all DNS results and connects directly to an approved IP. Ambient proxies and all HTTP redirects are disabled. Git resolves once and pins a validated IP using `http.curloptResolve`; TLS still verifies the original hostname. A failed pinned address is not replaced with a new DNS result. Custom trust accepts bounded PEM CA certificates only; verification cannot be disabled.

Checkout requires Git 2.43 or newer. It uses a minimal environment without control-plane secrets, global/system configuration, credential helpers, hooks, ambient proxies or interactive prompts. Only HTTPS is permitted. The token is an origin-scoped header in the short-lived process environment, not a URL, command argument or repository setting. Server stdout/stderr is not copied to user logs; Cooker emits its own progress. API errors contain status only and no provider response body. Temporary CA/environment files and partial checkout directories are removed normally on success/failure. Legacy GitHub clone now reuses the minimal environment and restrictive settings; its existing hard-coded-host flow is otherwise preserved.

Submodules and configured content filters (including LFS) fail closed. No implicit secondary fetch is permitted. HTTP responses are capped at 2 MiB and captured Git metadata at 16 MiB. Checkout has a five-minute bound. This is not a disk quota or an OS sandbox: deployment administrators still need worker resource limits and orphan-directory cleanup after process/machine failure. Large repositories that exceed these constraints need a separately designed policy. SSH, OAuth account linking, CI/deploy tokens, sub-path hosting and separate API hosts are not advertised by this slice.

## Verification and reproducibility

All `./internal/source/...` tests passed in a frozen source copy with Go 1.25.14. Security/contract cases include isolated environments, rejected redirects/untrusted CA/private or metadata destinations, bounded response/output capture, nested GitLab paths, slash refs, same-name branch/tag, lightweight/annotated tag resolution, cycle rejection, scoped repository identity, token revocation, pagination and credential-preserving rotation.

A real Git smart-HTTPS fixture exercises the shared transport with the actual Git CLI. It verifies a moved branch cannot change an accepted commit, revoked credentials fail, token values do not persist in Git configuration, and submodules/LFS fail before any additional fetch.

A real **Gitea 1.26.4** fixture passed private repository API access and Git checkout with a generated read-only repository token, stable identity, slash branch/tag collision, annotated-tag peeling, branch advancement after acceptance, exact source marker/HEAD, no persisted token, and revoked-token rejection. It uses a generated test CA at a loopback TLS proxy and an isolated rootless container; it is not a hosted GitHub/GitLab/Gitea account check or a complete webhook-to-runtime deployment check. The integration test removes its own container/volumes and source directory. It never uses existing accounts, credentials or services.

Run only against a disposable fixture:

```sh
# On the host, cache this pinned server image.
docker pull gitea/gitea:1.26.4-rootless
# Build the package test binary with Go 1.25 and CGO_ENABLED=0, then run it
# on a host with Docker and Git. This avoids Docker-in-Docker paths.
COOKER_GITEA_ACCEPTANCE=true ./gitea-acceptance.test \
  -test.run '^TestGiteaLiveSourceAcceptance$' -test.v -test.timeout 5m
```

Test: `backend/internal/source/gitea/adapter_live_integration_test.go`. Evidence log: `/mnt/data/cooker-gitea-source-acceptance-20260928.log`; source suite: `/mnt/data/cooker-m3-source-tests-20260928.log`. The first fixture setup got Gitea HTTP 409 when deleting/recreating a branch sharing a tag name. Updating the existing branch with the contents API preserved the required collision and moving-ref proof; the corrected fixture passed. Failed and successful fixtures were removed.

GitLab has API contract tests plus the shared real Git transport check; a disposable real GitLab instance and hosted/private-account acceptance have **not** been run. Do not call the entire M3 exit gate complete from this evidence.

## Primary references checked

- [GitLab branches](https://docs.gitlab.com/api/branches/) and [tags](https://docs.gitlab.com/api/tags/) distinguish typed endpoints; tag `commit.id` is the commit and annotated `target` is the tag object.
- [GitLab project webhook API](https://docs.gitlab.com/api/project_webhooks/) specifies signing-token shape; [signature verification](https://docs.gitlab.com/user/project/integrations/webhooks/) documents the signed message and headers.
- [Gitea stable repository ID](https://docs.gitea.com/api/1.26/operations/repo-get-by-id/), [Git refs](https://docs.gitea.com/api/1.26/operations/repo-list-git-refs/) and [annotated tags](https://docs.gitea.com/api/1.26/operations/get-annotated-tag/) define the tested v1 contracts.
- [Git configuration](https://git-scm.com/docs/git-config) documents host-scoped headers, redirects, `http.curloptResolve`, hooks and protocol controls.
