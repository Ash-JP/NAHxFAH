package tests

import (
	"math"
	"testing"

	"github.com/nahxfah/wifi-hunter-server/internal/protocol"
)

func TestBSSIDNormalization(t *testing.T) {
	cases := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"aa:bb:cc:dd:ee:ff", "AA:BB:CC:DD:EE:FF", false},
		{"AA:BB:CC:DD:EE:FF", "AA:BB:CC:DD:EE:FF", false},
		{"aA:bB:cC:dD:eE:fF", "AA:BB:CC:DD:EE:FF", false},
		{"", "", true},
		{"00:11:22:33:44", "", true},       // only 5 octets
		{"GG:BB:CC:DD:EE:FF", "", true},    // invalid hex
		{"00:11:22:33:44:55:66", "", true}, // too long
		{"not-a-bssid", "", true},
	}

	for _, tc := range cases {
		got, err := protocol.ValidateAndNormalizeBSSID(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ValidateAndNormalizeBSSID(%q): expected error, got nil", tc.input)
			}
		} else {
			if err != nil {
				t.Errorf("ValidateAndNormalizeBSSID(%q): unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("ValidateAndNormalizeBSSID(%q) = %q, want %q", tc.input, got, tc.want)
			}
		}
	}
}

func TestRSSIValidation(t *testing.T) {
	validCases := []int{-127, -100, -60, -1, 0}
	invalidCases := []int{-128, 1, 50, 100, -200}

	for _, rssi := range validCases {
		if err := protocol.ValidateRSSI(rssi); err != nil {
			t.Errorf("RSSI %d should be valid, got error: %v", rssi, err)
		}
	}

	for _, rssi := range invalidCases {
		if err := protocol.ValidateRSSI(rssi); err == nil {
			t.Errorf("RSSI %d should be invalid", rssi)
		}
	}
}

func TestHubIDValidation(t *testing.T) {
	valid := []string{"HUB-A7F32C", "HUB-AABBCC", "MOBILE-82A1F3", "HUB-ABC123"}
	invalid := []string{"", "HUB", "hub-abc123", "HUB-", "INVALID"}

	for _, id := range valid {
		if err := protocol.ValidateHubID(id); err != nil {
			t.Errorf("hub ID %q should be valid: %v", id, err)
		}
	}

	for _, id := range invalid {
		if err := protocol.ValidateHubID(id); err == nil {
			t.Errorf("hub ID %q should be invalid", id)
		}
	}
}

func TestFrequencyValidation(t *testing.T) {
	valid := []int{2412, 2437, 2462, 5180, 5500, 5745, 6175}
	invalid := []int{0, -1, 1000, 8000}

	for _, f := range valid {
		if err := protocol.ValidateFrequency(f); err != nil {
			t.Errorf("frequency %d should be valid: %v", f, err)
		}
	}

	for _, f := range invalid {
		if err := protocol.ValidateFrequency(f); err == nil {
			t.Errorf("frequency %d should be invalid", f)
		}
	}
}

func TestAPIKeyAuthentication(t *testing.T) {
	expected := "my-secret-api-key-12345"

	if !protocol.AuthenticateAPIKey(expected, expected) {
		t.Error("matching keys should authenticate")
	}
	if protocol.AuthenticateAPIKey("wrong-key", expected) {
		t.Error("wrong key should not authenticate")
	}
	if protocol.AuthenticateAPIKey("", expected) {
		t.Error("empty key should not authenticate")
	}
}

func TestCoordinateValidation(t *testing.T) {
	// Valid
	if err := protocol.ValidateCoordinates(1.0, 2.0, 3.0); err != nil {
		t.Errorf("valid coordinates should pass: %v", err)
	}
	if err := protocol.ValidateCoordinates(0, 0, 0); err != nil {
		t.Errorf("zero coordinates should pass: %v", err)
	}

	// Invalid
	nan := math.NaN()
	inf := math.Inf(1)

	if err := protocol.ValidateCoordinates(nan, 0, 0); err == nil {
		t.Error("NaN X should fail")
	}
	if err := protocol.ValidateCoordinates(0, inf, 0); err == nil {
		t.Error("Inf Y should fail")
	}
}

func TestCoordinateSystemValidation(t *testing.T) {
	valid := []string{"local", "enu", "gps"}
	invalid := []string{"", "LOCAL", "wgs84", "cartesian"}

	for _, cs := range valid {
		if err := protocol.ValidateCoordinateSystem(cs); err != nil {
			t.Errorf("coordinate system %q should be valid: %v", cs, err)
		}
	}

	for _, cs := range invalid {
		if err := protocol.ValidateCoordinateSystem(cs); err == nil {
			t.Errorf("coordinate system %q should be invalid", cs)
		}
	}
}
