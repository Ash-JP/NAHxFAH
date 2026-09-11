// Package protocol provides validation functions for incoming WebSocket messages.
package protocol

import (
	"crypto/subtle"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
)

// bssidPattern matches AA:BB:CC:DD:EE:FF (6 uppercase hex octets).
var bssidPattern = regexp.MustCompile(`^([0-9A-F]{2}:){5}[0-9A-F]{2}$`)

// hubIDPattern matches HUB-XXXXXX (3-20 uppercase alphanumeric chars after dash).
var hubIDPattern = regexp.MustCompile(`^[A-Z]+-[A-Z0-9]{3,20}$`)

// ValidateAndNormalizeBSSID normalizes a BSSID to uppercase AA:BB:CC:DD:EE:FF
// and validates the format. Returns an error if the BSSID is invalid.
func ValidateAndNormalizeBSSID(bssid string) (string, error) {
	normalized := strings.ToUpper(strings.TrimSpace(bssid))
	if !bssidPattern.MatchString(normalized) {
		return "", fmt.Errorf("invalid BSSID format: %q (expected AA:BB:CC:DD:EE:FF)", bssid)
	}
	return normalized, nil
}

// ValidateRSSI checks that RSSI is within a realistic dBm range.
// Returns an error if out of range; the caller should ignore (not clamp) the measurement.
func ValidateRSSI(rssi int) error {
	if rssi < -127 || rssi > 0 {
		return fmt.Errorf("RSSI %d dBm is outside valid range [-127, 0]", rssi)
	}
	return nil
}

// ValidateCoordinates checks that x, y, z are all finite (not NaN or Inf).
func ValidateCoordinates(x, y, z float64) error {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return fmt.Errorf("X coordinate is not a finite number: %v", x)
	}
	if math.IsNaN(y) || math.IsInf(y, 0) {
		return fmt.Errorf("Y coordinate is not a finite number: %v", y)
	}
	if math.IsNaN(z) || math.IsInf(z, 0) {
		return fmt.Errorf("Z coordinate is not a finite number: %v", z)
	}
	return nil
}

// ValidateFrequency checks that a frequency value is plausible for Wi-Fi.
// Accepts 2.4 GHz (2400–2500), 5 GHz (4900–5900), and 6 GHz (5925–7125) bands.
func ValidateFrequency(freqMHz int) error {
	if freqMHz <= 0 {
		return fmt.Errorf("invalid frequency: %d MHz", freqMHz)
	}
	// Accept any positive value — drivers may report unusual frequencies
	// in edge cases. We validate rather than silently discard.
	if freqMHz < 2400 || freqMHz > 7200 {
		return fmt.Errorf("frequency %d MHz is outside known Wi-Fi bands (2400–7200 MHz)", freqMHz)
	}
	return nil
}

// ValidateChannel checks that a Wi-Fi channel number is plausible.
func ValidateChannel(channel int) error {
	if channel < 1 || channel > 196 {
		return fmt.Errorf("channel %d is outside valid range [1, 196]", channel)
	}
	return nil
}

// ValidateHubID checks that a hub ID matches the expected format.
func ValidateHubID(hubID string) error {
	if !hubIDPattern.MatchString(hubID) {
		return fmt.Errorf("invalid hub ID format: %q", hubID)
	}
	return nil
}

// ValidateTimestamp checks that a timestamp is not too far in the past or future.
func ValidateTimestamp(ts time.Time) error {
	now := time.Now()
	if ts.Before(now.Add(-24 * time.Hour)) {
		return fmt.Errorf("timestamp %v is more than 24 hours in the past", ts)
	}
	if ts.After(now.Add(5 * time.Minute)) {
		return fmt.Errorf("timestamp %v is more than 5 minutes in the future", ts)
	}
	return nil
}

// ValidateCoordinateSystem checks that a coordinate system name is known.
func ValidateCoordinateSystem(cs string) error {
	switch cs {
	case "local", "enu", "gps":
		return nil
	default:
		return fmt.Errorf("unknown coordinate system: %q (valid: local, enu, gps)", cs)
	}
}

// AuthenticateAPIKey performs a constant-time comparison of the provided key
// against the expected key. Returns true if they match.
// IMPORTANT: Never log the provided key.
func AuthenticateAPIKey(provided, expected string) bool {
	// constant-time comparison to prevent timing attacks
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

// ValidateHubRegister validates all fields of a hub registration message.
func ValidateHubRegister(msg *HubRegisterMessage) error {
	if err := ValidateHubID(msg.HubID); err != nil {
		return err
	}
	if msg.DeviceType == "" {
		return fmt.Errorf("device_type is required")
	}
	if msg.Platform == "" {
		return fmt.Errorf("platform is required")
	}
	return nil
}

// ValidateObservationEntry validates a single Wi-Fi observation entry.
// Returns a list of field-level validation errors (non-fatal; callers may skip invalid entries).
func ValidateObservationEntry(entry *ObservationEntry) []error {
	var errs []error

	if _, err := ValidateAndNormalizeBSSID(entry.BSSID); err != nil {
		errs = append(errs, err)
	}

	if entry.RSSIDbm != nil {
		if err := ValidateRSSI(*entry.RSSIDbm); err != nil {
			errs = append(errs, err)
		}
	}

	if entry.FrequencyMHz != nil {
		if err := ValidateFrequency(*entry.FrequencyMHz); err != nil {
			errs = append(errs, err)
		}
	}

	if entry.Channel != nil {
		if err := ValidateChannel(*entry.Channel); err != nil {
			errs = append(errs, err)
		}
	}

	if entry.LinkQuality != nil {
		if *entry.LinkQuality < 0 || *entry.LinkQuality > 100 {
			errs = append(errs, fmt.Errorf("link_quality %d out of range [0,100]", *entry.LinkQuality))
		}
	}

	return errs
}

// ValidateWiFiObservations validates the outer wifi_observations message.
func ValidateWiFiObservations(msg *WiFiObservationsMessage) error {
	if err := ValidateHubID(msg.HubID); err != nil {
		return err
	}
	if err := ValidateCoordinateSystem(msg.Position.CoordinateSystem); err != nil {
		return err
	}
	if err := ValidateCoordinates(msg.Position.X, msg.Position.Y, msg.Position.Z); err != nil {
		return err
	}
	if len(msg.Observations) == 0 {
		return fmt.Errorf("observations array is empty")
	}
	return nil
}
