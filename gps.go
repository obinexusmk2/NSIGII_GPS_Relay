// Package gps provides real-time GPS coordinate capture for ltcodec.
// This forms the physical-layer anchor of the space-time fingerprint.
//
// Pipeline: riftlang.exe → .so.a → rift.exe → gosilang → ltcodec → nsigii
// Orchestration: nlink → polybuild
package gps

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"time"
)

// Coordinate represents a GPS position in 3D space
type Coordinate struct {
	Latitude  float64   `json:"lat"`
	Longitude float64   `json:"lon"`
	Altitude  float64   `json:"alt"`  // metres above sea level
	Accuracy  float64   `json:"acc"`  // metres radius of uncertainty
	Timestamp time.Time `json:"ts"`
	Source    string    `json:"src"`  // "gps", "ip", "manual"
}

// String returns a human-readable coordinate
func (c Coordinate) String() string {
	return fmt.Sprintf("%.6f,%.6f,%.1fm @ %s [%s]",
		c.Latitude, c.Longitude, c.Altitude,
		c.Timestamp.Format(time.RFC3339Nano),
		c.Source,
	)
}

// IsZero returns true if coordinate is uninitialised
func (c Coordinate) IsZero() bool {
	return c.Latitude == 0 && c.Longitude == 0
}

// DistanceTo returns the Haversine distance in metres to another coordinate
func (c Coordinate) DistanceTo(other Coordinate) float64 {
	const R = 6371000 // Earth radius in metres
	lat1 := c.Latitude * math.Pi / 180
	lat2 := other.Latitude * math.Pi / 180
	dLat := (other.Latitude - c.Latitude) * math.Pi / 180
	dLon := (other.Longitude - c.Longitude) * math.Pi / 180

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1)*math.Cos(lat2)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	c2 := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c2
}

// IPGeolocation is the fallback when no GPS hardware is available.
// It resolves the approximate coordinate from the outbound IP address.
type ipGeoResponse struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
	City string `json:"city"`
	Country string `json:"country"`
}

// FromIP resolves an approximate coordinate from the machine's outbound IP.
// This is the fallback source — less accurate but always available.
func FromIP() (Coordinate, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://ip-api.com/json/")
	if err != nil {
		return Coordinate{}, fmt.Errorf("ip geolocation failed: %w", err)
	}
	defer resp.Body.Close()

	var geo ipGeoResponse
	if err := json.NewDecoder(resp.Body).Decode(&geo); err != nil {
		return Coordinate{}, fmt.Errorf("ip geolocation parse failed: %w", err)
	}

	return Coordinate{
		Latitude:  geo.Lat,
		Longitude: geo.Lon,
		Accuracy:  5000, // IP-based accuracy ~5km radius
		Timestamp: time.Now().UTC(),
		Source:    "ip",
	}, nil
}

// Manual creates a coordinate from explicitly supplied values.
// Used when GPS and IP are both unavailable or overridden.
func Manual(lat, lon, alt float64) Coordinate {
	return Coordinate{
		Latitude:  lat,
		Longitude: lon,
		Altitude:  alt,
		Accuracy:  0,
		Timestamp: time.Now().UTC(),
		Source:    "manual",
	}
}

// CoordinateEvent is a single streamed GPS sample, optionally carrying an error.
type CoordinateEvent struct {
	Coord Coordinate
	Err   error
}

// Watch starts a real-time GPS polling loop at the given interval.
// Returns a read-only channel of CoordinateEvents.
// The channel is closed when ctx is cancelled.
//
// This is the electric/HERE_AND_NOW layer of the mosaic runtime:
// the compile-time anchor (canonical coordinate) was set at session start;
// Watch continuously emits the runtime position so drift can be computed.
func Watch(ctx context.Context, interval time.Duration) <-chan CoordinateEvent {
	ch := make(chan CoordinateEvent, 1)
	go func() {
		defer close(ch)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				coord, err := FromIP()
				select {
				case ch <- CoordinateEvent{Coord: coord, Err: err}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return ch
}

// GpsDrift computes the mosaic DriftVector components for GPS coordinates.
// Canonical centre C(t) is the session-start position.
// Current vector V(t) is the latest polled coordinate.
//
// Mapping onto the mosaic DriftCarcass:
//   R(t)   — Haversine distance from canonical anchor (metres)
//   Omega  — bearing angle from anchor to current (degrees, 0=N clockwise)
//   H(t)   — longitudinal (east-west) displacement (decimal degrees)
//   L(t)   — loyalty: 1.0 at anchor, approaches 0 at MaxDriftMetres
//
// Constitutional threshold: if angular deviation from expected bearing
// exceeds 90° the NSIGII VERIFY interrupt must fire.
type GpsDrift struct {
	R     float64 // radial — metres from anchor
	Omega float64 // angular — bearing degrees (0–360)
	H     float64 // horizontal — longitude delta (decimal degrees)
	L     float64 // loyalty — [0,1]
	Safe  bool    // false when Omega deviation >= 90° from canonical heading
}

// MaxDriftMetres is the constitutional maximum before loyalty collapses to zero.
const MaxDriftMetres = 50_000.0 // 50 km

// ComputeGpsDrift maps GPS displacement onto the mosaic DriftCarcass.
// canonical is the session-start coordinate (compile-time anchor).
// current is the latest runtime coordinate.
func ComputeGpsDrift(canonical, current Coordinate) GpsDrift {
	dist := canonical.DistanceTo(current)
	bearing := bearingDeg(canonical, current)
	hDelta := current.Longitude - canonical.Longitude
	loyalty := math.Max(0.0, 1.0-(dist/MaxDriftMetres))

	// Safety check: deviation from due-north canonical heading >= 90° → unsafe
	// (mirrors the mosaic 90° Omega threshold for NSIGII VERIFY)
	deviation := math.Abs(bearing - 0) // deviation from north baseline
	if deviation > 180 {
		deviation = 360 - deviation
	}
	safe := deviation < 90.0

	return GpsDrift{
		R:     dist,
		Omega: bearing,
		H:     hDelta,
		L:     loyalty,
		Safe:  safe,
	}
}

// bearingDeg computes the initial bearing (degrees, 0=N, clockwise) from a to b.
func bearingDeg(a, b Coordinate) float64 {
	lat1 := a.Latitude * math.Pi / 180
	lat2 := b.Latitude * math.Pi / 180
	dLon := (b.Longitude - a.Longitude) * math.Pi / 180
	y := math.Sin(dLon) * math.Cos(lat2)
	x := math.Cos(lat1)*math.Sin(lat2) - math.Sin(lat1)*math.Cos(lat2)*math.Cos(dLon)
	bearing := math.Atan2(y, x) * 180 / math.Pi
	return math.Mod(bearing+360, 360)
}
