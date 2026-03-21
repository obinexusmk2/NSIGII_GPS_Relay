// Package gps provides real-time GPS coordinate capture for ltcodec.
// This forms the physical-layer anchor of the space-time fingerprint.
//
// Pipeline: riftlang.exe → .so.a → rift.exe → gosilang → ltcodec → nsigii
// Orchestration: nlink → polybuild
package gps

import (
	"fmt"
	"math"
	"net/http"
	"encoding/json"
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
