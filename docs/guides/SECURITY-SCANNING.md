# Cooker security scanning

Cooker's security checks cover source code, secrets, exact dependency versions and the built container. Findings and scanner failures are different outcomes: both need attention, and an interrupted check provides no clean result.

## Automatic checks

| Check | Trigger | Behavior |
| --- | --- | --- |
| CodeQL Go and JavaScript/TypeScript | PRs to develop/main, pushes to those branches and security fix branches; weekly schedule | Runs security-extended queries and uploads code-scanning alerts. Go uses a manual build. |
| OSV dependency lookup | Same PR/push targets; daily schedule | Checks Go requirements and npm lockfile versions, including development packages. Fails for advisory matches requiring review, unsupported inventory, API failure or incomplete pagination. |
| Gitleaks | Same security workflow | Checks the current branch history and working tree. Reports are redacted. Only exact historical synthetic-fixture fingerprints are ignored. |
| govulncheck | Backend CI | Pinned analyzer checks reachable Go vulnerabilities using the selected build toolchain. Findings fail the step. |
| npm audit | Frontend CI | Includes development dependencies; high/critical findings fail the step. |
| Trivy image scan | After CI builds and loads `cooker:ci` | Checks the actual built image's OS/library inventory. High/critical matches fail the step; the JSON report remains available. |
| Trivy configuration report | Security workflow | Reports Dockerfile, Kubernetes, Helm and Terraform policy findings for review against the intended deployment. Successful report generation does not mean all policies passed. |

Actions are pinned to commit SHAs. The Gitleaks and Trivy installers pin both version and archive checksum; update those values together after checking the publisher. Dependency/advisory data is fetched on each scan. Existing Renovate configuration is retained; its presence does not establish whether the GitHub App is installed or active.

The daily dependency/secret job is configured for 03:17 Asia/Bangkok. Weekly CodeQL is configured for Thursday 03:31. GitHub schedules use UTC, run from the default branch and may be delayed. They require the scheduler workflow to be present on `main`.

A future CI-only scheduler bootstrap on `main` can call the reviewed reusable workflows at a pinned security-fix commit. No bootstrap is installed by this integration change. Scheduled calls check out the current `develop` source. Merge the implementation into `develop` before enabling that bootstrap; otherwise missing scripts/configuration correctly fail the run. Refresh the reusable-workflow commit pin when changing the scan implementation. This arrangement leaves application release promotion separate from security scheduling.

## Local checks

Use the same Go release as `backend/go.mod` and Node 24 or newer compatible with the package engines. Run from the repository root:

```sh
python3 -m unittest discover -s scripts/security -p 'test_*.py'
mkdir -p security-results
(cd backend && go list -mod=readonly -p=1 -deps -test -f '{{.ImportPath}}' ./...) > security-results/go-imports.txt
python3 scripts/security/dependencies.py --go-imports security-results/go-imports.txt
bash scripts/security/secrets.sh
bash scripts/security/trivy.sh config
```

The shell installers target Linux x86-64. They download checksum-verified tools into a temporary directory and remove those executables afterward. They do not install host packages or scan another project by default. Resource-bound the actual processes and any database/browser/build service before substantial tests; CI timeouts alone are not CPU/RAM limits on a developer workstation.

For image checks after building the candidate:

```sh
bash scripts/security/trivy.sh image cooker:ci
```

Run the normal backend/frontend regressions after dependency changes. The source-level checks above do not inventory an already deployed image, prove a cloud configuration was applied, or replace authenticated application security tests.

## Interpreting Go advisories

The OSV inventory includes exact requirements in `go.mod`; the compiler is checked separately through govulncheck. A `go` directive is not a measurement of the compiler embedded in an old deployed binary.

The optional fresh Go import inventory permits one narrow applicability decision: when an advisory explicitly lists affected Go subpackages and every one is absent from the default build/test imports, it remains visible under `not_imported` in the report rather than failing the gate. Missing import metadata, unknown applicability and failed lookups remain blocking. Importing an affected package reopens the finding automatically; this boundary is tested.

For example, GO-2026-5932 concerns the unmaintained `golang.org/x/crypto/openpgp` subpackages. Cooker needs other parts of `x/crypto`, but those OpenPGP imports are absent from the checked default graph. This is not an exception for all `x/crypto` vulnerabilities. Optional build tags, other architectures and future dependency paths require their own inventory/check.

Do not reuse a saved import list after source or dependency changes. CI regenerates it in the same checkout as the advisory scan. For releases, match checks to the exact source, compiler and image digest. Record upstream aliases as aliases rather than claiming every GHSA/GO/CVE ID is a separate vulnerability.

## Development exposure and deployment policies

The default development Compose file publishes Cooker only at `127.0.0.1:8080`. Its development authentication behavior is unsuitable for exposure to an untrusted network. Local Compose overrides can change published addresses; review the rendered configuration before starting it. Docker-published traffic can bypass UFW rules.

The runtime Dockerfile uses a supported Alpine baseline, current Go/Node build toolchains and an explicit kubectl release. `KUBECTL_VERSION` remains a build argument: match it to the target cluster's supported version skew rather than assuming one client supports every cluster. Updating a Dockerfile does not rebuild an existing deployment.

The AWS data security group no longer initiates unrestricted outbound traffic; security-group response traffic remains stateful. Public subnet definitions no longer assign public addresses to instances automatically. These are template changes; applying Terraform still needs an environment-specific plan and the normal deployment authorization.

Infrastructure reports also include examples and functionally required permissions. Review namespaced deployment RBAC, PostgreSQL writable mounts/init permissions, registry-refresh examples and third-party module policies against actual use. Do not remove required permissions blindly or hide the findings to obtain green CI. Configuration findings remain review items until their applicable deployment is verified.

## Repository activation and review

Open the security-fix PR against `develop`. The standard CI now includes PRs to that integration branch. Any later scheduler bootstrap must target `main` and change CI configuration only; it is outside this PR. After merging, verify at least one successful Security checks and CodeQL run for the intended commit/ref, and review the built-image report.

Workflow files do not establish branch protection or alert ownership. Use GitHub's rules/settings to select the checks required for merging and configure notifications for a responsible maintainer. If CodeQL default setup is already enabled, reconcile it with this advanced workflow to avoid conflicting configurations. PR operations follow the owner's current authorization; repository settings changes need their own scoped authorization.

For the next project, inventory its stack and deployment first, adapt the dependency inventory paths/ecosystems, retain any existing tests and policies, and repeat the positive/failure-boundary checks before enabling its automation. These scripts are currently scoped to Cooker's Go backend and npm frontend.

## Primary references

- [GitHub CodeQL build modes](https://docs.github.com/en/code-security/how-tos/find-and-fix-code-vulnerabilities/manage-your-configuration/codeql-for-compiled-languages)
- [OSV batch API](https://google.github.io/osv.dev/post-v1-querybatch/)
- [Gitleaks](https://github.com/gitleaks/gitleaks)
- [Trivy](https://trivy.dev/docs/latest/references/terminology/)
- [Go release support policy](https://go.dev/doc/devel/release)
- [Alpine release support](https://alpinelinux.org/releases/)
- [AWS security-group behavior](https://docs.aws.amazon.com/vpc/latest/userguide/vpc-security-groups.html)
- [Docker and host firewalls](https://docs.docker.com/engine/network/packet-filtering-firewalls/)
