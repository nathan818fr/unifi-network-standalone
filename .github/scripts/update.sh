#!/usr/bin/env bash
set -Eeuo pipefail
shopt -s inherit_errexit
# Updates what no update tool can in image/Containerfile: UniFi Network, from Ubiquiti's feed, and
# PostgreSQL's packages, within the same major version. Prints `title=<pull request title>` when
# something changed, nothing otherwise. Fails if a source changed shape: that failure is the alert.
# Runs at the repository's root.

CONTAINERFILE=image/Containerfile
FEED='https://fw-update.ubnt.com/api/firmware-latest?filter=eq~~product~~unifi&filter=eq~~channel~~release'
PGDG_PACKAGES=https://apt-archive.postgresql.org/pub/repos/apt/dists/trixie-pgdg-archive/main/binary-amd64/Packages.bz2

function main() {
  case "${1:-}" in
  network) cmd_network ;;
  postgres) cmd_postgres ;;
  *)
    printf 'usage: update.sh network|postgres\n' >&2
    return 2
    ;;
  esac
}

### Commands

function cmd_network() {
  local feed amd64 arm64 version url sha newest current
  feed="$(curl -fsSL --retry 5 --retry-all-errors "$FEED")"
  amd64="$(feed_entry "$feed" uos-deb13-amd64)"
  arm64="$(feed_entry "$feed" uos-deb13-arm64)"
  IFS=$'\t' read -r version url sha <<<"$amd64"
  if [[ "${arm64##*$'\t'}" != "$sha" ]]; then
    printf 'error: the feed has different packages for amd64 and arm64\n' >&2
    return 1
  fi
  if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ || ! "$sha" =~ ^[0-9a-f]{64}$ ||
    ! "$url" =~ ^https://fw-download\.ubnt\.com/data/unifi/[A-Za-z0-9._-]+\.deb$ ]]; then
    printf 'error: unexpected feed entry: %s\n' "$amd64" >&2
    return 1
  fi
  # Old platforms stay listed, so a move to another one would otherwise look like no update.
  newest="$(jq -r '._embedded.firmware[] | select(.platform | startswith("uos-deb"))
    | .version | ltrimstr("v") | split("-")[0]' <<<"$feed" | sort -V | tail -n 1)"
  if is_newer "$newest" "$version"; then
    printf 'error: Network %s is on another platform than uos-deb13\n' "$newest" >&2
    return 1
  fi
  current="$(current_arg UNIFI_VERSION)"
  if ! is_newer "$version" "$current"; then return 0; fi
  set_arg UNIFI_VERSION "$version"
  set_arg UNIFI_DEB_URL "$url"
  set_arg UNIFI_DEB_SHA256 "$sha"
  printf 'title=Update Network to %s\n' "$version"
}

# The pull request's tests, on both architectures, check that the packages install together.
function cmd_postgres() {
  local major current_version current_common packages version common
  major="$(current_arg POSTGRES_MAJOR)"
  current_version="$(current_arg POSTGRES_VERSION)"
  current_common="$(current_arg POSTGRES_COMMON_VERSION)"
  packages="$(curl -fsSL --retry 5 --retry-all-errors "$PGDG_PACKAGES" | bunzip2 |
    awk '/^Package: /{ package = $2 } /^Version: /{ print package, $2 }')"
  version="$(newest "$packages" "postgresql-${major}")"
  common="$(newest "$packages" postgresql-common)"
  if [[ -z "$version" || -z "$common" ]]; then
    printf 'error: PostgreSQL is missing from the archive\n' >&2
    return 1
  fi
  if ! is_newer "$version" "$current_version" && ! is_newer "$common" "$current_common"; then
    return 0
  fi
  set_arg POSTGRES_VERSION "$version"
  set_arg POSTGRES_COMMON_VERSION "$common"
  printf 'title=Update PostgreSQL to %s\n' "${version%%-*}"
}

### Helpers

# Prints the version, URL and SHA-256 of the one entry of a platform, separated by tabs.
# usage: feed_entry <feed> <platform>
function feed_entry() {
  jq -er --arg platform "$2" '
    [._embedded.firmware[] | select(.platform == $platform)]
    | if length == 1 then .[0] else error("expected one entry for \($platform)") end
    | [(.version | ltrimstr("v") | split("-")[0]), ._links.data.href, .sha256_checksum]
    | @tsv' <<<"$1"
}

# Prints the newest stable version of a package.
# usage: newest <packages> <package>
function newest() {
  awk -v package="$2" '$1 == package && $2 !~ /~/ { print $2 }' <<<"$1" | sort -V | tail -n 1
}

# usage: is_newer <version> <than>
function is_newer() {
  [[ "$1" != "$2" && "$(printf '%s\n%s\n' "$1" "$2" | sort -V | tail -n 1)" == "$1" ]]
}

# usage: current_arg <name>
function current_arg() {
  local _value
  _value="$(sed -n "s/^ARG ${1}=//p" "$CONTAINERFILE")"
  if [[ -z "$_value" ]]; then
    printf 'error: no %s in the Containerfile\n' "$1" >&2
    return 1
  fi
  printf '%s\n' "$_value"
}

# usage: set_arg <name> <value>
function set_arg() {
  sed -i "s|^ARG ${1}=.*|ARG ${1}=${2}|" "$CONTAINERFILE"
}

eval 'main "$@";exit "$?"'
