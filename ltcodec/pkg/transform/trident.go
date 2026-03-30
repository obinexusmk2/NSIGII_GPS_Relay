// Package transform implements NSIGII isomorphic transformation and trident verification.
package transform

import (
	"math"
)

// TridentState represents discriminant-derived state.
type TridentState int

const (
	StateOrder TridentState = iota // Δ > 0
	StateConsensus                  // Δ = 0
	StateChaos                      // Δ < 0
)

func (s TridentState) String() string {
	switch s {
	case StateOrder:
		return "ORDER"
	case StateConsensus:
		return "CONSENSUS"
	default:
		return "CHAOS"
	}
}

// RWXFlags mirrors Unix permissions.
const (
	RWXRead    uint8 = 0x04
	RWXWrite   uint8 = 0x02
	RWXExecute uint8 = 0x01
	RWXFull    uint8 = 0x07
)

// TridentResult carries verification outcome.
type TridentResult struct {
	State        TridentState
	RWXFlags     uint8
	WheelDeg     int
	Discriminant float64
	Verified     bool
	Polarity     byte
}

// RunTrident executes verification pipeline. READ-ONLY.
func RunTrident(data []byte) TridentResult {
	// Channel 0 & 1: Pass-through (no mutation)
	transmitted := transmit(data)
	received := receive(transmitted)

	// Channel 2: Discriminant verification
	a, b, c := bipartiteConsensusParams(received)
	delta := b*b - 4*a*c

	var state TridentState
	var rwx uint8
	var wheelDeg int

	switch {
	case delta > 0.001: // ORDER with tolerance
		state = StateOrder
		rwx = RWXFull
		wheelDeg = 120
	case delta < -0.001: // CHAOS with tolerance
		state = StateChaos
		rwx = RWXRead
		wheelDeg = 0
	default: // CONSENSUS (near zero)
		state = StateConsensus
		rwx = RWXFull
		wheelDeg = 240
	}

	return TridentResult{
		State:        state,
		RWXFlags:     rwx,
		WheelDeg:     wheelDeg,
		Discriminant: delta,
		Verified:     state != StateChaos,
		Polarity:     PolaritySign(data),
	}
}

// Internal helpers — READ-ONLY

func transmit(data []byte) []byte {
	out := make([]byte, len(data))
	copy(out, data)
	return out
}

func receive(data []byte) []byte {
	out := make([]byte, len(data))
	copy(out, data)
	return out
}

// bipartiteConsensusParams: A=1, B based on entropy, C=1
// For normal data: B≈2 → Δ≈0 (CONSENSUS)
// For ordered data (low entropy): B>2 → Δ>0 (ORDER)
// For chaotic data (high entropy): B<2 → Δ<0 (CHAOS)
func bipartiteConsensusParams(data []byte) (a, b, c float64) {
	if len(data) == 0 {
		return 1.0, 2.0, 1.0
	}
	
	// Count byte patterns (not bits)
	var transitions int
	for i := 1; i < len(data); i++ {
		if data[i] != data[i-1] {
			transitions++
		}
	}
	
	// High transition rate = chaotic, Low = ordered
	ratio := float64(transitions) / float64(len(data)-1)
	B := 4.0 * (1.0 - ratio) // Few transitions = ORDER (B≈4), Many = CHAOS (B≈0)
	
	return 1.0, B, 1.0
}