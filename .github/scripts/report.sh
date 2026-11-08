#!/usr/bin/env bash
set -Eeuo pipefail
shopt -s inherit_errexit
# Prints what a reviewer needs and the diff doesn't show, in Markdown: the packages this image changes
# against the published one, and what Network newly asks of unifi-core.
# usage: report.sh <image> <published image> <tests' output>

function main() {
  if [[ "$#" -ne 3 ]]; then
    printf 'usage: report.sh <image> <published image> <tests output>\n' >&2
    return 2
  fi
  local packages published calls
  packages="$(list_packages "$1")"
  printf '### Package changes\n\n'
  if published="$(list_packages "$2" 2>/dev/null)"; then
    print_block "$(diff <(printf '%s\n' "$published") <(printf '%s\n' "$packages") | grep '^[<>]' || true)"
  else
    printf 'No published image to compare with.\n\n'
  fi
  calls="$(grep -E 'core stub: unexpected|unexpected gRPC call' "$3" | sort -u || true)"
  printf '### Unexpected calls to unifi-core\n\n'
  print_block "$calls"
}

### Helpers

# usage: list_packages <image>
function list_packages() {
  podman run --rm --entrypoint dpkg-query "$1" -W
}

# usage: print_block <text>
function print_block() {
  if [[ -z "$1" ]]; then
    printf 'None.\n\n'
    return
  fi
  printf '%s\n%s\n%s\n\n' '```text' "$1" '```'
}

eval 'main "$@";exit "$?"'
