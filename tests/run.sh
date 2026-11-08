#!/usr/bin/env bash
set -Eeuo pipefail
shopt -s inherit_errexit
# End-to-end tests of an image: what can only be proved against the real Network.
# Needs the host's ports 8443, 8080, 5434 and 27117 free. Stops at the first failure.
# usage: run.sh <image> [<test>...]
# The tests share one data folder, in order: most need the ones before them, and some prepare the
# next one, so naming tests is for those that stand alone. Each test starts Network once or twice:
# starting it takes most of the time.
# PODMAN can name another command, for an environment that needs extra options.

PODMAN="${PODMAN:-podman}"
NAME=unifi-standalone-test
URL=https://127.0.0.1:8443
START_TIMEOUT_SECONDS=300
BACKUP_MAX_BYTES=1048576
BIN=/opt/unifi-standalone/bin
TESTS_DIR="$(cd -- "$(dirname -- "$0")" && pwd)"
FIXTURE="${TESTS_DIR}/fixture/sample-10.6.109.unf"
CONSOLE_FIXTURE="${TESTS_DIR}/fixture/sample-console.unifi"
ADMIN_PASSWORD=smoke-Pass-12345

IMAGE=''
DATA=''
COOKIES=''
TOOLS=''
UP_SINCE=0
MOUNT_DATA=()

function main() {
  if [[ "$#" -lt 1 ]]; then
    printf 'usage: run.sh <image> [<test>...]\n' >&2
    return 2
  fi
  IMAGE="$1"
  DATA="$(mktemp -d)"
  # SELinux would refuse the container a temporary folder, without this
  MOUNT_DATA=(--security-opt label=disable -v "${DATA}:/data")
  trap cleanup EXIT
  # unifi-export also runs outside the image, against any controller
  TOOLS="$(mktemp -d)"
  (cd -- "${TESTS_DIR}/../image" && go build -o "${TOOLS}/unifi-export" ./cmd/unifi-standalone)

  local _tests=(
    test_first_start
    test_backup_round_trip
    test_settings_lost
    test_root_dropped
    test_running
    test_repair_same_person
    test_repair_owner_only
    test_reset_admin
    test_restore
    test_undo_and_console_restore
    test_version_lost
  )
  if [[ "$#" -gt 1 ]]; then _tests=("${@:2}"); fi
  local _test
  for _test in "${_tests[@]}"; do
    "$_test"
  done
  printf '\nall tests passed in %s\n' "$(clock)"
}

### Tests

# Restarting before the setup must leave the setup open
function test_first_start() {
  title 'first start'
  start
  local _logs
  _logs="$("$PODMAN" logs "$NAME" 2>&1)"
  if ! [[ "$_logs" =~ unifi-standalone\ [^\ ]+:\ Network\ [0-9.]+,\ PostgreSQL\ [0-9.]+,\ Java\ [0-9.]+ ]]; then
    fail 'the start does not say which versions run'
  fi
  printf 'ok    the start says which versions run\n'
  stop
  start
  SMOKE_SETUP=true "${TESTS_DIR}/smoke.sh" "$URL"
  stop
}

# Stopping dumps the settings: keep only what a Home Assistant backup keeps, and start from it
function test_backup_round_trip() {
  title 'backup round trip'
  local _excluded _patterns _pattern _bytes
  _excluded="$(sed -n '/^backup_exclude:/,/^[a-z_]/s/^  - //p' "${TESTS_DIR}/../ha-app-beta/config.yaml")"
  if [[ -z "$_excluded" ]]; then fail 'no backup_exclude found in the app config'; fi
  mapfile -t _patterns <<<"$_excluded"
  for _pattern in "${_patterns[@]}"; do
    "$PODMAN" unshare find "$DATA" -path "${DATA}/${_pattern}" -prune -exec rm -rf -- {} +
  done
  _bytes="$("$PODMAN" unshare du -sb -- "$DATA" | cut -f1)"
  printf 'backup size: %s bytes\n' "$_bytes"
  if [[ "$_bytes" -gt "$BACKUP_MAX_BYTES" ]]; then fail 'the backup is too large'; fi
  start
  "${TESTS_DIR}/smoke.sh" "$URL"
  stop
}

# Only the dump left: the app would wipe the database, and let anyone in as the first admin
function test_settings_lost() {
  title 'only the dump left: the settings and the login stay'
  "$PODMAN" unshare rm -rf -- "${DATA}/postgres" "${DATA}/unifi"
  start
  local _status
  _status="$(curl -ksS -o /dev/null -w '%{http_code}' "${URL}/api/self")"
  if [[ "$_status" != '401' ]]; then fail "without a login, /api/self answers ${_status}"; fi
  printf 'ok    without a login, /api/self answers 401\n'
  "${TESTS_DIR}/smoke.sh" "$URL"
  stop
}

# Network must not run as root, and must not reach Ubiquiti's telemetry
function test_root_dropped() {
  title 'started as root'
  start
  local _hosts _owner
  _hosts="$("$PODMAN" exec "$NAME" cat /etc/hosts)"
  if [[ "$_hosts" != *trace.svc.ui.com* ]]; then fail 'the telemetry hosts are not blocked'; fi
  printf 'ok    the telemetry hosts are blocked\n'
  _owner="$("$PODMAN" top "$NAME" user comm | awk '$2 == "java" { print $1 }')"
  if [[ "$_owner" != 'unifi' ]]; then fail "Java runs as ${_owner:-nothing}"; fi
  printf 'ok    Java does not run as root\n'
  stop
}

# Everything that only needs Network running shares one start. Then it prepares the next start:
# analytics turned on, Inform Host Override set, and the users as a restore from UniFi OS leaves them.
function test_running() {
  title 'restart, without MongoDB'
  "$PODMAN" unshare rm -f -- "${DATA}/unifi/logs/mongod.log"
  start -e UNIFI_HOST_IP=192.0.2.10
  "${TESTS_DIR}/smoke.sh" "$URL"
  if "$PODMAN" unshare test -e "${DATA}/unifi/logs/mongod.log"; then fail 'MongoDB was started'; fi

  # Auto-backups run each minute from now, and are checked once the other checks are done
  local _auto_since
  _auto_since="$(mktemp)"
  set_management '{"autobackup_enabled":true,"autobackup_cron_expr":"* * * * *"}'

  check_forged_login
  check_export
  check_users
  check_backup_command
  check_backup_history
  check_host_ip
  check_auto_backup "$_auto_since"
  rm -f -- "$_auto_since"
  set_management '{"autobackup_cron_expr":"30 0 1 * *"}'
  check_core_stand_in

  title 'settings and users prepared for the next start'
  login
  curl -ksSf -b "$COOKIES" -o /dev/null -H 'Content-Type: application/json' \
    -d '{"enable_analytics":true}' "${URL}/api/s/default/set/setting/super_mgmt"
  if [[ "$(analytics_setting)" != 'true' ]]; then fail 'could not turn the setting on'; fi
  rm -f -- "$COOKIES"
  set_management '{"override_inform_host":true,"override_inform_host_location":"192.0.2.20"}'
  add_user other other-Pass-12345
  add_user person person-Pass-12345
  sql "DELETE FROM config.privilege WHERE admin_id IN
    (SELECT id FROM config.admin WHERE name IN ('other', 'person'))"
  sql "UPDATE config.admin SET name = 'smoke-admin' WHERE name = 'person'"
  sql "UPDATE config.admin SET is_owner = true, shadow = NULL
    WHERE name = 'smoke-admin' AND email NOT LIKE 'person@%'"
  if can_manage smoke-admin 'person-Pass-12345'; then fail 'the setup of this test is wrong'; fi
  printf 'ok    prepared\n'
  stop
  check_backup_command_stopped
}

# As after a restore from UniFi OS: the owner holds the rights, its same-named user the password.
# Then it prepares the next start: only the owner left, with no password.
function test_repair_same_person() {
  title 'after a restore from UniFi OS: the same person gets back its rights'
  local _record
  _record="$(log_size access-repair.log)"
  start
  if ! can_manage smoke-admin 'person-Pass-12345'; then fail 'the same person can not manage'; fi
  if can_manage other 'other-Pass-12345'; then fail 'an unrelated user got rights'; fi
  printf 'ok    only that user got rights, and it can manage\n'
  if ! log_added access-repair.log "$_record" 'gave admin rights'; then
    fail 'the repair left no record'
  fi
  printf 'ok    the repair left a record\n'

  title 'analytics turned off at each start'
  login smoke-admin 'person-Pass-12345'
  if [[ "$(analytics_setting)" != 'false' ]]; then fail 'the analytics setting stays on'; fi
  rm -f -- "$COOKIES"
  printf 'ok    the analytics setting is off again\n'

  # What devices are told is read from super_identity, not from what the settings page wrote
  title 'Inform Host Override reaches the devices'
  local _host _logs
  _host="$(sql "SELECT dynamic_part ->> 'hostname' FROM config.setting WHERE key = 'super_identity'")"
  if [[ "$_host" != '192.0.2.20' ]]; then fail "devices would be told ${_host}, not the override"; fi
  _logs="$("$PODMAN" logs "$NAME" 2>&1)"
  if [[ "$_logs" != *'Inform Host Override now gives devices 192.0.2.20'* ]]; then
    fail 'the change was not logged'
  fi
  printf 'ok    devices are told the override\n'

  sql "DELETE FROM config.privilege
    WHERE admin_id IN (SELECT id FROM config.admin WHERE NOT is_owner)"
  sql "DELETE FROM config.admin WHERE NOT is_owner"
  sql "UPDATE config.admin SET shadow = NULL"
  stop
}

# Only the owner can be told apart: a temporary password, shown in the log
function test_repair_owner_only() {
  title 'after a restore from UniFi OS: a temporary password for the owner'
  start
  local _password
  _password="$(temporary_password)"
  if [[ -z "$_password" ]]; then fail 'no temporary password in the log'; fi
  if ! can_manage smoke-admin "$_password"; then fail 'the temporary password gives no access'; fi
  printf 'ok    the temporary password gives access\n'
  if log_has access-repair.log "$_password"; then fail 'the password is in the permanent record'; fi
  printf 'ok    the password is not in the permanent record\n'
  users set-password smoke-admin <<<"$ADMIN_PASSWORD"
  stop
}

function test_reset_admin() {
  title "Home Assistant's reset_admin setting"
  local _options="${DATA}/options.json"
  printf '{"reset_admin":"smoke-admin"}' | "$PODMAN" unshare tee "$_options" >/dev/null
  start
  local _password
  _password="$(temporary_password)"
  if [[ -z "$_password" ]]; then fail 'no temporary password in the log'; fi
  if ! can_manage smoke-admin "$_password"; then fail 'the temporary password gives no access'; fi
  stop
  start
  if ! can_manage smoke-admin "$_password"; then fail 'a restart reset the password again'; fi
  printf 'ok    it resets once, even though the setting stays\n'
  users set-password smoke-admin <<<"$ADMIN_PASSWORD"
  "$PODMAN" unshare rm -f -- "$_options"
  stop
}

function test_restore() {
  title 'restore of a full 10.6.109 backup'
  start
  local _since _copy="${DATA}/backup/settings.pre-restore.dump"
  _since="$(mktemp)"
  restore_backup "$FIXTURE"
  smoke_on_fixture
  if "$PODMAN" unshare test -e "${DATA}/unifi/data/db"; then fail 'MongoDB files were left'; fi
  # An older copy would pass without this restore having made one
  if ! "$PODMAN" unshare test -s "$_copy" || ! "$PODMAN" unshare test "$_copy" -nt "$_since"; then
    fail 'no safety copy was made before the restore'
  fi
  rm -f -- "$_since"
  printf 'ok    a safety copy was made before the restore\n'
  if ! "$PODMAN" unshare grep -qx 'analytics.enabled=false' \
    "${DATA}/unifi/data/system.properties"; then
    fail 'analytics are on after the restore'
  fi
  printf 'ok    analytics are off after the restore\n'
  stop
}

# The way back from a restore that went wrong, in docs/moving.md. Then the way from UniFi OS's own
# backup file: convert it, and restore the result. The undo removed the network both backups hold,
# so finding it again proves this restore.
function test_undo_and_console_restore() {
  title 'undo a restore with the safety copy'
  "$PODMAN" unshare cp -- "${DATA}/backup/settings.pre-restore.dump" "${DATA}/backup/settings.dump"
  "$PODMAN" unshare cp -- "${DATA}/backup/settings.pre-restore.dump.version" \
    "${DATA}/backup/settings.dump.version"
  "$PODMAN" unshare rm -rf -- "${DATA}/postgres" "${DATA}/unifi/data/db" \
    "${DATA}/unifi/data/restore" "${DATA}/unifi/data/db_migrator"
  start
  "${TESTS_DIR}/smoke.sh" "$URL"
  local _wlans _out
  login
  _wlans="$(curl -ksSf -b "$COOKIES" "${URL}/api/s/default/rest/wlanconf")"
  rm -f -- "$COOKIES"
  if [[ "$_wlans" == *fixture-wlan* ]]; then fail 'the restored network is still there'; fi
  printf 'ok    back to the settings from before the restore\n'

  title 'console backup: converted, then restored'
  _out="$(mktemp -d)"
  "${TOOLS}/unifi-export" --from-console "$CONSOLE_FIXTURE" -o "${_out}/from-console.unf"
  restore_backup "${_out}/from-console.unf"
  rm -rf -- "$_out"
  smoke_on_fixture
  stop
}

# A database with admins and no way to know its version: the app would wipe it
function test_version_lost() {
  title 'version lost: refuses to start, keeps the data'
  "$PODMAN" unshare rm -rf -- "${DATA}/unifi/data/db_migrator" \
    "${DATA}/backup/settings.dump.version"
  "$PODMAN" run -d --name "$NAME" --network=host "${MOUNT_DATA[@]}" "$IMAGE" >/dev/null
  local _waited=0
  while [[ "$("$PODMAN" inspect -f '{{.State.Running}}' "$NAME")" == 'true' ]]; do
    if [[ "$_waited" -ge 120 ]]; then fail 'it did not refuse to start'; fi
    sleep 2
    _waited=$((_waited + 2))
  done
  local _logs
  _logs="$("$PODMAN" logs "$NAME" 2>&1)"
  "$PODMAN" rm -f "$NAME" >/dev/null
  if [[ "$_logs" != *'the app would wipe it'* ]]; then fail 'it stopped for another reason'; fi
  if ! "$PODMAN" unshare test -s "${DATA}/postgres/PG_VERSION"; then fail 'the database is gone'; fi
  if "$PODMAN" unshare test -e "${DATA}/unifi/data/db_migrator/version"; then
    fail 'the app started'
  fi
  printf 'ok    refused to start\n'
}

### Checks on a running controller, in test_running

# On UniFi OS, Network believes these headers about who is calling, on port 8081 behind core's nginx.
# Here they must give nothing, on either port.
function check_forged_login() {
  title 'no way in without a login'
  login
  local _id _header _wifi='/api/s/default/rest/wlanconf'
  _id="$(sql "SELECT id FROM config.admin WHERE name = 'smoke-admin'")"
  expect_status 200 -b "$COOKIES" "${URL}${_wifi}"
  rm -f -- "$COOKIES"
  expect_status 401 "${URL}${_wifi}"
  printf 'ok    no access without a session\n'
  for _header in "X-UserId: ${_id}" "X-ApiKeyId: ${_id}"; do
    expect_status 401 -H "$_header" "${URL}${_wifi}"
    expect_status 400 -H "$_header" "http://127.0.0.1:8081${_wifi}"
  done
  printf 'ok    forged headers give nothing\n'
}

# What only a real Network can show: damaged files are checked by the unit tests
function check_export() {
  title 'unifi-export'
  local _export="${TOOLS}/unifi-export" _out _site
  _out="$(mktemp -d)"
  UNIFI_PASSWORD="$ADMIN_PASSWORD" "$_export" "$URL" --insecure -u smoke-admin \
    -o "${_out}/full.unf"
  "$_export" --check "${_out}/full.unf"
  printf 'ok    exports a full backup that passes its check\n'
  "$PODMAN" run --rm --network=host --security-opt label=disable \
    -v "${TESTS_DIR}/fixture:/fixture:ro" --entrypoint "${BIN}/unifi-export" "$IMAGE" \
    --check /fixture/sample-10.6.109.unf
  printf 'ok    the image knows which versions its Network accepts\n'

  login
  _site="$(curl -ksSf -b "$COOKIES" -H 'Content-Type: application/json' -d '{"cmd":"export-site"}' \
    "${URL}/api/s/default/cmd/backup" | sed -nE 's/.*"url":"([^"]+)".*/\1/p')"
  curl -ksSf -b "$COOKIES" -o "${_out}/site.unf" "${URL}${_site}"
  rm -f -- "$COOKIES"
  if [[ "$("$_export" --check "${_out}/site.unf" 2>&1 || true)" != *'site export'* ]]; then
    fail 'a site export was not recognized'
  fi
  printf 'ok    recognizes a real site export\n'
  if [[ "$(UNIFI_PASSWORD='wrong' "$_export" "$URL" --insecure -u smoke-admin \
    -o "${_out}/no.unf" 2>&1 || true)" != *api.err.Invalid* ]]; then
    fail 'a refused login was not reported'
  fi
  printf 'ok    reports a refused login\n'
  rm -rf -- "$_out"
}

function check_users() {
  title 'unifi-users'
  if [[ "$(users list)" != *smoke-admin* ]]; then fail 'list does not show the admin'; fi
  printf 'ok    list reads the users\n'
  if users set-password smoke-admin <<<'short' 2>/dev/null; then fail 'a short password passed'; fi
  users set-password smoke-admin <<<'smoke-Pass-99999'
  if ! can_log_in smoke-admin 'smoke-Pass-99999'; then fail 'the new password does not work'; fi
  if can_log_in smoke-admin "$ADMIN_PASSWORD"; then fail 'the old password still works'; fi
  users set-password smoke-admin <<<"$ADMIN_PASSWORD"
  printf 'ok    set-password changes the password\n'

  add_user limited limited-Pass-12345
  sql "DELETE FROM config.privilege
    WHERE admin_id = (SELECT id FROM config.admin WHERE name = 'limited')"
  if can_manage limited limited-Pass-12345; then fail 'the setup of this test is wrong'; fi
  users grant-admin limited --yes
  if ! can_manage limited limited-Pass-12345; then fail 'grant-admin gave no access'; fi
  printf 'ok    grant-admin gives access\n'
  sql "DELETE FROM config.privilege
    WHERE admin_id = (SELECT id FROM config.admin WHERE name = 'limited')"
  sql "DELETE FROM config.admin WHERE name = 'limited'"
}

# What Home Assistant runs before a backup. The failure half runs once stopped, in
# check_backup_command_stopped.
function check_backup_command() {
  title "Home Assistant's backup command"
  local _dump="${DATA}/backup/settings.dump" _before _after
  _before="$("$PODMAN" unshare sha256sum -- "$_dump")"
  backup_command >/dev/null
  _after="$("$PODMAN" unshare sha256sum -- "$_dump")"
  if [[ "$_before" == "$_after" ]]; then fail 'it wrote no new dump'; fi
  printf 'ok    writes a new dump\n'
}

# A failure must not replace the previous dump
function check_backup_command_stopped() {
  title "Home Assistant's backup command, with Network stopped"
  local _dump="${DATA}/backup/settings.dump" _before _after
  _before="$("$PODMAN" unshare sha256sum -- "$_dump")"
  # No Postgres running here: its error is expected
  if backup_command >/dev/null 2>&1; then fail 'a failed dump exited with success'; fi
  _after="$("$PODMAN" unshare sha256sum -- "$_dump")"
  if [[ "$_before" != "$_after" ]]; then fail 'a failed dump changed the previous one'; fi
  printf 'ok    exits with a failure, keeps the previous dump\n'
}

# The dump's table of contents is plain text, with an entry per table holding data. Each dump reads
# the setting, whether Home Assistant asks for it or Network stops.
function check_backup_history() {
  title 'history kept in the dump, when asked'
  local _dump="${DATA}/backup/settings.dump" _options="${DATA}/options.json" _without _with _again
  _without="$("$PODMAN" unshare grep -ac 'TABLE DATA' "$_dump")"
  printf '{"backup_history":true}' | "$PODMAN" unshare tee "$_options" >/dev/null
  backup_command >/dev/null
  _with="$("$PODMAN" unshare grep -ac 'TABLE DATA' "$_dump")"
  "$PODMAN" unshare rm -f -- "$_options"
  backup_command >/dev/null
  _again="$("$PODMAN" unshare grep -ac 'TABLE DATA' "$_dump")"
  if [[ "$_with" -le "$_without" ]]; then fail 'the dump has no more tables with history'; fi
  if [[ "$_again" != "$_without" ]]; then fail 'history stays in the dump by default'; fi
  printf 'ok    with history: %s tables with data; without: %s\n' "$_with" "$_without"
}

# test_running starts Network with UNIFI_HOST_IP=192.0.2.10
function check_host_ip() {
  title 'host address given to the app'
  login
  local _waited=0
  until [[ "$(curl -ksSf -b "$COOKIES" "${URL}/api/s/default/stat/sysinfo")" == *192.0.2.10* ]]; do
    if [[ "$_waited" -ge 90 ]]; then fail 'the app does not report the host address'; fi
    sleep 5
    _waited=$((_waited + 5))
  done
  rm -f -- "$COOKIES"
  printf 'ok    the app reports the host address\n'
}

# Home Assistant's backups keep these as a way out: one must really appear, and pass the check. Only
# one written since this test turned them on counts.
# usage: check_auto_backup <file older than the auto-backups to count>
function check_auto_backup() {
  title 'auto-backup'
  local _copy _waited=0
  _copy="$(mktemp -d)"
  until auto_backup_is_valid "$1" "${_copy}/auto.unf"; do
    if [[ "$_waited" -ge 150 ]]; then fail 'no valid auto-backup appeared'; fi
    sleep 5
    _waited=$((_waited + 5))
  done
  rm -rf -- "$_copy"
  printf 'ok    an auto-backup appeared, and passes the check\n'
}

# Network's watchdog of core's gRPC starts after 30 seconds, then logs an error every 10. It asks
# for ulp-go's manifest each minute.
function check_core_stand_in() {
  title 'the stand-in for unifi-core'
  local _left=$((UP_SINCE + 75 - SECONDS))
  if [[ "$_left" -gt 0 ]]; then sleep "$_left"; fi
  login
  local _status
  _status="$(curl -ksS -b "$COOKIES" -o /dev/null -w '%{http_code}' \
    "${URL}/v2/api/site/default/device-tags")"
  rm -f -- "$COOKIES"
  if [[ "$_status" != '200' ]]; then fail "device tags answered ${_status}"; fi
  printf 'ok    device tags answer\n'
  if log_has server.log 'gRPC channel is in invalid'; then fail "Network's watchdog logs errors"; fi
  printf 'ok    the gRPC watchdog stays quiet\n'
  if log_has server.log 'Cannot preload the ULP'; then fail 'the ULP manifest error is still logged'; fi
  printf 'ok    the ULP manifest error is gone\n'
  if log_has tasks.log ' DEBUG '; then fail 'the tasks log is still at DEBUG'; fi
  printf 'ok    the tasks log stays at INFO\n'
}

### Helpers

# Runs unifi-users next to the running container.
# usage: users <arguments>...
function users() {
  "$PODMAN" run --rm -i --network=host "${MOUNT_DATA[@]}" --entrypoint "${BIN}/unifi-users" \
    "$IMAGE" "$@"
}

function backup_command() {
  "$PODMAN" run --rm --network=host "${MOUNT_DATA[@]}" --entrypoint "${BIN}/unifi-backup" "$IMAGE"
}

# usage: sql <query>
function sql() {
  "$PODMAN" run --rm --network=host --entrypoint psql "$IMAGE" -h 127.0.0.1 -p 5434 -U postgres \
    -d unifi-network -At -c "$1"
}

# usage: add_user <name> <password>
function add_user() {
  local _cookies
  _cookies="$(mktemp)"
  curl -ksSf -c "$_cookies" -o /dev/null -H 'Content-Type: application/json' \
    -d "{\"username\":\"smoke-admin\",\"password\":\"${ADMIN_PASSWORD}\"}" "${URL}/api/login"
  curl -ksSf -b "$_cookies" -o /dev/null -H 'Content-Type: application/json' \
    -d "{\"cmd\":\"create-admin\",\"name\":\"$1\",\"email\":\"$1@example.invalid\",
      \"x_password\":\"$2\",\"role\":\"admin\"}" \
    "${URL}/api/s/default/cmd/sitemgr"
  rm -f -- "$_cookies"
}

# usage: can_log_in <name> <password>
function can_log_in() {
  [[ "$(curl -ksS -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' \
    -d "{\"username\":\"$1\",\"password\":\"$2\"}" "${URL}/api/login")" == '200' ]]
}

# More than a login: without rights on sites, the web interface shows a black page after it.
# usage: can_manage <name> <password>
function can_manage() {
  local _cookies _status
  _cookies="$(mktemp)"
  _status="$(curl -ksS -c "$_cookies" -o /dev/null -w '%{http_code}' \
    -H 'Content-Type: application/json' \
    -d "{\"username\":\"$1\",\"password\":\"$2\"}" "${URL}/api/login")"
  if [[ "$_status" == '200' ]]; then
    _status="$(curl -ksS -b "$_cookies" -o /dev/null -w '%{http_code}' "${URL}/v2/api/info")"
  fi
  rm -f -- "$_cookies"
  [[ "$_status" == '200' ]]
}

# usage: expect_status <status> <curl arguments>...
function expect_status() {
  local _expected="$1" _status
  shift
  _status="$(curl -ksS -o /dev/null -w '%{http_code}' "$@")"
  if [[ "$_status" != "$_expected" ]]; then fail "answered ${_status}, not ${_expected}: $*"; fi
}

# usage: login [<name> <password>]
function login() {
  COOKIES="$(mktemp)"
  curl -ksSf -c "$COOKIES" -o /dev/null -H 'Content-Type: application/json' \
    -d "{\"username\":\"${1:-smoke-admin}\",\"password\":\"${2:-$ADMIN_PASSWORD}\"}" \
    "${URL}/api/login"
}

function analytics_setting() {
  curl -ksSf -b "$COOKIES" "${URL}/api/s/default/get/setting/super_mgmt" |
    sed -nE 's/.*"enable_analytics":(true|false).*/\1/p'
}

# Changes settings as the settings page does.
# usage: set_management <settings as JSON>
function set_management() {
  login
  curl -ksSf -b "$COOKIES" -o /dev/null -H 'Content-Type: application/json' -d "$1" \
    "${URL}/api/s/default/set/setting/super_mgmt"
  rm -f -- "$COOKIES"
}

# The password the container printed for this start
function temporary_password() {
  "$PODMAN" logs "$NAME" 2>&1 | sed -nE 's/.*temporary password "([^"]+)".*/\1/p'
}

# A missing log would otherwise look like a log without errors.
# usage: log_has <file in the logs folder> <text>
function log_has() {
  local _file="${DATA}/unifi/logs/$1"
  if ! "$PODMAN" unshare test -s "$_file"; then fail "the log $1 is missing or empty"; fi
  "$PODMAN" unshare grep -qF -- "$2" "$_file"
}

# usage: log_size <file in the logs folder>
function log_size() {
  "$PODMAN" unshare stat -c %s -- "${DATA}/unifi/logs/$1" 2>/dev/null || printf '0\n'
}

# Whether text was added to a log after it had a size: older lines from another start don't count.
# usage: log_added <file in the logs folder> <size before> <text>
function log_added() {
  local _added
  _added="$("$PODMAN" unshare tail -c "+$(($2 + 1))" -- "${DATA}/unifi/logs/$1")"
  [[ "$_added" == *"$3"* ]]
}

# It can be partly written for a while.
# usage: auto_backup_is_valid <file older than the auto-backups to count> <where to copy one>
function auto_backup_is_valid() {
  local _file
  _file="$("$PODMAN" unshare find "${DATA}/unifi/data/backup/autobackup" -name '*.unf' \
    -newer "$1" -print -quit 2>/dev/null)" || return 1
  if [[ -z "$_file" ]]; then return 1; fi
  "$PODMAN" unshare cat -- "$_file" >"$2"
  "${TOOLS}/unifi-export" --check "$2" >/dev/null 2>&1
}

# Uploads a backup and restores it. Network exits, then the supervisor starts it again, to migrate.
# usage: restore_backup <file>
function restore_backup() {
  login
  local _id
  _id="$(curl -ksSf -b "$COOKIES" -F "file=@$1" "${URL}/upload/backup" |
    sed -E 's/.*"backup_id":"([^"]+)".*/\1/')"
  curl -ksSf -b "$COOKIES" -o /dev/null -H 'Content-Type: application/json' \
    -d "{\"cmd\":\"restore\",\"backup_id\":\"${_id}\"}" "${URL}/api/s/default/cmd/backup"
  rm -f -- "$COOKIES"
  wait_down
  wait_up
}

function smoke_on_fixture() {
  SMOKE_EXPECT_WLAN=fixture-wlan SMOKE_EXPECT_NETWORK=fixture-vlan "${TESTS_DIR}/smoke.sh" "$URL"
}

# usage: start [<podman run options>...]
function start() {
  "$PODMAN" run -d --name "$NAME" --network=host "${MOUNT_DATA[@]}" "$@" "$IMAGE" >/dev/null
  wait_up
}

function stop() {
  "$PODMAN" stop -t 120 "$NAME" >/dev/null
  print_unexpected_calls
  "$PODMAN" rm -f "$NAME" >/dev/null
}

# While starting, /status answers 200 with an HTML page. Not with `| grep -q`: grep leaving early
# makes curl fail, which pipefail reports as not up.
function is_up() {
  [[ "$(curl -ksf -m 3 "${URL}/status" || true)" == *'"up":true'* ]]
}

function wait_up() {
  local _waited=0
  until is_up; do
    if [[ "$_waited" -ge "$START_TIMEOUT_SECONDS" ]]; then fail 'the controller did not start'; fi
    if [[ "$("$PODMAN" inspect -f '{{.State.Running}}' "$NAME")" != 'true' ]]; then
      fail 'the container exited'
    fi
    sleep 3
    _waited=$((_waited + 3))
  done
  UP_SINCE="$SECONDS"
}

function wait_down() {
  local _waited=0
  while is_up; do
    if [[ "$_waited" -ge 60 ]]; then fail 'the controller did not exit for the restore'; fi
    sleep 1
    _waited=$((_waited + 1))
  done
}

function cleanup() {
  "$PODMAN" rm -f -t 30 "$NAME" >/dev/null 2>&1 || true
  if [[ -n "$DATA" ]]; then "$PODMAN" unshare rm -rf -- "$DATA"; fi
  if [[ -n "$TOOLS" ]]; then rm -rf -- "$TOOLS"; fi
}

# Not a failure, but what a new Network version newly asks of core shows here
function print_unexpected_calls() {
  "$PODMAN" logs "$NAME" 2>&1 | grep -E 'core stub: unexpected|unexpected gRPC call' | sort -u || true
}

# The time since the tests started, as minutes and seconds
function clock() {
  printf '%dm%02ds' "$((SECONDS / 60))" "$((SECONDS % 60))"
}

# usage: title <text>
function title() {
  printf '\n=== %s  [%s]\n' "$1" "$(clock)"
}

# usage: fail <message>
function fail() {
  printf 'FAIL  %s\n' "$1" >&2
  print_unexpected_calls
  "$PODMAN" logs --tail 40 "$NAME" >&2 || true
  exit 1
}

eval 'main "$@";exit "$?"'
