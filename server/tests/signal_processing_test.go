package tests

import (
	"math"
	"testing"

	"github.com/nahxfah/wifi-hunter-server/internal/services"
)

func TestEMASmoothing(t *testing.T) {
	sp := services.NewSignalProcessor(0.25)

	// First update should return the raw value itself
	result := sp.UpdateAndGetSmoothed("HUB-TEST", "AA:BB:CC:DD:EE:FF", -50.0)
	if result != -50.0 {
		t.Errorf("first reading should return raw value, got %.2f", result)
	}

	// Second update: 0.25 * (-60) + 0.75 * (-50) = -15 + -37.5 = -52.5
	result = sp.UpdateAndGetSmoothed("HUB-TEST", "AA:BB:CC:DD:EE:FF", -60.0)
	expected := 0.25*(-60.0) + 0.75*(-50.0)
	if math.Abs(result-expected) > 0.01 {
		t.Errorf("EMA result %.2f, expected %.2f", result, expected)
	}
}

func TestEMAIndependentPerHubBSSID(t *testing.T) {
	sp := services.NewSignalProcessor(0.25)

	// Different hub+bssid pairs should have independent state
	sp.UpdateAndGetSmoothed("HUB-001", "AA:BB:CC:DD:EE:FF", -50.0)
	sp.UpdateAndGetSmoothed("HUB-001", "AA:BB:CC:DD:EE:FF", -60.0)
	sp.UpdateAndGetSmoothed("HUB-002", "AA:BB:CC:DD:EE:FF", -70.0) // different hub, same BSSID

	v1, ok1 := sp.GetSmoothed("HUB-001", "AA:BB:CC:DD:EE:FF")
	v2, ok2 := sp.GetSmoothed("HUB-002", "AA:BB:CC:DD:EE:FF")

	if !ok1 || !ok2 {
		t.Fatal("expected smoothed values to exist")
	}
	if v1 == v2 {
		t.Errorf("different hubs should have independent RSSI state: both got %.2f", v1)
	}
}

func TestRSSIToDistance(t *testing.T) {
	// At d=1m: RSSI should equal A (by definition of the model)
	A := -45.0
	n := 3.0
	d := services.RSSIToDistance(A, A, n)
	if math.Abs(d-1.0) > 0.01 {
		t.Errorf("at d=1m RSSI=A, expected d≈1.0, got %.4f", d)
	}

	// Closer AP (stronger RSSI) should give smaller distance
	d1 := services.RSSIToDistance(-40.0, A, n)
	d2 := services.RSSIToDistance(-70.0, A, n)
	if d1 >= d2 {
		t.Errorf("stronger RSSI should give shorter distance: d(-40)=%.2f, d(-70)=%.2f", d1, d2)
	}
}

func TestDistanceToRSSI(t *testing.T) {
	A := -45.0
	n := 3.0

	// Distance 1m → RSSI should be A
	rssi := services.DistanceToRSSI(1.0, A, n)
	if math.Abs(rssi-A) > 0.01 {
		t.Errorf("at d=1m, expected RSSI=%.2f, got %.2f", A, rssi)
	}

	// Increasing distance → decreasing RSSI (more negative)
	r1 := services.DistanceToRSSI(2.0, A, n)
	r2 := services.DistanceToRSSI(5.0, A, n)
	r3 := services.DistanceToRSSI(10.0, A, n)
	if !(r1 > r2 && r2 > r3) {
		t.Errorf("RSSI should decrease with distance: d2=%.2f d5=%.2f d10=%.2f", r1, r2, r3)
	}
}

func TestOutlierFiltering(t *testing.T) {
	rssis := []float64{-51, -53, -92, -52, -50}
	// -92 is a clear outlier among readings in the -50 range

	outliers := services.FilterOutliers(rssis, 3.5)
	if len(outliers) != len(rssis) {
		t.Fatalf("outlier slice length mismatch")
	}

	// The -92 value should be flagged
	outlierIdx := 2 // index of -92
	if !outliers[outlierIdx] {
		t.Errorf("expected -92 to be flagged as outlier")
	}

	// Others should not be outliers
	for i, isOut := range outliers {
		if i != outlierIdx && isOut {
			t.Errorf("value %v at index %d should not be an outlier", rssis[i], i)
		}
	}
}

func TestRSSILikelihood(t *testing.T) {
	// Perfect match → likelihood near 1
	l := services.RSSILikelihood(-50.0, -50.0, 6.0)
	if l < 0.99 {
		t.Errorf("perfect RSSI match should give likelihood ≈ 1, got %.4f", l)
	}

	// 3-sigma difference → likelihood should be small
	l3sigma := services.RSSILikelihood(-50.0, -68.0, 6.0)
	if l3sigma > 0.1 {
		t.Errorf("3-sigma RSSI difference should give low likelihood, got %.4f", l3sigma)
	}
}
