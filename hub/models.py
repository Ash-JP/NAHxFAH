"""
hub/models.py

Data models for the WIFI HUNTER AR hub agent.
Uses dataclasses and Pydantic for type safety and serialization.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Optional
import datetime


@dataclass
class WiFiObservation:
    """
    A single Wi-Fi AP observation from a real Windows WLAN scan.

    rssi_dbm is the ACTUAL value from Windows WLAN API (lRssi).
    It is None if the driver did not provide it.
    """
    bssid: str                    # Normalized uppercase AA:BB:CC:DD:EE:FF
    ssid: str                     # Decoded SSID or "<hidden>"
    rssi_dbm: Optional[int]       # Actual RSSI in dBm. None if unavailable.
    link_quality: Optional[int]   # 0–100 from Windows. Separate from RSSI.
    frequency_mhz: Optional[int]  # Channel center frequency in MHz
    channel: Optional[int]        # Channel number (derived from frequency)
    interface: str                # WLAN interface description
    timestamp: str                # ISO 8601


@dataclass
class HubPosition:
    """Hub physical position in the local coordinate system."""
    coordinate_system: str = "local"
    x: float = 0.0  # East (metres)
    y: float = 0.0  # North (metres)
    z: float = 1.0  # Up (metres)

    def is_configured(self) -> bool:
        """Return True if position has been explicitly set by the user."""
        return not (self.x == 0.0 and self.y == 0.0 and self.z == 0.0)


@dataclass
class HubConfig:
    """Hub runtime configuration loaded from config.json."""
    server_url: str = "ws://127.0.0.1:8000/ws/hub"
    api_key: str = "change_me"
    scan_interval_seconds: float = 5.0
    heartbeat_interval_seconds: float = 5.0
    coordinate_system: str = "local"
    position: HubPosition = field(default_factory=HubPosition)

    @classmethod
    def from_dict(cls, data: dict) -> "HubConfig":
        pos_data = data.get("position", {})
        position = HubPosition(
            coordinate_system=data.get("coordinate_system", "local"),
            x=float(pos_data.get("x", 0.0)),
            y=float(pos_data.get("y", 0.0)),
            z=float(pos_data.get("z", 1.0)),
        )
        return cls(
            server_url=data.get("server_url", cls.server_url),
            api_key=data.get("api_key", cls.api_key),
            scan_interval_seconds=float(data.get("scan_interval_seconds", 5.0)),
            heartbeat_interval_seconds=float(data.get("heartbeat_interval_seconds", 5.0)),
            coordinate_system=data.get("coordinate_system", "local"),
            position=position,
        )

    def to_dict(self) -> dict:
        return {
            "server_url": self.server_url,
            "api_key": self.api_key,
            "scan_interval_seconds": self.scan_interval_seconds,
            "heartbeat_interval_seconds": self.heartbeat_interval_seconds,
            "coordinate_system": self.coordinate_system,
            "position": {
                "x": self.position.x,
                "y": self.position.y,
                "z": self.position.z,
            },
        }


@dataclass
class HubIdentity:
    """Persistent hub identity."""
    hub_id: str
    created_at: str


@dataclass
class ObservationEntry:
    """A single AP observation for the WebSocket message payload."""
    bssid: str
    ssid: str
    rssi_dbm: Optional[int]
    link_quality: Optional[int]
    frequency_mhz: Optional[int]
    channel: Optional[int]

    def to_dict(self) -> dict:
        return {
            "bssid": self.bssid,
            "ssid": self.ssid,
            "rssi_dbm": self.rssi_dbm,
            "link_quality": self.link_quality,
            "frequency_mhz": self.frequency_mhz,
            "channel": self.channel,
        }


@dataclass
class ScannerStatus:
    """Scanner operational status."""
    state: str = "INITIALIZING"  # INITIALIZING, READY, SCANNING, SCAN_FAILED, UNAVAILABLE
    interface: Optional[str] = None
    last_scan_time: Optional[float] = None
    ap_count: int = 0
    error_message: Optional[str] = None

    def seconds_since_last_scan(self) -> Optional[float]:
        import time
        if self.last_scan_time is None:
            return None
        return time.time() - self.last_scan_time


@dataclass
class ConnectionStatus:
    """Server connection status."""
    state: str = "CONNECTING"  # CONNECTING, CONNECTED, DISCONNECTED, RECONNECTING
    observations_sent: int = 0
    last_upload_time: Optional[float] = None
    reconnect_count: int = 0

    def seconds_since_last_upload(self) -> Optional[float]:
        import time
        if self.last_upload_time is None:
            return None
        return time.time() - self.last_upload_time
