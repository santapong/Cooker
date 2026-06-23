<!-- DRAFT — contributor-agreement setup. Not activated. Decide DCO vs CLA, then enable. -->

# Contributor agreement setup (DCO vs CLA)

Gate **G4** (launch-readiness tracker): a contributor-origin check must be live **before the first external
PR merges**. Cooker is **Apache-2.0**, so the inbound=outbound default already covers licensing; the only
question is how contributors *affirm* origin.

## Recommendation: DCO (lightweight)

The **Developer Certificate of Origin** is a one-line `Signed-off-by` per commit (`git commit -s`). No
account linking, no clickwrap, no stored CLA records. It's what the Linux kernel, GitLab, and most modern
Apache-adjacent OSS projects use. `CONTRIBUTING.md` already documents it. This is the recommended path for a
solo-maintained project — lowest friction, sufficient provenance.

To enforce it, activate the draft workflow `cla-workflow.draft.yml` (move it to
`.github/workflows/dco.yml`). It fails a PR check if any commit lacks a valid `Signed-off-by`.

## Alternative: a clickwrap CLA (cla-assistant)

If you later want an explicit, stored agreement (e.g. an enterprise contributor asks, or you anticipate a
relicense), use **[cla-assistant](https://github.com/cla-assistant/cla-assistant)** (hosted) or the
[`contributor-assistant/github-action`](https://github.com/contributor-assistant/github-action) (self-hosted
in Actions, stores signatures in a repo file). It posts a one-time "sign the CLA" comment on a contributor's
first PR and blocks merge until they click. Heavier, but creates a durable record.

> Don't run both. DCO **or** CLA. Start with DCO; upgrade to a CLA only if a concrete need appears.

## Activation checklist

- [ ] Confirm `CONTRIBUTING.md` describes the chosen mechanism (currently: DCO).
- [ ] Move `cla-workflow.draft.yml` → `.github/workflows/dco.yml` (DCO) **or** wire `contributor-assistant`.
- [ ] Add a branch-protection rule on `main` requiring the new check to pass.
- [ ] Do this **before** merging the first external PR (the d90 contributor target accelerates the window).
