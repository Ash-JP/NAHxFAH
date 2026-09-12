// Package services orchestrates the full observation processing pipeline.
//
// Pipeline (per wifi_observations message):
//
//  1. Validate message
//  2. Verify hub is registered and authenticated
//  3. Verify hub has a configured position
//  4. Normalize BSSID (uppercase, validate format)
//  5. Deduplicate observations per scan (same BSSID from same interface)
//  6. Store raw observations in PostgreSQL (raw RSSI preserved as-is)
//  7. Ensure AP record exists
//  8. Update RSSI smoothing state (in-memory, separate from raw)
//  9. Aggregate recent observations for this BSSID
// 10. Run localization if enough spatially-diverse hubs contributed
// 11. Update AP localization in PostgreSQL
// 12. Broadcast AP update to WebSocket clients
package services

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nahxfah/wifi-hunter-server/internal/models"
	"github.com/nahxfah/wifi-hunter-server/internal/protocol"
	"github.com/nahxfah/wifi-hunter-server/internal/repository"
)

// BroadcastFunc is the function signature for broadcasting AP updates.
type BroadcastFunc func(msg *protocol.APUpdateMessage)

// ObservationService orchestrates the full observation processing pipeline.
type ObservationService struct {
	mu            sync.Mutex
	hubManager    *HubManager
	sigProc       *SignalProcessor
	localizer     *LocalizationEngine
	obsRepo       *repository.ObservationRepository
	apRepo        *repository.AccessPointRepository
	broadcast     BroadcastFunc
	bssidHashing  bool
	serverSecret  string
	windowSeconds int

	// in-memory cache of recent hub observations per BSSID for localization
	// key: bssid → []HubObservation
	obsCache     map[string][]HubObservation
	obsCacheMu   sync.Mutex
}

// ObservationServiceConfig holds configuration for the observation service.
type ObservationServiceConfig struct {
	BSSIDHashing  bool
	ServerSecret  string
	WindowSeconds int
}

// NewObservationService creates a new ObservationService.
func NewObservationService(
	hubManager *HubManager,
	sigProc *SignalProcessor,
	localizer *LocalizationEngine,
	obsRepo *repository.ObservationRepository,
	apRepo *repository.AccessPointRepository,
	cfg ObservationServiceConfig,
	broadcast BroadcastFunc,
) *ObservationService {
	return &ObservationService{
		hubManager:    hubManager,
		sigProc:       sigProc,
		localizer:     localizer,
		obsRepo:       obsRepo,
		apRepo:        apRepo,
		broadcast:     broadcast,
		bssidHashing:  cfg.BSSIDHashing,
		serverSecret:  cfg.ServerSecret,
		windowSeconds: cfg.WindowSeconds,
		obsCache:      make(map[string][]HubObservation),
	}
}

// ProcessObservations handles an incoming wifi_observations WebSocket message.
func (s *ObservationService) ProcessObservations(ctx context.Context, msg *protocol.WiFiObservationsMessage) error {
	// Step 1: Validate message structure
	if err := protocol.ValidateWiFiObservations(msg); err != nil {
		return fmt.Errorf("invalid message: %w", err)
	}

	// Step 2: Determine observation position
	var hubX, hubY, hubZ float64
	if strings.HasPrefix(msg.HubID, "MOBILE-") {
		// Roving mobile phone: use current pose directly for observation cache
		hubX = msg.Position.X
		hubY = msg.Position.Y
		hubZ = msg.Position.Z
		// Note: Do NOT add mobile phones to hubManager, as they are not stationary venue hubs
	} else {
		// Stationary venue hub: prioritize calibrated position from hubManager
		var hasPos bool
		hubX, hubY, hubZ, _, hasPos = s.hubManager.GetPosition(msg.HubID)
		if !hasPos {
			hubX = msg.Position.X
			hubY = msg.Position.Y
			hubZ = msg.Position.Z
			if hubX != 0 || hubY != 0 || hubZ != 0 {
				_ = s.hubManager.UpdatePosition(ctx, msg.HubID, hubX, hubY, hubZ, msg.Position.CoordinateSystem)
			}
		}
		s.hubManager.IncrementObservations(msg.HubID, len(msg.Observations))
	}

	// Step 3: Deduplicate observations by BSSID within this scan
	seen := make(map[string]bool)
	var deduped []*protocol.ObservationEntry
	for i := range msg.Observations {
		entry := &msg.Observations[i]
		normalized, err := protocol.ValidateAndNormalizeBSSID(entry.BSSID)
		if err != nil {
			slog.Warn("skipping invalid BSSID", "bssid", entry.BSSID, "error", err.Error())
			continue
		}
		entry.BSSID = normalized
		if seen[normalized] {
			continue // duplicate in this scan
		}
		seen[normalized] = true
		deduped = append(deduped, entry)
	}

	slog.Info("observations received", "hub_id", msg.HubID, "count", len(deduped))

	// Step 4: Build insert batch
	var insertBatch []*models.InsertObservation
	for _, entry := range deduped {
		// Validate RSSI if present
		if entry.RSSIDbm != nil {
			if err := protocol.ValidateRSSI(*entry.RSSIDbm); err != nil {
				slog.Warn("invalid RSSI, setting null", "bssid", entry.BSSID, "rssi", *entry.RSSIDbm, "error", err.Error())
				entry.RSSIDbm = nil
			}
		}

		bssid := s.maybehashBSSID(entry.BSSID)
		ssidStr := entry.SSID
		var ssidPtr *string
		if ssidStr != "" {
			ssidPtr = &ssidStr
		}

		obs := &models.InsertObservation{
			HubID:        msg.HubID,
			BSSID:        bssid,
			SSID:         ssidPtr,
			RSSIDbm:      entry.RSSIDbm,
			LinkQuality:  entry.LinkQuality,
			FrequencyMHz: entry.FrequencyMHz,
			Channel:      entry.Channel,
			X:            &hubX,
			Y:            &hubY,
			Z:            &hubZ,
			Timestamp:    msg.Timestamp,
		}
		insertBatch = append(insertBatch, obs)
	}

	// Step 5: Bulk insert raw observations (raw RSSI preserved forever)
	if len(insertBatch) > 0 {
		if err := s.obsRepo.BulkInsert(ctx, insertBatch); err != nil {
			slog.Error("failed to bulk insert observations", "error", err.Error())
			// Don't abort — continue with in-memory processing
		}
	}

	// Step 6: For each observation, update AP state and (maybe) localize
	for _, entry := range deduped {
		bssid := s.maybehashBSSID(entry.BSSID)
		ssidStr := entry.SSID
		var ssidPtr *string
		if ssidStr != "" && ssidStr != "<hidden>" {
			ssidPtr = &ssidStr
		}

		// Ensure AP record exists
		if err := s.apRepo.EnsureExists(ctx, bssid, ssidPtr); err != nil {
			slog.Warn("failed to ensure AP exists", "bssid", bssid, "error", err.Error())
		}

		// Update RSSI smoothing (only if RSSI is valid)
		if entry.RSSIDbm != nil {
			rawRSSI := float64(*entry.RSSIDbm)
			smoothed := s.sigProc.UpdateAndGetSmoothed(msg.HubID, bssid, rawRSSI)

			freq := 2412
			if entry.FrequencyMHz != nil && *entry.FrequencyMHz > 0 {
				freq = *entry.FrequencyMHz
			}
			// Update in-memory observation cache for localization
			s.updateObsCache(bssid, msg.HubID, Point3D{X: hubX, Y: hubY, Z: hubZ}, smoothed, rawRSSI, freq)
		}
	}

	// Step 7: Run localization for affected BSSIDs
	for _, entry := range deduped {
		if entry.RSSIDbm == nil {
			continue // can't localize without RSSI
		}
		bssid := s.maybehashBSSID(entry.BSSID)
		go s.maybeLocalize(bssid, entry.SSID)
	}

	return nil
}

// updateObsCache adds a hub observation to the in-memory sliding window cache.
func (s *ObservationService) updateObsCache(bssid, hubID string, pos Point3D, smoothed, raw float64, freq int) {
	s.obsCacheMu.Lock()
	defer s.obsCacheMu.Unlock()

	now := time.Now()
	windowStart := now.Add(-time.Duration(s.windowSeconds) * time.Second)

	// Find existing entry for this hub+bssid
	existing := s.obsCache[bssid]
	var updated []HubObservation
	var found bool

	for _, o := range existing {
		if o.LastSeen.Before(windowStart) {
			continue // expire
		}
		// If from same hub and within 0.5m: update existing observation at this vantage point
		if o.HubID == hubID && Distance3D(o.HubPosition, pos) < 0.5 {
			o.SmoothedRSSI = smoothed
			o.RawRSSIs = append(o.RawRSSIs, raw)
			if len(o.RawRSSIs) > 50 { // cap raw history
				o.RawRSSIs = o.RawRSSIs[len(o.RawRSSIs)-50:]
			}
			o.Count++
			o.LastSeen = now
			o.HubPosition = pos
			o.FrequencyMHz = freq
			updated = append(updated, o)
			found = true
		} else {
			updated = append(updated, o)
		}
	}

	if !found {
		updated = append(updated, HubObservation{
			HubID:        hubID,
			HubPosition:  pos,
			SmoothedRSSI: smoothed,
			RawRSSIs:     []float64{raw},
			Count:        1,
			LastSeen:     now,
			FrequencyMHz: freq,
		})
	}

	// Cap at 30 observations per BSSID to prevent unbounded growth
	if len(updated) > 30 {
		updated = updated[len(updated)-30:]
	}

	s.obsCache[bssid] = updated
}

// maybeLocalize attempts to localize an AP if sufficient data is available.
func (s *ObservationService) maybeLocalize(bssid, ssid string) {
	s.obsCacheMu.Lock()
	hubObs := make([]HubObservation, len(s.obsCache[bssid]))
	copy(hubObs, s.obsCache[bssid])
	s.obsCacheMu.Unlock()

	ctx := context.Background()

	// Get total observation count from DB (approximate — avoid a DB call per AP)
	ap, err := s.apRepo.GetByBSSID(ctx, bssid)
	var totalObs int64
	if err == nil && ap != nil {
		totalObs = ap.ObservationCount
	}

	result, err := s.localizer.Localize(bssid, hubObs, totalObs+int64(len(hubObs)))
	if err != nil {
		slog.Error("localization failed", "bssid", bssid, "error", err.Error())
		return
	}

	// Update AP in database
	if result.Status == "localized" || result.Status == "unstable" {
		err = s.apRepo.UpdateLocalization(ctx,
			bssid,
			result.Position.X, result.Position.Y, result.Position.Z,
			"local",
			result.Confidence,
			result.ErrorRadiusM,
			result.HubCount,
			models.APStatus(result.Status),
		)
		if err != nil {
			slog.Error("failed to update AP localization", "bssid", bssid, "error", err.Error())
		}
	}

	// Update observation meta
	_ = s.apRepo.UpdateObservationMeta(ctx, bssid, time.Now(), result.HubCount)

	// Broadcast meaningful update
	if s.broadcast != nil && (result.Status == "localized" || result.Status == "unstable" || result.Status == "insufficient_data") {
		ssidDisplay := ssid
		if ssidDisplay == "" {
			ssidDisplay = "<hidden>"
		}

		msg := &protocol.APUpdateMessage{
			Type: protocol.MsgAPUpdate,
			AP: protocol.APUpdatePayload{
				BSSID:            bssid,
				SSID:             ssidDisplay,
				CoordinateSystem: "local",
				Confidence:       result.Confidence,
				ErrorRadiusM:     result.ErrorRadiusM,
				ObservationCount: result.ObservationCount,
				HubCount:         result.HubCount,
				Status:           result.Status,
				Quality:          result.Quality,
				LastSeen:         time.Now(),
			},
		}

		if result.Status == "localized" || result.Status == "unstable" {
			msg.AP.Position = &protocol.APPosition{
				X: result.Position.X,
				Y: result.Position.Y,
				Z: result.Position.Z,
			}
		}

		s.broadcast(msg)
	}
}

// maybehashBSSID returns a hashed BSSID when hashing is enabled.
func (s *ObservationService) maybehashBSSID(bssid string) string {
	if !s.bssidHashing {
		return bssid
	}
	h := sha256.Sum256([]byte(bssid + s.serverSecret))
	return fmt.Sprintf("%x", h)
}

// CleanupOldObservations removes raw observations older than retentionHours.
// Called by the background cleanup worker. AP state is never deleted.
func (s *ObservationService) CleanupOldObservations(ctx context.Context, retentionHours int) (int64, error) {
	cutoff := time.Now().Add(-time.Duration(retentionHours) * time.Hour)
	deleted, err := s.obsRepo.DeleteOlderThan(ctx, cutoff)
	if err != nil {
		return 0, err
	}
	if deleted > 0 {
		slog.Info("cleaned up old observations", "deleted", deleted, "older_than", cutoff)
	}
	return deleted, nil
}
