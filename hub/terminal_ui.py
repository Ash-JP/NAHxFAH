"""
hub/terminal_ui.py

Rich terminal dashboard for the WIFI HUNTER AR hub agent.

Updates in-place without flooding the terminal.
Degrades gracefully if 'rich' is not installed.

Layout:
    ════════════════════════════════════════
              WIFI HUNTER AR
               WINDOWS HUB
    ════════════════════════════════════════

    Hub ID:     HUB-A7F32C
    Server:     CONNECTED
    Position:   X: 4.20  Y: 8.10  Z: 1.00
    Scanner:    ACTIVE
    Interface:  Wi-Fi
    APs:        17
    Last scan:  1.2 sec ago

    ────────────────────────────────────────
    TOP SIGNALS
    ────────────────────────────────────────
    CEAL-WIFI        -42 dBm    CH 36
    Student-WiFi     -51 dBm    CH 6
    Guest-WiFi       -61 dBm    CH 11

    ────────────────────────────────────────
    Upload:     CONNECTED
    Sent:       342 observations
    Last:       0.7 sec ago
    ════════════════════════════════════════
"""

from __future__ import annotations

import time
from typing import Optional

from models import ScannerStatus, ConnectionStatus, HubConfig, HubIdentity, WiFiObservation

try:
    from rich.console import Console
    from rich.live import Live
    from rich.table import Table
    from rich.panel import Panel
    from rich.text import Text
    from rich import box
    _RICH_AVAILABLE = True
except ImportError:
    _RICH_AVAILABLE = False


class TerminalUI:
    """
    In-place updating terminal dashboard using 'rich' if available.
    Falls back to simple stdout printing if rich is not installed.
    """

    def __init__(
        self,
        identity: HubIdentity,
        config: HubConfig,
    ) -> None:
        self._identity = identity
        self._config = config
        self._scanner_status = ScannerStatus()
        self._conn_status = ConnectionStatus()
        self._top_aps: list[WiFiObservation] = []
        self._live: Optional["Live"] = None
        self._console: Optional["Console"] = None

    def update(
        self,
        scanner_status: ScannerStatus,
        conn_status: ConnectionStatus,
        top_aps: list[WiFiObservation],
    ) -> None:
        """Update the dashboard with fresh data."""
        self._scanner_status = scanner_status
        self._conn_status = conn_status
        self._top_aps = sorted(
            top_aps,
            key=lambda a: a.rssi_dbm if a.rssi_dbm is not None else -999,
            reverse=True,
        )[:10]

        if _RICH_AVAILABLE and self._live:
            self._live.update(self._build_rich_layout())
        else:
            self._print_simple()

    def start(self) -> None:
        """Start the live display."""
        if _RICH_AVAILABLE:
            self._console = Console()
            self._live = Live(
                self._build_rich_layout(),
                console=self._console,
                refresh_per_second=2,
                screen=False,
            )
            self._live.start()
        else:
            self._print_header()

    def stop(self) -> None:
        """Stop the live display."""
        if self._live:
            self._live.stop()

    def _build_rich_layout(self):
        """Build the rich panel layout."""
        identity = self._identity
        config = self._config
        scan = self._scanner_status
        conn = self._conn_status
        pos = config.position

        # Status colors
        server_color = "green" if conn.state == "CONNECTED" else "red"
        scanner_color = "green" if scan.state in ("READY", "SCANNING") else "red"
        scanner_state = "ACTIVE" if scan.state in ("READY", "SCANNING") else scan.state

        # Build main info text
        last_scan_str = "N/A"
        secs = scan.seconds_since_last_scan()
        if secs is not None:
            last_scan_str = f"{secs:.1f} sec ago"

        last_upload_str = "N/A"
        usecs = conn.seconds_since_last_upload()
        if usecs is not None:
            last_upload_str = f"{usecs:.1f} sec ago"

        text = Text()
        text.append(f"\nHub ID:      ", style="bold")
        text.append(f"{identity.hub_id}\n", style="cyan bold")
        text.append(f"Server:      ", style="bold")
        text.append(f"{conn.state}\n", style=server_color + " bold")
        text.append(f"Position:    ", style="bold")
        text.append(f"X:{pos.x:.2f}  Y:{pos.y:.2f}  Z:{pos.z:.2f}\n")
        text.append(f"Scanner:     ", style="bold")
        text.append(f"{scanner_state}\n", style=scanner_color)
        text.append(f"Interface:   ", style="bold")
        text.append(f"{scan.interface or 'N/A'}\n")
        text.append(f"APs:         ", style="bold")
        text.append(f"{scan.ap_count}\n", style="yellow")
        text.append(f"Last scan:   ", style="bold")
        text.append(f"{last_scan_str}\n")

        # AP table
        table = Table(box=box.SIMPLE, show_header=True, header_style="bold magenta")
        table.add_column("SSID", style="cyan", max_width=20)
        table.add_column("BSSID", style="dim")
        table.add_column("RSSI", justify="right", style="green")
        table.add_column("Freq", justify="right")
        table.add_column("CH", justify="right")

        for ap in self._top_aps[:8]:
            rssi_str = f"{ap.rssi_dbm} dBm" if ap.rssi_dbm is not None else "N/A"
            freq_str = f"{ap.frequency_mhz}" if ap.frequency_mhz else "N/A"
            ch_str = str(ap.channel) if ap.channel else "N/A"
            table.add_row(
                ap.ssid[:20],
                ap.bssid,
                rssi_str,
                freq_str,
                ch_str,
            )

        # Upload stats
        upload_text = Text()
        upload_text.append(f"\nUpload:      ", style="bold")
        upload_text.append(f"{conn.state}\n", style=server_color)
        upload_text.append(f"Sent:        ", style="bold")
        upload_text.append(f"{conn.observations_sent} observations\n")
        upload_text.append(f"Last upload: ", style="bold")
        upload_text.append(f"{last_upload_str}\n")

        from rich.columns import Columns
        from rich.layout import Layout

        combined = Text()
        combined.append_text(text)
        if self._top_aps:
            combined.append("\nTOP SIGNALS\n", style="bold underline")

        panel = Panel(
            combined,
            title="[bold]WIFI HUNTER AR — WINDOWS HUB[/bold]",
            subtitle=f"[dim]{identity.hub_id}[/dim]",
            border_style="blue",
        )
        return panel

    def _print_simple(self) -> None:
        """Simple fallback output when rich is not available."""
        import os
        os.system("cls" if os.name == "nt" else "clear")
        self._print_header()
        scan = self._scanner_status
        conn = self._conn_status
        pos = self._config.position

        print(f"Hub ID:    {self._identity.hub_id}")
        print(f"Server:    {conn.state}")
        print(f"Position:  X:{pos.x:.2f}  Y:{pos.y:.2f}  Z:{pos.z:.2f}")
        print(f"Scanner:   {scan.state}")
        print(f"Interface: {scan.interface or 'N/A'}")
        print(f"APs:       {scan.ap_count}")
        secs = scan.seconds_since_last_scan()
        print(f"Last scan: {f'{secs:.1f}s ago' if secs else 'N/A'}")
        print()
        if self._top_aps:
            print("TOP SIGNALS")
            print("-" * 60)
            print(f"{'SSID':<20} {'BSSID':<18} {'RSSI':>8}  {'CH':>4}")
            for ap in self._top_aps[:8]:
                rssi_str = f"{ap.rssi_dbm} dBm" if ap.rssi_dbm is not None else " N/A"
                ch_str = str(ap.channel) if ap.channel else "  -"
                print(f"{ap.ssid[:20]:<20} {ap.bssid:<18} {rssi_str:>8}  {ch_str:>4}")
        print()
        print(f"Sent: {conn.observations_sent} observations")
        usecs = conn.seconds_since_last_upload()
        print(f"Last upload: {f'{usecs:.1f}s ago' if usecs else 'N/A'}")

    def _print_header(self) -> None:
        print("=" * 42)
        print("         WIFI HUNTER AR")
        print("          WINDOWS HUB")
        print("=" * 42)
        print()
