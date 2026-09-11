# WIFI HUNTER AR 🎯📡

**Indoor Wi-Fi Access Point 3D Localization & Spatial AR Platform**

[![Go Version](https://img.shields.io/badge/Go-1.24-00ADD8?style=flat&logo=go)](https://golang.org)
[![Python Version](https://img.shields.io/badge/Python-3.10%2B-3776AB?style=flat&logo=python)](https://python.org)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-336791?style=flat&logo=postgresql)](https://postgresql.org)
[![Docker Compose](https://img.shields.io/badge/Docker-Compose-2496ED?style=flat&logo=docker)](https://docker.com)
[![Platform: Windows](https://img.shields.io/badge/Hub_Platform-Windows_10%2F11-0078D6?style=flat&logo=windows)](https://microsoft.com)

---

## Overview

**WIFI HUNTER AR** is an indoor spatial intelligence platform designed to passively discover, aggregate, and mathematically localize 802.11 Wi-Fi Access Points (APs) in a shared 3D local coordinate space $(X, Y, Z\text{ in meters})$.

Phase 1 provides a production-grade, two-tier architecture:
1. **Windows Hub Agents (`/hub`)**: Lightweight Python daemons interfacing directly with the native Windows WLAN API (`wlanapi.dll`) to perform real hardware RF scans without faking or synthetic mocks.
2. **Central Server (`/server`)**: A high-performance Go 1.24 backend running inside Docker, orchestrating observation ingestion, log-distance path loss modeling, signal smoothing (EMA + MAD outlier rejection), multi-lateration 3D grid search, confidence estimation, and real-time WebSocket fanout.

The platform is designed from day one with a unified local coordinate frame and stubbed protocols for future **Android ARCore** spatial visualization.

---

## System Architecture

```
[ Windows Hub 1 ]        [ Windows Hub 2 ]        [ Windows Hub 3 ]
(wlanapi.dll ctypes)    (wlanapi.dll ctypes)    (wlanapi.dll ctypes)
       │                        │                        │
       └────────────────────────┼────────────────────────┘
                                │
                  WebSocket JSON (/ws/hub)
                  api_key authentication
                                │
                                ▼
               ┌─────────────────────────────────┐
               │    WIFI HUNTER AR Server (Go)   │
               │         (Port 8000)             │
               │                                 │
               │  • Validation & Rate Limiting   │
               │  • EMA & MAD Signal Filtering   │
               │  • 3D Grid Search Localization  │
               │  • Confidence & Error Radius    │
               │  • Non-blocking WS Dispatcher   │
               └────────────────┬────────────────┘
                                │
               ┌────────────────┴────────────────┐
               │                                 │
               ▼                                 ▼
     ┌──────────────────┐               ┌──────────────────┐
     │  PostgreSQL 16   │               │ Realtime Clients │
     │  (Internal Net)  │               │                  │
     │                  │               │ • Terminal UI    │
     │ • observations   │               │ • /ws/dashboard  │
     │ • access_points  │               │ • /ws/mobile     │
     │ • hubs & anchors │               │   (ARCore stub)  │
     └──────────────────┘               └──────────────────┘
```

---

## Network Topology & Port Mappings

```
                         [ LAN Router / Wi-Fi ]
                                    │
            ┌───────────────────────┴───────────────────────┐
            │ (LAN IP: 192.168.1.50)                        │ (LAN IP: 192.168.1.101)
     ┌──────┴──────────────┐                         ┌──────┴──────────────┐
     │  Host Running       │                         │ Windows Laptop Hub  │
     │  Docker Compose     │                         │ (Agent Terminal)    │
     │                     │                         │                     │
     │  Port 8000 (Exposed)│◄─── ws://192.168.1.50:8000/ws/hub ───────────┤
     │  [Go Server]        │                         └─────────────────────┘
     │        │            │
     │        │ (Internal) │
     │  [PostgreSQL 16]    │
     │  (Port 5432: HIDDEN)│
     └─────────────────────┘
```

- **Port 8000**: Exposed to LAN (HTTP REST API + WebSocket endpoints).
- **Port 5432**: Internal Docker network only (`backend_net`). PostgreSQL is not reachable from the LAN.

---

## Quick Start Guide

### 1. Start Server (Docker Compose)

Copy the sample environment file and launch the backend stack:

```bash
cp .env.example .env
docker compose up --build -d
```

Verify service health:
```bash
curl http://localhost:8000/health
# Expected: {"status":"ok","version":"1.0.0","database":"connected",...}
```

View server logs:
```bash
docker compose logs -f server
```

---

### 2. Configure & Run Windows Hub Agent

From a Windows 10/11 machine equipped with an active Wi-Fi adapter:

```powershell
cd hub

# Install Python requirements
python -m pip install -r requirements.txt

# Run standalone scan test (requires NO server; tests native WLAN API directly)
python main.py --scan-once
```

Configure hub coordinates and target server:
```powershell
python main.py --configure-location
```
Prompts will ask for:
- **Server WebSocket URL**: e.g. `ws://192.168.1.50:8000/ws/hub` (or `ws://localhost:8000/ws/hub` if running locally)
- **API Key**: Must match `HUB_API_KEY` defined in `.env`
- **Hub Position $(X, Y, Z)$**: Your physical position in meters within the building

Launch the hub daemon with interactive terminal dashboard:
```powershell
python main.py
```

---

## Hub CLI Reference

| Flag | Purpose |
|---|---|
| *(no arguments)* | Runs the full daemon: continuous scanning + live terminal dashboard + server sync |
| `--scan-once` | Performs a single hardware scan via `wlanapi.dll` and prints an ASCII table |
| `--configure-location` | Interactive CLI wizard to set coordinates, server URL, and API key |
| `--show-config` | Prints the active `config.json` |
| `--show-id` | Displays the persistent cryptographic Hub ID (e.g. `HUB-A7F32C`) |
| `--test-server` | Tests handshake and authentication with the server, then exits |
| `--version` | Displays hub software version |

---

## REST & WebSocket APIs

### REST Endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/health` | Liveness & database connection probe |
| `GET` | `/api/system/status` | Realtime telemetry: AP counts, connected hubs, client metrics |
| `GET` | `/api/hubs` | List all registered hubs and operational states |
| `GET` | `/api/hubs/{hub_id}` | Retrieve specific hub details |
| `POST` | `/api/hubs/{hub_id}/position` | Update hub 3D coordinates remotely |
| `GET` | `/api/access-points` | List detected APs with filter by status (`localized`, `insufficient_hubs`) |
| `GET` | `/api/access-points/{bssid}` | Get AP position estimate, confidence, and error radius |
| `GET` | `/api/access-points/{bssid}/observations` | Fetch recent raw observations for an AP |
| `GET` | `/api/observations` | Paginated raw observation feed |
| `GET` | `/api/anchors` | List spatial coordinate anchors |
| `POST` | `/api/anchors` | Create a new reference anchor |

### WebSocket Endpoints

- `/ws/hub`: Hub agent ingestion channel (requires API key).
- `/ws/dashboard`: Telemetry feed broadcasting `hub_status`, `observation_stats`, and `ap_update`.
- `/ws/mobile`: Android ARCore device channel for real-time 3D pose updates and AP visual tracking.

Full OpenAPI 3.1 specification available at [`docs/openapi.yaml`](file:///docs/openapi.yaml).

---

## Mathematical Localization Model

Distance is modeled using the Log-Distance Path Loss equation:
$$\hat{d} = 10^{\frac{A - \text{RSSI}}{10 \cdot n}}$$
where $A = -40\text{ dBm}$ (reference signal at 1m) and $n = 2.7$ (indoor clutter exponent).

When $\ge 3$ spatially diverse hubs report observations for the same BSSID within a 45-second sliding window, a 3D Weighted Non-Linear Least Squares Grid Search evaluates candidate points to minimize residual distance error:
$$\mathcal{J}(x, y, z) = \sum_{i=1}^{H} \frac{1}{\hat{d}_i^2} \left( \|\mathbf{p} - \mathbf{h}_i\| - \hat{d}_i \right)^2$$

Confidence ($0.0 - 1.0$) is calculated from hub count, spatial diversity (angular spread), RSSI standard deviation, observation frequency, and grid residual.

For in-depth math and physics documentation, see [`docs/localization.md`](file:///docs/localization.md).

---

## Troubleshooting Guide

### 1. `ERROR: No Wi-Fi interfaces found`
- **Cause**: Wi-Fi adapter is physically disabled, driver is missing, or laptop is in Airplane Mode.
- **Fix**: Verify Wi-Fi is toggled ON in Windows Settings. Run `netsh wlan show interfaces` to verify adapter state.

### 2. `--scan-once` returns 0 APs
- **Cause**: Windows 10/11 requires Location permissions enabled to query nearby Wi-Fi beacons.
- **Fix**: Open Windows Settings > Privacy & Security > Location, toggle ON "Location Services" and allow desktop apps.

### 3. Server Authentication Fails (`AUTH_FAILED`)
- **Cause**: `api_key` in `hub/config.json` does not match `HUB_API_KEY` in `.env`.
- **Fix**: Check `HUB_API_KEY` in `.env` and rerun `python main.py --configure-location` on the hub.

### 4. Hub Status is `STALE` or `OFFLINE`
- **Cause**: Hub hasn't sent an observation or heartbeat within the configured timeout (default: 15s / 30s).
- **Fix**: Verify the hub terminal is active and network latency between laptop and Docker host is stable.

---

## Phase 2: Android ARCore Forward Compatibility

WIFI HUNTER AR was constructed specifically so Android ARCore mobile devices can connect to `/ws/mobile` without modifying server internals:
- Shared right-handed coordinate frame (meters: $+X$ East/Right, $+Y$ North/Forward, $+Z$ Up).
- Pre-defined message schemas for `mobile_register` and 6-DoF `mobile_pose` (`qx, qy, qz, qw`).
- Anchor synchronization via `/api/anchors` allows aligning the ARCore World Origin with physical room landmarks (e.g. entrance doors or QR markers).

---

## Documentation Index

- [Architecture Specification](file:///docs/architecture.md)
- [WebSocket Protocol Specification](file:///docs/protocol.md)
- [Localization Engine & RF Physics](file:///docs/localization.md)
- [Windows Hub Agent Internals](file:///docs/windows-hub.md)
- [OpenAPI 3.1 Specification](file:///docs/openapi.yaml)
