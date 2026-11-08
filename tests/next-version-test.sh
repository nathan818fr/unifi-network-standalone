#!/usr/bin/env bash
set -Eeuo pipefail
shopt -s inherit_errexit
# Checks which version a release gets: a wrong one would hide an update from Home Assistant, or
# reuse a tag that users already have.

SCRIPT="$(cd -- "$(dirname -- "$0")/.." && pwd)/.github/scripts/next-version.sh"
REPO=''

function main() {
  REPO="$(mktemp -d)"
  trap 'rm -rf -- "$REPO"' EXIT
  cd -- "$REPO"
  git init -q
  git config user.name test
  git config user.email test@example.invalid
  git config commit.gpgsign false
  git config tag.gpgsign false
  mkdir -p image ha-app ha-app-beta docs
  set_network 11.0.99
  printf 'version: "11.0.99-build1"\nslug: test\n' >ha-app-beta/config.yaml
  commit first

  expect 11.0.99-build1 'the first release'
  git tag v11.0.99-build1
  expect '' 'nothing changed'

  sed -i 's/^version:.*/version: "11.0.99-build2"/' ha-app-beta/config.yaml
  commit version
  expect '' 'only the version the workflow writes'

  printf 'docs\n' >docs/page.md
  commit docs
  expect '' 'only the documentation'

  printf 'version: "11.0.99-build1"\nslug: stable\n' >ha-app/config.yaml
  commit promote
  expect '' 'only a promotion'

  printf 'code\n' >image/code
  commit code
  expect 11.0.99-build2 'the image changed'
  git tag v11.0.99-build2

  printf 'option: 1\n' >>ha-app-beta/config.yaml
  commit option
  expect 11.0.99-build3 "the app's settings changed"

  git tag v11.0.99-build9
  printf 'more\n' >>image/code
  commit more
  expect 11.0.99-build10 'past nine builds'
  git tag v11.0.99-build10

  set_network 11.0.120
  commit network
  expect 11.0.120-build1 'a new Network'
  git tag v11.0.120-build1

  printf 'again\n' >>image/code
  commit again
  expect 11.0.120-build2 'the newest tag is compared numerically'
  git tag v11.0.120-build2

  git checkout -q v11.0.120-build1
  if "$SCRIPT" >/dev/null 2>&1; then
    printf 'FAIL  a commit older than the last release got a version\n' >&2
    return 1
  fi
  printf 'ok    refuses a commit older than the last release\n'
  git checkout -q -

  set_network 11.0.99
  commit 'back to an older Network'
  expect 11.0.99-build11 'an older Network continues its own numbers'
  git tag v11.0.99-build11
  expect '' 'an older Network with nothing new settles'
  printf 'all tests passed\n'
}

### Helpers

# usage: set_network <version>
function set_network() {
  printf 'ARG UNIFI_VERSION=%s\n' "$1" >image/Containerfile
}

# usage: commit <message>
function commit() {
  git add -A
  git commit -qm "$1"
}

# usage: expect <version> <label>
function expect() {
  local _got
  _got="$("$SCRIPT")"
  _got="${_got#version=}"
  if [[ "$_got" != "$1" ]]; then
    printf 'FAIL  %s: got "%s", want "%s"\n' "$2" "$_got" "$1" >&2
    return 1
  fi
  printf 'ok    %s\n' "$2"
}

eval 'main "$@";exit "$?"'
