#!/usr/bin/env bash
set -euo pipefail

mode=${1:?Use config or image}
directory=$(mktemp -d)
trap 'rm -rf "$directory"' EXIT
version=0.76.1
checksum=c7cbe3137fb8a48a17574bede3dd6beb16742d1e9772929940fc90082c39d9c0
curl --fail --silent --show-error --location --retry 2 --max-time 90 \
  "https://github.com/aquasecurity/trivy/releases/download/v${version}/trivy_${version}_Linux-64bit.tar.gz" \
  --output "$directory/trivy.tar.gz"
printf '%s  %s\n' "$checksum" "$directory/trivy.tar.gz" | sha256sum --check --status
tar -xzf "$directory/trivy.tar.gz" -C "$directory" trivy
mkdir -p security-results
common=(--config '' --ignorefile '' --disable-telemetry --skip-version-check --parallel 2
        --cache-dir "${RUNNER_TEMP:-$directory}/cooker-trivy-cache" --no-progress --format json)
case "$mode" in
  config)
    # Policy findings are retained for deployment-specific review, not silently waived.
    "$directory/trivy" fs "${common[@]}" --scanners misconfig --timeout 3m \
      --skip-dirs .claude --tf-exclude-downloaded-modules \
      --output security-results/configuration.json .
    ;;
  image)
    "$directory/trivy" image "${common[@]}" --scanners vuln --timeout 10m \
      --severity HIGH,CRITICAL --exit-code 1 \
      --output security-results/image.json "${2:?Specify the exact built image}"
    ;;
  *) echo 'Unknown scan mode' >&2; exit 2 ;;
esac
