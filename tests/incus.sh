#!/usr/bin/env bash
# @@License : WTFPL
# Full system-container integration tests for systemd and OpenRC.

set -euo pipefail

MODE="${1:-all}"
case "$MODE" in
    systemd|openrc) MODES="$MODE" ;;
    all) MODES="systemd openrc" ;;
    *) echo "Usage: $0 [systemd|openrc|all]" >&2; exit 2 ;;
esac

if ! command -v incus >/dev/null 2>&1; then
    echo "ERROR: Incus is required for tests/incus.sh" >&2
    exit 69
fi

PROJECT_DIR=$(git rev-parse --show-toplevel)
PROJECT_NAME=$(awk '/^## Project variables/{inside=1; next} /^## /{inside=0} inside && /^project_name:[[:space:]]*/{sub(/^project_name:[[:space:]]*/, ""); print; exit}' "$PROJECT_DIR/IDEA.md")
PROJECT_ORG=$(awk '/^## Project variables/{inside=1; next} /^## /{inside=0} inside && /^project_org:[[:space:]]*/{sub(/^project_org:[[:space:]]*/, ""); print; exit}' "$PROJECT_DIR/IDEA.md")
INTERNAL_NAME=$(awk '/^## Project variables/{inside=1; next} /^## /{inside=0} inside && /^internal_name:[[:space:]]*/{sub(/^internal_name:[[:space:]]*/, ""); print; exit}' "$PROJECT_DIR/IDEA.md")
REMOTE=$(git -C "$PROJECT_DIR" remote get-url origin 2>/dev/null || true)
if [ -z "$PROJECT_NAME" ]; then PROJECT_NAME=$(printf '%s\n' "$REMOTE" | sed -E 's|.*/([^/]+)(\.git)?$|\1|'); [ -n "$PROJECT_NAME" ] || PROJECT_NAME=$(basename "$PROJECT_DIR"); fi
if [ -z "$PROJECT_ORG" ]; then PROJECT_ORG=$(printf '%s\n' "$REMOTE" | sed -E 's|.*/([^/]+)/[^/]+(\.git)?$|\1|'); [ -n "$PROJECT_ORG" ] || PROJECT_ORG=$(basename "$(dirname "$PROJECT_DIR")"); fi
if [ -z "$INTERNAL_NAME" ]; then INTERNAL_NAME="$PROJECT_NAME"; fi

API_VERSION="${API_VERSION:-v1}"
INCUS_CPUS="${INCUS_CPUS:-2}"
INCUS_MEM="${INCUS_MEM:-4GiB}"
INCUS_CPU_PRIORITY="${INCUS_CPU_PRIORITY:-2}"
INCUS_OP_TIMEOUT="${INCUS_OP_TIMEOUT:-120}"
INCUS_INSTALL_TIMEOUT="${INCUS_INSTALL_TIMEOUT:-600}"
INCUS_TEST_TIMEOUT="${INCUS_TEST_TIMEOUT:-1800}"
TOR_WAIT="${TOR_WAIT:-300}"
I2P_WAIT="${I2P_WAIT:-600}"
INCUS_DNS_SERVER="${INCUS_DNS_SERVER:-}"
TEMP_PARENT="${TMPDIR:-/tmp}/${PROJECT_ORG}"
mkdir -p "$TEMP_PARENT"
TEST_TEMP_DIR=$(mktemp -d "$TEMP_PARENT/${PROJECT_NAME}-incus-XXXXXXXX")
chmod 700 "$TEST_TEMP_DIR"
BUILD_DIR="$TEST_TEMP_DIR/binaries"
RELEASE_DIR="$TEST_TEMP_DIR/releases"
CONTAINER_NAME=""

cleanup() {
    result=$?
    if [ -n "$CONTAINER_NAME" ]; then
        timeout 60 incus delete "$CONTAINER_NAME" --force 2>/dev/null || true
    fi
    if [ -d "$TEST_TEMP_DIR" ]; then
        python3 -c 'import shutil,sys; shutil.rmtree(sys.argv[1])' "$TEST_TEMP_DIR"
    fi
    return "$result"
}
trap cleanup EXIT INT TERM

incus_exec() {
    local limit="$1"
    shift
    timeout "$limit" incus exec "$CONTAINER_NAME" -- "$@"
}

mkdir -p "$BUILD_DIR" "$RELEASE_DIR"
make -C "$PROJECT_DIR" local BINDIR="$BUILD_DIR" RELDIR="$RELEASE_DIR"

random_suffix() {
    od -An -N4 -tx1 /dev/urandom | tr -d ' \n'
}

wait_for_container() {
    local container="$1"
    for _ in $(seq 1 60); do
        if timeout 10 incus exec "$container" -- true >/dev/null 2>&1; then
            return 0
        fi
        sleep 2
    done
    echo "ERROR: Incus container $container did not become ready" >&2
    return 1
}

run_mode() {
    local init="$1" image container
    container="${PROJECT_NAME}-$(random_suffix)"
    CONTAINER_NAME="$container"
    case "$init" in
        systemd) image="images:debian/trixie" ;;
        openrc) image="images:alpine/3.24" ;;
        *) echo "ERROR: unsupported init mode $init" >&2; return 2 ;;
    esac

    echo "=== [$init] Launching $image as $container ==="
    timeout "$INCUS_INSTALL_TIMEOUT" incus launch "$image" "$container" -c limits.cpu="$INCUS_CPUS" -c limits.memory="$INCUS_MEM" -c limits.cpu.priority="$INCUS_CPU_PRIORITY"
    wait_for_container "$container"

    if [ -n "$INCUS_DNS_SERVER" ]; then
        if [[ ! "$INCUS_DNS_SERVER" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]]; then
            echo "ERROR: INCUS_DNS_SERVER must be an IPv4 address" >&2
            return 2
        fi
        echo "=== [$init] Using temporary resolver $INCUS_DNS_SERVER ==="
        # The guest command intentionally expands its positional resolver at runtime.
        # shellcheck disable=SC2016
        incus_exec "$INCUS_OP_TIMEOUT" sh -ec 'printf "nameserver %s\n" "$1" > /etc/resolv.conf; nslookup dl-cdn.alpinelinux.org >/dev/null' sh "$INCUS_DNS_SERVER"
    fi

    if [ "$init" = systemd ]; then
        incus_exec "$INCUS_INSTALL_TIMEOUT" bash -lc 'apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq bash curl jq file tor i2pd'
        incus_exec "$INCUS_OP_TIMEOUT" bash -lc 'systemctl disable --now tor.service tor@default.service i2pd.service >/dev/null 2>&1 || true'
    else
        incus_exec "$INCUS_INSTALL_TIMEOUT" sh -lc 'apk add --no-cache bash curl jq file tor i2pd tzdata ca-certificates'
        incus_exec "$INCUS_OP_TIMEOUT" sh -lc 'rc-service tor stop >/dev/null 2>&1 || true; rc-service i2pd stop >/dev/null 2>&1 || true; rc-update del tor default >/dev/null 2>&1 || true; rc-update del i2pd default >/dev/null 2>&1 || true'
    fi

    timeout "$INCUS_OP_TIMEOUT" incus file push "$BUILD_DIR/$PROJECT_NAME" "$container/usr/local/bin/"
    timeout "$INCUS_OP_TIMEOUT" incus file push "$BUILD_DIR/$PROJECT_NAME-cli" "$container/usr/local/bin/"
    incus_exec "$INCUS_OP_TIMEOUT" chmod 0755 "/usr/local/bin/$PROJECT_NAME" "/usr/local/bin/$PROJECT_NAME-cli"

    incus_exec "$INCUS_TEST_TIMEOUT" env PROJECT_NAME="$PROJECT_NAME" PROJECT_ORG="$PROJECT_ORG" INTERNAL_NAME="$INTERNAL_NAME" INIT="$init" API_VERSION="$API_VERSION" TOR_WAIT="$TOR_WAIT" I2P_WAIT="$I2P_WAIT" REQUIRE_OVERLAY="${REQUIRE_OVERLAY:-0}" bash -s <<'REMOTE'
set -euo pipefail

fail() {
    echo "FAILED: $*" >&2
    exit 1
}

case "$INIT" in
    systemd)
        [ "$(ps -p 1 -o comm= | tr -d ' ')" = systemd ] || fail "PID 1 is not systemd"
        SERVICE_FILE="/etc/systemd/system/${INTERNAL_NAME}.service"
        service_active() { systemctl is-active --quiet "$INTERNAL_NAME"; }
        service_enabled() { systemctl is-enabled --quiet "$INTERNAL_NAME"; }
        service_pid() { systemctl show -p MainPID --value "$INTERNAL_NAME"; }
        service_restart() { systemctl restart "$INTERNAL_NAME"; }
        service_stop() { systemctl stop "$INTERNAL_NAME"; }
        service_start() { systemctl start "$INTERNAL_NAME"; }
        service_disabled() { ! systemctl is-enabled --quiet "$INTERNAL_NAME"; }
        ;;
    openrc)
        [ -x /sbin/openrc-run ] && [ -d /run/openrc ] || fail "OpenRC is not running"
        case "$(ps -p 1 -o comm= | tr -d ' ')" in init|openrc-init) ;; *) fail "PID 1 is not OpenRC" ;; esac
        SERVICE_FILE="/etc/init.d/${INTERNAL_NAME}"
        service_active() { rc-service "$INTERNAL_NAME" status >/dev/null 2>&1; }
        service_enabled() { rc-update show default | grep -Eq "(^|[[:space:]])${INTERNAL_NAME}([[:space:]]|$)"; }
        service_pid() { cat "/var/run/${PROJECT_ORG}-${INTERNAL_NAME}.pid"; }
        service_restart() { rc-service "$INTERNAL_NAME" restart; }
        service_stop() { rc-service "$INTERNAL_NAME" stop; }
        service_start() { rc-service "$INTERNAL_NAME" start; }
        service_disabled() { ! service_enabled; }
        ;;
esac

echo "=== [$INIT] Init identity and service installation ==="
"$PROJECT_NAME" --version
"$PROJECT_NAME" --help >/dev/null
"$PROJECT_NAME" --service --install --yes
[ -f "$SERVICE_FILE" ] || fail "service file was not installed"
service_enabled || fail "service is not enabled"
service_active || fail "service is not active after install"

wait_for_health() {
    local attempt port
    port=$(awk '/^[[:space:]]+port:/ {gsub(/[^0-9]/, "", $2); print $2; exit}' "/etc/${PROJECT_ORG}/${INTERNAL_NAME}/server.yml")
    [ -n "$port" ] || fail "server port is missing from server.yml"
    BASE_URL="http://127.0.0.1:${port}"
    for attempt in $(seq 1 60); do
        if curl -q -fsS --max-time 2 "$BASE_URL/api/${API_VERSION}/server/healthz" >/dev/null 2>&1; then
            return 0
        fi
        sleep 1
    done
    echo "Health URL: $BASE_URL/api/${API_VERSION}/server/healthz" >&2
    if [ "$INIT" = systemd ]; then
        systemctl status "$INTERNAL_NAME" --no-pager >&2 || true
        journalctl -u "$INTERNAL_NAME" -n 80 --no-pager >&2 || true
    else
        rc-service "$INTERNAL_NAME" status >&2 || true
        tail -n 80 "/var/log/${PROJECT_ORG}/${INTERNAL_NAME}/server.log" >&2 || true
        tail -n 80 "/var/log/${PROJECT_ORG}/${INTERNAL_NAME}/error.log" >&2 || true
    fi
    ss -lntp >&2 || true
    fail "health endpoint did not become ready"
}

wait_for_health
old_pid=$(service_pid)
"$PROJECT_NAME" --service restart
for attempt in $(seq 1 30); do
    new_pid=$(service_pid 2>/dev/null || true)
    if service_active && [ -n "$new_pid" ] && [ "$new_pid" != "$old_pid" ]; then break; fi
    sleep 1
done
[ "$new_pid" != "$old_pid" ] || fail "service restart did not change MainPID"
wait_for_health

echo "=== [$INIT] Stop and start checks ==="
service_stop
for attempt in $(seq 1 20); do
    service_active || break
    sleep 1
done
! service_active || fail "service remained active after stop"
if curl -q -fsS --max-time 2 "$BASE_URL/api/${API_VERSION}/server/healthz" >/dev/null 2>&1; then
    fail "health endpoint still answered after service stop"
fi
"$PROJECT_NAME" --service start
service_active || fail "service did not start"
wait_for_health

echo "=== [$INIT] API and client smoke checks ==="
curl -q -fsS -H 'Accept: application/json' "$BASE_URL/api/${API_VERSION}/list" | jq -e 'length > 0' >/dev/null
curl -q -fsS "$BASE_URL/api/${API_VERSION}/templates/go" | grep -q 'Go'
curl -q -fsS "$BASE_URL/api/${API_VERSION}/server/swagger" | jq -e '.openapi' >/dev/null
printf '%s\n' '{"query":"{ __typename }"}' | curl -q -fsS -X POST -H 'Content-Type: application/json' --data-binary @- "$BASE_URL/api/${API_VERSION}/server/graphql" | jq -e '.data' >/dev/null
"${PROJECT_NAME}-cli" --server "$BASE_URL" Go Node | grep -q '### Go ###'
TEST_TMP=/tmp/apimgr/${PROJECT_NAME}-test
mkdir -p "$TEST_TMP"
cp "/usr/local/bin/${PROJECT_NAME}" "$TEST_TMP/renamed-server"
chmod 0755 "$TEST_TMP/renamed-server"
"$TEST_TMP/renamed-server" --help 2>&1 | grep -q 'renamed-server' || fail "renamed binary did not use its actual name"

echo "=== [$INIT] Tor and I2P default contracts ==="
health=$(curl -q -fsS -H 'Accept: application/json' "$BASE_URL/api/${API_VERSION}/server/healthz")
echo "$health" | jq -e '.features.tor.enabled == true' >/dev/null || fail "Tor binary was not detected"
echo "$health" | jq -e '.features.i2p.enabled == false and (.checks | has("i2p") | not)' >/dev/null || fail "I2P was not disabled by default on a system container"

wait_overlay() {
    local feature="$1" timeout="$2" attempt status
    for attempt in $(seq 1 "$timeout"); do
        health=$(curl -q -fsS -H 'Accept: application/json' "$BASE_URL/api/${API_VERSION}/server/healthz")
        if echo "$health" | grep -Eq '/(etc|var|home|config|data)/'; then
            fail "health response leaked a filesystem path"
        fi
        status=$(echo "$health" | jq -r ".features.${feature}.status")
        case "$status" in healthy|unavailable|disabled) break ;; esac
        sleep 1
    done
    echo "$health"
}

tor_health=$(wait_overlay tor "$TOR_WAIT")
tor_status=$(echo "$tor_health" | jq -r '.features.tor.status')
case "$tor_status" in healthy|unavailable) ;; *) fail "invalid Tor status: $tor_status" ;; esac
if [ "$tor_status" = healthy ]; then
    echo "$tor_health" | jq -e '.checks.tor == "ok"' >/dev/null || fail "healthy Tor check was not ok"
    echo "$tor_health" | jq -r '.features.tor.hostname' | grep -Eq '^[a-z2-7]{56}\.onion$' || fail "Tor hostname format is invalid"
    [ -f "/var/lib/${PROJECT_ORG}/${INTERNAL_NAME}/tor/site/hostname" ] || fail "Tor hostname was not persisted"
    for directory in "/etc/${PROJECT_ORG}/${INTERNAL_NAME}/tor" "/var/lib/${PROJECT_ORG}/${INTERNAL_NAME}/tor" "/var/lib/${PROJECT_ORG}/${INTERNAL_NAME}/tor/site"; do
        [ "$(stat -c '%a' "$directory")" = 700 ] || fail "Tor directory permissions are not 0700: $directory"
    done
elif [ "$REQUIRE_OVERLAY" = 1 ]; then
    fail "Tor did not bootstrap (status $tor_status)"
else
    echo "SKIP: Tor bootstrap unavailable (status $tor_status)"
fi

mkdir -p "/etc/${PROJECT_ORG}/${INTERNAL_NAME}/yaml.d"
printf 'server:\n  i2p:\n    enabled: true\n' > "/etc/${PROJECT_ORG}/${INTERNAL_NAME}/yaml.d/50-i2p.yml"
chown "${INTERNAL_NAME}:${INTERNAL_NAME}" "/etc/${PROJECT_ORG}/${INTERNAL_NAME}/yaml.d/50-i2p.yml"
chmod 0600 "/etc/${PROJECT_ORG}/${INTERNAL_NAME}/yaml.d/50-i2p.yml"
"$PROJECT_NAME" --service restart
wait_for_health
health=$(curl -q -fsS -H 'Accept: application/json' "$BASE_URL/api/${API_VERSION}/server/healthz")
echo "$health" | jq -e '.features.i2p.enabled == true and .features.i2p.provider == "i2pd"' >/dev/null || fail "I2P provider was not detected after enabling it"
i2p_health=$(wait_overlay i2p "$I2P_WAIT")
i2p_status=$(echo "$i2p_health" | jq -r '.features.i2p.status')
case "$i2p_status" in healthy|unavailable) ;; *) fail "invalid I2P status: $i2p_status" ;; esac
if [ "$i2p_status" = healthy ]; then
    echo "$i2p_health" | jq -e '.checks.i2p == "ok"' >/dev/null || fail "healthy I2P check was not ok"
    echo "$i2p_health" | jq -r '.features.i2p.hostname' | grep -Eq '^[a-z2-7]{52}\.b32\.i2p$' || fail "I2P hostname format is invalid"
    [ -s "/var/lib/${PROJECT_ORG}/${INTERNAL_NAME}/i2p/site/site-keys.dat" ] || fail "I2P destination key was not persisted"
elif [ "$REQUIRE_OVERLAY" = 1 ]; then
    fail "I2P did not bootstrap (status $i2p_status)"
else
    echo "SKIP: I2P bootstrap unavailable (status $i2p_status)"
fi

"$PROJECT_NAME" --service stop
if [ "$INIT" = systemd ]; then
    apt-get remove -y -qq i2pd >/dev/null
else
    apk del i2pd >/dev/null
fi
"$PROJECT_NAME" --service start
wait_for_health
health=$(curl -q -fsS -H 'Accept: application/json' "$BASE_URL/api/${API_VERSION}/server/healthz")
if echo "$health" | grep -Eq '/(etc|var|home|config|data)/'; then fail "health response leaked a filesystem path"; fi
echo "$health" | jq -e '.features.i2p.enabled == false and .features.i2p.provider == "none" and (.checks | has("i2p") | not)' >/dev/null || fail "missing i2pd did not disable I2P cleanly"

"$PROJECT_NAME" --service stop
if [ "$INIT" = systemd ]; then
    apt-get remove -y -qq tor >/dev/null
else
    apk del tor >/dev/null
fi
"$PROJECT_NAME" --service start
wait_for_health
health=$(curl -q -fsS -H 'Accept: application/json' "$BASE_URL/api/${API_VERSION}/server/healthz")
if echo "$health" | grep -Eq '/(etc|var|home|config|data)/'; then fail "health response leaked a filesystem path"; fi
echo "$health" | jq -e '.features.tor.enabled == false and .features.tor.status == "disabled" and .features.tor.hostname == "" and (.checks | has("tor") | not)' >/dev/null || { echo "$health" >&2; command -v tor >&2 || true; fail "missing Tor binary did not disable Tor cleanly"; }

echo "=== [$INIT] Container restart persistence ==="
service_enabled || fail "service lost its enabled state before reboot"
echo "PASS: lifecycle, API, overlay contracts, and binary rename checks"
REMOTE

    echo "=== [$init] Restarting the Incus container ==="
    timeout "$INCUS_OP_TIMEOUT" incus restart "$container"
    wait_for_container "$container"
    incus_exec "$INCUS_OP_TIMEOUT" env PROJECT_NAME="$PROJECT_NAME" PROJECT_ORG="$PROJECT_ORG" INTERNAL_NAME="$INTERNAL_NAME" INIT="$init" API_VERSION="$API_VERSION" bash -s <<'REMOTE'
set -euo pipefail
case "$INIT" in
    systemd)
        for _ in $(seq 1 30); do systemctl show-environment >/dev/null 2>&1 && break; sleep 1; done
        systemctl show-environment >/dev/null 2>&1 || { ps -p 1 -o pid,comm,args >&2 || true; echo "systemd bus did not become ready after reboot" >&2; exit 1; }
        systemctl is-enabled --quiet "$INTERNAL_NAME" || { echo "service is not enabled after reboot" >&2; exit 1; }
        for _ in $(seq 1 30); do systemctl is-active --quiet "$INTERNAL_NAME" && break; sleep 1; done
        systemctl is-active --quiet "$INTERNAL_NAME" || { systemctl status "$INTERNAL_NAME" --no-pager >&2 || true; journalctl -u "$INTERNAL_NAME" -n 80 --no-pager >&2 || true; echo "service is not active after reboot" >&2; exit 1; }
        ;;
    openrc)
        rc-update show default | grep -Eq "(^|[[:space:]])${INTERNAL_NAME}([[:space:]]|$)" || { echo "service is not enabled after reboot" >&2; exit 1; }
        for _ in $(seq 1 30); do rc-service "$INTERNAL_NAME" status >/dev/null 2>&1 && break; sleep 1; done
        rc-service "$INTERNAL_NAME" status >/dev/null || { rc-service "$INTERNAL_NAME" status >&2 || true; echo "service is not active after reboot" >&2; exit 1; }
        ;;
esac
port=$(awk '/^[[:space:]]+port:/ {gsub(/[^0-9]/, "", $2); print $2; exit}' "/etc/${PROJECT_ORG}/${INTERNAL_NAME}/server.yml")
curl -q -fsS -H 'Accept: application/json' "http://127.0.0.1:${port}/api/${API_VERSION}/server/healthz" | jq -e '.status == "healthy" or .status == "degraded"' >/dev/null
"$PROJECT_NAME" --service --disable
case "$INIT" in
    systemd)
        [ -f "/etc/systemd/system/${INTERNAL_NAME}.service" ] || { echo "disable removed service file" >&2; exit 1; }
        ! systemctl is-enabled --quiet "$INTERNAL_NAME" || { echo "service remains enabled after disable" >&2; exit 1; }
        ! systemctl is-active --quiet "$INTERNAL_NAME" || { echo "service remains active after disable" >&2; exit 1; }
        ;;
    openrc)
        [ -f "/etc/init.d/${INTERNAL_NAME}" ] || { echo "disable removed service file" >&2; exit 1; }
        ! rc-update show default | grep -Eq "(^|[[:space:]])${INTERNAL_NAME}([[:space:]]|$)" || { echo "service remains enabled after disable" >&2; exit 1; }
        ! rc-service "$INTERNAL_NAME" status >/dev/null 2>&1 || { echo "service remains active after disable" >&2; exit 1; }
        ;;
esac
"$PROJECT_NAME" --service --install --yes
"$PROJECT_NAME" --service --uninstall --yes
case "$INIT" in
    systemd) [ ! -e "/etc/systemd/system/${INTERNAL_NAME}.service" ] || { echo "uninstall left service file" >&2; exit 1; } ;;
    openrc) [ ! -e "/etc/init.d/${INTERNAL_NAME}" ] || { echo "uninstall left service file" >&2; exit 1; } ;;
esac
for path in "/etc/${PROJECT_ORG}/${INTERNAL_NAME}" "/var/lib/${PROJECT_ORG}/${INTERNAL_NAME}" "/var/cache/${PROJECT_ORG}/${INTERNAL_NAME}" "/var/log/${PROJECT_ORG}/${INTERNAL_NAME}" "/var/backups/${PROJECT_ORG}/${INTERNAL_NAME}"; do
    [ ! -e "$path" ] || { echo "uninstall left application data at $path" >&2; exit 1; }
done
echo "PASS: reboot persistence, disable, reinstall, and uninstall"
REMOTE

    timeout "$INCUS_OP_TIMEOUT" incus delete "$container" --force
    CONTAINER_NAME=""
}

for init in $MODES; do
    run_mode "$init"
done

echo "Incus tests completed successfully ($MODES)"
