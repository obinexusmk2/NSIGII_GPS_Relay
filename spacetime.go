// Package spacetime implements the OBINexus space-time fingerprint.
//
// A SpacetimeState is the non-reproducible anchor for every ltcodec packet:
//   GPS coordinate + Hardware identity + delta-T timestamp
//
// This is the entropy source that replaces Cloudflare's lava lamp wall.
// Instead of physical entropy from static geometry, we use:
//   Location (where) + Hardware (who) + Time (when) = unique state
//
// Rectorial reasoning application:
//   The state cannot be denied. It was captured. It is recorded.
//   The inverse relay-replay mechanism can reproduce it at any delta-T.
package spacetime

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/obinexus/ltcodec/pkg/gps"
	"github.com/obinexus/ltcodec/pkg/identity"
)

// State is the fundamental unit of the ltcodec system.
// Every encoded payload is anchored to one State.
type State struct {
	// WHO
	Hardware identity.HardwareID `json:"hardware"`

	// WHERE
	Position gps.Coordinate `json:"position"`

	// WHEN
	DeltaT    time.Time     `json:"delta_t"`     // Absolute timestamp
	Sequence  uint64        `json:"sequence"`    // Monotonic counter
	Elapsed   time.Duration `json:"elapsed"`     // Since session start

	// PROOF
	Fingerprint string `json:"fingerprint"` // SHA-256 of WHO+WHERE+WHEN
}

// String returns a human-readable state summary
func (s State) String() string {
	return fmt.Sprintf("[SEQ:%d] %s | %s | %s",
		s.Sequence,
		s.Hardware.MAC,
		s.Position.String(),
		s.DeltaT.Format(time.RFC3339Nano),
	)
}

// Fingerprint computes the SHA-256 hash of the state.
// This is the non-reproducible proof of existence at this moment.
func computeFingerprint(s *State) string {
	data := fmt.Sprintf("%s|%.6f|%.6f|%d|%s",
		s.Hardware.UUIDV6,
		s.Position.Latitude,
		s.Position.Longitude,
		s.DeltaT.UnixNano(),
		s.Hardware.IPv4,
	)
	h := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", h)
}

// ToJSON serialises the state for transmission
func (s State) ToJSON() ([]byte, error) {
	return json.MarshalIndent(s, "", "  ")
}

// Session manages a sequence of States over time.
// This is the relay-replay mechanism — every state is recorded
// and can be replayed at any delta-T.
type Session struct {
	ID       string
	Start    time.Time
	States   []State
	sequence uint64
}

// NewSession initialises a new spacetime session
func NewSession() (*Session, error) {
	hw, err := identity.Capture()
	if err != nil {
		return nil, fmt.Errorf("identity capture failed: %w", err)
	}

	// Generate session ID from hardware + start time
	now := time.Now().UTC()
	h := sha256.Sum256([]byte(hw.UUIDV6 + now.String()))
	sessionID := fmt.Sprintf("%x", h[:8])

	return &Session{
		ID:    sessionID,
		Start: now,
	}, nil
}

// Capture takes a new spacetime snapshot and appends it to the session.
// coord can be supplied from GPS, IP geolocation, or manual input.
func (sess *Session) Capture(coord gps.Coordinate) (State, error) {
	hw, err := identity.Capture()
	if err != nil {
		return State{}, fmt.Errorf("identity capture failed: %w", err)
	}

	now := time.Now().UTC()
	sess.sequence++

	s := State{
		Hardware:  hw,
		Position:  coord,
		DeltaT:    now,
		Sequence:  sess.sequence,
		Elapsed:   now.Sub(sess.Start),
	}
	s.Fingerprint = computeFingerprint(&s)

	sess.States = append(sess.States, s)
	return s, nil
}

// Replay returns the state at a given sequence number.
// This is the inverse relay mechanism — accountability through replay.
func (sess *Session) Replay(sequence uint64) (State, bool) {
	for _, s := range sess.States {
		if s.Sequence == sequence {
			return s, true
		}
	}
	return State{}, false
}

// ReplayAt returns the state closest to a given time.
// Implements the delta-T replay described in the Turing machine video.
func (sess *Session) ReplayAt(t time.Time) (State, bool) {
	if len(sess.States) == 0 {
		return State{}, false
	}

	closest := sess.States[0]
	minDiff := absDuration(t.Sub(closest.DeltaT))

	for _, s := range sess.States[1:] {
		diff := absDuration(t.Sub(s.DeltaT))
		if diff < minDiff {
			minDiff = diff
			closest = s
		}
	}

	return closest, true
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
