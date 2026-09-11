"""
hub/identity.py

Persistent hub identity management.

The hub ID is:
  - Cryptographically random (6 hex characters → 3 bytes of randomness)
  - Persistent across restarts (stored in hub_data/identity.json)
  - Human-readable (e.g., HUB-A7F32C)
  - Independent of Windows username, MAC address, or hostname
  - Unique per hub agent installation

File location: hub_data/identity.json
"""

from __future__ import annotations

import json
import logging
import os
import secrets
import datetime

from models import HubIdentity

logger = logging.getLogger(__name__)

_IDENTITY_DIR  = "hub_data"
_IDENTITY_FILE = os.path.join(_IDENTITY_DIR, "identity.json")


def _generate_hub_id() -> str:
    """Generate a cryptographically random hub ID in the format HUB-XXXXXX."""
    random_bytes = secrets.token_bytes(3)
    hex_part = random_bytes.hex().upper()
    return f"HUB-{hex_part}"


def load_or_create_identity() -> HubIdentity:
    """
    Load the persistent hub identity from disk, creating it if it doesn't exist.

    On first run:
        - Generates a cryptographically random HUB-XXXXXX identifier
        - Saves it to hub_data/identity.json
        - Returns the new identity

    On subsequent runs:
        - Loads the existing identity from hub_data/identity.json
        - Returns the stored identity unchanged

    The hub_data/ directory is created automatically if needed.
    """
    os.makedirs(_IDENTITY_DIR, exist_ok=True)

    if os.path.exists(_IDENTITY_FILE):
        try:
            with open(_IDENTITY_FILE, "r", encoding="utf-8") as f:
                data = json.load(f)
            hub_id = data.get("hub_id")
            created_at = data.get("created_at")
            if hub_id and created_at:
                logger.info("Loaded existing hub identity: %s", hub_id)
                return HubIdentity(hub_id=hub_id, created_at=created_at)
            else:
                logger.warning("Identity file is malformed, regenerating")
        except (json.JSONDecodeError, KeyError, OSError) as e:
            logger.warning("Failed to load identity file (%s), regenerating", e)

    # Create new identity
    hub_id = _generate_hub_id()
    created_at = datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    identity = HubIdentity(hub_id=hub_id, created_at=created_at)

    _save_identity(identity)
    logger.info("Created new hub identity: %s", hub_id)
    return identity


def _save_identity(identity: HubIdentity) -> None:
    """Save identity to disk."""
    data = {
        "hub_id": identity.hub_id,
        "created_at": identity.created_at,
    }
    os.makedirs(_IDENTITY_DIR, exist_ok=True)
    with open(_IDENTITY_FILE, "w", encoding="utf-8") as f:
        json.dump(data, f, indent=4)
        f.write("\n")


def get_identity_path() -> str:
    """Return the absolute path to the identity file."""
    return os.path.abspath(_IDENTITY_FILE)
