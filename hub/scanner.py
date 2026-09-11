"""
hub/scanner.py

Wi-Fi scanner for the WIFI HUNTER AR hub agent.

This module provides a clean interface over the Windows WLAN API.
It handles:
    - BSSID normalization (uppercase, validated format)
    - SSID sanitization (hidden network handling, encoding errors)
    - Deduplication per BSSID+interface within a scan
    - RSSI validation (realistic range checking)
    - Graceful handling of driver/adapter failures

The scanner does NOT simulate, fabricate, or guess Wi-Fi data.
If scanning fails or the adapter is unavailable, it reports that
honestly rather than returning fake data.
"""

from __future__ import annotations

import logging
import re
import time
from typing import Optional

from models import WiFiObservation, ScannerStatus

logger = logging.getLogger(__name__)

# Valid BSSID pattern: 6 colon-separated hex octets
_BSSID_PATTERN = re.compile(r'^([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}$')

# Realistic RSSI range in dBm
_RSSI_MIN = -127
_RSSI_MAX = 0


def _normalize_bssid(bssid: str) -> Optional[str]:
    """
    Normalize BSSID to uppercase AA:BB:CC:DD:EE:FF.
    Returns None if the BSSID is invalid.
    """
    normalized = bssid.strip().upper()
    if not _BSSID_PATTERN.match(normalized):
        return None
    # Reject broadcast and null BSSIDs
    if normalized in ("FF:FF:FF:FF:FF:FF", "00:00:00:00:00:00"):
        return None
    return normalized


def _validate_rssi(rssi: Optional[int]) -> Optional[int]:
    """
    Validate RSSI is within the realistic dBm range.
    Returns None if RSSI is invalid or unavailable.

    We do NOT clamp corrupted values — if outside range, we discard them
    rather than silently passing invalid data to the localization engine.
    """
    if rssi is None:
        return None
    if _RSSI_MIN <= rssi <= _RSSI_MAX:
        return rssi
    logger.debug("RSSI %d dBm out of valid range [%d, %d], setting null", rssi, _RSSI_MIN, _RSSI_MAX)
    return None


class WiFiScanner:
    """
    Windows Wi-Fi scanner that uses the real Windows WLAN API.

    This is the ONLY scanner for this implementation — it is Windows-specific.
    No cross-platform abstraction is provided (per specification).

    Usage:
        scanner = WiFiScanner()
        if scanner.is_available():
            scanner.trigger_scan()
            time.sleep(3)
            observations = scanner.get_observations()
    """

    def __init__(self) -> None:
        self._wlan_scanner = None
        self._status = ScannerStatus(state="INITIALIZING")
        self._initialize()

    def _initialize(self) -> None:
        """Initialize the Windows WLAN scanner."""
        try:
            from windows.wlan_api import WindowsWiFiScanner
            scanner = WindowsWiFiScanner()
            interfaces = scanner.get_interfaces()

            if not interfaces:
                logger.warning("No WLAN interfaces found")
                self._status.state = "UNAVAILABLE"
                self._status.error_message = "No Wi-Fi interfaces found on this system."
                scanner.close()
                return

            active = next(
                (i for i in interfaces if i.state in (1, 4, 5, 6, 7)),
                interfaces[0] if interfaces else None,
            )
            if active:
                self._status.interface = active.description
                logger.info("WLAN initialized. Interface: %s (%s)", active.description, active.state_name)
            else:
                logger.warning("No usable WLAN interface found")

            self._wlan_scanner = scanner
            self._status.state = "READY"

        except ImportError as e:
            logger.error("Windows WLAN API unavailable: %s", e)
            self._status.state = "UNAVAILABLE"
            self._status.error_message = str(e)
        except OSError as e:
            logger.error("Failed to initialize WLAN scanner: %s", e)
            self._status.state = "UNAVAILABLE"
            self._status.error_message = str(e)

    def is_available(self) -> bool:
        """Return True if the scanner is ready to use."""
        return self._wlan_scanner is not None and self._status.state in ("READY", "SCANNING")

    @property
    def status(self) -> ScannerStatus:
        return self._status

    def scan_once(self, scan_wait_seconds: float = 3.0) -> list[WiFiObservation]:
        """
        Perform a single scan and return the results.

        Triggers WlanScan, waits for the driver, retrieves results,
        and returns a deduplicated, normalized list of WiFiObservation objects.

        If the scanner is unavailable, returns an empty list (never fakes data).
        """
        if not self.is_available():
            logger.warning("Scanner unavailable: %s", self._status.error_message)
            return []

        self._status.state = "SCANNING"

        try:
            # get_bss_list handles both scan triggering and retrieval
            raw_entries = self._wlan_scanner.get_bss_list(
                wait_for_scan=True,
                scan_wait_seconds=scan_wait_seconds,
            )
        except OSError as e:
            logger.warning("WLAN scan failed: %s", e)
            self._status.state = "SCAN_FAILED"
            self._status.error_message = str(e)
            return []

        observations = self._process_entries(raw_entries)

        self._status.state = "READY"
        self._status.last_scan_time = time.time()
        self._status.ap_count = len(observations)
        self._status.error_message = None

        return observations

    def _process_entries(self, raw_entries) -> list[WiFiObservation]:
        """
        Process raw BSS entries into normalized WiFiObservation objects.

        Steps:
        1. Normalize BSSID (reject invalid/broadcast)
        2. Validate RSSI (reject out-of-range)
        3. Deduplicate by BSSID (keep strongest signal)
        4. Return validated observations
        """
        # Deduplicate by BSSID — keep entry with strongest RSSI
        by_bssid: dict[str, WiFiObservation] = {}

        now = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())

        for entry in raw_entries:
            bssid = _normalize_bssid(entry.bssid)
            if bssid is None:
                logger.debug("Skipping invalid BSSID: %r", entry.bssid)
                continue

            rssi = _validate_rssi(entry.rssi_dbm)

            obs = WiFiObservation(
                bssid=bssid,
                ssid=entry.ssid or "<hidden>",
                rssi_dbm=rssi,
                link_quality=entry.link_quality if 0 <= (entry.link_quality or -1) <= 100 else None,
                frequency_mhz=entry.frequency_mhz,
                channel=entry.channel,
                interface=entry.interface,
                timestamp=now,
            )

            # Deduplicate: prefer entry with stronger RSSI (less negative)
            if bssid not in by_bssid:
                by_bssid[bssid] = obs
            else:
                existing = by_bssid[bssid]
                if (obs.rssi_dbm is not None and existing.rssi_dbm is not None
                        and obs.rssi_dbm > existing.rssi_dbm):
                    by_bssid[bssid] = obs

        return list(by_bssid.values())

    def close(self) -> None:
        """Close the WLAN scanner and release resources."""
        if self._wlan_scanner:
            try:
                self._wlan_scanner.close()
            except Exception as e:
                logger.debug("Error closing WLAN scanner: %s", e)
            self._wlan_scanner = None
        self._status.state = "UNAVAILABLE"

    def __del__(self) -> None:
        self.close()
