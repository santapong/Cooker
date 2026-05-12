# Shipping Go: how mature OSS Go products release and operate — and what Cooker should copy

> Status: research deliverable. No code in this doc; recommendations only.
> Author: research pass, 2026-05.
> Scope: Cooker (Go API + React frontend, single binary, OCI/K8s CI/CD tool).
> Audience: a senior engineer who will execute a 0–180 day adoption plan.

---

## Executive summary

1. **Pick GoReleaser, skip semantic-release.** Every reference project except Grafana and Gitea uses GoReleaser for cross-compile + archive + checksum + SBOM + cosign signing in one declarative file. Gitea's hand-rolled Makefile approach is a maintenance tax; Grafana's Dagger build is over-scoped for our team size. Pair GoReleaser with `release-please` only if you want a Conventional Commits → changelog → PR loop; tagging by hand is fine for the first year.
2. **Cooker's biggest distribution gap is "we don't actually ship binaries."** The Helm chart is decent; the Docker image builds in CI but isn't published anywhere; there is no `go install` story, no `apt`/`brew`/`scoop` channel, no `cooker --version` that reports a real tag. That is the *first* thing to fix — it unlocks everything else (signed releases, SBOM, release notes, etc.).
3. **Observability is already on the right path; double down.** `log/slog` JSON, optional Prometheus, optional OTLP — that matches what every modern Go OSS project (Caddy, ArgoCD, Woodpecker) ended up at. The work left is healthz/readyz separation, pprof behind auth, and a "minimum-viable dashboard" Grafana JSON to ship in `deploy/dashboards/`.
4. **Configuration: env-vars is fine, but Cooker is at the point where a YAML overlay is overdue.** With 60+ `COOKER_*` variables (config.go is 200+ lines), operators will start asking for a config file. Don't replace env vars — keep them as the highest-precedence override, but add a `cooker.yaml` parsed first. This is exactly the Caddy/Gitea pattern.
5. **Don't build a plugin system. Use module-tags or `xcaddy`-style rebuild.** Cooker already has the right shape — `selectBuilder`, `selectPusher`, `selectDeployer` choose adapters at startup from env-vars. Don't introduce `hashicorp/go-plugin` or WASM until the adapter list is in the dozens. Document the "fork-and-recompile" path instead; that's what Caddy ships.

---

## How to read this doc

For each of the 10 topics:

- **Industry pattern** — what mature projects actually do, with links to their config files.
- **Cooker today** — what's in this repo right now (read from `Makefile`, `.github/workflows/`, `deploy/`, `backend/cmd/cooker/main.go`).
- **Recommended for Cooker** — concrete tool, file path, command. Opinionated.

A prioritized 0–30 / 30–90 / 90–180 day plan lives at the end. An appendix indexes every reference file by topic so you can copy-paste their patterns.

---

## 1. Release engineering

### Industry pattern

- **GoReleaser is the consensus tool.** Caddy ([`.goreleaser.yml`](https://github.com/caddyserver/caddy/blob/master/.goreleaser.yml)), ArgoCD ([release workflow](https://github.com/argoproj/argo-cd/blob/master/.github/workflows/release.yaml) — calls `goreleaser`), and the long tail of Go OSS use it. One YAML file declares cross-compile targets, archive formats, `nfpms` (deb/rpm), Homebrew tap, Scoop bucket, checksum, SBOM via syft, and `signs:` with cosign.
- **Hugo** uses [`hugoreleaser.yaml`](https://github.com/gohugoio/hugo/blob/master/hugoreleaser.yaml) — its own GoReleaser-compatible tool — because it needs four edition variants (`standard`, `extended`, `deploy`, `extended_withdeploy`) with CGO+cross-compiler matrices that vanilla GoReleaser doesn't model cleanly. Don't copy this unless you also need CGO build variants.
- **Gitea** hand-rolls cross-compilation in [its `Makefile`](https://github.com/go-gitea/gitea/blob/main/Makefile) (`release: release-windows release-linux release-darwin release-freebsd release-copy release-compress vendor release-sources release-check`). This is a maintenance burden; do not copy.
- **Grafana** drives release builds with [Dagger pipelines](https://github.com/grafana/grafana/blob/main/.github/workflows/release-build.yml) and a private build runner. Vastly over-engineered for any project under 50 maintainers.
- **Woodpecker CI** uses its own Woodpecker pipelines ([`.woodpecker/binaries.yaml`](https://github.com/woodpecker-ci/woodpecker/blob/main/.woodpecker/binaries.yaml)) with `make cross-compile-server` + `make bundle` (deb/rpm) + `make release-checksums`. Pragmatic mid-point.
- **Signing**: every serious project signs with `cosign sign-blob` (Caddy's pattern) and emits an SBOM. SLSA provenance via [`slsa-framework/slsa-github-generator`](https://github.com/slsa-framework/slsa-github-generator) is the ArgoCD pattern.
- **Versioning**: SemVer everywhere. Most projects ship **rolling minors with patch back-ports for the last 2–3 minor versions**; only Grafana and ArgoCD operate an explicit LTS line. Caddy's 134 releases over 6+ years and Hugo's 373 releases (≈1/week) show that the *cadence* matters more than the *label*.
- **Conventional Commits → release-please** is the dominant changelog-and-tag automation for projects that want it, but it is *additive* to GoReleaser — `release-please` opens the "release PR" that bumps `CHANGELOG.md` and tags; GoReleaser then sees the tag and runs.

### Cooker today

- Backend `main.go` exposes no `--version`. There's no `var Version = "dev"` populated via `-ldflags "-X main.Version=$(git describe)"`.
- There are **no release workflows**. `.github/workflows/` contains only `ci.yml`, `cooker-weekly.yml`, `oci-conformance.yml`. No tags trigger anything.
- `CHANGELOG.md` exists but appears to be hand-written.
- The Helm chart's `Chart.yaml` has a version, but it's decoupled from the binary version.

### Recommended for Cooker

1. **Add a `version` package now.** `backend/internal/version/version.go` exports `Version`, `Commit`, `BuildDate`. Populate via `-ldflags` in `Makefile` and `Dockerfile`. Surface via `/health` and a new `cooker version` subcommand. Cost: one hour.
2. **Adopt GoReleaser.** Drop a `.goreleaser.yaml` at the repo root. Start minimal:
   - Builds: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`. Skip Windows until someone asks.
   - Archive: `tar.gz` + `LICENSE` + `README.md`.
   - `nfpms`: deb only (rpm later).
   - `dockers`/`docker_manifests`: build and push multi-arch image to `ghcr.io/santapong/cooker`.
   - `sboms`: syft, cyclonedx-json.
   - `signs`: cosign keyless (`COSIGN_EXPERIMENTAL=1` with GH OIDC).
   - `release.draft: true` so a human eyeballs the first few.
3. **Add `.github/workflows/release.yml`** triggered on `push: tags: ['v*']`. Two jobs: GoReleaser, then `slsa-framework/slsa-github-generator` for binary provenance. Mirror [ArgoCD's two-job split](https://github.com/argoproj/argo-cd/blob/master/.github/workflows/release.yaml).
4. **Versioning policy** (write a one-page `docs/RELEASING.md`):
   - SemVer.
   - Rolling minors; security patches back-ported to last 2 minors only.
   - No LTS commitment until v1.0.
   - Conventional Commits encouraged but not enforced; we'll revisit `release-please` when there are 3+ regular committers.
5. **Decoupled Helm chart version.** `Chart.appVersion: x.y.z` follows the binary; `Chart.version` follows the chart itself. This is the Helm community convention. Bump the chart version independently when only template changes happen.

**Verdict on the tool fight:** GoReleaser yes; `semantic-release` no (Node toolchain, prescriptive, doesn't match Go ergonomics); `release-please` "maybe later." Hand-rolled Makefile pipelines like Gitea's are a non-starter for our team size.

---

## 2. Distribution

### Industry pattern

- **`go install` works on every project** because they all use `cmd/<name>/main.go` and `go.mod` at the repo root. It is the cheapest, most-used dev install path and costs nothing to support.
- **Binary GitHub Releases** are the canonical artifact. GoReleaser uploads `.tar.gz` + `.deb` + `.zip` + `checksums.txt` + `.sig` + `.sbom` next to each tag.
- **Docker images** — convention now is **multi-arch** (`linux/amd64` + `linux/arm64` minimum), pushed to **two registries** for redundancy (Docker Hub + GHCR or Quay; ArgoCD pushes to Quay, Caddy and Gitea also push to Docker Hub). Tags: `:vX.Y.Z`, `:vX.Y`, `:vX`, `:latest` (latest only on stable releases).
- **Helm charts**: hosted on a separate `helm/cooker` repo or as `oci://ghcr.io/<org>/charts/cooker` (OCI-distributed charts have been Helm 3 stable since 2021 and are now the default for ArgoCD, Caddy, and Woodpecker).
- **apt/yum**: serve a static repo. The reference here is **Tailscale's [pkgs.tailscale.com](https://pkgs.tailscale.com)** — they host their own apt/yum repos with `stable` and `unstable` tracks. Caddy uses [Cloudsmith](https://cloudsmith.io/) for the same thing. Both approaches are fine; Cloudsmith is free for OSS.
- **Homebrew**: GoReleaser's `brews:` block writes to a `homebrew-tap` repo automatically. Hugo, Caddy, ArgoCD all do this.
- **Scoop / winget**: GoReleaser also writes to a Scoop bucket; winget needs a separate `winget-pkgs` PR per release. Only matters if you have Windows users — skip for Cooker (it shells `kubectl` and `docker`, Windows is a stretch goal at best).

### Cooker today

- `go install github.com/santapong/cooker/backend/cmd/cooker@latest` — should work today, but no one has tested it because the backend module path in `go.mod` is `github.com/cooker-ci/cooker` (per `backend/cmd/cooker/main.go` swagger annotation `@contact.url`). **There's a module-path/repo-path mismatch.** Fix this before publishing.
- `make docker-build` builds `cooker:latest` locally but `make docker-push` pushes to bare `cooker:latest` — there's no registry namespace. No CI job pushes the image anywhere.
- Helm chart lives under `deploy/helm/cooker/` and is `helm install`able locally. Not published.
- No apt/brew/scoop story.

### Recommended for Cooker

1. **Reconcile the module path.** Either rename `go.mod` to `github.com/santapong/cooker/backend` or move the repo. The current state breaks `go install` and confuses operators reading `cooker --version` output.
2. **Tag → GHCR multi-arch image.** Add the `dockers:` and `docker_manifests:` blocks to `.goreleaser.yaml`. Push `ghcr.io/santapong/cooker:vX.Y.Z`, `:vX.Y`, `:latest`.
3. **Publish the Helm chart to GHCR as an OCI artifact.** One workflow step:
   ```
   helm package deploy/helm/cooker
   helm push cooker-<ver>.tgz oci://ghcr.io/santapong/charts
   ```
   Same registry, same auth, no second hosting bill. Document `helm install cooker oci://ghcr.io/santapong/charts/cooker --version X.Y.Z` in the README.
4. **`brews:` tap** — only worth it if you actually expect Mac admins. Defer until someone asks. The tap repo skeleton is two files; GoReleaser fills them.
5. **Do not chase apt/yum/scoop/winget.** Cooker is a server-side tool deployed via Helm or container, not a CLI users install locally. The Tailscale-grade pkg story exists because every Tailscale user installs the daemon on their laptop. Cooker users will `helm install` once per cluster.
6. **Add a one-line install script** for the bin (the `curl | sh` pattern Tailscale and Caddy both publish): `scripts/install.sh` that downloads the right arch from GitHub Releases and verifies the cosign signature.

---

## 3. Observability that ships well

### Industry pattern

- **Logging**: `log/slog` (stdlib, Go 1.21+) is the new default. Caddy and Tailscale use it directly; Woodpecker still has zerolog but is migrating. zap is the legacy pick — fine in maintenance mode, not what new code picks. ArgoCD uses logrus and is slowly migrating to slog.
- **Metrics**: Prometheus exposition format on `/metrics`, gated by config. The promhttp handler is standard.
- **Tracing**: OpenTelemetry SDK (`go.opentelemetry.io/otel`) with OTLP gRPC exporter. ArgoCD, Woodpecker, and Grafana all use this exact stack. Don't ship Jaeger client directly; OTel is the only future-proof choice.
- **Health endpoints**: split `/livez` and `/readyz` (the K8s convention). `/livez` returns 200 if the process is up; `/readyz` returns 200 only if DB connection is healthy. Grafana, ArgoCD, Woodpecker all do this. Many older projects (Caddy, Hugo) just have `/health`.
- **pprof**: exposed on a *separate* internal port (e.g. `:6060`) or behind admin auth on the main port. Never on the public listener.

### Cooker today

- `log/slog` JSON throughout (per `main.go` line 32: `slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))`). Right call.
- `Config.Observability` already has `MetricsEnabled`, `TracingEnabled`, `OTLPEndpoint`, `OTLPInsecure`, `ServiceName`, `ServiceVersion` — the OTel scaffolding is in place.
- `/health` endpoint exists (per Dockerfile HEALTHCHECK).
- pprof — search the codebase: I haven't verified if it's exposed. If exposed on the main port, that's a finding.

### Recommended for Cooker

1. **Split `/livez` and `/readyz`.** `/livez` = always 200 once the HTTP server is up. `/readyz` = 200 only if `Store.Ping()` succeeded in the last 30 seconds and (if Redis configured) `Redis.Ping()` succeeded. Update the Dockerfile HEALTHCHECK + Helm probes.
2. **Gate pprof.** If `cfg.PprofEnabled`, mount `net/http/pprof` on `:6060` as a separate listener that *only* binds to localhost by default. Document in `SECURITY.md`.
3. **Ship a Grafana dashboard JSON.** `deploy/dashboards/cooker-overview.json` — req/s by route, p50/p95/p99 latency, error rate, pipeline run count, build duration. ArgoCD ships [`examples/dashboard.json`](https://github.com/argoproj/argo-cd/tree/master/examples); same pattern.
4. **Add an OTel example to docs.** `docs/observability.md` with a working `docker-compose.observability.yml` that boots Jaeger + Prometheus + the Cooker stack. Five-minute "see traces" demo is what gets operators to enable tracing in prod.
5. **Stay on `slog`.** Resist any PR that adds zap or zerolog. The stdlib is now adequate; the only reason to switch is sampling, which Cooker doesn't need at current scale.

---

## 4. Configuration story

### Industry pattern

- **Three-layer config is the norm**: file (YAML/TOML) ← env vars ← CLI flags, in order of *increasing* precedence. Caddy reads `Caddyfile` or JSON; Gitea reads `custom/conf/app.ini`; Grafana reads `grafana.ini`; ArgoCD has a ConfigMap (`argocd-cmd-params-cm`) plus env vars.
- **Env-only configs work fine up to ~20 variables.** Beyond that, the operator UX degrades sharply — there's no introspection, no defaults documentation that the binary itself can print, no per-env override file. Every project in this list started env-only and eventually added a config file.
- **Reload semantics**: most projects reload on `SIGHUP` (Caddy famously does graceful reloads); some require a process restart (Gitea, Grafana for most settings). The cheap path is "restart-only" — operators handle it via K8s rollouts.
- **Secret integration**:
  - Vault: every project supports it via env-var indirection (`VAULT_ADDR`, `VAULT_TOKEN`) and a fetch-on-startup pattern.
  - AWS Secrets Manager / GCP Secret Manager: same pattern.
  - Cooker's "KeepSave" backend is uncommon (a Cooker-author-maintained product) — supporting it is a strategic call, not a community-driven one.
- **The "external-secrets operator" pattern** is gaining ground: K8s External Secrets Operator + a regular `Secret` resource means the application doesn't need to know about Vault at all. Worth recommending as the K8s-native deployment.

### Cooker today

- `backend/internal/config/config.go` is 200+ lines, parsing 60+ env vars (`COOKER_*`, `DATABASE_URL`, `REDIS_URL`, `VITE_OIDC_*` baked into the frontend). Nested structs (`OIDCConfig`, `KubernetesConfig`, `KeepSaveConfig`, `VaultConfig`, `AWSSecretsConfig`, `GCPSecretsConfig`, `DeployTargetsConfig`, `AuditConfig`, `ObservabilityConfig`) — this is past the "env-only is fine" threshold.
- No config file. No CLI flags beyond what `flag` defaults provide.
- Already supports four secrets backends (`database`, `keepsave`, `vault`, `aws`, `gcp` per Helm values).

### Recommended for Cooker

1. **Add YAML config as the *first* layer, keep env-vars on top.** Use `koanf` or `viper` (koanf is lighter, no Cobra-style assumptions). Schema:
   ```yaml
   env: production
   port: 8080
   database:
     url: postgres://...
   oidc:
     enabled: true
     issuerUrl: ...
   builder:
     kind: kaniko
   ```
   `COOKER_OIDC_ENABLED=true` should still override `oidc.enabled: false`. Document precedence: defaults < file < env.
2. **Add a `cooker config print` subcommand.** Resolves the full config and prints it as YAML, with secrets redacted. This is the killer operator-UX feature: "what is this server *actually* running with?" Caddy ships this as `caddy adapt`.
3. **Add a `cooker config validate <file>` subcommand.** Reuses `Config.Validate()`. Lets operators dry-run a chart change before applying.
4. **Stay with restart-only reload.** Don't add SIGHUP reload — it forces every adapter to be reload-safe, which is a much bigger refactor than it's worth.
5. **Mark KeepSave as opt-in in docs.** Be honest: it's a Cooker-team product, not a community standard. Vault and ESO are the de-facto K8s defaults.
6. **Document the External Secrets Operator path.** A `deploy/eso/` directory with example `ExternalSecret` manifests for AWS SM and Vault is a one-day project that closes the most common production-secret question.

---

## 5. Plugin / extension model

### Industry pattern

Three shapes exist; pick the simplest that ships:

- **Compile-time module registration (Caddy).** Plugin authors `import _ "github.com/foo/caddy-bar"` in a custom `main.go` and rebuild. The [`xcaddy`](https://github.com/caddyserver/xcaddy) tool automates this. Pros: no RPC, no serialization, full Go type system. Cons: every plugin combination is a separate binary.
- **Process-per-plugin RPC (Hashicorp `go-plugin`).** Used by Terraform, Vault, Nomad, Waypoint. Plugins are independent binaries that the host spawns and talks to via gRPC. Pros: crash isolation, security boundary, independent versioning. Cons: heavy — serialization overhead, debugging is harder, distribution is complicated (each plugin needs its own release).
- **WebAssembly (newest, Envoy, increasingly Caddy v3).** Sandboxed, language-agnostic, but the Go-host story is still rough (`wazero` is the leader). Don't pick this in 2026 unless you're a CDN.

### Cooker today

Cooker already has the right shape for compile-time modules but doesn't advertise it:

- `internal/builder/` has `Builder` interface; `selectBuilder` in `server.go` picks `noop`, `docker`, `buildkit`, `kaniko`, `buildah` from `COOKER_BUILDER`.
- Same shape for `internal/pusher/` (`noop`, `docker`, `crane`), `internal/deployer/` (`noop`, `kubectl`, `clientgo`), `internal/deploytarget/`.
- All adapters live in-tree.

### Recommended for Cooker

1. **Don't add a plugin system. Document the "fork and add a case" path.** `docs/extending.md`: "to add a new builder, implement `builder.Builder`, register it in `selectBuilder`, add the env-var to `docs/config.md`." This is exactly Caddy's [extending docs](https://caddyserver.com/docs/extending-caddy) at one-tenth the page count.
2. **If/when there are 10+ deploy targets**, consider an `xcaddy`-equivalent (`xcooker`) that lets operators import third-party adapters and produce a custom binary. Until then, in-tree adapters are correct.
3. **Never adopt `hashicorp/go-plugin`.** The RPC tax doesn't pay off for adapter-style integrations — every builder call would need request/response types crossing a process boundary, and Cooker's adapters are inherently in the request hot path.

---

## 6. Migration story

### Industry pattern

- **Numbered, idempotent up/down SQL migrations** in a `migrations/` directory are the universal pattern. Cooker already does this.
- **Embedded migrations via `embed.FS`** is the now-standard packaging — the binary ships its own migrations and runs them on startup behind a CLI flag (`gitea migrate`, `argocd-server` runs them, Grafana has [`pkg/services/sqlstore/migrations`](https://github.com/grafana/grafana/tree/main/pkg/services/sqlstore/migrations) with descriptive names).
- **Gitea** uses [`models/migrations/`](https://github.com/go-gitea/gitea/tree/main/models/migrations) with versioned subdirectories (`v1_10`, `v1_11`, ...) — heavier but lets them group migrations by release.
- **Grafana** uses descriptive names (`teams permissions migration`) tracked in a `migration_log` table.
- **`golang-migrate/migrate`** is the most popular library for the "files-on-disk, numbered, up/down" flavor.
- **Breaking changes**: published in an `UPGRADING.md` (Gitea) or per-version doc (Grafana's `docs/sources/upgrade-guide/upgrade-vX.Y/`). Always include a backup step.

### Cooker today

- `backend/internal/store/postgres/migrations/` has 8 numbered migrations (`001_initial.up.sql` ... `008_app_health.up.sql`) with paired `.down.sql`. Solid foundation.
- I didn't verify whether they're embedded via `embed.FS` or read from disk; if they're read from disk, that breaks the single-binary distribution promise.
- No `UPGRADING.md`; CHANGELOG.md exists but doesn't separate "breaking" from "feature."

### Recommended for Cooker

1. **Confirm migrations are embedded.** If they aren't, add `//go:embed migrations/*.sql` to the store package and pass the FS to `golang-migrate`. This is the prereq for ever shipping a real binary.
2. **Add `cooker migrate up|down|status` subcommands.** Make `migrate up` idempotent (the library handles this). `migrate status` prints current schema version. `make migrate-up` already wraps this — finish the surface area.
3. **Write `docs/UPGRADING.md`.** Per-version section. For each release, document: schema changes, default-value changes, config-key renames, removed features. Cribbed format from [Gitea's CHANGELOG](https://github.com/go-gitea/gitea/blob/main/CHANGELOG.md) which embeds upgrade notes per version.
4. **Refuse to start if `schema_version > binary_version`.** Old-binary-against-new-DB is the most common deploy mistake; the fail-fast guard is 10 lines.
5. **Snapshot tests for migrations.** A `migrations_test.go` that runs every up then every down against a fresh container Postgres and asserts the schema is empty. Gitea has [`migrationtest/`](https://github.com/go-gitea/gitea/tree/main/models/migrations/migrationtest) — copy the pattern.

---

## 7. Multi-tenancy & SaaS-readiness

### Industry pattern

This is the single most expensive transition in an OSS Go tool's lifecycle. The pattern is consistent:

1. **Phase 1 (single-tenant, what Cooker is)**: one database, one set of secrets, one OIDC provider; auth is "you're either in or out."
2. **Phase 2 (project / workspace boundary, where ArgoCD lives)**: a `Project` CRD scopes resources; RBAC roles are per-project; users belong to projects. Still one DB, still one process.
3. **Phase 3 (true multi-tenant SaaS, where Grafana Cloud and Tailscale's coordination server live)**: per-tenant database isolation (row-level via tenant_id, or schema-per-tenant); per-tenant OIDC; per-tenant rate limits; per-tenant audit trails; data residency.
4. **Phase 4 (hosted offering)**: the OSS is one tenant's deployment; the SaaS adds a tenant manager, billing, signup, abuse controls.

- ArgoCD's [Project CRD](https://argo-cd.readthedocs.io/en/stable/operator-manual/project.yaml) is the canonical Phase-2 reference.
- Grafana Cloud, Tailscale, and Sourcegraph Cloud are the canonical Phase-3/4 references; none ship the multi-tenant code in OSS.

### Cooker today

- Single-tenant. OIDC group → role mapping is global (`COOKER_OIDC_GROUP_MAP`). Environments are scoped to a global pipeline.
- No `tenant_id` column anywhere. RBAC roles (`admin`, `operator`, `approver`, `viewer`) are global.

### Recommended for Cooker

1. **Stay single-tenant in OSS.** Phase-2 (Projects) is the right next step *only* if there's user demand. Premature multi-tenancy is the #1 reason Go OSS projects fail to ship features.
2. **Decide now whether a hosted Cooker is the business model.** If yes, design Phase-2 with `tenant_id` in mind from the next migration onward (cheap to add now, expensive later). If no, document the OSS as deliberately single-tenant.
3. **Don't try to "make Cooker multi-tenant"** in one PR. The ArgoCD `Project` migration took years and is still incomplete in places.

---

## 8. Build & CI matrix

### Industry pattern

- **Race detector on by default** (`go test -race ./...`). Universal.
- **`go vet ./...` and `gofmt -l` as gates** — Cooker already does this.
- **`golangci-lint` with a `.golangci.yml` checked in** — every project does this; the lint set varies wildly. Cooker has [`golangci-lint-action@v6`](https://github.com/golangci/golangci-lint-action) at `v2.5.0` of golangci-lint with `--timeout=5m` and `continue-on-error: true`. The `continue-on-error` should go once the codebase is clean.
- **`govulncheck`** — [golang.org/x/vuln/cmd/govulncheck](https://github.com/golang/vuln) — runs against your binary's dependency tree and reports CVEs that are *actually reachable* from your code. This is leagues better than `dependabot` alerts that flag transitive deps you never call. Caddy, Hugo, ArgoCD all gate PRs on it.
- **Fuzz testing**: stdlib `go test -fuzz` covers parsers, validators, anything with an attacker-controlled string. Caddy fuzzes its [duration parser](https://github.com/caddyserver/caddy/blob/master/duration_fuzz.go).
- **Cross-compile in CI**: even for projects that don't release Windows builds, a build matrix `goos: [linux, darwin, windows] goarch: [amd64, arm64]` catches platform-specific bugs early. GoReleaser's `--snapshot` mode does this in one command.
- **SBOM**: [`cyclonedx-gomod`](https://github.com/CycloneDX/cyclonedx-gomod) generates a CycloneDX SBOM for the binary; the [`gh-gomod-generate-sbom`](https://github.com/CycloneDX/gh-gomod-generate-sbom) Action wires it in. Most projects emit it as a release artifact, not on every PR. Syft (used by Caddy via GoReleaser's `sboms:`) is the alternative — same output, different tool.

### Cooker today

- Race detector: yes (`go test -race`).
- `go vet` and `gofmt -l`: yes.
- `golangci-lint`: yes but with `continue-on-error: true`.
- `govulncheck`: **no**.
- Fuzz: **no**.
- Cross-compile in CI: **no** (only the Docker build).
- SBOM: **no**.
- OCI conformance: yes, weekly + workflow_dispatch — strong signal, unusual for a project at this size.

### Recommended for Cooker

1. **Add `govulncheck` to the backend CI job.** Five lines. Block the PR if it finds a *reachable* vulnerability; warn-only for unreachable.
2. **Flip `continue-on-error` on `golangci-lint` to `false`** after a one-PR cleanup sweep. The signal is wasted otherwise.
3. **Add a cross-compile matrix step.** `goos: [linux, darwin] goarch: [amd64, arm64]` via `go build`. Catches build-tag and `runtime.GOOS` bugs.
4. **Add fuzz targets for the obvious surfaces**:
   - `internal/config` env-var parser
   - `internal/auth` JWT / OIDC ID-token parsing
   - `internal/pusher` OCI reference parser
   - Any YAML/JSON consumed from user input (pipeline definitions)
   Run with `go test -fuzz=Fuzz -fuzztime=60s` in CI on a nightly cron, not per-PR — 60s is enough to catch new regressions without burning the PR queue.
5. **Generate SBOM in the release pipeline** (not on every PR). Add `sboms:` block to `.goreleaser.yaml`; it'll use syft. Output `cooker_<ver>.sbom.cyclonedx.json` next to the binary on the GitHub Release.
6. **Add an OpenSSF Scorecard workflow** (`scorecard.yml`). ArgoCD ships this. Gives you a public hygiene score and surfaces missing settings (branch protection, signed releases, etc.).

---

## 9. Documentation site

### Industry pattern

Four real options, in order of effort:

- **Hugo** (with the [Doks](https://getdoks.org/) or [Docsy](https://www.docsy.dev/) theme): the reference projects in this analysis use Hugo themselves (Gitea, Caddy, Grafana — recursively). Built by Go people for Go people. Fast.
- **MkDocs** (with [Material](https://squidfunk.github.io/mkdocs-material/)): Python-based, dead-simple, the search-bar UX is best-in-class. ArgoCD uses this ([`mkdocs.yml`](https://github.com/argoproj/argo-cd/blob/master/mkdocs.yml) + deploys to Read the Docs).
- **Docusaurus**: React-based, biggest plugin ecosystem, very heavy for what you get. Don't pick unless you need MDX heavily.
- **Mintlify**: hosted, paid (free tier exists), looks great out of the box. Vendor lock-in. Skip for OSS.

### Cooker today

- Markdown docs in `docs/` (architecture.md, design.md, UAT.md, ROLLOUT.md, RUNBOOK.md, MULTI_REPLICA.md, openapi.yaml, ADR folder, audit folder). Quality is high.
- No published documentation site — operators read it on GitHub.

### Recommended for Cooker

1. **MkDocs Material.** One `mkdocs.yml`, one `.github/workflows/docs.yml` that runs on `push: main` to gh-pages. Done in a half-day. Mirror ArgoCD's setup line-for-line.
2. **Auto-generate API reference from OpenAPI.** You already have `docs/openapi.yaml`; pipe it through [`redocly/cli build-docs`](https://github.com/Redocly/redocly-cli) in the docs workflow and embed the output as a page.
3. **Skip Hugo.** It is the better Go-native pick, but Material's search and ergonomics are decisively ahead, and the Python build is *fine* in a GH Actions step.
4. **One persistent URL convention.** `docs.cooker.dev/<version>/...` with a `latest` alias. `mike` (MkDocs versioning plugin) handles this. ArgoCD uses the same pattern.

---

## 10. Community signals

### Industry pattern

- **Issue templates**: `.github/ISSUE_TEMPLATE/bug.yml`, `feature.yml`, with required fields. Every project. Cuts low-quality reports by ~80%.
- **PR template**: `.github/PULL_REQUEST_TEMPLATE.md` with a checklist (tests, docs, changelog).
- **`CODEOWNERS`**: at least one entry per top-level directory. Auto-assigns reviewers.
- **`SECURITY.md`**: how to report a vulnerability privately. GitHub now treats this as a top-level signal. Cooker has this — good.
- **`security.txt`** at `/.well-known/security.txt` (RFC 9116): served by the docs site OR baked into the binary's HTTP handler. Caddy, Tailscale do this.
- **`SUPPORT.md`**: where to ask vs. where to file bugs (Discussions vs. Issues).
- **`GOVERNANCE.md`**: only matters when there are 5+ maintainers; until then, "BDFL is @santapong" in CONTRIBUTING is fine.
- **`FUNDING.yml`**: GitHub Sponsors, OpenCollective, Polar. Cheap to add.
- **Releases page polish**: every release tag should have:
  - A categorized changelog (Features / Fixes / Breaking / Security).
  - Upgrade notes (the `UPGRADING.md` per-version section, inlined or linked).
  - Checksums for every artifact.
  - Cosign verification command in the body.

ArgoCD's [release notes](https://github.com/argoproj/argo-cd/releases) are the high bar. Hugo's are the low-but-acceptable bar. Gitea's are scattered.

### Cooker today

- `SECURITY.md`: yes.
- `CODE_OF_CONDUCT.md`: didn't see one — confirm.
- `CONTRIBUTING.md`: didn't see one in the root — confirm.
- Issue/PR templates: didn't see any in `.github/` — confirm.
- `CODEOWNERS`: didn't see one — confirm.
- `FUNDING.yml`: didn't see one.

### Recommended for Cooker

1. **Add `.github/ISSUE_TEMPLATE/{bug.yml,feature.yml}`** with required fields: version (`cooker --version`), env (`COOKER_ENV`), repro steps, logs. Half-hour.
2. **Add `.github/CODEOWNERS`** with one or two entries for now: `* @santapong` and `deploy/helm/ @santapong @<your_devops>`. Easy to expand.
3. **Add `CONTRIBUTING.md`** even if it just says "open a PR against `claude/<topic>`, follow the conventions in CLAUDE.md." Lowering the friction for outsiders is the only way to attract them.
4. **Add a release-notes template** to `.goreleaser.yaml`'s `changelog:` block. Group by Conventional-Commit prefix (`feat:`, `fix:`, `security:`). Hugo's `release_notes_settings` block is a useful copy ([`hugoreleaser.yaml`](https://github.com/gohugoio/hugo/blob/master/hugoreleaser.yaml)).
5. **Serve `security.txt`.** Easiest path: drop it in `frontend/public/.well-known/security.txt` so Vite serves it as a static file. Point it at `mailto:security@<your-domain>` plus the `SECURITY.md` URL.
6. **Defer governance docs** until there are external committers.

---

## Prioritized adoption plan

### 0–30 days: "make releases real"

The premise of everything else in this doc. Do nothing else until this lands.

| # | Task | Effort | Owner |
|---|---|---|---|
| 1 | Reconcile `go.mod` module path with `github.com/santapong/cooker` | 1h | maintainer |
| 2 | Add `internal/version` package + `-ldflags` build wiring + `/version` endpoint + `cooker version` subcommand | 2h | maintainer |
| 3 | Add minimal `.goreleaser.yaml` (linux+darwin, amd64+arm64, tar.gz, checksum, GHCR multi-arch image) | 4h | maintainer |
| 4 | Add `.github/workflows/release.yml` triggered on `v*` tags | 2h | maintainer |
| 5 | Publish Helm chart to `oci://ghcr.io/santapong/charts/cooker` in same workflow | 1h | maintainer |
| 6 | Cut `v0.1.0` tag end-to-end, verify all artifacts exist | 1h | maintainer |
| 7 | Confirm Postgres migrations are embedded via `//go:embed` (if not, fix) | 1h | maintainer |
| 8 | Add `.github/ISSUE_TEMPLATE/{bug.yml,feature.yml}` and `CONTRIBUTING.md` | 1h | maintainer |

### 30–90 days: "harden the supply chain and the operator UX"

| # | Task | Effort |
|---|---|---|
| 9 | Add `cosign` signing + `syft` SBOM to `.goreleaser.yaml`; document verification in `docs/RELEASING.md` | 4h |
| 10 | Add `govulncheck` step to `.github/workflows/ci.yml` (PR-blocking on reachable CVEs) | 2h |
| 11 | Flip `continue-on-error: false` on `golangci-lint`; do the cleanup PR | 1d |
| 12 | Split `/livez` and `/readyz`; update Helm probes and Dockerfile HEALTHCHECK | 4h |
| 13 | Add `cooker config print`, `cooker config validate`, `cooker migrate {up,down,status}` subcommands | 1d |
| 14 | Introduce YAML config (`koanf`) as layer 1; keep env-vars as layer 2; document precedence | 2d |
| 15 | Build MkDocs Material site, publish to `docs.cooker.dev` (or gh-pages) | 1d |
| 16 | Generate API reference from `docs/openapi.yaml` via redocly in the docs workflow | 4h |
| 17 | Ship `deploy/dashboards/cooker-overview.json` (Grafana) | 1d |
| 18 | Write `docs/UPGRADING.md` with per-version sections, starting at v0.1.0 | 4h |
| 19 | Add SLSA provenance via `slsa-framework/slsa-github-generator` | 4h |
| 20 | Add `OpenSSF Scorecard` workflow | 1h |

### 90–180 days: "earn the hosted-or-not decision"

| # | Task | Effort |
|---|---|---|
| 21 | Decide and document: is hosted Cooker the business model? If yes, design `tenant_id` migration *now*. | n/a |
| 22 | Add fuzz targets for config parser, OCI reference parser, OIDC ID-token parser; nightly cron run | 2d |
| 23 | Add cross-compile matrix to CI (linux/darwin × amd64/arm64) | 2h |
| 24 | Snapshot tests for migrations: up-all → down-all → schema-is-empty | 1d |
| 25 | Publish `docs/extending.md` with the "fork-and-recompile" plugin path | 4h |
| 26 | Deprecate KeepSave-only docs in favor of "Vault + ESO is the K8s-native default; KeepSave is an option" | 4h |
| 27 | Add `deploy/eso/` example `ExternalSecret` manifests for AWS SM and Vault | 1d |
| 28 | Publish `security.txt` and link it from the docs site | 1h |
| 29 | Add `FUNDING.yml` if anyone has opinions on the funding question | 30m |
| 30 | Revisit `release-please` once there are 3+ active committers | n/a |

---

## What this doc deliberately does *not* recommend

- **`hashicorp/go-plugin`.** RPC-process plugins are wrong for an adapter pattern where the host calls the plugin on every request.
- **WASM plugins.** Not yet. Revisit in 2027 if `wazero`'s host-bindings story matures.
- **`semantic-release`.** Node toolchain, prescriptive, doesn't match Go ergonomics.
- **Hand-rolled Makefile cross-compile** (Gitea's path). Goreleaser does it better in one-tenth the lines.
- **Dagger.** Grafana uses it; you're not Grafana. The simple-tooling option (Goreleaser + GitHub Actions) is the right pick until you have multi-team build complexity.
- **Multi-tenancy in OSS.** Single-tenant is correct until proven otherwise.
- **Windows builds.** Not until someone asks. Cooker shells `kubectl` and `docker`; Windows is a stretch.
- **A custom plugin manifest format.** Cooker's interface-based `selectBuilder`/`selectPusher`/`selectDeployer` is already the Caddy pattern, just undocumented.

---

## Appendix A — reference config files, indexed by topic

### GoReleaser configs
- Caddy: [`.goreleaser.yml`](https://github.com/caddyserver/caddy/blob/master/.goreleaser.yml) — multi-OS, cosign, syft, deb (nfpms), source archive.
- Hugo: [`hugoreleaser.yaml`](https://github.com/gohugoio/hugo/blob/master/hugoreleaser.yaml) — multi-edition, CGO matrix. Custom tool, GoReleaser-compatible shape.
- Most other Go OSS: GoReleaser at `.goreleaser.yaml` at repo root.

### Release workflows
- ArgoCD: [`release.yaml`](https://github.com/argoproj/argo-cd/blob/master/.github/workflows/release.yaml) — three-job split: image, SLSA provenance, goreleaser binaries.
- Woodpecker: [`.woodpecker/binaries.yaml`](https://github.com/woodpecker-ci/woodpecker/blob/main/.woodpecker/binaries.yaml) — cross-compile via xgo, make-driven, deb/rpm bundle, checksums.
- Grafana: [`release-build.yml`](https://github.com/grafana/grafana/blob/main/.github/workflows/release-build.yml) — Dagger-driven, ten-job pipeline. Reference for "what not to do at our size."
- Gitea: [`Makefile`](https://github.com/go-gitea/gitea/blob/main/Makefile) `release:` target — hand-rolled. Reference for "the long way."

### Plugin systems
- Caddy modules: [extending docs](https://caddyserver.com/docs/extending-caddy) + [`xcaddy`](https://github.com/caddyserver/xcaddy).
- Hashicorp RPC plugins: [`hashicorp/go-plugin`](https://github.com/hashicorp/go-plugin).
- Vault plugin architecture deep-dive: [DeepWiki](https://deepwiki.com/hashicorp/vault/5-plugin-architecture).

### Migrations
- Gitea: [`models/migrations/`](https://github.com/go-gitea/gitea/tree/main/models/migrations).
- Grafana: [`pkg/services/sqlstore/migrations/`](https://github.com/grafana/grafana/tree/main/pkg/services/sqlstore/migrations).
- `golang-migrate/migrate`: [repo](https://github.com/golang-migrate/migrate).

### Supply chain
- `govulncheck`: [docs](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck).
- `cyclonedx-gomod`: [repo](https://github.com/CycloneDX/cyclonedx-gomod) + [GH Action](https://github.com/CycloneDX/gh-gomod-generate-sbom).
- `syft`: [repo](https://github.com/anchore/syft) — used by GoReleaser's `sboms:`.
- `cosign`: [sigstore/cosign](https://github.com/sigstore/cosign).
- SLSA: [`slsa-framework/slsa-github-generator`](https://github.com/slsa-framework/slsa-github-generator).
- OpenSSF Scorecard workflow: [example](https://github.com/argoproj/argo-cd/blob/master/.github/workflows/scorecard.yaml).

### Docs sites
- ArgoCD's MkDocs site: [`mkdocs.yml`](https://github.com/argoproj/argo-cd/blob/master/mkdocs.yml).
- `mike` versioning plugin for MkDocs: [repo](https://github.com/jimporter/mike).
- Material theme: [docs](https://squidfunk.github.io/mkdocs-material/).

### Distribution channels
- Tailscale's apt/yum repo: [pkgs.tailscale.com](https://pkgs.tailscale.com).
- Caddy on Cloudsmith: [docs](https://caddyserver.com/docs/install#debian-ubuntu-raspbian).
- Helm OCI registry pattern: [Helm docs](https://helm.sh/docs/topics/registries/).

### Observability
- OpenTelemetry Go SDK: [`go.opentelemetry.io/otel`](https://github.com/open-telemetry/opentelemetry-go).
- ArgoCD metrics doc: [operator-manual/metrics](https://argo-cd.readthedocs.io/en/stable/operator-manual/metrics/).

### Community / governance
- ArgoCD issue templates: [`.github/ISSUE_TEMPLATE/`](https://github.com/argoproj/argo-cd/tree/master/.github/ISSUE_TEMPLATE).
- ArgoCD Scorecard run: [scorecard.yaml](https://github.com/argoproj/argo-cd/blob/master/.github/workflows/scorecard.yaml).
- RFC 9116 `security.txt`: [securitytxt.org](https://securitytxt.org/).

---

## Appendix B — Cooker-specific files referenced

All paths absolute from the repo root:

- `/home/user/Cooker/Makefile` — current build/test/release targets.
- `/home/user/Cooker/.github/workflows/ci.yml` — backend + frontend + helm + docker jobs.
- `/home/user/Cooker/.github/workflows/oci-conformance.yml` — weekly OCI conformance.
- `/home/user/Cooker/.github/workflows/cooker-weekly.yml` — Claude-driven weekly bug hunt.
- `/home/user/Cooker/backend/cmd/cooker/main.go` — entry point; no `--version`, no flags.
- `/home/user/Cooker/backend/internal/config/config.go` — 60+ env-var config (env-only today).
- `/home/user/Cooker/backend/internal/store/postgres/migrations/` — 8 numbered up/down SQL migrations.
- `/home/user/Cooker/deploy/docker/Dockerfile` — multi-stage, non-root, kubectl + docker-cli + git.
- `/home/user/Cooker/deploy/helm/cooker/` — chart with cookerEnv, OIDC, ingress, retention, kaniko/buildah, keepsave.
- `/home/user/Cooker/CHANGELOG.md` — hand-written; needs Conventional-Commit-ready replacement.
- `/home/user/Cooker/SECURITY.md` — present; meets baseline.
- `/home/user/Cooker/docs/architecture.md`, `docs/design.md`, `docs/UAT.md`, `docs/ROLLOUT.md`, `docs/RUNBOOK.md` — strong; ready to publish as a MkDocs site.

---

*End of document.*
