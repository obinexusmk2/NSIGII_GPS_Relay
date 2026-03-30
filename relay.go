// relay.go — NSIGII relay types for NSIGII GPS Relay
// OBINexus Computing
//
// RelayEvent records one GPS tick with full mosaic drift report.
// This file contains types used by the relay subcommand in main.go.
// The relay runtime loop itself lives in gps.go (runDriftRelay).
package main

import "time"

// RelayEvent is one tick in the real-time relay session.
type RelayEvent struct {
	Sequence    uint64       `json:"seq"`
	Timestamp   time.Time    `json:"ts"`
	Location    LocationData `json:"location"`
	NSIGIIVerify bool        `json:"nsigii_verify"` // true = safety interrupt fired
}
