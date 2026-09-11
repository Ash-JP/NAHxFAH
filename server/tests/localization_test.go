package tests

import (
	"math"
	"testing"

	"github.com/nahxfah/wifi-hunter-server/internal/services"
)

// TestLocalizationKnownPosition tests that the localization engine estimates
// a position reasonably close to the known AP location when hubs are placed
// at the four corners of a 10x10m space and the AP is at (5,5).
//
// This uses SYNTHETIC data for the unit test ONLY.
// Production code must NEVER use synthetic/fake scan data.
func TestLocalizationKnownPosition(t *testing.T) {
	cfg := services.DefaultLocalizationConfig()
	cfg.MinHubs = 3
	cfg.GridResolution = 0.5
	cfg.GridPaddingM = 3.0

	sigProc := services.NewSignalProcessor(0.25)
	engine := services.NewLocalizationEngine(cfg, sigProc)

	// Known AP position: (5, 5, 2)
	knownAP := services.Point3D{X: 5, Y: 5, Z: 2}

	// Hub positions at four corners
	hubPositions := []services.Point3D{
		{X: 0, Y: 0, Z: 1},
		{X: 10, Y: 0, Z: 1},
		{X: 0, Y: 10, Z: 1},
		{X: 10, Y: 10, Z: 1},
	}

	// Generate synthetic RSSI values using the path loss model
	// These are the expected RSSI values if the AP is at (5,5,2) and hubs at corners
	A := cfg.PathLossA // reference RSSI at 1m
	n := cfg.PathLossN // path loss exponent

	var hubObs []services.HubObservation
	for i, hubPos := range hubPositions {
		dist := services.Distance3D(knownAP, hubPos)
		// Compute expected RSSI using path loss model
		expectedRSSI := A - 10*n*math.Log10(dist)

		// Add a small amount of controlled noise (±2 dBm)
		noisyRSSI := expectedRSSI + float64(i%2)*2.0 - 1.0

		hubObs = append(hubObs, services.HubObservation{
			HubID:        generateHubID(i),
			HubPosition:  hubPos,
			SmoothedRSSI: noisyRSSI,
			RawRSSIs:     []float64{noisyRSSI},
			Count:        10,
		})
	}

	result, err := engine.Localize("AA:BB:CC:DD:EE:FF", hubObs, 40)
	if err != nil {
		t.Fatalf("localization failed: %v", err)
	}

	if result.Status == "insufficient_data" {
		t.Fatalf("expected localization result, got insufficient_data")
	}

	// Allow ±3m error given grid resolution and RSSI noise
	errorTolerance := 3.0
	actualDist := services.Distance2D(result.Position, knownAP)
	if actualDist > errorTolerance {
		t.Errorf("estimated position (%.2f, %.2f) is %.2fm from known AP (%.2f, %.2f), tolerance: %.2fm",
			result.Position.X, result.Position.Y,
			actualDist,
			knownAP.X, knownAP.Y,
			errorTolerance,
		)
	}

	t.Logf("Known AP: (%.2f, %.2f) | Estimated: (%.2f, %.2f) | Error: %.2fm",
		knownAP.X, knownAP.Y, result.Position.X, result.Position.Y, actualDist)
	t.Logf("Confidence: %.2f | Quality: %s | Error radius: %.2fm",
		result.Confidence, result.Quality, result.ErrorRadiusM)
}

func TestLocalizationInsufficientHubs(t *testing.T) {
	cfg := services.DefaultLocalizationConfig()
	cfg.MinHubs = 3

	sigProc := services.NewSignalProcessor(0.25)
	engine := services.NewLocalizationEngine(cfg, sigProc)

	// Only 2 hubs — should return insufficient_data
	hubObs := []services.HubObservation{
		{HubID: "HUB-001", HubPosition: services.Point3D{X: 0, Y: 0, Z: 1}, SmoothedRSSI: -55, Count: 5},
		{HubID: "HUB-002", HubPosition: services.Point3D{X: 10, Y: 0, Z: 1}, SmoothedRSSI: -65, Count: 5},
	}

	result, err := engine.Localize("BB:CC:DD:EE:FF:00", hubObs, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "insufficient_data" {
		t.Errorf("expected status=insufficient_data with 2 hubs, got %q", result.Status)
	}
}

func TestSpatialDiversity(t *testing.T) {
	// Well-distributed hubs should have higher diversity than clustered ones
	wellDistributed := []services.Point3D{
		{X: 0, Y: 0, Z: 1},
		{X: 10, Y: 0, Z: 1},
		{X: 5, Y: 10, Z: 1},
	}

	clustered := []services.Point3D{
		{X: 1, Y: 1, Z: 1},
		{X: 1.5, Y: 1.2, Z: 1},
		{X: 1.2, Y: 1.8, Z: 1},
	}

	diverseScore := services.SpatialDiversity(wellDistributed)
	clusteredScore := services.SpatialDiversity(clustered)

	if diverseScore <= clusteredScore {
		t.Errorf("well-distributed score (%.3f) should be > clustered score (%.3f)",
			diverseScore, clusteredScore)
	}
	t.Logf("Well-distributed diversity: %.3f | Clustered diversity: %.3f", diverseScore, clusteredScore)
}

func generateHubID(i int) string {
	ids := []string{"HUB-AAAAAA", "HUB-BBBBBB", "HUB-CCCCCC", "HUB-DDDDDD"}
	if i < len(ids) {
		return ids[i]
	}
	return "HUB-XXXXXX"
}
