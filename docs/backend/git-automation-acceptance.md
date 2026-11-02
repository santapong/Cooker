# Local Git automation acceptance

The opt-in `TestGiteaWebhookToDockerLive` exercises a private Gitea repository,
actual provider-generated signed HTTPS webhook, real Cooker handlers and rule
preview, PostgreSQL admission, the worker, and Docker Compose deployment.

From `backend/`, with Go 1.25, Docker/Compose, Git 2.43+ and loopback host
networking available:

```sh
COOKER_GITEA_FLOW_ACCEPTANCE=true go test -tags integration ./internal/handler \
  -run '^TestGiteaWebhookToDockerLive$' -count=1 -v -timeout=8m
```

The fixture creates its own Gitea, PostgreSQL, private repository, read-only
source token and webhook secret. A generated CA protects the HTTPS source
proxy and another trusted fixture CA protects the HTTPS webhook receiver.
Certificate verification remains enabled. All listeners are local; the fixture
does not publish a public route or use an existing account's credentials.

The administrator/MFA identity and governance-disabled policy are explicit
fixture prerequisites. The test creates the source connection and App through
handlers, rejects saving a rule without review, and obtains hashes from the real
preview/save contract. It does not seed rules, intents, plans, hashes or jobs.

A repository update makes Gitea send its own signed delivery. The worker
checks the accepted source and image plan, deploys digest-pinned Redis and
requires a healthy PONG. A second database connection reads durable stage
logs, and runtime logs are attached to the recorded container identity.
Replaying the authenticated delivery creates no additional job; an invalid
signature is rejected. Cleanup removes the exact owned provider, database,
workload, networks and volumes.

## Observed result — 28 September 2026, 21:07 ICT

The final-source run passed in **19.07 seconds** on Gitea 1.26.4. All source
hashes matched the implementation worktree after the final lint cleanup.
Integration-tagged handler `go vet` also passed. Evidence:
`/mnt/data/cooker-gitea-flow-final-20260928/acceptance.json`, `run.log`,
`compile.log`, `vet.log` and `source-sha256.json`.

This proves the local configured path through those real components. It does
not prove hosted GitHub/GitLab behavior, a real OIDC session, governance-enabled
delegation, internet ingress, a source build in this same run, or cloud/Kubernetes
execution. BuildKit/Crane source publication has separate acceptance evidence.
