// Package websocket provides the /ws/mobile WebSocket handler.
//
// This endpoint is designed for future Android ARCore integration.
// It is NOT yet implemented on the client side.
//
// The mobile client will:
//  1. Connect to /ws/mobile
//  2. Send mobile_register (same API key as hubs)
//  3. Send mobile_pose (ARCore device position in shared coordinate system)
//  4. Receive ap_update broadcasts (for AR rendering)
//
// No mobile-specific logic is implemented yet — the server accepts connections,
// receives messages, and broadcasts AP updates to all connected mobile clients.
package websocket

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nahxfah/wifi-hunter-server/internal/protocol"
)

// MobileHandler handles WebSocket connections from future Android ARCore apps.
type MobileHandler struct {
	manager *Manager
	apiKey  string
}

// NewMobileHandler creates a MobileHandler.
func NewMobileHandler(mgr *Manager, apiKey string) *MobileHandler {
	return &MobileHandler{manager: mgr, apiKey: apiKey}
}

// ServeHTTP upgrades and handles mobile client connections.
func (h *MobileHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := h.manager.Upgrade(w, r)
	if err != nil {
		slog.Error("failed to upgrade mobile connection", "error", err.Error())
		return
	}

	clientID := fmt.Sprintf("mobile-%d", time.Now().UnixNano())
	slog.Info("mobile WebSocket connected (future ARCore endpoint)", "remote", conn.RemoteAddr().String())

	conn.SetReadLimit(maxMessageSize)
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	client := NewClient(clientID, ClientTypeMobile, conn, h.manager)
	h.manager.RegisterMobile(clientID, client)
	defer func() {
		h.manager.UnregisterMobile(clientID)
		slog.Info("mobile client disconnected", "id", clientID)
	}()

	go client.WritePump()

	// Mobile clients receive AP updates — they are registered and then
	// the server delivers ap_update broadcasts automatically.
	for {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		_, rawMsg, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				slog.Warn("mobile WebSocket error", "id", clientID, "error", err.Error())
			}
			break
		}

		msgType, err := protocol.ParseBaseMessage(rawMsg)
		if err != nil {
			continue
		}

		switch msgType {
		case protocol.MsgMobileRegister:
			var msg protocol.MobileRegisterMessage
			if err := json.Unmarshal(rawMsg, &msg); err == nil {
				if !protocol.AuthenticateAPIKey(msg.APIKey, h.apiKey) {
					sendError(conn, "UNAUTHORIZED", "Invalid API key")
					conn.Close()
					return
				}
				client.hubID = msg.HubID
				slog.Info("mobile client registered", "hub_id", msg.HubID, "device_type", msg.DeviceType)
			}

		case protocol.MsgMobilePose:
			// Future: receive device pose from ARCore and trigger location-relative rendering
			var msg protocol.MobilePoseMessage
			if err := json.Unmarshal(rawMsg, &msg); err == nil {
				slog.Debug("mobile pose received", "hub_id", msg.HubID,
					"x", msg.Position.X, "y", msg.Position.Y, "z", msg.Position.Z)
			}

		default:
			slog.Debug("unknown mobile message type", "type", string(msgType))
		}
	}
}
