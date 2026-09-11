// Package models defines database model types.
package models

import (
	"encoding/json"
	"time"
)

// Anchor is a named reference point in the local coordinate system.
// Primarily designed for future ARCore/mobile integration.
type Anchor struct {
	ID               int64           `json:"id"`
	AnchorID         string          `json:"anchor_id"`
	Name             string          `json:"name"`
	X                float64         `json:"x"`
	Y                float64         `json:"y"`
	Z                float64         `json:"z"`
	CoordinateSystem string          `json:"coordinate_system"`
	Metadata         json.RawMessage `json:"metadata,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
}

// CreateAnchorRequest is the request body for POST /api/anchors.
type CreateAnchorRequest struct {
	AnchorID         string          `json:"anchor_id"`
	Name             string          `json:"name"`
	X                float64         `json:"x"`
	Y                float64         `json:"y"`
	Z                float64         `json:"z"`
	CoordinateSystem string          `json:"coordinate_system"`
	Metadata         json.RawMessage `json:"metadata,omitempty"`
}
