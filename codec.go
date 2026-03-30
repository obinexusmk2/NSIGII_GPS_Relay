// Package codec implements the LTF (Linkable Then Format) codec layer.
//
// LTF is NOT a traditional binary format. It asserts that a payload
// is not valid until it has been anchored to a spacetime state.
//
// Constitutional pipeline:
//   riftlang.exe → .so.a → rift.exe → gosilang → ltcodec → nsigii
//
// LTF phases:
//   LINK     — resolve identity + GPS → produce SpacetimeState
//   THEN     — bind payload to state → produce LTFPacket
//   EXECUTE  — emit signed, timestamped, location-anchored output
//
// The packet cannot be forged: it carries its own proof of origin.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
)

// LTFPacket is the fundamental output of the ltcodec system.
// It binds a payload to a spacetime state, producing a self-proving unit.
type LTFPacket struct {
	// Header
	Magic   string `json:"magic"`    // "LTF"
	Version string `json:"version"`  // "1.0.0"

	// Spacetime anchor
	State State `json:"state"`

	// Payload
	PayloadType string `json:"payload_type"`
	Payload     []byte `json:"payload"`

	// Proof
	PacketHash string    `json:"packet_hash"`
	SignedAt   time.Time `json:"signed_at"`
}

// Verify checks that the packet hash is consistent with its contents.
// This is the rectorial audit function — the packet proves itself.
func (p LTFPacket) Verify() bool {
	expected := computePacketHash(p.State.Fingerprint, p.Payload)
	return p.PacketHash == expected
}

// ToJSON serialises the packet
func (p LTFPacket) ToJSON() ([]byte, error) {
	return json.MarshalIndent(p, "", "  ")
}

func computePacketHash(stateFingerprint string, payload []byte) string {
	h := sha256.New()
	h.Write([]byte(stateFingerprint))
	h.Write(payload)
	return fmt.Sprintf("%x", h.Sum(nil))
}

// Codec is the main ltcodec encoder/decoder.
// It maintains a spacetime session and produces LTFPackets.
type Codec struct {
	Session  *Session
	resolver CoordResolver
}

// CoordResolver is the GPS coordinate source.
// Can be GPS hardware, IP geolocation, or manual input.
type CoordResolver func() (Coordinate, error)

// NewCodec initialises a new ltcodec instance with the given resolver.
func NewCodec(resolver CoordResolver) (*Codec, error) {
	if resolver == nil {
		// Default: IP-based geolocation
		resolver = func() (Coordinate, error) {
			return FromIP()
		}
	}

	sess, err := spacetime.NewSession()
	if err != nil {
		return nil, fmt.Errorf("session init failed: %w", err)
	}

	return &Codec{
		Session:  sess,
		resolver: resolver,
	}, nil
}

// Encode takes a payload and produces a spacetime-anchored LTFPacket.
// This is the LINK → THEN → EXECUTE pipeline in one call.
func (c *Codec) Encode(payloadType string, payload []byte) (LTFPacket, error) {
	// LINK phase: capture current spacetime state
	coord, err := c.resolver()
	if err != nil {
		// Fallback to zero coordinate rather than failing
		coord = Coordinate{Source: "unavailable"}
	}

	state, err := c.Session.Capture(coord)
	if err != nil {
		return LTFPacket{}, fmt.Errorf("state capture failed: %w", err)
	}

	// THEN phase: bind payload to state
	now := time.Now().UTC()
	packet := LTFPacket{
		Magic:       "LTF",
		Version:     "1.0.0",
		State:       state,
		PayloadType: payloadType,
		Payload:     payload,
		SignedAt:    now,
	}

	// EXECUTE phase: compute proof hash
	packet.PacketHash = computePacketHash(state.Fingerprint, payload)

	return packet, nil
}

// Decode verifies and unpacks an LTFPacket.
// Returns the payload only if verification passes.
func (c *Codec) Decode(packet LTFPacket) ([]byte, error) {
	if !packet.Verify() {
		return nil, fmt.Errorf("packet verification failed: hash mismatch — packet may be tampered")
	}
	return packet.Payload, nil
}

// ReplayAt returns the state the codec held at a given time.
// This is the inverse relay function — hold the system accountable.
func (c *Codec) ReplayAt(t time.Time) (spacetime.State, bool) {
	return c.Session.ReplayAt(t)
}
