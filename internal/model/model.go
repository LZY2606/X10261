// Package model holds the persisted domain types shared by the parser,
// replay engine, store and web layer. Every entity that must survive
// export/delete/import round-trips carries an explicit stable ID; SQLite
// rowids are never exposed as identities.
package model

import "time"

// ID is a stable, content-independent identity ("id_…"). IDs survive
// export and re-import into an empty database and must never be reused.
type ID string

// Verdict is the explicit three-state outcome of every assertion attempt
// and of the property as a whole.
type Verdict string

const (
	VerdictHold    Verdict = "hold"    // property held on all completed attempts
	VerdictFail    Verdict = "fail"    // at least one attempt conclusively failed
	VerdictUnknown Verdict = "unknown" // X/Z reached a controlling position: neither hold nor fail
)

// Tri is a local three-valued result used during evaluation.
type Tri uint8

const (
	TriFalse Tri = iota
	TriTrue
	TriUnknown
)

func (t Tri) Verdict() Verdict {
	switch t {
	case TriTrue:
		return VerdictHold
	case TriFalse:
		return VerdictFail
	default:
		return VerdictUnknown
	}
}

// Region labels where, relative to a clock edge, a sampled value was read.
// Every evaluation step is required to carry one of these so the review UI
// can show the exact "sampling region" of each displayed value.
type Region string

const (
	RegionPreponed Region = "preponed" // value sampled in the Preponed region (strictly before the edge)
	RegionObserved Region = "observed" // value observed in the Observed region (at/after the edge, NBA view)
	RegionReactive Region = "reactive" // reactive region used by assertion actions
	RegionAsync    Region = "async"    // value evaluated outside a clock region (reset/disable checks)
	RegionUntimed  Region = "untimed"  // material with no timescale attached yet
)

// Kind tags for materials.
const (
	KindVCD      = "vcd"
	KindClocks   = "clocks"
	KindProperty = "property"
	KindConfig   = "config"
)

// ChangeKind distinguishes ordinary value changes from parser diagnostics.
const (
	ChangeValue     = "value"
	ChangeUnconvert = "unconvertible" // raw token retained, no value invented
)

// Material is an imported source artifact. Raw bytes and a parse
// explanation are stored separately and both are immutable.
type Material struct {
	ID         ID
	ProjectID  ID
	Kind       string
	Name       string
	RawBytes   []byte
	RawSHA256  string
	MediaType  string
	Timescale  string // raw timescale token as written (e.g. "1ns")
	Origin     string // "casepack", "upload", "reinterpret"
	ImportedAt time.Time
	Diagnostic string // non-fatal parse diagnostic, empty when clean
}

// Signal is parsed signal metadata; raw representation is preserved.
type Signal struct {
	ID         ID
	MaterialID ID
	VCDCode    string // raw VCD identifier code
	RawName    string // hierarchical name exactly as written
	Width      int
	Index      int // declaration order, used to keep stable ordering
}

// Change is one raw value-change event from a VCD. Ticks are kept in the
// file's native timescale; Picoseconds is the derived, normalized view.
// Unconvertible tokens are retained verbatim with Diagnostics set.
type Change struct {
	ID          ID
	MaterialID  ID
	SignalID    ID
	Seq         int64 // global sequence index within the material (stable order)
	Tick        int64 // raw timescale tick
	Timescale   string
	Picoseconds int64 // derived normalization; -1 when not convertible
	Region      Region
	Value       string // canonical four-state token ("0/1/x/z"), "" when unconvertible
	RawValue    string // token exactly as it appeared
	Kind        string
	Diagnostics string
}

// ClockDomain binds a clock signal to edge semantics and a sampling region.
type ClockDomain struct {
	ID          ID
	MaterialID  ID
	Name        string
	ClockSignal ID
	Edge        string // "posedge" | "negedge" | "both"
	Region      Region // canonical sampling region for the domain
	SourceStart int64  // raw tick of the first active edge
	SourceEnd   int64  // raw tick of the last active edge
}

// ResetInterval marks an async reset window in native ticks.
type ResetInterval struct {
	ID          ID
	MaterialID  ID
	SignalID    ID
	ActiveValue string // "1" for active-high, "0" for active-low
	StartTick   int64
	EndTick     int64  // inclusive tick of the last active sample
	Kind        string // "power_on" | "runtime"
}

// Property is an assertion definition with its AST and semantic version.
type Property struct {
	ID          ID
	MaterialID  ID
	Name        string
	SourceText  string // exact small-gate language text
	AST         *ASTNode
	SemVer      string // semantic version of the replay rules, e.g. "1.0"
	ClockDomain ID
	ResetSignal ID
	RawLine     int
	Diagnostics string
}

// ASTNode is a serialized syntax node. Kind uses stable vocabulary so ASTs
// remain readable across semantic versions.
type ASTNode struct {
	Kind     string     `json:"kind"`
	Text     string     `json:"text,omitempty"` // identifiers/numbers/raw operators
	Children []*ASTNode `json:"children,omitempty"`
}

// Edge is one clock edge with native and normalized coordinates.
type Edge struct {
	Seq         int
	Tick        int64
	Picoseconds int64
	Region      Region
}
