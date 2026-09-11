"""
hub/config.py

Hub configuration loading and saving.

Configuration file: config.json
Example: config.example.json
"""

from __future__ import annotations

import json
import logging
import os
from typing import Optional

from models import HubConfig

logger = logging.getLogger(__name__)

_CONFIG_FILE = "config.json"


def load_config() -> HubConfig:
    """
    Load hub configuration from config.json.

    Returns default config if the file doesn't exist.
    Raises ValueError for invalid configuration values.
    """
    if not os.path.exists(_CONFIG_FILE):
        logger.warning(
            "config.json not found. Using defaults. "
            "Run: cp config.example.json config.json"
        )
        return HubConfig()

    try:
        with open(_CONFIG_FILE, "r", encoding="utf-8") as f:
            data = json.load(f)
        cfg = HubConfig.from_dict(data)
        _validate_config(cfg)
        logger.info("Loaded config from %s", _CONFIG_FILE)
        return cfg
    except json.JSONDecodeError as e:
        raise ValueError(f"config.json is not valid JSON: {e}") from e
    except OSError as e:
        raise ValueError(f"Cannot read config.json: {e}") from e


def save_config(cfg: HubConfig) -> None:
    """Save configuration to config.json."""
    with open(_CONFIG_FILE, "w", encoding="utf-8") as f:
        json.dump(cfg.to_dict(), f, indent=4)
        f.write("\n")
    logger.info("Configuration saved to %s", _CONFIG_FILE)


def _validate_config(cfg: HubConfig) -> None:
    """Validate configuration values. Raises ValueError for invalid entries."""
    if not cfg.server_url:
        raise ValueError("server_url is required in config.json")
    if not cfg.server_url.startswith(("ws://", "wss://")):
        raise ValueError(
            f"server_url must start with ws:// or wss://, got: {cfg.server_url!r}"
        )
    if cfg.api_key in ("", "change_me"):
        logger.warning(
            "api_key appears to be the default placeholder. "
            "Update config.json with the real HUB_API_KEY from the server."
        )
    if cfg.scan_interval_seconds < 1:
        raise ValueError("scan_interval_seconds must be >= 1")
    if cfg.heartbeat_interval_seconds < 1:
        raise ValueError("heartbeat_interval_seconds must be >= 1")
    if cfg.coordinate_system not in ("local", "enu", "gps"):
        raise ValueError(
            f"coordinate_system must be local, enu, or gps, got: {cfg.coordinate_system!r}"
        )
    import math
    pos = cfg.position
    for name, val in [("x", pos.x), ("y", pos.y), ("z", pos.z)]:
        if not math.isfinite(val):
            raise ValueError(f"position.{name} must be a finite number, got: {val}")
