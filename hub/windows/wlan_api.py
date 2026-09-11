"""
hub/windows/wlan_api.py

Windows Native WLAN API via ctypes.

This module implements a clean Python interface over the Windows WLAN API.
All ctypes internals are contained here. External code uses only
WindowsWiFiScanner.

Primary Windows APIs used:
    WlanOpenHandle         - Open WLAN API handle
    WlanEnumInterfaces     - Enumerate WLAN interfaces
    WlanScan               - Trigger active scan
    WlanGetNetworkBssList  - Retrieve BSS list
    WlanFreeMemory         - Free WLAN-allocated memory
    WlanCloseHandle        - Close WLAN API handle

Key structures:
    GUID                   - 16-byte interface identifier
    DOT11_SSID             - SSID with length field
    WLAN_INTERFACE_INFO    - Interface GUID + description + state
    WLAN_INTERFACE_INFO_LIST - Variable-length list of interfaces
    WLAN_BSS_ENTRY         - Full BSS information including RSSI, frequency
    WLAN_BSS_LIST          - Variable-length list of BSS entries

IMPORTANT:
    - lRssi in WLAN_BSS_ENTRY contains the ACTUAL RSSI in dBm.
      Never substitute link quality percentages for RSSI values.
    - SSID is raw bytes; decode as UTF-8 with errors='replace'.
    - WlanFreeMemory MUST be called on all allocated structures.
    - Structure alignment is critical — use pack=1 where required.

Windows WLAN documentation:
    https://docs.microsoft.com/en-us/windows/win32/nativewifi/
"""

from __future__ import annotations

import ctypes
import ctypes.wintypes
import logging
import time
from dataclasses import dataclass
from typing import Optional

logger = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Windows DLL
# ---------------------------------------------------------------------------
try:
    _wlan = ctypes.windll.wlanapi
except (AttributeError, OSError) as e:
    raise ImportError(
        "Windows WLAN API not available. This module requires Windows with wlanapi.dll."
    ) from e

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------
WLAN_CLIENT_VERSION_XP_SP2 = 1
WLAN_CLIENT_VERSION_VISTA   = 2
DOT11_SSID_MAX_LENGTH       = 32
DOT11_MAC_ADDRESS_LENGTH    = 6
WLAN_MAX_NAME_LENGTH        = 256
ERROR_SUCCESS               = 0

# DOT11 BSS types
DOT11_BSS_TYPE_INFRASTRUCTURE = 1
DOT11_BSS_TYPE_INDEPENDENT    = 2
DOT11_BSS_TYPE_ANY            = 3


# ---------------------------------------------------------------------------
# GUID structure (16 bytes, standard COM GUID layout)
# ---------------------------------------------------------------------------
class GUID(ctypes.Structure):
    _fields_ = [
        ("Data1", ctypes.c_ulong),
        ("Data2", ctypes.c_ushort),
        ("Data3", ctypes.c_ushort),
        ("Data4", ctypes.c_ubyte * 8),
    ]

    def __str__(self) -> str:
        return (
            f"{{{self.Data1:08X}-"
            f"{self.Data2:04X}-"
            f"{self.Data3:04X}-"
            f"{''.join(f'{b:02X}' for b in self.Data4[:2])}-"
            f"{''.join(f'{b:02X}' for b in self.Data4[2:])}}}"
        )


# ---------------------------------------------------------------------------
# DOT11_SSID
# ---------------------------------------------------------------------------
class DOT11_SSID(ctypes.Structure):
    _fields_ = [
        ("uSSIDLength", ctypes.c_ulong),
        ("ucSSID", ctypes.c_ubyte * DOT11_SSID_MAX_LENGTH),
    ]

    def decode(self) -> str:
        """Decode SSID bytes to string. Returns '<hidden>' for empty SSID."""
        length = self.uSSIDLength
        if length == 0:
            return "<hidden>"
        raw = bytes(self.ucSSID[:length])
        try:
            return raw.decode("utf-8", errors="replace")
        except Exception:
            return "<hidden>"


# ---------------------------------------------------------------------------
# WLAN_INTERFACE_INFO
# ---------------------------------------------------------------------------
class WLAN_INTERFACE_INFO(ctypes.Structure):
    _fields_ = [
        ("InterfaceGuid",        GUID),
        ("strInterfaceDescription", ctypes.c_wchar * WLAN_MAX_NAME_LENGTH),
        ("isState",              ctypes.c_uint),
    ]


# ---------------------------------------------------------------------------
# WLAN_INTERFACE_INFO_LIST
# Variable-length structure: dwNumberOfItems items follow the header.
# ---------------------------------------------------------------------------
class WLAN_INTERFACE_INFO_LIST(ctypes.Structure):
    _fields_ = [
        ("dwNumberOfItems", ctypes.c_ulong),
        ("dwIndex",         ctypes.c_ulong),
        ("InterfaceInfo",   WLAN_INTERFACE_INFO * 1),  # first element
    ]


# ---------------------------------------------------------------------------
# WLAN_BSS_ENTRY
#
# Full BSS information structure as returned by WlanGetNetworkBssList.
# lRssi is the actual RSSI in dBm (signed LONG).
# ulChCenterFrequency is in kHz (divide by 1000 for MHz).
#
# Reference:
#   https://docs.microsoft.com/en-us/windows/win32/api/wlanapi/ns-wlanapi-wlan_bss_entry
#
# NOTE: This structure must be packed correctly. The Windows ABI uses the
# default packing for most members. We use _pack_ = 1 for the header to avoid
# any platform-dependent padding differences between Python versions.
# ---------------------------------------------------------------------------
class WLAN_BSS_ENTRY(ctypes.Structure):
    _fields_ = [
        # SSID of the AP
        ("dot11Ssid",                DOT11_SSID),
        # PHY identifier (ignored)
        ("uPhyId",                   ctypes.c_ulong),
        # BSSID (MAC address) — 6 bytes
        ("dot11Bssid",               ctypes.c_ubyte * DOT11_MAC_ADDRESS_LENGTH),
        # BSS type
        ("dot11BssType",             ctypes.c_uint),
        # PHY type
        ("dot11BssPhyType",          ctypes.c_uint),
        # ACTUAL RSSI in dBm (signed). This is the value to use.
        ("lRssi",                    ctypes.c_long),
        # Link quality 0–100
        ("uLinkQuality",             ctypes.c_ulong),
        # Whether the BSS is connectable
        ("bInRegDomain",             ctypes.c_bool),
        # Beacon interval (ignored)
        ("usBeaconPeriod",           ctypes.c_ushort),
        # Timestamp (ignored)
        ("ullTimestamp",             ctypes.c_ulonglong),
        # Host timestamp (ignored)
        ("ullHostTimestamp",         ctypes.c_ulonglong),
        # Capability information (ignored)
        ("usCapabilityInformation",  ctypes.c_ushort),
        # Channel center frequency in kHz
        ("ulChCenterFrequency",      ctypes.c_ulong),
        # WLAN_RATE_SET (ignored — 126 bytes: 1 ULONG + 126 USHORTs)
        ("wlanRateSet",              ctypes.c_ubyte * (4 + 126 * 2)),
        # Offset to IE data from start of this structure (ULONG)
        ("ulIeOffset",               ctypes.c_ulong),
        # Size of IE data (ULONG)
        ("ulIeSize",                 ctypes.c_ulong),
    ]

    def bssid_string(self) -> str:
        """Return BSSID formatted as AA:BB:CC:DD:EE:FF."""
        return ":".join(f"{b:02X}" for b in self.dot11Bssid)

    def ssid_string(self) -> str:
        """Decode SSID. Returns '<hidden>' for empty/hidden networks."""
        return self.dot11Ssid.decode()

    def rssi_dbm(self) -> int:
        """Return RSSI in dBm (negative integer)."""
        return int(self.lRssi)

    def link_quality(self) -> int:
        """Return link quality 0–100."""
        return int(self.uLinkQuality)

    def frequency_mhz(self) -> Optional[int]:
        """Return channel center frequency in MHz, or None if unavailable."""
        freq_khz = int(self.ulChCenterFrequency)
        if freq_khz == 0:
            return None
        return freq_khz // 1000


# ---------------------------------------------------------------------------
# WLAN_BSS_LIST
#
# Variable-length structure returned by WlanGetNetworkBssList.
# After the header, dwNumberOfItems WLAN_BSS_ENTRY structures follow.
# We use pointer arithmetic to walk them correctly.
# ---------------------------------------------------------------------------
class WLAN_BSS_LIST(ctypes.Structure):
    _pack_ = 4
    _fields_ = [
        ("dwTotalSize",      ctypes.c_ulong),
        ("dwNumberOfItems",  ctypes.c_ulong),
        ("wlanBssEntries",   WLAN_BSS_ENTRY * 1),  # first entry
    ]


# ---------------------------------------------------------------------------
# WinAPI function declarations
# ---------------------------------------------------------------------------
_wlan.WlanOpenHandle.argtypes  = [
    ctypes.c_ulong,             # dwClientVersion
    ctypes.c_void_p,            # pReserved
    ctypes.POINTER(ctypes.c_ulong),  # pdwNegotiatedVersion
    ctypes.POINTER(ctypes.c_void_p), # phClientHandle
]
_wlan.WlanOpenHandle.restype  = ctypes.c_ulong

_wlan.WlanCloseHandle.argtypes = [ctypes.c_void_p, ctypes.c_void_p]
_wlan.WlanCloseHandle.restype  = ctypes.c_ulong

_wlan.WlanEnumInterfaces.argtypes = [
    ctypes.c_void_p,
    ctypes.c_void_p,
    ctypes.POINTER(ctypes.POINTER(WLAN_INTERFACE_INFO_LIST)),
]
_wlan.WlanEnumInterfaces.restype = ctypes.c_ulong

_wlan.WlanScan.argtypes = [
    ctypes.c_void_p,
    ctypes.POINTER(GUID),
    ctypes.POINTER(DOT11_SSID),
    ctypes.c_void_p,
    ctypes.c_void_p,
]
_wlan.WlanScan.restype = ctypes.c_ulong

_wlan.WlanGetNetworkBssList.argtypes = [
    ctypes.c_void_p,
    ctypes.POINTER(GUID),
    ctypes.POINTER(DOT11_SSID),
    ctypes.c_uint,              # dot11BssType
    ctypes.c_bool,              # bSecurityEnabled
    ctypes.c_void_p,            # pReserved
    ctypes.POINTER(ctypes.POINTER(WLAN_BSS_LIST)),
]
_wlan.WlanGetNetworkBssList.restype = ctypes.c_ulong

_wlan.WlanFreeMemory.argtypes = [ctypes.c_void_p]
_wlan.WlanFreeMemory.restype  = None


# ---------------------------------------------------------------------------
# Data classes
# ---------------------------------------------------------------------------
@dataclass
class WLANInterface:
    """Represents a Windows WLAN network interface."""
    guid: GUID
    description: str
    state: int
    state_name: str


@dataclass
class BSSEntry:
    """
    A single BSS (Basic Service Set) entry from a Windows Wi-Fi scan.

    rssi_dbm uses the ACTUAL Windows WLAN API RSSI value (lRssi).
    This is a real measurement in dBm, not a converted percentage.

    Fields that are unavailable from the adapter/driver are None.
    """
    bssid: str              # AA:BB:CC:DD:EE:FF
    ssid: str               # decoded SSID or "<hidden>"
    rssi_dbm: int           # actual RSSI in dBm (e.g., -47)
    link_quality: int       # 0-100 from Windows
    frequency_mhz: Optional[int]  # channel center frequency in MHz
    channel: Optional[int]  # derived channel number, or None
    interface: str          # interface description
    timestamp: str          # ISO 8601 scan timestamp


def _frequency_to_channel(freq_mhz: Optional[int]) -> Optional[int]:
    """
    Convert a Wi-Fi frequency in MHz to the corresponding channel number.
    Returns None if the frequency is unknown or outside known bands.

    Does NOT guess — if the frequency doesn't map to a known channel, returns None.
    """
    if freq_mhz is None:
        return None

    # 2.4 GHz band (channels 1-14)
    if 2412 <= freq_mhz <= 2484:
        if freq_mhz == 2484:
            return 14
        ch = (freq_mhz - 2412) // 5 + 1
        return ch if 1 <= ch <= 14 else None

    # 5 GHz band (channels 36-177)
    if 5180 <= freq_mhz <= 5885:
        ch = (freq_mhz - 5000) // 5
        return ch if ch > 0 else None

    # 6 GHz band (channels 1-233)
    if 5955 <= freq_mhz <= 7115:
        ch = (freq_mhz - 5950) // 5
        return ch if ch > 0 else None

    return None


# ---------------------------------------------------------------------------
# WindowsWiFiScanner — the public interface
# ---------------------------------------------------------------------------
class WindowsWiFiScanner:
    """
    Windows Native Wi-Fi scanner using the Windows WLAN API.

    Usage:
        scanner = WindowsWiFiScanner()
        scanner.scan()  # trigger active scan
        entries = scanner.get_bss_list()  # retrieve results
        scanner.close()

    Or as a context manager:
        with WindowsWiFiScanner() as scanner:
            scanner.scan()
            entries = scanner.get_bss_list()

    All ctypes internals are encapsulated here.
    """

    def __init__(self) -> None:
        self._handle: ctypes.c_void_p = ctypes.c_void_p(None)
        self._interfaces: list[WLANInterface] = []
        self._open_handle()

    def _open_handle(self) -> None:
        """Open a WLAN API client handle."""
        negotiated_version = ctypes.c_ulong(0)
        handle = ctypes.c_void_p(None)

        err = _wlan.WlanOpenHandle(
            WLAN_CLIENT_VERSION_VISTA,
            None,
            ctypes.byref(negotiated_version),
            ctypes.byref(handle),
        )
        if err != ERROR_SUCCESS:
            raise OSError(f"WlanOpenHandle failed with error code {err}")

        self._handle = handle
        logger.debug("WlanOpenHandle succeeded, negotiated version=%d", negotiated_version.value)

    def get_interfaces(self) -> list[WLANInterface]:
        """
        Enumerate all Windows WLAN interfaces.

        Returns a list of WLANInterface objects. May be empty if no
        wireless adapters are present or enabled.

        Frees the WLAN-allocated interface list using WlanFreeMemory.
        """
        iface_list_ptr = ctypes.POINTER(WLAN_INTERFACE_INFO_LIST)()
        err = _wlan.WlanEnumInterfaces(self._handle, None, ctypes.byref(iface_list_ptr))
        if err != ERROR_SUCCESS:
            raise OSError(f"WlanEnumInterfaces failed with error code {err}")

        results: list[WLANInterface] = []
        try:
            iface_list = iface_list_ptr.contents
            count = iface_list.dwNumberOfItems

            # Walk the variable-length array using pointer arithmetic
            base_addr = ctypes.addressof(iface_list.InterfaceInfo)
            entry_size = ctypes.sizeof(WLAN_INTERFACE_INFO)

            for i in range(count):
                entry_ptr = ctypes.cast(
                    base_addr + i * entry_size,
                    ctypes.POINTER(WLAN_INTERFACE_INFO),
                )
                entry = entry_ptr.contents
                state_names = {
                    0: "not_ready",
                    1: "connected",
                    2: "ad_hoc_network_formed",
                    3: "disconnecting",
                    4: "disconnected",
                    5: "associating",
                    6: "discovering",
                    7: "authenticating",
                }
                results.append(WLANInterface(
                    guid=entry.InterfaceGuid,
                    description=entry.strInterfaceDescription,
                    state=entry.isState,
                    state_name=state_names.get(entry.isState, "unknown"),
                ))
        finally:
            _wlan.WlanFreeMemory(iface_list_ptr)

        self._interfaces = results
        return results

    def scan(self, interface: Optional[WLANInterface] = None) -> None:
        """
        Trigger an active Wi-Fi scan on the specified interface.
        If no interface is provided, scans the first available interface.

        Note: WlanScan returns immediately — results are not available
        until the driver completes the scan (typically 2-4 seconds).
        """
        if not self._interfaces:
            self.get_interfaces()

        if not self._interfaces:
            raise OSError("No WLAN interfaces found")

        target = interface or self._get_active_interface()
        if target is None:
            raise OSError("No active WLAN interface found")

        guid_ptr = ctypes.byref(target.guid)
        err = _wlan.WlanScan(self._handle, guid_ptr, None, None, None)
        if err != ERROR_SUCCESS:
            logger.warning("WlanScan returned error %d (may still have cached results)", err)

    def get_bss_list(
        self,
        interface: Optional[WLANInterface] = None,
        wait_for_scan: bool = True,
        scan_wait_seconds: float = 3.0,
    ) -> list[BSSEntry]:
        """
        Retrieve the current BSS list from the driver.

        Args:
            interface: WLAN interface to query. Uses first active if None.
            wait_for_scan: Whether to trigger a scan and wait before querying.
            scan_wait_seconds: Seconds to wait for scan to complete.

        Returns:
            List of BSSEntry objects. May be empty if no APs found.

        Note:
            The driver may return cached results even without a fresh scan.
            Use wait_for_scan=True for fresh data. The scan_wait_seconds
            delay is necessary because WlanScan is asynchronous.
        """
        if not self._interfaces:
            self.get_interfaces()

        if not self._interfaces:
            logger.warning("No WLAN interfaces available")
            return []

        target = interface or self._get_active_interface()
        if target is None:
            logger.warning("No active WLAN interface found")
            return []

        if wait_for_scan:
            try:
                self.scan(target)
                time.sleep(scan_wait_seconds)
            except OSError as e:
                logger.warning("Scan trigger failed (%s), using cached results", e)

        bss_list_ptr = ctypes.POINTER(WLAN_BSS_LIST)()
        err = _wlan.WlanGetNetworkBssList(
            self._handle,
            ctypes.byref(target.guid),
            None,                    # ssid filter: None = all
            DOT11_BSS_TYPE_ANY,
            False,                   # security-enabled filter
            None,                    # reserved
            ctypes.byref(bss_list_ptr),
        )

        if err != ERROR_SUCCESS:
            logger.warning("WlanGetNetworkBssList failed with error %d", err)
            return []

        results: list[BSSEntry] = []
        now = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())

        try:
            bss_list = bss_list_ptr.contents
            count = bss_list.dwNumberOfItems

            if count == 0:
                return []

            # WLAN_BSS_LIST returns a contiguous array of dwNumberOfItems WLAN_BSS_ENTRY structs.
            # (The IE blob referenced by ulIeOffset is located at the tail of the buffer).
            base_addr = ctypes.addressof(bss_list.wlanBssEntries)
            stride = ctypes.sizeof(WLAN_BSS_ENTRY)

            for i in range(count):
                entry_ptr = ctypes.cast(base_addr + i * stride, ctypes.POINTER(WLAN_BSS_ENTRY))
                entry = entry_ptr.contents

                bssid = entry.bssid_string()
                ssid = entry.ssid_string()
                rssi = entry.rssi_dbm()
                quality = entry.link_quality()
                freq_mhz = entry.frequency_mhz()
                channel = _frequency_to_channel(freq_mhz)

                # Skip obviously invalid entries
                # Broadcast BSSID (FF:FF:FF:FF:FF:FF) is not a real AP
                if bssid == "FF:FF:FF:FF:FF:FF":
                    pass
                elif bssid == "00:00:00:00:00:00":
                    pass
                else:
                    results.append(BSSEntry(
                        bssid=bssid,
                        ssid=ssid,
                        rssi_dbm=rssi,
                        link_quality=quality,
                        frequency_mhz=freq_mhz,
                        channel=channel,
                        interface=target.description,
                        timestamp=now,
                    ))

        finally:
            _wlan.WlanFreeMemory(bss_list_ptr)

        return results

    def _get_active_interface(self) -> Optional[WLANInterface]:
        """Return the first active (connected or not) interface, preferring connected ones."""
        if not self._interfaces:
            return None

        # Prefer connected interfaces
        for iface in self._interfaces:
            if iface.state == 1:  # connected
                return iface

        # Fall back to any interface
        return self._interfaces[0]

    def close(self) -> None:
        """Close the WLAN API handle and release resources."""
        if self._handle:
            _wlan.WlanCloseHandle(self._handle, None)
            self._handle = ctypes.c_void_p(None)
            logger.debug("WLAN handle closed")

    def __enter__(self) -> "WindowsWiFiScanner":
        return self

    def __exit__(self, *args) -> None:
        self.close()
