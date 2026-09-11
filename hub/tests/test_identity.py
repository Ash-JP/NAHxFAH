"""tests/test_identity.py — Tests for hub identity management."""

import json
import os
import sys
import tempfile
import pytest

# Allow imports from hub/ directory
sys.path.insert(0, os.path.dirname(os.path.dirname(__file__)))


def test_generate_hub_id_format():
    """Hub ID must match HUB-XXXXXX format with uppercase hex."""
    import re
    from identity import _generate_hub_id

    hub_id = _generate_hub_id()
    assert re.match(r'^HUB-[0-9A-F]{6}$', hub_id), f"Invalid hub ID format: {hub_id}"


def test_generate_hub_id_is_unique():
    """Each call should produce a different hub ID."""
    from identity import _generate_hub_id

    ids = set(_generate_hub_id() for _ in range(20))
    # Very unlikely to have collision in 20 random 3-byte values
    assert len(ids) > 1, "Hub IDs are not unique"


def test_load_or_create_creates_new_identity():
    """First run creates a new identity file."""
    with tempfile.TemporaryDirectory() as tmpdir:
        # Patch the identity file path
        import identity as identity_module
        original_dir  = identity_module._IDENTITY_DIR
        original_file = identity_module._IDENTITY_FILE

        try:
            identity_module._IDENTITY_DIR  = os.path.join(tmpdir, "hub_data")
            identity_module._IDENTITY_FILE = os.path.join(identity_module._IDENTITY_DIR, "identity.json")

            result = identity_module.load_or_create_identity()
            assert result.hub_id.startswith("HUB-")
            assert result.created_at

            # File should exist now
            assert os.path.exists(identity_module._IDENTITY_FILE)
        finally:
            identity_module._IDENTITY_DIR  = original_dir
            identity_module._IDENTITY_FILE = original_file


def test_load_or_create_returns_same_identity_on_second_run():
    """Subsequent runs return the same hub ID."""
    with tempfile.TemporaryDirectory() as tmpdir:
        import identity as identity_module
        original_dir  = identity_module._IDENTITY_DIR
        original_file = identity_module._IDENTITY_FILE

        try:
            identity_module._IDENTITY_DIR  = os.path.join(tmpdir, "hub_data")
            identity_module._IDENTITY_FILE = os.path.join(identity_module._IDENTITY_DIR, "identity.json")

            first  = identity_module.load_or_create_identity()
            second = identity_module.load_or_create_identity()

            assert first.hub_id == second.hub_id
            assert first.created_at == second.created_at
        finally:
            identity_module._IDENTITY_DIR  = original_dir
            identity_module._IDENTITY_FILE = original_file
