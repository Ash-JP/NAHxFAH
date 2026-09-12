// Package services provides confidence scoring for AP localization results.
//
// Confidence is a value in [0.0, 1.0] computed from multiple independent factors.
// It reflects how reliable the position estimate is — NOT a probability in a
// strict mathematical sense.
//
// Confidence factors and their weights:
//
//	Hub count          30%  — more hubs = better triangulation
//	Spatial diversity  25%  — well-spread hubs constrain the estimate better
//	RSSI consistency   20%  — stable RSSI readings are more reliable
//	Observation count  15%  — more samples reduce noise
//	Residual           10%  — lower grid search error = better fit
package services

import (
	"math"
)

// ConfidenceFactors holds the input metrics for confidence calculation.
type ConfidenceFactors struct {
	// HubCount: number of unique hubs contributing to this localization.
	HubCount int

	// SpatialDiversityScore: normalized [0,1] measure of how well the hubs
	// surround the estimated AP position. 1.0 = perfect enclosure.
	SpatialDiversityScore float64

	// RSSIStdDev: standard deviation of RSSI observations from all contributing
	// hubs (lower = more consistent = better).
	RSSIStdDev float64

	// ObservationCount: total number of raw observations used.
	ObservationCount int64

	// LocalizationResidual: the minimum weighted error from the grid search (lower = better).
	LocalizationResidual float64
}

// CalculateConfidence computes a composite confidence score in [0.0, 1.0].
func CalculateConfidence(f ConfidenceFactors) float64 {
	hubScore        := scoreHubCount(f.HubCount)
	diversityScore  := clamp01(f.SpatialDiversityScore)
	consistencyScore := scoreRSSIConsistency(f.RSSIStdDev)
	countScore      := scoreObservationCount(f.ObservationCount)
	residualScore   := scoreResidual(f.LocalizationResidual)

	// Weighted composite
	confidence := 0.30*hubScore +
		0.25*diversityScore +
		0.20*consistencyScore +
		0.15*countScore +
		0.10*residualScore

	return clamp01(confidence)
}

// QualityFromConfidence maps a confidence score to a human-readable tier.
func QualityFromConfidence(confidence float64) string {
	switch {
	case confidence >= 0.70:
		return "high"
	case confidence >= 0.40:
		return "medium"
	default:
		return "low"
	}
}

// EstimateErrorRadius estimates the position uncertainty radius in metres
// based on the localization factors.
//
// IMPORTANT: This is an uncertainty estimate, not a guaranteed mathematical bound.
func EstimateErrorRadius(f ConfidenceFactors, gridResolution float64) float64 {
	confidence := CalculateConfidence(f)

	// Base uncertainty inversely proportional to confidence
	// Scaled for room environments:
	// confidence=1.0 -> 0.8m; confidence=0.8 -> 1.7m; confidence=0.5 -> 5.0m
	baseError := 0.8 + 12.0*math.Pow(1.0-confidence, 1.5)

	// Floor based on fine grid resolution (5cm)
	floor := 0.25

	errorRadius := math.Max(baseError, floor)

	// Cap at a realistic room-scale maximum
	return math.Min(errorRadius, 15.0)
}

// SpatialDiversity calculates how well a set of hub positions surrounds an
// estimated AP position. Uses the mean pairwise distance between hubs,
// normalized against the expected coverage radius.
//
// Returns a score in [0.0, 1.0]:
//
//	1.0 = hubs are well distributed around the space
//	0.0 = all hubs are clustered together
func SpatialDiversity(hubPositions []Point3D) float64 {
	n := len(hubPositions)
	if n < 2 {
		return 0.0
	}

	// Compute all pairwise 2D distances
	var totalDist float64
	pairs := 0
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			totalDist += Distance2D(hubPositions[i], hubPositions[j])
			pairs++
		}
	}

	if pairs == 0 {
		return 0.0
	}

	meanDist := totalDist / float64(pairs)

	// Normalize: assume 10m is a well-spread deployment for indoor spaces
	// Score approaches 1.0 as mean pairwise distance approaches 10m
	normalizedDist := meanDist / 10.0

	// Use sigmoid-like curve: good spread saturates around 1.0
	score := 1.0 - math.Exp(-normalizedDist)

	return clamp01(score)
}

// --- Private scoring sub-functions ---

func scoreHubCount(count int) float64 {
	switch {
	case count <= 0:
		return 0.0
	case count == 1:
		return 0.2
	case count == 2:
		return 0.5
	case count == 3:
		return 0.75
	case count == 4:
		return 0.88
	case count >= 5:
		return 1.0
	default:
		return 0.0
	}
}

// scoreRSSIConsistency: lower stddev = more consistent = higher score.
// stddev=0 → 1.0, stddev=15 → 0.0
func scoreRSSIConsistency(stddev float64) float64 {
	if stddev <= 0 {
		return 1.0
	}
	score := 1.0 - (stddev / 15.0)
	return clamp01(score)
}

// scoreObservationCount: more observations = higher score (logarithmic growth).
// 1 obs → ~0.1, 10 obs → ~0.5, 100 obs → ~0.8, 1000 obs → ~1.0
func scoreObservationCount(count int64) float64 {
	if count <= 0 {
		return 0.0
	}
	score := math.Log10(float64(count)+1) / 3.0 // saturates around 1000 obs
	return clamp01(score)
}

// scoreResidual: lower residual (better fit) = higher score.
// residual=0 → 1.0, residual=1.0 → 0.0
func scoreResidual(residual float64) float64 {
	if residual <= 0 {
		return 1.0
	}
	score := 1.0 - clamp01(residual)
	return score
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// RSSIStdDev computes the standard deviation of a slice of RSSI values.
func RSSIStdDev(values []float64) float64 {
	n := len(values)
	if n < 2 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(n)
	var variance float64
	for _, v := range values {
		d := v - mean
		variance += d * d
	}
	return math.Sqrt(variance / float64(n))
}
