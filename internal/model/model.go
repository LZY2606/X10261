// Package model defines the domain objects of the signal tribunal:
// HDL waveforms (four-state), clock domains/reset intervals, property ASTs,
// replay attempts/threads, reduced counterexamples and the branch/event ledger.
//
// Every displayable value carries a stable, content-addressed identity so that
// export, database deletion and re-import preserve identity, order and lineage.
package model


// Quad is the Verilog four-state scalar value.
type Quad string

const (
	Q0 Quad = "0"
	Q1 Quad = "1"
	QX Quad = "x"
	QZ Quad = "z"
)

// Valid reports whether q is one of 0/1/x/z.
func (q Quad) Valid() bool {
	switch q {
	case Q0, Q1, QX, QZ:
		return true
	}
	return false
}

// Tri is the explicit three-state result required of every derivation rule.
type Tri string

const (
	TriTRUE    Tri = "true"
	TriFALSE   Tri = "false"
	TriUNKNOWN Tri = "unknown"
)

// VerdictTag explains WHY a verdict node produced its tri result.
// Every tag still maps to exactly one Tri; the tag keeps X/Z propagation,
// disable-iff and empty-match distinguishable at the UI.
type VerdictTag string

const (
	TagPlain      VerdictTag = "plain"       // normal boolean/sequence evaluation
	TagXProp      VerdictTag = "xz_propagate" // tri forced unknown by X/Z
	TagDisabled   VerdictTag = "disabled"     // disable iff active
	TagEmptyMatch VerdictTag = "empty_match"  // zero-length sequence match
)

// NodeVerdict is the explicit tri-state result of one AST node at one tick.
type NodeVerdict struct {
	NodeID  string     `json:"node_id"`
	AtTick  int        `json:"at_tick"`
	Tri     Tri        `json:"tri"`
	Tag     VerdictTag `json:"tag"`
	Detail  string     `json:"detail,omitempty"`
}

// UnitSpec records the unit and coordinate basis of a domain quantity.
// Raw representations are always retained; normalization happens only in the
// derived layer (ps) and is stored alongside, never replacing the raw value.
type UnitSpec struct {
	Raw   string `json:"raw"`   // original textual representation, e.g. "1ns"
	Kind  string `json:"kind"`  // "time" | "frequency" | "dimensionless"
	Scale int64  `json:"scale"` // derived: picoseconds per raw tick (time only)
}

// TimePoint is a timestamp that keeps its origin/raw form plus derived ps.
type TimePoint struct {
	Raw      int64    `json:"raw"`
	Unit     UnitSpec `json:"unit"`
	PS       int64    `json:"ps"`
	OriginID string   `json:"origin_id"` // stable id of the coordinate origin/source
}

// Signal is a declared waveform signal; original width/representation kept.
type Signal struct {
	ID       string `json:"id"`
	VCDID    string `json:"vcd_id"`
	Name     string `json:"name"`
	Width    int    `json:"width"`
	Original string `json:"original"` // original declaration text fragment
}

// Change is one original waveform value change.
type Change struct {
	ID       string    `json:"id"`
	SignalID string    `json:"signal_id"`
	At       TimePoint `json:"at"`
	RawValue string    `json:"raw_value"` // exactly as present in the VCD
	Quad     Quad      `json:"quad,omitempty"`
	Width    int       `json:"width,omitempty"`
	Diagnostic string  `json:"diagnostic,omitempty"` // unconvertible values stay here
}

// EdgeKind is rising/falling/unknown.
type EdgeKind string

const (
	EdgeRising   EdgeKind = "posedge"
	EdgeFalling  EdgeKind = "negedge"
	EdgeUnknown  EdgeKind = "unknown"
)

// ClockEdge is one detected clock edge with its four-state determination.
type ClockEdge struct {
	ID       string    `json:"id"`
	DomainID string    `json:"domain_id"`
	Tick     int       `json:"tick"`
	At       TimePoint `json:"at"`
	Kind     EdgeKind  `json:"kind"`
	Before   Quad      `json:"before"`
	After    Quad      `json:"after"`
	Usable   bool      `json:"usable"` // false on X/Z edges (skipped, diagnosed)
	Diagnostic string  `json:"diagnostic,omitempty"`
}

// ResetInterval marks a reset-active region in raw and derived coordinates.
type ResetInterval struct {
	ID       string    `json:"id"`
	SignalID string    `json:"signal_id"`
	Level    Quad      `json:"level"`
	Start    TimePoint `json:"start"`
	End      TimePoint `json:"end"`
	StartTick int      `json:"start_tick"`
	EndTick   int      `json:"end_tick"`
}

// ClockDomain binds a clock signal, edge polarity and reset specification.
type ClockDomain struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	ClockSigID string          `json:"clock_signal_id"`
	Polarity   EdgeKind        `json:"polarity"`
	ResetSigID string          `json:"reset_signal_id,omitempty"`
	ResetLevel Quad            `json:"reset_level"`
	Edges      []ClockEdge     `json:"edges"`
	Resets     []ResetInterval `json:"resets"`
}

// WaveSource is one imported VCD file (original bytes retained, never cleaned).
type WaveSource struct {
	ID       string    `json:"id"`
	Path     string    `json:"path"`
	ByteHash string    `json:"byte_hash"`
	Bytes    []byte    `json:"-"`
	Timescale UnitSpec `json:"timescale"`
	Signals  []Signal  `json:"signals"`
	Changes  []Change  `json:"changes"`
	Domains  []ClockDomain `json:"domains"`
}
