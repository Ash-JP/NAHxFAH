// Package config loads and validates server configuration from environment variables.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

// Config holds all server configuration.
type Config struct {
	// HTTP server
	ServerHost string
	ServerPort string

	// Database
	DatabaseURL string

	// Security
	HubAPIKey    string
	BSSIDHashing bool
	ServerSecret string

	// RSSI signal processing
	RSSISmoothingAlpha float64

	// Path loss model: RSSI = A - 10*n*log10(d)
	PathLossA float64 // Reference RSSI at 1m (dBm)
	PathLossN float64 // Path loss exponent

	// Localization
	LocalizationMinHubs       int
	LocalizationWindowSeconds int
	PositionSmoothingAlpha    float64
	LocalizationGridResolution float64

	// Hub status thresholds
	HubStaleThresholdSeconds   int
	HubOfflineThresholdSeconds int

	// Observation retention
	ObservationRetentionHours int

	// Logging
	LogLevel string
}

// Load reads configuration from environment variables with defaults.
func Load() (*Config, error) {
	cfg := &Config{
		ServerHost:                 getEnv("SERVER_HOST", "0.0.0.0"),
		ServerPort:                 getEnv("SERVER_PORT", "8000"),
		DatabaseURL:                getEnv("DATABASE_URL", ""),
		HubAPIKey:                  getEnv("HUB_API_KEY", ""),
		ServerSecret:               getEnv("SERVER_SECRET", ""),
		LogLevel:                   strings.ToLower(getEnv("LOG_LEVEL", "info")),
		RSSISmoothingAlpha:         0.25,
		PathLossA:                  -45.0,
		PathLossN:                  3.0,
		LocalizationMinHubs:        3,
		LocalizationWindowSeconds:  45,
		PositionSmoothingAlpha:     0.3,
		LocalizationGridResolution: 0.5,
		HubStaleThresholdSeconds:   15,
		HubOfflineThresholdSeconds: 30,
		ObservationRetentionHours:  24,
	}

	// Parse boolean flags
	var err error
	if v := getEnv("BSSID_HASHING", "false"); v != "" {
		cfg.BSSIDHashing, err = strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("invalid BSSID_HASHING: %w", err)
		}
	}

	// Parse float64 values
	floatVars := map[string]*float64{
		"RSSI_SMOOTHING_ALPHA":       &cfg.RSSISmoothingAlpha,
		"PATH_LOSS_A":                &cfg.PathLossA,
		"PATH_LOSS_N":                &cfg.PathLossN,
		"POSITION_SMOOTHING_ALPHA":   &cfg.PositionSmoothingAlpha,
		"LOCALIZATION_GRID_RESOLUTION": &cfg.LocalizationGridResolution,
	}
	for envKey, ptr := range floatVars {
		if v := os.Getenv(envKey); v != "" {
			parsed, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid %s: %w", envKey, err)
			}
			*ptr = parsed
		}
	}

	// Parse int values
	intVars := map[string]*int{
		"LOCALIZATION_MIN_HUBS":        &cfg.LocalizationMinHubs,
		"LOCALIZATION_WINDOW_SECONDS":  &cfg.LocalizationWindowSeconds,
		"HUB_STALE_THRESHOLD_SECONDS":  &cfg.HubStaleThresholdSeconds,
		"HUB_OFFLINE_THRESHOLD_SECONDS": &cfg.HubOfflineThresholdSeconds,
		"OBSERVATION_RETENTION_HOURS":  &cfg.ObservationRetentionHours,
	}
	for envKey, ptr := range intVars {
		if v := os.Getenv(envKey); v != "" {
			parsed, err := strconv.Atoi(v)
			if err != nil {
				return nil, fmt.Errorf("invalid %s: %w", envKey, err)
			}
			*ptr = parsed
		}
	}

	// Validate required fields
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.HubAPIKey == "" {
		return nil, fmt.Errorf("HUB_API_KEY is required")
	}

	// Validate ranges
	if cfg.RSSISmoothingAlpha < 0 || cfg.RSSISmoothingAlpha > 1 {
		return nil, fmt.Errorf("RSSI_SMOOTHING_ALPHA must be between 0 and 1")
	}
	if cfg.LocalizationMinHubs < 1 {
		return nil, fmt.Errorf("LOCALIZATION_MIN_HUBS must be >= 1")
	}

	return cfg, nil
}

// Addr returns the address string for net.Listen.
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%s", c.ServerHost, c.ServerPort)
}

// LogLevelValue returns the slog.Level for the configured log level.
func (c *Config) LogLevelValue() slog.Level {
	switch c.LogLevel {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func getEnv(key, defaultValue string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return defaultValue
}
