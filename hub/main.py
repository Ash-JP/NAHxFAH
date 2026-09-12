"""
hub/main.py

WIFI HUNTER AR — Windows Hub Agent

Entry point for all hub operations.

Usage:
    python main.py                     # Start hub in full mode
    python main.py --scan-once         # Perform one real Wi-Fi scan and print results
    python main.py --configure-location # Interactive hub position setup
    python main.py --show-config       # Print current configuration
    python main.py --show-id           # Print persistent hub ID
    python main.py --test-server       # Test WebSocket connection to server
    python main.py --version           # Print version

Requirements:
    - Windows 10/11
    - Active Wi-Fi adapter (for scanning)
    - Python 3.12+
    - See requirements.txt for dependencies

IMPORTANT:
    This agent performs PASSIVE Wi-Fi observation only.
    It does NOT connect to discovered networks, capture packets,
    perform deauthentication, or conduct any form of network intrusion.
"""

from __future__ import annotations

import argparse
import asyncio
import json
import logging
import sys
import time

VERSION = "1.0.0"

logger = logging.getLogger(__name__)


def cmd_scan_once() -> None:
    """
    Perform a single real Wi-Fi scan and print the results.

    This command does NOT require a server connection.
    It demonstrates that the Windows WLAN API integration works.

    Output format:
        SSID             BSSID              RSSI     FREQ    CH
        CEAL-WIFI        AA:BB:CC:DD:EE:FF  -47 dBm  5180    36
    """
    from scanner import WiFiScanner

    print()
    print("Performing real Wi-Fi scan (Windows WLAN API)...")
    print("Please wait 3-4 seconds for the driver to complete the scan.")
    print()

    scanner = WiFiScanner()
    if not scanner.is_available():
        print(f"ERROR: {scanner.status.error_message}")
        print()
        print("Possible causes:")
        print("  - No Wi-Fi adapter installed")
        print("  - Wi-Fi adapter is disabled")
        print("  - Windows WLAN service is stopped")
        sys.exit(1)

    observations = scanner.scan_once(scan_wait_seconds=3.5)
    scanner.close()

    if not observations:
        print("WARNING: No access points found.")
        print("This may be normal if you are in an area with no Wi-Fi networks.")
        print("If unexpected, check that your Wi-Fi adapter is enabled.")
        return

    # Sort by RSSI (strongest first)
    observations.sort(key=lambda o: o.rssi_dbm if o.rssi_dbm is not None else -999, reverse=True)

    print(f"Found {len(observations)} access points:\n")
    header = f"{'SSID':<22} {'BSSID':<18} {'RSSI':>9}  {'FREQ (MHz)':>10}  {'CH':>4}  {'LQ':>4}"
    print(header)
    print("-" * len(header))

    for obs in observations:
        rssi_str  = f"{obs.rssi_dbm} dBm" if obs.rssi_dbm is not None else "  N/A"
        freq_str  = str(obs.frequency_mhz) if obs.frequency_mhz else "N/A"
        ch_str    = str(obs.channel) if obs.channel else "N/A"
        lq_str    = str(obs.link_quality) if obs.link_quality is not None else "N/A"
        ssid_disp = obs.ssid[:22]

        print(f"{ssid_disp:<22} {obs.bssid:<18} {rssi_str:>9}  {freq_str:>10}  {ch_str:>4}  {lq_str:>4}")

    print()
    print(f"Interface: {observations[0].interface if observations else 'N/A'}")
    print()
    print("NOTE: RSSI values are the actual lRssi from the Windows WLAN API (dBm).")
    print("      Link Quality (LQ) is the Windows 0-100 metric. They are independent.")


def cmd_configure_location() -> None:
    """Interactive location configuration."""
    from config import load_config
    from location import configure_location_interactive

    try:
        cfg = load_config()
    except ValueError as e:
        # If config.json doesn't exist, use defaults
        from models import HubConfig
        cfg = HubConfig()

    configure_location_interactive(cfg)


def cmd_show_config() -> None:
    """Print current configuration (redacts API key)."""
    from config import load_config

    try:
        cfg = load_config()
    except ValueError as e:
        print(f"ERROR: {e}")
        sys.exit(1)

    data = cfg.to_dict()
    # Redact API key
    if "api_key" in data:
        key = data["api_key"]
        data["api_key"] = f"{key[:4]}{'*' * (len(key) - 4)}" if len(key) > 4 else "****"

    print(json.dumps(data, indent=4))


def cmd_show_id() -> None:
    """Show the persistent hub ID."""
    from identity import load_or_create_identity, get_identity_path

    identity = load_or_create_identity()
    print(f"Hub ID:     {identity.hub_id}")
    print(f"Created:    {identity.created_at}")
    print(f"Stored at:  {get_identity_path()}")


def cmd_test_server() -> None:
    """Test connection to the configured server."""
    from config import load_config
    from identity import load_or_create_identity

    try:
        cfg = load_config()
    except ValueError as e:
        print(f"ERROR: {e}")
        sys.exit(1)

    identity = load_or_create_identity()

    print(f"Testing connection to: {cfg.server_url}")
    print(f"Hub ID: {identity.hub_id}")
    print()

    async def _test() -> None:
        import websockets
        import websockets.exceptions

        try:
            async def _connect_and_test() -> None:
                async with websockets.connect(cfg.server_url) as ws:
                    pos = cfg.position
                    msg = {
                        "type": "hub_register",
                        "hub_id": identity.hub_id,
                        "device_type": "windows_laptop",
                        "platform": "windows",
                        "version": VERSION,
                        "api_key": cfg.api_key,
                        "position": {
                            "coordinate_system": pos.coordinate_system,
                            "x": pos.x,
                            "y": pos.y,
                            "z": pos.z,
                        },
                    }
                    await ws.send(json.dumps(msg))

                    # Wait for response
                    raw = await asyncio.wait_for(ws.recv(), timeout=5)
                    resp = json.loads(raw)

                    if resp.get("type") == "hub_registered":
                        print(f"SUCCESS: Server responded with hub_registered")
                        print(f"  Server time: {resp.get('server_time')}")
                        print(f"  Status:      {resp.get('status')}")
                        print(f"  Position:    X={pos.x}, Y={pos.y}, Z={pos.z}")
                        return
                    elif resp.get("type") == "error":
                        print(f"FAILED: Server returned error")
                        print(f"  Code:    {resp.get('code')}")
                        print(f"  Message: {resp.get('message')}")
                        return
                    else:
                        print(f"UNEXPECTED: Server returned {resp.get('type')}")
                        return

            await asyncio.wait_for(_connect_and_test(), timeout=10)

        except asyncio.TimeoutError:
            print("FAILED: Connection timed out")
        except websockets.exceptions.WebSocketException as e:
            print(f"FAILED: WebSocket error: {e}")
        except OSError as e:
            print(f"FAILED: Cannot connect: {e}")
            print()
            print("Troubleshooting:")
            print("  1. Is the server running? (docker compose up --build)")
            print("  2. Is the server_url correct?")
            print("  3. Is port 8000 accessible?")
            print("  4. Check Windows Firewall")

    asyncio.run(_test())


def cmd_run_hub() -> None:
    """Run the full hub agent."""
    from config import load_config
    from identity import load_or_create_identity
    from scanner import WiFiScanner
    from websocket_client import HubWebSocketClient
    from terminal_ui import TerminalUI
    from models import ObservationEntry, ScannerStatus, ConnectionStatus

    try:
        cfg = load_config()
    except ValueError as e:
        print(f"ERROR: Invalid configuration: {e}")
        print("Run: python main.py --configure-location")
        sys.exit(1)

    identity = load_or_create_identity()

    print(f"WIFI HUNTER AR Hub Agent v{VERSION}")
    print(f"Hub ID: {identity.hub_id}")
    print(f"Server: {cfg.server_url}")
    print()

    # Initialize scanner
    scanner = WiFiScanner()
    if not scanner.is_available():
        print(f"WARNING: Wi-Fi scanner unavailable: {scanner.status.error_message}")
        print("Hub will connect to server but cannot send observations.")

    # Terminal UI
    ui = TerminalUI(identity, cfg)
    ui.start()

    # WebSocket client state (shared between async and sync code)
    conn_status = ConnectionStatus(state="CONNECTING")
    all_aps: list = []

    def on_ap_update(msg: dict) -> None:
        nonlocal all_aps
        ap = msg.get("ap", {})
        logger.debug("AP update: %s confidence=%.2f", ap.get("bssid"), ap.get("confidence", 0))

    def on_status_change(state: str) -> None:
        conn_status.state = state

    ws_client = HubWebSocketClient(
        config=cfg,
        identity=identity,
        on_ap_update=on_ap_update,
        on_status_change=on_status_change,
    )

    async def main_loop() -> None:
        """Main async loop: WebSocket + scan cycle run concurrently."""

        async def scan_loop() -> None:
            """Scan Wi-Fi at configured interval and upload observations."""
            while True:
                scan_start = time.monotonic()

                if scanner.is_available():
                    observations = scanner.scan_once(scan_wait_seconds=min(cfg.scan_interval_seconds * 0.7, 3.5))
                else:
                    observations = []

                # Convert to ObservationEntry for upload
                entries = [
                    ObservationEntry(
                        bssid=o.bssid,
                        ssid=o.ssid,
                        rssi_dbm=o.rssi_dbm,
                        link_quality=o.link_quality,
                        frequency_mhz=o.frequency_mhz,
                        channel=o.channel,
                    )
                    for o in observations
                ]

                if entries:
                    await ws_client.send_observations(entries)

                # Update UI
                ui.update(
                    scanner_status=scanner.status,
                    conn_status=ws_client.status,
                    top_aps=observations,
                )

                elapsed = time.monotonic() - scan_start
                sleep_time = max(0, cfg.scan_interval_seconds - elapsed)
                await asyncio.sleep(sleep_time)

        try:
            await asyncio.gather(
                ws_client.run(),
                scan_loop(),
            )
        except KeyboardInterrupt:
            pass
        finally:
            ws_client.stop()
            scanner.close()
            ui.stop()
            print("\nHub stopped.")

    try:
        asyncio.run(main_loop())
    except KeyboardInterrupt:
        pass


def main() -> None:
    """Parse arguments and dispatch to the appropriate command."""
    from logger import setup_logging
    setup_logging("INFO")

    parser = argparse.ArgumentParser(
        prog="python main.py",
        description="WIFI HUNTER AR — Windows Hub Agent",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  python main.py                     # Start hub (requires config.json)
  python main.py --scan-once         # Test Wi-Fi scanning
  python main.py --configure-location # Set hub position
  python main.py --test-server       # Test server connectivity
  python main.py --show-id           # Show hub identifier
        """,
    )
    parser.add_argument("--scan-once",          action="store_true", help="Perform one Wi-Fi scan and print results")
    parser.add_argument("--configure-location", action="store_true", help="Interactive hub position setup")
    parser.add_argument("--show-config",        action="store_true", help="Print current configuration")
    parser.add_argument("--show-id",            action="store_true", help="Show persistent hub ID")
    parser.add_argument("--test-server",        action="store_true", help="Test server WebSocket connection")
    parser.add_argument("--version",            action="store_true", help="Print version and exit")

    args = parser.parse_args()

    if args.version:
        print(f"WIFI HUNTER AR Hub v{VERSION}")
        return

    if args.scan_once:
        cmd_scan_once()
        return

    if args.configure_location:
        cmd_configure_location()
        return

    if args.show_config:
        cmd_show_config()
        return

    if args.show_id:
        cmd_show_id()
        return

    if args.test_server:
        cmd_test_server()
        return

    # Default: run the hub
    cmd_run_hub()


if __name__ == "__main__":
    main()
