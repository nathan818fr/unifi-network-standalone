#!/usr/bin/env bash
set -Eeuo pipefail
shopt -s inherit_errexit
# Writes the stable app, ha-app, from the beta one, ha-app-beta: the same app under its stable name.
# Runs at the repository's root.

rm -rf -- ha-app
cp -r -- ha-app-beta ha-app
sed -i \
  -e 's/^name: .*/name: UniFi Network (standalone)/' \
  -e 's/^description: .*/description: Latest UniFi Network application, without UniFi OS. Unofficial and unsupported./' \
  -e 's/^slug: .*/slug: unifi_network_standalone/' \
  -e '/^stage: /d' \
  ha-app/config.yaml
# A beta line left behind would offer the beta under the stable name.
if grep -qE '^(stage: |slug: .*_beta$|name: .*beta)' ha-app/config.yaml; then
  printf 'error: ha-app/config.yaml still has a beta line\n' >&2
  exit 1
fi
