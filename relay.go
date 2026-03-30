// relay.go — NSIGII Real-Time GPS Relay
// OBINexus Computing | NSIGII GPS System
//
// Implements the HERE_AND_NOW (electric/runtime) phase of the mosaic model
// applied to the GPS spacetime fingerprint pipeline.
//
// Mosaic phase mapping:
//   Phase 1 (magnetic/compile-time / THERE_AND_THEN):
//     Session.Start coordinate = canonical anchor = lattice crystallised.
//   Phase 2 (electric/runtime / HERE_AND_NOW):
//     Watch() polls GPS continuously = lattice reorientation loop.
//
// Constitutional safety:
//   When GPS drift Omega >= 90° from canonical heading →
//   NSIGII VERIFY interrupt fires (mirrors mosaic 90° DriftCarcass threshold).
//   When drift is safe → L(t) loyalty reported; relay continues.
//
// Pipeline: riftlang.exe → .so.a → rift.exe → gosilang → ltcodec → nsigii → relay
// Orchestration: nlink → polybuild
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/obinexus/ltcodec/pkg/codec"
	"github.com/obinexus/ltcodec/pkg/gps"
)

// RelayConfig controls the real-time relay behaviour.
type RelayConfig struct {
	Interval    time.Duration // GPS poll interval (default: 5s)
	MaxStates   int           // rolling window of states to retain (0 = unlimited)
	OutputFile  string        // optional JSON log file ("-" = stdout only)
	VerboseMode bool          // print full state JSON on each capture
}

// DefaultRelayConfig returns the constitutional default configuration.
// 5-second polling, unlimited state retention, stdout only.
func DefaultRelayConfig() RelayConfig {
	return RelayConfig{
		Interval:    5 * time.Second,
		MaxStates:   0,
		OutputFile:  "",
		VerboseMode: false,
	}
}

// NSIGIIRelay is the real-time GPS spacetime relay.
//
// It continuously captures GPS coordinates, anchors each to a spacetime
// state, and runs the mosaic-derived NSIGII drift monitor on every tick.
// When constitutional drift threshold is breached, the VERIFY interrupt
// fires and the event is logged to telemetry.
type NSIGIIRelay struct {
	codec     *codec.Codec
	config    RelayConfig
	out       io.Writer
	canonical gps.Coordinate // compile-time anchor: session-start position
	stateLog  []RelayEvent
}

// RelayEvent is one tick in the real-time relay session.
// It captures the spacetime state AND the mosaic drift report.
type RelayEvent struct {
	Sequence  uint64         `json:"seq"`
	Timestamp time.Time      `json:"ts"`
	Coord     gps.Coordinate `json:"coord"`
	Drift     gps.GpsDrift   `json:"drift"`
	Verify    bool           `json:"nsigii_verify"` // true = safety interrupt fired
	Fingerprint string       `json:"fingerprint"`
}

// NewNSIGIIRelay constructs a relay with an established codec session.
// The canonical anchor is set here — this is the magnetic/compile-time phase.
func NewNSIGIIRelay(c *codec.Codec, cfg RelayConfig, out io.Writer) (*NSIGIIRelay, error) {
	if out == nil {
		out = os.Stdout
	}

	// Phase 1: capture canonical (compile-time) anchor
	anchor, err := gps.FromIP()
	if err != nil {
		anchor = gps.Coordinate{Source: "unavailable"}
		fmt.Fprintf(out, "[RELAY] ⚠ Canonical anchor unavailable — using zero coordinate\n")
	}

	fmt.Fprintf(out, "\n[RELAY] ── Phase 1: Magnetic / Compile-Time ─────────────────────\n")
	fmt.Fprintf(out, "[RELAY] Canonical anchor (THERE_AND_THEN): %s\n", anchor.String())
	fmt.Fprintf(out, "[RELAY] ── Phase 2: Electric / Runtime active (HERE_AND_NOW) ────\n\n")

	return &NSIGIIRelay{
		codec:     c,
		config:    cfg,
		out:       out,
		canonical: anchor,
	}, nil
}

// Run starts the real-time GPS relay loop.
// Blocks until ctx is cancelled or an OS signal is received.
//
// On every tick:
//   1. Poll GPS coordinate
//   2. Capture spacetime state (sequence + fingerprint)
//   3. Compute mosaic-derived GpsDrift against canonical anchor
//   4. If drift unsafe → fire NSIGII VERIFY interrupt
//   5. Log RelayEvent to session
func (r *NSIGIIRelay) Run(ctx context.Context) error {
	events := gps.Watch(ctx, r.config.Interval)
	var seq uint64

	for {
		select {
		case <-ctx.Done():
			r.printSummary()
			return nil

		case ev, ok := <-events:
			if !ok {
				r.printSummary()
				return nil
			}

			if ev.Err != nil {
				fmt.Fprintf(r.out, "[RELAY] GPS error: %v — using canonical anchor\n", ev.Err)
				ev.Coord = r.canonical
			}

			// Spacetime capture
			state, err := r.codec.Session.Capture(ev.Coord)
			if err != nil {
				fmt.Fprintf(r.out, "[RELAY] State capture error: %v\n", err)
				continue
			}

			// Mosaic drift computation: D(t) = V(t) - C(t)
			drift := gps.ComputeGpsDrift(r.canonical, ev.Coord)

			seq++
			relayEv := RelayEvent{
				Sequence:    seq,
				Timestamp:   state.DeltaT,
				Coord:       ev.Coord,
				Drift:       drift,
				Verify:      !drift.Safe,
				Fingerprint: state.Fingerprint,
			}

			// Constitutional NSIGII VERIFY interrupt
			if !drift.Safe {
				fmt.Fprintf(r.out,
					"[RELAY] ⚠ NSIGII VERIFY INTERRUPT — drift Ω=%.1f° ≥90° — memory safety violation\n",
					drift.Omega)
				fmt.Fprintf(r.out,
					"[RELAY]   Carcass: R=%.1fm Ω=%.1f° H=%.5f° L(t)=%.4f\n",
					drift.R, drift.Omega, drift.H, drift.L)
			} else {
				fmt.Fprintf(r.out,
					"[RELAY] [SEQ:%d] R=%.1fm Ω=%.1f° H=%+.5f° L(t)=%.4f | fp=%s...\n",
					seq, drift.R, drift.Omega, drift.H, drift.L,
					state.Fingerprint[:12])
			}

			if r.config.VerboseMode {
				stateJSON, _ := state.ToJSON()
				fmt.Fprintf(r.out, "%s\n", string(stateJSON))
			}

			// Rolling window
			r.stateLog = append(r.stateLog, relayEv)
			if r.config.MaxStates > 0 && len(r.stateLog) > r.config.MaxStates {
				r.stateLog = r.stateLog[len(r.stateLog)-r.config.MaxStates:]
			}
		}
	}
}

// printSummary emits the relay session summary: total events, safety violations.
func (r *NSIGIIRelay) printSummary() {
	total := len(r.stateLog)
	violations := 0
	for _, ev := range r.stateLog {
		if ev.Verify {
			violations++
		}
	}

	fmt.Fprintf(r.out, "\n╔══ NSIGIIRelay Session Summary ════════════════════════\n")
	fmt.Fprintf(r.out, "║  Total events:     %d\n", total)
	fmt.Fprintf(r.out, "║  Safe events:      %d\n", total-violations)
	fmt.Fprintf(r.out, "║  VERIFY interrupts: %d\n", violations)
	if total > 0 {
		lastEv := r.stateLog[total-1]
		fmt.Fprintf(r.out, "║  Last R(t):        %.1fm\n", lastEv.Drift.R)
		fmt.Fprintf(r.out, "║  Last L(t):        %.4f\n", lastEv.Drift.L)
		fmt.Fprintf(r.out, "║  Last fingerprint: %s...\n", lastEv.Fingerprint[:16])
	}
	fmt.Fprintf(r.out, "╚════════════════════════════════════════════════════\n\n")
}

// RunRelay is the entry point called from main for the `relay` subcommand.
// Sets up signal handling and starts the relay loop.
func RunRelay(c *codec.Codec, cfg RelayConfig) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Graceful shutdown on SIGINT / SIGTERM
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\n[RELAY] Signal received — canonical restore initiated (#NoGhosting)")
		cancel()
	}()

	relay, err := NewNSIGIIRelay(c, cfg, os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "relay init failed: %v\n", err)
		os.Exit(1)
	}

	if err := relay.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "relay error: %v\n", err)
		os.Exit(1)
	}
}
