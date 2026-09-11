"""
hub/logger.py

Structured logging configuration for the WIFI HUNTER AR hub agent.
"""

from __future__ import annotations

import logging
import logging.handlers
import os
import sys

_LOG_DIR  = "logs"
_LOG_FILE = os.path.join(_LOG_DIR, "hub.log")


def setup_logging(level: str = "INFO") -> None:
    """
    Configure logging for the hub agent.

    - DEBUG/INFO to stderr with human-readable format
    - All levels to rotating file (logs/hub.log)
    - Log level controlled by the 'level' parameter
    """
    os.makedirs(_LOG_DIR, exist_ok=True)

    numeric_level = getattr(logging, level.upper(), logging.INFO)
    root = logging.getLogger()
    root.setLevel(logging.DEBUG)  # root captures everything

    # Console handler — INFO and above
    console = logging.StreamHandler(sys.stderr)
    console.setLevel(numeric_level)
    console.setFormatter(logging.Formatter(
        "%(asctime)s  %(levelname)-8s  %(name)-20s  %(message)s",
        datefmt="%H:%M:%S",
    ))

    # Rotating file handler — DEBUG and above
    file_handler = logging.handlers.RotatingFileHandler(
        _LOG_FILE,
        maxBytes=5 * 1024 * 1024,  # 5 MB
        backupCount=3,
        encoding="utf-8",
    )
    file_handler.setLevel(logging.DEBUG)
    file_handler.setFormatter(logging.Formatter(
        "%(asctime)s  %(levelname)-8s  %(name)-30s  %(message)s",
    ))

    root.addHandler(console)
    root.addHandler(file_handler)

    logging.getLogger("websockets").setLevel(logging.WARNING)
    logging.getLogger("asyncio").setLevel(logging.WARNING)
