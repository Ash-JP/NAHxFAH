// Package websocket provides the /ws/dashboard WebSocket handler.
//
// Dashboard clients receive all server events in real-time:
//
//	hub_registered    — when a new hub connects
//	hub_status        — when hub status changes (ONLINE/STALE/OFFLINE)
//	observation_stats — after each observation batch
//	ap_update         — when AP localization changes
//	server_status     — periodic server health summary
//
// Dashboard clients are read-only — they receive broadcasts only.
package websocket

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// DashboardHandler handles WebSocket connections from dashboard clients.
type DashboardHandler struct {
	manager *Manager
}

// NewDashboardHandler creates a DashboardHandler.
func NewDashboardHandler(mgr *Manager) *DashboardHandler {
	return &DashboardHandler{manager: mgr}
}

// ServeHTTP upgrades and handles dashboard client connections.
func (h *DashboardHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := h.manager.Upgrade(w, r)
	if err != nil {
		slog.Error("failed to upgrade dashboard connection", "error", err.Error())
		return
	}

	clientID := fmt.Sprintf("dash-%d", time.Now().UnixNano())
	slog.Info("dashboard client connected", "id", clientID, "remote", conn.RemoteAddr().String())

	conn.SetReadLimit(maxMessageSize)
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	client := NewClient(clientID, ClientTypeDashboard, conn, h.manager)
	h.manager.RegisterDashboard(clientID, client)
	defer func() {
		h.manager.UnregisterDashboard(clientID)
		slog.Info("dashboard client disconnected", "id", clientID)
	}()

	go client.WritePump()

	// Dashboard is receive-only — drain any client messages (ignore content)
	for {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		if _, _, err := conn.ReadMessage(); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				slog.Debug("dashboard WebSocket error", "id", clientID, "error", err.Error())
			}
			break
		}
	}
}
