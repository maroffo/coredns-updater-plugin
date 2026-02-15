#!/usr/bin/env python3
# ABOUTME: Watches a network interface for IP changes and updates dynupdate DNS records.
# ABOUTME: Supports any interface including Tailscale; zero external dependencies.

"""
dynupdate-watcher: monitor a network interface and sync DNS records.

Usage:
    python dynupdate_watcher.py \
        --interface tailscale0 \
        --name myhost.ts.example.org. \
        --token my-secret

    # Or with environment variable:
    DYNUPDATE_TOKEN=my-secret python dynupdate_watcher.py \
        --interface tailscale0 \
        --name myhost.ts.example.org.

Tailscale integration:
    Use --interface=tailscale to auto-detect the Tailscale IP via CLI,
    or specify the real interface name (tailscale0 on Linux, utunN on macOS).
"""

from __future__ import annotations

import argparse
import ipaddress
import json
import logging
import os
import re
import shutil
import subprocess
import sys
import time
import urllib.error
import urllib.request

log = logging.getLogger("dynupdate-watcher")


def get_ip_from_interface(interface: str, ipv6: bool) -> str | None:
    """Extract the IP address assigned to a network interface."""

    # Special pseudo-interface: use the tailscale CLI
    if interface == "tailscale":
        return _get_tailscale_ip(ipv6)

    # Try 'ip' first (Linux), fall back to 'ifconfig' (macOS/BSD)
    if shutil.which("ip"):
        return _get_ip_via_iproute2(interface, ipv6)
    if shutil.which("ifconfig"):
        return _get_ip_via_ifconfig(interface, ipv6)

    log.error("Neither 'ip' nor 'ifconfig' found in PATH")
    return None


def _get_tailscale_ip(ipv6: bool) -> str | None:
    """Get the Tailscale IP using the tailscale CLI."""
    if not shutil.which("tailscale"):
        log.error("tailscale CLI not found; use a real interface name instead")
        return None

    flag = "-6" if ipv6 else "-4"
    result = subprocess.run(
        ["tailscale", "ip", flag],
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        log.warning("tailscale ip %s failed: %s", flag, result.stderr.strip())
        return None

    ip = result.stdout.strip().split("\n")[0]
    return ip if ip else None


def _get_ip_via_iproute2(interface: str, ipv6: bool) -> str | None:
    """Parse IP from `ip addr show`."""
    family = "-6" if ipv6 else "-4"
    result = subprocess.run(
        ["ip", family, "addr", "show", "dev", interface],
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        return None

    pattern = r"inet6\s+([0-9a-f:]+)" if ipv6 else r"inet\s+([0-9.]+)"
    for line in result.stdout.splitlines():
        # Skip link-local for IPv6
        if ipv6 and "scope link" in line:
            continue
        match = re.search(pattern, line)
        if match:
            return match.group(1)
    return None


def _get_ip_via_ifconfig(interface: str, ipv6: bool) -> str | None:
    """Parse IP from `ifconfig`."""
    result = subprocess.run(
        ["ifconfig", interface],
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        return None

    for line in result.stdout.splitlines():
        if ipv6:
            if "inet6" in line and "fe80" not in line:
                parts = line.split()
                idx = parts.index("inet6") + 1 if "inet6" in parts else -1
                if 0 < idx < len(parts):
                    addr = parts[idx].split("%")[0]  # strip zone ID
                    return addr
        else:
            if "inet " in line:
                parts = line.split()
                idx = parts.index("inet") + 1 if "inet" in parts else -1
                if 0 < idx < len(parts):
                    return parts[idx]
    return None


def validate_ip(ip: str, ipv6: bool) -> bool:
    """Verify the string is a valid IP of the expected family."""
    try:
        addr = ipaddress.ip_address(ip)
    except ValueError:
        return False
    if ipv6:
        return addr.version == 6
    return addr.version == 4


def update_dns(
    api_url: str,
    token: str,
    name: str,
    record_type: str,
    ttl: int,
    value: str,
) -> bool:
    """Upsert a DNS record via the dynupdate REST API."""
    payload = json.dumps({
        "name": name,
        "type": record_type,
        "ttl": ttl,
        "value": value,
    }).encode()

    url = f"{api_url.rstrip('/')}/api/v1/records"
    req = urllib.request.Request(
        url,
        data=payload,
        method="POST",
        headers={
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json",
        },
    )

    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            status = resp.status
    except urllib.error.HTTPError as exc:
        log.error("API returned HTTP %d: %s", exc.code, exc.read().decode())
        return False
    except urllib.error.URLError as exc:
        log.error("API connection failed: %s", exc.reason)
        return False

    if status in (200, 201):
        log.info("Updated %s %s -> %s (HTTP %d)", name, record_type, value, status)
        return True

    log.error("Unexpected HTTP status: %d", status)
    return False


def watch(args: argparse.Namespace) -> None:
    """Main watch loop: poll interface, detect changes, update DNS."""
    record_type = "AAAA" if args.ipv6 else "A"
    last_ip = ""

    log.info(
        "Watching interface=%s record=%s type=%s interval=%ds",
        args.interface,
        args.name,
        record_type,
        args.interval,
    )
    log.info("API=%s ttl=%ds", args.url, args.ttl)

    while True:
        current_ip = get_ip_from_interface(args.interface, args.ipv6)

        if current_ip is None:
            log.warning("No %s address found on %s", record_type, args.interface)
        elif not validate_ip(current_ip, args.ipv6):
            log.warning("Invalid IP detected: %s", current_ip)
        elif current_ip != last_ip:
            log.info("IP changed: '%s' -> '%s'", last_ip, current_ip)
            if update_dns(
                api_url=args.url,
                token=args.token,
                name=args.name,
                record_type=record_type,
                ttl=args.ttl,
                value=current_ip,
            ):
                last_ip = current_ip
        else:
            log.debug("No change: %s", current_ip)

        time.sleep(args.interval)


def build_parser() -> argparse.ArgumentParser:
    """Build the CLI argument parser."""
    parser = argparse.ArgumentParser(
        prog="dynupdate-watcher",
        description="Watch a network interface and update DNS records via dynupdate.",
        epilog="""
examples:
  # Monitor eth0 and update an A record
  %(prog)s -i eth0 -n myhost.example.org. -t secret

  # Monitor Tailscale auto-detected IP
  %(prog)s -i tailscale -n myhost.ts.example.org. -t secret

  # Monitor the Tailscale interface on Linux
  %(prog)s -i tailscale0 -n myhost.ts.example.org. -t secret

  # Monitor IPv6 on macOS Tailscale (utun interface)
  %(prog)s -i utun7 -n myhost.ts.example.org. -t secret --ipv6

  # Use env var for token (avoids token in process list)
  DYNUPDATE_TOKEN=secret %(prog)s -i tailscale0 -n myhost.ts.example.org.
        """,
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument(
        "-i", "--interface",
        required=True,
        help='Network interface to monitor (e.g. eth0, tailscale0, utun7, or "tailscale" for CLI auto-detect)',
    )
    parser.add_argument(
        "-n", "--name",
        required=True,
        help="DNS record name with trailing dot (e.g. myhost.example.org.)",
    )
    parser.add_argument(
        "-u", "--url",
        default="http://localhost:8080",
        help="dynupdate API URL (default: http://localhost:8080)",
    )
    parser.add_argument(
        "-t", "--token",
        default=os.environ.get("DYNUPDATE_TOKEN", ""),
        help="Bearer token for authentication (or set DYNUPDATE_TOKEN env var)",
    )
    parser.add_argument(
        "-I", "--interval",
        type=int,
        default=30,
        help="Poll interval in seconds (default: 30)",
    )
    parser.add_argument(
        "-T", "--ttl",
        type=int,
        default=300,
        help="TTL for DNS records (default: 300)",
    )
    parser.add_argument(
        "--ipv6",
        action="store_true",
        help="Monitor IPv6 instead of IPv4 (AAAA record)",
    )
    parser.add_argument(
        "-v", "--verbose",
        action="store_true",
        help="Enable debug logging",
    )
    return parser


def main() -> None:
    """Entry point: parse arguments, validate, and start the watch loop."""
    parser = build_parser()
    args = parser.parse_args()

    logging.basicConfig(
        level=logging.DEBUG if args.verbose else logging.INFO,
        format="[%(levelname)-5s] %(asctime)s %(message)s",
        datefmt="%H:%M:%S",
    )

    if not args.token:
        parser.error("--token is required (or set DYNUPDATE_TOKEN env var)")

    if not args.name.endswith("."):
        parser.error(f"Record name must end with a trailing dot: {args.name}")

    if args.interval < 1:
        parser.error(f"Interval must be a positive integer: {args.interval}")

    if not 60 <= args.ttl <= 86400:
        parser.error(f"TTL must be between 60 and 86400: {args.ttl}")

    try:
        watch(args)
    except KeyboardInterrupt:
        log.info("Shutting down")
        sys.exit(0)


if __name__ == "__main__":
    main()
