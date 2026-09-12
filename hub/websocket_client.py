"""
hub/websocket_client.py

WebSocket client for the WIFI HUNTER AR hub agent.

Connects to ws://SERVER_IP:8000/ws/hub and maintains the connection
with automatic reconnection using exponential backoff.

Reconnection backoff:
    1s → 2s → 4s → 8s → 16s → 30s (capped)

After reconnection:
    1. Re-send hub_register
    2. Resume heartbeat
    3. Resume observation uploads
"""

from __future__ import annotations

import asyncio
import json
import logging
import time
from typing import Optional, Callable

import websockets
import websockets.exceptions

from models import HubConfig, HubIdentity, ConnectionStatus, ObservationEntry

logger = logging.getLogger(__name__)

VERSION = "1.0.0"

# Exponential backoff schedule (seconds)
_BACKOFF_SCHEDULE = [1, 2, 4, 8, 16, 30]


class HubWebSocketClient:
    """
    Manages the WebSocket connection from hub to server.

    Sends:
        hub_register      — on connect
        wifi_observations — per scan cycle
        heartbeat         — every heartbeat_interval_seconds

    Receives:
        hub_registered    — registration acknowledgement
        heartbeat_ack     — heartbeat acknowledgement
        error             — protocol error from server
        ap_update         — AP localization result (broadcast)
    """

    def __init__(
        self,
        config: HubConfig,
        identity: HubIdentity,
        on_ap_update: Optional[Callable[[dict], None]] = None,
        on_status_change: Optional[Callable[[str], None]] = None,
    ) -> None:
        self._config = config
        self._identity = identity
        self._on_ap_update = on_ap_update
        self._on_status_change = on_status_change
        self._status = ConnectionStatus(state="CONNECTING")
        self._ws: Optional[websockets.WebSocketClientProtocol] = None
        self._running = False
        self._registered = False
        self._send_queue: asyncio.Queue = None  # type: ignore

    @property
    def status(self) -> ConnectionStatus:
        return self._status

    def _set_state(self, state: str) -> None:
        self._status.state = state
        if self._on_status_change:
            self._on_status_change(state)

    async def run(self) -> None:
        """Main connection loop with exponential backoff reconnection."""
        self._running = True
        attempt = 0

        while self._running:
            try:
                await self._connect_and_run()
                attempt = 0  # Reset on clean disconnect
            except (
                websockets.exceptions.ConnectionClosed,
                websockets.exceptions.WebSocketException,
                OSError,
                asyncio.TimeoutError,
            ) as e:
                logger.warning("WebSocket connection lost: %s", e)
                self._registered = False

            if not self._running:
                break

            # Exponential backoff
            backoff = _BACKOFF_SCHEDULE[min(attempt, len(_BACKOFF_SCHEDULE) - 1)]
            attempt += 1
            self._status.reconnect_count += 1
            self._set_state("RECONNECTING")
            logger.info("Reconnecting in %ds (attempt %d)...", backoff, attempt)
            await asyncio.sleep(backoff)

    async def _connect_and_run(self) -> None:
        """Connect to server and run the full session."""
        self._set_state("CONNECTING")
        url = self._config.server_url

        logger.info("Connecting to %s", url)

        async with websockets.connect(
            url,
            ping_interval=None,  # We handle heartbeats manually
            ping_timeout=None,
            close_timeout=10,
            max_size=1024 * 1024,
        ) as ws:
            self._ws = ws
            self._send_queue = asyncio.Queue()

            # 1. Send hub_register
            await self._send_register(ws)

            # 2. Wait for hub_registered acknowledgement
            registered = await self._wait_for_registration(ws)
            if not registered:
                return

            self._registered = True
            self._set_state("CONNECTED")
            logger.info("Hub registered successfully")

            # 3. Run heartbeat + message dispatcher concurrently
            await asyncio.gather(
                self._heartbeat_loop(ws),
                self._send_loop(ws),
                self._receive_loop(ws),
            )

    async def _send_register(self, ws) -> None:
        """Send hub_register message."""
        pos = self._config.position
        msg = {
            "type": "hub_register",
            "hub_id": self._identity.hub_id,
            "device_type": "windows_laptop",
            "platform": "windows",
            "version": VERSION,
            "api_key": self._config.api_key,
            "position": {
                "coordinate_system": pos.coordinate_system,
                "x": pos.x,
                "y": pos.y,
                "z": pos.z,
            },
        }
        await ws.send(json.dumps(msg))
        logger.info("Sent hub_register for %s at (%.2f, %.2f, %.2f)", 
                    self._identity.hub_id, pos.x, pos.y, pos.z)

    async def _wait_for_registration(self, ws) -> bool:
        """Wait for hub_registered acknowledgement from server."""
        try:
            async with asyncio.timeout(15):
                raw = await ws.recv()
                msg = json.loads(raw)
                if msg.get("type") == "hub_registered":
                    logger.info("Server acknowledged registration: %s", msg.get("status"))
                    return True
                elif msg.get("type") == "error":
                    logger.error("Registration rejected: %s — %s",
                                 msg.get("code"), msg.get("message"))
                    return False
                else:
                    logger.warning("Unexpected first message: %s", msg.get("type"))
                    return False
        except asyncio.TimeoutError:
            logger.error("Timed out waiting for hub_registered")
            return False

    async def _heartbeat_loop(self, ws) -> None:
        """Send heartbeat messages every heartbeat_interval_seconds."""
        interval = self._config.heartbeat_interval_seconds
        while True:
            await asyncio.sleep(interval)
            msg = {
                "type": "heartbeat",
                "hub_id": self._identity.hub_id,
                "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            }
            try:
                await ws.send(json.dumps(msg))
            except websockets.exceptions.ConnectionClosed:
                break

    async def _send_loop(self, ws) -> None:
        """Drain the send queue and write messages to the WebSocket."""
        while True:
            try:
                data = await asyncio.wait_for(self._send_queue.get(), timeout=1.0)
                await ws.send(data)
            except asyncio.TimeoutError:
                continue
            except websockets.exceptions.ConnectionClosed:
                break

    async def _receive_loop(self, ws) -> None:
        """Receive and dispatch messages from the server."""
        async for raw in ws:
            try:
                msg = json.loads(raw)
                await self._dispatch(msg)
            except json.JSONDecodeError as e:
                logger.debug("Failed to parse server message: %s", e)

    async def _dispatch(self, msg: dict) -> None:
        """Dispatch an incoming server message."""
        msg_type = msg.get("type")
        if msg_type == "heartbeat_ack":
            pass  # Normal — server is alive
        elif msg_type == "ap_update":
            if self._on_ap_update:
                self._on_ap_update(msg)
        elif msg_type == "error":
            logger.warning("Server error: %s — %s", msg.get("code"), msg.get("message"))
        else:
            logger.debug("Unknown message type from server: %s", msg_type)

    async def send_observations(self, observations: list[ObservationEntry]) -> bool:
        """
        Queue a wifi_observations message for sending.
        Returns False if not connected.
        """
        if not self._registered or self._send_queue is None:
            return False

        pos = self._config.position
        msg = {
            "type": "wifi_observations",
            "hub_id": self._identity.hub_id,
            "position": {
                "coordinate_system": pos.coordinate_system,
                "x": pos.x,
                "y": pos.y,
                "z": pos.z,
            },
            "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            "observations": [o.to_dict() for o in observations],
        }
        try:
            self._send_queue.put_nowait(json.dumps(msg))
            self._status.observations_sent += len(observations)
            self._status.last_upload_time = time.time()
            return True
        except asyncio.QueueFull:
            logger.warning("Send queue full, dropping observation batch")
            return False

    def stop(self) -> None:
        """Signal the connection loop to stop."""
        self._running = False
        if self._ws:
            asyncio.create_task(self._ws.close())
