// Package services provides hub lifecycle management.
package services

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nahxfah/wifi-hunter-server/internal/models"
	"github.com/nahxfah/wifi-hunter-server/internal/repository"
)

// HubState holds the in-memory runtime state for a connected hub.
type HubState struct {
	Hub              *models.Hub
	LastSeen         time.Time
	Status           models.HubStatus
	ObservationCount int
}

// HubManager manages connected hub state with thread-safe access.
type HubManager struct {
	mu                 sync.RWMutex
	hubs               map[string]*HubState // key: hub_id
	hubRepo            *repository.HubRepository
	staleThreshold     time.Duration
	offlineThreshold   time.Duration
	onStatusChange     func(hubID string, status models.HubStatus)
}

// NewHubManager creates a HubManager with the given thresholds.
func NewHubManager(
	hubRepo *repository.HubRepository,
	staleThresholdSec, offlineThresholdSec int,
	onStatusChange func(hubID string, status models.HubStatus),
) *HubManager {
	return &HubManager{
		hubs:             make(map[string]*HubState),
		hubRepo:          hubRepo,
		staleThreshold:   time.Duration(staleThresholdSec) * time.Second,
		offlineThreshold: time.Duration(offlineThresholdSec) * time.Second,
		onStatusChange:   onStatusChange,
	}
}

// Register creates or updates the hub record and marks it online.
func (hm *HubManager) Register(ctx context.Context, hub *models.Hub) error {
	// Mobile devices must not be registered as stationary venue hubs
	if strings.HasPrefix(hub.HubID, "MOBILE-") || hub.DeviceType == "android" {
		return nil
	}

	now := time.Now()
	hub.Status = models.HubStatusOnline
	hub.LastSeen = &now
	hub.CoordinateSystem = "local"

	// Check DB first: if DB already has a calibrated position for this hub, preserve it!
	if existing, err := hm.hubRepo.GetByID(ctx, hub.HubID); err == nil && existing != nil && existing.X != nil {
		hub.X = existing.X
		hub.Y = existing.Y
		hub.Z = existing.Z
		if existing.CoordinateSystem != "" {
			hub.CoordinateSystem = existing.CoordinateSystem
		}
	}

	if err := hm.hubRepo.Upsert(ctx, hub); err != nil {
		return err
	}

	hm.mu.Lock()
	hm.hubs[hub.HubID] = &HubState{
		Hub:      hub,
		LastSeen: now,
		Status:   models.HubStatusOnline,
	}
	hm.mu.Unlock()

	slog.Info("hub registered", "hub_id", hub.HubID, "device_type", hub.DeviceType, "platform", hub.Platform)
	return nil
}

// Heartbeat updates the hub's last-seen time.
func (hm *HubManager) Heartbeat(ctx context.Context, hubID string) {
	now := time.Now()

	hm.mu.Lock()
	state, ok := hm.hubs[hubID]
	if ok {
		state.LastSeen = now
		if state.Status != models.HubStatusOnline {
			state.Status = models.HubStatusOnline
		}
	}
	hm.mu.Unlock()

	// Update DB asynchronously to avoid blocking the WebSocket read loop
	go func() {
		_ = hm.hubRepo.UpdateStatus(context.Background(), hubID, models.HubStatusOnline, now)
	}()
}

// UpdatePosition stores the hub's physical position.
func (hm *HubManager) UpdatePosition(ctx context.Context, hubID string, x, y, z float64, cs string) error {
	// Mobile devices are roving clients, not stationary venue hubs
	if strings.HasPrefix(hubID, "MOBILE-") {
		return nil
	}

	if err := hm.hubRepo.UpdatePosition(ctx, hubID, x, y, z, cs); err != nil {
		return err
	}

	hm.mu.Lock()
	if state, ok := hm.hubs[hubID]; ok {
		state.Hub.X = &x
		state.Hub.Y = &y
		state.Hub.Z = &z
		state.Hub.CoordinateSystem = cs
	} else {
		now := time.Now()
		hm.hubs[hubID] = &HubState{
			Hub: &models.Hub{
				HubID:            hubID,
				DeviceType:       "laptop",
				Platform:         "windows",
				Version:          "1.0.0",
				CoordinateSystem: cs,
				X:                &x,
				Y:                &y,
				Z:                &z,
				Status:           models.HubStatusOnline,
				LastSeen:         &now,
			},
			LastSeen: now,
			Status:   models.HubStatusOnline,
		}
	}
	hm.mu.Unlock()

	slog.Info("hub position updated", "hub_id", hubID, "x", x, "y", y, "z", z)
	return nil
}

// GetState returns a copy of the hub state if it exists.
func (hm *HubManager) GetState(hubID string) (*HubState, bool) {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	state, ok := hm.hubs[hubID]
	if !ok {
		return nil, false
	}
	stateCopy := *state
	return &stateCopy, true
}

// ListStates returns copies of all currently tracked hub states.
func (hm *HubManager) ListStates() []*HubState {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	res := make([]*HubState, 0, len(hm.hubs))
	for _, s := range hm.hubs {
		cpy := *s
		res = append(res, &cpy)
	}
	return res
}

// ListAllHubs returns all hubs known to the server, combining database records with live runtime state.
func (hm *HubManager) ListAllHubs(ctx context.Context) []*HubState {
	dbHubs, err := hm.hubRepo.List(ctx)
	if err != nil {
		slog.Error("error querying hubs from DB", "error", err)
	}

	hm.mu.RLock()
	defer hm.mu.RUnlock()

	stateMap := make(map[string]*HubState, len(dbHubs))
	for _, h := range dbHubs {
		lastSeen := time.Now()
		if h.LastSeen != nil {
			lastSeen = *h.LastSeen
		}
		stateMap[h.HubID] = &HubState{
			Hub:      h,
			LastSeen: lastSeen,
			Status:   h.Status,
		}
	}

	// Overlay in-memory state (live status, observation count, position)
	for id, s := range hm.hubs {
		if existing, ok := stateMap[id]; ok {
			existing.Status = s.Status
			existing.LastSeen = s.LastSeen
			existing.ObservationCount = s.ObservationCount
			if s.Hub != nil && s.Hub.X != nil {
				existing.Hub.X = s.Hub.X
				existing.Hub.Y = s.Hub.Y
				existing.Hub.Z = s.Hub.Z
				existing.Hub.CoordinateSystem = s.Hub.CoordinateSystem
			}
		} else {
			cpy := *s
			stateMap[id] = &cpy
		}
	}

	res := make([]*HubState, 0, len(stateMap))
	for _, s := range stateMap {
		res = append(res, s)
	}
	return res
}

// IncrementObservations adds to the observation count for the given hub.
func (hm *HubManager) IncrementObservations(hubID string, count int) {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	if state, ok := hm.hubs[hubID]; ok {
		state.ObservationCount += count
	}
}

// GetPosition returns the hub's current position if configured.
func (hm *HubManager) GetPosition(hubID string) (x, y, z float64, cs string, ok bool) {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	state, exists := hm.hubs[hubID]
	if !exists {
		return 0, 0, 0, "", false
	}
	hub := state.Hub
	if hub.X == nil || hub.Y == nil || hub.Z == nil {
		return 0, 0, 0, "", false
	}
	return *hub.X, *hub.Y, *hub.Z, hub.CoordinateSystem, true
}

// Remove removes a hub from the in-memory state (called on disconnect).
func (hm *HubManager) Remove(hubID string) {
	hm.mu.Lock()
	delete(hm.hubs, hubID)
	hm.mu.Unlock()
}

// ConnectedCount returns the number of currently tracked hubs.
func (hm *HubManager) ConnectedCount() int {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	return len(hm.hubs)
}

// MonitorStatus runs as a background goroutine that periodically checks hub
// last-seen times and updates ONLINE/STALE/OFFLINE status.
func (hm *HubManager) MonitorStatus(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hm.checkStatuses(ctx)
		}
	}
}

func (hm *HubManager) checkStatuses(ctx context.Context) {
	now := time.Now()

	hm.mu.Lock()
	changes := make(map[string]models.HubStatus)
	for hubID, state := range hm.hubs {
		age := now.Sub(state.LastSeen)
		var newStatus models.HubStatus
		switch {
		case age > hm.offlineThreshold:
			newStatus = models.HubStatusOffline
		case age > hm.staleThreshold:
			newStatus = models.HubStatusStale
		default:
			newStatus = models.HubStatusOnline
		}
		if newStatus != state.Status {
			state.Status = newStatus
			changes[hubID] = newStatus
		}
	}
	hm.mu.Unlock()

	for hubID, status := range changes {
		slog.Info("hub status changed", "hub_id", hubID, "status", string(status))
		go func(id string, s models.HubStatus) {
			_ = hm.hubRepo.UpdateStatus(context.Background(), id, s, now)
		}(hubID, status)

		if hm.onStatusChange != nil {
			hm.onStatusChange(hubID, status)
		}
	}
}
