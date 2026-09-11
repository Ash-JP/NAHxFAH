package tests

import (
	"math"
	"testing"

	"github.com/nahxfah/wifi-hunter-server/internal/services"
)

func TestLocalToLocal(t *testing.T) {
	p := services.Point3D{X: 4.2, Y: 8.1, Z: 1.0}
	result := services.LocalToLocal(p)
	if result.X != p.X || result.Y != p.Y || result.Z != p.Z {
		t.Errorf("LocalToLocal should be identity: got %v, expected %v", result, p)
	}
}

func TestENUToLocal(t *testing.T) {
	// ENU uses same axis convention as local
	p := services.Point3D{X: 3.5, Y: 7.2, Z: 1.5}
	result := services.ENUToLocal(p)
	if result.X != p.X || result.Y != p.Y || result.Z != p.Z {
		t.Errorf("ENUToLocal should be identity for same convention: got %v, expected %v", result, p)
	}
}

func TestGPSToLocalOriginPoint(t *testing.T) {
	// GPS coordinates at the origin should give (0, 0, 0)
	origin := services.LocalOrigin{
		Latitude:  51.5074,
		Longitude: -0.1278,
		Altitude:  10.0,
	}

	result := services.GPSToLocal(origin.Latitude, origin.Longitude, origin.Altitude, origin)
	tolerance := 0.01 // 1cm

	if math.Abs(result.X) > tolerance || math.Abs(result.Y) > tolerance || math.Abs(result.Z) > tolerance {
		t.Errorf("GPS at origin should give (0,0,0), got (%.4f, %.4f, %.4f)",
			result.X, result.Y, result.Z)
	}
}

func TestGPSToLocalNorthward(t *testing.T) {
	// Moving north (increasing latitude) should increase Y (North axis)
	origin := services.LocalOrigin{
		Latitude:  51.5074,
		Longitude: -0.1278,
		Altitude:  10.0,
	}

	// ~100m north
	northPoint := services.GPSToLocal(51.5083, -0.1278, 10.0, origin)
	if northPoint.Y < 50 {
		t.Errorf("moving north should give positive Y, got Y=%.2f", northPoint.Y)
	}
	if math.Abs(northPoint.X) > 10 {
		t.Errorf("moving north should have minimal X deviation, got X=%.2f", northPoint.X)
	}
}

func TestDistance3D(t *testing.T) {
	a := services.Point3D{X: 0, Y: 0, Z: 0}
	b := services.Point3D{X: 3, Y: 4, Z: 0}
	d := services.Distance3D(a, b)
	if math.Abs(d-5.0) > 0.001 {
		t.Errorf("3-4-5 triangle: expected d=5.0, got %.4f", d)
	}
}

func TestDistance2D(t *testing.T) {
	a := services.Point3D{X: 0, Y: 0, Z: 100} // large Z should be ignored
	b := services.Point3D{X: 3, Y: 4, Z: 0}
	d := services.Distance2D(a, b)
	if math.Abs(d-5.0) > 0.001 {
		t.Errorf("2D 3-4-5 triangle: expected d=5.0, got %.4f", d)
	}
}

func TestTransformToLocal(t *testing.T) {
	p := services.Point3D{X: 1, Y: 2, Z: 3}

	result, err := services.TransformToLocal(services.CoordLocal, p, nil)
	if err != nil {
		t.Fatalf("unexpected error for local→local: %v", err)
	}
	if result.X != p.X || result.Y != p.Y || result.Z != p.Z {
		t.Errorf("local→local transform should be identity")
	}

	// GPS requires origin
	_, err = services.TransformToLocal(services.CoordGPS, p, nil)
	if err == nil {
		t.Error("GPS→local without origin should return error")
	}

	// Unknown coordinate system
	_, err = services.TransformToLocal("unknown", p, nil)
	if err == nil {
		t.Error("unknown coordinate system should return error")
	}
}
