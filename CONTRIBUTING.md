# Contributing to Cooker

Thanks for your interest in Cooker. Bug reports, feature requests, docs improvements, and pull
requests are all welcome. This guide covers how to get set up, the conventions we enforce, and the
contribution terms.

## Quick start

```bash
git clone https://github.com/santapong/Cooker.git
cd Cooker
make uat-up                  # spin up the dev stack
git checkout -b feature/my-feature
# ... make changes ...
make test                    # build + vet + race tests (backend) and lint/build/test (frontend)
git commit -s -m "feat: my feature"   # -s adds the DCO sign-off (required, see below)
git push -u origin feature/my-feature
# then open a PR against main
```

## Before a larger change

1. **Read [`docs/reference/design.md`](docs/reference/design.md)** — the layering rules, error-wrapping
   conventions, test patterns, and the "adding a feature" checklist (§11).
2. **Open an issue first** to discuss the approach before writing significant code.
3. **Follow the layering:** handler → service → store. No business logic in handlers, no HTTP types in
   services, no `panic` outside startup.
4. **Add tests** — the race detector is on in CI; every non-trivial change ships a `*_test.go`.

## What CI checks

PRs to `main` (and `claude/**`) run: backend `go build` → `go vet` → `go test -race` (against Postgres);
frontend `npm ci` → `lint` → `build` → `test`; and a `docker build`. Run `make test` locally first.

## Good first issues

Look for the [`good first issue`](https://github.com/santapong/Cooker/labels/good%20first%20issue) label.
If something is unclear, ask in the issue — improving the docs *because* something was unclear is itself a
valued contribution.

## Reporting security issues

Please **do not** open a public issue for security problems. Follow the responsible-disclosure process in
[`SECURITY.md`](SECURITY.md).

## Contribution terms (inbound = outbound) + DCO

Cooker is licensed under the **Apache License 2.0**. By submitting a contribution, you agree that your
contribution is licensed under the same Apache-2.0 terms (inbound license = outbound license).

We use the **Developer Certificate of Origin (DCO)** — a lightweight, sign-off-based affirmation that you
wrote the patch or otherwise have the right to submit it under the project license. Add a sign-off to every
commit:

```bash
git commit -s -m "your message"
```

This appends a `Signed-off-by: Your Name <your@email>` line. Read the full DCO at
<https://developercertificate.org/>. If a commit is missing the sign-off, you can amend it with
`git commit --amend -s` (or `git rebase --signoff` for a series).

> Maintainers: the optional DCO check is documented (as a draft) in
> `docs/marketing/research/launch-kit/cla-setup.md`. Enable it before the first external PR merges.

## Commit messages

Conventional-commit style is preferred (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`). Keep one
logical change per PR; we squash-merge.
