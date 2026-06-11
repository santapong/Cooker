## 2026-05 — GitHub Actions SHA-pinning plan

> Read-only research deliverable. No workflow edits in this PR. Implementation
> happens in a follow-up owned by `cooker-infra-ci`.

> **Implemented (`claude/jolly-knuth-j396dx`).** All 17 `uses:` refs in the three
> non-release workflows (`ci.yml`, `cooker-weekly.yml`, `oci-conformance.yml`)
> are now pinned to 40-char commit SHAs with a trailing `# vN.M.P` comment.
> SHAs were **re-resolved live** via `git ls-remote` at implementation time —
> the candidate column below was captured without live access and several
> entries were stale (e.g. `actions/checkout` resolved to v4.3.1
> `34e1148…`, not the documented v4.1.7 `b4ffde6…`), so the candidates were
> NOT used. `anthropics/claude-code-action@v1` (the highest-blast-radius ref —
> `contents:write` + `pull-requests:write`) was pinned first to the **peeled
> commit** of its annotated `v1` tag (`eee73e2…`, not the tag object
> `2ab86a3…`); `azure/setup-helm` and `golangci/golangci-lint-action` are also
> annotated and were pinned to their peeled commits. `SECURITY.md`'s
> `S26-05-15` row removal and `renovate.json` follow-up remain with
> `cooker-security` per the done-criteria below; this doc stays the historical
> migration record.

### Finding cross-reference

- **`S26-05-15`** (MEDIUM) — `docs/audits/2026-05-security-review.md:172-176` already
  enumerates this gap: every `uses:` in `.github/workflows/` references a floating
  major-version tag, including `anthropics/claude-code-action@v1` which holds
  `contents: write` + `pull-requests: write` in `cooker-weekly.yml`. **This audit
  is the migration plan that closes `S26-05-15`**; no new finding ID is needed.
- **`shipping-go.md`** does not call SHA-pinning out as a numbered item directly.
  It belongs to the same "30–90 days: harden the supply chain" theme as item #19
  (SLSA provenance) and item #20 (OpenSSF Scorecard) — both score higher with
  pinned actions. The user-requested "item #11" in the task brief appears to be
  a brief-side mislabel; item 11 in `shipping-go.md:386` is the `golangci-lint`
  `continue-on-error` flip. This audit is filed under the same supply-chain
  hardening bucket regardless.

### Inventory

17 `uses:` references across 3 workflow files. 10 unique actions. **0 are
currently pinned to a SHA; 17 need pinning.**

| Workflow file | Line | Current ref | Candidate SHA (verify at pin time) | Pin replacement |
|---|---|---|---|---|
| `.github/workflows/ci.yml` | 32 | `actions/checkout@v4` | `b4ffde6` (v4.1.7) | `actions/checkout@b4ffde65f46336ab88eb53be808477a3936bae11 # v4.1.7` |
| `.github/workflows/ci.yml` | 33 | `actions/setup-go@v5` | `0a12ed9` (v5.0.2) | `actions/setup-go@0a12ed9d6a96ab950c8f026ed9f722fe0da7ef32 # v5.0.2` |
| `.github/workflows/ci.yml` | 46 | `actions/cache@v4` | `0c45773` (v4.0.2) | `actions/cache@0c45773b623bea8c8e75f6c82b208c3cf94ea4f9 # v4.0.2` |
| `.github/workflows/ci.yml` | 68 | `golangci/golangci-lint-action@v6` | `aaa42aa` (v6.1.0) | `golangci/golangci-lint-action@aaa42aa0628b4ae2578232a66b541047968fac86 # v6.1.0` |
| `.github/workflows/ci.yml` | 90 | `actions/checkout@v4` | `b4ffde6` (v4.1.7) | `actions/checkout@b4ffde65f46336ab88eb53be808477a3936bae11 # v4.1.7` |
| `.github/workflows/ci.yml` | 91 | `actions/setup-node@v4` | `1e60f62` (v4.0.3) | `actions/setup-node@1e60f620b9541d16bece96c5465dc8ee9832be0b # v4.0.3` |
| `.github/workflows/ci.yml` | 112 | `actions/checkout@v4` | `b4ffde6` (v4.1.7) | `actions/checkout@b4ffde65f46336ab88eb53be808477a3936bae11 # v4.1.7` |
| `.github/workflows/ci.yml` | 113 | `azure/setup-helm@v4` | `fe7b79c` (v4.2.0) | `azure/setup-helm@fe7b79cd5ee1e45176fcad797de68ecaf3ca4814 # v4.2.0` |
| `.github/workflows/ci.yml` | 317 | `actions/checkout@v4` | `b4ffde6` (v4.1.7) | `actions/checkout@b4ffde65f46336ab88eb53be808477a3936bae11 # v4.1.7` |
| `.github/workflows/ci.yml` | 321 | `docker/setup-buildx-action@v3` | `988b5a0` (v3.6.1) | `docker/setup-buildx-action@988b5a0280414f521da01fcc63a27aeeb4b104db # v3.6.1` |
| `.github/workflows/ci.yml` | 323 | `docker/build-push-action@v6` | `5cd11c3` (v6.7.0) | `docker/build-push-action@5cd11c3a4ced054e52742c5fd54dca954e0edd85 # v6.7.0` |
| `.github/workflows/oci-conformance.yml` | 38 | `actions/checkout@v4` | `b4ffde6` (v4.1.7) | `actions/checkout@b4ffde65f46336ab88eb53be808477a3936bae11 # v4.1.7` |
| `.github/workflows/oci-conformance.yml` | 40 | `actions/setup-go@v5` | `0a12ed9` (v5.0.2) | `actions/setup-go@0a12ed9d6a96ab950c8f026ed9f722fe0da7ef32 # v5.0.2` |
| `.github/workflows/oci-conformance.yml` | 80 | `actions/upload-artifact@v4` | `5024830` (v4.4.0) | `actions/upload-artifact@50248302ce81d1c3c9a8f93cdfd45a8a0e1f4f60 # v4.4.0` |
| `.github/workflows/cooker-weekly.yml` | 26 | `actions/checkout@v4` | `b4ffde6` (v4.1.7) | `actions/checkout@b4ffde65f46336ab88eb53be808477a3936bae11 # v4.1.7` |
| `.github/workflows/cooker-weekly.yml` | 34 | `actions/setup-go@v5` | `0a12ed9` (v5.0.2) | `actions/setup-go@0a12ed9d6a96ab950c8f026ed9f722fe0da7ef32 # v5.0.2` |
| `.github/workflows/cooker-weekly.yml` | 38 | `actions/setup-node@v4` | `1e60f62` (v4.0.3) | `actions/setup-node@1e60f620b9541d16bece96c5465dc8ee9832be0b # v4.0.3` |
| `.github/workflows/cooker-weekly.yml` | 57 | `anthropics/claude-code-action@v1` | **verify at pin time** | `anthropics/claude-code-action@<sha> # v1.x` |

> **SHA-verification caveat.** The candidate SHAs above are the public latest
> stable as of audit time but were captured without live GitHub API access
> (rate-limited 403 during this research pass). The implementation PR **MUST**
> re-fetch each SHA from `https://github.com/<owner>/<repo>/releases/tag/<vN.M.P>`
> immediately before flipping the workflow, and confirm the tag points to the
> documented commit. Trailing `# vN.M.P` comments are required (Renovate keys
> off them).

### Note on `anthropics/claude-code-action@v1`

Highest blast radius in the inventory. `cooker-weekly.yml:21-23` grants
`contents: write` + `pull-requests: write`, and the job carries
`ANTHROPIC_API_KEY`. A compromise of the upstream `v1` tag could open a PR that
passes CI and ships a backdoor. Pin this one first, separately, even before
the rest. The action is also published less predictably than the
`actions/*` line — verify the SHA against a Git-signed tag if one exists,
otherwise against the release notes.

### Migration plan

Three-PR series; each PR is independently revertable. All three should land
within a single week so the workflow set converges on one policy.

1. **PR 1 — `ci.yml`.** Highest-traffic workflow; gates every `main` merge. Pin
   all 11 `uses:` lines (checkout × 4, setup-go, cache, golangci-lint-action,
   setup-node, setup-helm, setup-buildx-action, build-push-action). Run the
   workflow once on the PR branch to confirm green before merge.
2. **PR 2 — `oci-conformance.yml`.** 3 `uses:` lines. Lower-traffic (manual +
   weekly schedule), but pin so the weekly cron isn't a soft-spot. Trigger
   `workflow_dispatch` manually post-merge to verify.
3. **PR 3 — `cooker-weekly.yml`.** 4 `uses:` lines. **Pin
   `anthropics/claude-code-action@v1` first within this PR**, since it carries
   the largest privilege set. Verify with a manual `workflow_dispatch` run
   while `COOKER_WEEKLY_ENABLED` is unset (so the step skips), then once with
   it set (in a dry-run / `--prompt` no-op variant if available).

Future workflows (release scaffolding from `shipping-go.md` 0–30d items #3–#6,
the `govulncheck` step from #10, the SLSA generator from #19, the OpenSSF
Scorecard from #20) **must ship pin-by-SHA from day one** — add a CI lint or a
CODEOWNERS-enforced review step if a regression is observed.

### Renovate scope confirmation

`renovate.json:30-34` already groups `github-actions` updates into one weekly
PR. Renovate's `github-actions` manager pins to SHAs **automatically** when
the existing reference is a SHA (it preserves the existing pinning style) and
keeps the trailing `# vN.M.P` comment in sync. **Action required from the
maintainer**: confirm Renovate is enabled on the GitHub side (the `S26-05-14`
remediation note flags Renovate as not-yet-running). Once enabled, the three
PRs above are one-time work; weekly bumps become automated.

Optional follow-up: add `helperOptions.pinDigests: true` under
`packageRules[matchManagers: github-actions]` to make the intent explicit and
to auto-pin any future workflow that lands with a tag-only reference.

### Done criteria

- All `uses:` lines in `.github/workflows/*.yml` reference a 40-char commit SHA
  followed by a `# vN.M.P` comment.
- `renovate.json` updated (if needed) to keep the SHA pinning sticky.
- `SECURITY.md` updated to remove the `S26-05-15` row from the open-findings
  list (audit owner: `cooker-security`; this audit doc remains as the
  historical migration record).
- A `grep -E 'uses: [^@]+@v[0-9]' .github/workflows/` invocation returns
  empty — add to CI as a one-line guard if desired (5-line bash step).
