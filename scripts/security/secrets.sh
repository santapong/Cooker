#!/usr/bin/env bash
set -euo pipefail

# Explicit version and publisher archive checksum. Keep version and checksum changes together.
version=8.30.1
checksum=551f6fc83ea457d62a0d98237cbad105af8d557003051f41f3e7ca7b3f2470eb
directory=$(mktemp -d)
trap 'rm -rf "$directory"' EXIT
curl --fail --silent --show-error --location --retry 2 --max-time 60 \
  "https://github.com/gitleaks/gitleaks/releases/download/v${version}/gitleaks_${version}_linux_x64.tar.gz" \
  --output "$directory/gitleaks.tar.gz"
printf '%s  %s\n' "$checksum" "$directory/gitleaks.tar.gz" | sha256sum --check --status
tar -xzf "$directory/gitleaks.tar.gz" -C "$directory" gitleaks
mkdir -p security-results
# The ignore file contains exact reviewed synthetic-fixture and prose fingerprints only.
status=0
"$directory/gitleaks" git . --log-opts='HEAD -- . :(exclude).claude' \
  --config .gitleaks.toml --gitleaks-ignore-path .gitleaksignore --ignore-gitleaks-allow --redact=100 --no-banner --no-color --timeout 180 \
  --report-format json --report-path security-results/secrets-history.json || status=$?
"$directory/gitleaks" dir . --config .gitleaks.toml --gitleaks-ignore-path .gitleaksignore --ignore-gitleaks-allow --redact=100 --no-banner --no-color --timeout 120 \
  --report-format json --report-path security-results/secrets-working-tree.json || status=$?
exit "$status"
