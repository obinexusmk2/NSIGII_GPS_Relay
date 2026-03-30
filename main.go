// ltcodec - Linkable Then Codec
// OBINexus Computing | github.com/obinexus/ltcodec
//
// Usage:
//   ltcodec encode -input <file> -type <payload_type>
//   ltcodec decode -input <file.ltf>
//   ltcodec state                    # print current spacetime state
//   ltcodec replay -time <RFC3339>   # replay state at given time
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/obinexus/ltcodec/pkg/codec"
	"github.com/obinexus/ltcodec/pkg/gps"
)

const banner = `
╔═══════════════════════════════════════════╗
║  ltcodec v1.0.0 — OBINexus Computing     ║
║  Linkable Then Format (LTF) Codec         ║
║  Pipeline: riftlang → nlink → polybuild  ║
╚═══════════════════════════════════════════╝
`

func main() {
	fmt.Print(banner)

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// Default GPS resolver: try IP geolocation
	resolver := func() (gps.Coordinate, error) {
		log.Println("Resolving location via IP geolocation...")
		coord, err := gps.FromIP()
		if err != nil {
			log.Printf("IP geolocation failed: %v — using zero coordinate", err)
			return gps.Coordinate{Source: "unavailable"}, nil
		}
		log.Printf("Location resolved: %.4f, %.4f [%s]",
			coord.Latitude, coord.Longitude, coord.Source)
		return coord, nil
	}

	c, err := codec.NewCodec(resolver)
	if err != nil {
		log.Fatalf("Codec init failed: %v", err)
	}

	switch os.Args[1] {
	case "encode":
		runEncode(c)
	case "decode":
		runDecode(c)
	case "state":
		runState(c)
	case "replay":
		runReplay(c)
	case "relay":
		runRelay(c)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func runEncode(c *codec.Codec) {
	fs := flag.NewFlagSet("encode", flag.ExitOnError)
	inputFile := fs.String("input", "", "input file to encode")
	payloadType := fs.String("type", "raw", "payload type (raw, legal, nsigii)")
	outputFile := fs.String("output", "", "output .ltf file (default: <input>.ltf)")
	fs.Parse(os.Args[2:])

	if *inputFile == "" {
		log.Fatal("encode: -input required")
	}

	data, err := os.ReadFile(*inputFile)
	if err != nil {
		log.Fatalf("encode: read failed: %v", err)
	}

	packet, err := c.Encode(*payloadType, data)
	if err != nil {
		log.Fatalf("encode: %v", err)
	}

	out := *outputFile
	if out == "" {
		out = *inputFile + ".ltf"
	}

	packetJSON, err := packet.ToJSON()
	if err != nil {
		log.Fatalf("encode: serialise failed: %v", err)
	}

	if err := os.WriteFile(out, packetJSON, 0644); err != nil {
		log.Fatalf("encode: write failed: %v", err)
	}

	fmt.Printf("\n✓ Encoded → %s\n", out)
	fmt.Printf("  Spacetime fingerprint: %s\n", packet.State.Fingerprint)
	fmt.Printf("  Packet hash:           %s\n", packet.PacketHash)
	fmt.Printf("  Sequence:              %d\n", packet.State.Sequence)
	fmt.Printf("  Location:              %s\n", packet.State.Position.String())
	fmt.Printf("  Hardware:              %s\n", packet.State.Hardware.MAC)
}

func runDecode(c *codec.Codec) {
	fs := flag.NewFlagSet("decode", flag.ExitOnError)
	inputFile := fs.String("input", "", "input .ltf file")
	fs.Parse(os.Args[2:])

	if *inputFile == "" {
		log.Fatal("decode: -input required")
	}

	// Load from JSON
	data, err := os.ReadFile(*inputFile)
	if err != nil {
		log.Fatalf("decode: read failed: %v", err)
	}

	// Re-encode with current state to verify
	fmt.Printf("\n✓ Packet loaded from %s\n", *inputFile)
	fmt.Printf("  Raw size: %d bytes\n", len(data))
	fmt.Printf("  Use the state subcommand to inspect the spacetime anchor\n")
}

func runState(c *codec.Codec) {
	// Capture a fresh state and display it
	resolver := func() (gps.Coordinate, error) {
		return gps.FromIP()
	}

	coord, _ := resolver()
	state, err := c.Session.Capture(coord)
	if err != nil {
		log.Fatalf("state: %v", err)
	}

	stateJSON, _ := state.ToJSON()
	fmt.Printf("\nCurrent spacetime state:\n%s\n", string(stateJSON))
}

func runReplay(c *codec.Codec) {
	fs := flag.NewFlagSet("replay", flag.ExitOnError)
	timeStr := fs.String("time", "", "RFC3339 timestamp to replay (e.g. 2026-02-27T07:10:00Z)")
	seq := fs.Uint64("seq", 0, "sequence number to replay")
	fs.Parse(os.Args[2:])

	if *seq > 0 {
		state, ok := c.Session.Replay(uint64(*seq))
		if !ok {
			log.Fatalf("replay: sequence %d not found", *seq)
		}
		stateJSON, _ := state.ToJSON()
		fmt.Printf("\nReplayed state [seq=%d]:\n%s\n", *seq, string(stateJSON))
		return
	}

	if *timeStr != "" {
		t, err := time.Parse(time.RFC3339, *timeStr)
		if err != nil {
			log.Fatalf("replay: invalid time format: %v", err)
		}
		state, ok := c.ReplayAt(t)
		if !ok {
			log.Fatal("replay: no states in session yet")
		}
		stateJSON, _ := state.ToJSON()
		fmt.Printf("\nReplayed state [t=%s]:\n%s\n", *timeStr, string(stateJSON))
		return
	}

	log.Fatal("replay: specify -time or -seq")
}

func runRelay(c *codec.Codec) {
	fs := flag.NewFlagSet("relay", flag.ExitOnError)
	intervalSec := fs.Int("interval", 5, "GPS poll interval in seconds")
	maxStates   := fs.Int("max-states", 0, "rolling window size (0 = unlimited)")
	verbose     := fs.Bool("verbose", false, "print full state JSON on each tick")
	output      := fs.String("output", "", "optional JSON log file")
	fs.Parse(os.Args[2:])

	cfg := RelayConfig{
		Interval:    time.Duration(*intervalSec) * time.Second,
		MaxStates:   *maxStates,
		VerboseMode: *verbose,
		OutputFile:  *output,
	}

	fmt.Printf("\n[RELAY] NSIGII GPS Real-Time Relay starting\n")
	fmt.Printf("[RELAY] Poll interval: %v | Max states: %d\n", cfg.Interval, cfg.MaxStates)
	fmt.Printf("[RELAY] Press Ctrl+C to stop and print session summary\n\n")

	RunRelay(c, cfg)
}

func printUsage() {
	fmt.Println(`
Usage: ltcodec <command> [flags]

Commands:
  encode   -input <file> [-type <type>] [-output <file>]
  decode   -input <file.ltf>
  state                              Print current spacetime state
  replay   -time <RFC3339>           Replay state at given time
           -seq  <n>                 Replay state at sequence n
  relay    [-interval <sec>]         Start real-time GPS relay loop
           [-max-states <n>]         Rolling window size (0 = unlimited)
           [-verbose]                Print full state JSON each tick
           [-output <file>]          Log relay events to JSON file

Payload types:
  raw      Raw binary data (default)
  legal    Legal document (Care Act, HRA claims)
  nsigii   NSIGII video codec output

Real-time relay (mosaic phase model):
  Phase 1 (compile-time): canonical GPS anchor captured at session start
  Phase 2 (runtime):      continuous GPS polling with drift monitoring
  NSIGII VERIFY fires when Omega drift >= 90° from canonical heading

Pipeline: riftlang.exe → .so.a → rift.exe → gosilang → ltcodec → nsigii → relay
Orchestration: nlink → polybuild
`)
}
