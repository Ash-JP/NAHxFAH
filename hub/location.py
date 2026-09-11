"""
hub/location.py

Interactive location configuration for the hub agent.

Coordinate convention:
    X = East  (right)
    Y = North (forward)
    Z = Up

Example deployments:
    HUB #1 (SW corner):  X=0,  Y=0,  Z=1
    HUB #2 (SE corner):  X=10, Y=0,  Z=1
    HUB #3 (NW corner):  X=0,  Y=10, Z=1
    HUB #4 (NE corner):  X=10, Y=10, Z=1
"""

from __future__ import annotations

import math
from models import HubConfig, HubPosition
from config import save_config


def configure_location_interactive(cfg: HubConfig) -> HubConfig:
    """
    Interactive terminal prompt to configure the hub's physical position.

    Prompts the user for coordinate system (default: local) and X, Y, Z values.
    Validates that all values are finite real numbers.
    Saves the updated config to config.json.

    Returns the updated HubConfig.
    """
    print()
    print("=" * 50)
    print("  HUB LOCATION CONFIGURATION")
    print("=" * 50)
    print()
    print("Coordinate convention:")
    print("  X = East  (right, metres)")
    print("  Y = North (forward, metres)")
    print("  Z = Up    (height, metres)")
    print()
    print("Example hub positions for a 10x10m room:")
    print("  SW corner:  X=0,  Y=0,  Z=1")
    print("  SE corner:  X=10, Y=0,  Z=1")
    print("  NW corner:  X=0,  Y=10, Z=1")
    print("  NE corner:  X=10, Y=10, Z=1")
    print()

    # Coordinate system
    current_cs = cfg.coordinate_system
    cs = input(f"Coordinate system [{current_cs}]: ").strip()
    if not cs:
        cs = current_cs
    if cs not in ("local", "enu", "gps"):
        print(f"WARNING: Unknown coordinate system {cs!r}. Using 'local'.")
        cs = "local"

    # X, Y, Z
    x = _prompt_float("X", cfg.position.x)
    y = _prompt_float("Y", cfg.position.y)
    z = _prompt_float("Z", cfg.position.z)

    cfg.coordinate_system = cs
    cfg.position = HubPosition(coordinate_system=cs, x=x, y=y, z=z)

    save_config(cfg)

    print()
    print("Location configured and saved:")
    print(f"  Coordinate system: {cs}")
    print(f"  X: {x:.2f} m")
    print(f"  Y: {y:.2f} m")
    print(f"  Z: {z:.2f} m")
    print()

    return cfg


def _prompt_float(name: str, current: float) -> float:
    """Prompt for a float value, falling back to current if input is empty."""
    while True:
        raw = input(f"{name} [{current:.2f}]: ").strip()
        if not raw:
            return current
        try:
            val = float(raw)
            if not math.isfinite(val):
                print(f"  ERROR: {name} must be a finite number.")
                continue
            return val
        except ValueError:
            print(f"  ERROR: {raw!r} is not a valid number. Please try again.")
