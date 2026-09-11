// Package websocket provides the /ws/hub WebSocket handler.
package websocket

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nahxfah/wifi-hunter-server/internal/models"
	"github.com/nahxfah/wifi-hunter-server/internal/protocol"
	"github.com/nahxfah/wifi-hunter-server/internal/services"
)

// HubHandler handles WebSocket connections from Windows hub agents.
type HubHandler struct {
	manager    *Manager
	hubManager *services.HubManager
	obsSvc     *services.ObservationService
	apiKey     string
}

// NewHubHandler creates a HubHandler.
func NewHubHandler(
	mgr *Manager,
	hubMgr *services.HubManager,
	obsSvc *services.ObservationService,
	apiKey string,
) *HubHandler {
	return &HubHandler{
		manager:    mgr,
		hubManager: hubMgr,
		obsSvc:     obsSvc,
		apiKey:     apiKey,
	}
}

// ServeHTTP upgrades the HTTP connection to WebSocket and handles the hub lifecycle.
func (h *HubHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := h.manager.Upgrade(w, r)
	if err != nil {
		slog.Error("failed to upgrade hub connection", "error", err.Error())
		return
	}

	slog.Info("hub WebSocket connected", "remote", conn.RemoteAddr().String())

	conn.SetReadLimit(maxMessageSize)
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	// First message MUST be hub_register
	_, rawMsg, err := conn.ReadMessage()
	if err != nil {
		slog.Warn("hub disconnected before registering", "error", err.Error())
		conn.Close()
		return
	}

	msgType, err := protocol.ParseBaseMessage(rawMsg)
	if err != nil || msgType != protocol.MsgHubRegister {
		sendError(conn, "INVALID_MESSAGE", "First message must be hub_register")
		conn.Close()
		return
	}

	var regMsg protocol.HubRegisterMessage
	if err := json.Unmarshal(rawMsg, &regMsg); err != nil {
		sendError(conn, "PARSE_ERROR", "Failed to parse hub_register message")
		conn.Close()
		return
	}

	// Validate registration message
	if err := protocol.ValidateHubRegister(&regMsg); err != nil {
		sendError(conn, "INVALID_MESSAGE", err.Error())
		conn.Close()
		return
	}

	// Authenticate API key (constant-time comparison)
	if !protocol.AuthenticateAPIKey(regMsg.APIKey, h.apiKey) {
		sendError(conn, "UNAUTHORIZED", "Invalid API key")
		slog.Warn("hub authentication failed", "hub_id", regMsg.HubID)
		conn.Close()
		return
	}

	// Register the hub
	hub := &models.Hub{
		HubID:      regMsg.HubID,
		DeviceType: regMsg.DeviceType,
		Platform:   regMsg.Platform,
		Version:    regMsg.Version,
		Status:     models.HubStatusOnline,
	}
	if err := h.hubManager.Register(r.Context(), hub); err != nil {
		slog.Error("failed to register hub", "hub_id", regMsg.HubID, "error", err.Error())
		sendError(conn, "INTERNAL_ERROR", "Failed to register hub")
		conn.Close()
		return
	}

	// Create client and register with manager
	client := NewClient(regMsg.HubID, ClientTypeHub, conn, h.manager)
	client.hubID = regMsg.HubID
	h.manager.RegisterHub(regMsg.HubID, client)
	defer func() {
		h.manager.UnregisterHub(regMsg.HubID)
		h.hubManager.Remove(regMsg.HubID)
		slog.Info("hub disconnected", "hub_id", regMsg.HubID)
	}()

	// Send registered acknowledgement
	ack := &protocol.HubRegisteredMessage{
		Type:       protocol.MsgHubRegistered,
		HubID:      regMsg.HubID,
		ServerTime: time.Now().UTC(),
		Status:     "ok",
	}
	if data, err := json.Marshal(ack); err == nil {
		client.send <- data
	}

	// Start write pump in background
	go client.WritePump()

	// Read loop
	for {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		_, rawMsg, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				slog.Warn("hub WebSocket error", "hub_id", regMsg.HubID, "error", err.Error())
			}
			break
		}

		msgType, err := protocol.ParseBaseMessage(rawMsg)
		if err != nil {
			slog.Warn("failed to parse message type", "hub_id", regMsg.HubID, "error", err.Error())
			continue
		}

		switch msgType {
		case protocol.MsgWiFiObservations:
			h.handleObservations(r.Context(), client, regMsg.HubID, rawMsg)

		case protocol.MsgHeartbeat:
			h.handleHeartbeat(client, regMsg.HubID, rawMsg)

		default:
			slog.Warn("unknown message type from hub",
				"hub_id", regMsg.HubID,
				"type", string(msgType),
			)
			sendClientError(client, "UNKNOWN_MESSAGE_TYPE", "Unknown message type: "+string(msgType))
		}
	}
}

func (h *HubHandler) handleObservations(ctx context.Context, client *Client, hubID string, rawMsg []byte) {
	var msg protocol.WiFiObservationsMessage
	if err := json.Unmarshal(rawMsg, &msg); err != nil {
		slog.Warn("failed to parse wifi_observations", "hub_id", hubID, "error", err.Error())
		sendClientError(client, "PARSE_ERROR", "Failed to parse wifi_observations")
		return
	}

	if err := h.obsSvc.ProcessObservations(ctx, &msg); err != nil {
		slog.Warn("observation processing failed", "hub_id", hubID, "error", err.Error())
		sendClientError(client, "PROCESSING_ERROR", err.Error())
	}
}

func (h *HubHandler) handleHeartbeat(client *Client, hubID string, rawMsg []byte) {
	var msg protocol.HeartbeatMessage
	if err := json.Unmarshal(rawMsg, &msg); err != nil {
		slog.Warn("failed to parse heartbeat", "hub_id", hubID)
		return
	}

	h.hubManager.Heartbeat(context.Background(), hubID)

	ack := &protocol.HeartbeatAckMessage{
		Type:       protocol.MsgHeartbeatAck,
		ServerTime: time.Now().UTC(),
	}
	data, _ := json.Marshal(ack)
	client.Send(data)
}

// sendError sends an error message and closes the connection.
func sendError(conn *websocket.Conn, code, message string) {
	errMsg := &protocol.ErrorMessage{
		Type:    protocol.MsgError,
		Code:    code,
		Message: message,
	}
	data, _ := json.Marshal(errMsg)
	conn.SetWriteDeadline(time.Now().Add(writeWait))
	conn.WriteMessage(websocket.TextMessage, data)
}

// sendClientError sends an error message to an established client.
func sendClientError(client *Client, code, message string) {
	errMsg := &protocol.ErrorMessage{
		Type:    protocol.MsgError,
		Code:    code,
		Message: message,
	}
	data, _ := json.Marshal(errMsg)
	client.Send(data)
}
