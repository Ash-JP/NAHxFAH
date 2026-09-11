# WIFI HUNTER AR — Windows Hub Agent Internals & Operation

## 1. Windows Native Wi-Fi Subsystem (`wlanapi.dll`)

The hub agent interfaces directly with the native Windows Native Wi-Fi API exposed by `C:\Windows\System32\wlanapi.dll` using Python's standard `ctypes` library.

### C Struct Layout & Alignment

The Windows WLAN API structures have strict memory alignment requirements across 32-bit and 64-bit architectures. `hub/windows/wlan_api.py` explicitly reproduces the official Windows SDK definitions:

```
+--------------------------------------------------------------------+
|                       WLAN_BSS_LIST                                |
|  ulTotalSize (DWORD)  |  dwNumberOfItems (DWORD)                   |
|  wlanBssEntries[dwNumberOfItems] (Variable length array)           |
+--------------------------------------------------------------------+
                                 │
                                 ▼
+--------------------------------------------------------------------+
|                       WLAN_BSS_ENTRY                               |
|  dot11Ssid (DOT11_SSID: uSSIDLength + ucSSID[32])                  |
|  uPhyId (ULONG)                                                    |
|  dot11Bssid (DOT11_MAC_ADDRESS: 6 bytes)                           |
|  dot11BssType (DOT11_BSS_TYPE: 4 bytes enum)                       |
|  dot11BssPhyType (DOT11_BSS_PHY_TYPE: 4 bytes enum)                |
|  lRssi (LONG: signed dBm, typically -100 to 0)                     |
|  ulLinkQuality (ULONG: 0 to 100)                                   |
|  bInRegDomain (BOOLEAN: 1 byte)                                    |
|  usBeaconPeriod (USHORT: 2 bytes)                                  |
|  ullTimestamp (ULONGLONG: 8 bytes)                                 |
|  ullHostTimestamp (ULONGLONG: 8 bytes)                             |
|  usCapabilityInformation (USHORT: 2 bytes)                         |
|  ulChCenterFrequency (ULONG: frequency in kHz)                     |
|  wlanRateSet (WLAN_RATE_SET)                                       |
|  ulIeOffset (ULONG) | ulIeSize (ULONG)                             |
+--------------------------------------------------------------------+
```

### Memory Management & Safety

Every call to `WlanGetNetworkBssList` allocates unmanaged heap memory allocated by the Windows WLAN service.
- The wrapper guarantees that `WlanFreeMemory(pBssList)` is executed within a `finally:` block for every scan.
- Handles opened via `WlanOpenHandle` are negotiated with client version `2` (Windows Vista through Windows 11) and cleaned up via `WlanCloseHandle`.

---

## 2. Windows Wi-Fi Driver Quirks & Constraints

### 2.1 Scan Throttling & Background Caching
- **Driver Caching**: Calling `WlanScan()` signals the NDIS miniport driver to issue 802.11 probe requests across all supported channels. Most standard Wi-Fi cards require $2.5 - 4.0\text{ seconds}$ to complete a full 2.4 GHz + 5 GHz channel sweep.
- **Throttling**: Windows limits non-connected background scans to prevent battery drain and latency spikes. The hub agent coordinates scanning with a default 5-second interval (`scan_interval_seconds: 5`), giving the hardware driver sufficient time to refresh its internal BSS cache.
- **Location Services (Windows 10/11)**:
  On Windows 10/11, standard user accounts querying WLAN BSS lists must ensure "Location Services" is enabled in Windows Settings (`Settings > Privacy & Security > Location`), or run in an elevated command prompt, otherwise the OS may return empty BSS lists or redact SSIDs.

### 2.2 Passive vs Active Probing
The agent executes passive observation of the standard beacon and probe response cache.
- **Zero Infiltration**: The hub does not transmit association requests, perform handshakes, deauthenticate stations, or capture packet payloads.
- **Hidden SSIDs**: When an AP suppresses beacon SSID broadcasts (`uSSIDLength == 0`), the hub displays and reports `"<hidden>"` while preserving the BSSID, RSSI, and frequency for localization.

---

## 3. Installation & Quick Start

### Prerequisites
- Windows 10 or 11 (64-bit)
- Internal or USB Wi-Fi 802.11 adapter
- Python 3.10+ (Python 3.12+ recommended)

### Setup Instructions

1. **Clone or Copy Repository**:
   ```powershell
   cd d:\Github\NAHxFAH\hub
   ```

2. **Install Dependencies**:
   ```powershell
   python -m pip install -r requirements.txt
   ```

3. **Verify Local Scanner (No Server Needed)**:
   Run a standalone one-shot scan to test `wlanapi.dll` communication and verify your adapter detects nearby access points:
   ```powershell
   python main.py --scan-once
   ```
   *Expected Output*: A formatted table displaying discovered SSIDs, BSSIDs, RSSI (in dBm), Frequency (MHz), and Channels.

4. **Configure Hub Position & Server Endpoint**:
   Launch the interactive location configuration CLI:
   ```powershell
   python main.py --configure-location
   ```
   Follow the prompts to enter:
   - Server WebSocket URL (e.g., `ws://192.168.1.50:8000/ws/hub`)
   - Shared `api_key` matching the server's `HUB_API_KEY`
   - Hub 3D Position in meters ($X, Y, Z$)
   
   This creates/updates `config.json`.

5. **Start Continuous Hub Operation**:
   ```powershell
   python main.py
   ```
   The `rich` live terminal dashboard will start, displaying live connection status, scanning telemetry, and top detected signals.

---

## 4. CLI Command Reference

| Command | Description |
|---|---|
| `python main.py` | Starts full continuous hub service with terminal UI and server upload |
| `python main.py --scan-once` | Performs single real Wi-Fi scan and prints results; requires no server |
| `python main.py --configure-location` | Interactive setup for server URL, API key, and hub 3D coordinates |
| `python main.py --show-config` | Prints current `config.json` in human-readable JSON |
| `python main.py --show-id` | Prints persistent Hub ID (e.g., `HUB-A7F32C`) from `hub_data/identity.json` |
| `python main.py --test-server` | Tests handshake and authentication with server, then exits |
| `python main.py --version` | Prints version number |

---

## 5. Troubleshooting

| Symptom | Cause | Resolution |
|---|---|---|
| `ERROR: No Wi-Fi interfaces found` | Adapter is disabled, unseated, or in airplane mode | Verify Wi-Fi is enabled in Windows Taskbar. Ensure driver is functional in Device Manager. |
| `--scan-once` returns 0 access points | Windows location permissions or driver busy | Open `Settings > Privacy & Security > Location` and enable location access. Alternatively, run terminal as Administrator. |
| `Server: CONNECTING` (stuck) | Server address unreachable or firewall blocking port 8000 | Ensure Go server is running and Docker host allows inbound port 8000 on LAN. Test with `curl http://SERVER_IP:8000/health`. |
| Server rejects connection with `AUTH_FAILED` | `api_key` mismatch between hub and server | Check `HUB_API_KEY` in `.env` and verify `api_key` in `hub/config.json`. |
