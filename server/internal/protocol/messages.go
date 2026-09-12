// Package protocol defines all WebSocket message types for the WIFI HUNTER AR protocol.
//
// Message flow:
//
//	Hub → Server: hub_register, wifi_observations, heartbeat
//	Server → Hub: hub_registered, heartbeat_ack, error, ap_update
//	Mobile → Server: mobile_register, mobile_pose  (future)
//	Server → Mobile: ap_update, error              (future)
//	Server → Dashboard: hub_registered, hub_status, observation_stats, ap_update, server_status
package protocol

import (
	"encoding/json"
	"time"
)

// MessageType identifies the type of a WebSocket message.
type MessageType string

const (
	// Hub → Server
	MsgHubRegister      MessageType = "hub_register"
	MsgWiFiObservations MessageType = "wifi_observations"
	MsgHeartbeat        MessageType = "heartbeat"

	// Server → Hub
	MsgHubRegistered MessageType = "hub_registered"
	MsgHeartbeatAck  MessageType = "heartbeat_ack"
	MsgError         MessageType = "error"
	MsgAPUpdate      MessageType = "ap_update"

	// Mobile → Server
	MsgMobileRegister         MessageType = "mobile_register"
	MsgMobilePose             MessageType = "mobile_pose"
	MsgMobileWiFiObservations MessageType = "mobile_wifi_observations"

	// Server → Dashboard
	MsgHubStatus        MessageType = "hub_status"
	MsgObservationStats MessageType = "observation_stats"
	MsgServerStatus     MessageType = "server_status"
)

// BaseMessage contains the message type discriminator.
type BaseMessage struct {
	Type MessageType `json:"type"`
}

// ---------------------------------------------------------------------------
// Hub → Server messages
// ---------------------------------------------------------------------------

// HubRegisterMessage is the first message sent by a hub after connecting.
type HubRegisterMessage struct {
	Type       MessageType         `json:"type"`
	HubID      string              `json:"hub_id"`
	DeviceType string              `json:"device_type"`
	Platform   string              `json:"platform"`
	Version    string              `json:"version"`
	APIKey     string              `json:"api_key"`
	Position   *HubPositionPayload `json:"position,omitempty"`
}

// WiFiObservationsMessage contains a batch of scan results from a hub.
type WiFiObservationsMessage struct {
	Type         MessageType          `json:"type"`
	HubID        string               `json:"hub_id"`
	Position     HubPositionPayload   `json:"position"`
	Timestamp    time.Time            `json:"timestamp"`
	Observations []ObservationEntry   `json:"observations"`
}

// HubPositionPayload carries hub spatial coordinates.
type HubPositionPayload struct {
	CoordinateSystem string  `json:"coordinate_system"`
	X                float64 `json:"x"`
	Y                float64 `json:"y"`
	Z                float64 `json:"z"`
}

// ObservationEntry is a single AP observation within a WiFiObservationsMessage.
type ObservationEntry struct {
	BSSID        string  `json:"bssid"`
	SSID         string  `json:"ssid"`
	RSSIDbm      *int    `json:"rssi_dbm"`      // NULL when unavailable
	LinkQuality  *int    `json:"link_quality"`  // 0–100
	FrequencyMHz *int    `json:"frequency_mhz"` // Actual frequency from driver
	Channel      *int    `json:"channel"`       // NULL when unavailable
}

// HeartbeatMessage is sent by the hub every heartbeat_interval_seconds.
type HeartbeatMessage struct {
	Type      MessageType `json:"type"`
	HubID     string      `json:"hub_id"`
	Timestamp time.Time   `json:"timestamp"`
}

// ---------------------------------------------------------------------------
// Server → Hub messages
// ---------------------------------------------------------------------------

// HubRegisteredMessage is sent by the server after successful hub registration.
type HubRegisteredMessage struct {
	Type       MessageType `json:"type"`
	HubID      string      `json:"hub_id"`
	ServerTime time.Time   `json:"server_time"`
	Status     string      `json:"status"`
}

// HeartbeatAckMessage acknowledges a heartbeat.
type HeartbeatAckMessage struct {
	Type       MessageType `json:"type"`
	ServerTime time.Time   `json:"server_time"`
}

// ErrorMessage reports a protocol error to the client.
type ErrorMessage struct {
	Type    MessageType `json:"type"`
	Code    string      `json:"code"`
	Message string      `json:"message"`
}

// APUpdateMessage broadcasts an AP localization update.
// This message is consumed by hubs, dashboard, and (future) mobile AR clients.
type APUpdateMessage struct {
	Type MessageType `json:"type"`
	AP   APUpdatePayload `json:"ap"`
}

// APUpdatePayload contains the full AP localization state.
// This is the primary data structure consumed by the future Android ARCore app.
type APUpdatePayload struct {
	BSSID            string      `json:"bssid"`
	SSID             string      `json:"ssid"`
	Position         *APPosition `json:"position,omitempty"`
	CoordinateSystem string      `json:"coordinate_system"`
	Confidence       float64     `json:"confidence"`
	ErrorRadiusM     float64     `json:"error_radius_m"`
	ObservationCount int64       `json:"observation_count"`
	HubCount         int         `json:"hub_count"`
	Status           string      `json:"status"`
	Quality          string      `json:"quality"`
	LastSeen         time.Time   `json:"last_seen"`
}

// APPosition is the 3D position of an estimated AP location.
type APPosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// ---------------------------------------------------------------------------
// Future Mobile → Server messages (stubs, not implemented client-side yet)
// ---------------------------------------------------------------------------

// MobileRegisterMessage is the Android ARCore registration message.
type MobileRegisterMessage struct {
	Type       MessageType `json:"type"`
	DeviceID   string      `json:"device_id,omitempty"`
	HubID      string      `json:"hub_id,omitempty"`
	DeviceType string      `json:"device_type"`
	Platform   string      `json:"platform"`
	AppVersion string      `json:"app_version,omitempty"`
	Version    string      `json:"version,omitempty"`
	APIKey     string      `json:"api_key,omitempty"`
}

// GetID returns DeviceID if populated, otherwise HubID.
func (m *MobileRegisterMessage) GetID() string {
	if m.DeviceID != "" {
		return m.DeviceID
	}
	return m.HubID
}

// MobilePoseMessage carries the ARCore device pose in the shared coordinate system.
type MobilePoseMessage struct {
	Type        MessageType `json:"type"`
	DeviceID    string      `json:"device_id,omitempty"`
	HubID       string      `json:"hub_id,omitempty"`
	Position    APPosition  `json:"position"`
	Orientation Quaternion  `json:"orientation"`
}

// MobileWiFiObservationsMessage carries Wi-Fi observations and the device's server-world pose.
type MobileWiFiObservationsMessage struct {
	Type         MessageType         `json:"type"`
	DeviceID     string              `json:"device_id,omitempty"`
	HubID        string              `json:"hub_id,omitempty"`
	Timestamp    time.Time           `json:"timestamp"`
	Observations []ObservationEntry  `json:"observations"`
	Pose         MobilePosePayload   `json:"pose"`
}

// MobilePosePayload carries server-world coordinates of the mobile device.
type MobilePosePayload struct {
	CoordinateSystem string  `json:"coordinate_system"`
	X                float64 `json:"x"`
	Y                float64 `json:"y"`
	Z                float64 `json:"z"`
}

// GetID returns DeviceID if populated, otherwise HubID.
func (m *MobileWiFiObservationsMessage) GetID() string {
	if m.DeviceID != "" {
		return m.DeviceID
	}
	return m.HubID
}

// Quaternion represents an orientation in 3D space.
type Quaternion struct {
	QX float64 `json:"qx"`
	QY float64 `json:"qy"`
	QZ float64 `json:"qz"`
	QW float64 `json:"qw"`
}

// ---------------------------------------------------------------------------
// Dashboard messages
// ---------------------------------------------------------------------------

// HubStatusMessage broadcasts a hub status change to dashboard clients.
type HubStatusMessage struct {
	Type   MessageType `json:"type"`
	HubID  string      `json:"hub_id"`
	Status string      `json:"status"`
	LastSeen *time.Time `json:"last_seen,omitempty"`
}

// ObservationStatsMessage provides observation ingestion statistics.
type ObservationStatsMessage struct {
	Type             MessageType `json:"type"`
	HubID            string      `json:"hub_id"`
	ObservationCount int         `json:"observation_count"`
	Timestamp        time.Time   `json:"timestamp"`
}

// ServerStatusMessage broadcasts overall server health.
type ServerStatusMessage struct {
	Type            MessageType `json:"type"`
	UptimeSeconds   int64       `json:"uptime_seconds"`
	ConnectedHubs   int         `json:"connected_hubs"`
	ActiveAPs       int         `json:"active_aps"`
	LocalizedAPs    int         `json:"localized_aps"`
	DatabaseStatus  string      `json:"database_status"`
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// ParseBaseMessage extracts the message type without full deserialization.
func ParseBaseMessage(data []byte) (MessageType, error) {
	var base BaseMessage
	if err := json.Unmarshal(data, &base); err != nil {
		return "", err
	}
	return base.Type, nil
}
