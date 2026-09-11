"""tests/test_parser.py — Tests for BSSID/SSID parsing and observation normalization."""

import os
import sys
import pytest

sys.path.insert(0, os.path.dirname(os.path.dirname(__file__)))

from scanner import _normalize_bssid, _validate_rssi


class TestBSSIDNormalization:
    """BSSID normalization and validation."""

    def test_valid_lowercase(self):
        assert _normalize_bssid("aa:bb:cc:dd:ee:ff") == "AA:BB:CC:DD:EE:FF"

    def test_valid_uppercase(self):
        assert _normalize_bssid("AA:BB:CC:DD:EE:FF") == "AA:BB:CC:DD:EE:FF"

    def test_valid_mixed_case(self):
        assert _normalize_bssid("aA:bB:cC:dD:eE:fF") == "AA:BB:CC:DD:EE:FF"

    def test_real_format(self):
        assert _normalize_bssid("12:34:56:78:9a:bc") == "12:34:56:78:9A:BC"

    def test_reject_broadcast(self):
        assert _normalize_bssid("FF:FF:FF:FF:FF:FF") is None

    def test_reject_null(self):
        assert _normalize_bssid("00:00:00:00:00:00") is None

    def test_reject_too_short(self):
        assert _normalize_bssid("AA:BB:CC:DD:EE") is None

    def test_reject_too_long(self):
        assert _normalize_bssid("AA:BB:CC:DD:EE:FF:11") is None

    def test_reject_invalid_hex(self):
        assert _normalize_bssid("GG:BB:CC:DD:EE:FF") is None

    def test_reject_empty(self):
        assert _normalize_bssid("") is None

    def test_reject_no_colons(self):
        assert _normalize_bssid("AABBCCDDEEFF") is None


class TestRSSIValidation:
    """RSSI value validation."""

    def test_valid_typical(self):
        assert _validate_rssi(-50) == -50

    def test_valid_strong(self):
        assert _validate_rssi(-30) == -30

    def test_valid_weak(self):
        assert _validate_rssi(-95) == -95

    def test_valid_boundary_min(self):
        assert _validate_rssi(-127) == -127

    def test_valid_boundary_max(self):
        assert _validate_rssi(0) == 0

    def test_reject_too_negative(self):
        assert _validate_rssi(-128) is None

    def test_reject_positive(self):
        assert _validate_rssi(1) is None

    def test_none_passthrough(self):
        assert _validate_rssi(None) is None

    def test_reject_positive_large(self):
        assert _validate_rssi(50) is None


class TestSSIDHandling:
    """SSID decoding via Windows WLAN API structure."""

    def test_hidden_ssid_decoding(self):
        """Empty SSID should produce '<hidden>'."""
        from windows.wlan_api import DOT11_SSID
        ssid = DOT11_SSID()
        ssid.uSSIDLength = 0
        assert ssid.decode() == "<hidden>"

    def test_normal_ssid_decoding(self):
        """ASCII SSID should decode correctly."""
        from windows.wlan_api import DOT11_SSID
        ssid = DOT11_SSID()
        test_name = b"MyNetwork"
        ssid.uSSIDLength = len(test_name)
        for i, b in enumerate(test_name):
            ssid.ucSSID[i] = b
        assert ssid.decode() == "MyNetwork"

    def test_utf8_ssid_decoding(self):
        """UTF-8 SSID should decode correctly."""
        from windows.wlan_api import DOT11_SSID
        ssid = DOT11_SSID()
        test_name = "Café-WiFi".encode("utf-8")
        ssid.uSSIDLength = len(test_name)
        for i, b in enumerate(test_name):
            ssid.ucSSID[i] = b
        assert ssid.decode() == "Café-WiFi"
