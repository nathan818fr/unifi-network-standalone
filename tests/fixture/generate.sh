#!/usr/bin/env bash
set -Eeuo pipefail
shopt -s inherit_errexit
# Makes sample-10.6.109.unf: a full backup of a clean 10.6.109 controller, with public test data.
# Runs as root inside this project's image, for its Java and MongoDB:
#   podman run --rm --network=host --entrypoint /fixture/generate.sh \
#     -v "$PWD/tests/fixture:/fixture" <image>

WORK=/tmp/fixture
OUT=/fixture/sample-10.6.109.unf
UNIFI_ZIP=https://dl.ui.com/unifi/10.6.109/UniFi.unix.zip
URL=https://127.0.0.1:8443
COOKIES="${WORK}/cookies"

function main() {
  mkdir -p -- "$WORK"
  apt-get update -qq
  apt-get install -y -qq --no-install-recommends unzip >/dev/null
  download "$UNIFI_ZIP" "${WORK}/unifi.zip"
  unzip -q "${WORK}/unifi.zip" -d "$WORK"

  # The old app starts mongod itself, from here
  mkdir -p -- "${WORK}/UniFi/bin"
  ln -s -- /opt/mongodb/bin/mongod "${WORK}/UniFi/bin/mongod"

  cd -- "${WORK}/UniFi"
  java -Xmx1024M \
    --add-opens=java.base/java.lang=ALL-UNNAMED \
    --add-opens=java.base/java.time=ALL-UNNAMED \
    --add-opens=java.base/sun.security.util=ALL-UNNAMED \
    --add-opens=java.base/java.io=ALL-UNNAMED \
    --add-opens=java.rmi/sun.rmi.transport=ALL-UNNAMED \
    -jar lib/ace.jar start &
  local _app="$!"
  wait_up

  api /api/cmd/sitemgr '{"cmd":"add-default-admin","name":"smoke-admin",
    "email":"a@example.invalid","x_password":"smoke-Pass-12345"}' >/dev/null
  api /api/cmd/system '{"cmd":"set-installed"}' >/dev/null
  api /api/login '{"username":"smoke-admin","password":"smoke-Pass-12345"}' >/dev/null
  api /api/s/default/rest/networkconf '{"name":"fixture-vlan","purpose":"vlan-only",
    "vlan_enabled":true,"vlan":42}' >/dev/null

  local _group _ap_group _path
  _group="$(curl -ksSf -b "$COOKIES" "${URL}/api/s/default/rest/usergroup" |
    sed -E 's/.*"_id":"([^"]+)".*/\1/')"
  _ap_group="$(curl -ksSf -b "$COOKIES" "${URL}/v2/api/site/default/apgroups" |
    sed -E 's/.*"_id":"([^"]+)".*/\1/')"
  api /api/s/default/rest/wlanconf "{\"name\":\"fixture-wlan\",\"security\":\"wpapsk\",
    \"x_passphrase\":\"fixture-pass-12345\",\"wpa_mode\":\"wpa2\",\"wpa_enc\":\"ccmp\",
    \"usergroup_id\":\"${_group}\",\"ap_group_ids\":[\"${_ap_group}\"],
    \"ap_group_mode\":\"all\"}" >/dev/null

  _path="$(api /api/s/default/cmd/backup '{"cmd":"backup","days":0}' |
    sed -E 's/.*"url":"([^"]+)".*/\1/')"
  curl -ksSf -b "$COOKIES" -o "$OUT" "${URL}${_path}"
  kill -- "$_app"
  wait "$_app" || true
  printf 'wrote %s\n' "$OUT"
}

### Helpers

# usage: download <url> <file>
function download() {
  curl -fsSL --retry 5 --retry-all-errors -o "$2" "$1"
}

# usage: api <path> <json>
function api() {
  local _out
  if ! _out="$(curl -ksS --fail-with-body -m 120 -b "$COOKIES" -c "$COOKIES" \
    -H 'Content-Type: application/json' -d "$2" "${URL}${1}")"; then
    printf 'error: %s: %s\n' "$1" "$_out" >&2
    return 1
  fi
  printf '%s\n' "$_out"
}

function wait_up() {
  local _tries=0
  # While starting, /status answers 200 with an HTML page
  until curl -ksf -m 3 "${URL}/status" | grep -q '"up":true'; do
    if [[ "$_tries" -ge 100 ]]; then
      printf 'error: the old controller did not start\n' >&2
      return 1
    fi
    sleep 3
    _tries=$((_tries + 1))
  done
}

eval 'main "$@";exit "$?"'
