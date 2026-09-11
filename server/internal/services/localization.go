// Package services provides the Wi-Fi AP localization engine.
//
// # Algorithm Overview
//
// Input: a set of recent (hub_position, smoothed_rssi, frequency, timestamp) tuples
// for one BSSID, from multiple hubs.
//
// Steps:
//  1. Aggregate observations into a sliding time window per hub.
//  2. Estimate distance from each hub using the log-distance path loss model.
//  3. Perform a 2D grid search over the local coordinate space.
//  4. At each candidate point, compute weighted likelihood from all hub observations.
//  5. Select the candidate with maximum total likelihood (minimum weighted error).
//  6. Estimate position uncertainty from the likelihood surface.
//  7. Smooth the position estimate over time (EMA).
//  8. Detect instability (large position jumps).
//
// # Limitations
//
// RSSI is not a precise distance sensor. Accuracy is affected by walls,
// multipath, people movement, antenna orientation, frequency, interference,
// and hardware differences. All position estimates carry inherent uncertainty.
//
// This implementation is modular and designed to be replaced with particle
// filtering, Kalman filtering, fingerprinting, or ML-based approaches.
package services

import (
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"
)

// LocalizationConfig holds tunable parameters for the localization engine.
type LocalizationConfig struct {
	// MinHubs: minimum unique hubs required to attempt localization.
	MinHubs int

	// WindowSeconds: sliding time window for observation aggregation.
	WindowSeconds int

	// GridResolution: candidate grid spacing in metres.
	GridResolution float64

	// GridPaddingM: metres to extend the search grid beyond hub positions.
	GridPaddingM float64

	// PositionAlpha: EMA smoothing for position updates (0=frozen, 1=no memory).
	PositionAlpha float64

	// PathLossA: reference RSSI at 1m in dBm.
	PathLossA float64

	// PathLossN: path loss exponent.
	PathLossN float64

	// RSSISigma: assumed RSSI uncertainty in dBm for likelihood calculation.
	RSSISigma float64

	// InstabilityThresholdM: position jump threshold for instability detection.
	InstabilityThresholdM float64

	// OutlierMADThreshold: MAD modified Z-score threshold for outlier flagging.
	OutlierMADThreshold float64
}

// DefaultLocalizationConfig returns sensible starting defaults.
func DefaultLocalizationConfig() LocalizationConfig {
	return LocalizationConfig{
		MinHubs:               3,
		WindowSeconds:         45,
		GridResolution:        0.5,
		GridPaddingM:          5.0,
		PositionAlpha:         0.3,
		PathLossA:             -45.0,
		PathLossN:             3.0,
		RSSISigma:             6.0,
		InstabilityThresholdM: 8.0,
		OutlierMADThreshold:   3.5,
	}
}

// HubObservation represents an aggregated observation from one hub for one BSSID.
type HubObservation struct {
	HubID        string
	HubPosition  Point3D
	SmoothedRSSI float64 // processed value, separate from raw
	RawRSSIs     []float64
	Count        int
	LastSeen     time.Time
}

// LocalizationResult is the output of the localization engine for one BSSID.
type LocalizationResult struct {
	BSSID            string
	Position         Point3D
	Confidence       float64
	ErrorRadiusM     float64
	HubCount         int
	ObservationCount int64
	Quality          string
	Status           string
	IsUnstable       bool
}

// APLocalizationState tracks per-BSSID localization state in memory.
type APLocalizationState struct {
	BSSID           string
	LastPosition    *Point3D
	LastResult      *LocalizationResult
	StableCount     int
	UnstableCount   int
}

// LocalizationEngine manages localization for all observed BSSIDs.
type LocalizationEngine struct {
	mu       sync.RWMutex
	config   LocalizationConfig
	sigProc  *SignalProcessor
	states   map[string]*APLocalizationState // key: bssid
}

// NewLocalizationEngine creates a new LocalizationEngine.
func NewLocalizationEngine(cfg LocalizationConfig, sigProc *SignalProcessor) *LocalizationEngine {
	return &LocalizationEngine{
		config:  cfg,
		sigProc: sigProc,
		states:  make(map[string]*APLocalizationState),
	}
}

// UpdateConfig replaces the engine configuration (safe to call at runtime).
func (le *LocalizationEngine) UpdateConfig(cfg LocalizationConfig) {
	le.mu.Lock()
	defer le.mu.Unlock()
	le.config = cfg
}

// Localize runs the localization algorithm for a given BSSID using the provided
// hub observations. Returns a LocalizationResult.
func (le *LocalizationEngine) Localize(
	bssid string,
	hubObs []HubObservation,
	totalObservations int64,
) (*LocalizationResult, error) {
	le.mu.Lock()
	defer le.mu.Unlock()

	cfg := le.config

	// --- Step 1: Filter observations for spatial diversity ---
	filtered := filterSpatiallyUseful(hubObs)
	if len(filtered) < cfg.MinHubs {
		return &LocalizationResult{
			BSSID:            bssid,
			HubCount:         len(filtered),
			ObservationCount: totalObservations,
			Status:           "insufficient_data",
			Quality:          "low",
			Confidence:       0,
			ErrorRadiusM:     50,
		}, nil
	}

	// --- Step 2: Build hub positions for diversity scoring ---
	positions := make([]Point3D, len(filtered))
	for i, o := range filtered {
		positions[i] = o.HubPosition
	}

	// --- Step 3: Grid search ---
	candidate, residual, err := le.gridSearch(filtered)
	if err != nil {
		return nil, fmt.Errorf("grid search for %q: %w", bssid, err)
	}

	// --- Step 4: Compute RSSI statistics for confidence ---
	var allRSSIs []float64
	for _, o := range filtered {
		allRSSIs = append(allRSSIs, o.SmoothedRSSI)
	}
	stddev := RSSIStdDev(allRSSIs)

	// --- Step 5: Spatial diversity ---
	diversity := SpatialDiversity(positions)

	// --- Step 6: Confidence and error radius ---
	factors := ConfidenceFactors{
		HubCount:              len(filtered),
		SpatialDiversityScore: diversity,
		RSSIStdDev:            stddev,
		ObservationCount:      totalObservations,
		LocalizationResidual:  residual,
	}
	confidence := CalculateConfidence(factors)
	errorRadius := EstimateErrorRadius(factors, cfg.GridResolution)
	quality := QualityFromConfidence(confidence)

	// --- Step 7: Position smoothing ---
	state := le.getOrCreateState(bssid)
	smoothedPos := le.smoothPosition(state, candidate)

	// --- Step 8: Instability detection ---
	isUnstable := false
	if state.LastPosition != nil {
		jump := Distance2D(*state.LastPosition, smoothedPos)
		if jump > cfg.InstabilityThresholdM {
			isUnstable = true
			state.UnstableCount++
			state.StableCount = 0
			slog.Warn("AP localization unstable",
				"bssid", bssid,
				"jump_m", fmt.Sprintf("%.2f", jump),
				"threshold_m", cfg.InstabilityThresholdM,
			)
		} else {
			state.StableCount++
			if state.UnstableCount > 0 {
				state.UnstableCount--
			}
		}
	}

	status := "localized"
	if isUnstable || state.UnstableCount > 2 {
		status = "unstable"
	}

	state.LastPosition = &smoothedPos

	result := &LocalizationResult{
		BSSID:            bssid,
		Position:         smoothedPos,
		Confidence:       confidence,
		ErrorRadiusM:     errorRadius,
		HubCount:         len(filtered),
		ObservationCount: totalObservations,
		Quality:          quality,
		Status:           status,
		IsUnstable:       isUnstable,
	}
	state.LastResult = result

	slog.Info("AP localized",
		"bssid", bssid,
		"x", fmt.Sprintf("%.2f", smoothedPos.X),
		"y", fmt.Sprintf("%.2f", smoothedPos.Y),
		"z", fmt.Sprintf("%.2f", smoothedPos.Z),
		"confidence", fmt.Sprintf("%.2f", confidence),
		"error_m", fmt.Sprintf("%.2f", errorRadius),
		"hub_count", len(filtered),
		"quality", quality,
	)

	return result, nil
}

// gridSearch performs a 2D grid search to find the position with maximum
// RSSI likelihood given the hub observations.
//
// The search grid is bounded by the convex bounding box of hub positions,
// extended by GridPaddingM in each direction.
//
// Returns the best candidate position and the normalized residual [0,1]
// (0 = perfect fit, 1 = worst fit).
func (le *LocalizationEngine) gridSearch(hubObs []HubObservation) (Point3D, float64, error) {
	cfg := le.config

	if len(hubObs) == 0 {
		return Point3D{}, 1.0, fmt.Errorf("no observations")
	}

	// Determine grid bounds
	minX, maxX := hubObs[0].HubPosition.X, hubObs[0].HubPosition.X
	minY, maxY := hubObs[0].HubPosition.Y, hubObs[0].HubPosition.Y
	var avgZ float64
	for _, o := range hubObs {
		minX = math.Min(minX, o.HubPosition.X)
		maxX = math.Max(maxX, o.HubPosition.X)
		minY = math.Min(minY, o.HubPosition.Y)
		maxY = math.Max(maxY, o.HubPosition.Y)
		avgZ += o.HubPosition.Z
	}
	avgZ /= float64(len(hubObs))

	minX -= cfg.GridPaddingM
	maxX += cfg.GridPaddingM
	minY -= cfg.GridPaddingM
	maxY += cfg.GridPaddingM

	bestPos := Point3D{X: (minX + maxX) / 2, Y: (minY + maxY) / 2, Z: avgZ}
	bestLikelihood := -1.0
	maxPossibleLikelihood := float64(len(hubObs)) // all likelihoods = 1.0

	res := cfg.GridResolution
	for cx := minX; cx <= maxX; cx += res {
		for cy := minY; cy <= maxY; cy += res {
			candidate := Point3D{X: cx, Y: cy, Z: avgZ}
			totalLikelihood := le.candidateLikelihood(candidate, hubObs)
			if totalLikelihood > bestLikelihood {
				bestLikelihood = totalLikelihood
				bestPos = candidate
			}
		}
	}

	// Residual: 0 = perfect, 1 = worst
	residual := 1.0
	if maxPossibleLikelihood > 0 {
		residual = 1.0 - (bestLikelihood / maxPossibleLikelihood)
	}

	return bestPos, residual, nil
}

// candidateLikelihood computes the total RSSI likelihood for a candidate position
// across all hub observations.
func (le *LocalizationEngine) candidateLikelihood(candidate Point3D, hubObs []HubObservation) float64 {
	cfg := le.config
	var total float64
	for _, o := range hubObs {
		dist := Distance3D(candidate, o.HubPosition)
		expectedRSSI := DistanceToRSSI(dist, cfg.PathLossA, cfg.PathLossN)
		likelihood := RSSILikelihood(o.SmoothedRSSI, expectedRSSI, cfg.RSSISigma)
		total += likelihood
	}
	return total
}

// smoothPosition applies EMA smoothing to the estimated position.
func (le *LocalizationEngine) smoothPosition(state *APLocalizationState, newPos Point3D) Point3D {
	alpha := le.config.PositionAlpha
	if state.LastPosition == nil {
		return newPos
	}
	return Point3D{
		X: alpha*newPos.X + (1-alpha)*state.LastPosition.X,
		Y: alpha*newPos.Y + (1-alpha)*state.LastPosition.Y,
		Z: alpha*newPos.Z + (1-alpha)*state.LastPosition.Z,
	}
}

// getOrCreateState returns or creates per-BSSID state.
// Caller must hold le.mu.
func (le *LocalizationEngine) getOrCreateState(bssid string) *APLocalizationState {
	state, ok := le.states[bssid]
	if !ok {
		state = &APLocalizationState{BSSID: bssid}
		le.states[bssid] = state
	}
	return state
}

// filterSpatiallyUseful returns only hub observations that contribute
// spatial information (non-duplicate positions).
func filterSpatiallyUseful(hubObs []HubObservation) []HubObservation {
	if len(hubObs) == 0 {
		return nil
	}
	// Deduplicate by hub_id (keep most recent)
	byHub := make(map[string]HubObservation)
	for _, o := range hubObs {
		existing, ok := byHub[o.HubID]
		if !ok || o.LastSeen.After(existing.LastSeen) {
			byHub[o.HubID] = o
		}
	}

	result := make([]HubObservation, 0, len(byHub))
	for _, o := range byHub {
		result = append(result, o)
	}
	return result
}
