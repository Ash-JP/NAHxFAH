// Package websocket provides the universal /ws WebSocket handler.
//
// When client apps (e.g. mobile apps, AR apps, generic dashboards) connect to /ws,
// this handler accepts the connection, registers the client to receive real-time
// ap_update broadcasts, sends the current snapshot of localized APs, and
// auto-dispatches any incoming message types (mobile_register, mobile_pose,
// hub_register, wifi_observations, heartbeat).
package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nahxfah/wifi-hunter-server/internal/models"
	"github.com/nahxfah/wifi-hunter-server/internal/protocol"
	"github.com/nahxfah/wifi-hunter-server/internal/repository"
	"github.com/nahxfah/wifi-hunter-server/internal/services"
)

// UniversalHandler handles WebSocket connections at /ws.
type UniversalHandler struct {
	manager       *Manager
	hubManager    *services.HubManager
	obsSvc        *services.ObservationService
	apRepo        *repository.AccessPointRepository
	anchorRepo    *repository.AnchorRepository
	apiKey        string
	dashboardHTML []byte
}

// NewUniversalHandler creates a UniversalHandler.
func NewUniversalHandler(
	mgr *Manager,
	hubMgr *services.HubManager,
	obsSvc *services.ObservationService,
	apRepo *repository.AccessPointRepository,
	anchorRepo *repository.AnchorRepository,
	apiKey string,
	dashboardHTML []byte,
) *UniversalHandler {
	return &UniversalHandler{
		manager:       mgr,
		hubManager:    hubMgr,
		obsSvc:        obsSvc,
		apRepo:        apRepo,
		anchorRepo:    anchorRepo,
		apiKey:        apiKey,
		dashboardHTML: dashboardHTML,
	}
}

// ServeHTTP upgrades and handles connections to /ws.
func (h *UniversalHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// If visited via regular HTTP browser GET without WebSocket Upgrade header, serve dashboard HTML
	if r.Header.Get("Upgrade") != "websocket" && len(h.dashboardHTML) > 0 {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(h.dashboardHTML)
		return
	}

	conn, err := h.manager.Upgrade(w, r)
	if err != nil {
		slog.Error("failed to upgrade /ws connection", "error", err.Error(), "remote", r.RemoteAddr)
		return
	}

	clientID := fmt.Sprintf("app-%d", time.Now().UnixNano())
	slog.Info("app client connected to /ws", "id", clientID, "remote", conn.RemoteAddr().String())

	conn.SetReadLimit(maxMessageSize)
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	// Register as mobile client so it receives all live ap_update broadcasts
	client := NewClient(clientID, ClientTypeMobile, conn, h.manager)
	h.manager.RegisterMobile(clientID, client)
	defer func() {
		h.manager.UnregisterMobile(clientID)
		slog.Info("app client disconnected from /ws", "id", clientID)
	}()

	go client.WritePump()

	// Send immediate snapshot of all known access points and venue hubs
	go h.sendInitialAPSnapshot(client)
	go h.sendInitialHubsSnapshot(client)

	// Read loop: auto-multiplex any incoming messages from the app
	for {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		_, rawMsg, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				slog.Debug("/ws client closed connection", "id", clientID, "error", err.Error())
			}
			break
		}

		msgType, err := protocol.ParseBaseMessage(rawMsg)
		if err != nil {
			slog.Debug("non-protocol message received on /ws", "id", clientID, "raw", string(rawMsg))
			continue
		}

		slog.Info("received message on /ws", "id", clientID, "type", string(msgType))

		switch msgType {
		case protocol.MsgMobileRegister:
			var msg protocol.MobileRegisterMessage
			if err := json.Unmarshal(rawMsg, &msg); err == nil {
				id := msg.GetID()
				client.hubID = id
				version := msg.AppVersion
				if version == "" {
					version = msg.Version
				}
				h.hubManager.Register(context.Background(), &models.Hub{
					HubID:      id,
					DeviceType: msg.DeviceType,
					Platform:   msg.Platform,
					Version:    version,
					Status:     models.HubStatusOnline,
				})
				ack := map[string]interface{}{
					"type":        "mobile_registered",
					"device_id":   id,
					"hub_id":      id,
					"status":      "ok",
					"server_time": time.Now().UTC(),
				}
				sendJSON(client, ack)
				slog.Info("mobile app registered on /ws", "device_id", id, "platform", msg.Platform)
			}

		case protocol.MsgMobilePose:
			var msg protocol.MobilePoseMessage
			if err := json.Unmarshal(rawMsg, &msg); err == nil {
				id := msg.DeviceID
				if id == "" {
					id = msg.HubID
				}
				slog.Debug("mobile pose received on /ws", "id", id, "x", msg.Position.X, "y", msg.Position.Y, "z", msg.Position.Z)
			}

		case protocol.MsgUpdateHubPosition:
			var updateMsg protocol.UpdateHubPositionMessage
			if err := json.Unmarshal(rawMsg, &updateMsg); err == nil {
				cs := updateMsg.CoordinateSystem
				if cs == "" {
					cs = "local"
				}
				slog.Info("hub position updated from mobile AR",
					"hub_id", updateMsg.HubID,
					"x", updateMsg.X,
					"y", updateMsg.Y,
					"z", updateMsg.Z,
				)
				_ = h.hubManager.UpdatePosition(context.Background(), updateMsg.HubID, updateMsg.X, updateMsg.Y, updateMsg.Z, cs)

				// 1. Notify the hub agent so it updates its config and saves to config.json
				hubPosMsg := protocol.HubPositionUpdateMessage{
					Type:  protocol.MsgHubPositionUpdate,
					HubID: updateMsg.HubID,
					Position: protocol.HubPositionPayload{
						CoordinateSystem: cs,
						X:                updateMsg.X,
						Y:                updateMsg.Y,
						Z:                updateMsg.Z,
					},
				}
				h.manager.SendToHub(updateMsg.HubID, hubPosMsg)

				// 2. Broadcast updated hub state to all mobile and dashboard clients
				hubUpdateMsg := protocol.HubUpdateMessage{
					Type: protocol.MsgHubUpdate,
					Hub: protocol.HubPayload{
						HubID:            updateMsg.HubID,
						Status:           "online",
						CoordinateSystem: cs,
						Position: &protocol.APPosition{
							X: updateMsg.X,
							Y: updateMsg.Y,
							Z: updateMsg.Z,
						},
					},
				}
				h.manager.BroadcastToAll(hubUpdateMsg)
			}

		case protocol.MsgSaveAnchor:
			var anchorMsg protocol.SaveAnchorMessage
			if err := json.Unmarshal(rawMsg, &anchorMsg); err == nil {
				cs := anchorMsg.CoordinateSystem
				if cs == "" {
					cs = "local"
				}
				name := anchorMsg.Name
				if name == "" {
					name = anchorMsg.AnchorID
				}
				if h.anchorRepo != nil {
					err := h.anchorRepo.Create(context.Background(), &models.Anchor{
						AnchorID:         anchorMsg.AnchorID,
						Name:             name,
						X:                anchorMsg.X,
						Y:                anchorMsg.Y,
						Z:                anchorMsg.Z,
						CoordinateSystem: cs,
					})
					if err != nil {
						slog.Error("failed to save anchor to postgres", "anchor_id", anchorMsg.AnchorID, "err", err)
					} else {
						slog.Info("anchor persisted to postgres from mobile AR",
							"anchor_id", anchorMsg.AnchorID,
							"x", anchorMsg.X,
							"y", anchorMsg.Y,
							"z", anchorMsg.Z,
						)
					}
				}
			}

		case protocol.MsgMobileWiFiObservations:
			var mobObs protocol.MobileWiFiObservationsMessage
			if err := json.Unmarshal(rawMsg, &mobObs); err == nil {
				id := mobObs.GetID()
				cs := mobObs.Pose.CoordinateSystem
				if cs == "" {
					cs = "local"
				}
				obsMsg := protocol.WiFiObservationsMessage{
					Type:      protocol.MsgWiFiObservations,
					HubID:     id,
					Timestamp: mobObs.Timestamp,
					Position: protocol.HubPositionPayload{
						CoordinateSystem: cs,
						X:                mobObs.Pose.X,
						Y:                mobObs.Pose.Y,
						Z:                mobObs.Pose.Z,
					},
					Observations: mobObs.Observations,
				}
				go func() {
					_ = h.obsSvc.ProcessObservations(context.Background(), &obsMsg)
				}()
			}

		case protocol.MsgHubRegister:
			var regMsg protocol.HubRegisterMessage
			if err := json.Unmarshal(rawMsg, &regMsg); err == nil {
				client.hubID = regMsg.HubID
				h.hubManager.Register(context.Background(), &models.Hub{
					HubID:      regMsg.HubID,
					DeviceType: regMsg.DeviceType,
					Platform:   regMsg.Platform,
					Version:    regMsg.Version,
					Status:     models.HubStatusOnline,
				})
				ack := protocol.HubRegisteredMessage{
					Type:       protocol.MsgHubRegistered,
					HubID:      regMsg.HubID,
					Status:     "registered",
					ServerTime: time.Now().UTC(),
				}
				sendJSON(client, ack)
				slog.Info("hub registered on /ws", "hub_id", regMsg.HubID)

				// Determine authoritative position (preserved from DB or registered)
				var pos *protocol.APPosition
				if x, y, z, cs, ok := h.hubManager.GetPosition(regMsg.HubID); ok {
					pos = &protocol.APPosition{X: x, Y: y, Z: z}
					// Sync the laptop hub with its saved calibrated position
					h.manager.SendToHub(regMsg.HubID, protocol.HubPositionUpdateMessage{
						Type:  protocol.MsgHubPositionUpdate,
						HubID: regMsg.HubID,
						Position: protocol.HubPositionPayload{
							CoordinateSystem: cs,
							X:                x,
							Y:                y,
							Z:                z,
						},
					})
				}

				h.manager.BroadcastToMobile(protocol.HubUpdateMessage{
					Type: protocol.MsgHubUpdate,
					Hub: protocol.HubPayload{
						HubID:            regMsg.HubID,
						DeviceType:       regMsg.DeviceType,
						Platform:         regMsg.Platform,
						Version:          regMsg.Version,
						Status:           "online",
						CoordinateSystem: "local",
						Position:         pos,
					},
				})
			}

		case protocol.MsgWiFiObservations:
			var obsMsg protocol.WiFiObservationsMessage
			if err := json.Unmarshal(rawMsg, &obsMsg); err == nil {
				go func() {
					_ = h.obsSvc.ProcessObservations(context.Background(), &obsMsg)
				}()
			}

		case protocol.MsgHeartbeat:
			sendJSON(client, protocol.HeartbeatAckMessage{
				Type:       protocol.MsgHeartbeatAck,
				ServerTime: time.Now().UTC(),
			})

		default:
			slog.Debug("unhandled message type on /ws", "type", string(msgType))
		}
	}
}

// sendInitialAPSnapshot pushes all current AP positions to the client upon connect.
func (h *UniversalHandler) sendInitialAPSnapshot(client *Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	aps, err := h.apRepo.List(ctx, 100, 0)
	if err != nil {
		slog.Debug("could not fetch AP snapshot for /ws client", "error", err.Error())
		return
	}

	for _, ap := range aps {
		ssid := "<hidden>"
		if ap.SSID != nil {
			ssid = *ap.SSID
		}

		var x, y, z float64
		if ap.EstimatedX != nil {
			x = *ap.EstimatedX
		}
		if ap.EstimatedY != nil {
			y = *ap.EstimatedY
		}
		if ap.EstimatedZ != nil {
			z = *ap.EstimatedZ
		}

		var conf, errRadius float64
		if ap.Confidence != nil {
			conf = *ap.Confidence
		}
		if ap.ErrorRadiusM != nil {
			errRadius = *ap.ErrorRadiusM
		}

		quality := "low"
		if conf >= 0.75 {
			quality = "high"
		} else if conf >= 0.45 {
			quality = "medium"
		}

		msg := protocol.APUpdateMessage{
			Type: protocol.MsgAPUpdate,
			AP: protocol.APUpdatePayload{
				BSSID: ap.BSSID,
				SSID:  ssid,
				Position: &protocol.APPosition{
					X: x,
					Y: y,
					Z: z,
				},
				CoordinateSystem: ap.CoordinateSystem,
				Confidence:       conf,
				ErrorRadiusM:     errRadius,
				Quality:          quality,
				Status:           string(ap.Status),
				HubCount:         ap.HubCount,
				ObservationCount: ap.ObservationCount,
				LastSeen:         ap.UpdatedAt,
			},
		}
		sendJSON(client, msg)
	}
}

// sendInitialHubsSnapshot pushes all active venue hubs to the client upon connect.
func (h *UniversalHandler) sendInitialHubsSnapshot(client *Client) {
	states := h.hubManager.ListAllHubs(context.Background())
	payloads := make([]protocol.HubPayload, 0, len(states))
	for _, s := range states {
		if s.Hub == nil {
			continue
		}
		var pos *protocol.APPosition
		if s.Hub.X != nil && s.Hub.Y != nil && s.Hub.Z != nil {
			pos = &protocol.APPosition{
				X: *s.Hub.X,
				Y: *s.Hub.Y,
				Z: *s.Hub.Z,
			}
		}
		payloads = append(payloads, protocol.HubPayload{
			HubID:            s.Hub.HubID,
			DeviceType:       s.Hub.DeviceType,
			Platform:         s.Hub.Platform,
			Version:          s.Hub.Version,
			Status:           string(s.Status),
			CoordinateSystem: s.Hub.CoordinateSystem,
			Position:         pos,
			ObservationCount: s.ObservationCount,
			LastSeen:         &s.LastSeen,
		})
	}
	sendJSON(client, protocol.HubsSnapshotMessage{
		Type: protocol.MsgHubsSnapshot,
		Hubs: payloads,
	})
}
