// Package vcd parses IEEE 1364 Value Change Dump files.
//
// Preservation rules (part of the source/interpretation separation):
//   - raw value tokens are always kept verbatim, including x/z, real
//     numbers and unconvertible junk;
//   - change order inside one time step is retained (StepIndex);
//   - the file timescale is kept as written and only the derived layer
//     converts ticks to femtoseconds;
//   - anything that cannot be converted stays in place with a Diagnostic
//     instead of being dropped to make the picture drawable.
package vcd

import (
	"crypto/sha256"
	"encoding/hex"
)

// ChangeKind classifies a value change token after parsing.
type ChangeKind string

const (
	KindScalar ChangeKind = "scalar"
	KindVector ChangeKind = "vector"
	KindReal   ChangeKind = "real"
	KindInvalid ChangeKind = "invalid" // kept in place, not erased
)

// Signal is one declared $var referenced by the dump.
type Signal struct {
	ID      string // VCD identifier, e.g. "!"
	Name    string // declared name
	Type    string // wire/reg/...
	Width   int    // declared vector width, 1 for scalar
	DimsRaw string // dimension text verbatim, e.g. "[7:0]"
}

// Change is one value-change entry at one simulation time.
type Change struct {
	StepIndex int // 0-based order among changes at the same time
	TimeRaw   string
	TimeFS    int64 // -1 when the tick cannot be converted
	SignalID  string
	RawValue  string // verbatim token, e.g. "0", "b101x", "r3.14"
	Kind      ChangeKind
	Bits      []byte // '0','1','x','z' in declared file order; nil when none
	Problem   string // empty for cleanly converted entries
}

// Diagnostic keeps unconvertible material visible.
type Diagnostic struct {
	Line    int
	Section string
	Message string
	Raw     string
}

// File is the parsed-but-preserved view of one VCD byte stream.
type File struct {
	Path          string
	RawSHA256     string
	ByteSize      int64
	TimescaleRaw  string // e.g. "100ps"
	TimescaleNum  int
	TimescaleUnit string
	FSPerTick     int64 // femtoseconds per simulation tick
	Signals       []Signal
	Changes       []Change
	Diagnostics   []Diagnostic
}

// HashBytes returns the stable content identity of raw bytes.
func HashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
