// Package models defines database model types.
package models

import "time"

// HubStatus represents the connection/activity status of a hub.
type HubStatus string

const (
	HubStatusOnline  HubStatus = "online"
	HubStatusStale   HubStatus = "stale"
	HubStatusOffline HubStatus = "offline"
)

// Hub represents a Wi-Fi scanning hub (Windows laptop or future Android device).
type Hub struct {
	ID               int64      `json:"id"`
	HubID            string     `json:"hub_id"`
	DeviceType       string     `json:"device_type"`
	Platform         string     `json:"platform"`
	Version          string     `json:"version"`
	CoordinateSystem string     `json:"coordinate_system"`
	X                *float64   `json:"x,omitempty"`
	Y                *float64   `json:"y,omitempty"`
	Z                *float64   `json:"z,omitempty"`
	Latitude         *float64   `json:"latitude,omitempty"`
	Longitude        *float64   `json:"longitude,omitempty"`
	Altitude         *float64   `json:"altitude,omitempty"`
	Status           HubStatus  `json:"status"`
	LastSeen         *time.Time `json:"last_seen,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// Position returns the hub's 3D position if all coordinates are set.
func (h *Hub) Position() (x, y, z float64, ok bool) {
	if h.X != nil && h.Y != nil && h.Z != nil {
		return *h.X, *h.Y, *h.Z, true
	}
	return 0, 0, 0, false
}
