// identity.go — Hardware identity for NSIGII GPS Relay
// OBINexus Computing
//
// Provides MAC → UUID v6 fingerprint for spacetime anchoring.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"time"
)

// HardwareID represents the physical identity of this machine.
type HardwareID struct {
	MAC        string    `json:"mac"`
	Hostname   string    `json:"hostname"`
	IPv4       string    `json:"ipv4"`
	IPv6       string    `json:"ipv6"`
	UUIDV6     string    `json:"uuid_v6"`
	CapturedAt time.Time `json:"captured_at"`
}

// String returns a compact identity string.
func (h HardwareID) String() string {
	return fmt.Sprintf("MAC:%s | IPv4:%s | UUID:%s", h.MAC, h.IPv4, h.UUIDV6)
}

// CaptureHardwareID reads the current machine's hardware identity.
func CaptureHardwareID() (HardwareID, error) {
	id := HardwareID{CapturedAt: time.Now().UTC()}

	ifaces, err := net.Interfaces()
	if err != nil {
		return id, fmt.Errorf("interface enumeration: %w", err)
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || len(iface.HardwareAddr) == 0 {
			continue
		}
		id.MAC = iface.HardwareAddr.String()
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			ip, _, err := net.ParseCIDR(addr.String())
			if err != nil || ip.IsLoopback() {
				continue
			}
			if ip.To4() != nil && id.IPv4 == "" {
				id.IPv4 = ip.String()
			} else if ip.To4() == nil && id.IPv6 == "" {
				id.IPv6 = ip.String()
			}
		}
		if id.MAC != "" {
			break
		}
	}

	id.UUIDV6 = generateUUIDv6(id.MAC)
	return id, nil
}

func generateUUIDv6(mac string) string {
	now := time.Now().UnixNano()
	var b [16]byte
	b[0] = byte(now >> 40)
	b[1] = byte(now >> 32)
	b[2] = byte(now >> 24)
	b[3] = byte(now >> 16)
	b[4] = byte(now >> 8)
	b[5] = byte(now)
	rand.Read(b[6:8])
	b[6] = (b[6] & 0x0f) | 0x60
	b[8] = (b[8] & 0x3f) | 0x80
	macClean := strings.ReplaceAll(mac, ":", "")
	if macBytes, err := hex.DecodeString(macClean); err == nil && len(macBytes) >= 6 {
		copy(b[10:], macBytes[:6])
	} else {
		rand.Read(b[10:])
	}
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
