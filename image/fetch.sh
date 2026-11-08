#!/usr/bin/env bash
set -Eeuo pipefail
shopt -s inherit_errexit
# Build step: download and verify the app, leave it in /build/out.
# Needs UNIFI_DEB_URL and UNIFI_DEB_SHA256. The package holds the same files for both architectures.

BUILD=/build
OUT=/build/out

function main() {
  mkdir -p -- "$OUT"
  fetch_unifi
  make_logging_config
  add_console_picture
}

### Steps

# Unpack the app: installing it would pull unifi-core and the other services
function fetch_unifi() {
  local _work="${BUILD}/unifi"
  mkdir -p -- "$_work"
  download "$UNIFI_DEB_URL" "$UNIFI_DEB_SHA256" "${_work}/unifi.deb"
  (cd -- "$_work" && ar x unifi.deb)
  tar -xJf "${_work}/data.tar.xz" -C "$_work" ./usr/lib/unifi
  mv -- "${_work}/usr/lib/unifi" "${OUT}/unifi"
  # What the stand-in reports as its version: the one this Network asks for, and the Java it runs
  # on. Without them, a build must fail: Network's own requirements have changed.
  local _control
  _control="$(tar -xOf "$(printf '%s\n' "${_work}"/control.tar.*)" ./control)"
  sed -nE 's/.*unifi-core \(>= ([0-9.]+)\).*/\1/p' <<<"$_control" >"${OUT}/core-version"
  sed -nE 's/.*temurin-([0-9]+)-jre.*/\1/p' <<<"$_control" >"${OUT}/java-major"
  [[ -s "${OUT}/core-version" && -s "${OUT}/java-major" ]]
  rm -rf -- "$_work"
}

# Network's own logging config, with its tasks log at INFO rather than a hard-coded DEBUG: 37 MB a
# day, when every idle disk write counts on an SD card. A build must fail if a new version changes
# what this edits.
function make_logging_config() {
  local _config="${OUT}/logback.xml"
  unzip -p "${OUT}/unifi/lib/ace.jar" BOOT-INF/classes/logback.xml |
    sed -E 's|<logger name="tasks" level="DEBUG"|<logger name="tasks" level="INFO"|' >"$_config"
  grep -q '<logger name="tasks" level="INFO"' "$_config"
}

# With core turned off, Network never loads the description of its console, and its dashboard asks for
# a picture at an address built from nothing. One rule of ours, first among Network's own, answers it
# with Network's own picture of a self-hosted Network server. Its file name holds a hash that changes
# with releases. A build must fail if a new version changes what this edits.
function add_console_picture() {
  local _root="${OUT}/unifi/webapps/ROOT" _picture
  _picture="$(cd -- "$_root" && find app-unifi/react/images/uidb \
    -name '94584a8c-d5c9-4f61-8019-c7457e262803_nopadding_*_96.png')"
  if [[ -z "$_picture" || "$_picture" == *$'\n'* ]]; then
    printf 'error: not one picture of a self-hosted Network server: %s\n' "${_picture:-none}" >&2
    return 1
  fi
  local _rules="${_root}/WEB-INF/urlrewrite.xml"
  [[ "$(head -n 1 -- "$_rules")" == '<urlrewrite>' ]]
  sed -i "1a\\    <rule>\\n        <from>^/manage/undefined/ui_topology_fallback_undefined\\\\.png\$</from>\\n        <to last=\"true\">/${_picture}</to>\\n    </rule>" "$_rules"
  grep -qF "<to last=\"true\">/${_picture}</to>" "$_rules"
}

### Helpers

# usage: download <url> <sha256> <file>
function download() {
  curl -fsSL --retry 5 --retry-all-errors -o "$3" "$1"
  printf '%s  %s\n' "$2" "$3" | sha256sum -c --quiet -
}

eval 'main "$@";exit "$?"'
