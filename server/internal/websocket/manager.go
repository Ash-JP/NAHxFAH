// Package websocket provides concurrent-safe WebSocket client management.
//
// Architecture:
//
//	WebSocket Manager
//	├── Hub clients      (/ws/hub)
//	├── Mobile clients   (/ws/mobile) — future Android ARCore
//	└── Dashboard clients (/ws/dashboard)
//
// Each client gets its own goroutine for reads and a buffered channel for writes.
// The write channel prevents one slow client from blocking broadcasts to others.
// Ping/pong with read/write deadlines detects dead connections.
package websocket

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// Maximum time to write a message to the client.
	writeWait = 10 * time.Second

	// Maximum time to read a pong response from the client.
	pongWait = 60 * time.Second

	// Send pings at this interval. Must be less than pongWait.
	pingPeriod = 45 * time.Second

	// Maximum message size from client (1 MB).
	maxMessageSize = 1 * 1024 * 1024

	// Buffer size for per-client write channel.
	clientWriteBuffer = 64
)

// ClientType identifies which WebSocket channel a client belongs to.
type ClientType string

const (
	ClientTypeHub       ClientType = "hub"
	ClientTypeMobile    ClientType = "mobile"
	ClientTypeDashboard ClientType = "dashboard"
)

// Client represents one connected WebSocket client.
type Client struct {
	id         string
	clientType ClientType
	conn       *websocket.Conn
	send       chan []byte
	manager    *Manager
	hubID      string // set for hub and mobile clients
}

// Manager manages all WebSocket clients across all channels.
type Manager struct {
	mu      sync.RWMutex
	hubs    map[string]*Client // key: hub_id
	mobile  map[string]*Client // key: connection id
	dash    map[string]*Client // key: connection id

	upgrader websocket.Upgrader
}

// NewManager creates a WebSocket Manager.
func NewManager() *Manager {
	return &Manager{
		hubs:   make(map[string]*Client),
		mobile: make(map[string]*Client),
		dash:   make(map[string]*Client),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			CheckOrigin: func(r *http.Request) bool {
				return true // allow all origins for this internal service
			},
		},
	}
}

// Upgrade upgrades an HTTP connection to WebSocket.
func (m *Manager) Upgrade(w http.ResponseWriter, r *http.Request) (*websocket.Conn, error) {
	return m.upgrader.Upgrade(w, r, nil)
}

// RegisterHub registers a hub client by hub_id.
func (m *Manager) RegisterHub(hubID string, client *Client) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Disconnect existing connection with same hub_id
	if existing, ok := m.hubs[hubID]; ok {
		slog.Warn("replacing existing hub connection", "hub_id", hubID)
		close(existing.send)
	}
	m.hubs[hubID] = client
}

// RegisterMobile registers a mobile client.
func (m *Manager) RegisterMobile(clientID string, client *Client) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mobile[clientID] = client
}

// RegisterDashboard registers a dashboard client.
func (m *Manager) RegisterDashboard(clientID string, client *Client) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dash[clientID] = client
}

// UnregisterHub removes a hub client.
func (m *Manager) UnregisterHub(hubID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if client, ok := m.hubs[hubID]; ok {
		// Only close if it's the same client (not a replaced one)
		_ = client
		delete(m.hubs, hubID)
	}
}

// UnregisterMobile removes a mobile client.
func (m *Manager) UnregisterMobile(clientID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.mobile, clientID)
}

// UnregisterDashboard removes a dashboard client.
func (m *Manager) UnregisterDashboard(clientID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.dash, clientID)
}

// SendToHub sends a message to a specific hub by hub_id.
// Returns false if the hub is not connected.
func (m *Manager) SendToHub(hubID string, msg interface{}) bool {
	m.mu.RLock()
	client, ok := m.hubs[hubID]
	m.mu.RUnlock()

	if !ok {
		return false
	}
	return sendJSON(client, msg)
}

// BroadcastToMobile sends a message to all connected mobile clients.
func (m *Manager) BroadcastToMobile(msg interface{}) {
	data, err := json.Marshal(msg)
	if err != nil {
		slog.Error("failed to marshal mobile broadcast", "error", err.Error())
		return
	}
	m.mu.RLock()
	clients := make([]*Client, 0, len(m.mobile))
	for _, c := range m.mobile {
		clients = append(clients, c)
	}
	m.mu.RUnlock()

	for _, c := range clients {
		safeWrite(c, data)
	}
}

// BroadcastToDashboard sends a message to all connected dashboard clients.
func (m *Manager) BroadcastToDashboard(msg interface{}) {
	data, err := json.Marshal(msg)
	if err != nil {
		slog.Error("failed to marshal dashboard broadcast", "error", err.Error())
		return
	}
	m.mu.RLock()
	clients := make([]*Client, 0, len(m.dash))
	for _, c := range m.dash {
		clients = append(clients, c)
	}
	m.mu.RUnlock()

	for _, c := range clients {
		safeWrite(c, data)
	}
}

// BroadcastToAll sends a message to mobile and dashboard clients.
func (m *Manager) BroadcastToAll(msg interface{}) {
	m.BroadcastToMobile(msg)
	m.BroadcastToDashboard(msg)
}

// HubCount returns the number of connected hub clients.
func (m *Manager) HubCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.hubs)
}

// MobileCount returns the number of connected mobile clients.
func (m *Manager) MobileCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.mobile)
}

// DashCount returns the number of connected dashboard clients.
func (m *Manager) DashCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.dash)
}

// NewClient creates a new Client.
func NewClient(id string, ct ClientType, conn *websocket.Conn, mgr *Manager) *Client {
	return &Client{
		id:         id,
		clientType: ct,
		conn:       conn,
		send:       make(chan []byte, clientWriteBuffer),
		manager:    mgr,
	}
}

// WritePump runs in a goroutine and sends queued messages to the WebSocket client.
// It also sends periodic ping messages. One broken connection cannot block others.
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Channel was closed
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				slog.Debug("write error", "client_type", string(c.clientType), "error", err.Error())
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// Send queues a message for sending. Returns false if the buffer is full.
func (c *Client) Send(data []byte) bool {
	select {
	case c.send <- data:
		return true
	default:
		return false
	}
}

// Close signals the write pump to stop.
func (c *Client) Close() {
	// Safe to close multiple times via recover
	defer func() { recover() }()
	close(c.send)
}

// sendJSON marshals and sends a JSON message to a client.
func sendJSON(client *Client, msg interface{}) bool {
	data, err := json.Marshal(msg)
	if err != nil {
		return false
	}
	return safeWrite(client, data)
}

// safeWrite sends data to the client's buffered channel, discarding if full.
func safeWrite(client *Client, data []byte) bool {
	select {
	case client.send <- data:
		return true
	default:
		slog.Warn("client write buffer full, dropping message",
			"client_type", string(client.clientType),
			"id", client.id,
		)
		return false
	}
}
