// codec.go — LTF packet stub for NSIGII GPS Relay
// OBINexus Computing
//
// The full LTF codec implementation lives in ./ltcodec/
// This file provides the LTFPacket type for relay session use.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
)

// LTFPacket binds a payload to a spacetime state producing a self-proving unit.
type LTFPacket struct {
	Magic       string    `json:"magic"`
	Version     string    `json:"version"`
	PayloadType string    `json:"payload_type"`
	Payload     []byte    `json:"payload"`
	PacketHash  string    `json:"packet_hash"`
	SignedAt    time.Time `json:"signed_at"`
}

// ToJSON serialises the packet.
func (p LTFPacket) ToJSON() ([]byte, error) {
	return json.MarshalIndent(p, "", "  ")
}

// Verify checks that the packet hash is consistent with its payload.
func (p LTFPacket) Verify() bool {
	h := sha256.New()
	h.Write([]byte(p.PacketHash))
	h.Write(p.Payload)
	return p.PacketHash == fmt.Sprintf("%x", h.Sum(nil))
}
