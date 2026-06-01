# Releasing Cooker

This document is the authoritative guide for cutting a Cooker release.
The process is: tag → push → observe → verify.

---

## Prerequisites (one-time setup)

| Tool | Version | Purpose |
|---|---|---|
| `git` | any | tag and push |
| `goreleaser` | v2+ | local snapshot testing (`make release-snapshot`) |
| `cosign` | v2+ | signature verification |
| `helm` | 3.8+ | chart install from OCI |
| `gh` CLI | any | watch the workflow run |

You do NOT need `goreleaser` installed locally to publish — the GitHub Actions workflow handles it. You only need it to run `make release-snapshot` for pre-flight checks.

---

## Repository secrets and permissions required

The release workflow (`.github/workflows/release.yml`) uses only the built-in `GITHUB_TOKEN`. No additional secrets need to be created in the repository.

The `GITHUB_TOKEN` must have the following permissions on the repository (set in **Settings → Actions → General → Workflow permissions**):

- **Read and write permissions** — required so Actions can create the GitHub Release, upload assets, and push to GHCR.
- **Allow GitHub Actions to create and approve pull requests** — not required for releases but harmless.

To verify:

1. Go to `https://github.com/santapong/Cooker/settings/actions`.
2. Under **Workflow permissions**, confirm "Read and write permissions" is selected.
3. Save if needed.

GHCR (GitHub Container Registry) uses the same `GITHUB_TOKEN` credential automatically — no separate `CR_PAT` is needed.

---

## Step 1 — Pre-flight checks

Run the snapshot build locally to confirm the GoReleaser config is valid and the cross-compilation succeeds. This does not push anything.

```sh
# Requires goreleaser v2+ on PATH.
make release-snapshot
```

A successful run produces `dist/` containing:

- `cooker_<version>-next_linux_amd64.tar.gz` (and arm64, darwin, windows variants)
- `checksums.txt`

Check that the binary works:

```sh
./dist/cooker_<version>-next_linux_amd64/cooker --version
# version: v0.1.0-next
# commit:  <sha>
# date:    <timestamp>
```

Also run:

```sh
helm lint deploy/helm/cooker/
```

Both should be clean before proceeding.

---

## Step 2 — Create and push the tag

```sh
# Confirm you are on the correct commit (usually main after merging
# all the PRs for this release).
git log --oneline -5

# Create an annotated tag.  The message appears in the GitHub Release.
git tag -a v0.1.0 -m "Cooker v0.1.0 — initial public release"

# Push the tag.  This fires the release workflow immediately.
git push origin v0.1.0
```

Do NOT push a lightweight tag (`git tag v0.1.0` without `-a`). GoReleaser requires an annotated tag to extract the release description.

---

## Step 3 — Observe the release workflow

```sh
# Watch the workflow in real-time (requires gh CLI).
gh run watch --repo santapong/Cooker

# Or open the Actions tab in the browser:
# https://github.com/santapong/Cooker/actions/workflows/release.yml
```

The workflow runs these steps in order:

1. Checkout (full history for changelog).
2. Set up Go (version from `backend/go.mod`).
3. Install cosign.
4. Set up QEMU + BuildX (for arm64 image cross-compilation).
5. Log in to GHCR.
6. **GoReleaser** — builds binaries, creates archives, pushes Docker images, stitches the manifest list, signs everything.
7. **Helm package + push** — packages `deploy/helm/cooker/` and pushes to `oci://ghcr.io/santapong/charts`.

Expected duration: 8–15 minutes (dominated by the arm64 Docker build under QEMU emulation).

---

## Step 4 — Verify the release artifacts

> See also [`SECURITY-RELEASE-VERIFY.md`](SECURITY-RELEASE-VERIFY.md) for the security-side post-publish checklist (Rekor inspection, identity drift checks, expected workflow subjects). The commands below are the operator how-to; the checklist over there is what the security curator runs after every publish.

### Binary signature

Verify the checksum file signature using cosign keyless:

```sh
VERSION=v0.1.0

# Download from the GitHub Release page
gh release download "${VERSION}" --repo santapong/Cooker \
  --pattern 'checksums.txt' \
  --pattern 'checksums.txt.sig' \
  --pattern 'checksums.txt.pem'

# Verify against the Sigstore transparency log.
# The --certificate-identity must match the workflow's OIDC subject.
# The --certificate-oidc-issuer must be the GitHub Actions OIDC issuer.
cosign verify-blob checksums.txt \
  --signature checksums.txt.sig \
  --certificate checksums.txt.pem \
  --certificate-identity "https://github.com/santapong/Cooker/.github/workflows/release.yml@refs/tags/${VERSION}" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com"

# Expected output: Verified OK
```

### Docker image signature

```sh
IMAGE="ghcr.io/santapong/cooker:${VERSION}"

cosign verify "${IMAGE}" \
  --certificate-identity "https://github.com/santapong/Cooker/.github/workflows/release.yml@refs/tags/${VERSION}" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com"

# Expected: one or more verification entries printed (JSON).
```

### Helm chart

```sh
helm pull oci://ghcr.io/santapong/charts/cooker \
  --version "${VERSION#v}"

# Should download cooker-<version>.tgz.
helm show chart "cooker-${VERSION#v}.tgz"
```

---

## Step 5 — Publish the GitHub Draft Release

GoReleaser creates the release in **draft** mode (see `release.draft: true` in `.goreleaser.yaml`). A human must review and publish it:

1. Go to `https://github.com/santapong/Cooker/releases`.
2. Find the draft release tagged `v0.1.0`.
3. Review the changelog, asset list, and checksums.
4. Click **Publish release**.

---

## Troubleshooting

### GoReleaser fails with "tag was not found"

The tag was pushed but the workflow checkout didn't see it. This can happen if the tag push races the workflow trigger. Re-run the failed workflow from the Actions tab; it will re-clone and see the tag.

### Docker push fails with "unauthorized"

Confirm that **Workflow permissions** in the repo settings is set to "Read and write" (see Prerequisites). A "Read repository contents" setting prevents writes to GHCR.

### cosign verify-blob fails with "certificate not found"

The signature files may not have been downloaded. Re-run `gh release download` to confirm all three files (`checksums.txt`, `checksums.txt.sig`, `checksums.txt.pem`) are present and non-empty.

### Helm push fails with "chart already exists"

GHCR OCI registries are immutable for a given version. If you need to re-publish the same version (e.g. due to a build error), delete the existing package version from `https://github.com/users/santapong/packages/container/charts%2Fcooker` first.

---

## Cutting a patch release

The process is identical — just use a patch tag:

```sh
git tag -a v0.1.1 -m "Cooker v0.1.1 — patch"
git push origin v0.1.1
```

GoReleaser's `snapshot.version_template` and `prerelease: auto` handle the version arithmetic automatically.
