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
	"strings"
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
		GridResolution:        0.3,
		GridPaddingM:          5.0,
		PositionAlpha:         0.3,
		PathLossA:             -40.0,
		PathLossN:             2.4,
		RSSISigma:             5.0,
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
	FrequencyMHz int
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

// gridSearch performs a 3D coarse-to-fine search to find the position with maximum
// RSSI likelihood given the hub observations.
//
// The search grid is bounded horizontally by the bounding box of hub positions
// plus GridPaddingM, and vertically between 0.5m and 2.8m (typical indoor AP heights).
//
// Returns the best candidate position and the normalized residual [0,1]
// (0 = perfect fit, 1 = worst fit).
func (le *LocalizationEngine) gridSearch(hubObs []HubObservation) (Point3D, float64, error) {
	cfg := le.config

	if len(hubObs) == 0 {
		return Point3D{}, 1.0, fmt.Errorf("no observations")
	}

	// Determine horizontal grid bounds from hub positions
	minX, maxX := hubObs[0].HubPosition.X, hubObs[0].HubPosition.X
	minY, maxY := hubObs[0].HubPosition.Y, hubObs[0].HubPosition.Y
	minZ, maxZ := hubObs[0].HubPosition.Z, hubObs[0].HubPosition.Z

	for _, o := range hubObs {
		minX = math.Min(minX, o.HubPosition.X)
		maxX = math.Max(maxX, o.HubPosition.X)
		minY = math.Min(minY, o.HubPosition.Y)
		maxY = math.Max(maxY, o.HubPosition.Y)
		minZ = math.Min(minZ, o.HubPosition.Z)
		maxZ = math.Max(maxZ, o.HubPosition.Z)
	}

	minX -= cfg.GridPaddingM
	maxX += cfg.GridPaddingM
	minY -= cfg.GridPaddingM
	maxY += cfg.GridPaddingM

	// Realistic indoor height bounds for AP placement (tables, shelves, ceilings: 0.5m to 2.8m)
	searchMinZ := math.Max(0.5, minZ-0.5)
	searchMaxZ := math.Min(2.8, math.Max(maxZ+1.0, 2.2))
	if searchMinZ >= searchMaxZ {
		searchMinZ = 0.8
		searchMaxZ = 2.2
	}

	bestPos := Point3D{X: (minX + maxX) / 2, Y: (minY + maxY) / 2, Z: (searchMinZ + searchMaxZ) / 2}
	bestLikelihood := -1.0

	// Stage 1: Coarse 3D search (0.5m horizontal step, 0.4m vertical step)
	res := cfg.GridResolution
	if res <= 0 {
		res = 0.5
	}
	zStep := 0.4

	for cz := searchMinZ; cz <= searchMaxZ; cz += zStep {
		for cx := minX; cx <= maxX; cx += res {
			for cy := minY; cy <= maxY; cy += res {
				candidate := Point3D{X: cx, Y: cy, Z: cz}
				totalLikelihood := le.candidateLikelihood(candidate, hubObs)
				if totalLikelihood > bestLikelihood {
					bestLikelihood = totalLikelihood
					bestPos = candidate
				}
			}
		}
	}

	// Stage 2: Fine 3D search around best candidate (±1.0m horizontal at 0.05m / 5cm, ±0.3m vertical at 0.1m)
	fineMinX := math.Max(minX, bestPos.X-1.0)
	fineMaxX := math.Min(maxX, bestPos.X+1.0)
	fineMinY := math.Max(minY, bestPos.Y-1.0)
	fineMaxY := math.Min(maxY, bestPos.Y+1.0)
	fineMinZ := math.Max(searchMinZ, bestPos.Z-0.3)
	fineMaxZ := math.Min(searchMaxZ, bestPos.Z+0.3)
	fineRes := 0.05

	for fz := fineMinZ; fz <= fineMaxZ; fz += 0.1 {
		for fx := fineMinX; fx <= fineMaxX; fx += fineRes {
			for fy := fineMinY; fy <= fineMaxY; fy += fineRes {
				candidate := Point3D{X: fx, Y: fy, Z: fz}
				totalLikelihood := le.candidateLikelihood(candidate, hubObs)
				if totalLikelihood > bestLikelihood {
					bestLikelihood = totalLikelihood
					bestPos = candidate
				}
			}
		}
	}

	// Residual: 0 = perfect, 1 = worst (likelihood in (0, 1])
	residual := math.Max(0.0, math.Min(1.0, 1.0-bestLikelihood))

	return bestPos, residual, nil
}

// candidateLikelihood computes the total Gaussian likelihood for a candidate position
// across all hub observations using frequency-aware path loss and weighted RMSE.
func (le *LocalizationEngine) candidateLikelihood(candidate Point3D, hubObs []HubObservation) float64 {
	cfg := le.config
	var totalCost float64
	var totalWeight float64

	for _, o := range hubObs {
		// Frequency-dependent reference loss A:
		// 5 GHz / 6 GHz attenuates ~6 dB faster at 1m than 2.4 GHz
		refA := cfg.PathLossA
		if o.FrequencyMHz > 4000 {
			refA -= 6.0
		}

		dist := Distance3D(candidate, o.HubPosition)
		expectedRSSI := DistanceToRSSI(dist, refA, cfg.PathLossN)

		// Higher SNR signals have significantly less multipath/penetration distortion
		weight := 1.0
		if o.SmoothedRSSI >= -50 {
			weight = 2.5
		} else if o.SmoothedRSSI >= -60 {
			weight = 1.8
		} else if o.SmoothedRSSI >= -70 {
			weight = 1.3
		}

		// Fixed venue laptop hubs provide true ground-truth spatial anchors
		if !strings.HasPrefix(o.HubID, "MOBILE-") {
			weight *= 3.0
		} else {
			weight *= 0.5
		}

		diff := o.SmoothedRSSI - expectedRSSI
		totalCost += weight * (diff * diff)
		totalWeight += weight
	}

	if totalWeight == 0 {
		return 0
	}

	rmse := math.Sqrt(totalCost / totalWeight)
	sigma := cfg.RSSISigma
	if sigma <= 0 {
		sigma = 5.0
	}
	return math.Exp(-(rmse * rmse) / (2 * sigma * sigma))
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
// spatial information (non-duplicate positions separated by >= 0.75m).
func filterSpatiallyUseful(hubObs []HubObservation) []HubObservation {
	if len(hubObs) == 0 {
		return nil
	}
	// We want observations that are spatially separated.
	// Iterate from newest to oldest. Keep observations that are at least 0.75m away
	// from already kept observations, or from a different hub if positions differ.
	var result []HubObservation
	for i := len(hubObs) - 1; i >= 0; i-- {
		o := hubObs[i]
		isDuplicate := false
		for _, kept := range result {
			if (o.HubID == kept.HubID && Distance3D(o.HubPosition, kept.HubPosition) < 0.5) ||
				Distance3D(o.HubPosition, kept.HubPosition) < 0.2 {
				isDuplicate = true
				break
			}
		}
		if !isDuplicate {
			result = append(result, o)
		}
	}
	return result
}
