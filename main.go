// NSIGII GPS Relay — OBINexus Computing
// Real-time GPS web server with LTF spacetime fingerprint.
//
// Commands:
//   serve    Start the real-time GPS web server (default port 8080)
//   state    Print current spacetime state as JSON
//   relay    Start headless GPS relay with NSIGII drift monitoring
//
// Pipeline: riftlang.exe → .so.a → rift.exe → gosilang → nsigii_gps_relay
// Orchestration: nlink → polybuild
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const banner = `
╔══════════════════════════════════════════════════╗
║  NSIGII GPS Relay — OBINexus Computing          ║
║  Real-time spacetime fingerprint relay          ║
║  Pipeline: riftlang → nlink → polybuild         ║
╚══════════════════════════════════════════════════╝
`

func main() {
	fmt.Print(banner)

	cmd := "serve"
	if len(os.Args) >= 2 {
		cmd = os.Args[1]
	}

	switch cmd {
	case "serve":
		runServe()
	case "state":
		runStateCmd()
	case "relay":
		runRelayCmd()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

// ── serve: HTTP web server ────────────────────────────────────────────────────

func runServe() {
	port := ":8080"
	if len(os.Args) >= 3 {
		port = ":" + os.Args[2]
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Phase 1 (magnetic/compile-time): capture canonical GPS anchor
	log.Println("[NSIGII] Phase 1 — capturing canonical GPS anchor (THERE_AND_THEN)")
	go runPoller(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("/", serveIndex)
	mux.HandleFunc("/api/location", handleAPILocation)
	mux.HandleFunc("/api/state", handleAPIState)
	mux.HandleFunc("/events", handleSSE)

	srv := &http.Server{
		Addr:         port,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // SSE needs no write timeout
	}

	// Graceful shutdown
	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
		<-ch
		log.Println("[NSIGII] Signal received — canonical restore (#NoGhosting)")
		cancel()
		srv.Shutdown(context.Background())
	}()

	log.Printf("[NSIGII] Phase 2 — electric runtime active (HERE_AND_NOW)")
	log.Printf("[NSIGII] Web server → http://localhost%s", port)
	log.Printf("[NSIGII] Open your browser: http://localhost%s", port)

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

func handleAPILocation(w http.ResponseWriter, r *http.Request) {
	loc := currentLocation()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(loc)
}

func handleAPIState(w http.ResponseWriter, r *http.Request) {
	canonical, current := getCanonicalAndCurrent()
	state := map[string]interface{}{
		"canonical": canonical,
		"current":   current,
		"phase":     "electric_runtime",
		"pipeline":  "riftlang → nlink → polybuild",
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(state)
}

// handleSSE streams location updates via Server-Sent Events.
// The browser connects once and receives a push on every GPS poll tick.
func handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := subscribeSSE()
	defer unsubscribeSSE(ch)

	// Send current location immediately on connect
	if loc := currentLocation(); loc.Lat != 0 || loc.Lon != 0 {
		data, _ := json.Marshal(loc)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	for {
		select {
		case loc, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(loc)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// ── state command ────────────────────────────────────────────────────────────

func runStateCmd() {
	log.Println("[NSIGII] Fetching current location...")
	loc, err := fetchFromIP()
	if err != nil {
		log.Fatalf("location fetch failed: %v", err)
	}
	data, _ := json.MarshalIndent(loc, "", "  ")
	fmt.Printf("\nCurrent spacetime state:\n%s\n", string(data))
}

// ── relay command (headless drift monitor) ───────────────────────────────────

func runRelayCmd() {
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
		<-ch
		log.Println("[RELAY] Signal — canonical restore (#NoGhosting)")
		cancel()
	}()

	log.Println("[RELAY] Headless GPS drift relay starting (Ctrl+C to stop)")
	runDriftRelay(ctx)
}

// ── usage ────────────────────────────────────────────────────────────────────

func printUsage() {
	fmt.Println(`
Usage: nsigii_gps_relay [command] [port]

Commands:
  serve [port]   Start GPS web server (default port: 8080)
                 Open http://localhost:8080 to view real-time map
  state          Print current GPS state as JSON
  relay          Headless drift relay with NSIGII VERIFY interrupt

Examples:
  nsigii_gps_relay serve
  nsigii_gps_relay serve 9090
  nsigii_gps_relay state
  nsigii_gps_relay relay
`)
}
