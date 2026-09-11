// Package services provides RSSI signal processing utilities.
//
// Key principles:
//   - Raw observations are NEVER modified — smoothing is maintained separately in memory.
//   - RSSI is treated as a noisy probabilistic signal, not a precise distance sensor.
//   - Outliers are detected and down-weighted, not silently discarded from storage.
package services

import (
	"math"
	"sort"
	"sync"
)

// RSSIState holds the smoothed RSSI state for one (hub_id, bssid) pair.
type RSSIState struct {
	SmoothedRSSI float64
	Initialized  bool
	SampleCount  int
}

// SignalProcessor maintains per-(hub,bssid) EMA state and provides outlier filtering.
type SignalProcessor struct {
	mu     sync.RWMutex
	alpha  float64 // EMA smoothing factor
	states map[string]*RSSIState // key: hubID + ":" + bssid
}

// NewSignalProcessor creates a SignalProcessor with the given EMA alpha.
func NewSignalProcessor(alpha float64) *SignalProcessor {
	return &SignalProcessor{
		alpha:  alpha,
		states: make(map[string]*RSSIState),
	}
}

// key returns the map key for a hub+bssid pair.
func (sp *SignalProcessor) key(hubID, bssid string) string {
	return hubID + ":" + bssid
}

// UpdateAndGetSmoothed applies EMA smoothing to the raw RSSI value and returns the smoothed value.
// Formula: smoothed = alpha * current + (1-alpha) * previous
func (sp *SignalProcessor) UpdateAndGetSmoothed(hubID, bssid string, rawRSSI float64) float64 {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	k := sp.key(hubID, bssid)
	state, exists := sp.states[k]
	if !exists || !state.Initialized {
		state = &RSSIState{
			SmoothedRSSI: rawRSSI,
			Initialized:  true,
			SampleCount:  1,
		}
		sp.states[k] = state
		return rawRSSI
	}

	state.SmoothedRSSI = sp.alpha*rawRSSI + (1-sp.alpha)*state.SmoothedRSSI
	state.SampleCount++
	return state.SmoothedRSSI
}

// GetSmoothed returns the current smoothed RSSI without updating it.
// Returns (value, true) if the state exists, (0, false) otherwise.
func (sp *SignalProcessor) GetSmoothed(hubID, bssid string) (float64, bool) {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	state, exists := sp.states[sp.key(hubID, bssid)]
	if !exists || !state.Initialized {
		return 0, false
	}
	return state.SmoothedRSSI, true
}

// Reset clears the smoothing state for a hub+bssid pair.
func (sp *SignalProcessor) Reset(hubID, bssid string) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	delete(sp.states, sp.key(hubID, bssid))
}

// FilterOutliers identifies outliers in a slice of RSSI values using the
// Median Absolute Deviation (MAD) method. Returns a slice of booleans
// indicating which values are outliers (true = outlier).
//
// Outliers are NOT removed from storage — they are flagged so that
// the localization engine can reduce their weight.
func FilterOutliers(rssiValues []float64, threshold float64) []bool {
	if len(rssiValues) < 3 {
		// Too few samples to reliably detect outliers
		return make([]bool, len(rssiValues))
	}

	median := medianFloat64(rssiValues)

	// Compute absolute deviations from the median
	deviations := make([]float64, len(rssiValues))
	for i, v := range rssiValues {
		deviations[i] = math.Abs(v - median)
	}

	mad := medianFloat64(deviations)
	if mad == 0 {
		// All values identical — no outliers
		return make([]bool, len(rssiValues))
	}

	// Modified Z-score: 0.6745 is a consistency factor for the normal distribution
	outliers := make([]bool, len(rssiValues))
	for i, v := range rssiValues {
		modifiedZ := 0.6745 * math.Abs(v-median) / mad
		outliers[i] = modifiedZ > threshold
	}
	return outliers
}

// medianFloat64 returns the median of a slice. Does NOT modify the original slice.
func medianFloat64(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)
	n := len(sorted)
	if n%2 == 0 {
		return (sorted[n/2-1] + sorted[n/2]) / 2
	}
	return sorted[n/2]
}

// RSSIToDistance converts an RSSI value to an estimated distance using the
// log-distance path loss model:
//
//	RSSI = A - 10 * n * log10(d)
//	d = 10 ^ ((A - RSSI) / (10 * n))
//
// Parameters:
//
//	rssi: measured RSSI in dBm (negative value)
//	a:    reference RSSI at 1m distance (dBm, typically -45 to -55)
//	n:    path loss exponent (free space=2.0, indoor typical=2.5–4.0)
//
// IMPORTANT: This model provides a noisy probabilistic estimate, NOT a
// precise distance. RSSI varies due to walls, multipath, interference,
// antenna orientation, and hardware differences. Calibrate A and N for
// your specific environment.
func RSSIToDistance(rssi, a, n float64) float64 {
	if n == 0 {
		return 1.0
	}
	exponent := (a - rssi) / (10.0 * n)
	d := math.Pow(10, exponent)
	// Clamp to a reasonable range [0.1m, 500m]
	if d < 0.1 {
		d = 0.1
	}
	if d > 500 {
		d = 500
	}
	return d
}

// DistanceToRSSI converts a distance back to expected RSSI for the path loss model.
// Used for computing likelihood in the localization grid search.
func DistanceToRSSI(distanceM, a, n float64) float64 {
	if distanceM <= 0 {
		distanceM = 0.01
	}
	return a - 10*n*math.Log10(distanceM)
}

// RSSILikelihood computes a Gaussian-shaped likelihood for an observed RSSI
// given an expected RSSI. sigma controls the spread (dBm uncertainty).
//
// Returns a weight in (0, 1]: higher means the observed RSSI is more
// consistent with the expected value at this candidate position.
func RSSILikelihood(observedRSSI, expectedRSSI, sigma float64) float64 {
	if sigma <= 0 {
		sigma = 5.0 // default ±5 dBm uncertainty
	}
	diff := observedRSSI - expectedRSSI
	return math.Exp(-(diff * diff) / (2 * sigma * sigma))
}
