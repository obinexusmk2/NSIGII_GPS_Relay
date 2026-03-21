// Package identity provides hardware-layer identity for ltcodec.
// Implements the MAC → UUID v6 → dual-IP fingerprint described in
// the OBINexus Cloudflare lava lamp analysis.
//
// Every system on the network gets a unique, non-reproducible identity:
//   AbsoluteMAC (hardware) → VirtualMAC (IPv4) → VirtualMAC2 (IPv6)
package identity

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"time"
)

// HardwareID represents the physical identity of a machine
type HardwareID struct {
	MAC        string `json:"mac"`         // Physical MAC address
	Hostname   string `json:"hostname"`
	IPv4       string `json:"ipv4"`        // First IP address
	IPv6       string `json:"ipv6"`        // Second IP address (v6)
	UUIDV6     string `json:"uuid_v6"`     // Time-based UUID derived from MAC
	CapturedAt time.Time `json:"captured_at"`
}

// String returns a compact identity string
func (h HardwareID) String() string {
	return fmt.Sprintf("MAC:%s | IPv4:%s | IPv6:%s | UUID:%s",
		h.MAC, h.IPv4, h.IPv6, h.UUIDV6)
}

// Capture reads the current machine's hardware identity
func Capture() (HardwareID, error) {
	id := HardwareID{CapturedAt: time.Now().UTC()}

	ifaces, err := net.Interfaces()
	if err != nil {
		return id, fmt.Errorf("interface enumeration failed: %w", err)
	}

	for _, iface := range ifaces {
		// Skip loopback and interfaces without MAC
		if iface.Flags&net.FlagLoopback != 0 || len(iface.HardwareAddr) == 0 {
			continue
		}

		id.MAC = iface.HardwareAddr.String()

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ip, _, err := net.ParseCIDR(addr.String())
			if err != nil {
				continue
			}
			if ip.IsLoopback() {
				continue
			}
			if ip.To4() != nil && id.IPv4 == "" {
				id.IPv4 = ip.String()
			} else if ip.To4() == nil && id.IPv6 == "" {
				id.IPv6 = ip.String()
			}
		}

		if id.MAC != "" {
			break // Use first non-loopback interface
		}
	}

	// Generate UUID v6 from MAC + timestamp
	id.UUIDV6 = generateUUIDv6(id.MAC)

	return id, nil
}

// generateUUIDv6 produces a time-ordered UUID anchored to the MAC address.
// Format: xxxxxxxx-xxxx-6xxx-yxxx-xxxxxxxxxxxx
// The '6' version nibble marks this as a time-ordered (v6) UUID.
func generateUUIDv6(mac string) string {
	now := time.Now().UnixNano()

	// Build 16 bytes: 6 bytes time + 2 bytes clock + 6 bytes MAC
	var b [16]byte

	// Time component (48 bits, big-endian)
	b[0] = byte(now >> 40)
	b[1] = byte(now >> 32)
	b[2] = byte(now >> 24)
	b[3] = byte(now >> 16)
	b[4] = byte(now >> 8)
	b[5] = byte(now)

	// Clock sequence (random 14 bits)
	rand.Read(b[6:8])
	b[6] = (b[6] & 0x0f) | 0x60 // Version 6
	b[8] = (b[8] & 0x3f) | 0x80 // Variant bits

	// Node: MAC address bytes
	macClean := strings.ReplaceAll(mac, ":", "")
	macBytes, err := hex.DecodeString(macClean)
	if err == nil && len(macBytes) >= 6 {
		copy(b[10:], macBytes[:6])
	} else {
		rand.Read(b[10:]) // Fallback: random node
	}

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
