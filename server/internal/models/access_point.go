// Package models defines database model types.
package models

import "time"

// APStatus represents the localization state of an access point.
type APStatus string

const (
	APStatusUnknown          APStatus = "unknown"
	APStatusInsufficientData APStatus = "insufficient_data"
	APStatusLocalized        APStatus = "localized"
	APStatusUnstable         APStatus = "unstable"
	APStatusStale            APStatus = "stale"
)

// APQuality represents the quality tier of a localization result.
type APQuality string

const (
	APQualityLow    APQuality = "low"
	APQualityMedium APQuality = "medium"
	APQualityHigh   APQuality = "high"
)

// AccessPoint represents the aggregated state of an observed Wi-Fi AP.
type AccessPoint struct {
	ID               int64      `json:"id"`
	BSSID            string     `json:"bssid"`
	SSID             *string    `json:"ssid,omitempty"`
	EstimatedX       *float64   `json:"estimated_x,omitempty"`
	EstimatedY       *float64   `json:"estimated_y,omitempty"`
	EstimatedZ       *float64   `json:"estimated_z,omitempty"`
	CoordinateSystem string     `json:"coordinate_system"`
	Confidence       *float64   `json:"confidence,omitempty"`
	ErrorRadiusM     *float64   `json:"error_radius_m,omitempty"`
	ObservationCount int64      `json:"observation_count"`
	HubCount         int        `json:"hub_count"`
	Status           APStatus   `json:"status"`
	FirstSeen        *time.Time `json:"first_seen,omitempty"`
	LastSeen         *time.Time `json:"last_seen,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// APPosition holds estimated 3D coordinates for API responses.
type APPosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// APSummary is the API response shape for access point localization results.
type APSummary struct {
	BSSID            string      `json:"bssid"`
	SSID             string      `json:"ssid"`
	Position         *APPosition `json:"position,omitempty"`
	Confidence       float64     `json:"confidence"`
	ErrorRadiusM     float64     `json:"error_radius_m"`
	ObservationCount int64       `json:"observation_count"`
	HubCount         int         `json:"hub_count"`
	Status           APStatus    `json:"status"`
	Quality          APQuality   `json:"quality"`
	LastSeen         *time.Time  `json:"last_seen,omitempty"`
}
