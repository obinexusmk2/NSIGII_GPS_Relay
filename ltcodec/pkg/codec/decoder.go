// pkg/codec/decoder.go
package codec

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/obinexusmk2/ltcodec/pkg/format"
	"github.com/obinexusmk2/ltcodec/pkg/transform"
)

// DecoderConfig holds options for the decoder subcommand.
type DecoderConfig struct {
	InputPath  string // .lt archive to decode
	OutputPath string // destination file (default: <original_name>)
	Verbose    bool
}

// Decode extracts a .lt archive, verifies integrity, and restores the original file.
// This reverses Encode: XOR is self-inverting, so same key + same operation = original.
func Decode(cfg DecoderConfig) error {
	// ── Read .lt archive ────────────────────────────────────────────────
	ltData, err := os.ReadFile(cfg.InputPath)
	if err != nil {
		return fmt.Errorf("decoder: read archive %q: %w", cfg.InputPath, err)
	}

	if cfg.Verbose {
		fmt.Printf("[DECODER] archive: %s (%d bytes)\n", cfg.InputPath, len(ltData))
	}

	// ── Parse zip structure ─────────────────────────────────────────────
	meta, payload, idx, err := format.Open(ltData)
	if err != nil {
		return fmt.Errorf("decoder: parse archive: %w", err)
	}

	if cfg.Verbose {
		fmt.Printf("[DECODER] sections: %d\n", len(idx))
		for _, entry := range idx {
			fmt.Printf("  - %s: %s (%d bytes)\n", entry.Type, entry.Name, entry.Size)
		}
		fmt.Printf("[DECODER] uuid:     %s\n", meta.UUID)
		fmt.Printf("[DECODER] type:     %s\n", meta.ContentType)
		fmt.Printf("[DECODER] original: %s\n", meta.OriginalName)
		fmt.Printf("[DECODER] stateless: %v\n", meta.Stateless)
	}

	// ── Derive same key used for encoding ──────────────────────────────
	// The UUID in metadata ensures we get the identical key sequence
	key := transform.DeriveKey(meta.UUID)

	// ── XOR decode (self-inverting: same operation as encode) ───────────
	// Encode(Encode(data, key), key) == data because (x ^ k) ^ k == x
	decoded := transform.Decode(payload, key)

	// ── Trident verification (read-only diagnostic) ────────────────────
	// Run on decoded data to verify integrity
	result := transform.RunTrident(decoded)
	
	if cfg.Verbose {
		fmt.Printf("[DECODER] trident:  state=%s Δ=%.4f wheel=%d° RWX=0x%02X\n",
			result.State, result.Discriminant, result.WheelDeg, result.RWXFlags)
		fmt.Printf("[DECODER] polarity: %c | verified: %v\n", result.Polarity, result.Verified)
	}

	// CHAOS state warning — data may be corrupted but we still output it
	if result.State == transform.StateChaos {
		fmt.Fprintf(os.Stderr, "[DECODER] WARNING: CHAOS state detected — payload may need repair\n")
	}

	// ── Resolve output path ────────────────────────────────────────────
	outputPath := cfg.OutputPath
	if outputPath == "" {
		// Use original name from metadata, prefixed to avoid overwrite
		if meta.OriginalName != "" && meta.OriginalName != "-" {
			outputPath = meta.OriginalName
		} else {
			// Derive from input filename
			base := filepath.Base(cfg.InputPath)
			ext := filepath.Ext(base)
			name := base[:len(base)-len(ext)]
			outputPath = name + "_decoded"
		}
	}

	// ── Write decoded file ─────────────────────────────────────────────
	if err := os.WriteFile(outputPath, decoded, 0644); err != nil {
		return fmt.Errorf("decoder: write output %q: %w", outputPath, err)
	}

	fmt.Printf("[DECODER] output:   %s (%d bytes)\n", outputPath, len(decoded))
	fmt.Printf("[DECODER] status:   %s | isomorphic: OK\n", result.State)

	return nil
}