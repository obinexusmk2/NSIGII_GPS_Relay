// spacetime.go — Spacetime state anchor for NSIGII GPS Relay
// OBINexus Computing
//
// SpacetimeState = WHO (hardware) + WHERE (GPS) + WHEN (timestamp)
// This is the non-reproducible proof of existence at a given moment.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
)

// SpacetimeState is the fundamental anchor unit.
type SpacetimeState struct {
	Hardware    HardwareID   `json:"hardware"`
	Position    LocationData `json:"position"`
	DeltaT      time.Time    `json:"delta_t"`
	Sequence    uint64       `json:"sequence"`
	Elapsed     time.Duration `json:"elapsed"`
	Fingerprint string       `json:"fingerprint"`
}

// ToJSON serialises the state.
func (s SpacetimeState) ToJSON() ([]byte, error) {
	return json.MarshalIndent(s, "", "  ")
}

// String returns a compact summary.
func (s SpacetimeState) String() string {
	return fmt.Sprintf("[SEQ:%d] %s | %.6f,%.6f | %s",
		s.Sequence, s.Hardware.MAC,
		s.Position.Lat, s.Position.Lon,
		s.DeltaT.Format(time.RFC3339Nano))
}

// SpacetimeSession records a sequence of states over time.
type SpacetimeSession struct {
	ID       string
	Start    time.Time
	States   []SpacetimeState
	sequence uint64
}

// NewSpacetimeSession initialises a session anchored to hardware identity.
func NewSpacetimeSession() (*SpacetimeSession, error) {
	hw, err := CaptureHardwareID()
	if err != nil {
		return nil, fmt.Errorf("identity capture: %w", err)
	}
	now := time.Now().UTC()
	h := sha256.Sum256([]byte(hw.UUIDV6 + now.String()))
	return &SpacetimeSession{
		ID:    fmt.Sprintf("%x", h[:8]),
		Start: now,
	}, nil
}

// Capture records a new spacetime state.
func (sess *SpacetimeSession) Capture(loc LocationData) (SpacetimeState, error) {
	hw, err := CaptureHardwareID()
	if err != nil {
		return SpacetimeState{}, fmt.Errorf("identity capture: %w", err)
	}
	now := time.Now().UTC()
	sess.sequence++
	s := SpacetimeState{
		Hardware: hw,
		Position: loc,
		DeltaT:   now,
		Sequence: sess.sequence,
		Elapsed:  now.Sub(sess.Start),
	}
	data := fmt.Sprintf("%s|%.6f|%.6f|%d|%s",
		hw.UUIDV6, loc.Lat, loc.Lon, now.UnixNano(), hw.IPv4)
	h := sha256.Sum256([]byte(data))
	s.Fingerprint = fmt.Sprintf("%x", h)
	sess.States = append(sess.States, s)
	return s, nil
}

// Replay returns the state at a given sequence number.
func (sess *SpacetimeSession) Replay(seq uint64) (SpacetimeState, bool) {
	for _, s := range sess.States {
		if s.Sequence == seq {
			return s, true
		}
	}
	return SpacetimeState{}, false
}
