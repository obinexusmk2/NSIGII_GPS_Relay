// gps.go — GPS coordinate service for NSIGII GPS Relay
// OBINexus Computing
//
// Provides:
//   - LocationData: GPS fix with mosaic DriftCarcass fields
//   - fetchFromIP: IP geolocation via ip-api.com (stdlib only)
//   - runPoller: background GPS poll loop (electric/runtime phase)
//   - SSE subscriber bus: push updates to connected browsers
//   - runDriftRelay: headless relay with NSIGII VERIFY interrupt
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"sync"
	"time"
)

// LocationData is a GPS fix with NSIGII mosaic drift metadata.
//
// Mosaic DriftCarcass mapping:
//   RadialDriftM  ≡ R(t)   — distance from canonical anchor (metres)
//   LoyaltyScore  ≡ L(t)   — fidelity to canonical [0, 1]
//   Safe          ≡ safe   — false when drift > 50 km (NSIGII VERIFY)
type LocationData struct {
	Lat          float64   `json:"lat"`
	Lon          float64   `json:"lon"`
	AccuracyM    float64   `json:"accuracy_m"`
	Source       string    `json:"source"`
	Timestamp    time.Time `json:"timestamp"`
	RadialDriftM float64   `json:"radial_drift_m"` // R(t) from canonical anchor
	LoyaltyScore float64   `json:"loyalty"`         // L(t): 1.0 = at anchor
	Safe         bool      `json:"safe"`
	Sequence     uint64    `json:"seq"`
}

// ── location store ────────────────────────────────────────────────────────────

var (
	locMu        sync.RWMutex
	latestLoc    LocationData
	canonicalLoc LocationData
	hasCanonical bool
	locSeq       uint64
)

func currentLocation() LocationData {
	locMu.RLock()
	defer locMu.RUnlock()
	return latestLoc
}

func getCanonicalAndCurrent() (LocationData, LocationData) {
	locMu.RLock()
	defer locMu.RUnlock()
	return canonicalLoc, latestLoc
}

func setLocation(loc LocationData) {
	locMu.Lock()
	defer locMu.Unlock()

	locSeq++
	loc.Sequence = locSeq

	// Phase 1 (magnetic/compile-time): first fix = canonical anchor
	if !hasCanonical {
		canonicalLoc = loc
		hasCanonical = true
		log.Printf("[GPS] Canonical anchor set: %.5f, %.5f [%s]",
			loc.Lat, loc.Lon, loc.Source)
	}

	// Compute mosaic drift relative to canonical anchor
	r := haversineM(canonicalLoc.Lat, canonicalLoc.Lon, loc.Lat, loc.Lon)
	loc.RadialDriftM = r
	loc.LoyaltyScore = math.Max(0, 1.0-(r/50000.0)) // L(t): collapses at 50 km
	loc.Safe = r < 50000

	if !loc.Safe {
		log.Printf("[NSIGII] ⚠ VERIFY INTERRUPT — drift R=%.0fm ≥ 50km threshold", r)
	}

	latestLoc = loc
	broadcastSSE(loc)
}

// ── IP geolocation ────────────────────────────────────────────────────────────

type ipAPIResp struct {
	Status  string  `json:"status"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	City    string  `json:"city"`
	Country string  `json:"countryCode"`
	Region  string  `json:"regionName"`
}

func fetchFromIP() (LocationData, error) {
	cl := &http.Client{Timeout: 8 * time.Second}
	resp, err := cl.Get("http://ip-api.com/json/?fields=status,lat,lon,city,countryCode,regionName")
	if err != nil {
		return LocationData{}, fmt.Errorf("ip-api request: %w", err)
	}
	defer resp.Body.Close()

	var r ipAPIResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return LocationData{}, fmt.Errorf("ip-api parse: %w", err)
	}
	if r.Status != "success" {
		return LocationData{}, fmt.Errorf("ip-api: status=%s", r.Status)
	}

	return LocationData{
		Lat:       r.Lat,
		Lon:       r.Lon,
		AccuracyM: 5000,
		Source:    fmt.Sprintf("ip [%s, %s, %s]", r.City, r.Region, r.Country),
		Timestamp: time.Now().UTC(),
	}, nil
}

// ── poller — electric/runtime phase ─────────────────────────────────────────

// runPoller is the HERE_AND_NOW loop: polls GPS every 10 seconds
// and updates the canonical drift monitor.
func runPoller(ctx context.Context) {
	// Immediate first poll
	if loc, err := fetchFromIP(); err == nil {
		setLocation(loc)
	} else {
		log.Printf("[GPS] Initial poll failed: %v", err)
	}

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if loc, err := fetchFromIP(); err == nil {
				setLocation(loc)
				log.Printf("[GPS] SEQ:%d  %.5f, %.5f  R=%.0fm  L=%.3f  [%s]",
					loc.Sequence, loc.Lat, loc.Lon,
					loc.RadialDriftM, loc.LoyaltyScore, loc.Source)
			} else {
				log.Printf("[GPS] Poll error: %v", err)
			}
		}
	}
}

// ── SSE subscriber bus ────────────────────────────────────────────────────────

var (
	sseMu       sync.Mutex
	sseClients  = map[chan LocationData]struct{}{}
)

func subscribeSSE() chan LocationData {
	ch := make(chan LocationData, 4)
	sseMu.Lock()
	sseClients[ch] = struct{}{}
	sseMu.Unlock()
	return ch
}

func unsubscribeSSE(ch chan LocationData) {
	sseMu.Lock()
	delete(sseClients, ch)
	sseMu.Unlock()
	close(ch)
}

func broadcastSSE(loc LocationData) {
	sseMu.Lock()
	defer sseMu.Unlock()
	for ch := range sseClients {
		select {
		case ch <- loc:
		default: // drop if client is slow
		}
	}
}

// ── headless drift relay ──────────────────────────────────────────────────────

func runDriftRelay(ctx context.Context) {
	go runPoller(ctx)

	ch := subscribeSSE()
	defer unsubscribeSSE(ch)

	for {
		select {
		case <-ctx.Done():
			printRelaySummary()
			return
		case loc, ok := <-ch:
			if !ok {
				return
			}
			status := "SAFE"
			if !loc.Safe {
				status = "⚠ VERIFY"
			}
			fmt.Printf("[RELAY] SEQ:%-4d  %.5f, %.5f  R=%8.0fm  L=%.3f  [%s]\n",
				loc.Sequence, loc.Lat, loc.Lon,
				loc.RadialDriftM, loc.LoyaltyScore, status)
		}
	}
}

func printRelaySummary() {
	_, cur := getCanonicalAndCurrent()
	fmt.Printf("\n╔══ NSIGII Relay Summary ═══════════════════════════\n")
	fmt.Printf("║  Final SEQ:    %d\n", cur.Sequence)
	fmt.Printf("║  R(t):         %.0f m from canonical\n", cur.RadialDriftM)
	fmt.Printf("║  L(t):         %.4f\n", cur.LoyaltyScore)
	fmt.Printf("║  Safe:         %v\n", cur.Safe)
	fmt.Printf("╚════════════════════════════════════════════════════\n\n")
}

// ── maths ─────────────────────────────────────────────────────────────────────

// haversineM returns the great-circle distance in metres between two lat/lon points.
func haversineM(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371000.0
	φ1 := lat1 * math.Pi / 180
	φ2 := lat2 * math.Pi / 180
	Δφ := (lat2 - lat1) * math.Pi / 180
	Δλ := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(Δφ/2)*math.Sin(Δφ/2) +
		math.Cos(φ1)*math.Cos(φ2)*math.Sin(Δλ/2)*math.Sin(Δλ/2)
	return R * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}
