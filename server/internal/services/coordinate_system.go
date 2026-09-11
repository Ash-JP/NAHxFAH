// Package services provides coordinate system transformation utilities.
//
// Coordinate convention:
//
//	X = East  (right)
//	Y = North (forward)
//	Z = Up
//
// Currently fully implemented: local → local (identity).
// Stubs provided for: GPS → local, ENU → local.
// Future Android ARCore transformations will be added here.
package services

import (
	"fmt"
	"math"
)

// CoordinateSystem identifies a coordinate reference frame.
type CoordinateSystem string

const (
	CoordLocal CoordinateSystem = "local"
	CoordENU   CoordinateSystem = "enu"
	CoordGPS   CoordinateSystem = "gps"
)

// Point3D is a position in 3D space.
type Point3D struct {
	X float64
	Y float64
	Z float64
}

// LocalOrigin defines the local origin in GPS coordinates (for future GPS→local transforms).
type LocalOrigin struct {
	Latitude  float64 // degrees
	Longitude float64 // degrees
	Altitude  float64 // metres
}

// TransformToLocal converts a point from the source coordinate system to local coordinates.
// Returns an error if the transformation is not supported.
func TransformToLocal(src CoordinateSystem, p Point3D, origin *LocalOrigin) (Point3D, error) {
	switch src {
	case CoordLocal:
		return LocalToLocal(p), nil
	case CoordENU:
		return ENUToLocal(p), nil
	case CoordGPS:
		if origin == nil {
			return Point3D{}, fmt.Errorf("GPS→local transform requires a local origin")
		}
		return GPSToLocal(p.X, p.Y, p.Z, *origin), nil
	default:
		return Point3D{}, fmt.Errorf("unsupported source coordinate system: %q", src)
	}
}

// LocalToLocal is an identity transform — the local system IS the target system.
func LocalToLocal(p Point3D) Point3D {
	return p
}

// ENUToLocal transforms ENU (East-North-Up) coordinates to local coordinates.
// For this implementation the two systems share the same axes convention,
// so this is also an identity transform. Future implementations may apply
// a rotation if the local system has a different orientation.
func ENUToLocal(p Point3D) Point3D {
	// ENU: X=East, Y=North, Z=Up  — same convention as local.
	return Point3D{X: p.X, Y: p.Y, Z: p.Z}
}

// GPSToLocal converts a GPS coordinate (latitude, longitude, altitude in degrees/metres)
// to local ENU coordinates relative to the given origin.
//
// Uses the standard geodetic → ECEF → ENU transformation.
// Accuracy is sufficient for indoor-scale distances (<1km from origin).
func GPSToLocal(lat, lon, alt float64, origin LocalOrigin) Point3D {
	// WGS-84 ellipsoid constants
	const (
		a  = 6378137.0         // semi-major axis (m)
		e2 = 0.00669437999014  // first eccentricity squared
	)

	toRad := func(deg float64) float64 { return deg * math.Pi / 180.0 }

	// Convert point to ECEF
	latR := toRad(lat)
	lonR := toRad(lon)
	N := a / math.Sqrt(1-e2*math.Pow(math.Sin(latR), 2))
	x := (N + alt) * math.Cos(latR) * math.Cos(lonR)
	y := (N + alt) * math.Cos(latR) * math.Sin(lonR)
	z := (N*(1-e2) + alt) * math.Sin(latR)

	// Convert origin to ECEF
	oLatR := toRad(origin.Latitude)
	oLonR := toRad(origin.Longitude)
	oN := a / math.Sqrt(1-e2*math.Pow(math.Sin(oLatR), 2))
	ox := (oN + origin.Altitude) * math.Cos(oLatR) * math.Cos(oLonR)
	oy := (oN + origin.Altitude) * math.Cos(oLatR) * math.Sin(oLonR)
	oz := (oN*(1-e2) + origin.Altitude) * math.Sin(oLatR)

	// ECEF difference
	dx := x - ox
	dy := y - oy
	dz := z - oz

	// Rotate to ENU using origin latitude/longitude
	sinLat := math.Sin(oLatR)
	cosLat := math.Cos(oLatR)
	sinLon := math.Sin(oLonR)
	cosLon := math.Cos(oLonR)

	east  := -sinLon*dx + cosLon*dy
	north := -sinLat*cosLon*dx - sinLat*sinLon*dy + cosLat*dz
	up    :=  cosLat*cosLon*dx + cosLat*sinLon*dy + sinLat*dz

	// ENU == local convention
	return Point3D{X: east, Y: north, Z: up}
}

// Distance3D returns the Euclidean distance between two 3D points.
func Distance3D(a, b Point3D) float64 {
	dx := a.X - b.X
	dy := a.Y - b.Y
	dz := a.Z - b.Z
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}

// Distance2D returns the horizontal Euclidean distance (ignoring Z).
func Distance2D(a, b Point3D) float64 {
	dx := a.X - b.X
	dy := a.Y - b.Y
	return math.Sqrt(dx*dx + dy*dy)
}
