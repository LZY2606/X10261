package domain

import "strings"

// Word is a multi-bit four-state value, bits stored MSB-first. Scalar
// signals carry one bit; Z remains Z here and is only coerced to X by
// boolean evaluation.
type Word struct {
	Bits []logicBit
	Raw  string // original VCD token, e.g. "10xz"
}

// logicBit aliases logic.L but domain avoids importing logic to keep
// the dependency graph flat; callers convert with FromWord.
type logicBit = uint8

const (
	B0 uint8 = iota
	B1
	BX
	BZ
)

// NewWord parses a VCD value token without a declared width ("10xz")
// and keeps the raw token. Unparseable characters are retained as X
// with the raw token preserved for diagnostics.
func NewWord(raw string) Word {
	w := Word{Raw: raw}
	for i := 0; i < len(raw); i++ {
		switch raw[i] {
		case '0':
			w.Bits = append(w.Bits, B0)
		case '1':
			w.Bits = append(w.Bits, B1)
		case 'x', 'X':
			w.Bits = append(w.Bits, BX)
		case 'z', 'Z':
			w.Bits = append(w.Bits, BZ)
		}
	}
	return w
}

// Width returns the bit count.
func (w Word) Width() int { return len(w.Bits) }

// Scalar returns the single bit as a four-state value.
func (w Word) Scalar() uint8 {
	if len(w.Bits) == 0 {
		return BX
	}
	return w.Bits[0]
}

// String renders the canonical four-state token.
func (w Word) String() string {
	var b strings.Builder
	for _, bit := range w.Bits {
		switch bit {
		case B0:
			b.WriteByte('0')
		case B1:
			b.WriteByte('1')
		case BX:
			b.WriteByte('x')
		default:
			b.WriteByte('z')
		}
	}
	return b.String()
}

// EdgeKind selects which clock edges define sampling instants.
type EdgeKind string

const (
	PosEdge EdgeKind = "posedge"
	NegEdge EdgeKind = "negedge"
	AnyEdge EdgeKind = "anyedge"
)

// ClockDomain describes a sampling clock and its active edges.
type ClockDomain struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Clock   string   `json:"clockSignal"`
	Edge    EdgeKind `json:"edge"`
	Basis   string   `json:"basis"`
	UnitRef string   `json:"unitRef"`
}

// Edge is a sampling instant on a clock domain.
type Edge struct {
	Tick    int    `json:"tick"`
	TimeFs  int64  `json:"timeFs"`
	RawTime string `json:"rawTime"`
	Kind    string `json:"kind"` // posedge/negedge
}

// ResetInterval is a half-open [StartFs, EndFs) reset region.
type ResetInterval struct {
	ID       string `json:"id"`
	StartFs  int64  `json:"startFs"`
	EndFs    int64  `json:"endFs"`
	StartRaw string `json:"startRaw"`
	EndRaw   string `json:"endRaw"`
	Kind     string `json:"kind"`     // sync/async
	Polarity string `json:"polarity"` // active-high/active-low
	Source   string `json:"source"`   // property:disable-iff / replay-param
	Property string `json:"property,omitempty"`
}

// Contains reports whether t lies in the half-open interval.
func (r ResetInterval) Contains(t int64) bool { return t >= r.StartFs && t < r.EndFs }

// Region names the two sampling regions rendered at every step.
type Region string

const (
	Preponed Region = "preponed" // #1step immediately before the clock edge
	Observed Region = "observed" // value in effect at the edge instant
)
