// Package models defines database model types.
package models

import "time"

// Observation represents a single raw Wi-Fi scan entry from a hub.
// Raw RSSI values are stored as-is and NEVER overwritten with processed values.
type Observation struct {
	ID           int64     `json:"id"`
	HubID        string    `json:"hub_id"`
	BSSID        string    `json:"bssid"`
	SSID         *string   `json:"ssid,omitempty"`
	RSSIDbm      *int      `json:"rssi_dbm,omitempty"` // NULL if unavailable from driver
	LinkQuality  *int      `json:"link_quality,omitempty"`
	FrequencyMHz *int      `json:"frequency_mhz,omitempty"`
	Channel      *int      `json:"channel,omitempty"`
	X            *float64  `json:"x,omitempty"` // Hub position when captured
	Y            *float64  `json:"y,omitempty"`
	Z            *float64  `json:"z,omitempty"`
	Timestamp    time.Time `json:"timestamp"`
	IngestedAt   time.Time `json:"ingested_at"`
}

// InsertObservation is the input struct for inserting a new observation.
type InsertObservation struct {
	HubID        string
	BSSID        string
	SSID         *string
	RSSIDbm      *int
	LinkQuality  *int
	FrequencyMHz *int
	Channel      *int
	X            *float64
	Y            *float64
	Z            *float64
	Timestamp    time.Time
}
