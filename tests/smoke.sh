#!/usr/bin/env bash
set -Eeuo pipefail
shopt -s inherit_errexit
# Checks a running controller through its API: its admin and its network must be there already, so
# that a controller that lost its data fails.
# usage: smoke.sh [<base url>]
# SMOKE_SETUP=true first creates them, on a new controller.
# SMOKE_EXPECT_NETWORK names the network that must exist, instead of the one the setup creates.
# SMOKE_EXPECT_WLAN names a Wi-Fi network that must exist.

URL="${1:-https://127.0.0.1:8443}"
ADMIN_NAME=smoke-admin
ADMIN_PASSWORD=smoke-Pass-12345
NETWORK="${SMOKE_EXPECT_NETWORK:-smoke-vlan}"
NETWORKS=/api/s/default/rest/networkconf
COOKIES=''

function main() {
  COOKIES="$(mktemp)"
  trap 'rm -f -- "$COOKIES"' EXIT

  if ! has_text /status '"up":true'; then
    printf 'error: controller not up at %s\n' "$URL" >&2
    return 1
  fi
  if [[ "${SMOKE_SETUP:-}" == 'true' ]]; then
    step 'create first admin' POST /api/cmd/sitemgr \
      "{\"cmd\":\"add-default-admin\",\"name\":\"${ADMIN_NAME}\",\"email\":\"a@example.invalid\",\"x_password\":\"${ADMIN_PASSWORD}\"}"
    step 'finish setup' POST /api/cmd/system '{"cmd":"set-installed"}'
  fi
  step login POST /api/login "{\"username\":\"${ADMIN_NAME}\",\"password\":\"${ADMIN_PASSWORD}\"}"
  if [[ -n "${SMOKE_EXPECT_WLAN:-}" ]]; then expect_text /api/s/default/rest/wlanconf "$SMOKE_EXPECT_WLAN"; fi
  if [[ "${SMOKE_SETUP:-}" == 'true' ]]; then
    step 'write network' POST "$NETWORKS" \
      "{\"name\":\"${NETWORK}\",\"purpose\":\"vlan-only\",\"vlan_enabled\":true,\"vlan\":99}"
  fi
  expect_text "$NETWORKS" "$NETWORK"
  write_and_delete
  step 'read ap groups' GET /v2/api/site/default/apgroups
  step 'read devices' GET /api/s/default/stat/device
  step 'make backup' POST /api/s/default/cmd/backup '{"cmd":"backup","days":0}'
  printf 'all checks passed\n'
}

### Checks

# Whatever the database has been through (loaded from a dump, migrated), it must still take changes
function write_and_delete() {
  local _created _id
  _created="$(api_ok POST "$NETWORKS" \
    '{"name":"smoke-write","purpose":"vlan-only","vlan_enabled":true,"vlan":98}')"
  _id="$(sed -nE 's/.*"_id":"([^"]+)".*/\1/p' <<<"$_created")"
  if [[ -z "$_id" ]]; then
    printf 'FAIL  the written network has no id\n' >&2
    return 1
  fi
  step 'write and delete a network' DELETE "${NETWORKS}/${_id}"
}

# usage: expect_text <path> <name>
function expect_text() {
  if ! has_text "$1" "\"${2}\""; then
    printf 'FAIL  %s is not in %s\n' "$2" "$1" >&2
    return 1
  fi
  printf 'ok    found %s\n' "$2"
}

### Helpers

# Print the response body; fail on an HTTP error
# usage: api_ok <method> <path> [<json>]
function api_ok() {
  local _args=(-ksSf -m 120 -b "$COOKIES" -c "$COOKIES" -X "$1" "${URL}${2}")
  if [[ "$#" -ge 3 ]]; then _args+=(-H 'Content-Type: application/json' -d "$3"); fi
  curl "${_args[@]}"
}

# Not with `| grep -q`: grep leaving early makes curl fail, which pipefail reports as no match
# usage: has_text <path> <text>
function has_text() {
  local _body
  _body="$(api_ok GET "$1")" || return 1
  [[ "$_body" == *"$2"* ]]
}

# usage: step <label> <method> <path> [<json>]
function step() {
  local _label="$1"
  shift
  if api_ok "$@" >/dev/null; then
    printf 'ok    %s\n' "$_label"
  else
    printf 'FAIL  %s\n' "$_label" >&2
    return 1
  fi
}

eval 'main "$@";exit "$?"'
