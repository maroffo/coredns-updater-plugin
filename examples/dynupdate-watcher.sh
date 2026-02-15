#!/bin/bash
# ABOUTME: Watches a network interface for IP changes and updates dynupdate DNS records.
# ABOUTME: Supports any interface (eth0, wlan0, tailscale0, utun) with configurable poll interval.

set -euo pipefail

# ── Defaults ─────────────────────────────────────────────────────────────────
readonly VERSION="1.0.0"
readonly DEFAULT_INTERVAL=30
readonly DEFAULT_TTL=300
readonly DEFAULT_API_URL="http://localhost:8080"

# ── State ────────────────────────────────────────────────────────────────────
INTERFACE=""
RECORD_NAME=""
API_URL="${DEFAULT_API_URL}"
API_TOKEN=""
INTERVAL="${DEFAULT_INTERVAL}"
TTL="${DEFAULT_TTL}"
RECORD_TYPE="A"
VERBOSE=false
LAST_IP=""

# ── Logging ──────────────────────────────────────────────────────────────────
log_info()  { printf "[INFO]  %s %s\n" "$(date +%H:%M:%S)" "${*}" >&2; }
log_warn()  { printf "[WARN]  %s %s\n" "$(date +%H:%M:%S)" "${*}" >&2; }
log_error() { printf "[ERROR] %s %s\n" "$(date +%H:%M:%S)" "${*}" >&2; }
log_debug() { [[ "${VERBOSE}" == "true" ]] && printf "[DEBUG] %s %s\n" "$(date +%H:%M:%S)" "${*}" >&2; return 0; }
die()       { log_error "${*}"; exit 1; }

# ── Help ─────────────────────────────────────────────────────────────────────
help() {
    cat <<'USAGE'
dynupdate-watcher.sh — Monitor an interface and update DNS records

USAGE:
    dynupdate-watcher.sh [OPTIONS]

OPTIONS:
    -i, --interface IFACE     Network interface to monitor (required)
    -n, --name FQDN           DNS record name with trailing dot (required)
    -u, --url URL             dynupdate API URL (default: http://localhost:8080)
    -t, --token TOKEN         Bearer token for authentication (required)
    -I, --interval SECONDS    Poll interval in seconds (default: 30)
    -T, --ttl SECONDS         TTL for DNS records (default: 300)
    -6, --ipv6                Monitor IPv6 instead of IPv4 (AAAA record)
    -v, --verbose             Enable debug logging
    -h, --help                Show this help
    -V, --version             Show version

EXAMPLES:
    # Watch eth0 and update an A record
    dynupdate-watcher.sh \
        -i eth0 \
        -n myhost.example.org. \
        -t super-secret-token

    # Watch Tailscale interface for IPv4 changes
    dynupdate-watcher.sh \
        -i tailscale0 \
        -n myhost.ts.example.org. \
        -u http://localhost:8080 \
        -t my-token \
        -I 15

    # Watch Tailscale with the utun interface (macOS)
    # First find the interface: ip route get 100.64.0.0 | grep -o 'utun[0-9]*'
    dynupdate-watcher.sh \
        -i utun7 \
        -n macbook.ts.example.org. \
        -t my-token \
        -6

    # Using DYNUPDATE_TOKEN env var (avoids token in process list)
    export DYNUPDATE_TOKEN="my-secret"
    dynupdate-watcher.sh -i tailscale0 -n myhost.ts.example.org.

TAILSCALE INTEGRATION:
    Tailscale assigns a stable IP (100.x.y.z for IPv4, fd7a:... for IPv6)
    to each device. By pointing this script at the Tailscale interface,
    you can maintain DNS records that always resolve to the device's
    Tailscale IP, making it reachable by name within your tailnet.

    On Linux, the interface is typically "tailscale0".
    On macOS, it is a "utunN" device; find it with:
        tailscale status --json | jq -r '.TailscaleIPs[0]'

    Alternatively, use --interface=tailscale and the script will
    auto-detect the Tailscale IP via the "tailscale ip" command.

USAGE
}

# ── Argument Parsing ─────────────────────────────────────────────────────────
parse_arguments() {
    while [[ $# -gt 0 ]]; do
        case "${1}" in
            -h|--help)      help; exit 0 ;;
            -V|--version)   echo "dynupdate-watcher ${VERSION}"; exit 0 ;;
            -v|--verbose)   VERBOSE=true; shift ;;
            -6|--ipv6)      RECORD_TYPE="AAAA"; shift ;;
            -i|--interface)
                [[ -z "${2:-}" ]] && die "--interface requires an argument"
                INTERFACE="${2}"; shift 2
                ;;
            -n|--name)
                [[ -z "${2:-}" ]] && die "--name requires an argument"
                RECORD_NAME="${2}"; shift 2
                ;;
            -u|--url)
                [[ -z "${2:-}" ]] && die "--url requires an argument"
                API_URL="${2}"; shift 2
                ;;
            -t|--token)
                [[ -z "${2:-}" ]] && die "--token requires an argument"
                API_TOKEN="${2}"; shift 2
                ;;
            -I|--interval)
                [[ -z "${2:-}" ]] && die "--interval requires an argument"
                INTERVAL="${2}"; shift 2
                ;;
            -T|--ttl)
                [[ -z "${2:-}" ]] && die "--ttl requires an argument"
                TTL="${2}"; shift 2
                ;;
            -*)
                die "Unknown option: ${1}. Use --help for usage."
                ;;
            *)
                die "Unexpected argument: ${1}. Use --help for usage."
                ;;
        esac
    done

    # Allow token from environment
    if [[ -z "${API_TOKEN}" ]]; then
        API_TOKEN="${DYNUPDATE_TOKEN:-}"
    fi
}

# ── Validation ───────────────────────────────────────────────────────────────
validate_config() {
    [[ -z "${INTERFACE}" ]] && die "--interface is required"
    [[ -z "${RECORD_NAME}" ]] && die "--name is required"
    [[ -z "${API_TOKEN}" ]] && die "--token or DYNUPDATE_TOKEN env var is required"

    if [[ "${RECORD_NAME}" != *. ]]; then
        die "Record name must end with a trailing dot: ${RECORD_NAME}"
    fi

    if ! [[ "${INTERVAL}" =~ ^[0-9]+$ ]] || (( INTERVAL < 1 )); then
        die "Interval must be a positive integer: ${INTERVAL}"
    fi

    if ! [[ "${TTL}" =~ ^[0-9]+$ ]] || (( TTL < 60 )) || (( TTL > 86400 )); then
        die "TTL must be between 60 and 86400: ${TTL}"
    fi

    # Verify curl is available
    command -v curl >/dev/null 2>&1 || die "curl is required but not installed"
}

# ── IP Detection ─────────────────────────────────────────────────────────────
get_ip() {
    local ip=""

    # Special case: "tailscale" pseudo-interface uses the tailscale CLI
    if [[ "${INTERFACE}" == "tailscale" ]]; then
        if ! command -v tailscale >/dev/null 2>&1; then
            log_error "tailscale CLI not found; use a real interface name instead"
            return 1
        fi
        if [[ "${RECORD_TYPE}" == "AAAA" ]]; then
            ip=$(tailscale ip -6 2>/dev/null) || true
        else
            ip=$(tailscale ip -4 2>/dev/null) || true
        fi
        echo "${ip}"
        return 0
    fi

    # Standard interface: parse from ip/ifconfig
    if command -v ip >/dev/null 2>&1; then
        if [[ "${RECORD_TYPE}" == "AAAA" ]]; then
            ip=$(ip -6 addr show dev "${INTERFACE}" scope global 2>/dev/null \
                | grep -oP 'inet6\s+\K[0-9a-f:]+' \
                | head -1) || true
        else
            ip=$(ip -4 addr show dev "${INTERFACE}" 2>/dev/null \
                | grep -oP 'inet\s+\K[0-9.]+' \
                | head -1) || true
        fi
    elif command -v ifconfig >/dev/null 2>&1; then
        if [[ "${RECORD_TYPE}" == "AAAA" ]]; then
            ip=$(ifconfig "${INTERFACE}" 2>/dev/null \
                | grep 'inet6 ' \
                | grep -v 'fe80' \
                | awk '{print $2}' \
                | head -1) || true
        else
            ip=$(ifconfig "${INTERFACE}" 2>/dev/null \
                | grep 'inet ' \
                | awk '{print $2}' \
                | head -1) || true
        fi
    else
        die "Neither 'ip' nor 'ifconfig' found"
    fi

    echo "${ip}"
}

# ── DNS Update ───────────────────────────────────────────────────────────────
update_dns() {
    local -r ip="${1}"
    local payload
    payload=$(printf '{"name":"%s","type":"%s","ttl":%d,"value":"%s"}' \
        "${RECORD_NAME}" "${RECORD_TYPE}" "${TTL}" "${ip}")

    log_debug "POST ${API_URL}/api/v1/records payload=${payload}"

    local http_code
    http_code=$(curl -s -o /dev/null -w "%{http_code}" \
        -X POST \
        -H "Authorization: Bearer ${API_TOKEN}" \
        -H "Content-Type: application/json" \
        -d "${payload}" \
        "${API_URL}/api/v1/records") || true

    if [[ "${http_code}" == "201" || "${http_code}" == "200" ]]; then
        log_info "Updated ${RECORD_NAME} ${RECORD_TYPE} -> ${ip} (HTTP ${http_code})"
        return 0
    else
        log_error "Failed to update DNS: HTTP ${http_code}"
        return 1
    fi
}

# ── Main Loop ────────────────────────────────────────────────────────────────
run() {
    log_info "Watching interface=${INTERFACE} record=${RECORD_NAME} type=${RECORD_TYPE}"
    log_info "API=${API_URL} interval=${INTERVAL}s ttl=${TTL}s"

    while true; do
        local current_ip
        current_ip=$(get_ip)

        if [[ -z "${current_ip}" ]]; then
            log_warn "No ${RECORD_TYPE} address found on ${INTERFACE}"
        elif [[ "${current_ip}" != "${LAST_IP}" ]]; then
            log_info "IP changed: '${LAST_IP}' -> '${current_ip}'"
            if update_dns "${current_ip}"; then
                LAST_IP="${current_ip}"
            fi
        else
            log_debug "No change: ${current_ip}"
        fi

        sleep "${INTERVAL}"
    done
}

# ── Entry Point ──────────────────────────────────────────────────────────────
main() {
    parse_arguments "${@}"
    validate_config
    run
}

main "${@}"
