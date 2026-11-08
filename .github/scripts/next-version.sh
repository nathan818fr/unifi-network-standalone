#!/usr/bin/env bash
set -Eeuo pipefail
shopt -s inherit_errexit
# Prints `version=<version to release>`, from the Network version in the image and the git tags:
# nothing if what users get hasn't changed since its last release. See CONTRIBUTING.md.
# Runs at the repository's root, with its tags fetched.

function main() {
  local network newest last
  network="$(sed -n 's/^ARG UNIFI_VERSION=//p' image/Containerfile)"
  if [[ -z "$network" ]]; then
    printf 'error: no UNIFI_VERSION in the Containerfile\n' >&2
    return 1
  fi
  newest="$(newest_tag 'v*-build*')"
  if [[ -n "$newest" ]] && ! git merge-base --is-ancestor "$newest" HEAD; then
    printf 'error: this commit is older than the release %s\n' "$newest" >&2
    return 1
  fi
  last="$(newest_tag "v${network}-build*")"
  if [[ -z "$last" ]]; then
    printf 'version=%s-build1\n' "$network"
    return 0
  fi
  # The release workflow itself writes the beta app's version, after the tagged commit. Promoting
  # only writes ha-app, which a release doesn't follow.
  if git diff --quiet -I '^version: ' "$last" HEAD -- image ha-app-beta; then return 0; fi
  printf 'version=%s-build%s\n' "$network" "$((${last##*-build} + 1))"
}

### Helpers

# usage: newest_tag <pattern>
function newest_tag() {
  git tag --list --sort=-version:refname "$1" | sed -n 1p
}

eval 'main "$@";exit "$?"'
