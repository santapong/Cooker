# Security-side release verification (v0.1.0 and onward)

Companion to [`docs/RELEASING.md`](RELEASING.md) (shipped by the W3 #1 release-workflow spawn). `RELEASING.md` describes *how* a release is cut. This doc describes *how the human operator confirms the published artefacts are the ones we asked GoReleaser to build*, and that nothing was inserted along the way.

Scope: invoked the moment a `v0.1.0` tag is pushed and the `release.yml` workflow starts. Re-run for every subsequent tag.

Findings closed by this checklist:

- `[S26-05-15]` GitHub Actions SHA-pinning — verification step in §3 enforces that every `uses:` in the running `release.yml` is a 40-char SHA, per [`docs/audits/2026-05-action-pinning.md`](../audits/2026-05-action-pinning.md).
- Reinforces (does not close) `[S26-05-04]` — the published image is verified to run as UID 65532 and to bind only port 8080.

---

## 1. Pre-tag checklist (before `git tag v0.1.0`)

Run each item, top to bottom. If any fails, fix before tagging.

1. **All `claude/*` PRs for the release week are merged.** `gh pr list --search "is:open base:main author:app/claude" --json number,title,headRefName` should return `[]`. If anything is mid-rebase, wait.
2. **Local snapshot build is sane.** `make release-snapshot` (target shipped by W3 #1). Confirm the printed paths under `dist/` contain a Linux+arm64 *and* Linux+amd64 tarball, an `SBOM.json`, and that the embedded `cooker --version` on the snapshot binary prints `v0.1.0-SNAPSHOT-<sha>` (not bare `v0.1.0-dev`).
3. **CI on the tip of `main` is green.** `gh run list --branch main --limit 1 --json conclusion,headSha` → `conclusion: success`. If GitHub is rate-limiting the API token, fall back to the web UI; do not skip.
4. **No untracked secret material in the worktree.** `git status` shows no `.env`, no `*.key`, no `*.pem`. Goreleaser pulls in `dist/` by glob and a stray file there will end up shipped.
5. **`SECURITY.md` and `CHANGELOG.md` reference `v0.1.0`.** If either still says `Unreleased`, push a docs PR first; do not promote at tag time.

## 2. Tag-and-push sequence

```
git checkout main
git pull --ff-only
git tag -a v0.1.0 -m "v0.1.0"
git push origin v0.1.0
```

No `--force`. No re-tagging. If you mis-tagged: §5.

## 3. Workflow-run checks (while `release.yml` is running)

Open the run in the GitHub Actions UI. Tail the log; do not just watch the green check.

1. **Job permissions are exactly what's needed.** Top of the workflow YAML must declare:
   ```yaml
   permissions:
     contents: write       # create the GitHub Release
     id-token: write       # cosign keyless OIDC
     packages: write       # GHCR push (image + chart)
   ```
   No `pull-requests:`, no `actions:`, no `security-events:`. If the running workflow has more, abort (§5) and patch the YAML.
2. **Every `uses:` in `release.yml` is a 40-char SHA.** Per [`docs/audits/2026-05-action-pinning.md`](../audits/2026-05-action-pinning.md), floating tags are forbidden in any privileged workflow. Quick visual check from the run page: every line is `owner/repo@<40-hex> # vN.M.P`. `release.yml` carries `contents: write` + `packages: write`, so it sits in the same threat class as `cooker-weekly.yml` — pin it equally hard.
3. **`cosign sign-blob` runs for each binary in `dist/`.** Search the log for `Signature pushed to:` lines — expect one per OS/arch tarball, plus checksums.txt. Empty list means signing was skipped silently; do not promote.
4. **`cosign sign` runs against the docker manifest, not just one arch.** The log should contain `Signing <ref>@sha256:<digest>` where the digest is the manifest list (multi-arch index), and the `cosign verify` smoke (next step in workflow) covers the manifest.
5. **`helm push oci://ghcr.io/santapong/charts/cooker` succeeds.** Look for `Pushed: ghcr.io/santapong/charts/cooker:0.1.0` and the SHA-256 digest line. If the chart push 404s, the GHCR package didn't exist yet and the workflow's `packages: write` is fine — the second run after the package is created will succeed. Don't switch permissions to `admin`.
6. **No `set-output` / `::set-env` legacy uses.** These are blocked by GitHub by default; if the log shows them, an action is unpinned-and-old. Cross-check against §3.2.

## 4. Post-publish verification

Once the workflow is green and the GitHub Release is visible (still in *draft* — see §6):

1. **Docker image signature (manifest).**
   ```
   cosign verify \
     --certificate-identity-regexp="https://github.com/santapong/Cooker/.github/workflows/release.yml@.*" \
     --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
     ghcr.io/santapong/cooker:v0.1.0
   ```
   The output must include `"subject": [...release.yml@refs/tags/v0.1.0"]`. Any other workflow ref (e.g. `@refs/heads/main`) means the image was signed from a *non-tag* run — that is an injection path; reject.
2. **Binary blob signatures (per platform).** For each `cooker_v0.1.0_<os>_<arch>.tar.gz` on the Release page:
   ```
   cosign verify-blob \
     --certificate cooker_..._cert.pem \
     --signature  cooker_..._sig \
     --certificate-identity-regexp="https://github.com/santapong/Cooker/.github/workflows/release.yml@.*" \
     --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
     cooker_v0.1.0_linux_amd64.tar.gz
   ```
   Repeat for `linux_arm64`, `darwin_amd64`, `darwin_arm64`.
3. **Helm chart signature.**
   ```
   cosign verify \
     --certificate-identity-regexp="https://github.com/santapong/Cooker/.github/workflows/release.yml@.*" \
     --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
     ghcr.io/santapong/charts/cooker:0.1.0
   ```
   Note the chart tag is `0.1.0` (no leading `v`) per OCI/Helm convention; the image tag is `v0.1.0`.
4. **Embedded version matches the tag.**
   ```
   docker run --rm --entrypoint cooker ghcr.io/santapong/cooker:v0.1.0 --version
   ```
   Must print `v0.1.0`. **Any `-dev` or `-SNAPSHOT` suffix means goreleaser's `-X main.version=...` ldflag was wrong** — usually a sign the workflow ran from `main` instead of the tag. Reject (§5).
5. **Image runs as UID 65532 (reinforces `[S26-05-04]` posture).**
   ```
   docker run --rm --entrypoint id ghcr.io/santapong/cooker:v0.1.0
   ```
   Expect `uid=65532 gid=65532 groups=65532`. Any `uid=0` is a regression and the release must be redone.
6. **Only port 8080 is listening.**
   ```
   docker run --rm --entrypoint sh ghcr.io/santapong/cooker:v0.1.0 -c \
     'cooker & sleep 2; ss -ltn 2>/dev/null || netstat -ltn'
   ```
   Expect a single `LISTEN` row on `:8080`. The `pprof` and admin debug endpoints share that port behind auth; nothing else should be bound.

## 5. Post-publish remediation (if any of §3 or §4 fails)

Order matters. Do not skip steps.

1. **Mark the GitHub Release as draft** in the UI (`Releases` → `Edit` → uncheck `Set as the latest release` → `Save as draft`). This pulls it out of `gh release view --latest` and stops mirrors from picking it up.
2. **Delete the GHCR image tag.** `gh api -X DELETE /user/packages/container/cooker/versions/<id>` (find the id via `gh api /user/packages/container/cooker/versions`). Same for the Helm chart: `/user/packages/container/charts%2Fcooker/versions/<id>`. The cosign signature blob in `cosign.sigstore.dev` is immutable; that's fine — we're only invalidating the tag → digest mapping.
3. **Delete the git tag locally and remotely.** `git tag -d v0.1.0 && git push origin :refs/tags/v0.1.0`.
4. **Fix the root cause** (action pin, ldflag, USER directive, whatever §4 surfaced) on `main` via a normal PR. Do NOT push a hotfix directly to a tag.
5. **Re-tag once `main` is fixed.** Go back to §2.

## 6. Open questions for the user (one-time setup)

These are operator decisions, not engineering ones. They block "release is policy-grade trusted" but not "release is cryptographically signed."

1. **Is there a cosign keyless trust policy in the repo's consuming clusters?** Without a policy controller (Kyverno / Sigstore Policy Controller / Connaisseur) actively verifying the `--certificate-identity-regexp` + `--certificate-oidc-issuer` pair, the signature is *verifiable* (we did it above) but not *enforced*. Decide: do we want clusters to refuse unsigned images on pull? If yes, ship a `ClusterImagePolicy` in `deploy/helm/cooker/templates/` in a follow-up.
2. **GHCR package retention.** Snapshot builds tag as `v0.1.0-SNAPSHOT-<sha>`; without retention, these accumulate forever and the GHCR billing/quota will surprise us. Decide: keep last N=20 snapshot tags, retain all `vN.M.P` semver tags. Configure via `gh api /user/packages/container/cooker -F retention=...`.
3. **Default `release.draft: true` in `.goreleaser.yaml`?** Yes — the operator should promote draft → published only after §4 passes. This is the single behaviour that gives §5 a chance to run without users seeing the broken artefact. Confirm W3 #1 ships with `draft: true`.

---

## Cross-references

- [`docs/RELEASING.md`](RELEASING.md) — operational release procedure (shipped by W3 #1).
- [`docs/audits/2026-05-security-review.md`](../audits/2026-05-security-review.md) §`S26-05-15` — SHA-pinning gap this checklist enforces.
- [`docs/audits/2026-05-action-pinning.md`](../audits/2026-05-action-pinning.md) — migration plan + canonical SHA inventory.
- [`SECURITY.md`](../../SECURITY.md) — threat model; supply-chain section should reference this doc.
