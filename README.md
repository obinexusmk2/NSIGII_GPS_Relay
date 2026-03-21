# ltcodec

**Linkable Then Codec** — OBINexus Computing  
`github.com/obinexus/ltcodec`

---

## What is ltcodec?

`ltcodec` is the **space-time identity layer** of the OBINexus toolchain. It anchors every payload to a non-reproducible fingerprint made of three things:

| Layer | What | Package |
|-------|------|---------|
| **WHO** | MAC address + UUID v6 + dual IP | `pkg/identity` |
| **WHERE** | GPS coordinates (lat/lon/alt) | `pkg/gps` |
| **WHEN** | delta-T timestamp + monotonic sequence | `pkg/spacetime` |

The combination produces a **SpacetimeState** — a proof of existence at a specific moment, from a specific device, at a specific location. This cannot be forged after the fact.

---

## Pipeline Position

```
riftlang.exe → .so.a → rift.exe → gosilang → ltcodec → nsigii
                                       ↑
                               nlink → polybuild
```

`ltcodec` sits between `gosilang` and `nsigii`. Every NSIGII-encoded payload is first wrapped in an LTF packet by ltcodec.

---

## LTF Format (Linkable Then Format)

LTF is **not** a traditional binary. It asserts three constitutional phases:

1. **LINK** — resolve identity + GPS → produce SpacetimeState
2. **THEN** — bind payload to state → produce LTFPacket  
3. **EXECUTE** — emit signed, location-anchored output

A payload is **not valid** until the LINK phase completes. This is the constitutional step — analogous to the Turing machine's halting state, but in reverse: the state is known *before* execution, not after.

---

## Rectorial Reasoning Connection

The **inverse relay-replay** mechanism is the computational formalisation of rectorial reasoning:

- Standard computation: `input → process → output` (forward only)
- ltcodec: `output → replay → original state` (accountability layer)

The institution (or system) cannot deny its state. The state was captured. It is recorded. It can be replayed at any `delta-T`.

```go
// Replay the system state at any past moment
state, ok := codec.ReplayAt(time.Parse(time.RFC3339, "2026-02-27T07:10:00Z"))
```

---

## GPS Strategy

When GPS hardware is unavailable, ltcodec falls back gracefully:

1. **GPS hardware** (when available) — highest accuracy
2. **IP geolocation** — ~5km accuracy, always available  
3. **Manual** — explicit lat/lon supplied by caller

This is the answer to Cloudflare's lava lamp entropy problem: **location + time + hardware** produces non-reproducible entropy without requiring static physical geometry.

---

## Installation

```bash
go get github.com/obinexus/ltcodec
```

## Usage

```bash
# Encode a file with spacetime anchor
ltcodec encode -input document.pdf -type legal

# View current spacetime state
ltcodec state

# Replay state at a past time (accountability)
ltcodec replay -time 2026-02-27T07:10:00Z
```

---

## Package Structure

```
ltcodec/
├── cmd/ltcodec/       CLI entry point
├── pkg/
│   ├── gps/           GPS coordinate resolution
│   ├── identity/      MAC + UUID v6 + IP fingerprint
│   ├── spacetime/     State capture + relay-replay
│   └── codec/         LTF encode/decode pipeline
└── go.mod
```

---

*OBINexus Computing — #NoGhosting — Constitutional Computing*
